# JumpServer PAM Python SDK 与 PAM Agent

实际连接 PostgreSQL 和 Redis、验证 SDK / PAM Agent 凭据切换，可运行 [数据库应用 Demo](database_app/README.md)。

应用可以使用 Python SDK 直接连接 JumpServer，也可以在应用主机运行 PAM Agent。PAM Agent 可在 Linux、macOS 和 Windows 前台运行；内置 systemd 安装仅支持 Linux。应用授权决定按需取密范围，应用级“账号凭据变更通知”覆盖全部授权账号，A/B 账号切换另行绑定轮换策略。客户端通过签名认证的 WebSocket 接收快照和后续事件；`credential.updated`、`snapshot` 和管理员手动发送的账号切换请求用于获取并应用当前凭据。

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

轮换周期与恢复操作见具体策略的“轮换运行说明”；手动指令和通用协议见下方“事件与指令”。

## 选择接入方式

| 方式 | 适用场景 | 应用需要完成的工作 |
| --- | --- | --- |
| Python SDK | 应用可以修改 Python 代码并直接访问 JumpServer | 监听事件、拉取变化的凭据、切换连接并上报事件应用结果 |
| PAM Agent | 不希望应用保存 JumpServer 密钥，或需要文件、EnvironmentFile、本地 Socket 交付 | 加载并验证 PAM Agent 交付的凭据，成功应用后按事件 ID 确认 |

<!-- agent-doc:start -->

# JumpServer PAM Agent 接入

在应用主机上运行独立 PAM Agent，无需安装 Python。按以下顺序完成安装、选择交付方式、配置业务动作和接入验收。

## 1. 接入前提与流程

1. 创建应用，授权允许按需获取的账号。下文账号 ID 指 JumpServer 账号 ID。
2. 在 store/services 中声明使用的账号。需要及时更新时，在应用中开启“账号凭据变更通知”，覆盖全部授权账号；需要 A/B 账号切换时再绑定独立轮换策略。
3. 在应用接入向导下载身份配置并保护 AK/SK，`instance_id` 可省略。

## 2. 安装与启动

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/pam-agent-linux-amd64) · [Linux ARM64](/download/pam/pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# 在下载的 PAM Agent 和 SHA256SUMS 所在目录执行。
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./pam-agent-linux-${agent_arch}" /usr/local/bin/pam-agent
```

在下载的 `pam-agent.json` 所在目录执行。下方 Linux 服务命令需要 root 和 systemd。前台运行时使用与主机系统和架构匹配的可执行文件。

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### 前台运行（Linux / macOS / Windows）

macOS、Windows 或非 root Linux 先执行一次 `init-local`，后续通过 `run --local` 复用生成的私有配置。支持标准 JSON 和环境变量文件；systemd 动作仅适用于 Linux 服务模式。

前台模式先将对应系统的二进制保存为 `pam-agent`（Windows 为 `pam-agent.exe`）；Linux/macOS 执行 `chmod 0755 ./pam-agent` 赋予执行权限。

```bash
chmod 0600 ./pam-agent.json
./pam-agent init-local \
  --bootstrap ./pam-agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app"
./pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\pam-agent.exe init-local --bootstrap (Join-Path $PWD 'pam-agent.json') --directory $agentDir
.\pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. 配置样例

下面是一份 Linux/systemd 完整配置样例，包含应用身份、拉取周期、标准凭据文件、本地 API 和业务服务。请替换应用身份、账号 ID、系统用户、文件路径、服务名和检查脚本，并提前部署检查脚本。仅需文件交付或本地 API 时，将 `services` 设为 `[]`。后续章节分别说明各配置块，它们均属于同一份配置文件。

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
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
}
```

`instance_id` 可省略或留空，PAM Agent 会生成 `<hostname>-<4 位随机十六进制字符>`，例如 `orders-pod-a1b2`。显式配置或传入 `--instance-id` 时优先使用指定值。安装和 `init-local` 会将生成的 ID 保存到输出配置；直接运行时保存到私有状态文件，重启后复用，不改写输入配置。K8s 多副本可共用省略该字段的配置，每个副本须使用独立、可写的状态目录；没有保留状态的新副本会生成新 ID。

`pull_interval` 是全局拉取周期，单位为秒，必须为正整数，省略时默认 300。PAM Agent 启动时立即同步，之后按周期核对账号和凭据版本，仅在版本或当前账号变化时取密。`update_on_notification: false` 的业务在下一次定时拉取时更新；通知不会推迟该周期。

## 4. 账号存储 store

`store` 用于将凭据保存为标准 JSON 或环境变量文件，供通过文件读取凭据的应用使用。它决定保存哪些账号、输出目录、文件格式和属主。修改已有业务配置文件或重载服务，应在 `services` 中配置。

`store` 与 `services` 独立生效。`store.accounts` 是字符串数组：`["all"]` 保存应用全部授权账号，也可指定账号 ID。应用开启通知后覆盖全部授权账号，本地声明决定存储哪些账号及执行哪些业务动作；关闭通知仍可按需取密和定时同步。省略 `store` 可仅执行业务更新，空数组表示不生成标准凭据文件。`all` 不能与 ID 混用。

```json
{
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  }
}
```

| 配置与运行 | store |
| --- | --- |
| `store.accounts: ["all"]` | 应用全部授权账号，不受通知开关影响。 |
| `store.accounts: ["<account-id>"]` | 只保存显式声明的账号 ID。 |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

每个账号按真实 account_id 单独生成文件，A/B 也分别存储；文件包含明文凭据，权限为 0600。正在改密的 A/B 非当前账号暂不可用。成功核对授权后清理撤销账号的文件。版本未变化时定时对账只读取元数据，不重复取密；实际取密仍保留审计，并计入轮换访问观察。

## 5. 本地接口 socket

`socket` 用于配置本机 Unix Socket 接口，供应用查询健康状态、列出账号、按需获取凭据、上报账号使用情况和确认业务处理结果。不配置 `store` 也可以通过 Socket 查询。取密权限由 Core 中的应用授权决定；`store.accounts` 只选择文件输出账号，不是本地 API 的账号访问范围。

`path` 默认是 `/run/jms-pam-agent/agent.sock`。`owner` 控制 Socket 访问及业务输出文件的默认属主，省略时先使用 `store.owner`，再回退到 PAM Agent 运行用户。省略整个 `socket` 块会采用默认配置，不会关闭接口。前台模式通过 `init-local` 将 Socket 设置为 `<directory>/run/agent.sock`，属主设置为当前用户。

非 root 运行时，可以省略 `store.owner` 和 `socket.owner`，默认使用当前用户；`init-local` 会自动将两者设置为当前用户。root 运行且需要业务用户读取文件或访问 Socket 时，再指定该业务用户。`owner` 只控制输出和访问属主，不会切换 PAM Agent 进程的运行用户。

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### 查询账号与凭据

CLI 和 Socket 请求应以配置的应用用户执行。`get_accounts` 只列出授权账号元数据；`get_secret` 按需取密，也适用于未订阅 push 的账号。取密命令会把密码写到标准输出，请只在私有终端中调试。前台模式请替换为实际 Socket 路径。

```bash
pam-agent get_accounts
pam-agent get_secret '<account-id>'

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
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 业务更新 service

将下面的业务对象加入 `services`。每个业务须有唯一且稳定的 `name`。`allow_account_switch` 放在 service 层，默认 false；开启后，有明确 A/B 关联的账号跟随当前生效账号，无关联的账号仍固定使用声明账号，只需声明 A。`update_on_notification` 同样放在 service 层，默认 true；设为 false 后，业务动作等定时同步再执行，默认周期 300 秒，存储更新和授权撤销不受影响。各业务分别记录执行版本；所有相关业务验证成功，且配置 `app_check.confirm_on_success: true`，才自动确认轮换成功。

业务按 `update_config` → `reload_service` → `app_check` 执行：更新业务配置、重载或重启服务（也可执行固定脚本）、验证运行中的业务。新字段名需要新版 PAM Agent；`services` 的这三个步骤仅接受新名称，旧字段名会报错。

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

### accounts

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

### update_config

`services` 中的每个对象表示一个业务服务。`update_config` 支持三种互斥方式：`file + fields_map` 修改已有字段，`files + template_file` 按模板生成完整文件，`script` 执行自定义更新。下面的配置片段替换对应业务的 `update_config`，每个业务只选一种。

#### file + fields_map

`update_config.format` 默认为 `auto`，省略与显式填写的效果相同。按文件扩展名识别：`.txt`、`.env`、`.properties` 使用 `env`，替换 `KEY=value`；`.yml`、`.yaml` 使用 `yaml`，替换顶层 `KEY: value`，新值写为带引号的字符串。其他扩展名需显式指定 `env` 或 `yaml`。映射字段必须已存在且只出现一次，其他配置项保留。INI section、嵌套 YAML 等复杂格式使用 `files` + `template_file` 完整生成，或用 `script` 自定义替换。环境变量文件更新后，业务仍需重新读取文件或重启才能生效。

```json
{
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "username",
      "DB_PASSWORD": "secret"
    }
  }
}
```

#### files + template_file

模板方式用于嵌套 JSON、INI 等需要完整生成的配置。通过 `files` 指定输出文件，`format` 为 `template`，`template_file` 为本机模板的绝对路径。模板先渲染，再原子替换目标文件，权限为 0600；`owner` 可省略，默认使用 `socket.owner`。`json` 函数负责 JSON 字符串的引号与转义。

```json
{
  "update_config": {
    "files": [
      {
        "path": "/etc/sample-app/credentials.json",
        "format": "template",
        "template_file": "/etc/pam-agent/templates/credentials.json.tmpl",
        "owner": "appuser"
      }
    ]
  }
}
```

`/etc/pam-agent/templates/credentials.json.tmpl`:

```gotemplate
{
  "database": {
    "username": {{json (index .Credentials "<primary-account-id>").Username}},
    "password": {{json (index .Credentials "<primary-account-id>").Secret}}
  }
}
```

模板中的 `.Credentials` 和脚本输入的 `credentials` 均以 `services.accounts[].id` 声明的账号 ID 为键，不能使用 alias。请将示例中的 `<primary-account-id>` 替换为实际 ID。A/B 切换后，该键仍是声明的 A 账号 ID，值中的 `AccountID`（脚本中为 `account_id`）是当前生效账号。

#### script

脚本方式必须用 `target` 声明一个输出路径，或用 `targets` 声明多个路径，供 PAM Agent 在修改前备份。`script.path` 是提前部署的本机可执行文件；`args` 仅放固定参数，凭据通过标准输入传入 JSON。`timeout_seconds` 为 1–300 秒，省略时默认 120 秒。脚本以 PAM Agent 的运行用户执行；退出码 0 表示更新成功，非 0 或超时会失败并重试，因此脚本应可重复执行。

```json
{
  "update_config": {
    "target": "/etc/sample-app/credentials.json",
    "script": {
      "path": "/usr/local/libexec/pam-agent/update-credentials",
      "args": [
        "/etc/sample-app/credentials.json"
      ],
      "timeout_seconds": 30
    }
  }
}
```

脚本标准输入示例（节选）：

```json
{
  "event": "credential.updated",
  "credentials": {
    "<primary-account-id>": {
      "account_id": "<active-account-id>",
      "username": "db_app",
      "secret": "<password>",
      "event_id": "<event-id>"
    }
  }
}
```

下面是适用于 Linux/macOS 的 Python 3 示例，更新已有 JSON 文件中的 database 字段，保留其他配置与文件属主，按 0600 权限原子替换。提前创建目标目录和初始 JSON 文件，部署脚本并赋予执行权限；自定义脚本需自行处理输出格式、权限与属主。

`/usr/local/libexec/pam-agent/update-credentials`:

```python
#!/usr/bin/env python3
import json
import os
import sys
import tempfile
from pathlib import Path

payload = json.load(sys.stdin)
credential = payload["credentials"]["<primary-account-id>"]
target = Path(sys.argv[1])
original = target.stat()
with target.open(encoding="utf-8") as source:
    config = json.load(source)
config.setdefault("database", {}).update(
    username=credential["username"], password=credential["secret"]
)

fd, temporary = tempfile.mkstemp(prefix=".pam-agent-", dir=target.parent)
try:
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        os.fchmod(output.fileno(), 0o600)
        os.fchown(output.fileno(), original.st_uid, original.st_gid)
        json.dump(config, output, ensure_ascii=False)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, target)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
```

模板或脚本成功只代表配置更新完成。需要重载服务或验证业务时，继续在同一个 service 中配置 `reload_service`、`app_check`。修改配置后先检查，再重启 PAM Agent：

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


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

## 7. 接入验收

1. 确认实例在线，且账号授权范围符合预期。
2. 在应用开启通知，修改授权账号凭据后核对交付文件或 Socket 中的版本，并在应用事件中查看各实例回执及处理结果。
3. 使用 A/B 轮换时，验证运行中的业务已采用新账号并释放旧连接，再确认成功。已接收、已取密、已交付都不能单独证明业务已生效。

### 排查问题

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent 在启动、相关通知及定时周期进行同步，默认 300 秒。临时网络故障保留仍获授权的凭据；明确撤销或应用禁用会阻断本地取密并删除对应标准存储文件，后续刷新失败也不例外。业务配置文件由应用管理员管理。连续通知不会推迟定时业务更新。SIGINT/SIGTERM 会关闭本地服务和事件读取线程。


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

SDK 取密先请求 API，成功后替换保留值，不允许旧版本覆盖新版本。仅超时、网络错误或 HTTP 5xx 可回退到相同选择条件下已获取的值，并标记本地来源；保留值不按时间过期。SDK 使用客户端私有内存，clone 和重启从空状态开始。HTTP 401/403/404 或 credential_not_found（HTTP 400）清空保留凭据并报错；HTTP 426、成功响应格式错误均不使用回退，可选元数据接口缺失不会清空凭据。PAM Agent 在受保护的状态中保留 store/services 声明的账号，未声明的按需查询不会自动持久化。明确撤销在 HTTP 刷新前生效，离线也不例外；push 快照不代表已存储账号的完整授权范围，成功对账会核对当前应用授权。

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

启用高层事件监听后，snapshot、credential.updated 会自动获取当前凭据并替换本地保留值，再调用业务处理函数。刷新失败时保留上一份凭据并重试。PAM Agent 同样在更新通知后主动取密，后端故障期间保留已有凭据。事件刷新和手动切换使用下方必须实时获取的调用；保留的密码不能被当成刚获取的新版本，也不会自动确认轮换。




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
2. 使用事件监听时，核对首次同步和一次授权账号更新或 A/B 切换；业务回调完成真实切换后，事件才应显示成功。
3. 分别检查实例使用账号、运行状态和事件结果。取密或使用状态上报不会确认凭据生效。

### 排查问题

with 或 close 关闭 HTTP 会话和事件流，clone 创建独立会话。HTTP、网络、认证和解码错误抛出 PAMError，包含 code、status_code、detail 和 original_error。在应用边界重试临时故障，明确处理授权拒绝，不记录凭据。

各 SDK 使用版本 1 协议，PAM Agent 配置格式为版本 1。收到 client_upgrade_required（HTTP 426）时检查兼容性并升级。允许未知可选字段及通知事件，未实现的策略类型不得应用或确认。事件接收回执由 SDK 自动发送，不能作为凭据已经生效的证明。 实例信息可包含 server_capabilities；缺失或 null 表示未知，未知名称可保留。能力列表描述服务端功能，不授予权限。

<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# 应用集成概览

## 先了解授权、通知与轮换

1. **账号授权**决定应用可以按需获取哪些账号的凭据。安装客户端前，先完成应用授权。
2. **账号凭据变更通知**是应用级开关，覆盖全部授权账号。应用需要跟随账号密码更新时开启，无需创建订阅策略。
3. **A/B 账号切换**使用独立绑定的轮换策略，先将业务切到另一个账号，再修改原账号密码。两个账号都须获得授权；周期、等待条件、取消与恢复请查看具体策略中的“轮换运行说明”。

## 选择接入方式

| 接入方式 | 适用情况 | 从哪里开始 |
| --- | --- | --- |
| SDK | 可以修改业务代码，由应用直接连接 JumpServer。 | 选择“SDK 接入”和编程语言，需要自动更新时实现托管账号变更回调。 |
| PAM Agent | 需要本地凭据文件、业务配置更新或本地 Socket，希望将 JumpServer 密钥保留在独立 Agent 中。 | 选择“Agent 接入”，安装后按需配置 store、socket 和 services。 |
| Socket API | 业务代码通过本机 Agent 取密，自行处理事件和连接切换，不持有应用密钥。 | 选择“Socket API 接入”，声明 delivery: socket 的业务服务。 |

## 接入与验收顺序

1. 创建应用，授权账号，下载并保护应用身份配置。
2. 选择 SDK 或 PAM Agent，安装匹配客户端，先获取一个账号并验证真实连接。
3. 需要自动更新时，开启应用通知并实现业务更新；需要 A/B 切换时，再绑定轮换策略并允许跟随当前账号。
4. 验证一次账号更新或 A/B 切换：运行中的业务采用新凭据、旧连接释放、业务处理结果成功。仅接收事件、取密或写文件不足以通过验收。

## 后续查阅位置

“Agent 接入”说明安装、文件存储、本地接口和业务动作；“SDK 接入”说明各语言安装、托管回调和 API；“事件与指令”集中解释通用事件数据、快照、回执及手动指令。每个账号切换策略内的“轮换运行说明”解释该策略的周期、阻塞和恢复。 “Socket API 接入”单独说明本机双向通信、事件接收、取密、业务确认、失败重试和 A/B 切换。

<!-- overview-doc:end -->

<!-- protocol-doc:start -->

# 事件与指令

本参考适用于各语言 SDK 和 PAM Agent。常规接入优先使用 SDK 托管回调或 Agent 业务服务；自行实现事件消费者时再使用底层协议。

## 事件职责

| 事件 | 应用职责 |
| --- | --- |
| credential.updated、snapshot 条目 | 获取事件指定的账号、校验版本，完成业务应用后上报结果。SDK 托管回调负责协议处理。 |
| app.scope.updated | 对账范围并清理失效状态。解除轮换绑定不等于撤销账号授权。 |
| `rotation.*`（如 `rotation.started`、`rotation.completed` 等）、`credential.change.*`（如 `credential.change.started`、`credential.change.completed` 等） | 更新进度、日志或告警，不重复应用凭据，也不对这些通知确认业务成功。 |
| credential.switch.requested、app.restart.requested | 管理员针对选定实例的手动指令，须认领、执行并上报指令结果。 |

## 完整事件清单

以下逐项列出 Core 应用事件、手动指令及连接控制消息；兼容项单独标注。这里是完整事件参考，轮换详情只解释其在具体流程中的作用。

### 凭据与范围事件

credential.updated 负责应用凭据；app.scope.updated 和 snapshot 负责范围对账。绑定消失时停止跟随策略并保留仍授权账号的使用状态；实际授权消失时才停用本地取密、清理凭据并通知业务释放连接。

| 事件标识 | 含义 |
| --- | --- |
| `credential.updated` | 当前凭据或发布账号更新，按事件账号取密并应用。 |
| `app.scope.updated` | 账号授权、通知开关或轮换策略绑定变化。按 authorized_account_ids 判断账号是否仍获授权，再通过 snapshot 对账通知项和轮换绑定；解除绑定不应关闭仍获授权账号的连接。 |

### 通用改密事件

通用任务生命周期通知用于进度与告警，不因此再次取密或确认业务成功；通过 credential.updated 或 snapshot 应用凭据。

| 事件标识 | 含义 |
| --- | --- |
| `credential.change.started` | 改密任务进入等待或执行阶段，不代表远端密码已改变。 |
| `credential.change.completed` | 改密任务成功，不代表应用已经采用新凭据。 |
| `credential.change.failed` | 改密失败或结果无法核验，检查任务及远端账号。 |

### 轮换进度事件

这些通知说明轮换进度与结果。应用通过 credential.updated 或 snapshot 切换，不因每条进度通知重复执行。

| 事件标识 | 含义 |
| --- | --- |
| `rotation.verify.started` | 开始验证备用账号能否登录。 |
| `rotation.verify.skipped` | 已按凭据策略跳过备用账号登录验证。 |
| `rotation.verify.failed` | 备用账号验证失败，本次不发布切换。 |
| `rotation.verify.cancelled` | 备用账号验证已取消。 |
| `rotation.started` | 轮换已发布目标账号并进入切换阶段。 |
| `rotation.waiting_for_app` | 等待必需应用实例完成切换并确认。 |
| `rotation.source.waiting` | 发布切换后，观察原主账号的成功取密记录。 |
| `rotation.source.ready` | 原主账号无取密窗口和客户端确认条件达标，可以继续改密。 |
| `rotation.completed` | 原主账号改密已核验，本轮轮换完成。 |
| `rotation.failed` | 轮换进入改密失败或需要核验恢复的状态。 |
| `rotation.force_stopped` | 管理员跳过剩余恢复确认，强制结束周期。 |

### 管理员指令事件

先用 running 认领，仅在 accepted 为 true 时执行，再上报指令结果；这些指令独立于自动轮换进度通知。

| 事件标识 | 含义 |
| --- | --- |
| `credential.switch.requested` | 要求选定实例应用已经发布的账号及版本，不会启动新轮换。 |
| `app.restart.requested` | 执行应用支持的重启动作，并验证健康状态。 |

### 连接与回执消息

SDK 和 Agent 管理保活及接收回执。snapshot、pong 无需 received 回执；快照中的凭据条目单独进行应用处理。

| 事件标识 | 含义 |
| --- | --- |
| `snapshot` | Core 在连接、重连或范围变化时发送当前通知范围及授权账号 ID。 |
| `ping` | 客户端向 Core 发送应用层保活消息。 |
| `pong` | Core 对应用层 ping 的响应。 |
| `received` | 客户端确认已读取事件，不表示业务成功。 |

### 兼容事件

旧事件名和旧准备周期兼容项，不作为新接入的执行流程。当前 Core 不再发送 credential.revoked；新轮换周期验证备用账号后直接发布切换。

| 事件标识 | 含义 |
| --- | --- |
| `credential.revoked` | 已停用：当前 Core 不再发送。由 app.scope.updated 和 snapshot 统一处理；仅保留旧 Core 接入及历史记录兼容。 |
| `configuration.updated` | 旧事件名，兼容映射为 `app.scope.updated`。 |
| `rotation.preparation.started` | 旧准备流程开始对齐应用当前账号。 |
| `rotation.accounts.aligned` | 旧准备流程中，应用账号已完成对齐。 |
| `rotation.standby.waiting` | 旧准备流程观察备用账号取密。 |
| `rotation.preparation.ready` | 旧准备流程就绪，不代表已切换或改密。 |
| `rotation.preparation.cancelled` | 旧准备流程已取消。 |
| `rotation.verification.started` | 旧事件名，兼容映射为 `rotation.verify.started`。 |
| `rotation.verification.failed` | 旧事件名，兼容映射为 `rotation.verify.failed`。 |
| `rotation.verification.cancelled` | 旧事件名，兼容映射为 `rotation.verify.cancelled`。 |
| `application.scope.updated` | 旧事件名，兼容映射为 `app.scope.updated`。 |
| `application.restart.requested` | 旧事件名，兼容映射为 `app.restart.requested`。 |
| `rotation.waiting_for_application` | 旧事件名，兼容映射为 `rotation.waiting_for_app`。 |
| `rotation.verification.skipped` | 旧事件名，兼容映射为 `rotation.verify.skipped`。 |

## 事件字段

| 字段 | 含义 |
| --- | --- |
| event_id | 用于接收回执和业务结果的事件标识。snapshot 本身无需回执，其中的凭据条目可有各自的 event_id。 |
| event / occurred_at | 事件类型和发布时间。 |
| account_id / account_revision | 实际目标账号及密码版本；应用前实时获取的账号版本须匹配。 |
| credential_key / revision | A/B 策略的发布元数据；普通账号通知使用账号版本，不混用两种版本。 |
| credential_mode | alternating_rotation 表示 A/B 策略，subscription 表示应用级账号通知，无需订阅策略；应用范围事件可能为 null。 |
| account_switch.account_ids | A/B 切换关联的账号。 |
| operation_id | 关联同一次变更或轮换周期中的事件。 |
| command_id / expires_at | 手动指令标识和执行截止时间，使用独立的指令结果接口。 |

## 快照与授权范围

首次连接、重连及范围变化时，Core 发送 snapshot。credentials 包含应用开启通知后的全部授权账号，以及绑定且仍有授权的有效 A/B 轮换策略。每项按 credential.updated 处理，使用 event_id、account_id 和 account_revision。快照还包含 authorized_account_ids；完整授权账号元数据可通过 get_accounts 查询。解除轮换策略绑定不等于撤销账号授权。

```json
{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }]
}
```

## 接收回执与业务结果

SDK 在交出业务事件前自动尽力发送 received 回执，应用无需额外发送。回执仅表示已读取事件，不表示凭据已经生效，也不能代替确认。应用应处理首次和重连快照；snapshot 与 pong 无需回执。

```json
{"event": "received", "event_id": "<event-id>"}
```

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

## 管理员手动指令

管理员从应用的“发送事件”操作选择目标实例及有效期。

- credential.switch.requested 要求实例应用轮换策略**已经发布**的账号及版本，不会启动新轮换，也不会选择另一个账号。
- app.restart.requested 要求应用执行支持的重启动作并验证健康状态；SDK 应用须实现相应处理函数，Agent 能力由本地服务配置决定。

SDK 托管处理函数负责指令认领和结果回报。底层消费者使用已签名的客户端 API：

1. GET /api/v1/accounts/credential-client/commands/，传入 instance_id，轮询待处理指令；已连接实例也可通过 WebSocket 接收。
2. POST /api/v1/accounts/credential-client/command-result/，提交 instance_id、command_id 和 status: running，仅在 accepted 为 true 时执行。
3. 实际完成业务切换，或重启且健康检查成功后，通过同一指令结果接口报告 status: success；失败报告 failed，并使用安全的 error_code。received 回执或仅调用 confirm_event 不能替代指令结果上报。

过期指令不能再认领，重复投递不得重复执行已认领动作。失败的指令动作不自动重做；重新发送重启前先检查应用状态。

## 连接保活与兼容

事件连接空闲时每 10 秒发送应用层 ping，约 30 秒收不到消息则重连。重连采用 1–30 秒指数退避并重新签名。重连快照恢复当前状态，不重放历史事件。

当前使用 app.scope.updated、app.restart.requested、rotation.verify.started/failed/cancelled 和 rotation.waiting_for_app。旧 application 前缀、rotation.verification 名称及 configuration.updated 兼容映射到新名称，事件 ID 不变。credential.revoked 已停止发送，仅保留旧 Core 接入及历史记录兼容。

`get_credential` 和基于 key 的确认接口保留兼容旧接入；新接入使用 `get_account` 和 `confirm_event`。事件结果流程需要同步更新 Core，仅升级 SDK 1.0.2 不会增加服务端能力。

<!-- protocol-doc:end -->

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
