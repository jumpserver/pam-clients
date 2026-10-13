"""Typed responses using the same field names as the HTTP API."""

from dataclasses import asdict, dataclass, field
from typing import Any, Optional


def _required(data: dict[str, Any], *names: str) -> None:
    if not isinstance(data, dict):
        raise TypeError("Response body must be a JSON object")
    missing = [name for name in names if data.get(name) in (None, "")]
    if missing:
        raise ValueError(f"{', '.join(missing)} is required")


def _string(data, name):
    _required(data, name)
    value = data[name]
    if not isinstance(value, str):
        raise TypeError(f"{name} must be a string")
    return value


def _revision(data):
    _required(data, "revision")
    value = data["revision"]
    if type(value) is not int or value < 0:
        raise ValueError("revision must be a non-negative integer")
    return value


def _boolean(data, name):
    _required(data, name)
    value = data[name]
    if type(value) is not bool:
        raise TypeError(f"{name} must be a boolean")
    return value


def _count(data, name):
    _required(data, name)
    value = data[name]
    if type(value) is not int or value < 0:
        raise ValueError(f"{name} must be a non-negative integer")
    return value


@dataclass(frozen=True)
class ApplicationInfo:
    id: str
    name: str
    org_id: str
    is_active: bool
    accounts_count: int
    policies_count: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ApplicationInfo":
        return cls(
            _string(data, "id"), _string(data, "name"), _string(data, "org_id"),
            _boolean(data, "is_active"), _count(data, "accounts_count"), _count(data, "policies_count"),
        )


@dataclass(frozen=True)
class ApplicationStatus:
    status: str
    account_ids: list[str]
    error_code: str

    @classmethod
    def from_dict(cls, data):
        status = _string(data, "status")
        accounts = data.get("account_ids")
        error_code = data.get("error_code")
        if status not in ("starting", "running", "degraded", "stopping", "stopped", "error") or not isinstance(accounts, list):
            raise ValueError("Invalid application status")
        if any(not isinstance(value, str) or not value for value in accounts) or not isinstance(error_code, str):
            raise ValueError("Invalid application status fields")
        return cls(status, accounts, error_code)


@dataclass(frozen=True)
class ClientInfo:
    id: str
    instance_id: str
    type: str
    is_active: bool
    client_version: str
    protocol_version: int
    config_schema_version: Optional[int]
    application_status: Optional[ApplicationStatus] = None
    date_status_reported: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ClientInfo":
        client_type = _string(data, "type")
        if client_type not in ("sdk", "agent"):
            raise ValueError("Invalid client type")
        schema = _count(data, "config_schema_version") if data.get("config_schema_version") is not None else None
        return cls(
            _string(data, "id"), _string(data, "instance_id"), client_type,
            _boolean(data, "is_active"), _string(data, "client_version"),
            _count(data, "protocol_version"), schema,
            ApplicationStatus.from_dict(data["application_status"]) if data.get("application_status") else None,
            _string(data, "date_status_reported") if data.get("date_status_reported") is not None else None,
        )


@dataclass(frozen=True)
class CredentialPolicy:
    id: str
    key: str
    name: str
    mode: str
    is_active: bool
    status: str
    revision: int
    accounts_count: int
    subscription_all_authorized: bool
    active_account_id: Optional[str]
    account_switch: Optional[dict[str, list[str]]]

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "CredentialPolicy":
        policy = AccountPolicy.from_dict(data)
        active_id = data.get("active_account_id")
        if active_id is not None and (not isinstance(active_id, str) or not active_id):
            raise ValueError("Invalid active account ID")
        return cls(
            _string(data, "id"), policy.key, _string(data, "name"), policy.mode,
            _boolean(data, "is_active"), _string(data, "status"), policy.revision,
            _count(data, "accounts_count"), _boolean(data, "subscription_all_authorized"),
            active_id, policy.account_switch,
        )


@dataclass(frozen=True)
class Platform:
    id: str
    name: str
    category: str
    type: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Platform":
        return cls(
            id=_string(data, "id"),
            name=_string(data, "name"),
            category=_string(data, "category"),
            type=_string(data, "type"),
        )


@dataclass(frozen=True)
class Asset:
    id: str
    name: str
    address: str
    platform: Platform

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Asset":
        _required(data, "id", "name", "address", "platform")
        return cls(
            _string(data, "id"),
            _string(data, "name"),
            _string(data, "address"),
            Platform.from_dict(data["platform"]),
        )


@dataclass(frozen=True)
class AccountPolicy:
    key: str
    mode: str
    revision: int
    account_switch: Optional[dict[str, list[str]]] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AccountPolicy":
        mode = _string(data, "mode")
        if mode not in ("subscription", "alternating_rotation"):
            raise ValueError("Invalid account policy mode")
        switch = data.get("account_switch")
        if switch is not None:
            if (
                not isinstance(switch, dict)
                or not isinstance(switch.get("account_ids"), list)
                or not all(isinstance(item, str) and item for item in switch["account_ids"])
            ):
                raise ValueError("Invalid account switch")
            switch = {"account_ids": list(switch["account_ids"])}
        return cls(_string(data, "key"), mode, _revision(data), switch)


@dataclass(frozen=True)
class AuthorizedAccount:
    """Account metadata in the application's current pull scope, without a secret."""

    id: str
    name: str
    username: str
    secret_type: str
    asset: Asset
    credentials: list[AccountPolicy]

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AuthorizedAccount":
        _required(data, "asset", "credentials")
        if not isinstance(data.get("username"), str):
            raise TypeError("username must be a string")
        if not isinstance(data["credentials"], list):
            raise TypeError("credentials must be a list")
        return cls(
            _string(data, "id"),
            _string(data, "name"),
            data["username"],
            _string(data, "secret_type"),
            Asset.from_dict(data["asset"]),
            [AccountPolicy.from_dict(item) for item in data["credentials"]],
        )


@dataclass(frozen=True)
class AccountsPage:
    count: int
    accounts: list[AuthorizedAccount]

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AccountsPage":
        _required(data, "count", "accounts")
        if type(data["count"]) is not int or data["count"] < 0:
            raise ValueError("count must be a non-negative integer")
        if not isinstance(data["accounts"], list):
            raise TypeError("accounts must be a list")
        accounts = [AuthorizedAccount.from_dict(item) for item in data["accounts"]]
        if len({item.id for item in accounts}) != len(accounts):
            raise ValueError("Duplicate account in page")
        return cls(data["count"], accounts)


@dataclass(frozen=True)
class Account:
    id: str
    name: str
    username: str
    secret_type: str
    secret: str = field(repr=False)
    revision: Optional[int] = None
    asset: Optional[Asset] = None
    from_local: bool = field(default=False, compare=False)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Account":
        _required(data, "id", "name", "username", "secret_type", "secret")
        return cls(
            id=_string(data, "id"),
            name=_string(data, "name"),
            username=_string(data, "username"),
            secret_type=_string(data, "secret_type"),
            secret=_string(data, "secret"),
            revision=_revision(data) if "revision" in data else None,
        )


@dataclass(frozen=True)
class Credential:
    key: str
    revision: int
    asset: Asset
    account: Account
    from_local: bool = field(default=False, compare=False)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Credential":
        _required(data, "key", "revision", "asset", "account")
        return cls(
            _string(data, "key"),
            _revision(data),
            Asset.from_dict(data["asset"]),
            Account.from_dict(data["account"]),
        )

    def to_dict(self) -> dict[str, Any]:
        """Return delivery data, including the secret; never log this mapping."""
        data = asdict(self)
        data.pop("from_local")
        data["account"].pop("asset")
        data["account"].pop("from_local")
        if data["account"]["revision"] is None:
            data["account"].pop("revision")
        return data


@dataclass(frozen=True)
class CredentialConfirmation:
    key: str
    revision: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "CredentialConfirmation":
        _required(data, "key", "revision")
        return cls(_string(data, "key"), _revision(data))


@dataclass(frozen=True)
class KnownRevision:
    key: str
    revision: int

    def __post_init__(self):
        if (
            not isinstance(self.key, str)
            or not self.key
            or type(self.revision) is not int
            or self.revision < 0
        ):
            raise ValueError("A key and a non-negative integer revision are required")

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class CredentialRevision:
    key: str
    revision: int
    available: bool
    changed: bool

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "CredentialRevision":
        _required(data, "key", "revision", "available", "changed")
        return cls(
            key=_string(data, "key"),
            revision=_revision(data),
            available=_boolean(data, "available"),
            changed=_boolean(data, "changed"),
        )


@dataclass(frozen=True)
class AgentSync:
    config_digest: str
    credentials: list[CredentialRevision]
    removed_keys: list[str]
    date_last_synced: str
    scope: Optional[dict[str, Any]] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AgentSync":
        _required(
            data, "config_digest", "credentials", "removed_keys", "date_last_synced"
        )
        if not isinstance(data["credentials"], list) or not isinstance(
            data["removed_keys"], list
        ):
            raise TypeError("Credential revisions and removed keys must be lists")
        if not all(isinstance(key, str) and key for key in data["removed_keys"]):
            raise TypeError("Removed keys must be non-empty strings")
        scope = data.get("scope")
        if scope is not None and not isinstance(scope, dict):
            raise TypeError("Agent scope must be an object")
        credentials = [
            CredentialRevision.from_dict(item) for item in data["credentials"]
        ]
        if len({item.key for item in credentials}) != len(credentials):
            raise ValueError("Credential revision keys must be unique")
        return cls(
            _string(data, "config_digest"),
            credentials,
            list(data["removed_keys"]),
            _string(data, "date_last_synced"),
            scope,
        )

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class CommandResult:
    accepted: bool
    status: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "CommandResult":
        _required(data, "accepted", "status")
        return cls(_boolean(data, "accepted"), _string(data, "status"))
