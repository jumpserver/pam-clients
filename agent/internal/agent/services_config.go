package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	pam "github.com/jumpserver/pam-clients/go"
)

type StoreConfiguration struct {
	Accounts []string `json:"accounts"`
	Root     string   `json:"root"`
	Format   string   `json:"format,omitempty"`
	Owner    string   `json:"owner,omitempty"`
}

type LocalAPIConfiguration struct {
	Socket string `json:"socket_path,omitempty"`
	Owner  string `json:"owner,omitempty"`
}

var accountIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func (c Config) modern() bool {
	return c.ConfigVersion == 2 || c.Store != nil || c.Services != nil || c.LocalAPI != nil
}

// Keep delivery/rules as the v1 implementation detail, never as v2 wire fields.
func (c Config) MarshalJSON() ([]byte, error) {
	type plain Config
	if !c.modern() {
		return json.Marshal(plain(c))
	}
	raw, err := json.Marshal(plain(c))
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	delete(object, "delivery")
	delete(object, "rules")
	object["config_version"] = json.RawMessage("2")
	return json.Marshal(object)
}

func (c Config) normalized() Config {
	if !c.modern() {
		return c
	}
	c.ConfigVersion = 2
	api := LocalAPIConfiguration{}
	if c.LocalAPI != nil {
		api = *c.LocalAPI
	}
	if api.Socket == "" {
		api.Socket = DefaultSocket
	}
	if api.Owner == "" && c.Store != nil {
		api.Owner = c.Store.Owner
	}
	if api.Owner == "" {
		if current, err := user.Current(); err == nil {
			api.Owner = current.Username
		}
	}
	c.LocalAPI = &api
	c.Delivery = DeliveryConfiguration{Mode: "socket", Socket: api.Socket, User: api.Owner, Root: filepath.Join(filepath.Dir(c.StateFile), "credentials")}
	if c.Store != nil {
		store := *c.Store
		if store.Format == "" {
			store.Format = "json"
		}
		if store.Owner == "" {
			store.Owner = api.Owner
		}
		c.Store = &store
		c.Delivery.Root = store.Root
	}
	return c
}

func (c Config) validateServices() error {
	if c.ConfigVersion != 0 && c.ConfigVersion != 1 && c.ConfigVersion != 2 {
		return unsupportedVersion("configuration", "config_version", c.ConfigVersion, 2)
	}
	if len(c.Rules) > 0 {
		return errors.New("services cannot be combined with legacy rules")
	}
	if c.Store != nil {
		store := c.Store
		if store.Accounts == nil || !absolute(store.Root) || (store.Format != "" && store.Format != "json" && store.Format != "environment") {
			return errors.New("store requires explicit accounts, an absolute root and json or environment format")
		}
		for _, path := range []string{c.StateFile, c.EventFile} {
			if path != "" && (path == store.Root || strings.HasPrefix(path, store.Root+string(filepath.Separator))) {
				return errors.New("private state and events must be outside the managed store")
			}
		}
		seen := map[string]bool{}
		for _, id := range store.Accounts {
			if seen[id] || (id == "all" && len(store.Accounts) != 1) || (id != "all" && !accountIDPattern.MatchString(id)) {
				return errors.New("invalid or duplicate store account ID; all must be used alone")
			}
			seen[id] = true
		}
	}
	names := map[string]bool{}
	owners := map[string]string{}
	for _, service := range c.Services {
		if !serviceNamePattern.MatchString(service.Name) || names[service.Name] {
			return errors.New("services require unique stable names")
		}
		names[service.Name] = true
		if len(service.Keys) > 0 || len(service.Accounts) == 0 || len(service.Files) > 0 || service.Action != nil {
			return errors.New("services require account selectors and staged actions")
		}
		aliases := map[string]bool{}
		for _, account := range service.Accounts {
			if !accountIDPattern.MatchString(account.AccountID) || account.AllowAccountSwitch {
				return errors.New("service accounts require valid IDs; allow_account_switch belongs to the service")
			}
			if account.Alias != "" {
				if !configFieldPattern.MatchString(account.Alias) || aliases[account.Alias] {
					return errors.New("invalid or duplicate account alias")
				}
				aliases[account.Alias] = true
			}
		}
		{
			paths := []string{}
			if update := service.ConfigUpdate; update != nil {
				paths = append(paths, update.File)
				paths = append(paths, update.targetPaths()...)
				for _, file := range update.Files {
					paths = append(paths, file.Path)
				}
			}
			for _, path := range paths {
				if path == "" {
					continue
				}
				if owner := owners[path]; owner != "" {
					return errors.New("a business target must belong to one service")
				}
				owners[path] = service.Name
				if c.Store != nil && (path == c.Store.Root || strings.HasPrefix(path, c.Store.Root+string(filepath.Separator))) {
					return errors.New("service targets must be outside the managed store")
				}
			}
		}
	}
	return nil
}

func sourceAccount(accounts []AccountSelector, source string) (AccountSelector, string, bool) {
	parts := strings.Split(source, ".")
	field := source
	var selected AccountSelector
	if len(parts) == 1 && len(accounts) == 1 {
		selected = accounts[0]
	} else if len(parts) == 2 {
		field = parts[1]
		for _, account := range accounts {
			if account.Alias != "" && account.Alias == parts[0] {
				selected = account
				break
			}
		}
	} else {
		return selected, "", false
	}
	valid := selected.AccountID != "" && (field == "username" || field == "secret" || field == "account_id" || field == "revision")
	return selected, field, valid
}

func validMappedSource(accounts []AccountSelector, source string) bool {
	_, _, valid := sourceAccount(accounts, source)
	return valid
}

func prepareServiceVersions(config Config, state *State) {
	hash := func(value any) string {
		raw, _ := json.Marshal(value)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	storeHash := hash(config.Store)
	if state.StoreConfigHash != storeHash {
		state.StoreVersions = map[string]string{}
		state.StoreConfigHash = storeHash
	}
	if state.ServiceConfigHashes == nil {
		state.ServiceConfigHashes = map[string]string{}
	}
	active := map[string]bool{}
	for _, service := range config.Services {
		active[service.Name] = true
		service.Accounts = append([]AccountSelector(nil), service.Accounts...)
		for index := range service.Accounts {
			service.Accounts[index].Username = ""
		}
		if service.UpdateOnNotification == nil {
			yes := true
			service.UpdateOnNotification = &yes
		}
		serviceHash := hash(struct {
			Service      Rule
			DefaultOwner string
		}{service, config.Delivery.User})
		if state.ServiceConfigHashes[service.Name] != serviceHash {
			for key := range state.ServiceVersions[service.Name] {
				delete(state.Delivered, key)
				delete(state.Applied, key)
			}
			delete(state.ServiceVersions, service.Name)
			state.ServiceConfigHashes[service.Name] = serviceHash
		}
	}
	for name := range state.ServiceVersions {
		if !active[name] {
			delete(state.ServiceVersions, name)
			delete(state.ServiceConfigHashes, name)
		}
	}
}

func (c Config) serviceScope() *pam.DeliveryScope {
	scope := &pam.DeliveryScope{Keys: []string{}, AccountIDs: []string{}, IncludeAuthorized: true, SwitchAccountIDs: []string{}}
	ids, switching := map[string]bool{}, map[string]bool{}
	if c.Store != nil {
		for _, id := range c.Store.Accounts {
			if id == "all" {
				scope.AllAuthorized = true
			} else {
				ids[id] = true
			}
		}
	}
	for _, service := range c.Services {
		for _, account := range service.Accounts {
			if service.AllowAccountSwitch {
				switching[account.AccountID] = true
			} else {
				ids[account.AccountID] = true
			}
		}
	}
	for id := range ids {
		scope.AccountIDs = append(scope.AccountIDs, id)
	}
	for id := range switching {
		scope.SwitchAccountIDs = append(scope.SwitchAccountIDs, id)
	}
	sort.Strings(scope.AccountIDs)
	sort.Strings(scope.SwitchAccountIDs)
	return scope
}

func (c Config) requestsMetadata(item pam.CredentialRevision) bool {
	if !accountIDPattern.MatchString(item.AccountID) {
		return false
	}
	if item.Key == "account:"+item.AccountID && item.AccountSwitch == nil {
		if c.Store != nil && (contains(c.Store.Accounts, "all") || contains(c.Store.Accounts, item.AccountID)) {
			return true
		}
		for _, service := range c.Services {
			for _, account := range service.Accounts {
				if account.AccountID == item.AccountID {
					return true
				}
			}
		}
	}
	if item.AccountSwitch != nil {
		for _, service := range c.Services {
			if service.AllowAccountSwitch {
				for _, account := range service.Accounts {
					if contains(item.AccountSwitch.AccountIDs, account.AccountID) {
						return true
					}
				}
			}
		}
	}
	return false
}
