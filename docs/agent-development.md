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

## 当前配置与运行约定

命令为 `pam-agent`，systemd 服务为 `pam-agent.service`。macOS 和 Windows 使用 `init-local` 与 `run --local`。

配置使用显式的 `version: 1`，并通过 `socket`、`store`、`services` 描述本地接口、凭据存储和业务服务。服务账号使用 `accounts[].id`；交付阶段使用 `update_config`、`credential_check`、`reload_service`、`app_check`。未知配置字段会被拒绝。

私有状态使用显式的 `state_version: 1`，保存实例身份、缓存凭据、交付进度和待上报事件。程序只读取当前格式，不读取或转换其他格式。

业务取密响应为 `{account, source, last_pull_at}`，凭据 JSON 文件为 `{account}`；账号版本位于 `account.version`。取密时间按实际账号写入 `last_pull_at.json`，只在成功实时取密后更新。读取缓存、核对元数据和取密失败不更新时间。

`credential.secret.updated`、`credential.switch.published`、`credential.refresh.requested` 触发取密和应用；`rotation.*` 与 `credential.change.*` 只报告进度。Agent 本地脚本和 Socket 交付批次使用 `credential.refresh.requested`，要求业务应用本次交付的凭据。

`credential.switch.requested` 属于账号轮换的手动操作；`app.restart.requested` 属于应用运维。指令必须先以 `running` 认领，仅在 `accepted` 为 true 时执行，再上报实际结果。

本地服务交付失败上报 `failed`，`error_code` 标识失败步骤：`credential_check_failed`（凭据校验）、`config_update_failed`（业务配置准备、备份或写入）、`service_action_failed`（服务重载或重启）、`application_check_failed`（应用校验）。无法识别步骤时使用 `service_update_failed`。只上报错误码，不上报脚本输出或凭据内容；失败仍须管理员填写原因确认，后续 `success` 或 `skip` 不会自动解除阻塞。
