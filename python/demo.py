from jms_pam import Client
from jms_pam_config import client_options, instance_id


def apply_account(account):
    # Validate a new connection, switch the pool, then release old connections.
    # Never log account.secret or authentication headers.
    raise NotImplementedError("Implement the application's account update first")


def restart_application():
    raise NotImplementedError("Implement application restart and health check first")


class ApplicationClient(Client):
    def apply_event(self, event):
        if event.get("event") == "application.restart.requested":
            restart_application()
            return
        account = self.get_account(
            account_id=event["account_id"], allow_local_fallback=False,
        )
        if account.revision != event["account_revision"]:
            raise ValueError("The event account version is superseded")
        apply_account(account)

    def handle_event(self, event):
        event_id = event["event_id"]
        if event.get("command_id"):
            claim = self.report_application_command_result(
                command_id=event["command_id"], status="running",
            )
            if not claim.accepted:
                return
        try:
            self.apply_event(event)
        except Exception:
            self.confirm_event(
                event_id=event_id, status="failed", error_code="application_failed",
            )
            raise
        # A fetch or delivery receipt never confirms application success.
        self.confirm_event(event_id=event_id)

    def run(self):
        for event in self.watch_credential_events():
            if event.get("command_id"):
                updates = [event]
            elif event.get("event") == "snapshot":
                updates = event.get("credentials", [])
                # Release application connections absent from the new scope.
            elif event.get("event") == "credential.updated":
                updates = [event]
            elif event.get("event") == "credential.revoked":
                # Release connections for event["account_id"].
                continue
            else:
                continue
            for update in updates:
                try:
                    self.handle_event(update)
                except Exception as error:
                    self.on_event_error(error, update)


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    client.run()
