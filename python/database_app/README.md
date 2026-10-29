# PostgreSQL + Redis 实际连接 Demo

一个 Python 应用，两种接入方式：SDK 直接访问 Core；PAM Agent 应用仅访问本机 Unix Socket，不持有 JumpServer 应用密钥。两份应用使用不同实例身份，默认分别运行在 `http://127.0.0.1:8098` 和 `http://127.0.0.1:8099`。

每两秒使用长期连接更新、读取 PostgreSQL 专用计数表，并对 Redis 专用计数器执行 `INCRBY` / `GET`。每个实例只占一行和一个 Redis 键，避免长期运行不断积累记录。页面显示数据库返回的真实身份、账号版本、成功读写次数与切换历史，不显示密码。新凭据先建立连接并验证读写，再替换旧连接并关闭旧会话，最后确认事件。相同账号、版本和密码不会反复重建连接；数据库连接断开后会重新建立。

## 本地准备

前提：Core 已运行、Docker 可用，本机已有可通过容器内 `psql` 管理的 PostgreSQL，以及 Go 编译工具。资源准备只创建新数据库、新角色和新 Redis 容器，不修改原有开发数据库或 `6379` 的 Redis。

以下命令在 **`apps/accounts/clients/python` 目录**执行。准备命令可重复执行，复用运行目录中的配置和账号；不要把包含密码的运行目录提交 Git。

```bash
cd /Users/guang/projects/jumpserver/apps/accounts/clients/python
python3 -m venv /tmp/pam-demo-venv
/tmp/pam-demo-venv/bin/pip install -r database_app/requirements.txt

# .test 已被 Core 仓库忽略；资源名附加随机后缀，防止误用现有资源。
demo_root=/Users/guang/projects/jumpserver/.test/pam-database-demo
/tmp/pam-demo-venv/bin/python -m database_app.prepare --root "$demo_root" \
  --pg-container postgre-postgre-1 --redis-port 6388

# 此步骤使用 Core 自己的 virtualenv、数据库及加密配置。
/Users/guang/projects/jumpserver/.venv/bin/python -m database_app.core \
  --root "$demo_root" --endpoint http://127.0.0.1:8080 provision

/tmp/pam-demo-venv/bin/python -m database_app.run start --root "$demo_root"
curl --fail http://127.0.0.1:8098/health
curl --fail http://127.0.0.1:8099/health
```

已有 Core virtualenv 同时安装了 `psycopg2`、`redis`、SDK 依赖时，可以用它运行 demo，无需另建环境。SDK 从当前源码目录导入，不需要应用侧 `jms_pam_config.py`。

生成文件：`resources.json` 保存专用数据库账号；`manifest.json` 保存 Core 对象 ID；`sdk.json` / `agent-app.json` 是业务应用配置；`agent.json` 是 PAM Agent 配置；`agent/state.json` 保存 PAM Agent 进度。含明文凭据的配置使用 `0600`，目录使用 `0700`。Redis ACL 文件只包含密码摘要，供容器内 Redis 用户读取。新 Redis 使用自己的 AOF volume，A/B ACL 用户仅能操作 `pam-demo:*` 键。

## 凭据切换验证

准备的应用只授权四个专用测试账号，绑定 PostgreSQL、Redis 各一个 A/B 策略。可在 JumpServer 中查看 `PAM Database Demo <随机后缀>` 的连接实例、取密、应用状态和确认记录。

开发验收可以使用下面的受控发布工具：它先真实连接备用账号，再调用 Core 发布切换，SDK / PAM Agent 通过真实 Core API、事件及本地 Socket 完成切换与确认。

```bash
/Users/guang/projects/jumpserver/.venv/bin/python -m database_app.core \
  --root "$demo_root" switch --service postgresql
/Users/guang/projects/jumpserver/.venv/bin/python -m database_app.core \
  --root "$demo_root" switch --service redis

curl --fail http://127.0.0.1:8098/health
curl --fail http://127.0.0.1:8099/health
```

这个工具只针对 manifest 指定的专用策略，停留在等待切换/旧账号观察阶段。它没有执行调度器的备用账号验证任务、跳过旧账号观察期或完成旧密码轮换。完整自动轮换仍须配置目标资产的改密账号和验证任务，通过正常策略周期验收。再次测试切换前，应先完成或取消当前周期。

本地 PostgreSQL 的 `pg_stat_activity` 可核对旧用户连接已经释放；Redis `CLIENT LIST` 可核对应用连接使用新 ACL 用户。先验证页面持续增加的读写次数，再核对 Core 的 applied 状态，不能仅靠已取密或收到了事件判定成功。

自动验收还会尝试错误密码，验证旧业务连接仍可读写且不确认失败版本。命令将不含密码的结果写入 `verification.json`：

```bash
/Users/guang/projects/jumpserver/.venv/bin/python -m database_app.verify --root "$demo_root"
```

需要重复进行开发验收时，使用 Core 正常的取消/恢复机制回到原账号，验证恢复成功后再完成取消。两种服务分别执行，命令不会强制跳过尚未确认的实例：

```bash
core_python=/Users/guang/projects/jumpserver/.venv/bin/python
"$core_python" -m database_app.core --root "$demo_root" revert --service postgresql
"$core_python" -m database_app.core --root "$demo_root" revert --service redis
"$core_python" -m database_app.verify --root "$demo_root"
"$core_python" -m database_app.core --root "$demo_root" finish-revert --service postgresql
"$core_python" -m database_app.core --root "$demo_root" finish-revert --service redis
```

## 同账号密码变更订阅

在已有专用 PG/Redis 资源上增加另一个应用，只授权当前两个测试账号，并建立明确选择账号的订阅策略。原 A/B 配置保持不变。订阅 SDK / PAM Agent 分别使用 `8108` / `8109`，运行配置位于 `demo_root/subscription`。

```bash
# 以下命令仍在 clients/python 目录执行。
core_python=/Users/guang/projects/jumpserver/.venv/bin/python
"$core_python" -m database_app.subscription --root "$demo_root" provision
/tmp/pam-demo-venv/bin/python -m database_app.run start --root "$demo_root/subscription"

"$core_python" -m database_app.subscription --root "$demo_root" snapshot
"$core_python" -m database_app.subscription --root "$demo_root" password --service postgresql
"$core_python" -m database_app.subscription --root "$demo_root" password --service redis
"$core_python" -m database_app.subscription --root "$demo_root" verify
```

这个开发适配器真实改数据库密码，并验证新密码可新建连接、旧密码不能新建连接；Core 的 started、completed、updated 发布走现有改密记录/历史信号。它不执行 Ansible/Celery 作业。密码变更影响专用 demo 的同一个账号，因此使用该账号的 A/B demo 也会加载更新后的密码。Redis ACL 持久化文件同步更新，以便容器重启后继续使用新密码。账号正在进行 A/B 轮换时拒绝这项测试操作。

SDK 页面的 `recent_events` 可以观察三个阶段，共用一个 operation_id。生命周期通知不触发取密，updated 才应用新版本。订阅验收使用事件应用结果，不把轮换的 applied_revision 门禁套用到订阅上。[详细事件契约](../../docs/credential-change-events.md)说明计划通知和预执行准备请求的区别。

`snapshot` 保存改密前的账号版本和数据库连接 ID，防止把旧长连接误认为加载了新密码。`verify` 检查同账号的新版本、新连接、持续读写、应用结果，以及 started → completed → updated 的同周期事件链。快照与结果保存在 `subscription/before-password.json`、`subscription/verification.json`。重复测试时从 snapshot 开始执行即可。

## 停止和更新

```bash
/tmp/pam-demo-venv/bin/python -m database_app.run stop --root "$demo_root"
# 更新源码/SDK 后使用同一运行目录重新启动，配置及实例身份不变。
# 更新 PAM Agent 时，停止进程后从 clients/agent 目录重新编译：
cd /Users/guang/projects/jumpserver/apps/accounts/clients/agent
go build -o "$demo_root/pam-agent" ./cmd/pam-agent
```

停止应用会关闭数据库连接；专用 Redis 和专用 PostgreSQL 数据保留以便继续开发。容器、volume、数据库和角色的名称都在私有 `resources.json` 中，需要清理时只删除其中列出的 demo 资源。
