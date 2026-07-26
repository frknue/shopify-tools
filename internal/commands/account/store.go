package account

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/config"
)

// legacyConfigEnv overrides the location of the configuration written by the
// standalone shopify-auth tool this one grew out of.
const legacyConfigEnv = "SHOPIFY_AUTH_CONFIG"

// configStore keeps the profile mapping in the shared config file, next to the
// store profiles. It holds no credentials, so the file's 0600 mode is about
// the Admin API tokens that live beside it, not about anything written here.
type configStore struct {
	f *app.Factory
	// imported guards the one-time takeover of the standalone tool's profiles.
	imported bool
}

func newConfigStore(f *app.Factory) *configStore { return &configStore{f: f} }

// Find returns the account linked to a profile name. A profile that is known
// but not linked yet is found with an empty alias, so the caller re-links it.
func (s *configStore) Find(name string) (config.CLIAccount, bool, error) {
	cfg, err := s.config()
	if err != nil {
		return config.CLIAccount{}, false, err
	}
	if account, ok := cfg.CLIAccount(name); ok {
		return account, true, nil
	}
	for _, pending := range cfg.CLIAccounts.Pending {
		if pending == name {
			return config.CLIAccount{Name: name}, true, nil
		}
	}
	return config.CLIAccount{}, false, nil
}

// Record links a profile to a Shopify account, makes it current, and persists.
func (s *configStore) Record(account config.CLIAccount) error {
	account.Name = strings.TrimSpace(account.Name)
	account.ShopifyAlias = strings.TrimSpace(account.ShopifyAlias)
	if err := validateProfileName(account.Name); err != nil {
		return fmt.Errorf("invalid profile name %q: %w", account.Name, err)
	}
	if err := validateShopifyAlias(account.ShopifyAlias); err != nil {
		return err
	}

	cfg, err := s.config()
	if err != nil {
		return err
	}
	cfg.SetCLIAccount(account)
	return cfg.Save()
}

// List returns the linked profiles by name, plus the one selected last.
func (s *configStore) List() ([]config.CLIAccount, string, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, "", err
	}
	return cfg.SortedCLIAccounts(), cfg.CLIAccounts.Current, nil
}

// Pending returns the profile names that still need an account linked.
func (s *configStore) Pending() ([]string, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, err
	}
	return cfg.PendingCLIAccounts(), nil
}

// Names returns every known profile name, for shell completion.
func (s *configStore) Names() ([]string, error) {
	accounts, _, err := s.List()
	if err != nil {
		return nil, err
	}
	pending, err := s.Pending()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(accounts)+len(pending))
	for _, account := range accounts {
		names = append(names, account.Name)
	}
	return append(names, pending...), nil
}

// Clear drops every profile mapping. Store credentials are left alone.
//
// It deliberately skips the takeover of the standalone tool's profiles: there
// is no point importing what is about to be deleted, and marking the takeover
// as done is what stops a logout from being undone by the next command.
func (s *configStore) Clear() error {
	cfg, err := s.f.Config()
	if err != nil {
		return err
	}
	s.imported = true
	cfg.ClearCLIAccounts()
	cfg.CLIAccounts.LegacyImported = true
	return cfg.Save()
}

// config returns the configuration, taking over the standalone tool's profiles
// the first time it is read.
func (s *configStore) config() (*config.Config, error) {
	cfg, err := s.f.Config()
	if err != nil {
		return nil, err
	}
	if !s.imported {
		s.imported = true
		if err := s.importLegacy(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// importLegacy copies the profiles of the standalone shopify-auth tool into
// the shared config, once. A missing or unreadable legacy file is not an
// error: there is nothing to take over and every profile can be re-created by
// using it.
func (s *configStore) importLegacy(cfg *config.Config) error {
	accounts := cfg.CLIAccounts
	if accounts.LegacyImported || len(accounts.Accounts) > 0 || len(accounts.Pending) > 0 {
		return nil
	}

	path, ok := legacyConfigPath()
	if !ok {
		return nil
	}
	imported, ok := readLegacyAccounts(path)
	if !ok {
		return nil
	}

	imported.LegacyImported = true
	cfg.CLIAccounts = imported
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("import profiles from %s: %w", path, err)
	}
	if !s.f.Options.Quiet {
		fmt.Fprintf(s.f.IOStreams.ErrOut, "Imported %d profile(s) from %s into %s.\n",
			len(imported.Accounts)+len(imported.Pending), path, cfg.Path())
	}
	return nil
}

// legacyConfigPath reports where the standalone tool kept its profiles.
func legacyConfigPath() (string, bool) {
	if path := os.Getenv(legacyConfigEnv); path != "" {
		return path, true
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, "shopify-auth", "accounts.json"), true
}

// readLegacyAccounts parses the standalone tool's JSON file. Both the current
// shape (objects with an alias) and the first one (bare profile names) are
// understood; the latter becomes a list of profiles pending a link.
func readLegacyAccounts(path string) (config.CLIAccounts, bool) {
	contents, err := os.ReadFile(path) //nolint:gosec // the path is the user's own config, by design
	if err != nil || len(bytes.TrimSpace(contents)) == 0 {
		return config.CLIAccounts{}, false
	}

	var raw struct {
		Accounts json.RawMessage `json:"accounts"`
		Pending  []string        `json:"pending_profiles"`
		Current  string          `json:"current"`
	}
	if err := json.Unmarshal(contents, &raw); err != nil {
		return config.CLIAccounts{}, false
	}

	imported := config.CLIAccounts{
		Current: strings.TrimSpace(raw.Current),
		Pending: raw.Pending,
	}
	// An empty "accounts" key means only pending profiles were stored.
	if trimmed := bytes.TrimSpace(raw.Accounts); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		var linked []config.CLIAccount
		if err := json.Unmarshal(trimmed, &linked); err == nil {
			imported.Accounts = linked
		} else {
			// The first release stored bare profile names. Keep them and link
			// each one through Shopify's own picker the next time it is used.
			var names []string
			if err := json.Unmarshal(trimmed, &names); err != nil {
				return config.CLIAccounts{}, false
			}
			imported.Pending = append(imported.Pending, names...)
		}
	}

	imported = sanitize(imported)
	if len(imported.Accounts) == 0 && len(imported.Pending) == 0 {
		return config.CLIAccounts{}, false
	}
	return imported, true
}

// sanitize drops entries the tool would not have written itself: invalid or
// duplicated names, profiles listed as both linked and pending, and a current
// profile that no longer exists.
func sanitize(accounts config.CLIAccounts) config.CLIAccounts {
	linked := make([]config.CLIAccount, 0, len(accounts.Accounts))
	seen := make(map[string]bool, len(accounts.Accounts))
	for _, account := range accounts.Accounts {
		account.Name = strings.TrimSpace(account.Name)
		account.ShopifyAlias = strings.TrimSpace(account.ShopifyAlias)
		if validateProfileName(account.Name) != nil || validateShopifyAlias(account.ShopifyAlias) != nil {
			continue
		}
		if seen[account.Name] {
			continue
		}
		seen[account.Name] = true
		linked = append(linked, account)
	}

	var pending []string
	pendingSeen := make(map[string]bool, len(accounts.Pending))
	for _, name := range accounts.Pending {
		name = strings.TrimSpace(name)
		if validateProfileName(name) != nil || seen[name] || pendingSeen[name] {
			continue
		}
		pendingSeen[name] = true
		pending = append(pending, name)
	}

	current := accounts.Current
	if !seen[current] {
		current = ""
	}
	return config.CLIAccounts{Current: current, Accounts: linked, Pending: pending}
}
