"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const http = require("node:http");
const { WebSocketServer } = require("../node/node_modules/ws");

const signedHeaders = [
  "(request-target)",
  "accept",
  "date",
  "digest",
  "x-jms-request-id",
  "x-jms-org",
  "x-jms-client-version",
  "x-jms-protocol-version",
  "x-jms-config-schema-version",
];

async function startServer() {
  const requests = [];
  const failures = [];
  const receipts = [];
  const connections = new Map();
  const ids = new Set();
  const sockets = new Set();
  const credentialRequests = new Map();
  const controls = new Map();
  const instanceSockets = new Map();
  const applicationStatuses = new Map();
  function authenticate(request, body) {
    const headers = request.headers;
    const auth = Object.fromEntries(
      [...String(headers.authorization).matchAll(/(\w+)="([^"]*)"/g)].map((match) => [
        match[1],
        match[2],
      ]),
    );
    assert.equal(auth.keyId, "contract-app");
    assert.equal(auth.algorithm, "hmac-sha256");
    assert.equal(auth.headers, signedHeaders.join(" "));
    assert.equal(
      headers.digest,
      "SHA-256=" + crypto.createHash("sha256").update(body).digest("base64"),
    );
    assert.equal(headers["x-jms-org"], "contract-org");
    assert.equal(headers["x-jms-client-version"], "1.0.2");
    assert.equal(headers["x-jms-protocol-version"], "1");
    assert.equal(
      headers["x-jms-config-schema-version"],
      headers["x-source"] === "jms-pam-agent" ? "1" : "0",
    );
    assert.match(
      headers["x-jms-request-id"],
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    assert.ok(!ids.has(headers["x-jms-request-id"]), "request ID reused");
    ids.add(headers["x-jms-request-id"]);
    assert.ok(Math.abs(Date.now() - Date.parse(headers.date)) < 30000);
    const signing = signedHeaders
      .map(
        (name) =>
          `${name}: ${
            name === "(request-target)"
              ? request.method.toLowerCase() + " " + request.url
              : headers[name]
          }`,
      )
      .join("\n");
    assert.equal(
      auth.signature,
      crypto.createHmac("sha256", "contract-secret").update(signing).digest("base64"),
    );
  }
  const server = http.createServer(async (request, response) => {
    const url = new URL(request.url, "http://localhost");
    if (url.pathname === "/__control") {
      const instance = url.searchParams.get("instance");
      const control = controls.get(instance) || {};
      if (url.searchParams.has("revision")) control.revision = Number(url.searchParams.get("revision"));
      if (url.searchParams.has("fault")) control.fault = Number(url.searchParams.get("fault"));
      if (url.searchParams.has("result_failures")) control.resultFailures = Number(url.searchParams.get("result_failures"));
      if (url.searchParams.has("account_id")) control.accountId = url.searchParams.get("account_id");
      for (const field of ["delivery_root", "socket_path", "app_user", "delivery_mode"])
        if (url.searchParams.has(field)) control[field] = url.searchParams.get(field);
      controls.set(instance, control);
      const event = url.searchParams.get("event");
      const payload = url.searchParams.has("payload") ? JSON.parse(url.searchParams.get("payload")) : null;
      if (event || payload) for (const socket of instanceSockets.get(instance) || []) socket.send(JSON.stringify(payload || {
        event, event_id: "controlled-" + control.revision, credential_mode: "alternating_rotation",
        credential_key: "db", revision: control.revision, account_id: control.accountId || "account",
        account_revision: control.revision,
      }));
      response.end("{}");
      return;
    }
    if (url.pathname === "/__stats") {
      response.setHeader("Content-Type", "application/json");
      response.end(
        JSON.stringify({
          requests,
          receipts,
          failures,
          connections: Object.fromEntries(connections),
        }),
      );
      return;
    }
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const body = Buffer.concat(chunks);
    response.setHeader("Content-Type", "application/json");
    try {
      authenticate(request, body);
      const data =
        request.method === "GET" ? Object.fromEntries(url.searchParams) : JSON.parse(body);
      assert.ok(data.instance_id);
      requests.push({
        path: url.pathname,
        method: request.method,
        data,
        requestId: request.headers["x-jms-request-id"],
      });
      const route = url.pathname
        .replace(/^\/prefix/, "")
        .replace("/api/v1/accounts/credential-client", "");
      const agentControl = data.instance_id.startsWith("agent-native-") ? controls.get(data.instance_id) : undefined;
      if (agentControl?.fault) {
        response.statusCode = agentControl.fault;
        response.end(JSON.stringify({code: "unavailable"}));
        return;
      }
      let result;
      if (["/application/", "/client/", "/policies/", "/policy/"].includes(route)) {
        assert.equal(request.method, "GET");
        if (data.instance_id.includes("compat-missing")) {
          response.statusCode = 404;
          response.end(JSON.stringify({code: "not_found"}));
          return;
        }
        const policies = [{
          id: "11111111-1111-4111-8111-111111111111", key: "db/空 格?=&", name: "Database rotation",
          mode: "alternating_rotation", is_active: true, status: "waiting_switch", revision: 2,
          accounts_count: 2, subscription_all_authorized: false, active_account_id: "account",
          account_switch: {account_ids: ["account", "backup"]}, secret: "DO_NOT_LOG_SECRET",
        }, {
          id: "22222222-2222-4222-8222-222222222222", key: "subscription", name: "Database subscription",
          mode: "subscription", is_active: false, status: "idle", revision: 1,
          accounts_count: 1, subscription_all_authorized: false, active_account_id: null, account_switch: null,
        }];
        if (route === "/application/") {
          result = {id: "contract-app", name: "Contract application", org_id: "contract-org", is_active: true,
            accounts_count: 3, policies_count: 2, secret: "DO_NOT_LOG_SECRET", future_field: "ignored"};
          if (data.instance_id.includes("invalid")) result.accounts_count = "3";
        } else if (route === "/client/") {
          result = {id: "current-client", instance_id: data.instance_id,
            type: request.headers["x-source"] === "jms-pam-agent" ? "agent" : "sdk",
            is_active: true, client_version: "1.0.2", protocol_version: 1,
            config_schema_version: request.headers["x-source"] === "jms-pam-agent" ? 1 : 0,
            application_status: applicationStatuses.get(data.instance_id) || null,
            date_status_reported: applicationStatuses.has(data.instance_id) ? "2026-10-13T00:00:00Z" : null,
            secret: "DO_NOT_LOG_SECRET"};
          if (data.instance_id.includes("invalid")) result.is_active = "true";
          if (data.instance_id.includes("compat-capabilities")) {
            result.server_capabilities = ["account_lookup", "application_status", "future_optional_capability"];
            result.future_optional_field = {ignored: true};
          }
          if (data.instance_id.includes("compat-bad-capabilities")) result.server_capabilities = ["account_lookup", 1];
        } else if (route === "/policies/") {
          result = {policies: policies.filter(p => !data.search || data.search.startsWith("fault:") || p.key.includes(data.search) || p.name.includes(data.search))};
          if (data.search === "fault:invalid-list") result.policies = null;
          if (data.search === "fault:duplicate") result.policies.push(result.policies[0]);
          if (data.search === "fault:invalid-policy") result.policies[0].revision = -1;
        } else {
          assert.ok(Boolean(data.key) !== Boolean(data.policy_id));
          result = policies.find(p => data.key ? p.key === data.key : p.id === data.policy_id);
          if (data.key === "invalid") result = {...policies[0], accounts_count: "2"};
          if (!result) {
            response.statusCode = 404;
            result = {code: "not_found", detail: "Credential policy not found"};
          }
        }
      } else if (route === "/credential/") {
        assert.equal(Boolean(data.key) !== Boolean(data.account_id), true);
        const control = controls.get(data.instance_id);
        if (control?.fault) {
          response.statusCode = control.fault;
          response.end(JSON.stringify({ code: "unavailable" }));
          return;
        }
        const selector = `${data.instance_id}:${data.key || data.account_id}`;
        const attempts = (credentialRequests.get(selector) || 0) + 1;
        credentialRequests.set(selector, attempts);
        if (data.key?.startsWith("cache-") && attempts > 1) {
          const fault = data.key.slice(6);
          if (fault === "426-invalid") { response.statusCode = 426; response.end("invalid"); return; }
          if (fault === "426-timeout") {
            response.writeHead(426);
            response.flushHeaders();
            return;
          }
          if (fault === "timeout") {
            // Headers arrive, but the body never completes. Client timeout must cancel it.
            response.writeHead(200);
            response.flushHeaders();
            return;
          }
          if (fault === "invalid") { response.end("invalid"); return; }
          if (fault === "revoked") { response.statusCode = 400; response.end(JSON.stringify({ code: "credential_not_found" })); return; }
          response.statusCode = Number(fault);
          response.end(JSON.stringify({ code: fault === "426" ? "client_upgrade_required" : "unavailable" }));
          return;
        }
        if (data.key === "forbidden" || data.key === "upgrade") {
          response.statusCode = data.key === "forbidden" ? 403 : 426;
          result = {
            code:
              data.key === "forbidden" ? "credential_not_authorized" : "client_upgrade_required",
            detail: "Denied",
            request_id: "failure-id",
          };
        } else if (data.key === "invalid-json") {
          response.end("invalid");
          return;
        } else if (data.key === "redirect") {
          response.statusCode = 302;
          response.setHeader("Location", "/unexpected-redirect");
          result = { detail: "Redirect" };
        } else {
          result = {
            key: data.account_id ? `account:${data.account_id}` : data.key,
            revision: control?.revision ?? (data.key === "invalid-revision" ? true
              : data.instance_id.startsWith("hooks-") ? (connections.get(data.instance_id) || 1) + 1 : 2),
            asset: {
              id: "asset",
              name: "database",
              address: "127.0.0.1",
              platform: {
                id: "platform",
                name: "PostgreSQL",
                category: "database",
                type: "postgresql",
              },
            },
            account: {
              id: data.instance_id.startsWith("managed-") ? data.account_id || "account" : "account",
              name: "db-user",
              username: "app",
              secret_type: "password",
              revision: control?.revision ?? 2,
              secret: control?.revision ? "LATEST_SECRET_" + control.revision : "DO_NOT_LOG_SECRET",
            },
            future_optional_field: "ignored",
          };
        }
      } else if (route === "/accounts/") {
        const control = controls.get(data.instance_id);
        result = {count: 1, accounts: [{
          id: "account", name: "db-user", username: "app", secret_type: "password",
          asset: {id: "asset", name: "database", address: "127.0.0.1", platform: {id: "platform", name: "PostgreSQL", category: "database", type: "postgresql"}},
          credentials: [{key: "db", mode: "alternating_rotation", revision: control?.revision || 2}],
        }]};
        if (data.instance_id.endsWith("-accounts")) {
          assert.equal(request.method, "GET");
          const limit = Number(data.limit), offset = Number(data.offset);
          assert.ok(Number.isInteger(limit) && limit >= 1 && limit <= 500);
          assert.ok(Number.isInteger(offset) && offset >= 0);
          const base = result.accounts[0];
          const all = Array.from({length: 205}, (_, index) => ({
            ...base,
            id: "account-" + index,
            name: index === 0 ? "db/空 格?=&" : "db-user-" + index,
            username: index === 2 ? "" : base.username,
            credentials: index === 0 ? [{...base.credentials[0], account_switch: {account_ids: ["account-0", "backup"]}}]
              : index === 1 ? [{key: "account:account-1", mode: "subscription", revision: 3}] : [],
            future_optional_field: "ignored",
            // Verify that SDK metadata projections never expose unexpected secrets.
            secret: "DO_NOT_LOG_SECRET",
          }));
          const fault = data.search.startsWith("fault:") ? data.search.slice(6) : "";
          const filtered = fault ? all : all.filter(item => !data.search || item.name.includes(data.search)
            || item.username.includes(data.search) || item.asset.name.includes(data.search) || item.asset.address.includes(data.search));
          result = {count: filtered.length, accounts: filtered.slice(offset, offset + limit)};
          if (fault === "empty") result = {count: 0, accounts: []};
          if (fault === "invalid-count") result.count = -1;
          if (fault === "null-count") result.count = null;
          if (fault === "invalid-account") result.accounts[0].id = "";
          if (fault === "invalid-policy") result.accounts[0].credentials[0].mode = "unknown";
          if (fault === "missing-policy-revision") delete result.accounts[0].credentials[0].revision;
          if (fault === "missing-username") delete result.accounts[0].username;
          if (fault === "null-policies") result.accounts[0].credentials = null;
          if (fault === "invalid-list") result.accounts = null;
          if (fault === "oversized") result.accounts.push(all[0]);
          if (fault === "duplicate-page") result.accounts[1] = result.accounts[0];
          if (offset > 0) {
            if (fault === "incomplete") result.accounts = [];
            if (fault === "duplicate") result.accounts[0] = all[0];
            if (fault === "changing-count") result.count++;
            if (fault === "forbidden") {
              response.statusCode = 403;
              result = {code: "credential_not_authorized", detail: "Scope denied"};
            }
          }
        }
      } else if (route === "/status/") {
        assert.equal(request.method, "POST");
        assert.ok(["starting", "running", "degraded", "stopping", "stopped", "error"].includes(data.status));
        assert.ok(Array.isArray(data.account_ids));
        assert.ok(data.account_ids.length <= 200);
        assert.equal(new Set(data.account_ids).size, data.account_ids.length);
        assert.match(data.error_code, /^[A-Za-z0-9_.:-]*$/);
        if (["fault_401", "fault_403", "fault_404", "fault_503"].includes(data.error_code)) {
          response.statusCode = Number(data.error_code.slice(6));
          result = {code: "credential_not_authorized"};
        } else if (data.error_code === "fault_invalid") {
          result = {accepted: "true"};
        } else if (data.error_code === "fault_network") {
          response.destroy();
          return;
        } else if (data.error_code === "fault_timeout") {
          response.writeHead(200);
          response.write("{");
          setTimeout(() => response.end("}"), 200).unref();
          return;
        } else {
          applicationStatuses.set(data.instance_id, {
            status: data.status, account_ids: data.account_ids, error_code: data.error_code,
          });
          result = {accepted: true};
        }
      } else if (route === "/event-result/") {
        assert.ok(data.event_id);
        assert.ok(["success", "failed"].includes(data.status));
        assert.equal(data.key, undefined);
        assert.equal(data.revision, undefined);
        const control = controls.get(data.instance_id);
        if (control?.resultFailures > 0) {
          control.resultFailures--;
          response.statusCode = 503;
          result = {code: "unavailable"};
        } else result = {accepted: true, status: data.status};
      } else if (route === "/confirm/") {
        assert.equal(data.account_id, "account");
        assert.equal(data.revision, 2);
        result = { key: data.key, revision: data.revision };
      } else if (route === "/agent/sync/") {
        assert.ok(Array.isArray(data.credentials));
        assert.ok(Array.isArray(data.delivered_credentials));
        result = {
          config_digest: "digest",
          date_last_synced: "now",
          removed_keys: [],
          credentials: [{ key: "db", revision: 2, available: true, changed: false }],
          scope: { credential_keys: ["db"], confirmation_keys: ["db"] },
        };
        if (data.sync_error === "invalid-flag") result.credentials[0].available = "false";
        if (agentControl) {
          result.credentials[0].revision = agentControl.revision || 2;
          result.credentials[0].account_id = "account";
          result.credentials[0].account_revision = agentControl.revision || 2;
          result.credentials[0].event_id = "agent-event-" + (agentControl.revision || 2);
          result.scope = { credential_keys: ["db"], confirmation_keys: ["db"] };
        }
      } else if (route === "/commands/") {
        result = {
          commands: [
            {
              event: "app.restart.requested",
              command_id: "command",
              event_id: "command-event",
            },
          ],
        };
        if (agentControl) result.commands = [];
      } else if (route === "/command-result/") {
        assert.ok(["running", "success", "failed"].includes(data.status));
        if (data.command_id === "report-failure" && data.status === "failed") {
          response.statusCode = 503;
          result = { code: "temporarily_unavailable" };
        } else result = { accepted: data.command_id !== "duplicate", status: data.status };
      } else throw new Error("Unexpected route");
      response.end(JSON.stringify(result));
    } catch (error) {
      failures.push(error.message);
      response.statusCode = 401;
      response.end(
        JSON.stringify({ code: "contract_failure", detail: "Protocol assertion failed" }),
      );
    }
  });
  const ws = new WebSocketServer({ noServer: true });
  server.on("upgrade", (request, socket, head) => {
    try {
      authenticate(request, Buffer.alloc(0));
      assert.equal(request.headers["x-jms-event-receipts"], "1");
      assert.match(
        new URL(request.url, "http://localhost").pathname,
        /\/ws\/accounts\/credential-events\/$/,
      );
      ws.handleUpgrade(request, socket, head, (connection) =>
        ws.emit("connection", connection, request),
      );
    } catch (error) {
      failures.push(error.message);
      socket.destroy();
    }
  });
  ws.on("connection", (socket, request) => {
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
    const url = new URL(request.url, "http://localhost");
    const instance = url.searchParams.get("instance_id");
    if (!instanceSockets.has(instance)) instanceSockets.set(instance, new Set());
    instanceSockets.get(instance).add(socket);
    socket.on("close", () => instanceSockets.get(instance).delete(socket));
    const count = (connections.get(instance) || 0) + 1;
    connections.set(instance, count);
    // A controlled Agent reconnect observes the current revision; reconnecting
    // alone must not advertise a revision that the HTTP fixture cannot serve.
    const managed = instance.startsWith("managed-");
    const snapshotRevision = instance.startsWith("agent-native-") || managed
      ? controls.get(instance)?.revision || 2 : count + 1;
    socket.send(JSON.stringify({ event: "pong" }));
    socket.send(
      JSON.stringify({
        event: "snapshot",
        credentials: [{ key: "db", credential_mode: "alternating_rotation", revision: snapshotRevision,
          ...(managed ? {event_id: "managed-" + snapshotRevision, account_id: controls.get(instance)?.accountId || "account", account_revision: snapshotRevision} : {}) }],
      }),
    );
    if (instance.startsWith("retry-")) return;
    socket.send(
      JSON.stringify({
        event: "credential.updated",
        event_id: managed ? "managed-" + snapshotRevision : "updated-" + count,
        credential_key: "db",
        credential_mode: "alternating_rotation",
        account_id: "account",
        revision: 2,
        ...(managed ? {account_id: controls.get(instance)?.accountId || "account", revision: snapshotRevision, account_revision: snapshotRevision} : {}),
      }),
    );
    socket.send(
      JSON.stringify({
        event: "future.notification",
        event_id: "future-" + count,
        future_payload: { notice: "preserved" },
      }),
    );
    socket.on("message", (message) => {
      const event = JSON.parse(message);
      if (event.event === "received") receipts.push({ instance, eventId: event.event_id });
      else if (event.event === "ping") socket.send(JSON.stringify({ event: "pong" }));
      else failures.push("Unexpected client WS event");
    });
    if (count === 1) setTimeout(() => socket.close(), 150).unref();
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    endpoint: `http://127.0.0.1:${server.address().port}/prefix`,
    stats: () => ({ requests, receipts, failures, connections }),
    async close() {
      for (const socket of sockets) socket.terminate();
      ws.close();
      server.closeAllConnections();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

module.exports = { startServer };
if (require.main === module)
  startServer().then((server) => {
    console.log(server.endpoint);
    process.on("SIGTERM", () => server.close().then(() => process.exit(0)));
  });
