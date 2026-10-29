# 同账号改密的事件边界

## 当前 v1 的事件链

本页说明普通授权账号的同账号改密。通知由应用级“账号凭据变更通知”开关控制，覆盖全部授权账号，无需订阅策略。A/B 轮换在应用已切换到备用账号后修改原主账号密码，其改密生命周期通知仅用于进度和告警，不要求应用切回原主账号。完整事件职责见 [SDK 事件处理接口](../EVENT_HANDLERS.md#当前事件职责)。

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

当前保留已有事件名，避免与 `pre_change_secret`、`post_change_secret`、`secret_updated` 同时发送造成重复取密。小版本添加可选事件或字段；旧客户端忽略不认识的通知。改变已有事件含义、把通知升级为执行门禁，需要新的明确契约。

## PAM Agent 进度与通用取密审计

普通取密仍由现有通用审计记录账号、版本、实例和结果，不要求提供 event_id，也不会因取密就确认业务已生效。PAM Agent 通过现有 `/agent/sync/` 的 `credentials` 上报已持有版本，通过 `delivered_credentials` 上报已交付版本。

Core 根据应用当前授权、有效轮换绑定及本次本机交付范围核对账号版本。陈旧或超前版本不能推进进度；没有持有证据的交付报告不能凭空建立取密进度。旧调用方的 key 取密及交付版本上报按既有兼容规则处理。

首次观察到已持有或已交付当前版本时，更新现有 `CredentialClientStatus` 的相应版本和时间；版本 0 同样有效。重复 sync 不生成取密审计，不重写这些阶段的时间，不把收到、持有或交付解释为应用成功。实际取密请求的准确时间仍以通用审计为准，PAM Agent 的进度时间表示服务端首次观察到该阶段的时间。

应用可授权多个账号；每个账号的应用结果以事件中的 account_id、account_revision 和 event_id 为准。轮换策略 revision 是策略发布版本，不是多个授权账号的汇总。阶段上报不会成功确认事件，应用完成连接验证和切换后仍按事件上报结果。

## 关联与应用处理

同一次改密的 started、completed/failed、updated 使用相同 `operation_id`，每条通知有独立 `event_id`。账号身份和账号版本用于判断是否仍为当前目标；普通账号通知的 revision 是账号版本，不能与轮换策略自身的发布版本混用。事件不携带密码，密码通过签名取密接口取得。

生命周期通知只更新应用状态或业务日志，无需确认应用成功。自动通知中仅 `credential.updated` 和权威快照触发新版本应用，避免 completed 与 updated 各重建一次连接。事件可重复投递或延迟到达；应用按账号和版本幂等处理，不能确认已经过期的版本。普通账号改密没有“等待所有应用确认后才改密”的门禁；应用结果用于观测，A/B 轮换才有切换阶段的确认门禁。

started 是尽力通知，不是预执行确认协议。WebSocket 的 received 仅证明收到消息，不能据此认为所有应用已准备好，也不能提前使用尚未生效的新密码重连。

同账号改密可能存在目标已改密、应用尚未取到新密码的窗口，单靠事件数量不能消除该窗口。本地 Redis ACL 测试使用 resetpass 清除旧密码，而不是同时保留两个密码，确保确实验证了新密码加载。Redis 可配置多密码的事实不能推导 PostgreSQL 也具备相同语义。

## 真实连接验证

[数据库应用 Demo](../python/database_app/README.md) 包含同账号改密验证场景。开发适配器真实修改专用 PostgreSQL、Redis 密码，验证新密码认证成功和旧密码新连接认证失败，再通过 Core 原有改密记录/历史信号发布事件。验收同时检查：账号 ID 不变、版本增加、连接 ID 替换、实际读写持续、Core 的 credential.updated 应用结果成功。

这是本地数据库适配器及真实 SDK/PAM Agent/Core 协议联调，不代表运行过 Ansible/Celery 自动改密作业。正常自动改密路径仍需在相应执行环境验收。
