"""Live same-account secret updates using isolated demo resources and Core events.

The local adapter changes the real databases, verifies new authentication, and
records Core's managed change lifecycle. It does not run an Ansible/Celery job.
"""

import argparse
import json
import secrets
from pathlib import Path

import psycopg2
import redis

from .core import initialize_django
from .prepare import pg_sql, private_json, redis_acl


def save_json(path, data):
    temporary = path.with_suffix(".tmp.json")
    private_json(temporary, data)
    temporary.replace(path)


def snapshot(parent):
    from .verify import statuses
    root = parent / "subscription"
    resources = json.loads((parent / "resources.json").read_text())
    configs = {name: json.loads((root / filename).read_text())
               for name, filename in (("sdk", "sdk.json"), ("agent", "agent-app.json"))}
    states = statuses(configs)
    if any(value["status"] != "ready" for value in states.values()):
        raise RuntimeError("Both subscription apps must be ready before capturing a password-change baseline")
    instances = {config["instance_id"] for config in configs.values()}
    sessions = json.loads(pg_sql(resources, """
        SELECT coalesce(json_agg(row_to_json(s)), '[]'::json) FROM (
            SELECT pid, usename, application_name FROM pg_stat_activity
            WHERE datname = current_database() AND pid <> pg_backend_pid()
        ) s;
    """, resources["database"]))
    with redis.Redis(host="127.0.0.1", port=resources["redis_port"], username="pam_demo_admin",
                     password=resources["redis_users"]["pam_demo_admin"], decode_responses=True) as admin:
        redis_sessions = [{key: value[key] for key in ("id", "name", "user")}
                          for value in admin.client_list() if value["name"] in instances]
    save_json(root / "before-password.json", {
        "applications": {str(configs[name]["http_port"]): state for name, state in states.items()},
        "postgresql_sessions": [value for value in sessions if value["application_name"] in instances],
        "redis_sessions": redis_sessions,
    })
    print("Captured current subscription identities, credential versions and live session IDs.")


def provision(parent):
    from accounts.models import (
        Account,
        ApplicationCredential,
        CredentialApplicationBinding,
        IntegrationApplication,
    )
    from django.db import transaction
    from orgs.models import Organization
    from orgs.utils import set_current_org

    root = parent / "subscription"
    if (root / "manifest.json").exists():
        print("Reusing the subscription demo, preserving identity and configuration.")
        return
    original = json.loads((parent / "manifest.json").read_text())
    set_current_org(Organization.objects.get(pk=original["org_id"]))
    services = {}
    with transaction.atomic():
        for name, value in original["services"].items():
            rotation = ApplicationCredential.objects.get(pk=value["policy_id"])
            if rotation.status != "idle":
                raise ValueError("Finish the dedicated demo rotation before testing same-account password changes")
            account = Account.objects.get(pk=rotation.active_account_id)
            policy = ApplicationCredential.objects.create(name=f"PAM Subscription {name} {original['tag']}", mode="subscription")
            policy.subscription_accounts.add(account)
            services[name] = {"asset_id": str(account.asset_id), "account_ids": [str(account.id)],
                              "policy_id": str(policy.id), "credential_key": policy.account_key(account.id)}
        app = IntegrationApplication.objects.create(
            name=f"PAM Subscription Demo {original['tag']}", secret=secrets.token_urlsafe(32),
            accounts={"type": "ids", "ids": [value["account_ids"][0] for value in services.values()]},
        )
        for value in services.values():
            CredentialApplicationBinding.objects.create(credential_id=value["policy_id"], application=app)
    for filename, instance, port in (("sdk.json", "sdk", 8108), ("agent-app.json", "agent-app", 8109), ("agent.json", "agent", None)):
        config = json.loads((parent / filename).read_text())
        config["instance_id"] = f"pam-subscription-{instance}-{original['tag']}"
        if "pam" in config:
            config["pam"].update(app_id=str(app.id), app_secret=app.secret)
        if filename == "agent.json":
            config.update(app_id=str(app.id), app_secret=app.secret,
                          state_file=str(root / "agent/state.json"), event_file=str(root / "agent/events.jsonl"))
            config["delivery"].update(delivery_root=str(root / "agent/credentials"), socket_path=str(root / "agent/run/agent.sock"))
        else:
            config["http_port"] = port
            for name in services:
                config[name]["credential_key"] = services[name]["credential_key"]
            if filename == "agent-app.json":
                config["agent_socket"] = str(root / "agent/run/agent.sock")
        private_json(root / filename, config)
    private_json(root / "manifest.json", {"tag": original["tag"], "org_id": original["org_id"],
                                           "application_id": str(app.id), "application_name": app.name,
                                           "resource_root": str(parent), "services": services})
    print("Created", app.name, "with exactly two authorized subscription accounts.")


def password(parent, service_name):
    from accounts.models import (
        Account,
        ApplicationCredential,
        AutomationExecution,
        ChangeSecretRecord,
    )
    from django.db import transaction
    from django.db.models import Q
    from django.utils import timezone
    from orgs.models import Organization
    from orgs.utils import set_current_org

    root = parent / "subscription"
    manifest = json.loads((root / "manifest.json").read_text())
    resources = json.loads((parent / "resources.json").read_text())
    set_current_org(Organization.objects.get(pk=manifest["org_id"]))
    service = manifest["services"][service_name]
    policy = ApplicationCredential.objects.get(pk=service["policy_id"], mode="subscription")
    account = Account.objects.get(pk=service["account_ids"][0], asset_id=service["asset_id"])
    if not policy.name.endswith(manifest["tag"]) or not policy.subscription_accounts.filter(pk=account.id).exists():
        raise ValueError("The subscription no longer belongs to the dedicated demo")
    if ApplicationCredential.objects.filter(
        Q(account=account) | Q(alternate_account=account), mode="alternating_rotation",
    ).exclude(status="idle").exists():
        raise ValueError("A rotation is in progress; do not change its account password")
    new_secret = secrets.token_urlsafe(24)
    old_secret = account.secret
    execution = AutomationExecution.objects.create(snapshot={"name": f"Local subscription demo {service_name}",
                                                             "adapter": "local_database_demo"}, org_id=manifest["org_id"],
                                                    status="running", date_start=timezone.now())
    change = ChangeSecretRecord.objects.create(account=account, asset=account.asset, execution=execution,
                                              old_secret=old_secret, new_secret=new_secret, account_version=account.version)
    # Persist the candidate before touching the real target, as the normal engine does.
    try:
        if service_name == "postgresql":
            username = '"' + account.username.replace('"', '""') + '"'
            literal = psycopg2.extensions.adapt(new_secret).getquoted().decode()
            pg_sql(resources, f"ALTER ROLE {username} PASSWORD {literal};")
        else:
            with redis.Redis(host="127.0.0.1", port=resources["redis_port"], username="pam_demo_admin",
                             password=resources["redis_users"]["pam_demo_admin"], decode_responses=True) as admin:
                # Replace all old passwords; merely adding one would not prove refresh.
                assert admin.execute_command("ACL", "SETUSER", account.username, "resetpass", ">" + new_secret) == "OK"
        def authenticate(secret):
            if service_name == "postgresql":
                connection = psycopg2.connect(host="127.0.0.1", port=5432, dbname=resources["database"],
                                              user=account.username, password=secret, connect_timeout=3)
                connection.close()
            else:
                with redis.Redis(host="127.0.0.1", port=resources["redis_port"], username=account.username,
                                 password=secret, socket_timeout=3, protocol=2) as connection:
                    connection.ping()
        authenticate(new_secret)
        try:
            authenticate(old_secret)
        except (psycopg2.OperationalError, redis.AuthenticationError):
            pass
        else:
            raise RuntimeError("Old password still authenticates new connections")
        resources["pg_users" if service_name == "postgresql" else "redis_users"][account.username] = new_secret
        save_json(parent / "resources.json", resources)
        if service_name == "redis":
            # Rewrite the existing bind-mounted inode, preserving the ACL on restart.
            (parent / "redis/users.acl").write_text(redis_acl(resources))
        with transaction.atomic():
            account.secret = new_secret
            account.save(update_fields=["secret"])
            change.status = "success"
            change.verification_status = "success"
            change.date_finished = change.date_verified = timezone.now()
            change.save(update_fields=["status", "verification_status", "date_finished", "date_verified"])
            execution.status = "success"
            execution.date_finished = timezone.now()
            execution.save(update_fields=["status", "date_finished"])
        save_json(root / f"{service_name}-change.json", {"operation_id": str(change.id), "account_id": str(account.id),
                                                       "old_version": change.account_version, "new_version": account.version,
                                                       "old_password_rejected": True, "new_password_accepted": True})
    except Exception:
        if change.status != "success":
            change.status = "unverified"
            change.error = "local_demo_change_requires_verification"
            change.save(update_fields=["status", "error"])
            execution.status = "failed"
            execution.date_finished = timezone.now()
            execution.save(update_fields=["status", "date_finished"])
        raise  # Recover using the encrypted candidate in this dedicated change record.
    print(service_name, "same account", account.username, "password changed; version", account.version, "operation", change.id)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path, help="Existing parent A/B demo directory")
    parser.add_argument("--core-root", type=Path, default=Path(__file__).resolve().parents[5])
    parser.add_argument("action", choices=("provision", "snapshot", "password", "verify"))
    parser.add_argument("--service", choices=("postgresql", "redis"))
    args = parser.parse_args()
    if args.action == "password" and not args.service:
        parser.error("password requires --service")
    initialize_django(args.core_root)
    if args.action == "provision":
        provision(args.root.resolve())
    elif args.action == "snapshot":
        snapshot(args.root.resolve())
    elif args.action == "password":
        password(args.root.resolve(), args.service)
    else:
        from .verify import verify
        verify(args.root.resolve() / "subscription")


if __name__ == "__main__":
    main()
