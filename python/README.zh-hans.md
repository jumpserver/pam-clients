# JumpServer PAM Python SDK 与 Go Agent

实际连接 PostgreSQL 和 Redis、验证 SDK / Agent 凭据切换，可运行 [数据库应用 Demo](database_app/README.md)。

应用可以使用 Python SDK 直接连接 JumpServer，也可以在应用主机运行 Go Agent。Agent 可在 Linux、macOS 和 Windows 前台运行；内置 systemd 安装仅支持 Linux。客户端启动时获取轮换凭据，并通过签名认证的凭据事件流 WebSocket 接收订阅账号快照和后续更新；`credential.updated`、重连快照中的新版本以及管理员手动发送的账号切换请求会触发取密。

## Python 接口风格

方法、参数和响应属性使用 `snake_case`；类名使用 `CapWords`。客户端通过关键字参数配置，取密、确认等操作直接传参，响应具有类型提示。使用 `with` 自动关闭 HTTP 会话：

先按照下方“Python SDK 接入”准备 `jms_pam_config.py`。它是应用自己的身份配置文件，不包含在 `jms-pam` 包中；仅执行 `pip install jms-pam` 无法导入它。

<!-- python-account-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


with Client(instance_id=instance_id, **client_options) as client:
    account = client.get_account(account_id="<account-id>")
    username = account.username
    password = account.secret
```
<!-- python-account-example:end -->

`get_account` 直接返回账号，包含密码、资产信息和缓存来源标记。

实现下面的账号变更回调并启动托管监听即可。SDK 负责快照、实时取密、账号与版本校验、重试、指令认领和结果回报。业务回调必须完成真实连接验证、连接池切换和旧连接释放后再返回。Python、Node.js 使用子类钩子，Go 使用 OnAccountChanged，Java 使用 AccountEventListener。未实现业务切换时，示例会明确失败。

旧的 `jms_pam.credential.v1` 请求对象接口作为兼容入口保留，并发出 `DeprecationWarning`。新代码使用本页示例中的 `Client` 接口；升级时同步重新生成 SDK 接入配置。

## 手动发起策略新周期

在凭据策略详情的“基本设置”或“事件接收”中点击“发起新周期”。发起后会打开新周期的时间线：

- 凭据变更订阅：为当前有应用授权的订阅账号重新发布 `credential.updated`，共用一个新的 `operation_id`。密码和版本不变，不生成改密开始、成功或失败事件。SDK 按事件账号 ID 获取凭据；Agent 收到更新通知后也会重新取密，同版本不重复交付或重启服务。回执仅表示收到通知。离线客户端重连时通过快照获取当前凭据。
- 账号轮换：上一周期完成或取消后发起新周期。JumpServer 先验证备用账号能登录，再发布切换事件。客户端实际应用备用账号并确认；近期通过旧取密 API 使用过原主账号的应用，还需在切换后取得备用账号。与此同时，从切换时刻起观察原主账号的 JumpServer 取密记录；原主账号连续无成功取密达到配置时长（默认 7 天）才允许改密。期间再次取密会重置窗口。校验、切换、观察与改密共用一个周期。

管理 API：`POST /api/v1/accounts/application-credentials/<policy-id>/start-cycle/`，账号轮换需要策略修改和账号验证权限，返回 `credential` 和 `cycle_id`。策略停用、轮换未结束时不能发起新周期；订阅账号正在改密时须等待改密结束再重新发布。

## 手动发送应用事件

在应用列表的“更多 > 发送事件”，或应用详情的“事件处理 > 发送事件”中，选择事件类型、目标连接实例和有效期。

- `credential.switch.requested`：要求应用切换到策略当前发布的账号和版本。该请求不会更换策略的发布账号；更换发布账号应在轮换策略中操作。应用取密后必须校验请求中的账号和版本，完成连接切换后通过 `confirm_event(event_id=...)` 回报成功。
- `application.restart.requested`：SDK 应用调用自己实现的重启处理函数，并在健康检查成功后回报结果。Agent 仅支持接入时配置为 EnvironmentFile + restart 的 systemd 服务，先重启，再检查服务状态。

WebSocket 的 `received` 回执表示收到请求，不表示执行成功。SDK 的 `execute_application_command(event, handler)` 会先向 Core 申请执行，只有 `accepted: true` 才调用处理函数；重复投递不会再次执行。处理函数正常返回后报告成功，异常则报告失败。应用自行负责处理函数中的业务幂等和健康检查。Agent 的账号切换请求在应用按事件确认成功切换后才报告成功。

离线实例可在有效期内重新连接接收请求。无法使用 WebSocket 的 API 应用也可通过 AK/SK 签名轮询，使用固定的 `instance_id`；首次轮询会登记实例，之后管理员便可选择该实例发送事件：

1. `GET /api/v1/accounts/credential-client/commands/?instance_id=<instance-id>` 获取待处理请求（SDK 对应 `list_application_commands()`）。使用与凭据客户端相同的签名头，协议版本为 1。
2. `POST /api/v1/accounts/credential-client/command-result/`，提交 `instance_id`、`command_id` 和 `status: running`。只有响应 `accepted: true` 才执行。
3. 执行结束后向同一接口提交 `status: success` 或 `status: failed`；失败可附带不含敏感数据的 `error_code`。SDK 对应 `report_application_command_result()`。

请求超过有效期会显示超时，不能再申请执行。已申请执行的重启若因进程退出而无法回报，需检查应用状态后由管理员决定是否重新发送。普通旧取密 API 保持不变，仅调用取密接口不会接收手动事件。

## 选择接入方式

| 方式 | 适用场景 | 应用需要完成的工作 |
| --- | --- | --- |
| Python SDK | 应用可以修改 Python 代码并直接访问 JumpServer | 监听事件、拉取变化的凭据、切换连接并上报事件应用结果 |
| Go Agent | 不希望应用保存 JumpServer 密钥，或需要文件、EnvironmentFile、本地 Socket 交付 | 加载并验证 Agent 交付的凭据，成功应用后按事件 ID 确认 |

<!-- agent-doc:start -->

# Go Agent 接入

在应用主机上运行独立 Go Agent，无需安装 Python。按以下顺序完成安装、选择交付方式、配置业务动作和接入验收。

## 1. 接入前提与流程

1. 创建应用，授权允许按需获取的账号（pull）。下文的账号 ID 指 JumpServer 账号 ID，不是用户名或策略 ID。
2. 需要主动交付更新时，再绑定改密通知或 A/B 账号轮换策略（push）；仅授权账号不会主动推送凭据。
3. 在应用的接入向导下载身份配置。每个副本使用稳定、唯一的 `instance_id`，应用 AK/SK 只保存在受保护的配置中。

## 2. 安装与启动

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/jms-pam-agent-linux-amd64) · [Linux ARM64](/download/pam/jms-pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# 在下载的 Agent 和 SHA256SUMS 所在目录执行。
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./jms-pam-agent-linux-${agent_arch}" /usr/local/bin/jms-pam-agent
```

在下载的 `jms_pam_agent.json` 所在目录执行。下方 Linux 服务命令需要 root 和 systemd；每个副本替换为自己的实例 ID。前台模式需使用对应系统的二进制，macOS / Windows 可能需要从源码构建。

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id orders-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### 前台运行（Linux / macOS / Windows）

内置安装需要 Linux/root。macOS、非 root Linux 或 Windows 请在接入向导中选择 JSON 或 Socket 交付，按照 init-local 和 run --local --config 命令运行。只初始化一次，后续复用生成的私有本机配置；前台模式不执行 systemd 动作。

前台模式先将对应系统的二进制保存为 `jms-pam-agent`（Windows 为 `jms-pam-agent.exe`）；Linux/macOS 执行 `chmod 0755 ./jms-pam-agent` 赋予执行权限。

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/orders" --instance-id orders-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/orders/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\orders'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id orders-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. 选择交付方式

JSON 交付文件，EnvironmentFile 对接固定 systemd 服务，Unix Socket 提供本地 API。Agent 在交付成功后保存交付版本，应用验证并使用后才保存生效版本。Socket 归配置的应用用户所有，权限为 0600，本地请求应以该用户执行。

| 选择交付方式 | 配置与运行 |
| --- | --- |
| JSON | `delivery.delivery_mode: json` → `/opt/jumpserver-pam/credentials/<credential-key>.json` |
| EnvironmentFile (Linux / systemd) | `delivery.delivery_mode: environment` → `/opt/jumpserver-pam/credentials/<credential-key>.env` |
| Socket | `delivery.delivery_mode: socket` → `/run/jms-pam-agent/agent.sock` |

Linux systemd 的私有配置为 `/etc/jms-pam-agent/agent.json`（0600）。`state_file` 保存最新凭据与处理进度，`event_file` 记录事件元数据，`delivery_root` 和 `socket_path` 指定本地交付位置。前台初始化会生成各自的私有路径，以生成的 `agent.json` 为准。

## 4. 更新业务配置与 A/B 轮换

下载的引导配置已包含 Agent 身份和交付设置。只需在 rules 中声明业务使用的账号 ID，再配置更新业务文件、生效动作和运行中连接验证。账号设置 allow_account_switch 后可沿用同一规则处理 A/B 双向轮换；可选 credential_check 用于改文件前验证新账号。状态、事件、Socket 路径和 300 秒对账周期均有默认值。rules 为空时按凭据写默认文件。

第一个 JSON 是完整的基础配置；需要更新业务文件时，用第二个 JSON 数组替换其中的空 `rules`。文件路径、服务名和可执行脚本均为示例，启用前须在应用主机准备好。普通账号更新可省略 `allow_account_switch`；A/B 轮换必须启用它，并验证运行中业务的真实连接。

| 配置与运行 | 更新业务配置与 A/B 轮换 |
| --- | --- |
| `credential_check` | 可选：修改文件前，用新账号验证目标连接。 |
| `config_update` | 通过字段映射、模板或脚本更新业务文件。 |
| `service_action` | 对固定服务执行 reload/restart，或运行固定脚本。 |
| `application_check` | 验证运行中的业务已使用新连接，再确认生效。 |

```json
{
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "orders-node-1",
  "delivery": {
    "delivery_mode": "json",
    "delivery_root": "/opt/jumpserver-pam/credentials",
    "app_user": "orders"
  },
  "rules": []
}
```

`rules`:

```json
[
  {
    "accounts": [
      {
        "account_id": "<primary-account-id>",
        "allow_account_switch": true
      }
    ],
    "config_update": {
      "file": "/etc/order-service/config.yml",
      "format": "auto",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "service_action": {
      "unit": "order-service.service",
      "operation": "restart"
    },
    "application_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

### 配置文件格式

`config_update.format` 默认为 `auto`，省略与显式填写的效果相同。按文件扩展名识别：`.txt`、`.env`、`.properties` 使用 `env`，替换 `KEY=value`；`.yml`、`.yaml` 使用 `yaml`，替换顶层 `KEY: value`，新值写为带引号的字符串。其他扩展名需显式指定 `env` 或 `yaml`。映射字段必须已存在且只出现一次，其他配置项保留。INI section、嵌套 YAML 等复杂格式使用 `files` + `template_file` 完整生成，或用 `script` 自定义替换。环境变量文件更新后，业务仍需重新读取文件或重启才能生效。

本机 rules 配置目标文件、JSON/EnvironmentFile 或可信模板，以及可选的 systemd reload/restart 或固定可执行脚本。脚本通过标准输入接收凭据 JSON，参数固定、有超时，并应在验证业务生效后返回成功。Core 不能新增脚本路径或扩大本机能力。修改私有配置后重启 Agent。

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. 本地 API 与确认

### 查询账号与凭据

CLI 和 Socket 请求应以配置的应用用户执行。`get_accounts` 只列出授权账号元数据；`get_secret` 按需取密，也适用于未订阅 push 的账号。取密命令会把密码写到标准输出，请只在私有终端中调试。前台模式请替换为实际 Socket 路径。

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

### 确认业务生效

业务验证真实连接、完成切换后再确认事件。可选择 `application_check` 验证成功后通过 `confirm_on_success: true` 自动确认，或由业务在验证后显式确认；同步执行的检查脚本不要反向调用确认接口。本地结果先持久化，`confirmed` 表示 Core 已接受，`pending` 表示稍后重试。

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 接入验收

1. 确认接入实例在线，且账号授权范围符合预期。
2. 使用 push 时，从已绑定策略发送更新，核对交付文件或 Socket 中的账号与版本。
3. 使用 A/B 轮换时，验证运行中的业务已使用新账号并释放旧连接，再检查事件生效结果。在线、已取密、已交付都不能单独证明业务切换成功。

### 排查问题

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent 在启动、相关事件和每 300 秒进行同步。网络故障保留已获取的最新授权凭据；身份或授权被拒绝时阻断 Socket 取密，成功签名同步后恢复。已交付文件保留。SIGINT/SIGTERM 会关闭服务、连接和事件读取线程。


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK 接入

## 1. 接入前提与流程

1. 创建应用，授权允许按需获取的账号（pull）。下文的账号 ID 指 JumpServer 账号 ID，不是用户名或策略 ID。
2. 需要主动交付更新时，再绑定改密通知或 A/B 账号轮换策略（push）；仅授权账号不会主动推送凭据。
3. 在应用的接入向导下载身份配置。每个副本使用稳定、唯一的 `instance_id`，应用 AK/SK 只保存在受保护的配置中。

仅按需取密时，完成“首次获取账号凭据”即可；需要主动更新或 A/B 轮换时，再实现托管业务回调并启动监听。SDK 负责协议和事件结果，业务负责验证并切换真实连接。本页示例对应当前源码，新增 API 需要配套的 Core 和 SDK 构建；已发布的 1.0.2 安装包尚未包含全部新接口，请先部署匹配版本再运行这些示例。

## 2. 安装 SDK 与身份配置

需要 Python 3.9+。使用应用自己的 Python 或虚拟环境。通过下方命令安装当前 Core 随附的客户端源码，避免已发布的 1.0.2 包缺少托管回调。将 jumpserver.example.com 替换为 Core 地址；保留解压后的 jms-pam-clients 目录，Go 模块使用本地替换路径。

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### 本地源码安装（备用）

当前 Core 尚未提供上述源码下载接口，或需要离线部署时，取得配套客户端源码，放到 `/path/to/pam-clients`，再从业务应用目录执行以下命令。请替换为实际源码路径；托管回调需要包含对应方法的 SDK。

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` 是应用配置文件，不属于 PyPI 的 `jms-pam` 包。在应用接入向导中下载，并保存到应用脚本旁边；也可按下面的内容创建文件，将占位值替换为实际应用身份信息。`client_options` 不要提交或写入日志。每个副本使用稳定且唯一的 `instance_id`。

```python
# jms_pam_config.py
import os

client_options = {
    "endpoint": "https://jumpserver.example.com",
    "app_id": "<application-id>",
    "app_secret": "<application-secret>",
    "org_id": "<org-id>",
}
instance_id = os.environ.get("JMS_INSTANCE_ID") or "orders-node-1"
```

## 3. 首次获取账号凭据

交互式测试时，先进入 `jms_pam_config.py` 所在目录再启动 Python，然后执行下方示例。脚本运行时，将示例保存为同目录的 `app.py`，改为执行 `python3 app.py`。如果出现 `ModuleNotFoundError: No module named 'jms_pam_config'`，请检查配置文件名和位置；重新安装 `jms-pam` 不会生成此文件。

```bash
cd /path/to/your/application
chmod 0600 jms_pam_config.py
python3
```

<!-- python-account-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


with Client(instance_id=instance_id, **client_options) as client:
    account = client.get_account(account_id="<account-id>")
    username = account.username
    password = account.secret
```
<!-- python-account-example:end -->

## 4. 事件监听与业务切换

实现下面的账号变更回调并启动托管监听即可。SDK 负责快照、实时取密、账号与版本校验、重试、指令认领和结果回报。业务回调必须完成真实连接验证、连接池切换和旧连接释放后再返回。Python、Node.js 使用子类钩子，Go 使用 OnAccountChanged，Java 使用 AccountEventListener。未实现业务切换时，示例会明确失败。

<!-- python-events-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


if not hasattr(Client, 'on_account_changed'):
    raise RuntimeError('Upgrade jms-pam to a version with managed account events')


class ApplicationClient(Client):
    def on_account_changed(self, account):
        # Validate a new connection, switch the pool, then release old connections.
        # Return only after the application uses the account. Never log secrets.
        raise NotImplementedError("Implement application account switching")

    def on_account_revoked(self, account_id):
        # Close the application connections for this account.
        raise NotImplementedError("Implement revoked account cleanup")


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    # SDK handles snapshots, fresh fetches, versions, retries and event results.
    # Applied accounts are reported automatically; pull-only code can use:
    # client.report_accounts([account_id])  # [] clears explicit SDK usage.
    client.watch_events()
```
<!-- python-events-example:end -->

账号回调通过有界事件队列串行执行；普通更新失败按 1–30 秒退避重试。业务回调成功返回后才上报成功；结果上传失败时，同一次监听仅重试上传，不重复已完成的动作。指令先认领再执行，失败的指令动作不自动重做。已处理账号自动按完整快照上报使用情况，A/B 切换会替换当前账号。撤销范围需实现账号连接清理回调。使用账号上报、取密或文件交付本身不会确认业务生效。原始事件流和旧凭据钩子保留显式确认方式。业务处理仍需支持进程重启后的幂等执行。

### 监听生命周期与退出

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events 等待停止；start_events 返回不代表初始同步完成，依赖凭据的服务需等待业务就绪。每个客户端允许一个高层监听器。stop_events 和 close 等待当前钩子结束，钩子可以停止自己的客户端。clone 重新初始化子类状态；有额外构造依赖时重写 clone。

### 最新凭据与后端不可用

取密始终先请求 API。成功获取新凭据后替换本地保留值，更旧版本不会覆盖已获取的新版本；保留值不按时间过期。只有 API 超时、网络故障或 HTTP 5xx 时，才返回相同查询条件下已获取的最新凭据，并设置本地来源标记。首次获取失败且没有保留值时，抛出原始错误。SDK 在当前客户端内存中保留这些值，直到更新、撤销或关闭；clone 和进程重启从空状态开始。Agent 通过已有受保护的本地状态保留最新凭据。HTTP 401/403/404 清空 SDK 的保留值并报错，成功响应格式错误也会报错。明确撤销删除相应凭据，push 快照移除订阅范围外的 push 项；配置变更通知先保留已有值，由后续快照核对授权范围。Agent 在 HTTP 同步前先执行明确撤销或快照授权范围缩小并保存范围，后端故障期间或重启后也会阻止相应本地取密。credential_not_found（HTTP 400）同样清除 SDK 保留值。 HTTP 426 明确报错，保留凭据，不用缓存代替本次请求成功。可选元数据接口缺失（HTTP 404）不清理凭据。Agent 纯 pull 值不做持久离线保留；SDK 可按原有临时故障规则使用内存保留值。

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

启用高层事件监听后，snapshot、credential.updated 会自动获取当前凭据并替换本地保留值，再调用业务处理函数。刷新失败时保留上一份凭据并重试。Agent 同样在更新通知后主动取密，后端故障期间保留已有凭据。事件刷新和手动切换使用下方必须实时获取的调用；保留的密码不能被当成刚获取的新版本，也不会自动确认轮换。

事件连接空闲时每 10 秒发送应用层 ping，约 30 秒收不到消息则重连。重连采用 1–30 秒指数退避并重新签名。重连快照恢复当前状态，不重放历史事件。



## 5. 常用方法

### 列出授权账号

使用账号列表方法查询应用当前授权的 pull 范围。列表只返回账号、资产和关联 push 策略的元数据，不获取密码，不记录取密；未绑定策略的授权账号也会返回。完整列表方法自动读取所有分页；分页方法返回 `count` 和 `accounts`（Go 分别返回列表和总数）。默认 limit 为 200、offset 为 0；limit 范围为 1–500，search 最长 128 个字符，可匹配账号名称、用户名、资产名称或地址。重复账号、分页不完整或总数变化会报错；授权范围变化时重新查询完整列表。需要密码时，再使用账号 ID 调用取密方法。示例需要包含这些新增方法的 SDK 源码；已发布的 1.0.2 尚未在 Python、Java、Node.js 中提供这些方法。

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id

with Client(instance_id=instance_id, **client_options) as client:
    accounts = client.list_accounts()
    for account in accounts:
        print(account.id, account.username, account.asset.name)

    page = client.list_accounts_page(limit=20, offset=0, search="database")
    print("Matching accounts:", page.count)
    # Use an authorized account.id with get_account when its secret is needed.
```


### 应用、实例与策略信息

get_application 返回当前签名应用的 ID、名称、组织以及授权账号和可见策略数量；get_client_info 只返回当前副本的登记信息、类型和版本。策略列表一次返回全部可见的关联策略，包括停用策略；search 匹配策略名称或 key。查询单个策略时只提供策略 ID 或策略本身的 key，不使用账号 push key。轮换策略要求两个账号及当前生效账号均在应用授权范围内；订阅账号数量只统计应用当前有权访问的账号。这些查询只返回元数据，不返回密钥或密码，不取密、不确认凭据生效。和账号查询一样，首次访问会登记当前副本。需要同步更新 Core 和 SDK 源码；已发布的 1.0.2 尚未包含这些方法。

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id

with Client(instance_id=instance_id, **client_options) as client:
    application = client.get_application()
    print(application.id, application.name, application.accounts_count)
    replica = client.get_client_info()
    print(replica.instance_id, replica.client_version)

    policies = client.list_policies()
    for policy in policies:
        print(policy.key, policy.name, policy.is_active, policy.status)
    if policies:
        policy = client.get_policy(policy_id=policies[0].id)
        # Alternatively: client.get_policy(key=policies[0].key)
        print(policy.name, policy.accounts_count)
```


### 应用状态上报

上报应用运行状态和当前使用的账号 ID，支持 `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。每次上报都是完整快照，空账号列表会清除此前使用记录。由应用在状态变化或自己的心跳中调用，不会自动启动后台上报。Python、Java、Node.js 遇到网络、API 错误或旧 Core 不支持接口时默认返回 false；Go 使用 `TryReportStatus` 获得相同行为。严格调用方式可以获取具体错误，参数错误仍会明确提示。调用遵循配置的 HTTP 超时，请放在不影响业务响应的执行路径。上报不会取密、确认轮换或清空已有凭据缓存。`error_code` 只传机器可读错误码，不要传密码或异常原文。`get_client_info` / `GetClientInfo` / `getClientInfo` 可以读取最后一次上报及其时间。

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### 底层事件与应用指令

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

list_application_commands 轮询待处理请求，execute_application_command(event, handler) 申请执行，只有 accepted 为 true 才运行处理函数。切换处理函数校验请求账号与版本、应用凭据并确认；重启处理函数重启应用并检查健康状态，工作完成后才报告成功。失败上报不会掩盖处理函数原始异常。

`get_credential` 和基于 key 的确认接口保留兼容旧接入；新接入使用 `get_account` 和 `confirm_event`。事件结果流程需要同步更新 Core，仅升级 SDK 1.0.2 不会增加服务端能力。

### API

- `list_accounts(search="")` / `list_accounts_page(limit=200, offset=0, search="")`
- `get_application()` / `get_client_info()`
- `report_accounts(account_ids)` / `report_status("running", account_ids=[...])`
- `list_policies(search="")` / `get_policy(key=...)` / `get_policy(policy_id=...)`
- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `watch_events(stop_event=...)` / `start_events(stop_event=...)` / `stop_events()`
- `watch_credential_events(stop_event=...)`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `list_application_commands()` / `execute_application_command(event, handler)`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

## 6. 接入验收

1. 将占位值替换为已授权的账号 ID，取密后验证真实连接，不记录密码。
2. 使用事件监听时，核对首次同步和一次策略更新；业务回调完成真实切换后，事件才应显示成功。
3. 分别检查实例使用账号、运行状态和事件结果。取密或使用状态上报不会确认凭据生效。

### 排查问题

with 或 close 关闭 HTTP 会话和事件流，clone 创建独立会话。HTTP、网络、认证和解码错误抛出 PAMError，包含 code、status_code、detail 和 original_error。在应用边界重试临时故障，明确处理授权拒绝，不记录凭据。

各 SDK 使用版本 1 协议，Agent 配置格式为版本 1。收到 client_upgrade_required（HTTP 426）时检查兼容性并升级。允许未知可选字段及通知事件，未实现的策略类型不得应用或确认。事件接收回执由 SDK 自动发送，不能作为凭据已经生效的证明。 实例信息可包含 server_capabilities；缺失或 null 表示未知，未知名称可保留。能力列表描述服务端功能，不授予权限。

<!-- sdk-doc:end -->
