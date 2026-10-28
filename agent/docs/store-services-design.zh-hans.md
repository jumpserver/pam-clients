# PAM Agent store / services 配置

Core 与 Agent 统一使用协议版本 1 和 `config_version: 1`。完整样例见 [store-services.json](../examples/store-services.json)，接入说明由 [docs/build.py](../../docs/build.py) 统一生成。`store` 与 `services` 独立配置，不与旧结构 `delivery` / `rules` 混用。

迁移配置时核对旧 delivery 输出位置，按需调整业务读取路径；旧版输出文件不在新版 store 的文件记录中，不会自动删除。新版只清理自身记录的标准存储文件，不删除业务配置文件。

## 职责与范围

- 应用授权定义可访问账号的上限。
- store 声明保存为标准本地凭据文件的账号；accounts 使用字符串数组，可以是指定 ID、空数组或单独的 `"all"`。
- services 声明业务如何使用凭据；与 store 独立，两者都可以省略。
- 内部缓存共享凭据获取和版本管理。没有 store 不代表不保存私有运行状态。
- 订阅提供通知，加快更新，不决定本地存储范围；未订阅的账号通过定时同步更新。

同步需求是 store 声明与 services 引用账号的并集，受服务端授权约束。all 包含后续新增授权账号，撤销授权后清理 Agent 管理的标准文件。收到明确撤销通知时，先撤销本地访问并清理对应文件，再尝试刷新；Core 暂时不可达也不能保留已撤销的标准文件。

store 按实际 account_id 生成单独的 `.json` 或 `.env` 文件，A/B 账号也分别保存。`local_api.owner` 控制 Socket 访问及业务输出的默认属主，默认使用 store.owner，再回退至 Agent 运行用户。配置、状态和事件日志继续使用默认私有路径；本机初始化会改为当前用户目录。

## 定时拉取

全局 `pull_interval` 以秒为单位，省略时默认 300，必须为正整数。启动时立即同步，之后按该周期核对账号和凭据版本，仅在版本或当前账号变化时获取凭据；所有 store 和 services 共用此周期。`update_on_notification: false` 的业务在下一次定时同步时更新，连续通知不会推迟定时同步。

## service 行为

每项须有唯一且稳定的 name，用于日志及各业务的版本记录，不要求与 systemd unit 同名。

两个行为开关均放在 service 层：

- `allow_account_switch` 默认 false。true 表示：有明确 A/B 策略关联时跟随当前生效账号，只需声明 A；没有关联时固定使用声明的账号。关联有歧义或授权不足时，该业务失败，不能静默回退到 A。
- `update_on_notification` 默认 true。true 在通知到来、凭据取得后立即执行该业务的更新流程；false 等下一次定时同步，默认周期 300 秒。它不关闭 Agent 通知、store 更新或授权撤销，也不代表手动模式。

启动及定时同步会处理所有有待更新的业务。连续通知不会重置定时同步的截止时间。相同账号可以用于多个业务，各业务独立记录已执行版本，避免即时业务反复执行或定时业务被错误跳过。

## 业务执行步骤

业务按 `update_config` → `reload_service` → `app_check` 执行：更新业务配置、重载或重启服务（也可执行固定脚本）、验证运行中的业务。新字段名需要新版 Agent；`services` 的这三个步骤仅接受新名称，旧字段名会报错。

## 多账号映射

`services.accounts` 用 `id` 指定账号，`username` 可选且只供阅读，不参与选择或渲染。多账号映射使用唯一 alias：

```json
{
  "accounts": [
    {"id": "<database-account-a>", "alias": "database"},
    {"id": "<cache-account>", "alias": "cache"}
  ],
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "database.username",
      "DB_PASSWORD": "database.secret",
      "CACHE_PASSWORD": "cache.secret"
    }
  }
}
```

单账号仍可简写 username / secret。多账号来源必须带 alias，未声明或重复的 alias 报错。A/B 切换后 alias 不变，值来自当前生效账号。所需凭据全部就绪、文件映射校验通过后，再写入并执行一次业务动作。内置映射只修改已有且唯一的顶层字段；嵌套 YAML 等复杂格式使用模板或可信本机脚本。

## 凭据获取、业务生效与轮换确认

store 写入、通知接收或成功取密均不表示业务切换成功。所有引用该轮换的业务都完成当前凭据版本的更新和应用检查，并显式配置 confirm_on_success，才自动确认。延迟业务、失败检查、缺少依赖账号均不能被其他业务的成功掩盖。业务文件与 store 管理目录不能重叠。

定时同步先核对版本元数据，只在凭据版本、当前账号或切换关联变化时取密；同一轮同步按实际账号共用一次获取。实际取密仍正常审计并影响 A/B 停止访问观察，不把后台获取伪装成业务连接成功。正在改密或恢复中的非当前账号暂不可获取，其标准文件也会移除。

## 验证范围

自动测试覆盖旧配置兼容、新结构导出、仅 store、仅 services、all 与指定账号范围、多账号 alias 映射、通知即时与定时更新、连续通知下的定时执行、A/B 共同确认、重启去重、授权撤销及离线撤销清理。Core 测试在独立测试数据库中执行。
