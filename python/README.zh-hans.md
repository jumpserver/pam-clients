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

1. 创建应用，授权允许按需获取的账号。下文账号 ID 指 JumpServer 账号 ID。
2. 在 store/services 中声明使用的账号。需要及时更新时，在应用中开启“账号凭据变更通知”，覆盖全部授权账号；需要 A/B 账号切换时再绑定独立轮换策略。
3. 在应用接入向导下载身份配置。每个副本使用稳定、唯一的实例 ID，AK/SK 保存在受保护的配置中。

## 2. 安装与启动

### Linux / systemd

Core 与 Agent 的协议版本和 `config_version` 统一为 1。`store` 与 `services` 独立配置，不与旧结构 `delivery` / `rules` 混用。Core 只同步账号范围和版本元数据，不下发本机脚本路径；修改私有配置后重启 Agent。

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

在下载的 `jms_pam_agent.json` 所在目录执行。下方 Linux 服务命令需要 root 和 systemd；每个副本替换为自己的实例 ID。前台模式需使用对应系统的二进制，macOS / Windows 可能需要从源码构建。

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### 前台运行（Linux / macOS / Windows）

macOS、Windows 或非 root Linux 先执行一次 `init-local`，后续通过 `run --local` 复用生成的私有配置。支持标准 JSON 和环境变量文件；systemd 动作仅适用于 Linux 服务模式。

前台模式先将对应系统的二进制保存为 `jms-pam-agent`（Windows 为 `jms-pam-agent.exe`）；Linux/macOS 执行 `chmod 0755 ./jms-pam-agent` 赋予执行权限。

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app" --instance-id app-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id app-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. store / local_api

`store` 与 `services` 独立生效。`store.accounts` 是字符串数组：`["all"]` 保存应用全部授权账号，也可指定账号 ID。应用开启通知后覆盖全部授权账号，本地声明决定存储哪些账号及执行哪些业务动作；关闭通知仍可按需取密和定时同步。省略 `store` 可仅执行业务更新，空数组表示不生成标准凭据文件。`all` 不能与 ID 混用。

```json
{
  "config_version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "app-node-1",
  "pull_interval": 300,
  "local_api": {
    "owner": "appuser"
  },
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "services": []
}
```

`pull_interval` 是全局拉取周期，单位为秒，必须为正整数，省略时默认 300。Agent 启动时立即同步，之后按周期核对账号和凭据版本，仅在版本或当前账号变化时取密。`update_on_notification: false` 的业务在下一次定时拉取时更新；通知不会推迟该周期。

| 配置与运行 | store |
| --- | --- |
| `store.accounts: ["all"]` | 应用全部授权账号，不受通知开关影响。 |
| `store.accounts: ["<account-id>"]` | 只保存显式声明的账号 ID。 |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

每个账号按真实 account_id 单独生成文件，A/B 也分别存储；文件包含明文凭据，权限为 0600。正在改密的 A/B 非当前账号暂不可用。成功核对授权后清理撤销账号的文件。`local_api.owner` 控制 Socket 访问及业务输出文件的默认属主，默认使用 `store.owner`，再回退到 Agent 运行用户。Socket 与文件存储独立；私有状态和事件路径沿用默认值。版本未变化时定时对账只读取元数据，不重复取密；实际取密仍保留审计，并计入轮换访问观察。

### EnvironmentFile / systemd

EnvironmentFile 将用户名、密码和账号元数据以明文 `KEY=value` 写入私有 `.env` 文件，权限为 `0600`。业务 systemd 服务需显式配置 `EnvironmentFile=文件路径`，systemd 在启动进程时将这些值加载为该服务的进程环境变量。它不会设置主机的全局环境变量。文件更新后，要重启服务才能刷新主进程环境；仅 reload 不会替换已运行进程的环境变量。

`services`:

```json
[
  {
    "name": "sample-app-env",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "files": [
        {
          "path": "/etc/sample-app/database.env",
          "format": "environment",
          "owner": "appuser"
        }
      ]
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    }
  }
]
```

```ini
JMS_PAM_ACCOUNT_ID="<account-id>"
JMS_PAM_USERNAME="db_user"
JMS_PAM_SECRET="<password>"
```

```ini
[Service]
EnvironmentFile=/etc/sample-app/database.env
```

## 4. 更新业务配置与 A/B 轮换

将下面的业务对象加入 `services`。每个业务须有唯一且稳定的 `name`。`allow_account_switch` 放在 service 层，默认 false；开启后，有明确 A/B 关联的账号跟随当前生效账号，无关联的账号仍固定使用声明账号，只需声明 A。`update_on_notification` 同样放在 service 层，默认 true；设为 false 后，业务动作等定时同步再执行，默认周期 300 秒，存储更新和授权撤销不受影响。各业务分别记录执行版本；所有相关业务验证成功，且配置 `app_check.confirm_on_success: true`，才自动确认轮换成功。

业务按 `update_config` → `reload_service` → `app_check` 执行：更新业务配置、重载或重启服务（也可执行固定脚本）、验证运行中的业务。新字段名需要新版 Agent；`services` 的这三个步骤仅接受新名称，旧字段名会报错。

| 配置与运行 | 更新业务配置与 A/B 轮换 |
| --- | --- |
| `credential_check` | 可选：修改文件前，用新账号验证目标连接。 |
| `update_config` | 通过字段映射、模板或脚本更新业务文件。 |
| `reload_service` | 对固定服务执行 reload/restart，或运行固定脚本。 |
| `app_check` | 验证运行中的业务已使用新连接，再确认生效。 |

`services`:

```json
[
  {
    "name": "sample-app",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "file": "/etc/sample-app/config.yml",
      "format": "yaml",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    },
    "app_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

`services.accounts` 用 `id` 指定账号。单账号可直接映射 `username` / `secret`；多账号必须使用唯一 alias，以 `database.username`、`cache.secret` 等限定字段来源。alias 在 A/B 切换后保持不变。账号声明中的可选 `username` 只供阅读，真实值始终取自 Core。所需账号全部就绪后再修改文件，业务动作只执行一次；缺少账号或映射有歧义时，在写入前报错。

```json
{
  "name": "sample-app",
  "allow_account_switch": true,
  "update_on_notification": true,
  "accounts": [
    {
      "id": "<primary-account-id>",
      "alias": "database"
    },
    {
      "id": "<cache-account-id>",
      "alias": "cache"
    }
  ],
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "database.username",
      "DB_PASSWORD": "database.secret",
      "CACHE_USER": "cache.username",
      "CACHE_PASSWORD": "cache.secret"
    }
  },
  "reload_service": {
    "unit": "sample-app.service",
    "operation": "restart"
  },
  "app_check": {
    "path": "/usr/local/libexec/jms-pam/check-running-db",
    "confirm_on_success": true
  }
}
```

### 配置文件格式

`update_config.format` 默认为 `auto`，省略与显式填写的效果相同。按文件扩展名识别：`.txt`、`.env`、`.properties` 使用 `env`，替换 `KEY=value`；`.yml`、`.yaml` 使用 `yaml`，替换顶层 `KEY: value`，新值写为带引号的字符串。其他扩展名需显式指定 `env` 或 `yaml`。映射字段必须已存在且只出现一次，其他配置项保留。INI section、嵌套 YAML 等复杂格式使用 `files` + `template_file` 完整生成，或用 `script` 自定义替换。环境变量文件更新后，业务仍需重新读取文件或重启才能生效。

Core 与 Agent 的协议版本和 `config_version` 统一为 1。`store` 与 `services` 独立配置，不与旧结构 `delivery` / `rules` 混用。Core 只同步账号范围和版本元数据，不下发本机脚本路径；修改私有配置后重启 Agent。

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
  'http://localhost/v1/credential?account_id=<account-id>'
```

### 确认业务生效

业务验证真实连接、完成切换后再确认事件。可选择 `app_check` 验证成功后通过 `confirm_on_success: true` 自动确认，或由业务在验证后显式确认；同步执行的检查脚本不要反向调用确认接口。本地结果先持久化，`confirmed` 表示 Core 已接受，`pending` 表示稍后重试。

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 接入验收

1. 确认实例在线，且账号授权范围符合预期。
2. 在应用开启通知，修改授权账号凭据后核对交付文件或 Socket 中的版本，并在应用事件中查看各实例回执及处理结果。
3. 使用 A/B 轮换时，验证运行中的业务已采用新账号并释放旧连接，再确认成功。已接收、已取密、已交付都不能单独证明业务已生效。

### 排查问题

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent 在启动、相关通知及定时周期进行同步，默认 300 秒。临时网络故障保留仍获授权的凭据；明确撤销或应用禁用会阻断本地取密并删除对应标准存储文件，后续刷新失败也不例外。业务配置文件由应用管理员管理。连续通知不会推迟定时业务更新。SIGINT/SIGTERM 会关闭本地服务和事件读取线程。


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK 接入

## 1. 接入前提与流程

1. 创建应用，授权允许按需获取的账号。
2. 需要及时更新时，在应用中开启“账号凭据变更通知”，覆盖全部授权账号。A/B 账号切换仍绑定独立轮换策略。
3. 下载应用接入材料，保护 AK/SK；每个副本使用稳定、唯一的实例 ID。

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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
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

SDK 取密先请求 API，成功后替换保留值，不允许旧版本覆盖新版本。仅超时、网络错误或 HTTP 5xx 可回退到相同选择条件下已获取的值，并标记本地来源；保留值不按时间过期。SDK 使用客户端私有内存，clone 和重启从空状态开始。HTTP 401/403/404 或 credential_not_found（HTTP 400）清空保留凭据并报错；HTTP 426、成功响应格式错误均不使用回退，可选元数据接口缺失不会清空凭据。Agent 在受保护的状态中保留 store/services 声明的账号，未声明的按需查询不会自动持久化。明确撤销在 HTTP 刷新前生效，离线也不例外；push 快照不代表已存储账号的完整授权范围，成功对账会核对当前应用授权。

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

get_application 返回当前签名应用的 ID、名称、组织以及授权账号和可见策略数量；get_client_info 只返回当前副本的登记信息、类型和版本。策略列表一次返回全部可见的关联策略，包括停用策略；search 匹配策略名称或 key。查询单个策略时只提供策略 ID 或策略本身的 key，不使用账号 push key。轮换策略要求两个账号及当前生效账号均在应用授权范围内；普通账号通知由应用开关控制，覆盖全部授权账号。这些查询只返回元数据，不返回密钥或密码，不取密、不确认凭据生效。和账号查询一样，首次访问会登记当前副本。需要同步更新 Core 和 SDK 源码；已发布的 1.0.2 尚未包含这些方法。

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
