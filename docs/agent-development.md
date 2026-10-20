# Agent 开发与平台构建

本页面向 Agent 开发者。部署用户请使用 [Release 安装说明](../agent/README.zh-hans.md)。1.0.2 发行包仅提供 Linux amd64 / arm64 二进制。

从仓库根目录执行测试：

```bash
go test -race ./go/... ./agent/...
```

在 `agent/` 模块中构建当前平台的前台程序：

```bash
cd agent
go build -o jms-pam-agent ./cmd/jms-pam-agent
```

Windows 构建：

```bash
GOOS=windows GOARCH=amd64 go build -o jms-pam-agent.exe ./cmd/jms-pam-agent
```

macOS / Windows 前台程序使用 `init-local` 和 `run --local`，不支持 systemd 或 EnvironmentFile 交付。Windows 需要 AF_UNIX 流套接字，身份及凭据路径位于当前用户主目录内并设置私有 ACL。Linux systemd 安装和实际业务激活须在目标主机验证。

## 本地格式兼容基线

`CurrentConfigVersion` 和 `CurrentStateVersion` 位于 `agent/internal/agent/compatibility.go`，分别管理本地配置和状态；SDK 程序版本及通信协议版本保持独立。缺省版本固定代表最初的 v1，不能随当前版本常量一起变化。新增可选配置保留旧默认值；改变字段语义或默认行为属于不兼容变更，需要先实现兼容读取和迁移。

`agent/internal/agent/testdata/compatibility/v1/` 固定无版本配置及状态样本，覆盖默认交付、旧 `files/action`、env/YAML 字段映射，以及缓存和待确认状态。样本使用合成身份和密码，来自 `e9feed9` 基线的格式。不要为适配新实现而修改这些样本；以后增加新版本目录和对应迁移测试。

Go 测试会验证旧文件加载后保持原字节、版本字段不改变交付行为、离线恢复和撤销状态、待确认进度、新配置生成，以及未知版本拒绝且不覆盖原文件。现有 SDK contracts CI 会执行这些测试。真实应用生效及二进制升级仍需目标环境验收。

通信版本和可选能力的演进规则见 [协议兼容规则](protocol-compatibility.md)。新增状态诊断字段保持可选；授权、交付和确认语义不变，因此 `state_version` 仍为 1。
