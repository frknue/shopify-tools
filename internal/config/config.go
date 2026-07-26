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

	// path is where this config was loaded from; not serialized.
	path string `yaml:"-" json:"-"`
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
func (c *Config) applyEnv() {
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
	}
	if shop != "" {
		p.Shop = shop
	}
	if token != "" {
		p.AccessToken = token
	}
	if apiVersion != "" {
		p.APIVersion = apiVersion
	}
	c.CurrentProfile = name
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
		c.CurrentProfile = name
	}
}

// RemoveProfile deletes a profile, clearing the current pointer if needed.
func (c *Config) RemoveProfile(name string) bool {
	if _, ok := c.Profiles[name]; !ok {
		return false
	}
	delete(c.Profiles, name)
	if c.CurrentProfile == name {
		c.CurrentProfile = ""
		if names := c.ProfileNames(); len(names) == 1 {
			c.CurrentProfile = names[0]
		}
	}
	return true
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

	data, err := yaml.Marshal(c)
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
