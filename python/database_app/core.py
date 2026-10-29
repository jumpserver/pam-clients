"""Provision only this demo's Core records, or publish a controlled test switch.

Use the Core virtualenv. The switch command is a development harness: it verifies
the target databases itself, then exercises real Core delivery/confirmation. It
does not simulate completion of the full scheduled password-rotation workflow.
"""

import argparse
import json
import logging
import os
import pwd
import sys
from pathlib import Path

import psycopg2
import redis

from .prepare import private_json


def initialize_django(core_root):
    sys.path.insert(0, str(core_root / "apps"))
    sys.path.insert(0, str(core_root))
    os.environ.setdefault("DJANGO_SETTINGS_MODULE", "jumpserver.settings")
    from django.conf import settings
    settings.DEBUG = settings.DEBUG_DEV = False
    for logger in settings.LOGGING.get("loggers", {}).values():
        logger["level"] = "ERROR"
    import django
    django.setup()
    logging.disable(logging.CRITICAL)


def provision(root, endpoint):
    from accounts.models import (
        Account,
        ApplicationCredential,
        CredentialApplicationBinding,
        IntegrationApplication,
    )
    from assets.models import Database, Platform, Protocol
    from django.db import transaction
    from orgs.models import Organization
    from orgs.utils import set_current_org

    if (root / "manifest.json").exists():
        print("Core demo records already exist; keeping credentials and configuration unchanged.")
        return
    resources = json.loads((root / "resources.json").read_text())
    if not (root / "prepared.json").exists():
        raise RuntimeError("Resource preparation did not finish; resolve that before provisioning Core")
    org = Organization.default()
    set_current_org(org)
    tag = resources["tag"]
    services = {}
    with transaction.atomic():
        for name, users, port, platform_name in (
            ("postgresql", resources["pg_users"], 5432, "PostgreSQL"),
            ("redis", {u: p for u, p in resources["redis_users"].items() if u != "pam_demo_admin"},
             resources["redis_port"], "Redis6+"),
        ):
            platform = Platform.objects.get(name=platform_name)
            asset = Database.objects.create(
                name=f"PAM Demo {name} {tag}", address="127.0.0.1", platform=platform,
                db_name=resources["database"] if name == "postgresql" else "0",
            )
            Protocol.objects.create(asset=asset, name=name, port=port)
            accounts = [Account.objects.create(name=username, username=username, asset=asset, secret=password)
                        for username, password in users.items()]
            policy = ApplicationCredential.objects.create(
                name=f"PAM Demo {name} {tag}", mode="alternating_rotation", account=accounts[0],
                alternate_account=accounts[1], active_account=accounts[0], source_no_traffic_days=1,
            )
            services[name] = {"asset_id": str(asset.id), "account_ids": [str(a.id) for a in accounts],
                              "policy_id": str(policy.id), "credential_key": policy.key}
        import secrets
        application = IntegrationApplication.objects.create(
            name=f"PAM Database Demo {tag}", secret=secrets.token_urlsafe(32),
            accounts={"type": "ids", "ids": [value for service in services.values() for value in service["account_ids"]]},
        )
        for service in services.values():
            CredentialApplicationBinding.objects.create(credential_id=service["policy_id"], application=application)
        manifest = {"tag": tag, "org_id": str(org.id), "application_id": str(application.id),
                    "application_name": application.name, "services": services}
    private_json(root / "manifest.json", manifest)
    pam = {"endpoint": endpoint, "app_id": str(application.id), "app_secret": application.secret,
           "org_id": str(org.id), "timeout": 5}
    targets = {"postgresql": {"host": "127.0.0.1", "port": 5432, "database": resources["database"],
                               "credential_key": services["postgresql"]["credential_key"]},
               "redis": {"host": "127.0.0.1", "port": resources["redis_port"],
                         "credential_key": services["redis"]["credential_key"]}}
    private_json(root / "sdk.json", {"version": 1, "mode": "sdk", "instance_id": f"pam-demo-sdk-{tag}",
                                     "http_port": 8098, "pam": pam, **targets})
    agent_root = root / "agent"
    socket_path = str(agent_root / "run/agent.sock")
    private_json(root / "agent-app.json", {"version": 1, "mode": "agent",
                                           "instance_id": f"pam-demo-agent-app-{tag}", "http_port": 8099,
                                           "agent_socket": socket_path, **targets})
    agent_pam = {key: value for key, value in pam.items() if key != "timeout"}
    private_json(root / "agent.json", {"version": 1, **agent_pam,
                                       "instance_id": f"pam-demo-agent-{tag}", "pull_interval": 2,
                                       "state_file": str(agent_root / "state.json"),
                                       "event_file": str(agent_root / "events.jsonl"),
                                       "delivery": {"delivery_mode": "json", "delivery_root": str(agent_root / "credentials"),
                                                    "socket_path": socket_path, "app_user": pwd.getpwuid(os.getuid()).pw_name},
                                       "rules": []})
    print("Created", application.name, "with exactly four authorized demo accounts.")


def switch(root, service_name):
    from accounts.credential_rotation.manager import CredentialRotationManager
    from accounts.models import ApplicationCredential, CredentialClientStatus
    from django.db import transaction
    from orgs.models import Organization
    from orgs.utils import set_current_org

    manifest = json.loads((root / "manifest.json").read_text())
    resources = json.loads((root / "resources.json").read_text())
    set_current_org(Organization.objects.get(pk=manifest["org_id"]))
    service = manifest["services"][service_name]
    manager = CredentialRotationManager(service["policy_id"])
    with transaction.atomic():
        policy = manager._get_locked_credential()
        if not policy.name.endswith(manifest["tag"]) or {str(policy.account_id), str(policy.alternate_account_id)} != set(service["account_ids"]):
            raise ValueError("The policy no longer belongs to this demo")
        if policy.status != ApplicationCredential.Status.idle:
            raise ValueError("An existing rotation is in progress; finish or cancel it before another test switch")
        target = policy.target_account
        if service_name == "postgresql":
            connection = psycopg2.connect(host="127.0.0.1", port=5432, dbname=resources["database"],
                                          user=target.username, password=target.secret, connect_timeout=3)
            try:
                with connection.cursor() as cursor:
                    cursor.execute("SELECT current_user")
                    assert cursor.fetchone()[0] == target.username
            finally:
                connection.close()
        else:
            with redis.Redis(host="127.0.0.1", port=resources["redis_port"], username=target.username,
                             password=target.secret, socket_timeout=3) as connection:
                assert connection.execute_command("ACL", "WHOAMI").decode() == target.username
        manager._publish(policy, operator="Dedicated database demo target verified by live connection")
        print("Published", service_name, "revision", policy.revision, "to", target.username)
    # Stop at waiting_switch; neither the observation gate nor old-secret change is skipped.
    print("Participants:", CredentialClientStatus.objects.filter(binding__credential=policy).count())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--core-root", type=Path, default=Path(__file__).resolve().parents[5])
    parser.add_argument("--endpoint", default="http://127.0.0.1:8080")
    parser.add_argument("action", choices=("provision", "switch", "revert", "finish-revert"))
    parser.add_argument("--service", choices=("postgresql", "redis"))
    args = parser.parse_args()
    if args.action != "provision" and not args.service:
        parser.error("this action requires --service")
    initialize_django(args.core_root)
    if args.action == "provision":
        provision(args.root.resolve(), args.endpoint)
    elif args.action == "switch":
        switch(args.root.resolve(), args.service)
    else:
        from accounts.credential_rotation.manager import CredentialRotationManager
        from django.db import transaction
        from orgs.models import Organization
        from orgs.utils import set_current_org
        manifest = json.loads((args.root / "manifest.json").read_text())
        set_current_org(Organization.objects.get(pk=manifest["org_id"]))
        service = manifest["services"][args.service]
        manager = CredentialRotationManager(service["policy_id"])
        with transaction.atomic():
            policy = manager._get_locked_credential()
            if not policy.name.endswith(manifest["tag"]) or {str(policy.account_id), str(policy.alternate_account_id)} != set(service["account_ids"]):
                raise ValueError("The policy no longer belongs to this demo")
            if args.action == "revert":
                policy = manager.cancel(reason="Dedicated demo recovery verification; old passwords were not changed")
            else:
                policy, blockers = manager.complete()
                if blockers:
                    raise RuntimeError("Recovery has not been confirmed by all participants")
        print(args.service, policy.status, "revision", policy.revision, "using", policy.active_account.username)


if __name__ == "__main__":
    main()
