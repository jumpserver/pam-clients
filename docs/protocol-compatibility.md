# PAM 客户端协议兼容规则

Core、SDK 和 PAM Agent 可以独立发布。程序版本、本地 `version`、本地 `state_version`、HTTP/WebSocket 协议版本及 PAM Agent 同步协议的 `config_schema_version` 分别管理，不能因为程序更新就一起增加版本。

## v1 基线

当前通信协议和 PAM Agent 同步 schema 均支持 v1。Core 的 HTTP 接口、WebSocket 认证和管理页面共用 `accounts/credential_client/protocol.py` 中的支持范围。不同程序版本使用 v1 协议时仍可连接；未知协议或 PAM Agent 同步 schema 被拒绝。HTTP 426 响应保留 `client_upgrade_required`，并附带 `supported_protocol_versions`、`supported_agent_config_schema_versions`，便于定位兼容问题。

小版本新增可选字段和通知事件，保留旧字段、默认值和取密/确认语义。旧客户端可以忽略可选字段；未知策略和执行指令不能应用或确认成功。若以后必须改变协议语义，应先实现新旧版本并存及升级路径，再启用新版本。

## 范围变更与旧撤销事件

当前 Core 统一发送 `app.scope.updated` 并随后发送 `snapshot`，不再发送 `credential.revoked`，Webhook 新规则也不再默认选择旧事件。旧名称、客户端入口和历史记录保留兼容。账号授权由 `authorized_account_ids` 判断，策略绑定和通知范围由快照中的 `credentials` 判断；解绑只停止跟随策略，不能据此关闭仍获授权账号的连接。SDK 保留实际使用账号并清理策略关联，真实授权撤销时才清理缓存与业务连接。Agent 在后续 HTTP 同步前持久化明确的账号授权减少，刷新失败或重启不会恢复访问。

## 服务端能力

已有 `/client/` 接口增加可选 `server_capabilities`，SDK 调用方式如下：

| SDK | 能力字段 | 判断兼容错误 |
| --- | --- | --- |
| Python | `client.get_client_info().server_capabilities` | `error.is_compatibility_error` |
| Go | `ClientInfo.ServerCapabilities`，通过 `GetClientInfo(ctx)` 获取 | `error.IsCompatibilityError()` |
| Java | `client.getClientInfo().getServerCapabilities()` | `error.isCompatibilityError()` |
| Node.js | `(await client.getClientInfo()).serverCapabilities` | `error.isCompatibilityError` |

能力列表描述服务端功能，不授予账号权限，也不表示某个实例能够执行重启。缺少该字段或返回 null 时表示能力未知，保持既有 v1 接入方式；未知能力名称可以保留。SDK 不会为普通取密自动请求能力列表，因此已有应用不增加配置或启动依赖。

以后新增需要客户端执行的动作时，还需增加客户端能力声明及服务端下发门禁。不能根据服务端能力列表推断旧客户端能执行新动作。现有 PAM Agent 重启能力仍由本机配置决定。

## 错误与保留值

- HTTP 426 或 `client_upgrade_required` 属于兼容错误。SDK 明确报错，保留已获取的凭据；本次请求不能用缓存伪装成功，也不能确认轮换。
- 已识别为 HTTP 426 的响应即使正文格式错误或读取超时，也不能变成临时故障回退。
- 真正的 HTTP 401/403、凭据取密的 HTTP 404、`credential_not_found`（HTTP 400）仍清理 SDK 保留值。权限撤销、授权快照缩小继续生效。
- 可选元数据接口 `/application/`、`/client/`、`/policies/`、`/policy/` 返回 HTTP 404 时仍报错，但不清理凭据。这样旧 Core 缺少这些接口或查询不到策略时，不会影响已有取密。
- 在兼容错误之后，临时网络故障或 HTTP 5xx 仍可按原规则返回已有保留值。已经被明确拒绝或撤销的值不会因此恢复。
- PAM Agent 记录 `sync_error: client_upgrade_required`，保持已知授权下的本地读取；本地实时查询 API 仍返回 426。真正的身份拒绝继续阻断访问。成功同步后清除错误记录。

## 发布验证

共享契约测试覆盖四种 SDK 的旧响应缺字段、新响应增加字段、未知能力、可选接口缺失、426 后缓存保留、后续 401 撤销，以及 426 正文错误/超时。PAM Agent 测试覆盖同步和取密阶段的兼容失败、重启恢复及已拒绝身份不被恢复。Core 测试覆盖程序版本独立、HTTP 错误支持范围、WebSocket 与管理页面一致性。

运行方法见 [文档维护](README.md) 和 [PAM Agent 开发](agent-development.md)。这些测试使用隔离数据库或合成协议服务；真实数据库连接切换和服务部署需要目标环境验证。

## 事件名称

当前统一使用 `rotation.verify.started/failed/cancelled`、`rotation.waiting_for_app`、`app.scope.updated` 和 `app.restart.requested`。SDK 接收旧 `rotation.verification.*`、`rotation.waiting_for_application`、`application.scope.updated` 和 `application.restart.requested` 时映射到新名称；事件 ID、回执和结果接口不变。Webhook 保留已有订阅承诺的事件名称，同时选择新旧名称时只投递一次，并优先使用新名称。旧轮换历史仍可按原记录展示，模型、API 路径及实例字段不因事件缩写改变。
