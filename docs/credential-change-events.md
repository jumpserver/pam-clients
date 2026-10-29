# 同账号改密的事件边界

## 当前 v1 的事件链

成功的托管改密：

```text
credential.change.started
  → 目标账号改密、结果保存
  → credential.change.completed
  → credential.updated
  → 应用实时取密、验证新连接、替换旧连接
  → 应用上报事件处理结果
```

失败的托管改密：`credential.change.started → credential.change.failed`。不能发布一次不存在的新凭据更新。远端可能已变化、但无法确认结果时应保留候选密码及待核验状态，不宣称改密成功。

| 用户侧概念 | v1 事件 | 用途 |
| --- | --- | --- |
| 改密前通知 | `credential.change.started` | 告知本次改密进入流程。当前 `ChangeSecretRecord` 建立为 pending 时已发送，pending → running 不重复发送，不能解读为远端已改密或所有应用已准备好。 |
| 改密后成功/失败 | `credential.change.completed` / `credential.change.failed` | 表示改密任务结果，不表示全部应用已经换好连接。 |
| 新凭据已发布 | `credential.updated` | 触发 SDK/PAM Agent 实时取密和交付；应用仍须验证实际数据库连接后才能报告应用成功。 |
| 计划改密 | 暂无；后续可增加 `credential.change.planned` | 提前通知预期执行时间，属于新增可选通知，不能替代 started 或代表一定会执行。 |

当前保留已有事件名，避免与 `pre_change_secret`、`post_change_secret`、`secret_updated` 同时发送造成重复取密。小版本添加可选事件或字段；旧客户端忽略不认识的通知。改变已有事件含义、把通知升级为执行门禁，需要新的明确契约。

## PAM Agent 进度与通用取密审计

普通取密仍由现有通用审计记录账号、版本、实例和结果，不要求提供 event_id，也不会自动成为订阅或轮换参与者。PAM Agent 不增加独立的取密回执接口，继续通过现有 `/agent/sync/` 的 `credentials` 上报已持有版本，通过 `delivered_credentials` 上报已交付版本。

Core 只接受当前应用授权、有效订阅和本次本机交付范围中的账号版本。陈旧或超前版本不能推进进度；没有持有证据的交付报告不能凭空建立取密进度。兼容旧调用方：通过订阅 key 实际取密后，可沿用只报告交付版本的方式。

首次观察到已持有或已交付当前版本时，更新现有 `CredentialClientStatus` 的相应版本和时间；版本 0 同样有效。重复 sync 不生成取密审计，不重写这些阶段的时间，不把收到、持有或交付解释为应用成功。实际取密请求的准确时间仍以通用审计为准，PAM Agent 的进度时间表示服务端首次观察到该阶段的时间。

同一策略包含多个账号时，既有 revision 字段继续作为策略汇总，按稳定账号顺序记录最后一个有效上报版本；具体账号的应用结果以事件中的 account_id、account_revision 和 event_id 为准。阶段上报不会成功确认事件，应用完成连接验证和切换后仍按事件上报结果。客户端配置、协议版本和 sync 参数不变，也不增加日志表或数据库迁移。

## 关联与应用处理

同一次改密的 started、completed/failed、updated 使用相同 `operation_id`，每条通知有独立 `event_id`。账号身份和账号版本用于判断是否仍为当前目标；订阅的 revision 是账号版本，不能与策略自身的修订次数混用。事件不携带密码，密码通过签名取密接口取得。

生命周期通知只更新应用状态或业务日志。仅 `credential.updated` 和权威快照触发新版本应用，避免 completed 与 updated 各重建一次连接。事件可重复投递或延迟到达；应用按账号和版本幂等处理，不能确认已经过期的版本。当前订阅没有“等待所有应用确认后才改密”的门禁；应用结果用于观测，A/B 轮换才有切换阶段的确认门禁。

started 是尽力通知，不是预执行确认协议。WebSocket 的 received 仅证明收到消息。要提供“所有必需应用准备完后再改密”，后续应增加独立的 prepare 请求/结果，并明确参与者、能力声明、超时、取消与失败策略；旧客户端不支持时不能默认为已准备。计划通知和 prepare 请求都不能提前使应用用尚未生效的新密码重连。

同账号改密可能存在目标已改密、应用尚未取到新密码的窗口，单靠事件数量不能消除该窗口。本地 Redis ACL 测试使用 resetpass 清除旧密码，而不是同时保留两个密码，确保确实验证了新密码加载。Redis 可配置多密码的事实不能推导 PostgreSQL 也具备相同语义。

## 真实连接验证

[数据库应用 Demo](../python/database_app/README.md) 支持独立的订阅实例。开发适配器真实修改专用 PostgreSQL、Redis 密码，验证新密码认证成功和旧密码新连接认证失败，再通过 Core 原有改密记录/历史信号发布事件。验收同时检查：账号 ID 不变、版本增加、连接 ID 替换、实际读写持续、Core 的 credential.updated 应用结果成功。

这是本地数据库适配器及真实 SDK/PAM Agent/Core 协议联调，不代表运行过 Ansible/Celery 自动改密作业。正常自动改密路径仍需在相应执行环境验收。
