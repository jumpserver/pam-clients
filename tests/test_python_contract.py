"""Verify the Python baseline against the same wire contract as other SDKs."""

import json
import os
import sys
import unittest
from contextlib import closing
from pathlib import Path
from threading import Timer
from urllib.parse import urlencode
from urllib.request import urlopen

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "python"))

from jms_pam import Client, PAMError
from jms_pam.models import KnownRevision


@unittest.skipUnless(os.environ.get("JMS_TEST_ENDPOINT"), "Run through tests/run.py")
class PythonContractTests(unittest.TestCase):
    def control(self, instance, fault):
        url = os.environ["JMS_TEST_ENDPOINT"].removesuffix("/prefix") + "/__control?" + urlencode({"instance": instance, "fault": fault})
        with urlopen(url) as response:
            self.assertEqual(response.status, 200)

    def test_compatibility_errors_preserve_cache_but_fail_explicitly(self):
        instance = "python-compat-cycle"
        with self.client(instance) as client:
            initial = client.get_account(account_id="account")
            self.control(instance, 426)
            for fresh in (False, True):
                with self.assertRaises(PAMError) as result:
                    client.get_account(account_id="account", allow_local_fallback=not fresh)
                self.assertTrue(result.exception.is_compatibility_error)
            self.control(instance, 503)
            retained = client.get_account(account_id="account")
            self.assertTrue(retained.from_local)
            self.assertEqual(retained.secret, initial.secret)
            self.control(instance, 401)
            with self.assertRaises(PAMError) as result:
                client.get_account(account_id="account")
            self.assertFalse(result.exception.is_compatibility_error)
            self.control(instance, 503)
            with self.assertRaises(PAMError):
                client.get_account(account_id="account")

    def test_optional_metadata_compatibility(self):
        with self.client("python-compat-legacy") as client:
            self.assertIsNone(client.get_client_info().server_capabilities)
        with self.client("python-compat-capabilities") as client:
            self.assertIn("future_optional_capability", client.get_client_info().server_capabilities)
        with self.client("python-compat-bad-capabilities") as client, self.assertRaises(PAMError):
            client.get_client_info()
        instance = "python-compat-missing"
        with self.client(instance) as client:
            client.get_account(account_id="account")
            with self.assertRaises(PAMError):
                client.get_client_info()
            self.control(instance, 503)
            self.assertTrue(client.get_account(account_id="account").from_local)

    def test_malformed_426_response_is_not_a_temporary_outage(self):
        for fault in ("426-invalid", "426-timeout"):
            instance = "python-compat-" + fault
            with self.client(instance, timeout=0.1) as client:
                key = "cache-" + fault
                client.get_credential(key=key)
                with self.assertRaises(PAMError) as result:
                    client.get_credential(key=key)
                self.assertTrue(result.exception.is_compatibility_error)
                self.assertEqual(len(client._latest_credentials), 1)
                self.control(instance, 503)
                self.assertTrue(client.get_credential(key=key).from_local)

    def client(self, instance="python-contract", **options):
        return Client(
            os.environ["JMS_TEST_ENDPOINT"],
            app_id="contract-app",
            app_secret="contract-secret",
            org_id="contract-org",
            instance_id=instance,
            **options,
        )

    def test_account_projection_and_event_results(self):
        with self.client("python-account-event") as client:
            account = client.get_account(account_id="account", allow_local_fallback=False)
            self.assertEqual(account.username, "app")
            self.assertEqual(account.asset.address, "127.0.0.1")
            self.assertFalse(account.from_local)
            self.assertEqual(account.revision, 2)
            self.assertNotIn("DO_NOT_LOG_SECRET", repr(account))
            self.assertTrue(client.confirm_event(event_id="event-one").accepted)
            self.assertEqual(client.confirm_event(event_id="event-two", status="failed", error_code="application_failed").status, "failed")

    def test_status_reporting_is_best_effort_and_preserves_credentials(self):
        with self.client("python-status") as client:
            self.assertTrue(client.report_status("running", account_id="account"))
            info = client.get_client_info()
            self.assertEqual(info.application_status.account_ids, ["account"])
            self.assertEqual(info.application_status.status, "running")
            self.assertIsNotNone(info.date_status_reported)
            self.assertTrue(client.report_status("degraded", account_ids=["account", "backup"], error_code="db_unavailable"))
            self.assertEqual(client.get_client_info().application_status.account_ids, ["account", "backup"])
            client.get_credential(key="cache-503")
            for code in ("fault_401", "fault_403", "fault_404", "fault_503", "fault_invalid", "fault_network"):
                self.assertFalse(client.report_status("running", error_code=code))
                with self.assertRaises(PAMError):
                    client.report_status("running", error_code=code, raise_on_error=True)
            self.assertTrue(client.get_credential(key="cache-503").from_local)
            self.assertTrue(client.report_status("stopped"))
            self.assertEqual(client.get_client_info().application_status.account_ids, [])
            with self.assertRaises(ValueError): client.report_status("unknown")
            with self.assertRaises(ValueError): client.report_status("running", account_ids=["account", "account"])
            with self.assertRaises(ValueError): client.report_status("error", error_code="DO NOT SEND SECRETS")
            client.close()
            self.assertFalse(client.report_status("stopped"))
            with self.assertRaises(RuntimeError): client.report_status("stopped", raise_on_error=True)
        with self.client("python-status-timeout", timeout=0.05) as client:
            self.assertFalse(client.report_status("running", error_code="fault_timeout"))

    def test_application_client_and_bound_policy_metadata(self):
        with self.client("python-metadata") as client:
            application = client.get_application()
            self.assertEqual(application.id, "contract-app")
            self.assertEqual(application.org_id, "contract-org")
            self.assertEqual(application.accounts_count, 3)
            replica = client.get_client_info()
            self.assertEqual(replica.instance_id, "python-metadata")
            self.assertEqual(replica.type, "sdk")
            self.assertEqual(replica.config_schema_version, 0)
            policies = client.list_policies()
            self.assertEqual(len(policies), 2)
            self.assertFalse(policies[1].is_active)
            self.assertIsNone(policies[1].active_account_id)
            self.assertEqual(policies[0].account_switch["account_ids"], ["account", "backup"])
            self.assertEqual(client.get_policy(key=policies[0].key), policies[0])
            self.assertEqual(client.get_policy(policy_id=policies[0].id), policies[0])
            self.assertEqual(client.list_policies(search="db/空 格?=&"), [policies[0]])
            self.assertEqual(client.list_policies(search="no-match"), [])
            for item in (application, replica, *policies):
                self.assertFalse(hasattr(item, "secret"))
                self.assertNotIn("DO_NOT_LOG_SECRET", repr(item))
            with urlopen(os.environ["JMS_TEST_ENDPOINT"].removesuffix("/prefix") + "/__stats") as response:
                requests = json.load(response)["requests"]
            self.assertTrue(all(item["path"].endswith(("/application/", "/client/", "/policies/", "/policy/")) for item in requests if item["data"]["instance_id"] == "python-metadata"))

    def test_metadata_validation_and_missing_policies(self):
        with self.client("python-invalid-metadata") as client:
            for method in (client.get_application, client.get_client_info):
                with self.assertRaises(PAMError) as failure:
                    method()
                self.assertEqual(failure.exception.code, "ResponseError")
            for search in ("fault:invalid-list", "fault:duplicate", "fault:invalid-policy"):
                with self.assertRaises(PAMError):
                    client.list_policies(search=search)
            for options in ({}, {"key": "db", "policy_id": "id"}, {"key": ""}, {"key": "x" * 65}):
                with self.assertRaises(ValueError): client.get_policy(**options)
            with self.assertRaises(ValueError): client.list_policies(search="x" * 129)
            with self.assertRaises(PAMError) as failure: client.get_policy(key="missing")
            self.assertEqual(failure.exception.status_code, 404)
            with self.assertRaises(PAMError) as failure: client.get_policy(key="invalid")
            self.assertEqual(failure.exception.code, "ResponseError")

    def test_account_listing_pages_search_and_metadata(self):
        with self.client("python-accounts") as client:
            accounts = client.list_accounts()
            self.assertEqual(len(accounts), 205)
            self.assertEqual(len({item.id for item in accounts}), 205)
            self.assertEqual(accounts[0].asset.platform.type, "postgresql")
            self.assertEqual(accounts[0].credentials[0].account_switch["account_ids"], ["account-0", "backup"])
            self.assertEqual(accounts[1].credentials[0].mode, "subscription")
            self.assertEqual(accounts[2].credentials, [])
            self.assertEqual(accounts[2].username, "")
            self.assertFalse(hasattr(accounts[0], "secret"))
            self.assertNotIn("DO_NOT_LOG_SECRET", repr(accounts))
            page = client.list_accounts_page(limit=2, offset=1)
            self.assertEqual(page.count, 205)
            self.assertEqual([item.id for item in page.accounts], ["account-1", "account-2"])
            self.assertEqual(client.list_accounts_page(offset=205).accounts, [])
            self.assertEqual([item.id for item in client.list_accounts(search="db/空 格?=&")], ["account-0"])
            self.assertEqual(client.list_accounts(search="no-match"), [])
            self.assertEqual(client.list_accounts(search="fault:empty"), [])
            # Enumeration must not call the secret endpoint or report a fetch/application.
            with urlopen(os.environ["JMS_TEST_ENDPOINT"].removesuffix("/prefix") + "/__stats") as response:
                requests = json.load(response)["requests"]
            self.assertTrue(all(item["path"].endswith("/accounts/") for item in requests if item["data"]["instance_id"] == "python-accounts"))

    def test_account_listing_rejects_invalid_and_incomplete_results(self):
        with self.client("python-errors-accounts") as client:
            for options in [
                {"limit": 0}, {"limit": 501}, {"limit": True}, {"limit": 1.5},
                {"offset": -1}, {"offset": True}, {"search": None}, {"search": "x" * 129},
            ]:
                with self.subTest(options=options), self.assertRaises(ValueError):
                    client.list_accounts_page(**options)
            for fault in ["invalid-count", "null-count", "missing-policy-revision", "missing-username", "null-policies", "invalid-account", "invalid-policy", "invalid-list", "oversized", "duplicate-page", "incomplete", "duplicate", "changing-count", "forbidden"]:
                with self.subTest(fault=fault), self.assertRaises(PAMError) as failure:
                    client.list_accounts(search="fault:" + fault)
                self.assertEqual(failure.exception.code, "credential_not_authorized" if fault == "forbidden" else "ResponseError")

    def test_signed_credential_and_confirmation(self):
        with self.client() as client:
            credential = client.get_credential(key="db /中文?&=")
            self.assertEqual(credential.key, "db /中文?&=")
            self.assertEqual(credential.account.username, "app")
            self.assertNotIn("DO_NOT_LOG_SECRET", repr(credential))
            self.assertEqual(client.get_credential(account_id="account").revision, 2)
            self.assertEqual(
                client.confirm_credential(
                    key="db", revision=2, account_id="account"
                ).revision,
                2,
            )

    def test_errors_and_validation(self):
        with self.client() as client:
            for key, status in [("forbidden", 403), ("upgrade", 426)]:
                with self.assertRaises(PAMError) as failure:
                    client.get_credential(key=key)
                self.assertEqual(failure.exception.status_code, status)
            for key in ["invalid-json", "invalid-revision"]:
                with self.assertRaises(PAMError):
                    client.get_credential(key=key)
            with self.assertRaises(ValueError):
                client.get_credential(key="db", account_id="account")

    def test_agent_sync(self):
        with self.client(source="jms-pam-agent") as client:
            result = client.sync_agent(
                credentials=[KnownRevision("db", 1)],
                delivered_credentials=[KnownRevision("db", 1)],
            )
            self.assertTrue(result.credentials[0].available)
            self.assertEqual(result.scope["credential_keys"], ["db"])
            with self.assertRaises(PAMError):
                client.sync_agent(
                    credentials=[],
                    delivered_credentials=[],
                    sync_error="invalid-flag",
                )

    def test_command_claim_and_original_error(self):
        with self.client() as client:
            commands = client.list_application_commands()
            called = []
            self.assertTrue(
                client.execute_application_command(commands[0], called.append).accepted
            )
            self.assertEqual(len(called), 1)
            duplicate = {**commands[0], "command_id": "duplicate"}
            self.assertFalse(
                client.execute_application_command(duplicate, called.append).accepted
            )
            self.assertEqual(len(called), 1)
            original = RuntimeError("Application switch failed")

            def fail(_):
                raise original

            with self.assertRaises(RuntimeError) as failure:
                client.execute_application_command(
                    {**commands[0], "command_id": "report-failure"},
                    fail,
                )
            self.assertIs(failure.exception, original)

    def test_events_reconnect_receipts_and_clone(self):
        instance = "python-events"
        client = self.client(instance)
        clone = client.clone()
        stop = Timer(8, client.close)
        stop.start()
        try:
            events = []
            with closing(client.watch_credential_events()) as stream:
                for event in stream:
                    events.append(event)
                    if (
                        event["event"] == "snapshot"
                        and event["credentials"][0]["revision"] == 3
                    ):
                        break
            self.assertEqual(
                [event["event"] for event in events],
                [
                    "snapshot",
                    "credential.updated",
                    "future.notification",
                    "snapshot",
                ],
            )
            self.assertEqual(events[2]["future_payload"]["notice"], "preserved")
            endpoint = os.environ["JMS_TEST_ENDPOINT"].removesuffix("/prefix")
            with urlopen(endpoint + "/__stats") as response:
                stats = json.load(response)
            self.assertFalse(stats["failures"])
            received = {
                item["eventId"]
                for item in stats["receipts"]
                if item["instance"] == instance
            }
            self.assertTrue({"updated-1", "future-1"}.issubset(received))
            client.close()
            self.assertEqual(clone.get_credential(key="db").revision, 2)
        finally:
            stop.cancel()
            client.close()
            clone.close()


if __name__ == "__main__":
    unittest.main()
