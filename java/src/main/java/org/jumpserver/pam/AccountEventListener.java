package org.jumpserver.pam;

import org.jumpserver.pam.Models.Account;
import org.jumpserver.pam.Models.Credential;

/** Managed account events: return only after connection validation and application switching. */
@FunctionalInterface
public interface AccountEventListener extends CredentialEventListener {
  void onAccountChanged(Account account) throws Exception;

  @Override
  default void onCredentialChanged(Credential credential) {
    throw new UnsupportedOperationException("Use managed account events");
  }

  default void onAccountRevoked(String accountId) throws Exception {
    throw new UnsupportedOperationException("Implement revoked account cleanup");
  }

  default void onApplicationRestart() throws Exception {
    throw new UnsupportedOperationException("Application restart is not configured");
  }
}
