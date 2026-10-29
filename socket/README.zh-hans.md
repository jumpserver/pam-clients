<!-- socket-doc:start -->
# Socket API 接入

业务应用通过本机 PAM Agent 接入，无需安装 JumpServer SDK，也无需在业务进程保存应用 AK/SK。Agent 负责 Core 身份认证、授权核对、凭据刷新、A/B 解析和结果重试；业务负责真实连接及生效验证。

## 1. Unix Socket / HTTP / WebSocket

底层是 Unix Domain Socket（AF_UNIX），不是 TCP。连接的是文件系统路径，不开放 IP 和端口。HTTP/JSON 请求与 WebSocket 事件通道共用该 Socket；URL 中的 localhost 仅作为 HTTP Host。Agent 与 Core 之间仍使用 HTTPS/WSS。

## 2. Agent / services

在 Agent 配置中声明 Socket 服务，复用文件交付的账号声明和 allow_account_switch 规则。声明的 A ID 始终是映射键，account_id 表示当前生效的 A/B 账号。Socket 服务不能同时配置 update_config、reload_service、credential_check 或 app_check；restart_supported 显式允许该业务处理重启指令。

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "orders",
      "delivery": "socket",
      "allow_account_switch": true,
      "restart_supported": false,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ]
    }
  ]
}
```

## 3. Events / A/B

建立连接、读取初始快照、获取交付凭据、验证真实新连接、替换业务连接并释放旧会话后，才返回 success。收到通知或取到密码都不代表业务已切换。业务失败返回 failed，并等待 Agent 退避后重试。

每次连接先发送最新快照，用于对齐当前状态，不重放全部历史。delivery 只包含元数据，包括 delivery_id、声明账号键和精确的账号/策略版本。Core 不可用时也会通知明确撤销，revoked_account_ids 要求业务关闭旧会话。凭据刷新尚未完成时，delivery 可以为空。每个独立生效的业务实例使用独立 service_id；同一服务只允许一个 WebSocket 长连接。

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {
    "delivery_id": "<delivery-id>",
    "event": "credential.updated",
    "service_id": "orders",
    "credentials": {
      "<primary-account-id>": {
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }
    },
    "revoked_account_ids": []
  },
  "commands": []
}
```

```json
{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}
```

```json
{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}
```

```json
{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}
```

按原 delivery_id 返回 success 或 failed。Agent 先持久化结果，再向 Core 上报；相关业务全部成功后才确认凭据事件。accepted 表示本地已保存，不代表 Core 已接收。成功结果可幂等重报；旧版本返回 HTTP 409。失败后等待 1–30 秒，retry_ready 会变为 true。结果响应丢失时重报结果，不重复已经成功的业务动作。回调必须可重复执行：业务动作和本地 ACK 之间发生崩溃时，无法保证只执行一次。 快照中的 received、credentials_read、result 分别表示业务接收、读取凭据和业务处理结果；core_status 单独表示 Core 上报状态，支持 pending、confirmed、failed、not_required。received 回执通过同名操作提交，取密也会记录接收阶段，二者都不确认业务成功。

## 4. Commands / status

Agent 先认领指令，再交给业务。只有声明 restart_supported=true 的服务会在 commands 中收到重启指令；重启并验证就绪后返回 command.result。成功或失败先持久化，上报 Core 失败时只重试上报。A/B 切换指令复用凭据交付和业务结果确认；过期指令不能确认。

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

业务状态与凭据确认分离，支持 starting、running、degraded、stopping、stopped、error，error_code 仅填写不含敏感数据的短码。账号使用情况只在业务成功后更新，Agent 汇总各服务状态。GET /v1/usage 获取汇总；已有 POST /v1/usage 替换整个 Agent 实例的显式账号声明，不确认事件。

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

| HTTP | API |
| --- | --- |
| GET | `/v1/health`, `/v1/services` |
| GET | `/v1/accounts`, `/v1/usage` |
| GET | `/v1/credential?account_id=<id>&allow_local_fallback=false` |
| GET | `/v1/application`, `/v1/instance`, `/v1/policies?search=...` |
| GET | `/v1/policy?key=<key>` or `/v1/policy?policy_id=<id>` |
| GET | `/v1/services/<service_id>` |
| GET | `/v1/services/<service_id>/credentials?delivery_id=<delivery_id>` |
| POST | `/v1/services/<service_id>/received`, `/v1/services/<service_id>/results` |
| POST | `/v1/services/<service_id>/status` |
| POST | `/v1/services/<service_id>/commands/<command_id>/result` |
| POST | `/v1/usage`, `/v1/confirm` |

WebSocket operations: `ping`, `received`, `accounts`, `credential`, `application`, `instance`, `policies`, `policy`, `credentials`, `result`, `command.result`, `status`, `usage`.
`usage` without `account_ids` reads the aggregate; with `account_ids` (including `[]`) it replaces the instance-wide explicit usage declaration, just like HTTP POST `/v1/usage`.
Every request includes `request_id`; responses contain the same ID and `result` or `error: {code, status}`. Snapshots can arrive between responses. `{"event":"ping"}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \
  http://localhost/v1/services/orders
```

Unix Socket 的属主权限是访问边界。service_id 用于区分业务消费者，不是独立安全身份：能打开同一 Socket 的进程共享应用授权。不可信的不同业务应使用独立 Agent 实例和 Socket。事件不携带密码，业务显式取密且不要记录密码。语言环境不支持 Unix Socket WebSocket 时，也可以轮询业务快照并提交结果。

## 6. Python

Python 示例使用 websockets，不依赖 JumpServer SDK。默认仅观察元数据，不会确认业务成功。真实接入时实现 Business 回调，并调用 run(socket_path, service_id, business)。业务进程新启动时，即使旧进程已应用当前交付，也应从当前凭据初始化连接；需要跨崩溃去重时，由业务持久化 delivery_id。

```bash
python3 -m pip install 'websockets>=11'
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/socket/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
"""Unix Socket WebSocket example, without a JumpServer SDK or application keys.

Observe: python python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
For business processing, subclass Business and call run(socket, service, business).
"""
import argparse
import json
import time
from collections import deque
from urllib.parse import urlencode

from websockets.sync.client import unix_connect


class Business:
    def apply_credentials(self, credentials):
        """Verify new real connections, switch pools, then release old sessions."""
        raise NotImplementedError

    def revoke_accounts(self, account_ids):
        """Close sessions and discard credentials for explicitly revoked accounts."""
        raise NotImplementedError

    def restart(self):
        """Restart the business and verify that it is ready before returning."""
        raise NotImplementedError


class Session:
    def __init__(self, connection):
        self.connection = connection
        self.snapshots = deque(maxlen=1)
        self.sequence = 0

    def call(self, operation, **fields):
        self.sequence += 1
        request_id = str(self.sequence)
        self.connection.send(json.dumps(dict(request_id=request_id, operation=operation, **fields)))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("request_id") == request_id:
                if "error" in response:
                    # Return only the protocol code, never credentials or server payloads.
                    raise RuntimeError(response["error"]["code"])
                return response["result"]
            if response.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(response)

    def snapshot(self):
        while not self.snapshots:
            message = json.loads(self.connection.recv())
            if message.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(message)
        return self.snapshots.popleft()


def process(session, snapshot, business, completed, commands):
    delivery = snapshot.get("delivery")
    if delivery and snapshot.get("retry_ready"):
        delivery_id = delivery["delivery_id"]
        session.call("received", delivery_id=delivery_id)
        status = completed.get(delivery_id)
        if status is None:
            try:
                # Cleanup also runs when the credential set is empty.
                revoked = delivery.get("revoked_account_ids", [])
                if revoked:
                    business.revoke_accounts(revoked)
                if delivery["credentials"]:
                    credentials = session.call("credentials", delivery_id=delivery_id)
                    business.apply_credentials(credentials)
                status = "success"
                # Remember before sending the result, so a lost response doesn't
                # replay successful work on the next reconnect in this process.
                completed[delivery_id] = status
            except Exception:
                status = "failed"
        session.call("result", delivery_id=delivery_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    for command in snapshot.get("commands", []):
        if command["status"] != "pending":
            continue
        command_id = command["command_id"]
        status = commands.get(command_id)
        if status is None:
            try:
                business.restart()
                status = "success"
            except Exception:
                status = "failed"
            commands[command_id] = status
        session.call("command.result", command_id=command_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    # Bound process-local de-duplication. Persist delivery IDs in your business
    # transaction if callbacks must survive a crash between effect and ACK.
    for history in (completed, commands):
        while len(history) > 256:
            del history[next(iter(history))]


def run(socket_path, service_id, business=None):
    uri = "ws://localhost/v1/events?" + urlencode({"service_id": service_id})
    completed, commands = {}, {}
    delay = 1
    while True:
        try:
            # AF_UNIX is used here; localhost is the HTTP Host, not a TCP target.
            with unix_connect(socket_path, uri=uri, open_timeout=5, max_size=1024 * 1024) as connection:
                session = Session(connection)
                delay = 1
                initialize = True
                while True:
                    snapshot = session.snapshot()
                    if business is None:
                        delivery = snapshot.get("delivery") or {}
                        print(json.dumps({"service_id": service_id, "event": snapshot["event"],
                                          "delivery_id": delivery.get("delivery_id"),
                                          "result": snapshot.get("result")}), flush=True)
                    else:
                        # A new process must rebuild its own connections even
                        # when a previous process applied this Agent delivery.
                        if initialize and snapshot.get("result") == "success":
                            snapshot["retry_ready"] = True
                        process(session, snapshot, business, completed, commands)
                        initialize = False
        except KeyboardInterrupt:
            return
        except Exception as error:
            # Errors may contain arbitrary business details; print only the type.
            print("Socket connection interrupted:", type(error).__name__, flush=True)
            time.sleep(delay)
            delay = min(delay * 2, 30)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", required=True)
    parser.add_argument("--service", required=True)
    options = parser.parse_args()
    # The executable example observes only. It never claims business success.
    run(options.socket, options.service)
```
<!-- socket-doc:end -->
