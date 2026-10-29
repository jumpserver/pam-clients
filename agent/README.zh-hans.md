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

## 4. store

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

## 5. socket

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

## 6. services

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
  "event": "credentials.updated",
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
