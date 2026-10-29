# PAM Agent 开发与平台构建

本页面向 PAM Agent 开发者。部署用户请使用 [Release 安装说明](../agent/README.zh-hans.md)。1.0.2 发行包仅提供 Linux amd64 / arm64 二进制。

从仓库根目录执行测试：

```bash
go test -race ./go/... ./agent/...
```

在 `agent/` 模块中构建当前平台的前台程序：

```bash
cd agent
go build -o pam-agent ./cmd/pam-agent
```

Windows 构建：

```bash
GOOS=windows GOARCH=amd64 go build -o pam-agent.exe ./cmd/pam-agent
```

## 命名与已有安装升级

产品名称为 **JumpServer PAM Agent**，命令和 systemd 服务分别为 `pam-agent` 和 `pam-agent.service`，Release 产物为 `pam-agent-linux-amd64` / `pam-agent-linux-arm64`。

已有安装升级前，先停止并禁用旧服务：`sudo systemctl disable --now jms-pam-agent.service`，再按照安装说明安装新二进制并运行 `pam-agent install`。不要同时运行新旧服务。现有配置、状态和 Socket 的系统默认目录继续使用 `/etc/jms-pam-agent`、`/var/lib/jms-pam-agent` 和 `/run/jms-pam-agent`，前台模式也继续使用原来的配置目录，避免改名导致身份或缓存丢失。

此次改名不改变通信协议：`X-Source: jms-pam-agent`、实例类型 `agent` 及审计来源值 `Agent` 保持不变。

macOS / Windows 前台程序使用 `init-local` 和 `run --local`，不支持 systemd 或 EnvironmentFile 交付。Windows 需要 AF_UNIX 流套接字，身份及凭据路径位于当前用户主目录内并设置私有 ACL。Linux systemd 安装和实际业务激活须在目标主机验证。

## 本地格式兼容基线

`CurrentConfigVersion` 和 `CurrentStateVersion` 位于 `agent/internal/agent/compatibility.go`，分别管理本地配置和状态；SDK 程序版本及通信协议版本保持独立。缺省版本固定代表最初的 v1，不能随当前版本常量一起变化。新增可选配置保留旧默认值；改变字段语义或默认行为属于不兼容变更，需要先实现兼容读取和迁移。

`agent/internal/agent/testdata/compatibility/v1/` 固定无版本配置及状态样本，覆盖默认交付、旧 `files/action`、env/YAML 字段映射，以及缓存和待确认状态。样本使用合成身份和密码，来自 `e9feed9` 基线的格式。不要为适配新实现而修改这些样本；以后增加新版本目录和对应迁移测试。

Go 测试会验证旧文件加载后保持原字节、版本字段不改变交付行为、离线恢复和撤销状态、待确认进度、新配置生成，以及未知版本拒绝且不覆盖原文件。现有 SDK contracts CI 会执行这些测试。真实应用生效及二进制升级仍需目标环境验收。

通信版本和可选能力的演进规则见 [协议兼容规则](protocol-compatibility.md)。新增状态诊断字段保持可选；授权、交付和确认语义不变，因此 `state_version` 仍为 1。

配置文件使用 `version` 和 `socket`；读取时兼容旧名称 `config_version` 和 `local_api`，新生成的文件只输出新名称。新旧名称同时出现会被拒绝，加载已有文件不会自动改写原文件。本次仅更名，配置格式版本仍为 1，状态文件仍使用 `state_version`。

Socket 地址使用 `socket.path`；旧的 `socket.socket_path` 和 `local_api.socket_path` 继续兼容读取，导出时统一使用 `socket.path`。同一块中同时配置 `path` 和 `socket_path` 会报错。

省略 `instance_id` 时生成 hostname 加四位随机十六进制后缀。直接运行在首次连接前将 ID 保存到私有状态的可选 `instance_id` 字段，恢复时复用；安装和 `init-local` 保存到生成的配置。输入配置无需可写，状态格式仍为 v1。旧状态没有可恢复的 ID 时，须显式提供原实例 ID，避免丢失已保存的凭据和处理进度。
