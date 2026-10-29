"""Subclass hooks with independent event reading and ordered credential delivery."""

from collections import OrderedDict
from dataclasses import dataclass
from queue import Empty, Full, Queue
from threading import Event, Thread, current_thread
from time import monotonic
from typing import Any, Optional


from ._event_names import canonical_event_name

@dataclass
class PendingCredential:
    update: dict[str, Any]
    event: dict[str, Any]
    delay: float
    due: float


@dataclass
class PendingRevocation:
    account_id: str
    event: dict[str, Any]
    delay: float
    due: float


class EventDispatcher:
    def __init__(self, client, stop_event: Optional[Event] = None):
        self.client = client
        self.stop_event = stop_event if stop_event is not None else Event()
        self.finished = Event()
        self.events = Queue(maxsize=128)
        self.pending = {}
        self.pending_revocations = {}
        self.reader = Thread(target=self._read, name="jms-pam-event-reader")
        self.worker = None
        self.reader_error = None
        self.account_events = client._uses_account_events() is True
        self.applied_events = OrderedDict()
        self.used_accounts = {}
        self.account_scope = None

    def start(self, background: bool) -> None:
        self.background = background
        self.worker = (
            Thread(target=self.run, name="jms-pam-event-handler")
            if background
            else current_thread()
        )
        try:
            self.reader.start()
            if background:
                self.worker.start()
        except BaseException:
            self.request_stop()
            if self.reader.ident is not None:
                self.reader.join()
            self.finished.set()
            raise

    def request_stop(self) -> None:
        self.stop_event.set()

    def wait(self) -> None:
        # A hook may stop or close its own client without joining itself.
        if current_thread() not in (self.worker, self.reader):
            self.finished.wait()
            if self.background and self.worker.ident is not None:
                self.worker.join()

    def _put(self, event) -> None:
        while not self.stop_event.is_set():
            try:
                self.events.put(event, timeout=0.1)
                return
            except Full:
                continue

    def _read(self) -> None:
        stream = None
        try:
            stream = self.client.watch_credential_events(self.stop_event)
            for event in stream:
                self._put(event)
                if self.stop_event.is_set():
                    break
        except Exception as error:
            self.reader_error = error
        finally:
            close = getattr(stream, "close", None)
            if close is not None:
                try:
                    close()
                except Exception as error:
                    self.reader_error = error
            self._put(None)

    def _report_error(self, error, event) -> None:
        try:
            self.client.on_event_error(error, event)
        except Exception as hook_error:
            # Both exception messages and event payloads can contain secrets.
            self.client._log_event_error(hook_error)

    def _call(self, handler, value, event) -> None:
        try:
            handler(value)
        except Exception as error:
            self._report_error(error, event)

    @staticmethod
    def _selector(update):
        mode = update.get("credential_mode")
        if mode == "subscription":
            account_id = update.get("account_id")
            key = update.get("credential_key") or update.get("key")
            if isinstance(key, str) and key and isinstance(account_id, str) and account_id:
                if not key.endswith(f":{account_id}"):
                    key = f"{key}:{account_id}"
                return "key", key
        elif mode == "alternating_rotation":
            key = update.get("credential_key") or update.get("key")
            if isinstance(key, str) and key:
                return "key", key
        return None

    def _refresh(self, update, event, delay=0) -> None:
        if self.stop_event.is_set():
            return
        if self.account_events:
            return self._refresh_account(update, event, delay)
        selector = self._selector(update)
        if selector is None:
            return
        previous = self.pending.get(selector)
        if previous is not None:
            previous_revision = previous.update.get("revision")
            revision = update.get("revision")
            if (
                type(previous_revision) is int
                and type(revision) is int
                and previous_revision > revision
            ):
                return
        try:
            credential = self.client.get_credential(
                **{selector[0]: selector[1]}, allow_local_fallback=False
            )
            expected = update.get("revision")
            if type(expected) is int and credential.revision < expected:
                raise ValueError("Credential API revision is behind the event")
            if self.stop_event.is_set():
                return
            self.client.on_credential_changed(credential)
        except Exception as error:
            self._report_error(error, event)
            delay = min(max(1, delay * 2), 30)
            self.pending[selector] = PendingCredential(
                update, event, delay, monotonic() + delay
            )
        else:
            self.pending.pop(selector, None)

    @staticmethod
    def _account_selector(update):
        if update.get('command_id'):
            if update.get('event') in ('credential.updated', 'credential.switch.requested', 'app.restart.requested'):
                return 'command', update['command_id']
            return None
        if update.get('credential_mode') == 'alternating_rotation':
            key = update.get('credential_key') or update.get('key')
            return ('policy', key) if key else None
        if update.get('credential_mode') == 'subscription' and update.get('account_id'):
            return 'account', update['account_id']
        return None

    def _remember_application(self, event_id, result):
        self.applied_events[event_id] = result
        self.applied_events.move_to_end(event_id)
        if len(self.applied_events) > 256:
            self.applied_events.popitem(last=False)

    def _report_usage(self):
        # Usage is telemetry; its failure must never repeat a business action.
        try:
            self.client.report_accounts(sorted(set(self.used_accounts.values())))
        except Exception:
            pass

    def _refresh_account(self, update, event, delay=0):
        selector = self._account_selector(update)
        event_id = update.get('event_id')
        if selector is None or not isinstance(event_id, str) or not event_id:
            return
        usage_selector = self._account_selector({**update, 'command_id': None})
        if update.get('event') != 'app.restart.requested' and self.account_scope is not None and usage_selector not in self.account_scope:
            return
        previous = self.pending.get(selector)
        if previous and previous.update.get('revision', 0) > update.get('revision', 0):
            return
        try:
            result = self.applied_events.get(event_id)
            if result is None:
                if update.get('command_id'):
                    claim = self.client.report_application_command_result(
                        command_id=update['command_id'], status='running',
                    )
                    if not claim.accepted:
                        self.pending.pop(selector, None)
                        return
                try:
                    if update.get('event') == 'app.restart.requested':
                        self.client.on_application_restart()
                    else:
                        if update.get('credential_mode') not in ('subscription', 'alternating_rotation'):
                            raise ValueError('Unsupported credential mode')
                        account = self.client.get_account(
                            account_id=update['account_id'], allow_local_fallback=False,
                        )
                        if account.id != update['account_id'] or type(update.get('account_revision')) is not int or account.revision != update['account_revision']:
                            raise ValueError('The event account version is superseded')
                        if self.stop_event.is_set():
                            return
                        self.client.on_account_changed(account)
                        target = ('policy', update.get('credential_key') or update.get('key')) if update.get('credential_mode') == 'alternating_rotation' else ('account', account.id)
                        self.used_accounts[target] = account.id
                except Exception:
                    if update.get('command_id'):
                        self._remember_application(event_id, ('failed', 'application_failed'))
                    else:
                        try:
                            self.client.confirm_event(event_id=event_id, status='failed', error_code='application_failed')
                        except Exception:
                            pass
                    raise
                result = ('success', '')
                self._remember_application(event_id, result)
            if self.stop_event.is_set():
                return
            self.client.confirm_event(event_id=event_id, status=result[0], error_code=result[1])
            self.pending.pop(selector, None)
            self._report_usage()
        except Exception as error:
            self._report_error(error, event)
            delay = min(max(1, delay * 2), 30)
            self.pending[selector] = PendingCredential(update, event, delay, monotonic() + delay)

    def _revoke_account(self, selector, account_id, event, delay=0):
        if self.stop_event.is_set():
            return
        try:
            self.client.on_account_revoked(account_id)
        except Exception as error:
            self._report_error(error, event)
            delay = min(max(1, delay * 2), 30)
            self.pending_revocations[selector] = PendingRevocation(
                account_id, event, delay, monotonic() + delay,
            )
        else:
            self.pending_revocations.pop(selector, None)
            self.used_accounts.pop(selector, None)
            self.applied_events.clear()
            self._report_usage()

    def _revoke_accounts(self, retained, event):
        # Notification membership and business use are separate. Retain the last
        # business account until it loses authorization; account_scope already
        # prevents detached policies from driving new callbacks.
        retained = retained | {selector for selector, account_id in self.used_accounts.items()
                               if ('account', account_id) in retained}
        for selector in list(self.pending_revocations):
            if selector in retained:
                del self.pending_revocations[selector]
        for selector, account_id in list(self.used_accounts.items()):
            if selector in retained or selector in self.pending_revocations:
                continue
            self._revoke_account(selector, account_id, event)
        self._report_usage()

    def _dispatch(self, event) -> None:
        event = {**event, 'event': canonical_event_name(event.get('event'))}
        name = event.get("event")
        if name in {"snapshot", "credential.revoked", "app.scope.updated", "configuration.updated"}:
            # The following snapshot supplies the currently authorized scope.
            self.pending.clear()
        self._call(self.client.on_event, event, event)
        if self.stop_event.is_set():
            return
        if name == "snapshot":
            if self.account_events:
                self.account_scope = {self._account_selector(item) for item in event.get('credentials', [])}
                try:
                    retained = self.account_scope | {('account', value) for value in event.get('authorized_account_ids', [])}
                    self._revoke_accounts(retained, event)
                except Exception as error:
                    self._report_error(error, event)
            for update in event.get("credentials", []):
                try:
                    self._refresh(update, event)
                except Exception as error:
                    self._report_error(error, event)
        elif name in {'app.scope.updated', 'configuration.updated'} and self.account_events:
            self.account_scope = set()
            if event.get('authorized_account_ids') is not None:
                allowed = set(event['authorized_account_ids'])
                retained = {selector for selector, account_id in self.used_accounts.items()
                            if account_id in allowed}
                retained |= {('account', account_id) for account_id in allowed}
                self._revoke_accounts(retained, event)
        elif self.account_events and event.get('command_id'):
            self._refresh(event, event)
        elif name == "credential.updated" and not event.get("command_id"):
            self._refresh(event, event)
        elif name == "credential.revoked":
            if self.account_events:
                account_id = event.get('account_id')
                key = event.get('credential_key') or event.get('key')
                if self.account_scope is not None:
                    self.account_scope.discard(('account', account_id))
                    self.account_scope.discard(('policy', key))
                retained = {selector for selector, value in self.used_accounts.items()
                            if value != account_id and selector != ('policy', key)}
                try:
                    self._revoke_accounts(retained, event)
                except Exception as error:
                    self._report_error(error, event)
            self._call(self.client.on_credential_revoked, event, event)

    def _retry(self) -> None:
        # Retry at most one credential between events so newer messages can run.
        revoked = min(self.pending_revocations, key=lambda key: self.pending_revocations[key].due, default=None)
        if revoked is not None and (
            not self.pending or self.pending_revocations[revoked].due <= min(item.due for item in self.pending.values())
        ):
            pending = self.pending_revocations[revoked]
            if pending.due <= monotonic():
                self._revoke_account(revoked, pending.account_id, pending.event, pending.delay)
            return
        if not self.pending:
            return
        selector = min(self.pending, key=lambda key: self.pending[key].due)
        pending = self.pending[selector]
        if pending.due <= monotonic():
            self._refresh(pending.update, pending.event, pending.delay)

    def run(self) -> None:
        try:
            while not self.stop_event.is_set():
                try:
                    event = self.events.get(timeout=0.1)
                except Empty:
                    self._retry()
                    continue
                if event is None:
                    if self.reader_error is not None:
                        self._report_error(self.reader_error, None)
                    break
                try:
                    self._dispatch(event)
                except Exception as error:
                    self._report_error(error, event)
                self._retry()
        finally:
            self.request_stop()
            self.reader.join()
            self.pending.clear()
            self.pending_revocations.clear()
            while not self.events.empty():
                self.events.get_nowait()
            self.finished.set()
