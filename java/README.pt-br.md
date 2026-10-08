# JumpServer PAM Java SDK

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

## Requisitos

- JDK 11+ / Maven / Jackson
- `src/main/java/org/jumpserver/pam/Demo.java`

## Configurar e executar

Instale o SDK fonte e configure os valores abaixo. Autorize contas para pull em Gerenciamento de aplicações; vincule políticas somente quando precisar de push ou rotação. Obtenha AK/SK e ID da organização nos materiais de acesso. Substitua os exemplos e proteja os segredos de implantação. Cada réplica precisa de um ID estável e único. Use um só seletor: ID da conta ou key da política.

```bash
cd java
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

mvn package dependency:copy-dependencies
java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.Demo
```

Os SDKs são instalados a partir deste repositório e ainda não foram publicados em registros públicos. Substitua /path/to/jumpserver por um caminho absoluto. Execute a instalação Go e Node.js no diretório da aplicação ou adicione a dependência Java ao pom.xml da aplicação. Substitua as importações locais dos exemplos pelas importações de pacote abaixo.

```bash
mvn -f /path/to/pam-clients/java/pom.xml install
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

## Requisição e resposta

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

## Eventos e aplicação de credenciais

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

```java
package org.jumpserver.pam;

import java.util.List;
import org.jumpserver.pam.Models.Account;
import org.jumpserver.pam.Models.Event;

public final class EventsDemo {
  private static void applyAccount(Account account) {
    throw new UnsupportedOperationException("Implement connection validation, pool switching and old connection cleanup");
  }
  private static void applyEvent(Client client, Event event) {
    if (event.getEvent().equals("application.restart.requested"))
      throw new UnsupportedOperationException("Implement restart and health check");
    Account account = client.getAccount(event.getAccountId(), false);
    if (account.getRevision() != event.getAccountRevision())
      throw new IllegalArgumentException("Event account version is superseded");
    applyAccount(account);
  }
  private static void handleEvent(Client client, Event event) {
    if (!event.getCommandId().isEmpty()
        && !client.reportApplicationCommandResult(event.getCommandId(), "running", null).isAccepted()) return;
    try { applyEvent(client, event); }
    catch (RuntimeException error) {
      client.confirmEvent(event.getEventId(), "failed", "application_failed");
      throw error;
    }
    client.confirmEvent(event.getEventId());
  }
  public static void main(String[] args) {
    Client.Options options = new Client.Options(System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
        System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID")).orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Thread stop = new Thread(client::close, "jms-pam-shutdown");
      Runtime.getRuntime().addShutdownHook(stop);
      try (EventStream stream = client.watchCredentialEvents()) {
        for (Event event : stream) {
          List<Event> updates = !event.getCommandId().isEmpty() || event.getEvent().equals("credential.updated")
              ? List.of(event) : event.getEvent().equals("snapshot") ? event.getCredentials() : List.of();
          // Reconcile removed connections on snapshots; release revoked accounts.
          for (Event update : updates) {
            try { handleEvent(client, update); }
            catch (RuntimeException error) { System.err.println(error.getClass().getSimpleName()); }
          }
        }
      } finally { Runtime.getRuntime().removeShutdownHook(stop); }
    }
  }
}
```

As APIs com key permanecem por compatibilidade. Novas integrações usam `get_account` e `confirm_event` e exigem atualização do Core.

## Comandos de aplicação

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

## Métodos comuns

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

## Solução de problemas

Falhas HTTP, rede, autenticação e decodificação usam o tipo de erro SDK com código e estado HTTP. Feche fluxos e clientes após usar; clones têm ciclos de vida independentes. Repita falhas transitórias na aplicação e não registre segredos nem cabeçalhos de autenticação.

`PAMException`

Todos os SDKs usam protocolo versão 1; Agent usa esquema de configuração versão 1. Em client_upgrade_required (HTTP 426), verifique compatibilidade e atualize. Campos opcionais e notificações desconhecidos são tolerados; não aplique nem confirme políticas não suportadas. Os recibos são automáticos e não provam aplicação de credenciais.

## Integração com o Go Agent

A identidade usa app_id, app_secret, org_id e instance_id estável; as permissões seguem as políticas da aplicação. Caminhos e ações são locais: state_file mantém as senhas atuais, event_file registra eventos sem segredos, delivery define a saída padrão e rules define arquivos, modelos e reload/restart ou scripts fixos. Ao receber uma atualização, o Agent busca e salva a senha atual, substitui os arquivos atomicamente e executa a ação; falhas são tentadas novamente. Use credentials[].key de get_accounts nas regras; chaves de assinatura incluem o ID da conta. rules vazio grava um arquivo por chave.

As rules locais definem arquivos, JSON/EnvironmentFile ou modelos confiáveis e uma ação systemd reload/restart ou executável fixo. Scripts recebem JSON via stdin, usam argumentos fixos e prazo limitado, e verificam a aplicação antes de retornar sucesso. Core não pode ampliar essas capacidades. Reinicie o Agent após editar a configuração privada.

A configuração baixada já inclui a identidade e a entrega do Agent. Em rules, declare os IDs das contas usadas pela aplicação, a atualização da configuração, a ativação e a verificação da conexão em execução. allow_account_switch permite a mesma regra na rotação A/B em ambos os sentidos. credential_check é opcional para testar o novo acesso antes de alterar arquivos. Há valores padrão para estado, eventos, Socket e reconciliação de 300 segundos. rules vazio grava um arquivo padrão por credencial.

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

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```
