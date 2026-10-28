# JumpServer PAM Java SDK

<!-- sdk-doc:start -->

## 1. Requisitos y flujo de integración

1. Cree la aplicación y autorice las cuentas para consulta (pull). Use IDs de cuenta, no nombres ni IDs de política.
2. Vincule una política de notificación o rotación A/B para entrega automática (push).
3. Descargue la identidad desde el asistente, use un `instance_id` estable y único por réplica y proteja AK/SK.

Para solo pull basta la primera consulta. Push y A/B necesitan callbacks de negocio y escucha. El SDK gestiona protocolo y resultados; la aplicación cambia conexiones reales. Los ejemplos reflejan el código actual: use builds Core/SDK compatibles. Los paquetes publicados 1.0.2 no incluyen todas las APIs nuevas.

- JDK 11+ / Maven / Jackson

## 2. Instalación del SDK e identidad

Instale el SDK y configure los valores siguientes. Autorice las cuentas para pull en Administración de aplicaciones; vincule políticas solo si necesita push o rotación. Obtenga AK/SK e ID de organización de los materiales de acceso. Sustituya los ejemplos y proteja los secretos de despliegue. Cada réplica necesita un ID estable y único. Obtenga por ID de cuenta.

Instale el código del cliente incluido en el mismo Core; los paquetes 1.0.2 no incluyen callbacks gestionados. Sustituya jumpserver.example.com por su Core y conserve el directorio jms-pam-clients, incluida la ruta local de reemplazo de Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/java/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
mvn -f ./jms-pam-clients/java/pom.xml install
```

```xml
<dependency>
  <groupId>org.jumpserver</groupId>
  <artifactId>jms-pam</artifactId>
  <version>1.0.2</version>
</dependency>
```

```java
import org.jumpserver.pam.Client;
```

### Instalación local alternativa

Si Core no ofrece el endpoint de código o para una instalación sin conexión, obtenga el código compatible en `/path/to/pam-clients` y ejecute desde el directorio de la aplicación. Sustituya la ruta real.

```bash
mvn -f /path/to/pam-clients/java/pom.xml install
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

`src/main/java/org/jumpserver/pam/Demo.java`

```java
package org.jumpserver.pam;

public final class Demo {
  public static void main(String[] args) {
    Client.Options options =
        new Client.Options(
                System.getenv("JMS_ENDPOINT"),
                System.getenv("JMS_APP_ID"),
                System.getenv("JMS_APP_SECRET"),
                System.getenv("JMS_INSTANCE_ID"))
            .orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Models.Account account =
          client.getAccount(System.getenv("JMS_ACCOUNT_ID"));
      // Pass account.getUsername() / getSecret() to the connection pool.
      System.out.println(
          "Fetched revision "
              + account.getRevision()
              + "; implement application credential switching.");
    }
  }
}
```

```bash
mvn compile dependency:copy-dependencies
java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.Demo
```

## 4. Manejadores de eventos

Implementa el callback de cambio de cuenta e inicia el listener administrado. El SDK gestiona instantáneas, consulta en tiempo real, validación de cuenta y versión, reintentos, reclamación de comandos y resultados. Retorna solo tras verificar la conexión real, cambiar el pool y cerrar conexiones antiguas. Python y Node.js usan subclases, Go usa OnAccountChanged y Java AccountEventListener. El ejemplo falla hasta implementar el cambio real.

`src/main/java/org/jumpserver/pam/EventsDemo.java`

```java
package org.jumpserver.pam;

import org.jumpserver.pam.Models.Account;

public final class EventsDemo {
  public static void main(String[] args) {
    Client.Options options = new Client.Options(System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
        System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID")).orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Thread stop = new Thread(client::close, "jms-pam-shutdown");
      Runtime.getRuntime().addShutdownHook(stop);
      try {
        // SDK handles snapshots, fresh fetches, versions, retries and event results.
        // Pull-only code can use client.reportAccounts(List.of(accountId)); List.of() clears usage.
        client.watchEvents(new AccountEventListener() {
          public void onAccountChanged(Account account) {
            // Validate, switch the pool and release old connections before returning.
            throw new UnsupportedOperationException("Implement application account switching");
          }
          public void onAccountRevoked(String accountId) {
            throw new UnsupportedOperationException("Implement revoked account cleanup");
          }
        });
      } catch (InterruptedException error) {
        Thread.currentThread().interrupt();
      } finally { Runtime.getRuntime().removeShutdownHook(stop); }
    }
  }
}
```

```bash
mvn compile dependency:copy-dependencies
java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.EventsDemo
```

Los callbacks se ejecutan en serie con una cola limitada; las actualizaciones fallidas se reintentan cada 1–30 segundos. El éxito se informa tras completar el callback. Si falla la subida del resultado, el mismo listener no repite la acción completada. Los comandos requieren una reclamación aceptada y sus acciones fallidas no se repiten automáticamente. Las cuentas aplicadas se reportan automáticamente y el cambio A/B sustituye la cuenta actual. Implementa la limpieza de conexiones revocadas. Informar uso, consultar o entregar archivos no confirma el éxito de la aplicación. Las API de bajo nivel conservan la confirmación explícita. Mantén la idempotencia tras reinicios.

### Ciclo de vida y cierre

`watchEvents(listener)` / `startEvents(listener)`; `stop()` / `close()` / `awaitTermination()`

watchEvents espera en el hilo; startEvents no garantiza la sincronización inicial. Un listener por cliente. close espera al manejador; este puede cerrar su propio cliente. Llame awaitTermination desde fuera.

### Credenciales más recientes y fallos del servidor

El SDK consulta primero la API y actualiza el valor retenido sin volver a una versión anterior. Solo timeout, error de red o HTTP 5xx permiten usar un valor previo del mismo selector, marcado como local y sin caducidad temporal. Clones y reinicios del SDK empiezan sin caché. HTTP 401/403/404 o credential_not_found (400) borran valores; 426 y respuestas incorrectas fallan sin respaldo. Una API opcional de metadatos ausente no borra la caché. Agent conserva las cuentas declaradas en store/services; una consulta puntual no declarada no se guarda automáticamente. Las revocaciones explícitas se aplican antes de la actualización HTTP, incluso sin conexión. Los snapshots push no definen todos los permisos de almacenamiento; una reconciliación correcta verifica la autorización actual.

- `account.isFromLocal()`
- `getAccount(accountId, false)`

Con la escucha gestionada activa, snapshot y credential.updated consultan automáticamente la credencial actual, reemplazan el valor y llaman al manejador. Un fallo de actualización conserva el valor anterior y reintenta. El Agent también consulta tras una notificación y conserva sus datos durante fallos del servidor. Use las llamadas que requieren la API siguientes para actualizar o cambiar conexiones; un valor retenido no es una versión recién obtenida ni confirma rotaciones automáticamente.

En reposo se envía ping cada 10 segundos; unos 30 segundos sin mensajes provocan reconexión, con espera exponencial de 1–30 segundos y firma nueva. El snapshot restaura el estado actual, sin reproducir eventos históricos.



## 5. Métodos habituales

### Listar cuentas autorizadas

Los métodos de listado consultan el ámbito pull autorizado actual y devuelven metadatos de cuentas, activos y políticas push, sin obtener contraseñas ni registrar consultas de secretos. Incluyen cuentas sin políticas. La lista completa recorre todas las páginas; el método de página devuelve `count` y `accounts` (Go devuelve lista y total por separado). Los valores predeterminados son limit 200 y offset 0; limit acepta 1–500 y search hasta 128 caracteres para nombre de cuenta, usuario, nombre de activo o dirección. Duplicados, resultados incompletos y cambios del total producen error; repita el listado completo si cambia la autorización. Para obtener un secreto, use el ID de la cuenta. Los ejemplos requieren código SDK con los métodos nuevos; la versión publicada 1.0.2 aún no los ofrece en Python, Java ni Node.js.

```java
package org.jumpserver.pam;

public final class AccountsDemo {
  public static void main(String[] args) {
    Client.Options options =
        new Client.Options(
                System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
                System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID"))
            .orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      for (Models.AuthorizedAccount account : client.listAccounts()) {
        System.out.println(
            account.getId() + " " + account.getUsername() + " " + account.getAsset().getName());
      }
      Models.AccountsPage page = client.listAccountsPage(20, 0, "database");
      System.out.println("Matching accounts: " + page.getCount());
      // Use an authorized account.getId() with getAccount when its secret is needed.
    }
  }
}
```


### Información de aplicación, réplica y políticas

getApplication devuelve ID, nombre, organización y cantidades de cuentas autorizadas y políticas visibles de la aplicación autenticada. getClientInfo devuelve solamente registro, tipo y versiones de la réplica actual. La lista devuelve todas las políticas vinculadas visibles, incluidas las desactivadas; search busca nombres o claves. Consulte una política por su ID o clave propia, sin usar claves push de cuentas. La rotación requiere autorización para ambas cuentas y la cuenta activa; los recuentos de suscripción incluyen solo cuentas autorizadas. Las consultas no devuelven secretos ni obtienen o confirman credenciales. El primer acceso registra la réplica. Requieren Core y código SDK actualizados; la versión publicada 1.0.2 aún no incluye estos métodos.

```java
package org.jumpserver.pam;

import java.util.List;

public final class ApplicationInfoDemo {
  public static void main(String[] args) {
    Client.Options options =
        new Client.Options(
                System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
                System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID"))
            .orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Models.ApplicationInfo application = client.getApplication();
      System.out.println(
          application.getId() + " " + application.getName() + " " + application.getAccountsCount());
      Models.ClientInfo replica = client.getClientInfo();
      System.out.println(replica.getInstanceId() + " " + replica.getClientVersion());
      List<Models.CredentialPolicy> policies = client.listPolicies();
      for (Models.CredentialPolicy policy : policies) {
        System.out.println(
            policy.getKey()
                + " "
                + policy.getName()
                + " "
                + policy.isActive()
                + " "
                + policy.getStatus());
      }
      if (!policies.isEmpty()) {
        Models.CredentialPolicy policy = client.getPolicyById(policies.get(0).getId());
        System.out.println(policy.getName() + " " + policy.getAccountsCount());
      }
    }
  }
}
```


### Informe de estado de la aplicación

Informe una instantánea completa del estado y los IDs de las cuentas en uso. Estados: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Una lista vacía borra el uso anterior. Llame al cambiar el estado o desde el heartbeat de la aplicación. Python, Java y Node.js devuelven false ante errores de red/API; Go ofrece `TryReportStatus`. Las variantes estrictas muestran los errores. Los argumentos inválidos se rechazan. Se utiliza el timeout HTTP configurado. El informe no obtiene secretos, confirma rotaciones ni invalida la caché. No incluya secretos ni mensajes de excepción en `error_code`. ClientInfo devuelve el último informe y su fecha.

```java
boolean accepted = client.reportAccounts(java.util.List.of(account.getId()));
client.reportAccounts(java.util.Collections.emptyList());
```


### Eventos de bajo nivel y comandos

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

Las API con key se conservan por compatibilidad. Use `get_account` y `confirm_event` para nuevas integraciones y actualice también Core.

### API

- `getApplication() / getClientInfo()`
- `reportAccounts(accountIds) / reportStatus(status, accountId) / reportStatus(ApplicationStatus) / reportStatusStrict(report)`
- `listPolicies() / listPolicies(search) / getPolicy(key) / getPolicyById(policyId)`
- `listAccounts() / listAccounts(search)`
- `listAccountsPage(limit, offset, search)`
- `getAccount(accountId)`
- `getAccount(accountId, false)`
- `confirmEvent(eventId, status, errorCode)`
- `watchCredentialEvents()`
- `watchEvents(listener) / startEvents(listener)`
- `EventSubscription.stop() / close() / awaitTermination()`
- `listApplicationCommands()`
- `reportApplicationCommandResult(commandId, status, errorCode)`
- `executeApplicationCommand(event, handler)`
- `syncAgent(credentials, deliveredCredentials, configDigest, syncStatus, syncError)`
- `clone() / close()`

## 6. Verificar la integración

1. Consulte una cuenta autorizada y pruebe una conexión real.
2. Verifique sincronización inicial y una actualización; el callback debe terminar antes del éxito.
3. Revise uso/estado y resultado por separado. Consulta e informe de uso no confirman aplicación.

### Solución de problemas

Los fallos HTTP, de red, autenticación y decodificación usan el tipo de error SDK con código y estado HTTP. Cierre flujos y clientes tras usarlos; los clones tienen ciclos de vida independientes. Reintente fallos transitorios en la aplicación y no registre secretos ni cabeceras de autenticación.

`PAMException`

Todos los SDK usan protocolo versión 1; Agent usa esquema de configuración versión 1. Ante client_upgrade_required (HTTP 426), verifique compatibilidad y actualice. Se toleran campos opcionales y notificaciones desconocidos; no aplique ni confirme políticas no soportadas. Los recibos se envían automáticamente y no prueban la aplicación de credenciales. Los metadatos del cliente pueden incluir server_capabilities. Ausente o null significa desconocido; se toleran nombres nuevos. La lista describe funciones del servidor, no permisos.

<!-- sdk-doc:end -->
