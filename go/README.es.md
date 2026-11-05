# JumpServer PAM Go SDK

<!-- sdk-doc:start -->

## 1. Requisitos y flujo de integración

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Para solo pull basta la primera consulta. Push y A/B necesitan callbacks de negocio y escucha. El SDK gestiona protocolo y resultados; la aplicación cambia conexiones reales. Los paquetes publicados 1.0.2 no incluyen todas las APIs nuevas.

- Go 1.23+ / coder/websocket

## 2. Instalación del SDK e identidad

Install the SDK below and use the identity configuration downloaded from the application access wizard. Protect AK/SK and assign each replica a stable, unique instance ID.

Instale el código del cliente incluido en el mismo Core; los paquetes 1.0.2 no incluyen callbacks gestionados. Sustituya jumpserver.example.com por su Core y conserve el directorio jms-pam-clients, incluida la ruta local de reemplazo de Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/go/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
go mod edit -replace github.com/jumpserver/pam-clients/go=./jms-pam-clients/go &&
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```

### Instalación local alternativa

Sustituya la ruta real.

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

## 3. Obtener la primera credencial

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

## 4. Manejadores de eventos

Implementa el callback de cambio de cuenta e inicia el listener administrado. El SDK gestiona instantáneas, consulta en tiempo real, validación de cuenta y versión, reintentos, reclamación de comandos y resultados. Retorna solo tras verificar la conexión real, cambiar el pool y cerrar conexiones antiguas. Python y Node.js usan subclases, Go usa OnAccountChanged y Java AccountEventListener. El ejemplo falla hasta implementar el cambio real.

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

Los callbacks se ejecutan en serie con una cola limitada; las actualizaciones fallidas se reintentan cada 1–30 segundos. El éxito se informa tras completar el callback. Si falla la subida del resultado, el mismo listener no repite la acción completada. Los comandos requieren una reclamación aceptada y sus acciones fallidas no se repiten automáticamente. Las cuentas aplicadas se reportan automáticamente y el cambio A/B sustituye la cuenta actual. Implementa la limpieza de conexiones revocadas. Informar uso, consultar o entregar archivos no confirma el éxito de la aplicación. Las API de bajo nivel conservan la confirmación explícita. Mantén la idempotencia tras reinicios.

### Ciclo de vida y cierre

`WatchEvents(ctx, handlers)` / `StartEvents(ctx, handlers)`; `Stop()` / `Wait()`; `context.CancelFunc`

WatchEvents espera en la goroutine; StartEvents no garantiza la sincronización inicial. Un listener por cliente. Stop y Close cancelan; espere con Wait desde fuera del manejador. Respete la cancelación del context.

### Credenciales más recientes y fallos del servidor

El SDK consulta primero la API y actualiza el valor retenido sin volver a una versión anterior. Solo timeout, error de red o HTTP 5xx permiten usar un valor previo del mismo selector, marcado como local y sin caducidad temporal. Clones y reinicios del SDK empiezan sin caché. HTTP 401/403/404 o credential_not_found (400) borran valores; 426 y respuestas incorrectas fallan sin respaldo. Una API opcional de metadatos ausente no borra la caché. PAM Agent conserva las cuentas declaradas en store/services; una consulta puntual no declarada no se guarda automáticamente. Las revocaciones explícitas se aplican antes de la actualización HTTP, incluso sin conexión. Los snapshots push no definen todos los permisos de almacenamiento; una reconciliación correcta verifica la autorización actual.

- `account.FromLocal`
- `GetAccountFresh(ctx, accountID)`

Con la escucha gestionada activa, snapshot y credential.secret.updated / credential.switch.published / credential.refresh.requested consultan automáticamente la credencial actual, reemplazan el valor y llaman al manejador. Un fallo de actualización conserva el valor anterior y reintenta. El PAM Agent también consulta tras una notificación y conserva sus datos durante fallos del servidor. Use las llamadas que requieren la API siguientes para actualizar o cambiar conexiones; un valor retenido no es una versión recién obtenida ni confirma rotaciones automáticamente.




## 5. Métodos habituales

### Listar cuentas autorizadas

Los métodos de listado consultan el ámbito pull autorizado actual y devuelven metadatos de cuentas, activos y políticas push, sin obtener contraseñas ni registrar consultas de secretos. Incluyen cuentas sin políticas. La lista completa recorre todas las páginas; el método de página devuelve `count` y `accounts` (Go devuelve lista y total por separado). Los valores predeterminados son limit 200 y offset 0; limit acepta 1–500 y search hasta 128 caracteres para nombre de cuenta, usuario, nombre de activo o dirección. Duplicados, resultados incompletos y cambios del total producen error; repita el listado completo si cambia la autorización. Para obtener un secreto, use el ID de la cuenta. Los ejemplos requieren código SDK con los métodos nuevos; la versión publicada 1.0.2 aún no los ofrece en Python, Java ni Node.js.

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


### Información de aplicación, réplica y políticas

GetApplication devuelve ID, nombre, organización y cantidades de cuentas autorizadas y políticas visibles de la aplicación autenticada. GetClientInfo devuelve solamente registro, tipo y versiones de la réplica actual. La lista devuelve todas las políticas vinculadas visibles, incluidas las desactivadas; search busca nombres o claves. Consulte una política por su ID o clave propia, sin usar claves push de cuentas. La rotación requiere autorización para ambas cuentas y la cuenta activa; los recuentos de suscripción incluyen solo cuentas autorizadas. Las consultas no devuelven secretos ni obtienen o confirman credenciales. El primer acceso registra la réplica. Requieren Core y código SDK actualizados; la versión publicada 1.0.2 aún no incluye estos métodos.

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


### Informe de estado de la aplicación

Informe una instantánea completa del estado y los IDs de las cuentas en uso. Estados: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Una lista vacía borra el uso anterior. Llame al cambiar el estado o desde el heartbeat de la aplicación. Python, Java y Node.js devuelven false ante errores de red/API; Go ofrece `TryReportStatus`. Las variantes estrictas muestran los errores. Los argumentos inválidos se rechazan. Se utiliza el timeout HTTP configurado. El informe no obtiene secretos, confirma rotaciones ni invalida la caché. No incluya secretos ni mensajes de excepción en `error_code`. ClientInfo devuelve el último informe y su fecha.

```go
err := client.ReportAccounts(ctx, []string{account.ID})
// ReportAccounts(ctx, nil) clears the complete explicit usage snapshot.
// ReportStatus(ctx, report) also reports application health.
```


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

## 6. Verificar la integración

1. Consulte una cuenta autorizada y pruebe una conexión real.
2. Verifique sincronización inicial y una actualización; el callback debe terminar antes del éxito.
3. Revise uso/estado y resultado por separado. Consulta e informe de uso no confirman aplicación.

### Solución de problemas

Los fallos HTTP, de red, autenticación y decodificación usan el tipo de error SDK con código y estado HTTP. Cierre flujos y clientes tras usarlos; los clones tienen ciclos de vida independientes. Reintente fallos transitorios en la aplicación y no registre secretos ni cabeceras de autenticación.

`PAMError`



<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# Application integration overview

## Authorization, notifications and rotation

1. **Account authorization** controls which accounts the application may fetch on demand. Authorize the application before installing a client.
2. **Account credential change notifications** is an application-level switch covering all authorized accounts. Use it when applications should react to password changes; no subscription policy is required.
3. **A/B rotation** is a separately bound policy for switching between two accounts before changing the original password. Both accounts must be authorized. Open the policy’s operation guide for its cycle, waiting conditions, cancellation and recovery.

## Choose an integration

Choose by who applies PAM-managed usernames and passwords to the service. Socket API is an application integration through PAM Agent: the application calls a local API while the Agent handles communication with PAM.

| Method | Integration characteristics | Responsibilities |
| --- | --- | --- |
| PAM Agent | Integrate through configuration, templates or adaptation scripts; suitable for existing services. | The Agent retrieves and refreshes the accounts required by the service. Configure field mappings, templates or adaptation scripts to update service configuration, reload or restart the service, and verify that the credentials are in use. |
| SDK | Use the SDK for the application language and call it directly from business code. | The application embeds a language-specific SDK and connects directly to PAM. The SDK handles PAM communication and event processing; the application implements connection validation, credential changes and release of old connections in business callbacks. |
| Socket API | Cross-language integration without embedding a JumpServer SDK; the Agent can be upgraded independently of the application. | The application uses HTTP / WebSocket over the local Agent's Unix Socket to retrieve credentials, receive notifications and report results. The Agent handles PAM authentication, credential refresh and reporting retries; the application validates and switches its own connections. |

Agent automation follows the configured service adaptation and checks. For every method, successful credential retrieval or delivery alone does not prove that the service has switched successfully.

## Setup and acceptance

1. Create an application, authorize accounts and download its private identity configuration.
2. Choose Agent service adaptation, an embedded SDK or the Agent's Socket API according to who will apply credentials. Install the matching components, fetch an account and verify a real connection.
3. For automatic updates, enable application notifications and configure Agent business actions or implement application connection changes. For A/B switching, also bind a rotation policy and enable following its current account.
4. Verify one account update or A/B switch: the application uses the new credential, old connections are released and the business result is successful. Reception, fetching and file delivery alone are insufficient.

## Where to find the details

Agent integration covers deployment and service adaptation: credential files, configuration updates, reload/restart actions and validation. SDK integration covers language-specific installation and business callbacks. Socket API access covers local API calls, credential retrieval, notifications, business results and A/B switching. Events and commands is the shared reference for event payloads, snapshots, receipts and administrator commands. The operation guide inside each rotation policy explains its own cycle, blockers and recovery.

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# Events and commands

This reference applies to all SDK languages and PAM Agent. Prefer managed SDK callbacks or configured Agent services for normal integration; use the raw protocol only when implementing an event consumer yourself.

## Event responsibilities

| Event | Responsibility |
| --- | --- |
| credential.secret.updated / credential.switch.published / credential.refresh.requested and snapshot items | Fetch the named account, verify its version, apply it and report the business result. Managed SDK callbacks handle the protocol. |
| app.scope.updated | Reconcile scope and remove invalid state. Removing a rotation binding does not revoke account authorization. |
| `rotation.*` (such as `rotation.started` and `rotation.completed`), `credential.change.*` (such as `credential.change.started` and `credential.change.completed`) | Update progress, logs or alerts. Do not repeat credential application or confirm business success for these notifications. |
| credential.switch.requested and app.restart.requested | Administrator commands for selected instances: claim, execute and report the command result. |

## Complete event catalog

Current application events, rotation operation commands and connection control messages. Rotation details display progress; each event has one execution responsibility.

### Credential and scope events

credential.secret.updated / credential.switch.published / credential.refresh.requested applies credentials. app.scope.updated and snapshot reconcile scope: detach a missing rotation policy while preserving use of authorized accounts. Only actual account authorization removal disables local access, clears retained credentials and asks the application to release connections.

| Event code | Meaning |
| --- | --- |
| `credential.secret.updated` | The account secret version changed. Fetch and apply the named account; this does not change the selected account. |
| `credential.switch.published` | A target account and policy revision were published for switching, including return to the source account. Fetch and apply the named target; publication is not application success. |
| `credential.refresh.requested` | Fetch and apply the currently published credentials for an authorization grant, manual refresh or snapshot reconciliation. This does not mean the password changed or a rotation started. |
| `app.scope.updated` | Account authorization, notification settings or rotation bindings changed. Use authorized_account_ids to determine account access, then snapshot to reconcile notifications and rotation following. Removing a binding must not close an otherwise authorized account connection. |

### General password-change events

General task lifecycle notifications for progress and alerts. Do not fetch again or report application success for these notifications; apply credentials on credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot.

| Event code | Meaning |
| --- | --- |
| `credential.change.started` | A password-change task is queued or running; the remote password may not have changed yet. |
| `credential.change.completed` | The password-change task succeeded; this is not proof that applications applied the credentials. |
| `credential.change.failed` | The password change failed or could not be verified; inspect the task and remote account. |

### Rotation progress events

These notifications explain cycle progress and outcomes. Application switching follows credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot, not a second action on each progress event.

| Event code | Meaning |
| --- | --- |
| `rotation.verify.started` | Backup-account login verification started. |
| `rotation.verify.skipped` | Backup-account login verification skipped by credential policy. |
| `rotation.verify.failed` | Backup verification failed; no switch is published. |
| `rotation.verify.cancelled` | Backup verification was cancelled. |
| `rotation.started` | The rotation cycle has published the target account and started switching. |
| `rotation.waiting_for_app` | Waiting for required application instances to apply and confirm the target. |
| `rotation.source.waiting` | La observación de la cuenta de origen comienza solo cuando todos confirman u omiten la comprobación de la aplicación y el administrador aprueba los fallos. |
| `rotation.source.ready` | The original account no-fetch window and required client conditions allow password change. |
| `rotation.completed` | The original account password change was verified and the rotation completed. |
| `rotation.failed` | Rotation entered a password-change failure or recovery-required state. |
| `rotation.force_stopped` | An administrator ended the cycle without waiting for remaining recovery confirmations. |

### Manual account rotation operations

credential.switch.requested asks selected instances to apply the account and revision already published by the rotation policy. Administrators can manually apply or retry the switch; the command does not start a new rotation.

| Event code | Meaning |
| --- | --- |
| `credential.switch.requested` | Apply the already published account and version on selected instances; this does not start a rotation. |

### Application operation commands

app.restart.requested restarts the application and verifies its health. Manual account switching belongs to the account rotation operations above.

| Event code | Meaning |
| --- | --- |
| `app.restart.requested` | Execute the supported application restart action and verify health. |

### Connection and receipt messages

SDK and Agent manage liveness and receipts. snapshot and pong need no received receipt; snapshot credential items are applied separately.

| Event code | Meaning |
| --- | --- |
| `snapshot` | Core sends the current notification scope and authorized account IDs on connection, reconnection or scope changes. |
| `ping` | The client sends an application-level keepalive to Core. |
| `pong` | Core replies to an application-level ping. |
| `received` | The client acknowledges reading an event; it does not report business success. |

## Correspondencia de actividades y eventos de protocolo

El registro de actividad contiene acciones de auditoría; el de eventos muestra notificaciones y comandos con su entrega y procesamiento. Las acciones de auditoría y las notificaciones son catálogos distintos. La tabla muestra asociaciones posibles, no una entrega confirmada; consulte los detalles. Recepción, obtención, aplicación, omisión y aprobación se registran por separado. credential_fetched figura en el registro de obtención.

| Tipo de actividad | Código de actividad | Notificaciones / mensajes relacionados | Meaning |
| --- | --- | --- | --- |
| Enviar comando de aplicación | `command_requested` | `credential.switch.requested`<br>`app.restart.requested` | Solicita aplicar credenciales publicadas o reiniciar instancias seleccionadas; no inicia una rotación. |
| Resultado del procesamiento de la aplicación | `command_result` | `credential.secret.updated`<br>`credential.switch.published`<br>`credential.refresh.requested`<br>`credential.switch.requested`<br>`app.restart.requested` | El cliente informa del progreso o resultado de credenciales y comandos. Un fallo de rotación requiere revisión incluso tras un reintento correcto. |
| Crear configuración de integración o rotación | `configuration_created` | `app.scope.updated` | Crea una aplicación o política; un cambio de alcance puede generar app.scope.updated. |
| Modificar configuración de integración o rotación | `configuration_updated` | `app.scope.updated` | Modifica ajustes; un cambio de alcance puede generar app.scope.updated. |
| Eliminar configuración de integración o rotación | `configuration_deleted` | Sin evento de protocolo fijo | Elimina una aplicación o política; no informa del uso de credenciales. |
| Añadir autorización o vínculo de credenciales | `authorization_granted` | `credential.refresh.requested` | Añade autorización o un vínculo de rotación y solicita actualizar; el vínculo no amplía permisos de cuenta. |
| Retirar autorización o vínculo de credenciales | `authorization_revoked` | `app.scope.updated` | Retira autorización o un vínculo. Cierre conexiones de negocio solo si se pierde realmente la autorización de la cuenta. |
| Obtener credenciales | `credential_fetched` | Sin evento de protocolo fijo | Se registra aparte en el registro de obtención; obtenerlas no demuestra su uso. |
| Versión de contraseña de cuenta actualizada | `credential_secret_updated` | `credential.secret.updated` | La versión cambió y debe obtenerse y aplicarse; no cambia la cuenta A/B seleccionada. |
| Publicar cambio de cuenta | `credential_switch_published` | `credential.switch.published` | Publica la cuenta de destino o retorno y la revisión; publicar no demuestra el cambio en la aplicación. |
| Solicitar aplicar credenciales actuales | `credential_refresh_requested` | `credential.refresh.requested` | Solicita obtener y aplicar la versión publicada; no implica cambio de contraseña ni nueva rotación. |
| Volver a publicar la notificación de credenciales | `credential_republished` | `credential.refresh.requested`<br>`snapshot` | La actualización manual o conciliación de snapshot solicita reaplicar las credenciales; la contraseña puede ser la misma. |
| Confirmar nuevas credenciales aplicadas | `credential_confirmed` | Sin evento de protocolo fijo | El cliente confirma la aplicación de la versión actual, por separado de la recepción y obtención. |
| Omitir comprobación de aplicación | `credential_confirmation_skipped` | Sin evento de protocolo fijo | Los otros pasos se completaron y el cliente informa skip para la comprobación opcional; no significa que se verificó correctamente. |
| Continuación aprobada por el administrador | `application_failure_reviewed` | Sin evento de protocolo fijo | El administrador registra un motivo y desbloquea el fallo; se conserva el fallo original. |
| Flujo de eventos de credenciales conectado | `credential_stream_connected` | `snapshot` | El cliente se conecta y concilia el alcance mediante snapshot; no demuestra aplicación correcta. |
| Flujo de eventos de credenciales desconectado | `credential_stream_disconnected` | Sin evento de protocolo fijo | Se desconecta el flujo de notificaciones; no implica por sí solo un fallo de conexión de negocio. |
| Etapa de rotación modificada | `rotation_step` | `rotation.verify.started`<br>`rotation.verify.skipped`<br>`rotation.verify.failed`<br>`rotation.verify.cancelled`<br>`rotation.waiting_for_app`<br>`rotation.source.waiting`<br>`rotation.source.ready`<br>`rotation.completed`<br>`rotation.failed` | Registra verificación, espera de aplicación, observación de la cuenta original y resultados; consulte la etapa en los detalles. |
| Iniciar rotación de cuenta | `rotation_started` | `rotation.started` | Se publica el destino y comienza el cambio de aplicación; la rotación aún no ha terminado. |
| Cancelar o forzar fin de rotación | `rotation_cancelled` | `credential.switch.published`<br>`rotation.force_stopped` | Cancelar espera confirmación de retorno a la cuenta original; forzar el fin omite las esperas de recuperación restantes. |
| Cambio de contraseña de cuenta iniciado | `secret_change_started` | `credential.change.started` | La tarea está en cola o iniciada; la contraseña remota podría no haber cambiado. |
| Cambio de contraseña de cuenta completado | `secret_change_completed` | `credential.change.completed` | La tarea terminó correctamente; no demuestra que las aplicaciones actualizaron las credenciales. |
| Cambio de contraseña de cuenta fallido | `secret_change_failed` | `credential.change.failed` | La tarea falló o el resultado es incierto; revise la tarea y la cuenta remota. |
| Registrar instancia de cliente | `client_registered` | Sin evento de protocolo fijo | Se registra una instancia SDK o Agent; no demuestra aplicación de credenciales. |
| Habilitar instancia de cliente | `client_enabled` | Sin evento de protocolo fijo | Reanuda acceso dentro de los permisos existentes; no concede nuevas autorizaciones. |
| Deshabilitar instancia de cliente | `client_disabled` | Sin evento de protocolo fijo | Se rechazan accesos posteriores; esta acción no cambia contraseñas de cuenta. |
| Entrega de notificación de aplicación | `notification` | Sin evento de protocolo fijo | Registra entregas y reintentos de Webhook. La entrega HTTP no demuestra aplicación de credenciales; el código real figura en los detalles. |
| Restablecer secreto de acceso de aplicación | `application_secret_reset` | Sin evento de protocolo fijo | Restablece el secreto de autenticación con JumpServer, aparte de la contraseña de la cuenta de negocio. |

## Payload fields

| Field | Meaning |
| --- | --- |
| event_id | Event identity for receipts and application results. snapshot itself has no receipt; credential items can have their own event_id. |
| event / occurred_at | Event type and publication time. |
| account_id / account_revision | Concrete target account and password version. A fresh account revision must match before application. |
| credential_key / revision | Policy publication metadata for A/B rotation. Ordinary account notifications use account revisions. Do not mix the two version domains. |
| credential_mode | alternating_rotation for an A/B policy; subscription for application-level account notifications, without a subscription policy. Application-wide events may use null. |
| account_switch.account_ids | Accounts associated with an A/B switch. |
| operation_id | Correlates events belonging to one change operation or rotation cycle. |
| command_id / expires_at | Administrator command identity and execution deadline. Commands have a separate result API. |

## Snapshots and authorization

Core envía snapshot al conectar, reconectar o cambiar el ámbito. credentials incluye todas las cuentas autorizadas si las notificaciones están activadas, junto con las políticas A/B vinculadas, activas y autorizadas. Procese cada elemento como credential.secret.updated / credential.switch.published / credential.refresh.requested con event_id, account_id y account_revision. authorized_account_ids indica la autorización; get_accounts devuelve los metadatos. Desvincular una política no revoca la autorización de cuenta.

```json
{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }]
}
```

## Receipts and business results

El SDK envía automáticamente un recibo received, cuando es posible, antes de entregar un evento de negocio. La aplicación no debe enviarlo de nuevo. El recibo solo indica lectura, no aplicación de credenciales, y no sustituye la confirmación. Concilie snapshots iniciales y de reconexión; snapshot y pong no requieren recibo.

Los resultados son success, failed o skip. Un service local sin app_check informa skip tras completar los demás pasos; no cuenta como confirmación de la aplicación. failed bloquea la siguiente etapa incluso después de un reintento correcto u omitido. Un administrador debe aprobar la continuación con un motivo; se conserva el fallo original.

La aprobación usa una sesión de usuario con accounts.change_applicationcredential. Envíe el failure_id actual y un motivo; se rechazan fallos o revisiones de política obsoletos. La aprobación se audita y conserva el resultado del cliente.

```http
POST /api/v1/accounts/application-credentials/<policy-id>/review-application-failure/
Content-Type: application/json

{"client_id": "<client-id>", "failure_id": "<current-failure-id>", "reason": "<review-reason>"}
```

```json
{"event": "received", "event_id": "<event-id>"}
```

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

## Command execution and results

Administrators select target instances and an expiry time. Use credential.switch.requested to apply the published rotation target, and app.restart.requested for application operations.

1. Poll GET /api/v1/accounts/credential-client/commands/ or receive commands through WebSocket.
2. Claim through POST /api/v1/accounts/credential-client/command-result/ with status: running. Execute only when accepted is true.
3. Report success after actual switching or a successful restart health check; report failed with a safe error code on failure.

Managed SDK callbacks claim and report automatically. received means receipt only. Automatic rotation progress events update progress without repeating command execution.

## Connection liveness

En reposo se envía ping cada 10 segundos; unos 30 segundos sin mensajes provocan reconexión, con espera exponencial de 1–30 segundos y firma nueva. El snapshot restaura el estado actual, sin reproducir eventos históricos.

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Integración con Socket API

La aplicación accede al PAM Agent local sin SDK de JumpServer ni AK/SK en el proceso de negocio. Se utiliza Unix Domain Socket, no TCP. A continuación se incluye la referencia completa del protocolo en inglés.

## 1. Unix Socket / HTTP / WebSocket

The transport is a Unix Domain Socket (AF_UNIX), not TCP. Connect to its filesystem path; no IP address or port is opened. HTTP/JSON requests and the WebSocket event channel share this socket. The localhost URL supplies the HTTP Host only. Agent-to-Core traffic still uses HTTPS/WSS.

## 2. Agent / services

Add a socket service to the Agent configuration. Use the same account declarations and allow_account_switch rules as file delivery. The declared A ID remains the map key while account_id identifies the active A/B account. Socket services cannot also configure update_config, reload_service, credential_check or app_check. restart_supported is an explicit opt-in for business restart commands.

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "orders",
      "delivery": "socket",
      "allow_account_switch": true,
      "restart_supported": false,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ]
    }
  ]
}
```

## 3. Events / A/B

Connect, read the initial snapshot, retrieve the delivery credentials, verify new real connections, replace business connections and release old sessions, then submit success. Receiving a notification or fetching a password never proves that the business has switched. A failed callback submits failed and is retried after the Agent backoff.

Every connection starts with the latest snapshot; this is current-state reconciliation, not historical replay. delivery contains metadata only, including delivery_id, declared-account keys and exact account/policy revisions. Revocations are delivered even during a Core outage; revoked_account_ids requires closing old sessions. With no complete credentials, a delivery may be absent while refresh is pending. Each independently applied business instance needs its own service_id. Only one WebSocket connection per service is accepted.

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {
    "delivery_id": "<delivery-id>",
    "event": "credential.refresh.requested",
    "service_id": "orders",
    "credentials": {
      "<primary-account-id>": {
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }
    },
    "revoked_account_ids": []
  },
  "commands": []
}
```

```json
{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}
```

```json
{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}
```

```json
{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}
```

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution.

## 4. Commands / status

Commands are claimed by the Agent before delegation. Restart commands appear in commands only for services with restart_supported=true. Report command.result after restart and readiness validation; success or failure is persisted and Core reporting is retried without executing the command again. A/B switch commands reuse credential delivery and its business result. Expired commands cannot be acknowledged.

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

Business status is separate from credential confirmation. Use starting, running, degraded, stopping, stopped or error and a short non-secret error_code. Account usage is updated only after business success, and the Agent aggregates all service reports. GET /v1/usage returns the aggregate. The existing POST /v1/usage replaces the instance-wide explicit account declaration; it does not acknowledge events.

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

Las consultas CLI/Socket y la lectura de credenciales de servicios devuelven `account`, `source` y `last_pull_at`. `account.version` es la versión real de las credenciales de la cuenta; los resultados normales omiten versiones de política, `account_revision` y `event_id`. `source` vale `api` para una consulta a Core y `local` para caché. `last_pull_at` es la fecha RFC 3339 de la última consulta correcta de esa cuenta a Core, incluso sin cambio de versión. Se conserva con la caché tras reiniciar. La sincronización de metadatos, las lecturas locales y los fallos no la actualizan. Una caché antigua sin fecha conocida devuelve null. Una consulta bajo demanda no activa la retención sin conexión.

`event_id` permanece en los metadatos de notificación y fuera de account en las notificaciones de scripts para informar del resultado. Los snapshots reutilizan el evento más reciente que coincide con cuenta, versión, política y cliente, o crean uno de republicación. Los scripts leen `credentials[selector].account`. Las plantillas Go, asignaciones de campos y variables EnvironmentFile conservan sus nombres. Los lectores JSON existentes deben adaptarse a la estructura account anidada.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  },
  "source": "local",
  "last_pull_at": "2026-10-30T06:30:00Z"
}
```

Los archivos JSON de credenciales solo contienen `account`, sin `source` ni `last_pull_at`. Las fechas se exportan por ID real de cuenta a `last_pull_at.json`, junto al directorio `store.root`, y se conservan en el estado privado. Una consulta en vivo correcta actualiza inmediatamente el diagnóstico incluso con la misma versión, sin reescribir credenciales. Comprobar versiones, leer la caché o fallar no actualiza la fecha. Las cuentas revocadas se eliminan del diagnóstico.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  }
}
```

`last_pull_at.json`:

```json
{
  "<active-account-id>": "2026-10-30T06:30:00Z"
}
```

| HTTP | API |
| --- | --- |
| GET | `/v1/health`, `/v1/services` |
| GET | `/v1/accounts`, `/v1/usage` |
| GET | `/v1/credential?account_id=<id>&allow_local_fallback=false` |
| GET | `/v1/application`, `/v1/instance`, `/v1/policies?search=...` |
| GET | `/v1/policy?key=<key>` or `/v1/policy?policy_id=<id>` |
| GET | `/v1/services/<service_id>` |
| GET | `/v1/services/<service_id>/credentials?delivery_id=<delivery_id>` |
| POST | `/v1/services/<service_id>/received`, `/v1/services/<service_id>/results` |
| POST | `/v1/services/<service_id>/status` |
| POST | `/v1/services/<service_id>/commands/<command_id>/result` |
| POST | `/v1/usage`, `/v1/confirm` |

WebSocket operations: `ping`, `received`, `accounts`, `credential`, `application`, `instance`, `policies`, `policy`, `credentials`, `result`, `command.result`, `status`, `usage`.
`usage` without `account_ids` reads the aggregate; with `account_ids` (including `[]`) it replaces the instance-wide explicit usage declaration, just like HTTP POST `/v1/usage`.
Every request includes `request_id`; responses contain the same ID and `result` or `error: {code, status}`. Snapshots can arrive between responses. `{"event":"ping"}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \
  http://localhost/v1/services/orders
```

Unix socket ownership is the access boundary. service_id identifies a business consumer, not a separate security identity: processes able to open the same socket share the application authorization. Keep unrelated trust domains on separate Agent instances/sockets. Events omit passwords; retrieve credentials explicitly and never log them. Polling the service snapshot and posting results is supported when a language has no Unix-socket WebSocket client.

## 6. Python

The Python example uses websockets, not a JumpServer SDK. By default it only observes metadata and never acknowledges business success. Implement the Business callbacks and call run(socket_path, service_id, business) for real processing. On a new business process, initialize connections from the current delivery even when it was applied by the previous process. Persist business-side delivery IDs when crash-safe de-duplication is required.

```bash
python3 -m pip install 'websockets>=11'
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/socket/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
"""Unix Socket WebSocket example, without a JumpServer SDK or application keys.

Observe: python python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
For business processing, subclass Business and call run(socket, service, business).
"""
import argparse
import json
import time
from collections import deque
from urllib.parse import urlencode

from websockets.sync.client import unix_connect


class Business:
    def apply_credentials(self, credentials):
        """Verify new real connections, switch pools, then release old sessions."""
        raise NotImplementedError

    def revoke_accounts(self, account_ids):
        """Close sessions and discard credentials for explicitly revoked accounts."""
        raise NotImplementedError

    def restart(self):
        """Restart the business and verify that it is ready before returning."""
        raise NotImplementedError


class Session:
    def __init__(self, connection):
        self.connection = connection
        self.snapshots = deque(maxlen=1)
        self.sequence = 0

    def call(self, operation, **fields):
        self.sequence += 1
        request_id = str(self.sequence)
        self.connection.send(json.dumps(dict(request_id=request_id, operation=operation, **fields)))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("request_id") == request_id:
                if "error" in response:
                    # Return only the protocol code, never credentials or server payloads.
                    raise RuntimeError(response["error"]["code"])
                return response["result"]
            if response.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(response)

    def snapshot(self):
        while not self.snapshots:
            message = json.loads(self.connection.recv())
            if message.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(message)
        return self.snapshots.popleft()


def process(session, snapshot, business, completed, commands):
    delivery = snapshot.get("delivery")
    if delivery and snapshot.get("retry_ready"):
        delivery_id = delivery["delivery_id"]
        session.call("received", delivery_id=delivery_id)
        status = completed.get(delivery_id)
        if status is None:
            try:
                # Cleanup also runs when the credential set is empty.
                revoked = delivery.get("revoked_account_ids", [])
                if revoked:
                    business.revoke_accounts(revoked)
                if delivery["credentials"]:
                    credentials = session.call("credentials", delivery_id=delivery_id)
                    business.apply_credentials(credentials)
                status = "success"
                # Remember before sending the result, so a lost response doesn't
                # replay successful work on the next reconnect in this process.
                completed[delivery_id] = status
            except Exception:
                status = "failed"
        session.call("result", delivery_id=delivery_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    for command in snapshot.get("commands", []):
        if command["status"] != "pending":
            continue
        command_id = command["command_id"]
        status = commands.get(command_id)
        if status is None:
            try:
                business.restart()
                status = "success"
            except Exception:
                status = "failed"
            commands[command_id] = status
        session.call("command.result", command_id=command_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    # Bound process-local de-duplication. Persist delivery IDs in your business
    # transaction if callbacks must survive a crash between effect and ACK.
    for history in (completed, commands):
        while len(history) > 256:
            del history[next(iter(history))]


def run(socket_path, service_id, business=None):
    uri = "ws://localhost/v1/events?" + urlencode({"service_id": service_id})
    completed, commands = {}, {}
    delay = 1
    while True:
        try:
            # AF_UNIX is used here; localhost is the HTTP Host, not a TCP target.
            with unix_connect(socket_path, uri=uri, open_timeout=5, max_size=1024 * 1024) as connection:
                session = Session(connection)
                delay = 1
                initialize = True
                while True:
                    snapshot = session.snapshot()
                    if business is None:
                        delivery = snapshot.get("delivery") or {}
                        print(json.dumps({"service_id": service_id, "event": snapshot["event"],
                                          "delivery_id": delivery.get("delivery_id"),
                                          "result": snapshot.get("result")}), flush=True)
                    else:
                        # A new process must rebuild its own connections even
                        # when a previous process applied this Agent delivery.
                        if initialize and snapshot.get("result") == "success":
                            snapshot["retry_ready"] = True
                        process(session, snapshot, business, completed, commands)
                        initialize = False
        except KeyboardInterrupt:
            return
        except Exception as error:
            # Errors may contain arbitrary business details; print only the type.
            print("Socket connection interrupted:", type(error).__name__, flush=True)
            time.sleep(delay)
            delay = min(delay * 2, 30)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", required=True)
    parser.add_argument("--service", required=True)
    options = parser.parse_args()
    # The executable example observes only. It never claims business success.
    run(options.socket, options.service)
```
<!-- socket-doc:end -->
