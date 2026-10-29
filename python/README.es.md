# JumpServer PAM Python SDK / PAM Agent

Python 3.9+ proporciona el SDK de políticas de credenciales. pam-agent en Go entrega credenciales mediante archivos locales, acciones fijas o Socket local sin Python. Se ejecuta en primer plano en Linux, macOS y Windows; la instalación systemd integrada es solo para Linux.

<!-- agent-doc:start -->

# Integración de JumpServer PAM Agent

Ejecute el PAM Agent Go independiente en el host de la aplicación, sin Python. Siga instalación, entrega, acciones de negocio y verificación.

## 1. Requisitos y flujo de integración

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Descargue la configuración de identidad y proteja AK/SK. `instance_id` es opcional.

## 2. Instalación e inicio

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

Ejecute desde el directorio del archivo `pam-agent.json` descargado. Los comandos del servicio Linux requieren root y systemd. Para ejecución en primer plano, use el ejecutable correspondiente al sistema y arquitectura.

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### Ejecución en primer plano

En macOS, Windows o Linux sin root, ejecute `init-local` una vez y reutilice la configuración privada con `run --local`. Admite JSON y archivos de entorno; systemd requiere el servicio Linux.

Guarde el binario como `pam-agent` (`pam-agent.exe` en Windows); en Linux/macOS ejecute `chmod 0755 ./pam-agent`.

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

## 3. Ejemplo de configuración

Este ejemplo completo para Linux/systemd incluye identidad, intervalo de consulta, archivos estándar, API local y un servicio de negocio. Sustituya la identidad, el ID de cuenta, usuario del sistema, rutas, nombre de servicio y script de comprobación; despliegue el script antes de iniciar. Para usar solo archivos o la API, establezca `services` en `[]`. Las secciones siguientes explican cada bloque del mismo archivo de configuración.

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

`instance_id` es opcional. Si falta o está vacío, se genera `<hostname>-<4 caracteres hexadecimales aleatorios>`, por ejemplo `orders-pod-a1b2`. Un valor explícito o `--instance-id` tiene prioridad. La instalación e `init-local` guardan el ID en la configuración generada. La ejecución directa lo guarda en el estado privado y lo reutiliza al reiniciar sin modificar la configuración de entrada. Las réplicas K8s pueden compartir configuración, pero cada una requiere su propio directorio de estado escribible. Una réplica nueva sin estado genera otro ID.

`pull_interval` es el intervalo global de consulta en segundos: un entero positivo, 300 por defecto. El PAM Agent sincroniza al iniciar y después comprueba las versiones de cuentas y credenciales en cada intervalo; solo obtiene secretos si cambia la versión o la cuenta activa. Los servicios con `update_on_notification: false` se actualizan en la siguiente consulta; las notificaciones no la aplazan.

## 4. store

`store` guarda credenciales en archivos JSON o de variables de entorno para aplicaciones que leen archivos. Define cuentas, directorio, formato y propietario. La modificación de configuración de negocio o la recarga de servicios se configura en `services`.

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

| Configuración y ejecución | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Solo los ID de cuentas declarados explícitamente. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

Cada cuenta, incluidas A y B, tiene su archivo por account_id. Las credenciales están en texto claro con permisos 0600. Una cuenta inactiva cuya contraseña está cambiando no está disponible temporalmente. La comprobación de autorización elimina archivos revocados. Sin cambios de versión solo se leen metadatos; las consultas reales siguen auditadas y cuentan para la observación de rotación.

## 5. socket

`socket` configura el Socket Unix local para salud, listado de cuentas, consulta de credenciales, reporte de uso y confirmación de resultados de negocio. Funciona sin `store`. Los permisos dependen de la autorización de la aplicación en Core; `store.accounts` solo selecciona archivos y no limita el alcance de la API.

`path` usa `/run/jms-pam-agent/agent.sock` por defecto. `owner` controla el acceso al Socket y el propietario predeterminado de las salidas de negocio; si se omite, usa `store.owner` y después el usuario PAM Agent. Omitir `socket` mantiene la interfaz predeterminada. `init-local` usa `<directory>/run/agent.sock` y el usuario actual.

Sin root, puede omitir `store.owner` y `socket.owner` para usar el usuario actual; `init-local` configura ambos automáticamente. Con root, indique el usuario de la aplicación si necesita leer los archivos o acceder al Socket. `owner` no cambia el usuario del proceso PAM Agent.

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### Consultar cuentas y credenciales

Use el usuario de la aplicación para CLI y Socket. `get_accounts` lista metadatos; `get_secret` consulta también cuentas solo pull. La contraseña sale por stdout: use un terminal privado. Ajuste el Socket en modo local.

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

### Confirmar la aplicación

Confirme tras validar y cambiar la conexión real. Use `app_check` con `confirm_on_success: true` o confirmación explícita. No llame a la API desde una comprobación síncrona. El resultado se guarda: `confirmed` indica aceptación del Core; `pending` se reintenta.

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. services

Añada los objetos siguientes a `services`, con un `name` único y estable. `allow_account_switch` pertenece al servicio y vale false por defecto: true sigue la cuenta activa de una asociación A/B inequívoca; otras cuentas permanecen fijas. Basta declarar A. `update_on_notification` también pertenece al servicio y vale true por defecto. False aplaza las acciones hasta la siguiente reconciliación, cada 300 segundos por defecto; el almacenamiento y las revocaciones siguen activos. Cada servicio registra sus versiones. Solo se confirma la rotación tras validar todos los servicios implicados con `app_check.confirm_on_success: true`.

Las configuraciones usan `update_config` → `reload_service` → `app_check`: actualizar el archivo, recargar/reiniciar el servicio (o ejecutar un script fijo) y validar la aplicación en ejecución. Se requiere un PAM Agent actualizado. `services` solo acepta estos tres nombres de pasos; los nombres antiguos se rechazan.

| Configuración y ejecución | Actualizar la configuración y rotación A/B |
| --- | --- |
| `credential_check` | Opcional: valide el nuevo inicio antes de cambiar archivos. |
| `update_config` | Actualice el archivo mediante campos, plantilla o script. |
| `reload_service` | Ejecute reload/restart del servicio fijo o un script fijo. |
| `app_check` | Valide la conexión de la aplicación en ejecución y confirme. |

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

En `services.accounts`, use `id` para seleccionar la cuenta. Una cuenta admite `username` / `secret` directamente. Varias cuentas requieren alias únicos y referencias como `database.username` o `cache.secret`. El alias permanece estable al cambiar A/B. El `username` opcional solo es informativo; los valores reales proceden de Core. Todas las cuentas deben estar listas antes de escribir y la acción del servicio se ejecuta una vez. Los errores de asignación se detectan antes de modificar.

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

Cada objeto de `services` representa un servicio. Elija un solo método `update_config`: `file + fields_map` edita campos existentes, `files + template_file` genera archivos completos o `script` ejecuta una actualización personalizada. Sustituya el bloque del servicio por uno de los siguientes fragmentos.

#### file + fields_map

`update_config.format` usa `auto` por defecto, también cuando se omite. Las extensiones `.txt`, `.env` y `.properties` usan `env` (`KEY=value`); `.yml` y `.yaml` usan `yaml` (`KEY: value` de nivel superior, valores escritos como cadenas entre comillas). Para otras extensiones, indique `env` o `yaml`. Cada campo debe existir una sola vez; se conservan los demás ajustes. Para secciones INI o YAML anidado, use `files` + `template_file` para generar todo el archivo, o un `script`. Tras actualizar el archivo de variables, la aplicación debe releerlo o reiniciarse.

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

Las plantillas generan archivos completos, como JSON anidado o INI. Use `files[].format: template` y una ruta absoluta en `template_file`. Tras renderizar, se reemplaza el archivo atómicamente con modo 0600; `owner` usa `socket.owner` por defecto. La función `json` aplica comillas y escapes JSON.

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

Las claves de `.Credentials` en plantillas y `credentials` en scripts son los `services.accounts[].id` declarados, no los alias. Sustituya `<primary-account-id>`. En un cambio A/B, la clave sigue siendo el ID A; `AccountID` (`account_id` en scripts) indica la cuenta activa.

#### script

Declare una salida con `target` o varias con `targets` para respaldarlas antes de modificar. Despliegue `script.path` como ejecutable local. `args` contiene parámetros fijos; las credenciales llegan en JSON por stdin. `timeout_seconds` admite 1–300 segundos, por defecto 120. Se ejecuta como el usuario PAM Agent. El código 0 indica éxito; otro código o un tiempo agotado causa fallo y reintento, por lo que debe ser idempotente.

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

Ejemplo de entrada estándar (extracto):

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

Este ejemplo Python 3 para Linux/macOS actualiza database en un JSON existente, conserva otras opciones y el propietario y reemplaza atómicamente con modo 0600. Prepare el directorio y JSON inicial, despliegue el script y conceda ejecución. El script gestiona formato, permisos y propietario.

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

El éxito de plantilla o script solo completa la actualización del archivo. Configure `reload_service` y `app_check` en el mismo servicio cuando sea necesario. Valide antes de reiniciar PAM Agent:

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


### EnvironmentFile / systemd

EnvironmentFile guarda usuario, contraseña y metadatos como texto plano `KEY=value` en un `.env` privado (0600). Configure `EnvironmentFile=` en el servicio systemd de la aplicación para cargarlo al entorno del proceso al iniciar. No cambia variables globales del host. Reinicie tras actualizar; reload no sustituye el entorno del proceso principal en ejecución.

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

## 7. Verificar la integración

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Solución de problemas

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent sincroniza al iniciar, con notificaciones relevantes y periódicamente, cada 300 segundos por defecto. Una interrupción temporal conserva los valores autorizados. Una revocación explícita o desactivación bloquea la lectura local y elimina archivos estándar aunque falle la actualización. Los archivos de negocio quedan bajo control del administrador. Las notificaciones no aplazan la reconciliación periódica. SIGINT/SIGTERM cierra el servidor y el lector de eventos.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Integración del SDK de Python

## 1. Requisitos y flujo de integración

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Para solo pull basta la primera consulta. Push y A/B necesitan callbacks de negocio y escucha. El SDK gestiona protocolo y resultados; la aplicación cambia conexiones reales. Los ejemplos reflejan el código actual: use builds Core/SDK compatibles. Los paquetes publicados 1.0.2 no incluyen todas las APIs nuevas.

## 2. Instalación del SDK e identidad

Instale el código del cliente incluido en el mismo Core; los paquetes 1.0.2 no incluyen callbacks gestionados. Sustituya jumpserver.example.com por su Core y conserve el directorio jms-pam-clients, incluida la ruta local de reemplazo de Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### Instalación local alternativa

Si Core no ofrece el endpoint de código o para una instalación sin conexión, obtenga el código compatible en `/path/to/pam-clients` y ejecute desde el directorio de la aplicación. Sustituya la ruta real.

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` es un archivo de configuración de la aplicación y no forma parte del paquete `jms-pam` de PyPI. Descárguelo desde el asistente de integración y guárdelo junto al script de la aplicación, o créelo con el contenido siguiente y sustituya los valores por la identidad real de la aplicación. No guarde `client_options` en el repositorio ni en registros. Use un `instance_id` estable y único por réplica.

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

## 3. Obtener la primera credencial

Para probar de forma interactiva, inicie Python en el directorio de `jms_pam_config.py` y ejecute el ejemplo siguiente. Para un script, guárdelo como `app.py` en el mismo directorio y ejecute `python3 app.py`. Si aparece `ModuleNotFoundError: No module named 'jms_pam_config'`, revise el nombre y la ubicación del archivo; reinstalar el paquete no lo crea.

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

## 4. Manejadores de eventos

Implementa el callback de cambio de cuenta e inicia el listener administrado. El SDK gestiona instantáneas, consulta en tiempo real, validación de cuenta y versión, reintentos, reclamación de comandos y resultados. Retorna solo tras verificar la conexión real, cambiar el pool y cerrar conexiones antiguas. Python y Node.js usan subclases, Go usa OnAccountChanged y Java AccountEventListener. El ejemplo falla hasta implementar el cambio real.

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

Los callbacks se ejecutan en serie con una cola limitada; las actualizaciones fallidas se reintentan cada 1–30 segundos. El éxito se informa tras completar el callback. Si falla la subida del resultado, el mismo listener no repite la acción completada. Los comandos requieren una reclamación aceptada y sus acciones fallidas no se repiten automáticamente. Las cuentas aplicadas se reportan automáticamente y el cambio A/B sustituye la cuenta actual. Implementa la limpieza de conexiones revocadas. Informar uso, consultar o entregar archivos no confirma el éxito de la aplicación. Las API de bajo nivel conservan la confirmación explícita. Mantén la idempotencia tras reinicios.

### Ciclo de vida y cierre

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events espera al cierre; start_events no garantiza la sincronización inicial. Espere a que la aplicación esté lista. Un listener por cliente; stop_events y close esperan al manejador activo. Puede detenerse desde el propio manejador. clone reinicializa la subclase.

### Credenciales más recientes y fallos del servidor

El SDK consulta primero la API y actualiza el valor retenido sin volver a una versión anterior. Solo timeout, error de red o HTTP 5xx permiten usar un valor previo del mismo selector, marcado como local y sin caducidad temporal. Clones y reinicios del SDK empiezan sin caché. HTTP 401/403/404 o credential_not_found (400) borran valores; 426 y respuestas incorrectas fallan sin respaldo. Una API opcional de metadatos ausente no borra la caché. PAM Agent conserva las cuentas declaradas en store/services; una consulta puntual no declarada no se guarda automáticamente. Las revocaciones explícitas se aplican antes de la actualización HTTP, incluso sin conexión. Los snapshots push no definen todos los permisos de almacenamiento; una reconciliación correcta verifica la autorización actual.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

Con la escucha gestionada activa, snapshot y credential.updated consultan automáticamente la credencial actual, reemplazan el valor y llaman al manejador. Un fallo de actualización conserva el valor anterior y reintenta. El PAM Agent también consulta tras una notificación y conserva sus datos durante fallos del servidor. Use las llamadas que requieren la API siguientes para actualizar o cambiar conexiones; un valor retenido no es una versión recién obtenida ni confirma rotaciones automáticamente.

En reposo se envía ping cada 10 segundos; unos 30 segundos sin mensajes provocan reconexión, con espera exponencial de 1–30 segundos y firma nueva. El snapshot restaura el estado actual, sin reproducir eventos históricos.



## 5. Métodos habituales

### Listar cuentas autorizadas

Los métodos de listado consultan el ámbito pull autorizado actual y devuelven metadatos de cuentas, activos y políticas push, sin obtener contraseñas ni registrar consultas de secretos. Incluyen cuentas sin políticas. La lista completa recorre todas las páginas; el método de página devuelve `count` y `accounts` (Go devuelve lista y total por separado). Los valores predeterminados son limit 200 y offset 0; limit acepta 1–500 y search hasta 128 caracteres para nombre de cuenta, usuario, nombre de activo o dirección. Duplicados, resultados incompletos y cambios del total producen error; repita el listado completo si cambia la autorización. Para obtener un secreto, use el ID de la cuenta. Los ejemplos requieren código SDK con los métodos nuevos; la versión publicada 1.0.2 aún no los ofrece en Python, Java ni Node.js.

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


### Información de aplicación, réplica y políticas

get_application devuelve ID, nombre, organización y cantidades de cuentas autorizadas y políticas visibles de la aplicación autenticada. get_client_info devuelve solamente registro, tipo y versiones de la réplica actual. La lista devuelve todas las políticas vinculadas visibles, incluidas las desactivadas; search busca nombres o claves. Consulte una política por su ID o clave propia, sin usar claves push de cuentas. La rotación requiere autorización para ambas cuentas y la cuenta activa; los recuentos de suscripción incluyen solo cuentas autorizadas. Las consultas no devuelven secretos ni obtienen o confirman credenciales. El primer acceso registra la réplica. Requieren Core y código SDK actualizados; la versión publicada 1.0.2 aún no incluye estos métodos.

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


### Informe de estado de la aplicación

Informe una instantánea completa del estado y los IDs de las cuentas en uso. Estados: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Una lista vacía borra el uso anterior. Llame al cambiar el estado o desde el heartbeat de la aplicación. Python, Java y Node.js devuelven false ante errores de red/API; Go ofrece `TryReportStatus`. Las variantes estrictas muestran los errores. Los argumentos inválidos se rechazan. Se utiliza el timeout HTTP configurado. El informe no obtiene secretos, confirma rotaciones ni invalida la caché. No incluya secretos ni mensajes de excepción en `error_code`. ClientInfo devuelve el último informe y su fecha.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### Eventos de bajo nivel y comandos

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

Use list_application_commands para consultar solicitudes pendientes y execute_application_command(event, handler) para reclamar su ejecución. Solo una reclamación aceptada ejecuta el handler. El de cambio verifica revisión y cuenta, aplica y confirma; el de reinicio reinicia y comprueba el estado. Informe de éxito solo al terminar. Un fallo al informar del resultado conserva la excepción original del handler.

Las API con key se conservan por compatibilidad. Use `get_account` y `confirm_event` para nuevas integraciones y actualice también Core.

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

## 6. Verificar la integración

1. Consulte una cuenta autorizada y pruebe una conexión real.
2. Verifique sincronización inicial y una actualización; el callback debe terminar antes del éxito.
3. Revise uso/estado y resultado por separado. Consulta e informe de uso no confirman aplicación.

### Solución de problemas

El cliente cierra sesiones HTTP y flujos de eventos con with o close. clone crea una sesión independiente. Los fallos HTTP, de red, autenticación y decodificación generan PAMError con code, status_code, detail y original_error. Reintente errores transitorios en la aplicación, gestione los rechazos de autorización y no registre credenciales.

Todos los SDK usan protocolo versión 1; PAM Agent usa esquema de configuración versión 1. Ante client_upgrade_required (HTTP 426), verifique compatibilidad y actualice. Se toleran campos opcionales y notificaciones desconocidos; no aplique ni confirme políticas no soportadas. Los recibos se envían automáticamente y no prueban la aplicación de credenciales. Los metadatos del cliente pueden incluir server_capabilities. Ausente o null significa desconocido; se toleran nombres nuevos. La lista describe funciones del servidor, no permisos.

<!-- sdk-doc:end -->
