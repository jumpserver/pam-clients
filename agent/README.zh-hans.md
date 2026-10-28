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
