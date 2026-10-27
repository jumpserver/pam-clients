# JumpServer PAM Python SDK / Agent

Python 3.9+ は認証情報ポリシー SDK を提供します。Go 製 jms-pam-agent は Python 不要で、ローカルファイル、固定アクションまたは Socket で認証情報を配信します。Linux、macOS、Windows でフォアグラウンド実行でき、組み込みの systemd インストールは Linux 専用です。

<!-- agent-doc:start -->

# Go Agent の接続

アプリホストで独立した Go Agent を実行します。Python は不要です。インストール、配信設定、業務処理、検証の順に進めます。

## 1. 接続の前提と流れ

1. アプリを作成し、取得可能なアカウントを許可します（pull）。ユーザー名やポリシー ID ではなくアカウント ID を使います。
2. 自動配信には通知または A/B ローテーションのポリシーを関連付けます（push）。
3. 接続ウィザードで認証設定を取得し、各レプリカに固定で一意の `instance_id` を設定して AK/SK を保護します。

## 2. インストールと起動

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/jms-pam-agent-linux-amd64) · [Linux ARM64](/download/pam/jms-pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# Run in the directory containing the downloaded Agent and SHA256SUMS.
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./jms-pam-agent-linux-${agent_arch}" /usr/local/bin/jms-pam-agent
```

取得した `jms_pam_agent.json` のディレクトリで実行します。Linux サービスには root と systemd が必要です。各レプリカの ID を変更してください。フォアグラウンドには OS 対応バイナリを使用します。macOS / Windows はソースビルドが必要な場合があります。

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id orders-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### フォアグラウンド実行

組み込みインストーラーには Linux と root が必要です。macOS、非 root Linux、Windows ではウィザードで JSON または Socket を選び、init-local と run --local --config を実行してください。初期化は一度だけ行い、生成された非公開のローカル設定を再利用します。フォアグラウンドモードでは systemd 操作を行いません。

バイナリ名を `jms-pam-agent`（Windows は `jms-pam-agent.exe`）にし、Linux/macOS では `chmod 0755 ./jms-pam-agent` を実行します。

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/orders" --instance-id orders-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/orders/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\orders'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id orders-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. 配信方法の選択

JSON はファイル配信、EnvironmentFile は固定した systemd サービス、Unix Socket はローカル API に使用します。Agent は配信成功後に配信リビジョンを記録し、アプリケーションは検証・適用後に生効リビジョンを記録します。Socket はアプリケーションユーザー所有、0600 で、そのユーザーとして操作します。

| 配信方法の選択 | 設定と実行 |
| --- | --- |
| JSON | `delivery.delivery_mode: json` → `/opt/jumpserver-pam/credentials/<credential-key>.json` |
| EnvironmentFile (Linux / systemd) | `delivery.delivery_mode: environment` → `/opt/jumpserver-pam/credentials/<credential-key>.env` |
| Socket | `delivery.delivery_mode: socket` → `/run/jms-pam-agent/agent.sock` |

Linux の設定は `/etc/jms-pam-agent/agent.json`（0600）です。`state_file` は資格情報と進捗、`event_file` はイベントメタデータを保存します。`delivery_root` と `socket_path` は配信先です。前面実行では生成された `agent.json` のパスを使います。

## 4. アプリ設定の更新と A/B ローテーション

ダウンロードした設定には Agent の識別情報と配信設定が含まれます。rules には業務で使うアカウント ID、設定更新、反映操作、稼働中の接続確認を指定します。allow_account_switch を設定したアカウントは A/B の双方向ローテーションに同じルールを使えます。任意の credential_check でファイル更新前に新しいログインを検証できます。状態、イベント、Socket のパスと 300 秒の照合間隔には既定値があります。rules が空なら資格情報ごとに既定ファイルを出力します。

最初の JSON が基本設定です。業務ファイルを更新する場合は空の `rules` を次の配列に置き換えます。パス、サービス、スクリプトは例です。事前にホストで準備してください。通常更新では `allow_account_switch` を省略でき、A/B にはこの設定と実接続の検証が必要です。

| 設定と実行 | アプリ設定の更新と A/B ローテーション |
| --- | --- |
| `credential_check` | 任意：ファイル変更前に新しいログインを検証します。 |
| `config_update` | フィールド、テンプレート、スクリプトで業務ファイルを更新します。 |
| `service_action` | 固定サービスを reload/restart、または固定スクリプトを実行します。 |
| `application_check` | 稼働中の実接続を検証してから適用を確認します。 |

```json
{
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "orders-node-1",
  "delivery": {
    "delivery_mode": "json",
    "delivery_root": "/opt/jumpserver-pam/credentials",
    "app_user": "orders"
  },
  "rules": []
}
```

`rules`:

```json
[
  {
    "accounts": [
      {
        "account_id": "<primary-account-id>",
        "allow_account_switch": true
      }
    ],
    "config_update": {
      "file": "/etc/order-service/config.yml",
      "format": "auto",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "service_action": {
      "unit": "order-service.service",
      "operation": "restart"
    },
    "application_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

### 設定ファイルの形式

`config_update.format` の既定値は `auto` で、省略時も同じです。拡張子 `.txt`、`.env`、`.properties` は `env`（`KEY=value`）、`.yml`、`.yaml` は `yaml`（最上位の `KEY: value`、新しい値は引用符付き文字列）として扱います。他の拡張子では `env` または `yaml` を明示します。対象フィールドは既存で一度だけ出現する必要があり、他の設定は保持されます。INI セクションやネストした YAML は `files` + `template_file` で全体を生成するか、`script` を使用します。環境変数ファイルの更新後はアプリケーションの再読み込みまたは再起動が必要です。

ローカル rules でファイル、JSON/EnvironmentFile または信頼済みテンプレートと、systemd reload/restart や固定実行ファイルを設定します。スクリプトは標準入力で認証情報 JSON を受け取り、固定引数とタイムアウトを使い、適用を検証してから成功を返します。Core は実行パスや権限を拡張できません。設定変更後に Agent を再起動します。

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. ローカル API と確認

### アカウントと資格情報の取得

設定したアプリユーザーで CLI と Socket を使用します。`get_accounts` はメタデータのみ、`get_secret` は pull のみのアカウントも取得できます。パスワードを標準出力するため非公開の端末で実行し、フォアグラウンドでは Socket パスを変更します。

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

### アプリでの適用を確認

実接続の検証と切替後に確認します。`application_check` と `confirm_on_success: true`、または明示確認を使用します。同期チェック内から確認 API を呼ばないでください。結果は永続化され、`confirmed` は Core 受理、`pending` は再試行を示します。

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 接続の検証

1. インスタンスのオンライン状態と許可範囲を確認します。
2. push では関連ポリシーから更新し、配信先のアカウントとバージョンを確認します。
3. A/B では実際の接続切替と旧接続解放後にイベント結果を確認します。取得や配信だけでは適用成功を証明しません。

### トラブルシューティング

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent は起動時、関連イベント受信時、300 秒ごとに同期します。通信障害時は取得済みの最新の許可済み認証情報を保持し、識別情報または権限が拒否されると Socket の取得を停止します。署名付き同期の成功で復旧します。配信済みファイルは保持され、SIGINT/SIGTERM でサービス、接続、読取スレッドを終了します。


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK の接続

## 1. 接続の前提と流れ

1. アプリを作成し、取得可能なアカウントを許可します（pull）。ユーザー名やポリシー ID ではなくアカウント ID を使います。
2. 自動配信には通知または A/B ローテーションのポリシーを関連付けます（push）。
3. 接続ウィザードで認証設定を取得し、各レプリカに固定で一意の `instance_id` を設定して AK/SK を保護します。

pull のみなら最初の取得までで十分です。push や A/B には業務コールバックと監視が必要です。SDK がプロトコルと結果を処理し、業務が実接続を切り替えます。例は現行ソース用です。対応する Core と SDK を用意してください。公開済みの 1.0.2 には新 API がすべて含まれるわけではありません。

## 2. SDK のインストールと認証設定

以下のコマンドで同じ Core に同梱されたクライアントソースをインストールします。公開済み 1.0.2 には管理コールバックがありません。jumpserver.example.com を Core のアドレスに置き換え、Go のローカル置換パスを含む jms-pam-clients ディレクトリを保持してください。

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### ローカルソースからの代替インストール

Core がソース取得 API を提供していない場合やオフラインの場合は、対応ソースを `/path/to/pam-clients` に用意し、アプリのディレクトリで実行します。実際のパスに置き換えてください。

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` はアプリケーションの設定ファイルで、PyPI の `jms-pam` パッケージには含まれません。接続ウィザードからダウンロードしてアプリケーションのスクリプトと同じディレクトリに保存するか、以下の内容で作成し、プレースホルダーを実際の識別情報に置き換えます。`client_options` をコミットしたりログに出力したりしないでください。各レプリカに安定した一意の `instance_id` を設定します。

```python
# jms_pam_config.py
import os

client_options = {
    "endpoint": "https://jumpserver.example.com",
    "app_id": "<application-id>",
    "app_secret": "<application-secret>",
    "org_id": "<org-id>",
}
instance_id = os.environ.get("JMS_INSTANCE_ID") or "orders-node-1"
```

## 3. 最初の資格情報を取得

対話形式でテストする場合は、`jms_pam_config.py` のあるディレクトリで Python を起動して以下の例を実行します。スクリプトの場合は同じディレクトリに `app.py` として保存し、`python3 app.py` を実行します。`ModuleNotFoundError: No module named 'jms_pam_config'` が出たらファイル名と場所を確認してください。再インストールではこのファイルは作成されません。

```bash
cd /path/to/your/application
chmod 0600 jms_pam_config.py
python3
```

<!-- python-account-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


with Client(instance_id=instance_id, **client_options) as client:
    account = client.get_account(account_id="<account-id>")
    username = account.username
    password = account.secret
```
<!-- python-account-example:end -->

## 4. イベントハンドラー

以下のアカウント変更コールバックを実装し、マネージドリスナーを開始します。SDK がスナップショット、最新情報の取得、アカウントと版の検証、再試行、コマンドの実行権取得と結果報告を処理します。実接続の検証、接続プール切替、旧接続の解放を完了してから戻ってください。Python と Node.js はサブクラス、Go は OnAccountChanged、Java は AccountEventListener を使用します。未実装の例は失敗します。

<!-- python-events-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


if not hasattr(Client, 'on_account_changed'):
    raise RuntimeError('Upgrade jms-pam to a version with managed account events')


class ApplicationClient(Client):
    def on_account_changed(self, account):
        # Validate a new connection, switch the pool, then release old connections.
        # Return only after the application uses the account. Never log secrets.
        raise NotImplementedError("Implement application account switching")

    def on_account_revoked(self, account_id):
        # Close the application connections for this account.
        raise NotImplementedError("Implement revoked account cleanup")


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    # SDK handles snapshots, fresh fetches, versions, retries and event results.
    # Applied accounts are reported automatically; pull-only code can use:
    # client.report_accounts([account_id])  # [] clears explicit SDK usage.
    client.watch_events()
```
<!-- python-events-example:end -->

コールバックは有界キューの後で直列実行され、通常更新の失敗は 1–30 秒で再試行します。業務処理が完了してから成功を報告し、結果送信の失敗では同じリスナー内で完了済み動作を繰り返しません。コマンドは実行権取得後に実行し、失敗した動作は自動再実行しません。使用アカウントは自動報告され、A/B 切替で現在値を置き換えます。権限範囲から外れた接続の解放処理を実装してください。使用状況報告や情報取得、ファイル配信だけでは業務成功を確認しません。低レベル API と旧フックは明示的確認を維持します。再起動後の冪等性も必要です。

### 監視のライフサイクルと終了

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events は停止まで待機します。start_events は初回同期の完了を保証しないため、業務の準備を待ってください。クライアントごとに監視は 1 つです。stop_events と close は処理を待ちます。フック内から停止できます。clone はサブクラスの状態を再初期化します。

### 最新の認証情報とバックエンド障害

取得はまず API を呼びます。取得成功時に保持値を更新し、古いバージョンで新しい値を上書きせず、時間による有効期限も設けません。タイムアウト、ネットワーク障害、HTTP 5xx の場合だけ、同じ検索条件の最後に取得した値をローカル由来のフラグ付きで返します。保持値がなければ元のエラーです。SDK は更新・失効・終了までクライアントのメモリに保持し、clone や再起動は空から始まります。Agent は保護された既存のローカル状態に保存します。HTTP 401/403/404 は SDK の保持値を消去して失敗し、不正な成功応答も失敗します。失効は対象を、snapshot は認可範囲外を削除します。設定変更時は次の snapshot で範囲を確認するまで保持します。Agent は HTTP 同期の前に明示的な取り消しと snapshot の範囲縮小を適用して保存し、バックエンド障害中や再起動後も該当するローカル取得を停止します。credential_not_found（HTTP 400）でも SDK の保持値を削除します。 HTTP 426 は保持値を削除せず明示的に失敗し、この要求にキャッシュを返しません。任意のメタデータ API が未提供（HTTP 404）でも保持値を削除しません。Agent の pull 専用値はオフライン用に永続保存しません。SDK は既存の一時障害ルールでメモリ内の保持値を利用できます。

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

管理された監視を有効にすると、snapshot と credential.updated が自動で取得し、保持値を更新してから業務フックを呼びます。更新失敗時は前の値を保持し再試行します。Agent も更新通知で取得し、障害時は既存値を保持します。更新や手動切替には下記の API 取得必須の呼び出しを使用してください。保持値を新しく取得したバージョンと見なしたり、ローテーションを自動確認したりしません。

アイドル時は 10 秒ごとに ping を送り、約 30 秒メッセージがなければ再接続します。待機は 1–30 秒の指数バックオフで毎回再署名します。snapshot は現在の状態を復元し、履歴イベントは再送しません。



## 5. 主なメソッド

### 認可されたアカウントの一覧

一覧メソッドはアプリケーションの現在の pull 認可範囲を取得します。アカウント、資産、関連する push ポリシーのメタデータのみを返し、パスワード取得や取得記録は行いません。ポリシーのないアカウントも含まれます。全件メソッドは全ページを読み取り、ページメソッドは `count` と `accounts` を返します（Go はリストと総数を別々に返します）。既定は limit 200、offset 0、limit は 1–500、search は最大128文字でアカウント名、ユーザー名、資産名、アドレスを検索します。重複、不完全な結果、総数の変化はエラーになります。認可が変わった場合は一覧全体を再取得してください。秘密が必要な場合はアカウント ID で取得メソッドを呼びます。例には新メソッドを含む SDK ソースが必要です。公開済み1.0.2の Python、Java、Node.js にはまだ含まれません。

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id

with Client(instance_id=instance_id, **client_options) as client:
    accounts = client.list_accounts()
    for account in accounts:
        print(account.id, account.username, account.asset.name)

    page = client.list_accounts_page(limit=20, offset=0, search="database")
    print("Matching accounts:", page.count)
    # Use an authorized account.id with get_account when its secret is needed.
```


### アプリケーション、インスタンス、ポリシー情報

get_application は署名したアプリケーションの ID、名前、組織、認可アカウント数と可視ポリシー数を返します。get_client_info は現在のレプリカの登録、種類、バージョンのみを返します。一覧は無効化されたものを含む可視な関連ポリシー全件を返し、search は名前と key を検索します。単一ポリシーは ID またはポリシー自体の key の一方で取得します。アカウントの push key は使用しません。輪換ポリシーには両アカウントと有効アカウントの認可が必要で、購読数は現在認可されたものだけです。秘密やパスワードを返さず、取得や適用確認も行いません。初回アクセスでレプリカを登録します。更新した Core と SDK ソースが必要です。公開済み1.0.2には未収録です。

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id

with Client(instance_id=instance_id, **client_options) as client:
    application = client.get_application()
    print(application.id, application.name, application.accounts_count)
    replica = client.get_client_info()
    print(replica.instance_id, replica.client_version)

    policies = client.list_policies()
    for policy in policies:
        print(policy.key, policy.name, policy.is_active, policy.status)
    if policies:
        policy = client.get_policy(policy_id=policies[0].id)
        # Alternatively: client.get_policy(key=policies[0].key)
        print(policy.name, policy.accounts_count)
```


### アプリケーション状態の報告

実行状態と使用中のアカウント ID を完全なスナップショットとして報告します。状態は `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。空のリストは以前の使用記録を消去します。状態変更時またはアプリ自身のハートビートから呼び出します。Python、Java、Node.js は通信や API の失敗時に false を返し、Go は `TryReportStatus` を提供します。厳格な形式で詳細エラーを取得できます。引数エラーは拒否されます。設定した HTTP タイムアウトを使用します。資格情報の取得、ローテーションの確認、キャッシュの無効化は行いません。`error_code` に秘密や例外メッセージを入れないでください。ClientInfo から最後の報告と時刻を取得できます。

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### 低レベルイベントとアプリコマンド

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

list_application_commands で保留中の要求を取得し、execute_application_command(event, handler) で実行権を申請します。accepted が true の場合だけ処理します。切替処理は要求されたアカウントとリビジョンを検証して適用・確認し、再起動処理はヘルスチェック後に成功を報告します。失敗の報告エラーは元の処理例外を隠しません。

従来の key API は互換用です。新規連携は `get_account` と `confirm_event` を使い、Core も更新してください。

### API

- `list_accounts(search="")` / `list_accounts_page(limit=200, offset=0, search="")`
- `get_application()` / `get_client_info()`
- `report_accounts(account_ids)` / `report_status("running", account_ids=[...])`
- `list_policies(search="")` / `get_policy(key=...)` / `get_policy(policy_id=...)`
- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `watch_events(stop_event=...)` / `start_events(stop_event=...)` / `stop_events()`
- `watch_credential_events(stop_event=...)`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `list_application_commands()` / `execute_application_command(event, handler)`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

## 6. 接続の検証

1. 許可された ID で取得し実接続を検証します。
2. 監視では初回同期とポリシー更新を確認し、業務処理完了後に成功を確認します。
3. 使用状態とイベント結果を別々に確認します。取得や状態報告は適用確認ではありません。

### トラブルシューティング

with または close で HTTP セッションとイベント接続を閉じ、clone で独立したセッションを作成します。HTTP、ネットワーク、認証、解析のエラーは code、status_code、detail、original_error を持つ PAMError になります。一時障害を再試行し、権限拒否を明示的に処理してください。

全 SDK はプロトコル版 1、Agent 設定はスキーマ版 1 を使用します。client_upgrade_required（HTTP 426）を受けたら互換性を確認し更新してください。未知の任意フィールドや通知イベントは許容しますが、未対応ポリシーは適用・確認できません。受領通知は自動送信され、認証情報の適用を証明しません。 クライアント情報の server_capabilities は任意です。欠落または null は不明を表し、未知の名前を許容します。サーバー機能の一覧であり、権限を付与しません。

<!-- sdk-doc:end -->
