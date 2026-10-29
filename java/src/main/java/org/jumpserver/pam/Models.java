package org.jumpserver.pam;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Immutable responses; unknown optional fields are tolerated. */
public final class Models {
  private Models() {}

  static JsonNode object(JsonNode value) {
    if (value == null || !value.isObject())
      throw new IllegalArgumentException("Expected an object");
    return value;
  }

  static String string(JsonNode value, String name) {
    JsonNode field = object(value).get(name);
    if (field == null || !field.isTextual() || field.asText().isEmpty())
      throw new IllegalArgumentException("Invalid " + name);
    return field.asText();
  }

  static long revision(JsonNode value) {
    JsonNode field = object(value).get("revision");
    if (field == null
        || !field.isIntegralNumber()
        || !field.canConvertToLong()
        || field.asLong() < 0) throw new IllegalArgumentException("Invalid revision");
    return field.asLong();
  }

  static boolean bool(JsonNode value, String name) {
    JsonNode field = object(value).get(name);
    if (field == null || !field.isBoolean()) throw new IllegalArgumentException("Invalid " + name);
    return field.asBoolean();
  }

  static long count(JsonNode value, String name) {
    JsonNode field = object(value).get(name);
    if (field == null
        || !field.isIntegralNumber()
        || !field.canConvertToLong()
        || field.asLong() < 0) throw new IllegalArgumentException("Invalid " + name);
    return field.asLong();
  }

  public static final class ApplicationInfo {
    private final String id, name, orgId;
    private final boolean isActive;
    private final long accountsCount, policiesCount;

    ApplicationInfo(JsonNode value) {
      id = string(value, "id");
      name = string(value, "name");
      orgId = string(value, "org_id");
      isActive = bool(value, "is_active");
      accountsCount = count(value, "accounts_count");
      policiesCount = count(value, "policies_count");
    }

    public String getId() {
      return id;
    }

    public String getName() {
      return name;
    }

    public String getOrgId() {
      return orgId;
    }

    public boolean isActive() {
      return isActive;
    }

    public long getAccountsCount() {
      return accountsCount;
    }

    public long getPoliciesCount() {
      return policiesCount;
    }
  }

  public static final class ApplicationStatus {
    private final String status, errorCode;
    private final List<String> accountIds;

    public ApplicationStatus(String status, List<String> accountIds, String errorCode) {
      if (!java.util.Arrays.asList("starting", "running", "degraded", "stopping", "stopped", "error").contains(status))
        throw new IllegalArgumentException("Invalid application status");
      if (accountIds == null || accountIds.size() > 200 || accountIds.stream().anyMatch(id -> id == null || id.isEmpty())
          || new HashSet<>(accountIds).size() != accountIds.size())
        throw new IllegalArgumentException("Use at most 200 unique non-empty account IDs");
      if (errorCode == null || errorCode.length() > 128 || !errorCode.matches("[A-Za-z0-9_.:-]*"))
        throw new IllegalArgumentException("Invalid machine-readable error code");
      this.status = status;
      this.accountIds = Collections.unmodifiableList(new ArrayList<>(accountIds));
      this.errorCode = errorCode;
    }

    private static List<String> accounts(JsonNode value) {
      JsonNode ids = object(value).get("account_ids");
      if (ids == null || !ids.isArray()) throw new IllegalArgumentException("Invalid account IDs");
      List<String> accounts = new ArrayList<>();
      for (JsonNode id : ids) {
        if (!id.isTextual()) throw new IllegalArgumentException("Invalid account ID");
        accounts.add(id.asText());
      }
      return accounts;
    }

    ApplicationStatus(JsonNode value) {
      this(string(value, "status"), accounts(value), object(value).path("error_code").isTextual()
          ? value.get("error_code").asText() : null);
    }

    public String getStatus() { return status; }
    public List<String> getAccountIds() { return accountIds; }
    public String getErrorCode() { return errorCode; }
  }

  public static final class ClientInfo {
    private final String id, instanceId, type, clientVersion;
    private final boolean isActive;
    private final long protocolVersion;
    private final Long configSchemaVersion;
    private final ApplicationStatus applicationStatus;
    private final String dateStatusReported;
    private final List<String> serverCapabilities;

    ClientInfo(JsonNode value) {
      id = string(value, "id");
      instanceId = string(value, "instance_id");
      type = string(value, "type");
      clientVersion = string(value, "client_version");
      if (!type.equals("sdk") && !type.equals("agent"))
        throw new IllegalArgumentException("Invalid client type");
      isActive = bool(value, "is_active");
      protocolVersion = count(value, "protocol_version");
      configSchemaVersion =
          value.hasNonNull("config_schema_version") ? count(value, "config_schema_version") : null;
      applicationStatus = value.hasNonNull("application_status") ? new ApplicationStatus(value.get("application_status")) : null;
      dateStatusReported = value.hasNonNull("date_status_reported") ? string(value, "date_status_reported") : null;
      if (value.hasNonNull("server_capabilities")) {
        JsonNode capabilities = value.get("server_capabilities");
        if (!capabilities.isArray()) throw new IllegalArgumentException("Invalid server capabilities");
        List<String> names = new ArrayList<>();
        for (JsonNode capability : capabilities) {
          if (!capability.isTextual() || capability.asText().isEmpty() || names.contains(capability.asText()))
            throw new IllegalArgumentException("Invalid server capability");
          names.add(capability.asText());
        }
        serverCapabilities = Collections.unmodifiableList(names);
      } else {
        serverCapabilities = null;
      }
    }

    public ApplicationStatus getApplicationStatus() { return applicationStatus; }
    public String getDateStatusReported() { return dateStatusReported; }
    public List<String> getServerCapabilities() { return serverCapabilities; }

    public String getId() {
      return id;
    }

    public String getInstanceId() {
      return instanceId;
    }

    public String getType() {
      return type;
    }

    public String getClientVersion() {
      return clientVersion;
    }

    public boolean isActive() {
      return isActive;
    }

    public long getProtocolVersion() {
      return protocolVersion;
    }

    public Long getConfigSchemaVersion() {
      return configSchemaVersion;
    }
  }

  public static final class CredentialPolicy {
    private final String id, name, status, activeAccountId;
    private final AccountPolicy policy;
    private final boolean isActive, subscriptionAllAuthorized;
    private final long accountsCount;

    CredentialPolicy(JsonNode value) {
      id = string(value, "id");
      name = string(value, "name");
      status = string(value, "status");
      policy = new AccountPolicy(value);
      isActive = bool(value, "is_active");
      subscriptionAllAuthorized = bool(value, "subscription_all_authorized");
      accountsCount = count(value, "accounts_count");
      activeAccountId =
          value.hasNonNull("active_account_id") ? string(value, "active_account_id") : null;
    }

    public String getId() {
      return id;
    }

    public String getKey() {
      return policy.getKey();
    }

    public String getName() {
      return name;
    }

    public String getMode() {
      return policy.getMode();
    }

    public String getStatus() {
      return status;
    }

    public boolean isActive() {
      return isActive;
    }

    public long getRevision() {
      return policy.getRevision();
    }

    public long getAccountsCount() {
      return accountsCount;
    }

    public boolean isSubscriptionAllAuthorized() {
      return subscriptionAllAuthorized;
    }

    public String getActiveAccountId() {
      return activeAccountId;
    }

    public List<String> getAccountIds() {
      return policy.getAccountIds();
    }
  }

  public static final class Platform {
    private final String id, name, category, type;

    Platform(JsonNode value) {
      id = string(value, "id");
      name = string(value, "name");
      category = string(value, "category");
      type = string(value, "type");
    }

    public String getId() {
      return id;
    }

    public String getName() {
      return name;
    }

    public String getCategory() {
      return category;
    }

    public String getType() {
      return type;
    }
  }

  public static final class Asset {
    private final String id, name, address;
    private final Platform platform;

    Asset(JsonNode value) {
      id = string(value, "id");
      name = string(value, "name");
      address = string(value, "address");
      platform = new Platform(value.get("platform"));
    }

    public String getId() {
      return id;
    }

    public String getName() {
      return name;
    }

    public String getAddress() {
      return address;
    }

    public Platform getPlatform() {
      return platform;
    }
  }

  public static final class AccountPolicy {
    private final String key, mode;
    private final long revision;
    private final List<String> accountIds;

    AccountPolicy(JsonNode value) {
      key = string(value, "key");
      mode = string(value, "mode");
      if (!mode.equals("subscription") && !mode.equals("alternating_rotation"))
        throw new IllegalArgumentException("Invalid account policy mode");
      revision = revision(value);
      JsonNode accountSwitch = value.get("account_switch");
      if (accountSwitch == null || accountSwitch.isNull()) {
        accountIds = Collections.emptyList();
      } else {
        JsonNode ids = object(accountSwitch).get("account_ids");
        if (ids == null || !ids.isArray())
          throw new IllegalArgumentException("Invalid account switch");
        List<String> parsed = new ArrayList<>();
        for (JsonNode id : ids) {
          if (!id.isTextual() || id.asText().isEmpty())
            throw new IllegalArgumentException("Invalid account switch ID");
          parsed.add(id.asText());
        }
        accountIds = Collections.unmodifiableList(parsed);
      }
    }

    public String getKey() {
      return key;
    }

    public String getMode() {
      return mode;
    }

    public long getRevision() {
      return revision;
    }

    public List<String> getAccountIds() {
      return accountIds;
    }
  }

  /** Metadata in the application's pull scope, without a secret. */
  public static final class AuthorizedAccount {
    private final String id, name, username, secretType;
    private final Asset asset;
    private final List<AccountPolicy> credentials;

    AuthorizedAccount(JsonNode value) {
      id = string(value, "id");
      name = string(value, "name");
      JsonNode user = value.get("username");
      if (user == null || !user.isTextual()) throw new IllegalArgumentException("Invalid username");
      username = user.asText();
      secretType = string(value, "secret_type");
      asset = new Asset(value.get("asset"));
      JsonNode policies = value.get("credentials");
      if (policies == null || !policies.isArray())
        throw new IllegalArgumentException("Invalid account policies");
      List<AccountPolicy> parsed = new ArrayList<>();
      for (JsonNode policy : policies) parsed.add(new AccountPolicy(policy));
      credentials = Collections.unmodifiableList(parsed);
    }

    public String getId() {
      return id;
    }

    public String getName() {
      return name;
    }

    public String getUsername() {
      return username;
    }

    public String getSecretType() {
      return secretType;
    }

    public Asset getAsset() {
      return asset;
    }

    public List<AccountPolicy> getCredentials() {
      return credentials;
    }
  }

  public static final class AccountsPage {
    private final int count;
    private final List<AuthorizedAccount> accounts;

    AccountsPage(JsonNode value) {
      JsonNode total = object(value).get("count");
      if (total == null
          || !total.isIntegralNumber()
          || !total.canConvertToInt()
          || total.asInt() < 0) throw new IllegalArgumentException("Invalid account count");
      count = total.asInt();
      JsonNode items = value.get("accounts");
      if (items == null || !items.isArray())
        throw new IllegalArgumentException("Invalid account list");
      Set<String> seen = new HashSet<>();
      List<AuthorizedAccount> parsed = new ArrayList<>();
      for (JsonNode item : items) {
        AuthorizedAccount account = new AuthorizedAccount(item);
        if (!seen.add(account.getId()))
          throw new IllegalArgumentException("Duplicate account in page");
        parsed.add(account);
      }
      accounts = Collections.unmodifiableList(parsed);
    }

    public int getCount() {
      return count;
    }

    public List<AuthorizedAccount> getAccounts() {
      return accounts;
    }
  }

  public static final class Account {
    private final String id, name, username, secretType, secret;
    private Asset asset;
    private boolean fromLocal;
    private long revision;

    Account(JsonNode value) {
      id = string(value, "id");
      name = string(value, "name");
      username = string(value, "username");
      secretType = string(value, "secret_type");
      secret = string(value, "secret");
      revision = value.has("revision") ? revision(value) : 0;
    }

    public String getId() {
      return id;
    }

    public String getName() {
      return name;
    }

    public String getUsername() {
      return username;
    }

    public String getSecretType() {
      return secretType;
    }

    Account(Account value, Asset asset, boolean fromLocal) {
      id=value.id; name=value.name; username=value.username; secretType=value.secretType;
      secret=value.secret; revision=value.revision; this.asset=asset; this.fromLocal=fromLocal;
    }
    public Asset getAsset() { return asset; }
    public boolean isFromLocal() { return fromLocal; }
    public long getRevision() { return revision; }

    public String getSecret() {
      return secret;
    }

    @Override
    public String toString() {
      return "Account{id=" + id + ", secret=[REDACTED]}";
    }
  }

  public static final class Credential {
    private final String key;
    private final long revision;
    private final Asset asset;
    private final Account account;
    private final boolean fromLocal;

    Credential(JsonNode value) {
      key = string(value, "key");
      revision = revision(value);
      asset = new Asset(value.get("asset"));
      account = new Account(value.get("account"));
      fromLocal = false;
    }

    private Credential(Credential value) {
      key = value.key;
      revision = value.revision;
      asset = value.asset;
      account = value.account;
      fromLocal = true;
    }

    Credential localCopy() {
      return new Credential(this);
    }

    public boolean isFromLocal() {
      return fromLocal;
    }

    public String getKey() {
      return key;
    }

    public long getRevision() {
      return revision;
    }

    public Asset getAsset() {
      return asset;
    }

    public Account getAccount() {
      return account;
    }

    @Override
    public String toString() {
      return "Credential{key=" + key + ", revision=" + revision + "}";
    }
  }

  public static final class CredentialConfirmation {
    private final String key;
    private final long revision;

    CredentialConfirmation(JsonNode value) {
      key = string(value, "key");
      revision = revision(value);
    }

    public String getKey() {
      return key;
    }

    public long getRevision() {
      return revision;
    }
  }

  public static final class KnownRevision {
    private final String key;
    private final long revision;

    public KnownRevision(String key, long revision) {
      if (key == null || key.isEmpty() || revision < 0)
        throw new IllegalArgumentException("Invalid known revision");
      this.key = key;
      this.revision = revision;
    }

    public String getKey() {
      return key;
    }

    public long getRevision() {
      return revision;
    }
  }

  public static final class CredentialRevision {
    private final String key;
    private final long revision;
    private final boolean available, changed;

    CredentialRevision(JsonNode value) {
      key = string(value, "key");
      revision = revision(value);
      available = bool(value, "available");
      changed = bool(value, "changed");
    }

    public String getKey() {
      return key;
    }

    public long getRevision() {
      return revision;
    }

    public boolean isAvailable() {
      return available;
    }

    public boolean isChanged() {
      return changed;
    }
  }

  public static final class AgentSync {
    private final String configDigest, dateLastSynced;
    private final List<CredentialRevision> credentials;
    private final List<String> removedKeys;
    private final Map<String, Object> scope;

    AgentSync(JsonNode value, ObjectMapper mapper) {
      configDigest = string(value, "config_digest");
      dateLastSynced = string(value, "date_last_synced");
      if (!value.path("credentials").isArray() || !value.path("removed_keys").isArray())
        throw new IllegalArgumentException("Invalid sync metadata");
      List<CredentialRevision> revisions = new ArrayList<>();
      Set<String> keys = new HashSet<>();
      for (JsonNode item : value.get("credentials")) {
        CredentialRevision entry = new CredentialRevision(item);
        if (!keys.add(entry.key)) throw new IllegalArgumentException("Duplicate revision keys");
        revisions.add(entry);
      }
      credentials = Collections.unmodifiableList(revisions);
      List<String> removed = new ArrayList<>();
      for (JsonNode key : value.get("removed_keys")) {
        if (!key.isTextual() || key.asText().isEmpty())
          throw new IllegalArgumentException("Invalid removed key");
        removed.add(key.asText());
      }
      removedKeys = Collections.unmodifiableList(removed);
      JsonNode config = value.get("scope");
      scope =
          config == null || config.isNull()
              ? null
              : Collections.unmodifiableMap(
                  mapper.convertValue(object(config), new TypeReference<Map<String, Object>>() {}));
    }

    public String getConfigDigest() {
      return configDigest;
    }

    public String getDateLastSynced() {
      return dateLastSynced;
    }

    public List<CredentialRevision> getCredentials() {
      return credentials;
    }

    public List<String> getRemovedKeys() {
      return removedKeys;
    }

    public Map<String, Object> getScope() {
      return scope;
    }
  }

  public static final class CommandResult {
    private final boolean accepted;
    private final String status;

    CommandResult(JsonNode value) {
      accepted = bool(value, "accepted");
      status = string(value, "status");
    }

    public boolean isAccepted() {
      return accepted;
    }

    public String getStatus() {
      return status;
    }
  }

  public static final class Event {
    private final JsonNode data;

    Event(JsonNode data) {
      this.data = object(data).deepCopy();
    }

    public String getEvent() {
      String value = data.path("event").asText("");
      switch (value) {
        case "rotation.verification.skipped": return "rotation.verify.skipped";
        case "rotation.verification.started": return "rotation.verify.started";
        case "rotation.verification.failed": return "rotation.verify.failed";
        case "rotation.verification.cancelled": return "rotation.verify.cancelled";
        case "application.scope.updated": return "app.scope.updated";
        case "application.restart.requested": return "app.restart.requested";
        case "rotation.waiting_for_application": return "rotation.waiting_for_app";
        case "configuration.updated": return "app.scope.updated";
        default: return value;
      }
    }

    public String getEventId() {
      return data.path("event_id").asText("");
    }

    public long getAccountRevision() { return data.path("account_revision").asLong(-1); }

    public String getCommandId() {
      return data.path("command_id").asText("");
    }

    public String getKey() {
      return data.path("credential_key").asText(data.path("key").asText(""));
    }

    public String getCredentialMode() {
      return data.path("credential_mode").asText("");
    }

    public String getAccountId() {
      return data.path("account_id").asText("");
    }

    public long getRevision() {
      return revision(data);
    }

    public List<String> getAuthorizedAccountIds() {
      List<String> ids = new ArrayList<>();
      JsonNode values = data.get("authorized_account_ids");
      if (values != null) {
        if (!values.isArray()) throw new IllegalArgumentException("Invalid authorization snapshot");
        for (JsonNode value : values) {
          if (!value.isTextual()) throw new IllegalArgumentException("Invalid account ID");
          ids.add(value.asText());
        }
      }
      return Collections.unmodifiableList(ids);
    }

    public List<Event> getCredentials() {
      List<Event> items = new ArrayList<>();
      JsonNode credentials = data.get("credentials");
      if (credentials != null) {
        if (!credentials.isArray()) throw new IllegalArgumentException("Invalid snapshot");
        for (JsonNode item : credentials) items.add(new Event(item));
      }
      return Collections.unmodifiableList(items);
    }

    public JsonNode getData() {
      return data.deepCopy();
    }
  }
}
