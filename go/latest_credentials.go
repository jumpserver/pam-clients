package pam

// Retain the latest successfully fetched credentials until authorization changes.

func (c *Client) reconcileLatestCredentials(event Event) {
	if event.Event != "snapshot" && event.Event != "app.scope.updated" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.credentialGeneration++
	if event.Event == "app.scope.updated" {
		authorized := make(map[string]bool)
		for _, id := range event.AuthorizedAccountIDs {
			authorized[id] = true
		}
		for selector, credential := range c.latestCredentials {
			if event.AuthorizedAccountIDs != nil && !authorized[credential.Account.ID] || event.AuthorizedAccountIDs == nil && selector.AccountID != "" {
				delete(c.latestCredentials, selector)
			}
		}
		return
	}
	keys := make(map[string]bool)
	for _, update := range event.Credentials {
		key := update.CredentialKey
		if key == "" {
			key = update.Key
		}
		keys[key] = true
	}
	authorized := make(map[string]bool)
	for _, id := range event.AuthorizedAccountIDs {
		authorized[id] = true
	}
	for selector, credential := range c.latestCredentials {
		// Push membership and explicit account authorization are separate scopes.
		if selector.Key != "" && !keys[selector.Key] || event.AuthorizedAccountIDs != nil && !authorized[credential.Account.ID] {
			delete(c.latestCredentials, selector)
		}
	}
}
