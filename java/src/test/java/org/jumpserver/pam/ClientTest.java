package org.jumpserver.pam;

import static org.junit.jupiter.api.Assertions.*;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import org.jumpserver.pam.Models.*;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

class ClientTest {
  private static String endpoint;

  @BeforeAll
  static void setup() {
    endpoint = System.getenv("JMS_TEST_ENDPOINT");
    assumeTrue(endpoint != null, "Use clients/tests/run.py for protocol tests");
  }

  private Client client(String instance, String source) {
    return new Client(
        new Client.Options(endpoint, "contract-app", "contract-secret", instance)
            .orgId("contract-org")
            .source(source));
  }

  @Test
  void statusReportsAreBestEffortAndPreserveCredentials() {
    try (Client sdk = client("java-status", "jms-pam")) {
      assertTrue(sdk.reportStatus("running", "account"));
      assertEquals(List.of("account"), sdk.getClientInfo().getApplicationStatus().getAccountIds());
      assertNotNull(sdk.getClientInfo().getDateStatusReported());
      assertTrue(sdk.reportStatus("degraded", List.of("account", "backup")));
      sdk.getCredential("cache-503");
      for (String code : List.of("fault_401", "fault_403", "fault_404", "fault_503", "fault_invalid", "fault_network")) {
        ApplicationStatus report = new ApplicationStatus("running", List.of(), code);
        assertFalse(sdk.reportStatus(report));
        assertThrows(PAMException.class, () -> sdk.reportStatusStrict(report));
      }
      assertTrue(sdk.getCredential("cache-503").isFromLocal());
      assertTrue(sdk.reportStatus("stopped", List.of()));
      assertEquals(List.of(), sdk.getClientInfo().getApplicationStatus().getAccountIds());
      assertThrows(IllegalArgumentException.class, () -> sdk.reportStatus("unknown", List.of()));
      sdk.close();
      assertFalse(sdk.reportStatus("stopped", List.of()));
    }
    try (Client sdk = new Client(new Client.Options(endpoint, "contract-app", "contract-secret", "java-status-timeout")
        .orgId("contract-org").timeout(Duration.ofMillis(50)))) {
      assertFalse(sdk.reportStatus(new ApplicationStatus("running", List.of(), "fault_timeout")));
    }
  }

  @Test
  void accountProjectionAndEventResult() {
    try (Client sdk = client("java-account-event", "jms-pam")) {
      Account account = sdk.getAccount("account", false);
      assertEquals("app", account.getUsername());
      assertEquals("127.0.0.1", account.getAsset().getAddress());
      assertEquals(2, account.getRevision());
      assertFalse(account.isFromLocal());
      assertFalse(account.toString().contains("DO_NOT_LOG_SECRET"));
      assertTrue(sdk.confirmEvent("event-one").isAccepted());
      assertEquals(
          "failed", sdk.confirmEvent("event-two", "failed", "application_failed").getStatus());
    }
  }

  @Test
  void automaticSigningAndStructuredResponses() {
    try (Client sdk = client("java", "jms-pam")) {
      for (Credential item :
          List.of(sdk.getCredentialByAccountId("account"), sdk.getCredential("db/空 格?=&"))) {
        assertEquals("DO_NOT_LOG_SECRET", item.getAccount().getSecret());
        assertEquals("password", item.getAccount().getSecretType());
        assertEquals("postgresql", item.getAsset().getPlatform().getType());
        assertFalse(item.toString().contains(item.getAccount().getSecret()));
        assertFalse(item.getAccount().toString().contains(item.getAccount().getSecret()));
      }
      assertEquals(2, sdk.confirmCredential("db", 2, "account").getRevision());
      assertThrows(IllegalArgumentException.class, () -> sdk.getCredential(""));
      assertThrows(IllegalArgumentException.class, () -> sdk.confirmCredential("db", 0, "account"));
    }
  }

  @Test
  void selfApplicationClientAndBoundPolicies() throws Exception {
    try (Client sdk = client("java-metadata", "jms-pam")) {
      ApplicationInfo application = sdk.getApplication();
      assertEquals("contract-app", application.getId());
      assertEquals("contract-org", application.getOrgId());
      assertEquals(3, application.getAccountsCount());
      ClientInfo replica = sdk.getClientInfo();
      assertEquals("java-metadata", replica.getInstanceId());
      assertEquals("sdk", replica.getType());
      assertEquals(0L, replica.getConfigSchemaVersion());
      List<CredentialPolicy> policies = sdk.listPolicies();
      assertEquals(2, policies.size());
      assertFalse(policies.get(1).isActive());
      assertNull(policies.get(1).getActiveAccountId());
      assertEquals(List.of("account", "backup"), policies.get(0).getAccountIds());
      assertEquals(policies.get(0).getId(), sdk.getPolicy(policies.get(0).getKey()).getId());
      assertEquals(policies.get(0).getId(), sdk.getPolicyById(policies.get(0).getId()).getId());
      assertEquals(1, sdk.listPolicies("db/空 格?=&").size());
      assertTrue(sdk.listPolicies("no-match").isEmpty());
      assertFalse(
          new ObjectMapper()
              .writeValueAsString(List.of(application, replica, policies))
              .contains("DO_NOT_LOG_SECRET"));
    }
  }

  @Test
  void metadataValidationAndMissingPolicies() {
    try (Client sdk = client("java-invalid-metadata", "jms-pam")) {
      assertEquals(
          "ResponseError", assertThrows(PAMException.class, sdk::getApplication).getCode());
      assertEquals("ResponseError", assertThrows(PAMException.class, sdk::getClientInfo).getCode());
      assertEquals(
          "ResponseError",
          assertThrows(PAMException.class, () -> sdk.getPolicy("invalid")).getCode());
      for (String search : List.of("fault:invalid-list", "fault:duplicate", "fault:invalid-policy"))
        assertThrows(PAMException.class, () -> sdk.listPolicies(search));
      assertThrows(IllegalArgumentException.class, () -> sdk.getPolicy(""));
      assertThrows(IllegalArgumentException.class, () -> sdk.getPolicy("x".repeat(65)));
      assertThrows(IllegalArgumentException.class, () -> sdk.getPolicyById(""));
      assertThrows(IllegalArgumentException.class, () -> sdk.listPolicies("x".repeat(129)));
      assertEquals(
          404, assertThrows(PAMException.class, () -> sdk.getPolicy("missing")).getStatusCode());
    }
  }

  @Test
  void accountListingPagesSearchAndMetadata() throws Exception {
    try (Client sdk = client("java-accounts", "jms-pam")) {
      List<AuthorizedAccount> accounts = sdk.listAccounts();
      assertEquals(205, accounts.size());
      assertEquals(205, accounts.stream().map(AuthorizedAccount::getId).distinct().count());
      assertEquals("postgresql", accounts.get(0).getAsset().getPlatform().getType());
      assertEquals(
          List.of("account-0", "backup"), accounts.get(0).getCredentials().get(0).getAccountIds());
      assertEquals("subscription", accounts.get(1).getCredentials().get(0).getMode());
      assertTrue(accounts.get(2).getCredentials().isEmpty());
      assertEquals("", accounts.get(2).getUsername());
      assertFalse(accounts.toString().contains("DO_NOT_LOG_SECRET"));
      AccountsPage page = sdk.listAccountsPage(2, 1, "");
      assertEquals(205, page.getCount());
      assertEquals(2, page.getAccounts().size());
      assertEquals("account-1", page.getAccounts().get(0).getId());
      assertTrue(sdk.listAccountsPage(200, 205, "").getAccounts().isEmpty());
      assertEquals("account-0", sdk.listAccounts("db/空 格?=&").get(0).getId());
      assertEquals(1, sdk.listAccounts("db/空 格?=&").size());
      assertTrue(sdk.listAccounts("no-match").isEmpty());
      assertTrue(sdk.listAccounts("fault:empty").isEmpty());
      JsonNode stats =
          new ObjectMapper()
              .readTree(
                  HttpClient.newBuilder()
                      .version(HttpClient.Version.HTTP_1_1)
                      .build()
                      .send(
                          HttpRequest.newBuilder(
                                  URI.create(endpoint.replace("/prefix", "") + "/__stats"))
                              .GET()
                              .build(),
                          HttpResponse.BodyHandlers.ofString())
                      .body());
      for (JsonNode request : stats.get("requests")) {
        if (request.get("data").get("instance_id").asText().equals("java-accounts"))
          assertTrue(request.get("path").asText().endsWith("/accounts/"));
      }
    }
  }

  @Test
  void accountListingRejectsInvalidAndIncompletePages() {
    try (Client sdk = client("java-errors-accounts", "jms-pam")) {
      for (int[] page : new int[][] {{0, 0}, {501, 0}, {200, -1}})
        assertThrows(
            IllegalArgumentException.class, () -> sdk.listAccountsPage(page[0], page[1], ""));
      assertThrows(IllegalArgumentException.class, () -> sdk.listAccounts((String) null));
      assertThrows(IllegalArgumentException.class, () -> sdk.listAccounts("x".repeat(129)));
      for (String fault :
          List.of(
              "invalid-count",
              "null-count",
              "missing-policy-revision",
              "missing-username",
              "null-policies",
              "invalid-account",
              "invalid-policy",
              "invalid-list",
              "oversized",
              "duplicate-page",
              "incomplete",
              "duplicate",
              "changing-count",
              "forbidden")) {
        PAMException error =
            assertThrows(PAMException.class, () -> sdk.listAccounts("fault:" + fault));
        assertEquals(
            fault.equals("forbidden") ? "credential_not_authorized" : "ResponseError",
            error.getCode());
      }
    }
  }

  @Test
  void errorsAndUpgradeAreExplicit() {
    try (Client sdk = client("java-errors", "jms-pam")) {
      Map<String, String> cases =
          Map.of(
              "forbidden",
              "credential_not_authorized",
              "upgrade",
              "client_upgrade_required",
              "invalid-json",
              "ResponseError",
              "invalid-revision",
              "ResponseError",
              "redirect",
              "HTTPError");
      cases.forEach(
          (key, code) ->
              assertEquals(
                  code, assertThrows(PAMException.class, () -> sdk.getCredential(key)).getCode()));
    }
  }

  @Test
  void synchronizationValidatesMetadata() {
    try (Client sdk = client("java-agent", "jms-pam-agent")) {
      List<KnownRevision> known = List.of(new KnownRevision("db", 2));
      AgentSync sync = sdk.syncAgent(known, List.of(), "", "", "");
      assertFalse(sync.getCredentials().get(0).isChanged());
      assertEquals(List.of("db"), sync.getScope().get("credential_keys"));
      assertEquals(
          "ResponseError",
          assertThrows(
                  PAMException.class, () -> sdk.syncAgent(known, List.of(), "", "", "invalid-flag"))
              .getCode());
    }
  }

  @Test
  void commandsClaimOnceAndPreserveHandlerException() {
    try (Client sdk = client("java-command", "jms-pam")) {
      Event event = sdk.listApplicationCommands().get(0);
      int[] executed = {0};
      assertEquals(
          "success", sdk.executeApplicationCommand(event, command -> executed[0]++).getStatus());
      assertEquals(1, executed[0]);
      ObjectMapper mapper = new ObjectMapper();
      Event duplicate =
          new Event(
              mapper.valueToTree(
                  Map.of("event", "application.restart.requested", "command_id", "duplicate")));
      assertFalse(sdk.executeApplicationCommand(duplicate, command -> executed[0]++).isAccepted());
      assertEquals(1, executed[0]);
      RuntimeException failure = new RuntimeException("business failure");
      Event rejected =
          new Event(
              mapper.valueToTree(
                  Map.of(
                      "event", "application.restart.requested", "command_id", "report-failure")));
      assertSame(
          failure,
          assertThrows(
              RuntimeException.class,
              () ->
                  sdk.executeApplicationCommand(
                      rejected,
                      command -> {
                        throw failure;
                      })));
    }
  }

  @Test
  void reconnectReceiptsAndStreamClosure() throws Exception {
    List<Event> events = new ArrayList<>();
    try (Client sdk = client("java-events", "jms-pam")) {
      assertTimeoutPreemptively(
          Duration.ofSeconds(6),
          () -> {
            try (EventStream stream = sdk.watchCredentialEvents()) {
              for (Event event : stream) {
                events.add(event);
                if (events.size() == 4) break;
              }
            }
          });
      assertEquals(
          List.of("snapshot", "credential.updated", "future.notification", "snapshot"),
          events.stream().map(Event::getEvent).collect(java.util.stream.Collectors.toList()));
      assertEquals(3, events.get(3).getCredentials().get(0).getRevision());
      assertEquals(
          "preserved", events.get(2).getData().path("future_payload").path("notice").asText());
      String statsUrl = endpoint.substring(0, endpoint.length() - "/prefix".length()) + "/__stats";
      JsonNode stats =
          new ObjectMapper()
              .readTree(
                  HttpClient.newBuilder()
                      .version(HttpClient.Version.HTTP_1_1)
                      .build()
                      .send(
                          HttpRequest.newBuilder(URI.create(statsUrl)).build(),
                          HttpResponse.BodyHandlers.ofString())
                      .body());
      List<String> receipts = new ArrayList<>();
      for (JsonNode receipt : stats.get("receipts"))
        if (receipt.path("instance").asText().equals("java-events"))
          receipts.add(receipt.path("eventId").asText());
      assertTrue(receipts.contains("updated-1"));
      assertTrue(receipts.contains("future-1"));
      try (Client copy = sdk.clone();
          EventStream stream = sdk.watchCredentialEvents()) {
        sdk.close();
        assertFalse(stream.iterator().hasNext());
        assertEquals(2, copy.getCredential("db").getRevision());
      }
    }
  }
}
