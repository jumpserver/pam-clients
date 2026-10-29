# JumpServer PAM Python SDK / PAM Agent

Python 3.9+ は認証情報ポリシー SDK を提供します。Go 製 pam-agent は Python 不要で、ローカルファイル、固定アクションまたは Socket で認証情報を配信します。Linux、macOS、Windows でフォアグラウンド実行でき、組み込みの systemd インストールは Linux 専用です。

<!-- agent-doc:start -->

# JumpServer PAM Agent 接続

アプリホストで独立した PAM Agent を実行します。Python は不要です。インストール、配信設定、業務処理、検証の順に進めます。

## 1. 接続の前提と流れ

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. 接続ウィザードから認証設定を取得し、AK/SK を保護します。`instance_id` は省略可能です。

## 2. インストールと起動

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/pam-agent-linux-amd64) · [Linux ARM64](/download/pam/pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# Run in the directory containing the downloaded PAM Agent and SHA256SUMS.
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./pam-agent-linux-${agent_arch}" /usr/local/bin/pam-agent
```

取得した `pam-agent.json` のディレクトリで実行します。以下の Linux サービスコマンドには root と systemd が必要です。フォアグラウンド実行では OS とアーキテクチャに対応する実行ファイルを使用します。

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### フォアグラウンド実行

macOS、Windows、非 root Linux では一度 `init-local` を実行し、その後 `run --local` で私有設定を再利用します。JSON と環境変数ファイルを使用できます。systemd 処理は Linux サービス専用です。

バイナリ名を `pam-agent`（Windows は `pam-agent.exe`）にし、Linux/macOS では `chmod 0755 ./pam-agent` を実行します。

```bash
chmod 0600 ./pam-agent.json
./pam-agent init-local \
  --bootstrap ./pam-agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app"
./pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\pam-agent.exe init-local --bootstrap (Join-Path $PWD 'pam-agent.json') --directory $agentDir
.\pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. 設定例

以下は、アプリケーションの認証情報、取得間隔、標準ファイル、ローカル API、業務サービスを含む Linux/systemd の完全な設定例です。認証情報、アカウント ID、OS ユーザー、パス、サービス名、検証スクリプトを実際の値に置き換え、スクリプトを事前に配置してください。ファイルまたは API のみを使う場合は `services` を `[]` にします。各ブロックは後続の章で説明し、すべて同じ設定ファイルに含めます。

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "sample-app",
      "allow_account_switch": true,
      "update_on_notification": true,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ],
      "update_config": {
        "file": "/etc/sample-app/config.yml",
        "format": "yaml",
        "fields_map": {
          "DB_USER": "username",
          "DB_PASSWORD": "secret"
        }
      },
      "reload_service": {
        "unit": "sample-app.service",
        "operation": "restart"
      },
      "app_check": {
        "path": "/usr/local/libexec/jms-pam/check-running-db",
        "confirm_on_success": true
      }
    }
  ]
}
```

`instance_id` は省略または空にできます。PAM Agent は `<hostname>-<ランダムな16進数4文字>`（例：`orders-pod-a1b2`）を生成します。明示した値または `--instance-id` が優先されます。インストールと `init-local` は ID を出力設定に保存します。直接実行では非公開の状態ファイルに保存し、入力設定を変更せず再起動後も再利用します。K8s の各レプリカは同じ設定を利用できますが、書き込み可能な独立した状態ディレクトリが必要です。状態のない新レプリカは新しい ID を生成します。

`pull_interval` は全体の取得間隔です。単位は秒で、正の整数を指定し、省略時は 300 です。PAM Agent は起動時に直ちに同期し、その後はこの間隔でアカウントと認証情報のバージョンを確認します。バージョンまたは有効なアカウントが変わった場合のみシークレットを取得します。`update_on_notification: false` のサービスは次回の定期取得で更新され、通知によって延期されることはありません。

## 4. store

`store` は、ファイルを読むアプリケーション向けに認証情報を標準 JSON または環境変数ファイルとして保存します。対象アカウント、出力先、形式、所有者を指定します。既存の業務設定の更新やサービスの再読み込みは `services` で設定します。

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  }
}
```

| 設定と実行 | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | 明示したアカウント ID のみ。 |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

実際の account_id ごとに個別ファイルを保存し、A/B も別々です。平文の認証情報を含むファイルの権限は 0600 です。パスワード変更中の非アクティブアカウントは一時利用不可です。権限確認後、取消済みファイルを削除します。変更のない定期同期はメタデータのみ取得し、実際の認証情報取得は監査とローテーション監視に記録します。

## 5. socket

`socket` は、ヘルスチェック、アカウント一覧、認証情報取得、アカウント使用状況の報告、業務結果の確認用のローカル Unix Socket を設定します。`store` なしでも利用できます。取得権限は Core のアプリケーション認可に従い、`store.accounts` はファイル出力だけを選択し、API の取得範囲を制限しません。

`path` の既定値は `/run/jms-pam-agent/agent.sock` です。`owner` は Socket アクセスと業務出力の既定所有者を指定し、省略時は `store.owner`、次に PAM Agent 実行ユーザーを使います。`socket` 全体を省略しても既定設定で動作します。`init-local` は Socket を `<directory>/run/agent.sock`、所有者を現在のユーザーに設定します。

root 以外で実行する場合、`store.owner` と `socket.owner` は省略でき、現在のユーザーが使われます。`init-local` は両方を現在のユーザーに設定します。root で実行し、アプリケーションユーザーにファイルや Socket へのアクセスを許可する場合は、そのユーザーを指定します。`owner` は PAM Agent の実行ユーザーを変更しません。

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### アカウントと資格情報の取得

設定したアプリユーザーで CLI と Socket を使用します。`get_accounts` はメタデータのみ、`get_secret` は pull のみのアカウントも取得できます。パスワードを標準出力するため非公開の端末で実行し、フォアグラウンドでは Socket パスを変更します。

```bash
pam-agent get_accounts
pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credential?account_id=<account-id>'
```

### アプリでの適用を確認

実接続の検証と切替後に確認します。`app_check` と `confirm_on_success: true`、または明示確認を使用します。同期チェック内から確認 API を呼ばないでください。結果は永続化され、`confirmed` は Core 受理、`pending` は再試行を示します。

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. services

以下の業務オブジェクトを `services` に追加し、一意で安定した `name` を指定します。service の `allow_account_switch` は既定 false。true では関連する A/B の現在のアカウントに追従し、関連のないアカウントは固定です。A の宣言だけで十分です。`update_on_notification` も service に置き、既定 true。false では次の定期同期（既定 300 秒）で業務処理します。保存更新と権限取消は継続します。業務ごとに版を記録し、全対象業務の検証成功と `app_check.confirm_on_success: true` が揃って初めて切替を確認します。

設定は `update_config` → `reload_service` → `app_check` の順で、業務設定の更新、サービスの reload/restart（または固定スクリプトの実行）、稼働中のアプリケーションの検証を行います。更新版 PAM Agent が必要です。`services` のこの3つの手順では新しい名前のみを受け付け、旧名はエラーになります。

| 設定と実行 | アプリ設定の更新と A/B ローテーション |
| --- | --- |
| `credential_check` | 任意：ファイル変更前に新しいログインを検証します。 |
| `update_config` | フィールド、テンプレート、スクリプトで業務ファイルを更新します。 |
| `reload_service` | 固定サービスを reload/restart、または固定スクリプトを実行します。 |
| `app_check` | 稼働中の実接続を検証してから適用を確認します。 |

`services`:

```json
[
  {
    "name": "sample-app",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "file": "/etc/sample-app/config.yml",
      "format": "yaml",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    },
    "app_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

### accounts

`services.accounts` では `id` でアカウントを指定します。単一アカウントは `username` / `secret` を直接指定できます。複数では一意の alias と `database.username`、`cache.secret` の形式が必要です。alias は A/B 切替でも不変です。任意の `username` は表示用で、実際の値は Core から取得します。全アカウントの準備後にファイルを更新し、業務処理を一度だけ実行します。未取得や曖昧な対応は書込み前にエラーとなります。

```json
{
  "name": "sample-app",
  "allow_account_switch": true,
  "update_on_notification": true,
  "accounts": [
    {
      "id": "<primary-account-id>",
      "alias": "database"
    },
    {
      "id": "<cache-account-id>",
      "alias": "cache"
    }
  ],
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "database.username",
      "DB_PASSWORD": "database.secret",
      "CACHE_USER": "cache.username",
      "CACHE_PASSWORD": "cache.secret"
    }
  },
  "reload_service": {
    "unit": "sample-app.service",
    "operation": "restart"
  },
  "app_check": {
    "path": "/usr/local/libexec/jms-pam/check-running-db",
    "confirm_on_success": true
  }
}
```

### update_config

`services` の各オブジェクトは業務サービスを表します。`update_config` は、既存項目を編集する `file + fields_map`、ファイル全体を生成する `files + template_file`、独自処理の `script` のいずれか一つを選びます。以下の断片で対象サービスの `update_config` を置き換えます。

#### file + fields_map

`update_config.format` の既定値は `auto` で、省略時も同じです。拡張子 `.txt`、`.env`、`.properties` は `env`（`KEY=value`）、`.yml`、`.yaml` は `yaml`（最上位の `KEY: value`、新しい値は引用符付き文字列）として扱います。他の拡張子では `env` または `yaml` を明示します。対象フィールドは既存で一度だけ出現する必要があり、他の設定は保持されます。INI セクションやネストした YAML は `files` + `template_file` で全体を生成するか、`script` を使用します。環境変数ファイルの更新後はアプリケーションの再読み込みまたは再起動が必要です。

```json
{
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "username",
      "DB_PASSWORD": "secret"
    }
  }
}
```

#### files + template_file

テンプレートはネストした JSON や INI などの完全生成に使います。`files[].format` を `template`、`template_file` をローカル絶対パスにします。描画後、権限 0600 で原子的に置換します。`owner` 省略時は `socket.owner` を使います。`json` 関数は JSON の引用符とエスケープを処理します。

```json
{
  "update_config": {
    "files": [
      {
        "path": "/etc/sample-app/credentials.json",
        "format": "template",
        "template_file": "/etc/pam-agent/templates/credentials.json.tmpl",
        "owner": "appuser"
      }
    ]
  }
}
```

`/etc/pam-agent/templates/credentials.json.tmpl`:

```gotemplate
{
  "database": {
    "username": {{json (index .Credentials "<primary-account-id>").Username}},
    "password": {{json (index .Credentials "<primary-account-id>").Secret}}
  }
}
```

テンプレートの `.Credentials` とスクリプトの `credentials` のキーは宣言した `services.accounts[].id` であり、alias ではありません。`<primary-account-id>` を実際の ID に置き換えます。A/B 切り替え後もキーは A の ID、値の `AccountID`（スクリプトでは `account_id`）は有効なアカウントです。

#### script

出力先は `target` 一つ、または `targets` 複数で宣言し、更新前のバックアップに使います。`script.path` は事前配置した実行ファイルです。`args` は固定引数、認証情報は標準入力の JSON で渡します。`timeout_seconds` は 1–300 秒、既定 120 秒です。PAM Agent のユーザーで実行し、終了コード 0 で成功、非 0 やタイムアウトは失敗・再試行となるため、繰り返し実行可能にします。

```json
{
  "update_config": {
    "target": "/etc/sample-app/credentials.json",
    "script": {
      "path": "/usr/local/libexec/pam-agent/update-credentials",
      "args": [
        "/etc/sample-app/credentials.json"
      ],
      "timeout_seconds": 30
    }
  }
}
```

標準入力の例（抜粋）：

```json
{
  "event": "credentials.updated",
  "credentials": {
    "<primary-account-id>": {
      "account_id": "<active-account-id>",
      "username": "db_app",
      "secret": "<password>",
      "event_id": "<event-id>"
    }
  }
}
```

以下は Linux/macOS の Python 3 例です。既存 JSON の database 項目を更新し、他の設定と所有者を維持して 0600 で原子的に置換します。出力ディレクトリと初期 JSON を用意し、スクリプトを配置して実行権限を与えます。独自スクリプトは出力形式、権限、所有者を管理します。

`/usr/local/libexec/pam-agent/update-credentials`:

```python
#!/usr/bin/env python3
import json
import os
import sys
import tempfile
from pathlib import Path

payload = json.load(sys.stdin)
credential = payload["credentials"]["<primary-account-id>"]
target = Path(sys.argv[1])
original = target.stat()
with target.open(encoding="utf-8") as source:
    config = json.load(source)
config.setdefault("database", {}).update(
    username=credential["username"], password=credential["secret"]
)

fd, temporary = tempfile.mkstemp(prefix=".pam-agent-", dir=target.parent)
try:
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        os.fchmod(output.fileno(), 0o600)
        os.fchown(output.fileno(), original.st_uid, original.st_gid)
        json.dump(config, output, ensure_ascii=False)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, target)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
```

テンプレートやスクリプトの成功は設定更新の完了だけを表します。必要に応じて同じサービスに `reload_service` と `app_check` を設定します。設定確認後に PAM Agent を再起動します：

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


### EnvironmentFile / systemd

EnvironmentFile はユーザー名、パスワードとメタデータを平文の `KEY=value` として非公開の `.env` ファイル（0600）に保存します。業務 systemd サービスに `EnvironmentFile=` を設定すると、起動時にそのプロセス環境へ読み込みます。ホスト全体の環境は変更しません。更新後はサービスを再起動してください。reload だけでは稼働中プロセスの環境は更新されません。

`services`:

```json
[
  {
    "name": "sample-app-env",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "files": [
        {
          "path": "/etc/sample-app/database.env",
          "format": "environment",
          "owner": "appuser"
        }
      ]
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    }
  }
]
```

```ini
JMS_PAM_ACCOUNT_ID="<account-id>"
JMS_PAM_USERNAME="db_user"
JMS_PAM_SECRET="<password>"
```

```ini
[Service]
EnvironmentFile=/etc/sample-app/database.env
```

## 7. 接続の検証

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### トラブルシューティング

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent は起動、関連通知、定期間隔（既定 300 秒）で同期します。一時障害では許可済み値を保持します。明示的取消やアプリ無効化では、更新に失敗してもローカル取得を止め標準保存ファイルを削除します。業務設定ファイルは管理者が管理します。連続通知は定期処理を延期しません。SIGINT/SIGTERM でサーバーと読取処理を終了します。


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK の接続

## 1. 接続の前提と流れ

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
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

SDK はまず API に問い合わせ、成功時に保持値を更新します。古い版には戻りません。同じ選択条件の取得済み値へフォールバックできるのはタイムアウト、ネットワーク障害、HTTP 5xx の場合だけで、ローカル由来と明示します。保持値に時間期限はなく、SDK の clone や再起動時は空になります。HTTP 401/403/404 と credential_not_found（400）は保持値を削除して失敗します。426 や不正な成功応答ではフォールバックせず、任意メタデータ API の欠如は保持値を削除しません。PAM Agent は store/services の宣言対象を保護された状態に永続化し、未宣言の都度取得は自動保存しません。明示的取消はオフラインでも HTTP 更新前に適用します。push スナップショットは保存対象の全権限を表さず、同期成功時に現在のアプリ権限を確認します。

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

管理された監視を有効にすると、snapshot と credential.updated が自動で取得し、保持値を更新してから業務フックを呼びます。更新失敗時は前の値を保持し再試行します。PAM Agent も更新通知で取得し、障害時は既存値を保持します。更新や手動切替には下記の API 取得必須の呼び出しを使用してください。保持値を新しく取得したバージョンと見なしたり、ローテーションを自動確認したりしません。

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

全 SDK はプロトコル版 1、PAM Agent 設定はスキーマ版 1 を使用します。client_upgrade_required（HTTP 426）を受けたら互換性を確認し更新してください。未知の任意フィールドや通知イベントは許容しますが、未対応ポリシーは適用・確認できません。受領通知は自動送信され、認証情報の適用を証明しません。 クライアント情報の server_capabilities は任意です。欠落または null は不明を表し、未知の名前を許容します。サーバー機能の一覧であり、権限を付与しません。

<!-- sdk-doc:end -->
