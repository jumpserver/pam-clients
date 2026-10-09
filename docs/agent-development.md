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
