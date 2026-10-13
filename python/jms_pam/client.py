"""Python client for credential access, event streams, and application commands."""

import logging
import math
import re
from dataclasses import replace
from email.utils import formatdate
from threading import Event, Lock
from typing import Any, Callable, Iterable, Iterator, Optional, TypeVar
from urllib.parse import urlencode, urlsplit, urlunsplit

import requests

from ._auth import HTTPSignatureAuth
from ._commands import execute_command
from ._event_dispatcher import EventDispatcher
from ._events import EventStream
from ._transport import Transport
from ._version import CONFIG_SCHEMA_VERSION, PROTOCOL_VERSION, __version__
from .exceptions import PAMError
from .models import (
    Account,
    AccountsPage,
    AgentSync,
    ApplicationInfo,
    AuthorizedAccount,
    ClientInfo,
    CommandResult,
    Credential,
    CredentialConfirmation,
    CredentialPolicy,
    KnownRevision,
)

CLIENT_PATH = "/api/v1/accounts/credential-client"
DEFAULT_ORG_ID = "00000000-0000-0000-0000-000000000002"
Response = TypeVar("Response")
logger = logging.getLogger(__name__)


class Client:
    """A signed client for one application replica.

    Use a stable, unique instance_id and close the client when finished,
    preferably with a context manager. Each clone owns its HTTP session.
    """

    def __init__(
        self,
        endpoint: str,
        *,
        app_id: str,
        app_secret: str,
        instance_id: str,
        org_id: str = DEFAULT_ORG_ID,
        timeout: float = 10,
        source: str = "jms-pam",
    ):
        if not all(isinstance(value, str) and value for value in (app_id, app_secret)):
            raise ValueError("app_id and app_secret are required")
        if not isinstance(endpoint, str) or not endpoint:
            raise ValueError("endpoint must be a non-empty HTTP(S) URL")
        parsed = urlsplit(endpoint)
        if (
            parsed.scheme not in ("http", "https")
            or not parsed.netloc
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError(
                "endpoint must be an HTTP(S) URL without credentials, query or fragment"
            )
        if (
            isinstance(timeout, bool)
            or not isinstance(timeout, (int, float))
            or not math.isfinite(timeout)
            or timeout <= 0
        ):
            raise ValueError("timeout must be a positive finite number")
        if (
            not isinstance(instance_id, str)
            or not instance_id.strip()
            or instance_id != instance_id.strip()
            or len(instance_id) > 128
        ):
            raise ValueError(
                "instance_id must be a stable, unique ID with 1-128 characters "
                "and no surrounding whitespace"
            )
        self.endpoint = endpoint.rstrip("/")
        self.org_id = org_id
        self.timeout = timeout
        self.source = source
        self.instance_id = instance_id
        self._app_id = app_id
        self._app_secret = app_secret
        self.auth = HTTPSignatureAuth(app_id, app_secret)
        self._transport = Transport()
        self._streams = set()
        self._streams_lock = Lock()
        self._events_lock = Lock()
        self._event_dispatcher = None
        self._closed = False
        self._credentials_lock = Lock()
        self._latest_credentials = {}
        self._credential_generation = 0

    def _request(
        self,
        method: str,
        path: str,
        data: dict[str, Any],
        parse: Callable[[dict[str, Any]], Response],
        query: bool = False,
        invalidate_credentials: bool = True,
    ) -> Response:
        data = {key: value for key, value in data.items() if value is not None}
        data["instance_id"] = self.instance_id
        try:
            return self._transport.request(
                method,
                f"{self.endpoint}{path}",
                parse,
                params=data if query else None,
                json=None if query else data,
                headers=self._headers(),
                auth=self.auth,
                timeout=self.timeout,
            )
        except PAMError as error:
            if invalidate_credentials and (
                error.status_code in (401, 403, 404)
                or error.code == "client_upgrade_required"
                or (error.status_code == 400 and error.code == "credential_not_found")
            ):
                with self._credentials_lock:
                    self._latest_credentials.clear()
                    self._credential_generation += 1
            raise

    def _headers(self) -> dict[str, str]:
        return {
            "Accept": "application/json",
            "X-JMS-ORG": self.org_id,
            "X-Source": self.source,
            "X-JMS-Client-Version": __version__,
            "X-JMS-Protocol-Version": str(PROTOCOL_VERSION),
            "X-JMS-Config-Schema-Version": (
                str(CONFIG_SCHEMA_VERSION) if self.source == "jms-pam-agent" else "0"
            ),
            "Date": formatdate(usegmt=True),
        }

    @property
    def session(self) -> requests.Session:
        """The owned session; use clone() when an independent session is needed."""
        return self._transport.session

    def get_credential(
        self,
        *,
        key: Optional[str] = None,
        account_id: Optional[str] = None,
        allow_local_fallback: bool = True,
    ) -> Credential:
        """Fetch by policy key or pull-authorized account ID; supply exactly one.

        Retain the latest successful credential without expiry. Only temporary
        backend failures may return it with from_local=True. Use
        allow_local_fallback=False when applying an event or confirming a rotation.
        """
        if (key is None) == (account_id is None) or not all(
            isinstance(value, str) and value
            for value in (key, account_id)
            if value is not None
        ):
            raise ValueError("exactly one of key or account_id is required")
        if type(allow_local_fallback) is not bool:
            raise ValueError("allow_local_fallback must be a boolean")
        selector = ("key", key) if key is not None else ("account_id", account_id)
        with self._credentials_lock:
            generation = self._credential_generation
        try:
            credential = self._request(
                "GET",
                f"{CLIENT_PATH}/credential/",
                {"key": key, "account_id": account_id},
                Credential.from_dict,
                query=True,
            )
        except PAMError as error:
            denied = (
                error.status_code in (401, 403, 404)
                or error.code == "client_upgrade_required"
                or (error.status_code == 400 and error.code == "credential_not_found")
            )
            temporary = error.code == "NetworkError" or (
                error.status_code is not None and 500 <= error.status_code < 600
            )
            with self._credentials_lock:
                if denied:
                    self._latest_credentials.clear()
                    self._credential_generation += 1
                latest = self._latest_credentials.get(selector)
                if (
                    allow_local_fallback
                    and temporary
                    and not denied
                    and not self._closed
                    and latest
                ):
                    return replace(latest, from_local=True)
            raise
        with self._credentials_lock:
            if not self._closed and generation == self._credential_generation:
                latest = self._latest_credentials.get(selector)
                if latest is not None and credential.revision < latest.revision:
                    raise PAMError(
                        "ResponseError",
                        "Credential revision moved backwards",
                        status_code=200,
                    )
                self._latest_credentials[selector] = credential
        return credential

    def _reconcile_latest_credentials(self, event):
        name = event.get("event")
        if name not in {"snapshot", "credential.revoked", "configuration.updated"}:
            return
        with self._credentials_lock:
            self._credential_generation += 1
            if name == "configuration.updated":
                # Push snapshots cannot prove the full pull authorization scope.
                self._latest_credentials = {selector: value for selector, value in self._latest_credentials.items() if selector[0] != "account_id"}
                return
            if name != "snapshot":
                key = event.get("credential_key") or event.get("key")
                account_id = event.get("account_id")
                key = key if isinstance(key, str) and key else None
                account_id = (
                    account_id if isinstance(account_id, str) and account_id else None
                )
                if key is None and account_id is None:
                    self._latest_credentials.clear()
                else:
                    self._latest_credentials = {
                        selector: credential
                        for selector, credential in self._latest_credentials.items()
                        if not (
                            key is not None
                            and (
                                selector == ("key", key)
                                or credential.key == key
                                or credential.key.startswith(key + ":")
                            )
                            or account_id is not None
                            and (key is None or key.startswith("account:"))
                            and (
                                selector == ("account_id", account_id)
                                or credential.account.id == account_id
                            )
                        )
                    }
                return
            updates = event.get("credentials", [])
            if not isinstance(updates, list) or any(
                not isinstance(item, dict) for item in updates
            ):
                self._latest_credentials.clear()
                return
            keys = {
                value
                for item in updates
                if isinstance(
                    value := item.get("key") or item.get("credential_key"), str
                )
            }
            self._latest_credentials = {
                selector: credential
                for selector, credential in self._latest_credentials.items()
                if selector[0] != "key" or selector[1] in keys
            }

    def get_application(self) -> ApplicationInfo:
        """Read metadata for the authenticated application, without its secret."""
        return self._request("GET", f"{CLIENT_PATH}/application/", {}, ApplicationInfo.from_dict, query=True)

    def get_client_info(self) -> ClientInfo:
        """Read registration and version metadata for this replica only."""
        return self._request("GET", f"{CLIENT_PATH}/client/", {}, ClientInfo.from_dict, query=True)

    def report_status(
        self, status: str, *, account_id: Optional[str] = None,
        account_ids: Optional[Iterable[str]] = None, error_code: str = "",
        raise_on_error: bool = False,
    ) -> bool:
        """Report a complete usage snapshot; transport failures return False by default.

        This does not confirm a rotation. Invalid arguments still raise ValueError.
        Use an empty account list when no accounts are in use. Never pass secrets
        or exception messages as error_code. Calls use the configured HTTP timeout.
        """
        if status not in ("starting", "running", "degraded", "stopping", "stopped", "error"):
            raise ValueError("Invalid application status")
        if type(raise_on_error) is not bool:
            raise ValueError("raise_on_error must be a boolean")
        if account_id is not None and account_ids is not None:
            raise ValueError("Use account_id or account_ids, not both")
        if isinstance(account_ids, (str, bytes)):
            raise ValueError("account_ids must be a sequence of account IDs")
        accounts = [account_id] if account_id is not None else list(account_ids or ())
        if len(accounts) > 200 or any(not isinstance(value, str) or not value for value in accounts):
            raise ValueError("Use at most 200 non-empty account IDs")
        if len(set(accounts)) != len(accounts):
            raise ValueError("Duplicate account IDs")
        if not isinstance(error_code, str) or len(error_code) > 128 or re.fullmatch(r"[A-Za-z0-9_.:-]*", error_code) is None:
            raise ValueError("error_code must be a machine-readable code of at most 128 characters")

        def parse(data):
            if type(data.get("accepted")) is not bool:
                raise ValueError("Invalid status acknowledgment")
            return data["accepted"]

        try:
            return self._request(
                "POST", f"{CLIENT_PATH}/status/",
                {"status": status, "account_ids": accounts, "error_code": error_code},
                parse, invalidate_credentials=False,
            )
        except (PAMError, RuntimeError):
            if raise_on_error:
                raise
            return False

    def list_policies(self, *, search: str = "") -> list[CredentialPolicy]:
        """Read all visible bound policies, including disabled ones, without secrets."""
        if not isinstance(search, str) or len(search) > 128:
            raise ValueError("search must be a string of at most 128 characters")

        def parse(data):
            if not isinstance(data.get("policies"), list):
                raise TypeError("policies must be a list")
            policies = [CredentialPolicy.from_dict(item) for item in data["policies"]]
            if len({item.id for item in policies}) != len(policies):
                raise ValueError("Duplicate policy in list")
            return policies

        return self._request("GET", f"{CLIENT_PATH}/policies/", {"search": search}, parse, query=True)

    def get_policy(
        self, *, key: Optional[str] = None, policy_id: Optional[str] = None,
    ) -> CredentialPolicy:
        """Read one visible bound policy by exactly one of key or policy_id."""
        if (key is None) == (policy_id is None):
            raise ValueError("Provide exactly one of key or policy_id")
        name, value = ("key", key) if key is not None else ("policy_id", policy_id)
        if not isinstance(value, str) or not value or (name == "key" and len(value) > 64):
            raise ValueError(f"Invalid {name}")
        return self._request("GET", f"{CLIENT_PATH}/policy/", {name: value}, CredentialPolicy.from_dict, query=True)

    def list_accounts(self, *, search: str = "") -> list[AuthorizedAccount]:
        """Fetch all authorized metadata pages live, without retrieving secrets.

        A changed count, duplicate or incomplete page raises PAMError. Retry the
        entire listing if the application's scope changes during pagination.
        """
        accounts = []
        seen = set()
        count = None
        while True:
            page = self.list_accounts_page(offset=len(accounts), search=search)
            if count is None:
                count = page.count
            if page.count != count:
                raise PAMError("ResponseError", "Account count changed during pagination; retry listing")
            if not page.accounts and len(accounts) < count:
                raise PAMError("ResponseError", "Incomplete account list")
            for account in page.accounts:
                if account.id in seen:
                    raise PAMError("ResponseError", "Duplicate account in paginated list")
                seen.add(account.id)
                accounts.append(account)
            if len(accounts) == count:
                return accounts

    def list_accounts_page(
        self, *, limit: int = 200, offset: int = 0, search: str = "",
    ) -> AccountsPage:
        """Return one live page of authorized account metadata and its total count."""
        if type(limit) is not int or not 1 <= limit <= 500:
            raise ValueError("limit must be an integer between 1 and 500")
        if type(offset) is not int or offset < 0:
            raise ValueError("offset must be a non-negative integer")
        if not isinstance(search, str) or len(search) > 128:
            raise ValueError("search must be a string of at most 128 characters")

        def parse(data):
            page = AccountsPage.from_dict(data)
            if len(page.accounts) > min(limit, max(page.count - offset, 0)):
                raise ValueError("Account page exceeds limit or total count")
            return page

        return self._request(
            "GET", f"{CLIENT_PATH}/accounts/",
            {"limit": limit, "offset": offset, "search": search}, parse, query=True,
        )

    def get_account(self, *, account_id: str, allow_local_fallback: bool = True) -> Account:
        """Fetch an authorized account, including asset and cache provenance."""
        value = self.get_credential(account_id=account_id, allow_local_fallback=allow_local_fallback)
        return replace(value.account, asset=value.asset, from_local=value.from_local, revision=value.account.revision or 0)

    def confirm_event(self, *, event_id: str, status: str = "success", error_code: str = "") -> CommandResult:
        """Report only after applying the event; receiving or fetching is insufficient."""
        if not isinstance(event_id, str) or not event_id or status not in ("success", "failed"):
            raise ValueError("event_id and a success or failed status are required")
        return self._request(
            "POST", f"{CLIENT_PATH}/event-result/",
            {"event_id": event_id, "status": status, "error_code": error_code},
            CommandResult.from_dict,
        )

    def confirm_credential(
        self,
        *,
        key: str,
        revision: int,
        account_id: str,
    ) -> CredentialConfirmation:
        """Confirm only after the application has applied this account revision."""
        if (
            not all(isinstance(value, str) and value for value in (key, account_id))
            or type(revision) is not int
            or revision < 1
        ):
            raise ValueError(
                "key, account_id and a positive integer revision are required"
            )
        return self._request(
            "POST",
            f"{CLIENT_PATH}/confirm/",
            {"key": key, "revision": revision, "account_id": account_id},
            CredentialConfirmation.from_dict,
        )

    def sync_agent(
        self,
        *,
        credentials: Iterable[KnownRevision],
        delivered_credentials: Iterable[KnownRevision],
        config_digest: str = "",
        sync_status: str = "",
        sync_error: str = "",
    ) -> AgentSync:
        """Reconcile retained and delivered revisions with the authorized scope."""
        return self._request(
            "POST",
            f"{CLIENT_PATH}/agent/sync/",
            {
                "credentials": [item.to_dict() for item in credentials],
                "delivered_credentials": [
                    item.to_dict() for item in delivered_credentials
                ],
                "config_digest": config_digest,
                "sync_status": sync_status,
                "sync_error": sync_error,
            },
            AgentSync.from_dict,
        )

    def list_application_commands(self) -> list[dict[str, Any]]:
        """Return outstanding commands for this replica."""

        def parse(data):
            commands = data.get("commands")
            if not isinstance(commands, list) or not all(
                isinstance(command, dict) for command in commands
            ):
                raise TypeError("commands must be a list of objects")
            return commands

        return self._request(
            "GET",
            f"{CLIENT_PATH}/commands/",
            {},
            parse,
            query=True,
        )

    def report_application_command_result(
        self,
        *,
        command_id: str,
        status: str,
        error_code: Optional[str] = None,
    ) -> CommandResult:
        """Claim a command with running, then report its actual outcome."""
        if not command_id or status not in ("running", "success", "failed"):
            raise ValueError("command_id and a valid command status are required")
        return self._request(
            "POST",
            f"{CLIENT_PATH}/command-result/",
            {"command_id": command_id, "status": status, "error_code": error_code},
            CommandResult.from_dict,
        )

    def execute_application_command(
        self,
        event: dict[str, Any],
        handler: Callable[[dict[str, Any]], Any],
    ) -> CommandResult:
        """Claim once, run an application handler, then report its actual result."""
        return execute_command(event, handler, self.report_application_command_result)

    def clone(self) -> "Client":
        """Construct a fresh client of the same type, without copying hook state.

        Subclasses with additional required constructor arguments must override
        this method. Cloning never starts an event listener.
        """
        return type(self)(
            self.endpoint,
            app_id=self._app_id,
            app_secret=self._app_secret,
            instance_id=self.instance_id,
            org_id=self.org_id,
            timeout=self.timeout,
            source=self.source,
        )

    def close(self) -> None:
        """Stop all active event streams and close the owned HTTP session."""
        with self._events_lock:
            with self._streams_lock:
                self._closed = True
                streams = tuple(self._streams)
            dispatcher = self._event_dispatcher
            if dispatcher is not None:
                dispatcher.request_stop()
        for stream in streams:
            stream.close()
        if dispatcher is not None:
            dispatcher.wait()
        self._transport.close()
        with self._credentials_lock:
            self._latest_credentials.clear()
            self._credential_generation += 1

    def __enter__(self) -> "Client":
        return self

    def __exit__(self, *_):
        self.close()

    def _start_event_dispatcher(self, stop_event, background):
        with self._events_lock:
            if self._closed:
                raise RuntimeError("Client is closed")
            if (
                self._event_dispatcher is not None
                and not self._event_dispatcher.finished.is_set()
            ):
                raise RuntimeError("An event listener is already running")
            dispatcher = EventDispatcher(self, stop_event)
            self._event_dispatcher = dispatcher
            dispatcher.start(background)
            return dispatcher

    def watch_events(self, stop_event: Optional[Event] = None) -> None:
        """Block while dispatching events to subclass hooks until stopped.

        The reader runs independently of the serial hooks. Initial and reconnect
        snapshots fetch current credentials; failed fetches or credential hooks
        retry with backoff. Confirmation remains the application's responsibility.
        """
        dispatcher = self._start_event_dispatcher(stop_event, background=False)
        dispatcher.run()

    def start_events(self, stop_event: Optional[Event] = None) -> None:
        """Start a background reader and serial hook worker, then return.

        Return does not mean the initial snapshot has been applied. Use an Event
        in your subclass if application startup must wait for its credentials.
        """
        self._start_event_dispatcher(stop_event, background=True)

    def stop_events(self) -> None:
        """Stop the hook listener and wait for active hooks; keep HTTP usable."""
        with self._events_lock:
            dispatcher = self._event_dispatcher
            if dispatcher is not None:
                dispatcher.request_stop()
        if dispatcher is not None:
            dispatcher.wait()

    def on_event(self, event: dict[str, Any]) -> None:
        """Observe each raw event before credential hooks, including snapshots.

        Override for snapshot cache cleanup, configuration changes, lifecycle
        events or application commands. This observer is not automatically retried.
        """

    def on_credential_changed(self, credential: Credential) -> None:
        """Apply a fetched credential; override with idempotent business logic.

        Called for subscription and rotation updates and every reconnect snapshot.
        Failures retry after fetching the current credential again. For rotation,
        confirm_credential must be called explicitly after a successful switch.
        """

    def on_credential_revoked(self, event: dict[str, Any]) -> None:
        """Override to stop using revoked credentials and release connections."""

    def on_event_error(self, error: Exception, event: Optional[dict[str, Any]]) -> None:
        """Handle hook/fetch failures; event is None for a fatal reader failure.

        Never log credential values or arbitrary exception messages.
        """
        self._log_event_error(error)

    @staticmethod
    def _log_event_error(error: Exception) -> None:
        logger.warning("Credential event handler failed: %s", type(error).__name__)

    def watch_credential_events(
        self,
        stop_event: Optional[Event] = None,
    ) -> Iterator[dict[str, Any]]:
        """Acknowledge received events before yielding; this is not an applied confirmation."""
        stream = EventStream(
            self._event_stream_url(), self._event_stream_headers, self.timeout
        )
        with self._streams_lock:
            if self._closed:
                raise RuntimeError("Client is closed")
            self._streams.add(stream)
        try:
            for event in stream.watch(stop_event):
                self._reconcile_latest_credentials(event)
                yield event
        finally:
            stream.close()
            with self._streams_lock:
                self._streams.discard(stream)

    def _event_stream_url(self):
        endpoint = urlsplit(self.endpoint)
        scheme = "wss" if endpoint.scheme == "https" else "ws"
        params = {}
        params["instance_id"] = self.instance_id
        query = urlencode(params)
        return urlunsplit(
            (
                scheme,
                endpoint.netloc,
                f"{endpoint.path.rstrip('/')}/ws/accounts/credential-events/",
                query,
                "",
            )
        )

    def _event_stream_headers(self):
        headers = {**self._headers(), "X-JMS-Event-Receipts": "1"}
        prepared = requests.Request(
            "GET", self._event_stream_url(), headers=headers
        ).prepare()
        self.auth(prepared)
        return [f"{name}: {value}" for name, value in prepared.headers.items()]
