# JumpServer PAM Go SDK

<!-- sdk-doc:start -->

## 1. Условия и порядок подключения

1. Создайте приложение и разрешите получение аккаунтов (pull). Используйте ID аккаунта, а не имя или ID политики.
2. Для автоматической доставки привяжите политику уведомлений или ротации A/B (push).
3. Загрузите настройки в мастере, задайте стабильный уникальный `instance_id` каждой реплике и защитите AK/SK.

Для pull достаточно первого запроса. Для push и A/B реализуйте бизнес-обработчики и слушатель. SDK ведёт протокол и результаты, приложение переключает реальные соединения. Примеры для текущих исходников: нужны совместимые сборки Core и SDK. Опубликованные пакеты 1.0.2 содержат не все новые API.

- Go 1.23+ / coder/websocket

## 2. Установка SDK и настройка идентификации

Установите SDK и задайте параметры ниже. Разрешите аккаунты для pull в управлении приложениями; привязывайте политики только при необходимости push или ротации. Получите AK/SK и ID организации из материалов подключения. Замените шаблонные значения и защитите секреты развёртывания. Каждой реплике нужен стабильный уникальный ID. Получайте по ID учётной записи.

Установите исходный код клиента из той же версии Core; пакеты 1.0.2 не содержат управляемых обработчиков. Замените jumpserver.example.com адресом Core и сохраните каталог jms-pam-clients, включая локальный путь замены модуля Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/go/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
go mod edit -replace github.com/jumpserver/pam-clients/go=./jms-pam-clients/go &&
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```

### Альтернатива: локальные исходники

Если Core не предоставляет API загрузки исходников или установка автономная, подготовьте совместимые исходники в `/path/to/pam-clients` и выполните команды из каталога приложения. Замените путь.

```bash
go mod edit -replace=github.com/jumpserver/pam-clients/go=/path/to/pam-clients/go
go mod tidy
```


```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'
```

## 3. Первое получение учётных данных

`demo.go`

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pam "github.com/jumpserver/pam-clients/go"
)

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint:   os.Getenv("JMS_ENDPOINT"),
		AppID:      os.Getenv("JMS_APP_ID"),
		AppSecret:  os.Getenv("JMS_APP_SECRET"),
		InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID:      os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	account, err := client.GetAccount(context.Background(), os.Getenv("JMS_ACCOUNT_ID"))
	if err != nil {
		log.Fatalf("Credential fetch failed: %T", err)
	}
	// Pass account.Username / Secret to the application connection pool.
	fmt.Printf("Fetched revision %d; implement application credential switching.\n", account.Revision)
}
```

```bash
go run ./demo.go
```

## 4. Обработчики событий

Реализуйте обработчик смены аккаунта и запустите управляемое прослушивание. SDK обрабатывает снимки, получение актуальных данных, проверку аккаунта и версии, повторы, получение права выполнения команды и результаты. Возвращайтесь только после проверки реального подключения, переключения пула и закрытия старых соединений. Python и Node.js используют подклассы, Go — OnAccountChanged, Java — AccountEventListener. Без реализации переключения пример завершается ошибкой.

`events.go`

```go
package main

import (
	"context"
	"errors"
	"fmt"
	pam "github.com/jumpserver/pam-clients/go"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"),
		AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID: os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// SDK handles snapshots, fresh fetches, versions, retries and event results.
	// Applied accounts are reported automatically. Pull-only code can call
	// client.ReportAccounts(ctx, []string{accountID}); an empty list clears usage.
	err = client.WatchEvents(ctx, pam.EventHandlers{
		OnAccountChanged: func(ctx context.Context, account pam.Account) error {
			// Validate, switch the connection pool and release old connections.
			// Return nil only after the application uses this account.
			return fmt.Errorf("implement application account switching")
		},
		OnAccountRevoked: func(ctx context.Context, accountID string) error {
			return fmt.Errorf("implement revoked account cleanup")
		},
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("Event stream failed: %T", err)
	}
}
```

```bash
go run ./events.go
```

Обработчики выполняются последовательно через ограниченную очередь; обычные обновления повторяются с задержкой 1–30 секунд. Успех сообщается только после завершения обработчика. При сбое отправки результата завершённое действие в том же слушателе не повторяется. Команды требуют подтверждённого права выполнения; неудачные действия команд автоматически не повторяются. Применённые аккаунты сообщаются автоматически, при A/B-переключении текущий аккаунт заменяется. Реализуйте закрытие отозванных соединений. Отчёт об использовании, получение пароля и доставка файла сами не подтверждают успех приложения. Старые API сохраняют явное подтверждение. Обеспечьте идемпотентность после перезапуска.

### Жизненный цикл и завершение

`WatchEvents(ctx, handlers)` / `StartEvents(ctx, handlers)`; `Stop()` / `Wait()`; `context.CancelFunc`

WatchEvents ждёт в вызывающей goroutine; StartEvents не гарантирует синхронизацию. Один слушатель на клиент. Stop и Close отменяют работу; вызывайте Wait вне обработчика. Учитывайте отмену context.

### Последние учётные данные и недоступность сервера

Сначала выполняется запрос API. Успешный ответ заменяет сохранённые последние данные; старая версия не заменяет новую, срок истечения по времени отсутствует. Только тайм-аут, сетевой сбой или HTTP 5xx позволяют вернуть последний успешный результат того же селектора с признаком локального источника. Без прежнего значения возвращается исходная ошибка. SDK хранит данные в памяти клиента до обновления, отзыва или закрытия; clone и перезапуск начинают с пустого состояния. Agent использует защищённое локальное состояние. HTTP 401/403/404 очищают данные SDK и вызывают ошибку; некорректный успешный ответ также ошибочен. Отзыв удаляет соответствующие данные, snapshot — данные вне разрешений. Изменение конфигурации сохраняет значения до проверки следующего snapshot. Agent применяет явный отзыв и сокращение области snapshot до HTTP-синхронизации, сохраняет эту область и блокирует соответствующую локальную выдачу даже при сбое сервера или после перезапуска. Ответ credential_not_found (HTTP 400) также удаляет сохранённые значения SDK. HTTP 426 возвращает явную ошибку без удаления сохранённых данных и без подмены ответа кешем. Отсутствие необязательного API метаданных (HTTP 404) не удаляет данные. Agent не сохраняет значения только pull для автономного доступа; SDK используют память по прежним правилам временного сбоя.

- `account.FromLocal`
- `GetAccountFresh(ctx, accountID)`

При управляемой подписке snapshot и credential.updated автоматически получают данные, заменяют локальное значение и вызывают обработчик. Ошибка обновления сохраняет прежнее значение и запускает повтор. Agent также получает данные по уведомлениям и сохраняет их при сбоях сервера. Для обновления и ручного переключения используйте вызовы с обязательным запросом API ниже. Сохранённое значение не считается новой полученной версией и не подтверждает ротацию автоматически.

При простое ping отправляется каждые 10 секунд. Около 30 секунд без сообщений вызывают переподключение с задержкой 1–30 секунд и новой подписью. snapshot восстанавливает текущее состояние без воспроизведения истории.



## 5. Основные методы

### Список разрешённых аккаунтов

Методы списка возвращают текущую область pull-доступа приложения: метаданные аккаунтов, активов и push-политик без чтения паролей и записи получения секретов. Включены аккаунты без политик. Полный список читает все страницы; метод страницы возвращает `count` и `accounts` (Go возвращает список и число отдельно). По умолчанию limit 200, offset 0; limit 1–500, search до 128 символов ищет по имени аккаунта, пользователю, имени актива или адресу. Дубликаты, неполные результаты и изменение числа вызывают ошибку; при изменении доступа повторите полный запрос. Для получения секрета используйте ID аккаунта. Примеры требуют исходный код SDK с новыми методами; опубликованная 1.0.2 ещё не содержит их в Python, Java или Node.js.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pam "github.com/jumpserver/pam-clients/go"
)

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"),
		AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID: os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx := context.Background()
	accounts, err := client.ListAccounts(ctx, "")
	if err != nil {
		log.Fatalf("Account listing failed: %T", err)
	}
	for _, account := range accounts {
		fmt.Println(account.ID, account.Username, account.Asset.Name)
	}
	_, count, err := client.ListAccountsPage(ctx, 20, 0, "database")
	if err != nil {
		log.Fatalf("Account page failed: %T", err)
	}
	fmt.Println("Matching accounts:", count)
	// Use an authorized account.ID with GetAccount when its secret is needed.
}
```


### Информация о приложении, экземпляре и политиках

GetApplication возвращает ID, имя, организацию и число разрешённых аккаунтов и видимых политик подписавшего приложения. GetClientInfo возвращает регистрацию, тип и версии только текущей реплики. Список возвращает все видимые привязанные политики, включая отключённые; search ищет по имени или ключу. Для одной политики используйте ID либо собственный ключ политики, а не push-ключ аккаунта. Ротация требует доступа к обоим аккаунтам и активному аккаунту; число подписок учитывает только разрешённые аккаунты. Запросы не возвращают секреты, не получают и не подтверждают учётные данные. Первый доступ регистрирует реплику. Нужны обновлённые Core и исходный код SDK; в опубликованной 1.0.2 методов пока нет.

```go
package main

import (
	"context"
	"fmt"
	pam "github.com/jumpserver/pam-clients/go"
	"log"
	"os"
)

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"),
		AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID: os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx := context.Background()
	application, err := client.GetApplication(ctx)
	if err != nil {
		log.Fatalf("Application query failed: %T", err)
	}
	fmt.Println(application.ID, application.Name, application.AccountsCount)
	replica, err := client.GetClientInfo(ctx)
	if err != nil {
		log.Fatalf("Client query failed: %T", err)
	}
	fmt.Println(replica.InstanceID, replica.ClientVersion)
	policies, err := client.ListPolicies(ctx, "")
	if err != nil {
		log.Fatalf("Policy listing failed: %T", err)
	}
	for _, policy := range policies {
		fmt.Println(policy.Key, policy.Name, policy.IsActive, policy.Status)
	}
	if len(policies) > 0 {
		policy, err := client.GetPolicy(ctx, pam.PolicySelector{PolicyID: policies[0].ID})
		if err != nil {
			log.Fatalf("Policy query failed: %T", err)
		}
		fmt.Println(policy.Name, policy.AccountsCount)
	}
}
```


### Отчёт о состоянии приложения

Передавайте полный снимок состояния и ID используемых аккаунтов. Состояния: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Пустой список очищает прежние сведения. Вызывайте при изменении состояния или из heartbeat приложения. Python, Java и Node.js возвращают false при ошибках сети/API; Go предоставляет `TryReportStatus`. Строгие варианты возвращают подробности ошибок. Неверные аргументы отклоняются. Используется настроенный HTTP timeout. Отчёт не получает секреты, не подтверждает ротацию и не очищает кеш. Не передавайте секреты и тексты исключений в `error_code`. ClientInfo возвращает последний отчёт и его время.

```go
err := client.ReportAccounts(ctx, []string{account.ID})
// ReportAccounts(ctx, nil) clears the complete explicit usage snapshot.
// ReportStatus(ctx, report) also reports application health.
```


### Низкоуровневые события и команды

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

Старые API key сохранены для совместимости. Новые интеграции используют `get_account` и `confirm_event` и требуют обновления Core.

### API

- `GetApplication(ctx) / GetClientInfo(ctx)`
- `ReportAccounts(ctx, accountIDs) / TryReportStatus(ctx, ApplicationStatus{Status: ..., AccountIDs: ...}) / ReportStatus(ctx, report)`
- `ListPolicies(ctx, search) / GetPolicy(ctx, PolicySelector{Key: ...})`
- `ListAccounts(ctx, search)`
- `ListAccountsPage(ctx, limit, offset, search)`
- `GetAccount(ctx, accountID)`
- `GetAccountFresh(ctx, accountID)`
- `ConfirmEvent(ctx, eventID, status, errorCode)`
- `WatchEvents(ctx, EventHandlers{...}) / StartEvents(ctx, handlers)`
- `EventWatcher.Stop() / EventWatcher.Wait()`
- `WatchCredentialEvents(ctx, handler)`
- `ListApplicationCommands(ctx)`
- `ReportApplicationCommandResult(ctx, commandID, status, errorCode)`
- `ExecuteApplicationCommand(ctx, event, handler)`
- `SyncAgent(ctx, AgentSyncOptions{...})`
- `Clone() / Close()`

## 6. Проверка интеграции

1. Получите разрешённый аккаунт и проверьте реальное соединение.
2. Проверьте начальную синхронизацию и обновление; успех — после завершения обработчика.
3. Отдельно проверьте статус/использование и результат события. Получение и отчёт не подтверждают применение.

### Устранение неполадок

Ошибки HTTP, сети, аутентификации и декодирования представлены типом ошибки SDK с кодом и HTTP-статусом. Закрывайте потоки и клиенты после использования; жизненные циклы копий независимы. Повторяйте временные сбои на уровне приложения и не записывайте секреты или заголовки авторизации.

`PAMError`

Все SDK используют протокол версии 1; конфигурация Agent — схему версии 1. Ответ client_upgrade_required (HTTP 426) требует проверки совместимости и обновления. Неизвестные необязательные поля и уведомления допустимы; неподдерживаемые политики нельзя применять или подтверждать. Квитанции отправляются автоматически и не доказывают применение учётных данных. Метаданные клиента могут содержать server_capabilities. Отсутствие или null означает неизвестность; новые имена допустимы. Список описывает возможности сервера, а не разрешения.

<!-- sdk-doc:end -->
