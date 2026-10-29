# JumpServer PAM Python SDK / PAM Agent

Python 3.9+ предоставляет SDK политик учётных данных. Go pam-agent доставляет данные через локальные файлы, фиксированные действия или локальный Socket без Python. Запуск на переднем плане поддерживается в Linux, macOS и Windows; встроенная установка systemd доступна только в Linux.

<!-- agent-doc:start -->

# Интеграция JumpServer PAM Agent

Запустите отдельный PAM Agent на хосте приложения; Python не требуется. Далее: установка, доставка, бизнес-действия и проверка.

## 1. Условия и порядок подключения

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Скачайте конфигурацию и защитите AK/SK. Поле `instance_id` необязательно.

## 2. Установка и запуск

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

Выполняйте команды из каталога скачанного файла `pam-agent.json`. Команды службы Linux требуют root и systemd. Для запуска на переднем плане используйте исполняемый файл для своей ОС и архитектуры.

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### Запуск на переднем плане

На macOS, Windows и Linux без root выполните `init-local` один раз, затем используйте частную конфигурацию через `run --local`. Поддерживаются JSON и файлы окружения; systemd требует службы Linux.

Назовите файл `pam-agent` (`pam-agent.exe` в Windows); в Linux/macOS выполните `chmod 0755 ./pam-agent`.

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

## 3. Пример конфигурации

Полный пример для Linux/systemd включает идентификатор и ключ приложения, период опроса, стандартные файлы, локальный API и службу приложения. Замените данные приложения, ID записи, системного пользователя, пути, имя службы и скрипт проверки; заранее установите скрипт. Для работы только с файлами или API задайте `services: []`. Следующие разделы объясняют каждый блок одного файла конфигурации.

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

Поле `instance_id` необязательно. Если оно отсутствует или пусто, создаётся `<hostname>-<4 случайных шестнадцатеричных символа>`, например `orders-pod-a1b2`. Явное значение или `--instance-id` имеет приоритет. Установка и `init-local` сохраняют ID в выходной конфигурации. При прямом запуске ID хранится в закрытом файле состояния и повторно используется после перезапуска без изменения входной конфигурации. Реплики K8s могут использовать общую конфигурацию, но каждой нужен отдельный каталог состояния с правом записи. Новая реплика без состояния получает новый ID.

`pull_interval` — общий интервал опроса в секундах: положительное целое число, по умолчанию 300. PAM Agent синхронизируется сразу при запуске, затем проверяет версии учётных записей и реквизитов с этим интервалом; секреты запрашиваются только при изменении версии или активной учётной записи. Сервисы с `update_on_notification: false` обновляются при следующем опросе; уведомления не откладывают его.

## 4. store

`store` сохраняет секреты в стандартных файлах JSON или переменных окружения для приложений, читающих файлы. Задаёт записи, каталог, формат и владельца. Изменение конфигурации приложения и перезагрузка службы настраиваются в `services`.

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

| Настройка и запуск | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Только явно объявленные ID учётных записей. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

Для каждой записи, включая A и B, создаётся отдельный файл по account_id. Файл содержит открытые учётные данные с правами 0600. Неактивная запись во время смены пароля временно недоступна. Проверка прав удаляет отозванные файлы. При неизменной версии читаются только метаданные; реальные запросы секретов остаются в аудите и учитываются при наблюдении ротации.

## 5. socket

`socket` настраивает локальный Unix Socket для проверки состояния, списка записей, получения секретов, отчётов об использовании и подтверждения результатов приложения. Работает без `store`. Доступ определяется правами приложения в Core; `store.accounts` выбирает только файловый вывод и не ограничивает область API.

По умолчанию `path` равен `/run/jms-pam-agent/agent.sock`. `owner` задаёт доступ к Socket и владельца выходных файлов; при отсутствии используются `store.owner`, затем пользователь PAM Agent. Отсутствие блока `socket` не отключает интерфейс. `init-local` использует `<directory>/run/agent.sock` и текущего пользователя.

При запуске без root можно опустить `store.owner` и `socket.owner`: используется текущий пользователь. `init-local` задаёт его для обоих полей. При запуске от root укажите пользователя приложения, если ему нужен доступ к файлам или Socket. `owner` не меняет пользователя процесса PAM Agent.

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### Запрос аккаунтов и учётных данных

Выполняйте CLI и Socket-запросы от пользователя приложения. `get_accounts` возвращает метаданные, `get_secret` получает секреты, включая pull-only. Пароль выводится в stdout: используйте приватный терминал. Для локального режима замените путь Socket.

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

### Подтверждение применения

Подтверждайте после проверки и переключения реального соединения. Используйте `app_check` с `confirm_on_success: true` или явное подтверждение. Не вызывайте API подтверждения из синхронной проверки. Результат сохраняется: `confirmed` принят Core, `pending` повторяется.

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. services

Добавьте объекты ниже в `services`, задав уникальный стабильный `name`. `allow_account_switch` задаётся на уровне сервиса, по умолчанию false: true следует активной записи однозначной пары A/B; остальные записи фиксированы. Достаточно объявить A. `update_on_notification` также относится к сервису, по умолчанию true. False откладывает действия до плановой сверки, по умолчанию через 300 секунд; хранение и отзыв прав продолжают работать. Каждый сервис хранит свои версии. Ротация подтверждается только после проверки всех связанных сервисов с `app_check.confirm_on_success: true`.

Настройки используют `update_config` → `reload_service` → `app_check`: обновить файл, перезагрузить/перезапустить службу (или выполнить фиксированный скрипт), затем проверить работающее приложение. Требуется обновлённый PAM Agent. Для этих трёх этапов `services` принимает только новые имена; старые имена вызывают ошибку.

| Настройка и запуск | Обновление конфигурации и ротация A/B |
| --- | --- |
| `credential_check` | Необязательно: проверьте новый вход до изменения файлов. |
| `update_config` | Обновите файл через поля, шаблон или скрипт. |
| `reload_service` | Выполните reload/restart фиксированной службы или фиксированный скрипт. |
| `app_check` | Проверьте реальное соединение работающего приложения и подтвердите. |

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

В `services.accounts` укажите запись через `id`. Для одной записи допустимы `username` / `secret`. Для нескольких нужны уникальные alias и ссылки вида `database.username`, `cache.secret`. Alias не меняется при переключении A/B. Необязательный `username` — подсказка; реальные значения поступают из Core. Все записи должны быть готовы до изменения файла, затем действие сервиса выполняется один раз. Неоднозначное сопоставление вызывает ошибку до записи.

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

Каждый объект `services` описывает службу приложения. Выберите один способ `update_config`: `file + fields_map` меняет существующие поля, `files + template_file` создаёт полный файл, `script` выполняет собственное обновление. Замените блок службы одним из фрагментов ниже.

#### file + fields_map

`update_config.format` по умолчанию равен `auto`, в том числе при отсутствии поля. Расширения `.txt`, `.env`, `.properties` используют `env` (`KEY=value`), а `.yml`, `.yaml` — `yaml` (`KEY: value` верхнего уровня, новые значения записываются как строки в кавычках). Для других расширений задайте `env` или `yaml`. Каждое поле должно уже существовать ровно один раз; остальные настройки сохраняются. Для секций INI и вложенного YAML используйте `files` + `template_file` для генерации всего файла либо `script`. После обновления файла переменных приложение должно перечитать его или перезапуститься.

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

Шаблоны создают полные файлы, включая вложенный JSON или INI. Укажите `files[].format: template` и абсолютный путь `template_file`. После рендеринга файл атомарно заменяется с правами 0600; по умолчанию `owner` берётся из `socket.owner`. Функция `json` добавляет кавычки и экранирует JSON.

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

Ключи `.Credentials` в шаблоне и `credentials` в скрипте — объявленные `services.accounts[].id`, а не alias. Замените `<primary-account-id>`. При переключении A/B ключ остаётся ID A, а `AccountID` (`account_id` в скрипте) указывает активную запись.

#### script

Объявите один выходной путь через `target` или несколько через `targets` для резервного копирования до изменений. `script.path` — установленный локальный исполняемый файл. `args` содержит фиксированные параметры, секреты поступают JSON через stdin. `timeout_seconds`: 1–300 секунд, по умолчанию 120. Скрипт запускается пользователем PAM Agent. Код 0 означает успех; иной код или тайм-аут вызывает сбой и повтор, поэтому обработка должна быть идемпотентной.

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

Пример стандартного ввода (фрагмент):

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

Пример Python 3 для Linux/macOS обновляет database в существующем JSON, сохраняет другие настройки и владельца и атомарно заменяет файл с правами 0600. Подготовьте каталог и начальный JSON, установите скрипт и разрешите выполнение. Скрипт сам управляет форматом, правами и владельцем.

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

Успех шаблона или скрипта означает только обновление файла. При необходимости настройте `reload_service` и `app_check` в той же службе. Проверьте конфигурацию перед перезапуском PAM Agent:

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


### EnvironmentFile / systemd

EnvironmentFile сохраняет имя, пароль и метаданные открытым текстом `KEY=value` в приватный `.env` (0600). Задайте `EnvironmentFile=` в systemd-службе приложения: значения загружаются в окружение процесса при запуске. Глобальное окружение хоста не меняется. После обновления перезапустите службу; reload не заменяет окружение работающего главного процесса.

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

## 7. Проверка интеграции

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Устранение неполадок

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent синхронизирует при запуске, по значимым уведомлениям и периодически, по умолчанию каждые 300 секунд. Временные сбои сохраняют разрешённые значения. Явный отзыв или отключение блокирует локальное чтение и удаляет стандартные файлы, даже если обновление не удалось. Файлы приложения остаются под контролем администратора. Уведомления не откладывают плановую сверку. SIGINT/SIGTERM закрывает сервер и чтение событий.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Подключение Python SDK

## 1. Условия и порядок подключения

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Для pull достаточно первого запроса. Для push и A/B реализуйте бизнес-обработчики и слушатель. SDK ведёт протокол и результаты, приложение переключает реальные соединения. Примеры для текущих исходников: нужны совместимые сборки Core и SDK. Опубликованные пакеты 1.0.2 содержат не все новые API.

## 2. Установка SDK и настройка идентификации

Установите исходный код клиента из той же версии Core; пакеты 1.0.2 не содержат управляемых обработчиков. Замените jumpserver.example.com адресом Core и сохраните каталог jms-pam-clients, включая локальный путь замены модуля Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### Альтернатива: локальные исходники

Если Core не предоставляет API загрузки исходников или установка автономная, подготовьте совместимые исходники в `/path/to/pam-clients` и выполните команды из каталога приложения. Замените путь.

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` — файл конфигурации приложения, он не входит в пакет `jms-pam` на PyPI. Загрузите его через мастер подключения и сохраните рядом со скриптом приложения либо создайте файл с содержимым ниже и замените значения данными приложения. Не сохраняйте `client_options` в репозитории или журналах. Используйте стабильный уникальный `instance_id` для каждой реплики.

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

## 3. Первое получение учётных данных

Для интерактивного теста запустите Python из каталога с `jms_pam_config.py` и выполните пример ниже. Скрипт можно сохранить как `app.py` в том же каталоге и запустить командой `python3 app.py`. При `ModuleNotFoundError: No module named 'jms_pam_config'` проверьте имя и расположение файла; переустановка пакета не создаёт этот файл.

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

## 4. Обработчики событий

Реализуйте обработчик смены аккаунта и запустите управляемое прослушивание. SDK обрабатывает снимки, получение актуальных данных, проверку аккаунта и версии, повторы, получение права выполнения команды и результаты. Возвращайтесь только после проверки реального подключения, переключения пула и закрытия старых соединений. Python и Node.js используют подклассы, Go — OnAccountChanged, Java — AccountEventListener. Без реализации переключения пример завершается ошибкой.

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

Обработчики выполняются последовательно через ограниченную очередь; обычные обновления повторяются с задержкой 1–30 секунд. Успех сообщается только после завершения обработчика. При сбое отправки результата завершённое действие в том же слушателе не повторяется. Команды требуют подтверждённого права выполнения; неудачные действия команд автоматически не повторяются. Применённые аккаунты сообщаются автоматически, при A/B-переключении текущий аккаунт заменяется. Реализуйте закрытие отозванных соединений. Отчёт об использовании, получение пароля и доставка файла сами не подтверждают успех приложения. Старые API сохраняют явное подтверждение. Обеспечьте идемпотентность после перезапуска.

### Жизненный цикл и завершение

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events ждёт остановки; start_events не гарантирует начальную синхронизацию. Дождитесь готовности приложения. Один слушатель на клиент. stop_events и close ждут обработчик; он может остановить свой клиент. clone заново создаёт состояние подкласса.

### Последние учётные данные и недоступность сервера

SDK сначала обращается к API и обновляет сохранённое значение без отката версии. Только тайм-аут, сетевая ошибка или HTTP 5xx допускают прежнее значение того же селектора с отметкой локального источника и без временного срока. Клоны и перезапуски SDK начинают с пустого кэша. HTTP 401/403/404 или credential_not_found (400) удаляют значения; 426 и неверные успешные ответы завершаются ошибкой без кэша. Отсутствие необязательного API метаданных кэш не очищает. PAM Agent сохраняет записи, объявленные в store/services; необъявленный разовый запрос автоматически не сохраняется. Явный отзыв применяется до HTTP-обновления даже офлайн. Push-снимки не определяют все права хранения; успешная сверка проверяет текущие права приложения.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

При управляемой подписке snapshot и credential.updated автоматически получают данные, заменяют локальное значение и вызывают обработчик. Ошибка обновления сохраняет прежнее значение и запускает повтор. PAM Agent также получает данные по уведомлениям и сохраняет их при сбоях сервера. Для обновления и ручного переключения используйте вызовы с обязательным запросом API ниже. Сохранённое значение не считается новой полученной версией и не подтверждает ротацию автоматически.

При простое ping отправляется каждые 10 секунд. Около 30 секунд без сообщений вызывают переподключение с задержкой 1–30 секунд и новой подписью. snapshot восстанавливает текущее состояние без воспроизведения истории.



## 5. Основные методы

### Список разрешённых аккаунтов

Методы списка возвращают текущую область pull-доступа приложения: метаданные аккаунтов, активов и push-политик без чтения паролей и записи получения секретов. Включены аккаунты без политик. Полный список читает все страницы; метод страницы возвращает `count` и `accounts` (Go возвращает список и число отдельно). По умолчанию limit 200, offset 0; limit 1–500, search до 128 символов ищет по имени аккаунта, пользователю, имени актива или адресу. Дубликаты, неполные результаты и изменение числа вызывают ошибку; при изменении доступа повторите полный запрос. Для получения секрета используйте ID аккаунта. Примеры требуют исходный код SDK с новыми методами; опубликованная 1.0.2 ещё не содержит их в Python, Java или Node.js.

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


### Информация о приложении, экземпляре и политиках

get_application возвращает ID, имя, организацию и число разрешённых аккаунтов и видимых политик подписавшего приложения. get_client_info возвращает регистрацию, тип и версии только текущей реплики. Список возвращает все видимые привязанные политики, включая отключённые; search ищет по имени или ключу. Для одной политики используйте ID либо собственный ключ политики, а не push-ключ аккаунта. Ротация требует доступа к обоим аккаунтам и активному аккаунту; число подписок учитывает только разрешённые аккаунты. Запросы не возвращают секреты, не получают и не подтверждают учётные данные. Первый доступ регистрирует реплику. Нужны обновлённые Core и исходный код SDK; в опубликованной 1.0.2 методов пока нет.

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


### Отчёт о состоянии приложения

Передавайте полный снимок состояния и ID используемых аккаунтов. Состояния: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Пустой список очищает прежние сведения. Вызывайте при изменении состояния или из heartbeat приложения. Python, Java и Node.js возвращают false при ошибках сети/API; Go предоставляет `TryReportStatus`. Строгие варианты возвращают подробности ошибок. Неверные аргументы отклоняются. Используется настроенный HTTP timeout. Отчёт не получает секреты, не подтверждает ротацию и не очищает кеш. Не передавайте секреты и тексты исключений в `error_code`. ClientInfo возвращает последний отчёт и его время.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### Низкоуровневые события и команды

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

Получайте ожидающие команды через list_application_commands и запрашивайте выполнение через execute_application_command(event, handler). Обработчик запускается лишь при accepted: true. Переключение проверяет аккаунт и ревизию, применяет данные и подтверждает их; перезапуск проверяет работоспособность до сообщения об успехе. Ошибка отчёта о неудаче не скрывает исходное исключение.

Старые API key сохранены для совместимости. Новые интеграции используют `get_account` и `confirm_event` и требуют обновления Core.

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

## 6. Проверка интеграции

1. Получите разрешённый аккаунт и проверьте реальное соединение.
2. Проверьте начальную синхронизацию и обновление; успех — после завершения обработчика.
3. Отдельно проверьте статус/использование и результат события. Получение и отчёт не подтверждают применение.

### Устранение неполадок

with или close закрывают HTTP-сеанс и поток событий, clone создаёт независимый сеанс. Ошибки HTTP, сети, аутентификации и разбора вызывают PAMError с code, status_code, detail и original_error. Повторяйте временные ошибки на границе приложения, явно обрабатывайте отказ доступа и не записывайте учётные данные.

Все SDK используют протокол версии 1; конфигурация PAM Agent — схему версии 1. Ответ client_upgrade_required (HTTP 426) требует проверки совместимости и обновления. Неизвестные необязательные поля и уведомления допустимы; неподдерживаемые политики нельзя применять или подтверждать. Квитанции отправляются автоматически и не доказывают применение учётных данных. Метаданные клиента могут содержать server_capabilities. Отсутствие или null означает неизвестность; новые имена допустимы. Список описывает возможности сервера, а не разрешения.

<!-- sdk-doc:end -->
