"""Verify live traffic, real session identities and Core applied confirmations."""

import argparse
import json
import time
import urllib.request
from dataclasses import replace
from pathlib import Path

import redis

from .app import Application
from .core import initialize_django
from .prepare import pg_sql, private_json


def statuses(configs):
    result = {}
    for name, config in configs.items():
        with urllib.request.urlopen(f"http://127.0.0.1:{config['http_port']}/api/status", timeout=5) as response:
            result[name] = json.load(response)
    return result


def verify(root):
    from accounts.models import (
        Account,
        ApplicationAudit,
        ApplicationCredential,
        CredentialClientStatus,
        CredentialRotationEvent,
    )
    from orgs.models import Organization
    from orgs.utils import set_current_org

    manifest = json.loads((root / "manifest.json").read_text())
    resource_root = Path(manifest.get("resource_root", root))
    resources = json.loads((resource_root / "resources.json").read_text())
    set_current_org(Organization.objects.get(pk=manifest["org_id"]))
    policies = {name: ApplicationCredential.objects.get(pk=value["policy_id"])
                for name, value in manifest["services"].items()}
    accounts = {name: policy.active_account if policy.mode == "alternating_rotation" else
                Account.objects.get(pk=manifest["services"][name]["account_ids"][0])
                for name, policy in policies.items()}
    revisions = {name: policy.revision if policy.mode == "alternating_rotation" else accounts[name].version
                 for name, policy in policies.items()}
    configs = {name: json.loads((root / filename).read_text())
               for name, filename in (("sdk", "sdk.json"), ("agent", "agent-app.json"))}
    before = statuses(configs)
    deadline = time.monotonic() + 30
    while True:
        after = statuses(configs)
        ready = all(state["status"] == "ready" and all(
            state["services"][name]["account_id"] == str(accounts[name].id)
            and state["services"][name]["revision"] == revisions[name]
            and state["services"][name]["authenticated_user"] == accounts[name].username
            and state["services"][name]["successful_writes"] > before[mode]["services"][name]["successful_writes"]
            for name, policy in policies.items()
        ) for mode, state in after.items())
        if ready:
            break
        if time.monotonic() >= deadline:
            raise AssertionError("Live apps did not apply and continuously use the published accounts")
        time.sleep(0.3)
    confirmations = {}
    lifecycle = {}
    for name, policy in policies.items():
        records = list(CredentialClientStatus.objects.filter(
            binding__credential=policy, binding__application_id=manifest["application_id"],
        ).values("client__instance_id", "fetched_revision", "delivered_revision", "applied_revision", "applied_account_id"))
        if policy.mode == "alternating_rotation":
            assert len(records) == 2, "Expected one SDK and one Agent status record"
            assert all(record["applied_revision"] == policy.revision and
                       str(record["applied_account_id"]) == str(policy.active_account_id) for record in records), "Missing Core application confirmation"
        else:
            assert any(record["client__instance_id"] == configs["sdk"]["instance_id"]
                       and record["fetched_revision"] == accounts[name].version for record in records)
            source_ids = ApplicationAudit.objects.filter(credential=policy, revision=accounts[name].version).values("id")
            events = CredentialRotationEvent.objects.filter(source_event_id__in=source_ids, event="credential.updated")
            applied_instances = {recipient["instance_id"] for event in events for recipient in event.recipients
                                 if recipient.get("status") == "success" and recipient.get("account_id") == str(accounts[name].id)
                                 and recipient.get("account_revision") == accounts[name].version}
            agent_identity = json.loads((root / "agent.json").read_text())["instance_id"]
            assert {configs["sdk"]["instance_id"], agent_identity} <= applied_instances, "Missing subscription application results"
            change = json.loads((root / f"{name}-change.json").read_text())
            ids = ApplicationAudit.objects.filter(credential=policy, operation_id=change["operation_id"]).values("id")
            chain = list(CredentialRotationEvent.objects.filter(source_event_id__in=ids).order_by("published_at").values_list("event", flat=True))
            assert chain == ["credential.change.started", "credential.change.completed", "credential.updated"], chain
            assert change["old_password_rejected"] and change["new_password_accepted"]
            lifecycle[name] = {**change, "events": chain, "applied_instances": sorted(applied_instances)}
        confirmations[name] = [{**record, "applied_account_id": str(record["applied_account_id"])} for record in records]
    dbname = resources["database"].replace("'", "''")
    sessions = json.loads(pg_sql(resources, f"""
        SELECT coalesce(json_agg(row_to_json(s)), '[]'::json) FROM (
            SELECT pid, usename, application_name FROM pg_stat_activity
            WHERE datname = '{dbname}' AND pid <> pg_backend_pid()
        ) s;
    """, resources["database"]))
    instances = {config["instance_id"] for config in configs.values()}
    pg_sessions = [value for value in sessions if value["application_name"] in instances]
    assert len(pg_sessions) == 2 and all(s["usename"] == accounts["postgresql"].username for s in pg_sessions), "Old PostgreSQL application sessions remain"
    with redis.Redis(host="127.0.0.1", port=resources["redis_port"], username="pam_demo_admin",
                     password=resources["redis_users"]["pam_demo_admin"], decode_responses=True) as admin:
        redis_sessions = [{key: value[key] for key in ("id", "name", "user")}
                          for value in admin.client_list() if value["name"] in instances]
    assert len(redis_sessions) == 2 and all(s["user"] == accounts["redis"].username for s in redis_sessions), "Old Redis application sessions remain"
    if lifecycle:
        previous = json.loads((root / "before-password.json").read_text())
        assert {s["pid"] for s in pg_sessions}.isdisjoint(s["pid"] for s in previous["postgresql_sessions"]), "PostgreSQL continued with old authenticated connections"
        assert {s["id"] for s in redis_sessions}.isdisjoint(s["id"] for s in previous["redis_sessions"]), "Redis continued with old authenticated connections"
        for mode, config in configs.items():
            before_service = previous["applications"][str(config["http_port"])]["services"]
            assert all(after[mode]["services"][name]["account_id"] == before_service[name]["account_id"]
                       and after[mode]["services"][name]["account_revision"] > before_service[name]["account_revision"]
                       for name in accounts), "Expected the same account with a new credential version"
    # Invalid passwords are tested with a separate probe instance, preserving
    # the running applications and their event confirmation state.
    probe = Application({**configs["agent"], "instance_id": configs["agent"]["instance_id"] + "-negative-check"})
    negative = {}
    try:
        for name in policies:
            credential = probe.fetch(name)
            probe.apply(name, credential)
            current = probe.connections[name]
            try:
                probe.apply(name, replace(credential, secret="invalid-demo-password", account_revision=credential.account_revision + 1))
            except Exception as error:
                assert probe.connections[name] is current
                assert current.probe()["authenticated_user"] == credential.username
                assert not probe.confirmed
                negative[name] = type(error).__name__
            else:
                raise AssertionError("Invalid database credentials were accepted")
    finally:
        probe.close()
    result = {"verified_at": time.time(), "application": manifest["application_name"],
              "checks": {"live_reads_and_writes": True, "database_authenticated_identities": True,
                         "core_applied_confirmations": True, "old_application_sessions_released": True,
                         "invalid_credentials_preserve_old_connections_without_confirmation": True},
              "applications": after, "core_confirmations": confirmations, "secret_change_lifecycle": lifecycle,
              "postgresql_sessions": pg_sessions, "redis_sessions": redis_sessions,
              "invalid_credential_errors": negative,
              "scope": "Real local Core, PostgreSQL and dedicated Redis. Local test adapters verify and change credentials; Ansible/Celery jobs are not executed."}
    temporary = root / "verification.tmp.json"
    private_json(temporary, result)
    temporary.replace(root / "verification.json")
    print("PASS: SDK + Agent real writes, authenticated users, Core applied confirmations, old sessions released, invalid credentials rejected.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--core-root", type=Path, default=Path(__file__).resolve().parents[5])
    args = parser.parse_args()
    initialize_django(args.core_root)
    verify(args.root.resolve())


if __name__ == "__main__":
    main()
