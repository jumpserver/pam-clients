# JumpServer PAM Python SDK / Agent

Python 3.9+ предоставляет SDK политик учётных данных. Go jms-pam-agent доставляет данные через локальные файлы, фиксированные действия или локальный Socket без Python. Запуск на переднем плане поддерживается в Linux, macOS и Windows; встроенная установка systemd доступна только в Linux.

<!-- agent-doc:start -->

# Подключение Go Agent

Запустите отдельный Go Agent на хосте приложения; Python не требуется. Далее: установка, доставка, бизнес-действия и проверка.

## 1. Условия и порядок подключения

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Download the identity configuration from the application access wizard. Give each replica a stable, unique instance ID and protect the AK/SK.

## 2. Установка и запуск

### Linux / systemd

Core и Agent используют протокол 1 и `config_version: 1`. Настраивайте `store` и `services` независимо, не смешивая со старой структурой `delivery` / `rules`. Core передаёт область доступа и версии, но не локальные исполняемые пути. После изменения частной конфигурации перезапустите Agent.

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

Выполняйте команды из каталога `jms_pam_agent.json`. Службе Linux нужны root и systemd. Измените ID для каждой реплики. Для переднего плана нужен бинарный файл своей ОС; macOS / Windows могут требовать сборки.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Запуск на переднем плане

На macOS, Windows и Linux без root выполните `init-local` один раз, затем используйте частную конфигурацию через `run --local`. Поддерживаются JSON и файлы окружения; systemd требует службы Linux.

Назовите файл `jms-pam-agent` (`jms-pam-agent.exe` в Windows); в Linux/macOS выполните `chmod 0755 ./jms-pam-agent`.

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app" --instance-id app-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id app-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. store / local_api

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "config_version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "app-node-1",
  "pull_interval": 300,
  "local_api": {
    "owner": "appuser"
  },
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "services": []
}
```

`pull_interval` — общий интервал опроса в секундах: положительное целое число, по умолчанию 300. Agent синхронизируется сразу при запуске, затем проверяет версии учётных записей и реквизитов с этим интервалом; секреты запрашиваются только при изменении версии или активной учётной записи. Сервисы с `update_on_notification: false` обновляются при следующем опросе; уведомления не откладывают его.

| Настройка и запуск | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Только явно объявленные ID учётных записей. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

Для каждой записи, включая A и B, создаётся отдельный файл по account_id. Файл содержит открытые учётные данные с правами 0600. Неактивная запись во время смены пароля временно недоступна. Проверка прав удаляет отозванные файлы. `local_api.owner` определяет доступ к Socket и владельца выходных файлов; по умолчанию используется `store.owner`, затем пользователь Agent. Socket не зависит от файлового хранения. При неизменной версии читаются только метаданные; реальные запросы секретов остаются в аудите и учитываются при наблюдении ротации.

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

## 4. Обновление конфигурации и ротация A/B

Добавьте объекты ниже в `services`, задав уникальный стабильный `name`. `allow_account_switch` задаётся на уровне сервиса, по умолчанию false: true следует активной записи однозначной пары A/B; остальные записи фиксированы. Достаточно объявить A. `update_on_notification` также относится к сервису, по умолчанию true. False откладывает действия до плановой сверки, по умолчанию через 300 секунд; хранение и отзыв прав продолжают работать. Каждый сервис хранит свои версии. Ротация подтверждается только после проверки всех связанных сервисов с `app_check.confirm_on_success: true`.

Настройки используют `update_config` → `reload_service` → `app_check`: обновить файл, перезагрузить/перезапустить службу (или выполнить фиксированный скрипт), затем проверить работающее приложение. Требуется обновлённый Agent. Для этих трёх этапов `services` принимает только новые имена; старые имена вызывают ошибку.

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

### Формат конфигурации

`update_config.format` по умолчанию равен `auto`, в том числе при отсутствии поля. Расширения `.txt`, `.env`, `.properties` используют `env` (`KEY=value`), а `.yml`, `.yaml` — `yaml` (`KEY: value` верхнего уровня, новые значения записываются как строки в кавычках). Для других расширений задайте `env` или `yaml`. Каждое поле должно уже существовать ровно один раз; остальные настройки сохраняются. Для секций INI и вложенного YAML используйте `files` + `template_file` для генерации всего файла либо `script`. После обновления файла переменных приложение должно перечитать его или перезапуститься.

Core и Agent используют протокол 1 и `config_version: 1`. Настраивайте `store` и `services` независимо, не смешивая со старой структурой `delivery` / `rules`. Core передаёт область доступа и версии, но не локальные исполняемые пути. После изменения частной конфигурации перезапустите Agent.

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. Локальный API и подтверждение

### Запрос аккаунтов и учётных данных

Выполняйте CLI и Socket-запросы от пользователя приложения. `get_accounts` возвращает метаданные, `get_secret` получает секреты, включая pull-only. Пароль выводится в stdout: используйте приватный терминал. Для локального режима замените путь Socket.

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'

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
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Проверка интеграции

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Устранение неполадок

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent синхронизирует при запуске, по значимым уведомлениям и периодически, по умолчанию каждые 300 секунд. Временные сбои сохраняют разрешённые значения. Явный отзыв или отключение блокирует локальное чтение и удаляет стандартные файлы, даже если обновление не удалось. Файлы приложения остаются под контролем администратора. Уведомления не откладывают плановую сверку. SIGINT/SIGTERM закрывает сервер и чтение событий.


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

SDK сначала обращается к API и обновляет сохранённое значение без отката версии. Только тайм-аут, сетевая ошибка или HTTP 5xx допускают прежнее значение того же селектора с отметкой локального источника и без временного срока. Клоны и перезапуски SDK начинают с пустого кэша. HTTP 401/403/404 или credential_not_found (400) удаляют значения; 426 и неверные успешные ответы завершаются ошибкой без кэша. Отсутствие необязательного API метаданных кэш не очищает. Agent сохраняет записи, объявленные в store/services; необъявленный разовый запрос автоматически не сохраняется. Явный отзыв применяется до HTTP-обновления даже офлайн. Push-снимки не определяют все права хранения; успешная сверка проверяет текущие права приложения.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

При управляемой подписке snapshot и credential.updated автоматически получают данные, заменяют локальное значение и вызывают обработчик. Ошибка обновления сохраняет прежнее значение и запускает повтор. Agent также получает данные по уведомлениям и сохраняет их при сбоях сервера. Для обновления и ручного переключения используйте вызовы с обязательным запросом API ниже. Сохранённое значение не считается новой полученной версией и не подтверждает ротацию автоматически.

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

Все SDK используют протокол версии 1; конфигурация Agent — схему версии 1. Ответ client_upgrade_required (HTTP 426) требует проверки совместимости и обновления. Неизвестные необязательные поля и уведомления допустимы; неподдерживаемые политики нельзя применять или подтверждать. Квитанции отправляются автоматически и не доказывают применение учётных данных. Метаданные клиента могут содержать server_capabilities. Отсутствие или null означает неизвестность; новые имена допустимы. Список описывает возможности сервера, а не разрешения.

<!-- sdk-doc:end -->
