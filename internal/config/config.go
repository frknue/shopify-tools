// Package config loads and persists the CLI configuration.
//
// Resolution order, lowest precedence first:
//
//	built-in defaults  <  config file  <  environment  <  command-line flags
//
// Flags are applied by the caller (see internal/app), because only the command
// layer knows which flags were actually set.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvPrefix is prepended to every environment variable this CLI reads.
const EnvPrefix = "SHOPIFY_TOOLS_"

// DefaultAPIVersion is the Shopify Admin API version used when none is set.
const DefaultAPIVersion = "2026-04"

// ErrNoProfile is returned when no usable profile could be resolved.
var ErrNoProfile = errors.New("no store profile configured")

// Config is the root configuration document.
type Config struct {
	// CurrentProfile is the profile used when --profile is not given.
	CurrentProfile string `yaml:"current_profile,omitempty" json:"current_profile,omitempty"`
	// Profiles maps a profile name to its store credentials.
	Profiles map[string]*Profile `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	// Defaults holds settings shared by every profile.
	Defaults Defaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	// CLIAccounts maps profile names onto Shopify CLI sessions. It is
	// independent of Profiles: those hold Admin API tokens, these only name
	// accounts the Shopify CLI already knows about.
	CLIAccounts CLIAccounts `yaml:"cli_accounts,omitempty" json:"cli_accounts,omitempty"`

	// path is where this config was loaded from; not serialized.
	path string `yaml:"-" json:"-"`
	// env is what the environment overlaid on the file; not serialized.
	env *envOverlay `yaml:"-" json:"-"`
}

// Defaults holds global, profile-independent settings.
type Defaults struct {
	Output string `yaml:"output,omitempty" json:"output,omitempty"`
	// TimeoutSeconds bounds a single API call.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
}

// Profile holds the credentials for one Shopify store.
type Profile struct {
	// Name is the key under which the profile is stored; not serialized.
	Name string `yaml:"-" json:"name"`
	// Shop is the myshopify domain, e.g. "example.myshopify.com".
	Shop string `yaml:"shop" json:"shop"`
	// AccessToken is an Admin API access token (shpat_...).
	AccessToken string `yaml:"access_token,omitempty" json:"-"`
	// APIVersion pins the Admin API version, e.g. "2026-04".
	APIVersion string `yaml:"api_version,omitempty" json:"api_version,omitempty"`
}

// CLIAccounts is the `account` tool's slice of the configuration: the mapping
// between local profile names and the accounts the Shopify CLI is logged into.
// No credentials live here — the Shopify CLI owns and refreshes those.
type CLIAccounts struct {
	// Current is the profile selected last.
	Current string `yaml:"current,omitempty" json:"current,omitempty"`
	// Accounts holds the profiles that are linked to a Shopify CLI account.
	Accounts []CLIAccount `yaml:"accounts,omitempty" json:"accounts,omitempty"`
	// Pending holds profile names that have no account linked yet, carried
	// over from a configuration written before aliases were recorded. They are
	// linked the next time they are used.
	Pending []string `yaml:"pending,omitempty" json:"pending,omitempty"`
	// LegacyImported records that the profiles of the standalone shopify-auth
	// tool were taken over already, so the one-time import does not run again
	// once a logout has emptied the list.
	LegacyImported bool `yaml:"legacy_imported,omitempty" json:"legacy_imported,omitempty"`
}

// CLIAccount links a local profile name to one Shopify CLI account.
type CLIAccount struct {
	// Name is the local profile name, e.g. "work".
	Name string `yaml:"name" json:"name"`
	// ShopifyAlias is the account the Shopify CLI reports as its current one,
	// usually an email address. It selects a session; it is not a credential.
	ShopifyAlias string `yaml:"shopify_alias" json:"shopify_alias"`
}

// New returns an empty configuration with defaults applied.
func New() *Config {
	return &Config{
		Profiles: map[string]*Profile{},
		Defaults: Defaults{Output: "table", TimeoutSeconds: 30},
	}
}

// DefaultPath returns the config file location, honouring
// SHOPIFY_TOOLS_CONFIG and XDG_CONFIG_HOME.
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvPrefix + "CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, "shopify-tools", "config.yaml"), nil
}

// Load reads the config file at path. A missing file is not an error: an empty
// configuration is returned so the CLI works out of the box with env vars only.
func Load(path string) (*Config, error) {
	cfg := New()
	cfg.path = path

	data, err := os.ReadFile(path) //nolint:gosec // path is user supplied by design
	switch {
	case errors.Is(err, os.ErrNotExist):
		cfg.applyEnv()
		return cfg, nil
	case err != nil:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	for name, p := range cfg.Profiles {
		p.Name = name
	}
	cfg.applyEnv()
	return cfg, nil
}

// applyEnv overlays environment variables on top of the file contents.
// SHOPIFY_TOOLS_SHOP / _ACCESS_TOKEN define an implicit "env" profile that
// wins over the file, mirroring how CI environments are usually wired.
//
// Everything it changes is recorded, because the environment configures a run
// but must never edit the file: see envOverlay.
func (c *Config) applyEnv() {
	// The file's own values, before anything is overlaid on them.
	env := &envOverlay{prevCurrent: c.CurrentProfile, prevOutput: c.Defaults.Output}
	c.env = env
	defer func() {
		env.current, env.currentSet = c.CurrentProfile, c.CurrentProfile != env.prevCurrent
		env.output, env.outputSet = c.Defaults.Output, c.Defaults.Output != env.prevOutput
	}()

	if v := os.Getenv(EnvPrefix + "PROFILE"); v != "" {
		c.CurrentProfile = v
	}
	if v := os.Getenv(EnvPrefix + "OUTPUT"); v != "" {
		c.Defaults.Output = v
	}

	shop := os.Getenv(EnvPrefix + "SHOP")
	token := os.Getenv(EnvPrefix + "ACCESS_TOKEN")
	apiVersion := os.Getenv(EnvPrefix + "API_VERSION")
	if shop == "" && token == "" && apiVersion == "" {
		return
	}

	name := c.CurrentProfile
	if name == "" {
		name = "env"
	}
	p := c.Profiles[name]
	if p == nil {
		p = &Profile{Name: name}
		c.Profiles[name] = p
		env.created = true
	}

	env.profile = name
	env.previous = *p
	if shop != "" {
		p.Shop = shop
	}
	if token != "" {
		p.AccessToken = token
	}
	if apiVersion != "" {
		p.APIVersion = apiVersion
	}
	env.applied = *p
	c.CurrentProfile = name
}

// envOverlay records what applyEnv put on top of the file, together with the
// values it replaced.
//
// Save reverts it, so that a run configured by the environment cannot rewrite
// the file: an access token exported in CI would otherwise be persisted to
// disk, and a stored one silently overwritten by it. Values that were changed
// again afterwards are left alone, so a deliberate edit still saves.
type envOverlay struct {
	// profile is the profile the environment supplied credentials for, and
	// created reports that it did not exist in the file at all.
	profile string
	created bool
	// applied is the profile as the environment left it; previous is how the
	// file had it.
	applied  Profile
	previous Profile

	current, prevCurrent string
	currentSet           bool

	output, prevOutput string
	outputSet          bool
}

// Path returns the file this config was loaded from.
func (c *Config) Path() string { return c.path }

// SetPath records where the config should be written.
func (c *Config) SetPath(p string) { c.path = p }

// ProfileNames returns the configured profile names in stable order.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Profile resolves a profile by name. An empty name selects the current
// profile, or the only configured one if there is exactly one.
func (c *Config) Profile(name string) (*Profile, error) {
	if name == "" {
		name = c.CurrentProfile
	}
	if name == "" && len(c.Profiles) == 1 {
		name = c.ProfileNames()[0]
	}
	if name == "" {
		return nil, fmt.Errorf("%w: run `shopify-tools auth login` or set %sSHOP and %sACCESS_TOKEN",
			ErrNoProfile, EnvPrefix, EnvPrefix)
	}

	p, ok := c.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("%w: profile %q not found (known: %s)",
			ErrNoProfile, name, strings.Join(c.ProfileNames(), ", "))
	}
	p.Name = name
	if p.APIVersion == "" {
		p.APIVersion = DefaultAPIVersion
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// SetProfile adds or replaces a profile and makes it current if it is the first.
func (c *Config) SetProfile(name string, p *Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	p.Name = name
	c.Profiles[name] = p
	if c.CurrentProfile == "" {
		c.SetCurrentProfile(name)
	}
	c.forgetEnvProfile(name)
}

// SetCurrentProfile selects the profile used when --profile is not given.
//
// Prefer it over assigning the field: it records the choice as the user's, so
// that it is written to the file even when the environment happens to name the
// same profile.
func (c *Config) SetCurrentProfile(name string) {
	c.CurrentProfile = name
	if c.env != nil {
		c.env.currentSet = false
		c.env.prevCurrent = name
	}
}

// forgetEnvProfile drops the record of what the environment supplied for a
// profile, because the caller has just set it deliberately.
func (c *Config) forgetEnvProfile(name string) {
	if c.env != nil && c.env.profile == name {
		c.env.profile, c.env.created = "", false
	}
}

// RemoveProfile deletes a profile, clearing the current pointer if needed.
func (c *Config) RemoveProfile(name string) bool {
	if _, ok := c.Profiles[name]; !ok {
		return false
	}
	delete(c.Profiles, name)
	c.forgetEnvProfile(name)
	if c.CurrentProfile == name {
		c.SetCurrentProfile("")
		if names := c.ProfileNames(); len(names) == 1 {
			c.SetCurrentProfile(names[0])
		}
	}
	return true
}

// CLIAccount returns the Shopify CLI account linked to a profile name.
func (c *Config) CLIAccount(name string) (CLIAccount, bool) {
	for _, a := range c.CLIAccounts.Accounts {
		if a.Name == name {
			return a, true
		}
	}
	return CLIAccount{}, false
}

// SetCLIAccount links a profile to a Shopify CLI account and makes it current.
func (c *Config) SetCLIAccount(a CLIAccount) {
	for i, existing := range c.CLIAccounts.Accounts {
		if existing.Name == a.Name {
			c.CLIAccounts.Accounts[i] = a
			c.CLIAccounts.Current = a.Name
			c.CLIAccounts.Pending = removeString(c.CLIAccounts.Pending, a.Name)
			return
		}
	}
	c.CLIAccounts.Accounts = append(c.CLIAccounts.Accounts, a)
	c.CLIAccounts.Current = a.Name
	c.CLIAccounts.Pending = removeString(c.CLIAccounts.Pending, a.Name)
}

// SortedCLIAccounts returns the linked profiles ordered by name.
func (c *Config) SortedCLIAccounts() []CLIAccount {
	accounts := append([]CLIAccount(nil), c.CLIAccounts.Accounts...)
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	return accounts
}

// PendingCLIAccounts returns the profile names that still need linking.
func (c *Config) PendingCLIAccounts() []string {
	pending := append([]string(nil), c.CLIAccounts.Pending...)
	sort.Strings(pending)
	return pending
}

// ClearCLIAccounts drops every profile mapping. Store profiles are untouched:
// this is the counterpart of a Shopify CLI logout, not of `auth logout`.
func (c *Config) ClearCLIAccounts() {
	c.CLIAccounts = CLIAccounts{LegacyImported: c.CLIAccounts.LegacyImported}
}

func removeString(values []string, target string) []string {
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if v != target {
			kept = append(kept, v)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// forFile returns the configuration as it should be written: this one, with
// everything the environment supplied and nobody has since changed taken back
// out. A value that differs from what the environment left is a deliberate
// change and is kept.
func (c *Config) forFile() *Config {
	out := *c
	out.Profiles = make(map[string]*Profile, len(c.Profiles))
	for name, p := range c.Profiles {
		clone := *p
		out.Profiles[name] = &clone
	}

	env := c.env
	if env == nil {
		return &out
	}

	if env.profile != "" {
		if p := out.Profiles[env.profile]; p != nil {
			if p.Shop == env.applied.Shop {
				p.Shop = env.previous.Shop
			}
			if p.AccessToken == env.applied.AccessToken {
				p.AccessToken = env.previous.AccessToken
			}
			if p.APIVersion == env.applied.APIVersion {
				p.APIVersion = env.previous.APIVersion
			}
			// A profile that only ever existed because of the environment goes
			// with it, unless something was added to it in the meantime.
			if env.created && *p == (Profile{Name: env.profile}) {
				delete(out.Profiles, env.profile)
			}
		}
	}

	if env.currentSet && out.CurrentProfile == env.current {
		out.CurrentProfile = env.prevCurrent
	}
	if _, ok := out.Profiles[out.CurrentProfile]; !ok && out.CurrentProfile != "" {
		// Never leave the file pointing at a profile it does not contain.
		out.CurrentProfile = env.prevCurrent
	}
	if env.outputSet && out.Defaults.Output == env.output {
		out.Defaults.Output = env.prevOutput
	}
	return &out
}

// Save writes the config atomically with owner-only permissions, because it
// may contain access tokens.
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config path is not set")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c.forFile())
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Validate checks that a profile is usable. Its errors wrap ErrNoProfile so
// that the CLI maps them onto the configuration exit code.
func (p *Profile) Validate() error {
	if p.Shop == "" {
		return fmt.Errorf("%w: profile %q has no shop domain", ErrNoProfile, p.Name)
	}
	if p.AccessToken == "" {
		return fmt.Errorf("%w: profile %q has no access token; run `shopify-tools auth login --shop %s`",
			ErrNoProfile, p.Name, p.Shop)
	}
	return nil
}

// NormalizeShop turns user input such as "https://acme.myshopify.com/" or
// "acme" into the canonical "acme.myshopify.com".
func NormalizeShop(s string) (string, error) {
	v := strings.TrimSpace(strings.ToLower(s))
	v = strings.TrimPrefix(strings.TrimPrefix(v, "https://"), "http://")
	v = strings.TrimSuffix(strings.TrimSuffix(v, "/"), "/admin")
	v = strings.TrimSuffix(v, "/")

	if v == "" {
		return "", errors.New("shop domain is empty")
	}
	if !strings.Contains(v, ".") {
		v += ".myshopify.com"
	}
	if strings.ContainsAny(v, " \t/?#") {
		return "", fmt.Errorf("invalid shop domain %q", s)
	}
	return v, nil
}
