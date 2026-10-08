# JumpServer PAM 客户端

本目录统一维护应用接入 JumpServer 的 SDK、Agent 和示例，按实现语言组织。

| 目录 | 当前能力 | 接入文档 |
| --- | --- | --- |
| `python/` | 完整 SDK；Python 3.9+ | [中文](python/README.zh-hans.md) / [English](python/README.en.md) |
| `go/` | 完整 SDK、跨平台前台 Agent 与 Linux systemd 安装；Go 1.23+ | [中文](go/README.zh-hans.md) / [English](go/README.en.md) |
| `java/` | 完整 SDK；Java 11+ | [中文](java/README.zh-hans.md) / [English](java/README.en.md) |
| `node/` | 完整 SDK、TypeScript 类型声明；Node.js 20.3+ | [中文](node/README.zh-hans.md) / [English](node/README.en.md) |
| `curl/` | 旧账号取密接口的协议调试脚本 | [中文](curl/README.zh-hans.md) / [English](curl/README.en.md) |

四种语言的 SDK 以 Python 为功能基准，封装同一套凭据客户端协议。应用只传入服务端地址、应用 AK/SK、组织及实例配置，调用客户端方法即可；API 路径、查询编码、HMAC 签名、Digest、UTC 时间、请求 ID 和版本头由 SDK 自动生成。HTTP 请求和 WebSocket 重连都会重新签名。

## SDK 功能对应

| 功能 | Python | Go | Java | Node.js |
| --- | --- | --- | --- | --- |
| 按账号取密 | `get_credential(account_id=...)` | `GetCredential(ctx, CredentialSelector{AccountID: ...})` | `getCredentialByAccountId(accountId)` | `getCredential({accountId})` |
| 按策略取密 | `get_credential(key=...)` | `GetCredential(ctx, CredentialSelector{Key: ...})` | `getCredential(key)` | `getCredential({key})` |
| 确认生效版本 | `confirm_credential(...)` | `ConfirmCredential(...)` | `confirmCredential(...)` | `confirmCredential({...})` |
| 监听事件及重连快照 | `watch_credential_events(...)` | `WatchCredentialEvents(ctx, handler)` | `watchCredentialEvents()` | `watchCredentialEvents({...})` |
| 高层事件处理 | `watch_events()` / `start_events()` | `WatchEvents(ctx, handlers)` / `StartEvents(ctx, handlers)` | `watchEvents(listener)` / `startEvents(listener)` | `watchEvents()` / `startEvents()` |
| 强制实时取密 | `get_credential(..., allow_local_fallback=False)` | `GetCredentialFresh(ctx, selector)` | `getCredential(key, false)` | `getCredential({...selector, allowLocalFallback: false})` |
| 查询待处理指令 | `list_application_commands()` | `ListApplicationCommands(ctx)` | `listApplicationCommands()` | `listApplicationCommands()` |
| 认领并执行指令 | `execute_application_command(event, handler)` | `ExecuteApplicationCommand(ctx, event, handler)` | `executeApplicationCommand(event, handler)` | `executeApplicationCommand(event, handler)` |
| 上报指令结果 | `report_application_command_result(...)` | `ReportApplicationCommandResult(...)` | `reportApplicationCommandResult(...)` | `reportApplicationCommandResult({...})` |
| Agent 状态同步 | `sync_agent(...)` | `SyncAgent(ctx, options)` | `syncAgent(...)` | `syncAgent({...})` |
| 生命周期 | `clone()` / `close()` / `with` | `Clone()` / `Close()` | `clone()` / `close()` / try-with-resources | `clone()` / `close()` |

每个客户端实例对应一个稳定、唯一的应用副本 ID。取密返回结构化凭据与版本，错误包含错误码及 HTTP 状态。事件接收回执自动发送，仅表示读取事件。交替轮换必须在验证真实连接、切换连接池并释放旧连接后确认准确的版本；凭据订阅无需确认。指令只有认领成功才执行，业务处理失败的原始错误会保留。

各 SDK 使用协议版本 1，Agent 配置版本为 1。遇到 `client_upgrade_required`（HTTP 426）需检查兼容性并升级；未知可选字段和通知事件可以传给应用，未实现的策略类型不得应用或确认。

四种 SDK 均支持事件处理函数、首次与重连快照同步、串行处理及失败退避重试，原有调用形式保留。启用高层监听后，凭据更新事件会自动取密，成功后替换本地保留的最新凭据。保留值不按时间过期；普通取密先请求 API，只有超时、网络故障或 HTTP 5xx 时才返回本地值，并标记来源。权限拒绝或明确撤销后停止使用相应凭据。Agent 自动更新并保存最新凭据，后端故障期间保留已有值。轮换切换仍要求实时获取并显式确认。接口与生命周期对照见 [事件处理接口](EVENT_HANDLERS.md)。

## 在应用中安装 SDK

Python SDK 通过 PyPI 发布，首次发布完成后可直接安装。其他语言的包仓库发布尚未配置；对应的源码安装说明仅供开发者使用。

在应用详情的接入向导中选择 SDK 语言，可下载当前应用的身份配置并查看对应事件处理示例；完整安装和接口说明在文档中心。向导为 SDK 生成实例 ID 并写入下载文件。部署时复用该文件以保持身份稳定；多个副本应分别生成材料，或为每个副本设置不同且可复用的 `JMS_INSTANCE_ID`。

### Python

```bash
python3 -m pip install jms-pam
```

导入：`from jms_pam import Client`。

### Go

在应用的 Go module 目录执行：

```bash
go mod edit -replace=github.com/jumpserver/pam-clients/go=/path/to/pam-clients/go
go get github.com/jumpserver/pam-clients/go@v0.0.0
```

导入：`import pam "github.com/jumpserver/pam-clients/go"`。示例位于 `go/cmd/demo/`、`go/cmd/events/` 和 `go/cmd/hooks/`。

### Java

先将 SDK 安装到本地 Maven 仓库：

```bash
mvn -f /path/to/pam-clients/java/pom.xml install
```

在应用的 `pom.xml` 添加：

```xml
<dependency>
  <groupId>org.jumpserver</groupId>
  <artifactId>jms-pam</artifactId>
  <version>1.0.2</version>
</dependency>
```

导入：`import org.jumpserver.pam.Client;`。示例为 `Demo.java`、`EventsDemo.java` 和 `HooksDemo.java`。

### Node.js

```bash
npm install /path/to/pam-clients/node
```

导入：`const { Client } = require('@jumpserver/pam')`，ESM 可以使用 `import { Client } from '@jumpserver/pam'`。示例为 `node/demo.js`、`node/events.js` 和 `node/hooks.js`。

## 文档语言

每个客户端目录提供十种文档语言：英文、简体中文、繁体中文、日文、韩文、巴西葡萄牙文、俄文、越南文、西班牙文和法文，对应 `README.<locale>.md`。

文档中心可以选择编程语言，正文跟随界面语言；地区别名会映射到对应文档，未知或缺失的语言回退到英文。API 路径、配置字段和代码标识符保持原名。维护与生成方法见 [文档维护](docs/README.md)。

## Python SDK 与 Go Agent

- SDK 入口：`python/jms_pam/client.py`。
- 响应模型：`python/jms_pam/models.py`，使用带类型提示的 dataclass。
- Agent 命令入口：`agent/cmd/jms-pam-agent`；配置、交付、本地 API 和安装源码位于 `agent/internal/agent`，复用 Go SDK。
- Python 分发包：`jms-pam`，通过 `jms_pam` 导入 SDK。Agent 为独立 Go 二进制 `jms-pam-agent`，固定服务名 `jms-pam-agent.service`。
- 示例应用：`python/demo.py`、`python/postgresql_app.py`、`python/file_apps/`。

从仓库根目录安装：

```bash
python3 -m pip install -e ./python
```

详细接入说明：[中文](python/README.zh-hans.md)、[English](python/README.en.md)。

## 语言规范

凭据策略 SDK 与 Agent 使用统一的 HTTP / WebSocket 协议，新实现采用各自语言的接口习惯。

- Python 遵循 [PEP 8](https://peps.python.org/pep-0008/)：方法、参数和属性使用 `snake_case`，类使用 `CapWords`，通过关键字参数调用客户端，响应使用 dataclass，客户端支持上下文管理器。
- Go 遵循 [Effective Go](https://go.dev/doc/effective_go)，使用 `gofmt`、导出标识符和显式错误返回；耗时操作接受 `context.Context`。
- Java 使用 `UpperCamelCase` 类名和 `lowerCamelCase` 方法、字段。
- Node.js 使用 `camelCase` 方法、字段和 Promise / `async`、`await`。

Python 的原始 `credential.v1` 请求对象接口作为兼容入口保留；新接入统一使用 `from jms_pam import Client`。

## Go Agent

以 `/etc/jms-pam-agent/agent.json` 为核心，支持默认 JSON 文件、EnvironmentFile、本机模板、systemd reload/restart、固定脚本及 Unix Socket。通知触发实时取密，保留最新成功值并在交付失败时重试。服务通过 `systemctl start jms-pam-agent` 启动；身份仅使用应用 AK/SK 与稳定的 `instance_id`，账号范围随应用授权，交付和服务动作由本机配置决定。详见 [配置与安装](agent/README.zh-hans.md) / [English](agent/README.en.md)。

CLI 提供 `get_accounts` 和 `get_secret ACCOUNT_ID`，通过运行中的 Agent 查询，并标明 API / 本地最新值来源。macOS / Linux 开发可用 `init-local` 和 `run --local` 前台运行，记录私有 `events.jsonl` 并原子更新最新凭据文件；生产配置也支持可选的 `event_file`。

## 独立发布与 JumpServer 集成

本仓库独立维护 Python、Go、Java、Node.js、cURL SDK 和 Go Agent。
客户端协议版本为 1；客户端版本独立于 JumpServer 服务端版本。

推送 `vX.Y.Z` tag 后，Release CI 测试 Go 客户端并编译 Linux amd64 / arm64 Agent，
同时打包各 SDK 源码，上传到草稿 Release。完整协议测试由 SDK contracts CI 执行。
发布前检查两项 CI 均通过，然后发布草稿。初始客户端版本为 `1.0.0`。
Go 模块位于 `github.com/jumpserver/pam-clients/go`；Go 子模块使用 `go/vX.Y.Z` tag，
需要通过 Go 模块代理分发时，应同时创建该 tag。

JumpServer 在 `apps/accounts/clients` 保留本仓库的 submodule，固定到已验证提交：

```bash
git submodule update --init --recursive apps/accounts/clients
```

后端继续从原路径读取文档和示例。更新客户端时，在 submodule 中检出指定版本，
然后在 JumpServer 提交新的 gitlink。构建后端镜像前必须初始化 submodule。
docker-web 使用独立的 `pam-agent-version.txt` 从本仓库 Release 下载二进制。

## Agent 与 Go SDK

`agent/` 是独立 Go 模块，可部署程序入口为 `agent/cmd/jms-pam-agent`，
内部实现为 `agent/internal/agent`。`go/` 只提供 SDK 和 SDK 示例。
根目录 `go.work` 支持两个模块联合开发；Agent 的本地模块替换保证发布时使用同一提交的 SDK。

```bash
cd agent
go test -race ./...
go build -o jms-pam-agent ./cmd/jms-pam-agent
```

仓库根目录运行 `go test -race ./go/... ./agent/...` 可同时验证两个模块。

## Python SDK 自动发布

`.github/workflows/publish-python.yml` 在 GitHub Release 发布时构建 wheel / sdist，
校验版本与 tag 一致，然后通过 PyPI Trusted Publishing 上传。推送普通提交或仅创建 tag
不会上传 PyPI；支持在 Actions 中输入已经发布的 release tag 手动重试。
CI 也会检查包构建、元数据和 wheel 安装。当前尚未完成首次 PyPI 发布。

一次性设置：

1. 在 PyPI `jumpserver` 组织中创建或添加 `jms-pam` 项目。
2. 项目 Publishing 中添加 GitHub Trusted Publisher，填写下表。
3. 在 GitHub 仓库 Settings → Environments 创建 `pypi`，与 PyPI 中的名称保持一致。

| PyPI 设置项 | 值 |
| --- | --- |
| Project name | `jms-pam` |
| Owner | `jumpserver`（GitHub owner） |
| Repository name | `pam-clients` |
| Workflow name | `publish-python.yml`（不含目录） |
| Environment name | `pypi` |

该方案不需要设置用户名、密码、API Token 或自定义 GitHub Secret。
`id-token: write` 已在发布 job 中声明，临时认证由 GitHub Actions 与 PyPI 完成。
PyPI 项目名为全局名称，组织归属不改变 `pip install jms-pam` 的命令。

每次发版前更新 `python/jms_pam/_version.py`，例如 `1.0.2`，再打对应的 `v1.0.2` tag。
Python 包版本由该文件统一读取；发布 workflow 会检查包版本与 tag 相符。
先配置 Trusted Publisher，再发布 GitHub Release。首次发布成功后，企业可同步发行包
到内部 PyPI 镜像，应用直接使用 `python3 -m pip install jms-pam`。

## 1.0.2 账号与事件接口

普通取密使用 `get_account(account_id=...)` / `GetAccount` / `getAccount`，直接读取账号的用户名、密码、资产及缓存来源。事件处理按 `account_id` 实时取密，校验 `account_revision`，应用成功后通过 `event_id` 上报结果。收到事件、取密或文件投递成功都不表示应用成功。对应的 Core 更新需同步部署。旧 key API 保留兼容。
