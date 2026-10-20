"""Run real PostgreSQL/Redis traffic and acknowledge only applied credentials."""

import argparse
import http.client
import json
import logging
import signal
import socket
import threading
import time
from collections import deque
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import quote

import psycopg2
import redis

from jms_pam import Client, PAMError

LOG = logging.getLogger(__name__)


def agent_request(path, method="GET", data=None, *, socket_path):
    connection = http.client.HTTPConnection("localhost", timeout=5)
    connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    connection.sock.settimeout(5)
    try:
        connection.sock.connect(socket_path)
        body = None if data is None else json.dumps(data)
        connection.request(method, path, body, {"Content-Type": "application/json"})
        response = connection.getresponse()
        value = json.load(response)
        if response.status != 200:
            raise PAMError(value.get("code", "AgentError"), "Agent request failed", status_code=response.status)
        return value
    finally:
        connection.close()


@dataclass(repr=False)
class Credentials:
    key: str
    revision: int
    account_id: str
    account_revision: int
    username: str
    secret: str = field(repr=False)
    event_id: str = ""

    @classmethod
    def sdk(cls, value):
        return cls(value.key, value.revision, value.account.id, value.account.revision,
                   value.account.username, value.account.secret)

    @classmethod
    def agent(cls, value):
        return cls(**{name: value[name] for name in (
            "key", "revision", "account_id", "account_revision", "username", "secret",
        )}, event_id=value.get("event_id", ""))

    def identity(self):
        # A same-version resend is idempotent. Credentials never appear in status/logs.
        return self.account_id, self.account_revision, self.revision, self.secret


class DatabaseConnection:
    def __init__(self, name, options, credential, instance_id):
        self.name, self.credential, self.instance_id = name, credential, instance_id
        self.connection = None
        try:
            if name == "postgresql":
                self.connection = psycopg2.connect(
                    host=options["host"], port=options["port"], dbname=options["database"],
                    user=credential.username, password=credential.secret, connect_timeout=3,
                    application_name=instance_id, options="-c statement_timeout=3000",
                )
                self.connection.autocommit = True
            else:
                self.connection = redis.Redis(
                    host=options["host"], port=options["port"], db=0,
                    username=credential.username, password=credential.secret,
                    socket_connect_timeout=3, socket_timeout=3, decode_responses=True,
                    client_name=instance_id, protocol=2,
                )
            self.probe()  # New connections perform real writes and reads before publication.
        except Exception:
            self.close()
            raise

    def probe(self):
        if self.name == "postgresql":
            with self.connection.cursor() as cursor:
                cursor.execute(
                    "INSERT INTO public.pam_demo_activity(instance_id, authenticated_user) "
                    "VALUES (%s, current_user) ON CONFLICT (instance_id) DO UPDATE "
                    "SET writes = pam_demo_activity.writes + 1, "
                    "authenticated_user = EXCLUDED.authenticated_user, updated_at = now() "
                    "RETURNING writes, authenticated_user",
                    (self.instance_id,),
                )
                row_id, user = cursor.fetchone()
                cursor.execute(
                    "SELECT authenticated_user FROM public.pam_demo_activity WHERE instance_id = %s", (self.instance_id,),
                )
                if cursor.fetchone() != (user,):
                    raise RuntimeError("PostgreSQL read-back failed")
        else:
            user = self.connection.execute_command("ACL", "WHOAMI")
            key = f"pam-demo:{self.instance_id}:ticks"
            row_id = self.connection.incr(key)
            if self.connection.get(key) != str(row_id):
                raise RuntimeError("Redis read-back failed")
        if user != self.credential.username:
            raise RuntimeError("Authenticated database identity differs from the credential")
        return {"authenticated_user": user, "last_write_id": row_id}

    def close(self):
        if self.connection is not None:
            self.connection.close()


class Application:
    def __init__(self, config):
        version = config.get("config_version", 1)
        if type(version) is not int or version != 1:
            raise ValueError("Unsupported demo configuration version")
        self.config = config
        self.instance_id = config["instance_id"]
        self.mode = config["mode"]
        if self.mode not in ("sdk", "agent"):
            raise ValueError("mode must be sdk or agent")
        self.stop = threading.Event()
        self.lock = threading.RLock()
        self.connections = {}
        self.services = {name: {"status": "starting", "successful_writes": 0, "switches": 0}
                         for name in ("postgresql", "redis")}
        self.history = deque(maxlen=30)
        self.events = deque(maxlen=40)
        self.pending = {}
        self.confirmed = deque(maxlen=100)
        self.client = None if self.mode == "agent" else Client(
            instance_id=self.instance_id, **config["pam"],
        )
        self.listener = None

    def fetch(self, name):
        key = self.config[name]["credential_key"]
        if self.client:
            return Credentials.sdk(self.client.get_credential(key=key, allow_local_fallback=False))
        value = agent_request("/v1/credentials/" + quote(key, safe=""),
                              socket_path=self.config["agent_socket"])
        return Credentials.agent(value)

    def revoke(self, name):
        with self.lock:
            old = self.connections.pop(name, None)
            if old:
                old.close()
            self.services[name]["status"] = "revoked"

    def on_event(self, event):
        kind = event.get("event")
        with self.lock:
            self.events.append({key: event[key] for key in (
                "event", "event_id", "operation_id", "credential_key", "account_id", "account_revision", "revision",
            ) if key in event})
        if kind == "snapshot":
            updates = event.get("credentials", [])
            keys = {item.get("credential_key") or item.get("key") for item in updates}
            for name in self.services:
                if self.config[name]["credential_key"] not in keys:
                    self.revoke(name)
        elif kind in ("credential.updated", "credential.switch.requested"):
            updates = [event]
        elif kind == "credential.revoked":
            for name in self.services:
                if event.get("credential_key") == self.config[name]["credential_key"]:
                    self.revoke(name)
            return
        else:
            return
        with self.lock:
            for update in updates:
                key = update.get("credential_key") or update.get("key")
                if update.get("event_id") and any(self.config[name]["credential_key"] == key for name in self.services):
                    self.pending[update["event_id"]] = dict(update)
                    while len(self.pending) > 128:
                        self.pending.pop(next(iter(self.pending)))

    def listen(self):
        while not self.stop.is_set():
            try:
                for event in self.client.watch_credential_events(stop_event=self.stop):
                    self.on_event(event)
            except Exception as error:
                LOG.warning("Credential event stream interrupted (%s)", type(error).__name__)
                self.stop.wait(2)

    def apply(self, name, credential):
        with self.lock:
            old = self.connections.get(name)
            if old is None or old.credential.identity() != credential.identity():
                candidate = DatabaseConnection(name, self.config[name], credential, self.instance_id)
                self.connections[name] = candidate
                if old:
                    old.close()  # Release old authenticated sessions before acknowledging.
                self.services[name]["switches"] += 1
                self.history.append({"service": name, "username": credential.username,
                                     "revision": credential.revision, "at": time.time()})
            current = self.connections[name]
            try:
                observed = current.probe()
            except Exception:
                self.connections.pop(name).close()
                raise  # The next iteration builds a fresh connection.
            self.services[name].update(
                status="connected", account_id=credential.account_id,
                account_revision=credential.account_revision, revision=credential.revision,
                **observed, last_success=time.time(), error_code="",
            )
            self.services[name]["successful_writes"] += 1

    def acknowledge(self, name, credential):
        if self.mode == "agent":
            events = [{"event_id": credential.event_id}] if credential.event_id else []
        else:
            with self.lock:
                events = [dict(event) for event in self.pending.values()
                          if (event.get("credential_key") or event.get("key")) == credential.key]
        for event in events:
            event_id = event["event_id"]
            if event_id in self.confirmed:
                continue
            if self.client:
                if event.get("account_id") != credential.account_id or event.get("account_revision") != credential.account_revision:
                    continue  # Never acknowledge a superseded account version.
                if event.get("revision") != credential.revision:
                    continue  # Returning to A does not acknowledge an older A cycle.
                if event.get("command_id"):
                    claim = self.client.report_application_command_result(
                        command_id=event["command_id"], status="running",
                    )
                    if not claim.accepted:
                        with self.lock:
                            self.pending.pop(event_id, None)
                        continue
                self.client.confirm_event(event_id=event_id)
            else:
                result = agent_request("/v1/confirm", "POST", {"event_id": event_id},
                                       socket_path=self.config["agent_socket"])
                if result["status"] != "confirmed":
                    continue
            self.confirmed.append(event_id)
            with self.lock:
                self.pending.pop(event_id, None)
            LOG.info("Applied %s revision %s using %s", name, credential.revision, credential.username)

    def status(self):
        with self.lock:
            services = {name: dict(value) for name, value in self.services.items()}
            ready = all(value["status"] == "connected" and time.time() - value["last_success"] < 10
                        for value in services.values())
            return {"status": "ready" if ready else "degraded", "mode": self.mode,
                    "instance_id": self.instance_id, "services": services,
                    "history": list(self.history), "recent_events": list(self.events),
                    "confirmed_events": len(self.confirmed)}

    def run(self):
        if self.client:
            self.listener = threading.Thread(target=self.listen, daemon=True)
            self.listener.start()
        while not self.stop.is_set():
            for name in self.services:
                try:
                    credential = self.fetch(name)
                    self.apply(name, credential)
                    self.acknowledge(name, credential)
                except Exception as error:
                    if isinstance(error, PAMError) and error.status_code in (401, 403, 404):
                        self.revoke(name)
                    with self.lock:
                        self.services[name].update(status="error", error_code=type(error).__name__)
                    LOG.warning("%s update/probe failed (%s)", name, type(error).__name__)
            if self.client:
                try:
                    state = self.status()
                    self.client.report_status(
                        status="running" if state["status"] == "ready" else "degraded",
                        account_ids=[s["account_id"] for s in state["services"].values()
                                     if s["status"] == "connected"],
                        error_code="" if state["status"] == "ready" else "database_probe_failed",
                    )
                except Exception as error:
                    LOG.warning("Application status report failed (%s)", type(error).__name__)
            self.stop.wait(self.config.get("interval", 2))

    def close(self):
        self.stop.set()
        if self.client:
            self.client.close()
        if self.listener:
            self.listener.join(timeout=8)
        with self.lock:
            for connection in self.connections.values():
                connection.close()


PAGE = """<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>PAM 数据库 Demo</title>
<style>body{font:16px system-ui;background:#f4f6f8;color:#182230;max-width:960px;margin:48px auto;padding:0 24px}h1{font-size:26px}article{background:white;border:1px solid #dce1e8;border-radius:12px;padding:24px;margin:16px 0}pre{white-space:pre-wrap;line-height:1.6}small{color:#687387}</style>
<h1>PAM · PostgreSQL & Redis</h1><small>每 2 秒真实读写；验证新连接并关闭旧连接后确认凭据事件。</small>
<article><h2 id="state">正在连接…</h2><pre id="data"></pre></article>
<script>async function refresh(){try{const r=await fetch('/api/status');const s=await r.json();document.getElementById('state').textContent=s.mode.toUpperCase()+' · '+s.status;document.getElementById('data').textContent=JSON.stringify(s,null,2)}catch(e){document.getElementById('state').textContent='应用不可达'}}refresh();setInterval(refresh,2000)</script></html>"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, type=Path)
    args = parser.parse_args()
    config = json.loads(args.config.read_text())
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    application = Application(config)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            if self.path in ("/health", "/api/status"):
                state = application.status()
                body = json.dumps(state).encode()
                code = 503 if self.path == "/health" and state["status"] != "ready" else 200
                content_type = "application/json"
            elif self.path == "/":
                body, code, content_type = PAGE.encode(), 200, "text/html; charset=utf-8"
            else:
                body, code, content_type = b"Not found", 404, "text/plain"
            self.send_response(code)
            self.send_header("Content-Type", content_type)
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = ThreadingHTTPServer(("127.0.0.1", config["http_port"]), Handler)
    worker = threading.Thread(target=application.run, daemon=True)
    worker.start()

    def shutdown(*_):
        application.stop.set()
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)
    LOG.info("Demo %s listening at http://127.0.0.1:%s", application.mode, config["http_port"])
    try:
        server.serve_forever(poll_interval=0.2)
    finally:
        application.stop.set()
        worker.join(timeout=20)
        application.close()
        server.server_close()


if __name__ == "__main__":
    main()
