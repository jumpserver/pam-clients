# 客户端文档维护

SDK 编程语言为 Python、Go、Java 和 Node.js；cURL 保留为协议调试脚本。文档语言跟随界面语言。

文档覆盖 `en`、`zh-hans`、`zh-hant`、`ja`、`ko`、`pt-br`、`ru`、`vi`、`es`、`fr`。服务端将界面语言别名规范化为文件名，未知语言回退到英文。

- Python 的英文与简体中文 README 保留手写的完整参考说明；其中 `overview-doc`、`agent-doc`、`socket-doc`、`sdk-doc`、`protocol-doc` 标记内的文档中心内容统一由 `build.py` 和翻译文件生成。
- 其他语言的 Python 接入指南、Go/Java/Node.js SDK 指南和 cURL 调试指南从 `translations/*.json` 生成。
- SDK 功能以 Python 为准。四种 SDK 统一按接入前提、安装配置、首次取密、托管事件、接口参考、验收排障分组；PAM Agent 单独按安装、交付方式、业务规则、本地查询与确认分组。SDK 文档不要混入 PAM Agent 安装配置。
- 方法映射、运行命令、配置字段在 `build.py` 中统一维护；原生 SDK 的取密与事件示例直接读取各语言的可编译示例源码，翻译仅修改正文。
- 事件处理、最新凭据保留及重连说明也由 `build.py` 和翻译文件生成；新增钩子示例同时维护 `HOOK_EXAMPLES`，强制实时取密与本地来源标记维护在 `LATEST_CREDENTIAL_APIS`。
- 文档中心读取各目录的 `README.<locale>.md`，不在请求过程中生成或翻译文档。概览、Agent 接入和事件与指令读取 Python 文档的对应标记块；SDK 接入读取所选编程语言的 `sdk-doc` 块。
- 概览负责授权、通知和 A/B 轮换的关系、方式选择与验收顺序；Agent / SDK 章节负责具体接入；完整事件清单、事件字段、快照、心跳、回执和手动指令由 `protocol_guide` 集中维护；清单包含当前应用事件、账号轮换操作、应用运维指令和连接消息，应对照 Core 事件定义核对。轮换详情只保留策略流程、应用要求、阻塞、恢复及事件在轮换中的含义，通过链接进入通用参考，不重复维护事件表或 SDK 示例。
- 活动日志的审计类型、含义及相关协议事件由 `audit-events.json` 和翻译中的 `audit_events` 集中维护。文档表格与 Lina 的活动筛选名称、详情含义共用这份来源；事件日志按实际通知或手动指令的协议代码展示投递、回执与处理结果。审计动作不是协议通知的一一改名，取密、回执、应用结果、跳过和管理员确认需分别说明。
- PAM Agent 接入文档面向部署使用者，采用二进制下载、校验、安装和配置流程。Go 环境准备、源码编译、测试与调试说明只放在仓库根 README 和开发文档中；前台运行仍作为正常部署方式保留。

从仓库根目录生成并检查：

```bash
python3 docs/build.py
python3 docs/build.py --check
```

同步或检查 Lina 中的事件定义和 10 种语言说明时，增加 `--frontend /path/to/lina`；使用 `--check --frontend /path/to/lina` 检查两边是否一致。Lina 的 `auditEventDefinitions.json` 及 `AppAuditType*`、`AppAuditMeaning*` 翻译键为生成内容，不直接修改。

变更 SDK 时同步更新服务端 `credential_client/documentation.py` 的运行环境、示例文件、安装命令及能力标记。应用接入向导复用这里的语言清单和事件示例，并另外生成当前应用的身份配置。新增公共方法须同步四种 SDK、方法映射和协议测试；协议功能和示例统一使用当前接口与格式。

## 协议验证

安装 Node.js SDK 的依赖后，运行共享 HTTP/WebSocket 契约测试。测试服务检查精确请求体摘要、签名头顺序、URL 编码、请求 ID、版本头、事件回执和重新签名，覆盖四种 SDK 的取密、确认、同步、指令与事件生命周期。

```bash
npm ci --prefix node
python3 tests/run.py
```

需要 Go、Java、Maven、Node.js，以及已安装 Python SDK 的 Python 环境；可用 `JMS_MAVEN` 指定 Maven 路径。测试使用本地协议服务，实际应用连接切换和 Core 联调需在目标环境另行验证。

## PAM Agent

独立 PAM Agent 入口为 `agent/cmd/pam-agent`，配置和交付说明位于 `agent/README.*.md`。Python 包只提供 SDK，运行 PAM Agent 不需要 Python。服务名称固定为 `pam-agent`；生成文档不得拼接 configuration ID 作为服务名。PAM Agent 文档通过翻译键 `agent_setup`、`agent_rules`、`native_agent` 等维护。

Socket API 接入独立使用 `socket-doc` 标记块与 `socket/README.<locale>.md`，说明 AF_UNIX HTTP / WebSocket、业务接收与取密阶段、按服务确认、A/B 切换、结果重试和不依赖 JumpServer SDK 的示例。中文与英文提供完整说明，其他语言入口明确展示英文协议参考。
