package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frknue/shopify-tools/internal/config"
)

func TestNormalizeShop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "bare subdomain", in: "acme", want: "acme.myshopify.com"},
		{name: "full domain", in: "acme.myshopify.com", want: "acme.myshopify.com"},
		{name: "https prefix", in: "https://acme.myshopify.com", want: "acme.myshopify.com"},
		{name: "trailing slash", in: "https://acme.myshopify.com/", want: "acme.myshopify.com"},
		{name: "admin path", in: "https://acme.myshopify.com/admin", want: "acme.myshopify.com"},
		{name: "uppercase", in: "ACME.MyShopify.com", want: "acme.myshopify.com"},
		{name: "custom domain kept", in: "shop.example.com", want: "shop.example.com"},
		{name: "empty", in: "  ", wantErr: true},
		{name: "with path", in: "acme.myshopify.com/products/1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.NormalizeShop(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeShop(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeShop(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeShop(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected no profiles, got %d", len(cfg.Profiles))
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")

	cfg := config.New()
	cfg.SetPath(path)
	cfg.SetProfile("staging", &config.Profile{
		Shop:        "acme.myshopify.com",
		AccessToken: "shpat_test",
		APIVersion:  "2026-04",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %o, want 600 (it holds access tokens)", perm)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	profile, err := loaded.Profile("")
	if err != nil {
		t.Fatalf("Profile() returned error: %v", err)
	}
	if profile.Name != "staging" || profile.Shop != "acme.myshopify.com" || profile.AccessToken != "shpat_test" {
		t.Errorf("round-tripped profile = %+v, want staging/acme.myshopify.com", profile)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg := config.New()
	cfg.SetPath(path)
	cfg.SetProfile("prod", &config.Profile{Shop: "file.myshopify.com", AccessToken: "from-file"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	t.Setenv(config.EnvPrefix+"SHOP", "env.myshopify.com")
	t.Setenv(config.EnvPrefix+"ACCESS_TOKEN", "from-env")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	profile, err := loaded.Profile("")
	if err != nil {
		t.Fatalf("Profile() returned error: %v", err)
	}
	if profile.Shop != "env.myshopify.com" || profile.AccessToken != "from-env" {
		t.Errorf("env did not win over file: got %+v", profile)
	}
}

func TestProfileErrorsWhenUnconfigured(t *testing.T) {
	cfg := config.New()
	if _, err := cfg.Profile(""); !errors.Is(err, config.ErrNoProfile) {
		t.Errorf("Profile() error = %v, want ErrNoProfile", err)
	}
	cfg.SetProfile("a", &config.Profile{Shop: "a.myshopify.com", AccessToken: "t"})
	if _, err := cfg.Profile("missing"); !errors.Is(err, config.ErrNoProfile) {
		t.Errorf("Profile(missing) error = %v, want ErrNoProfile", err)
	}
}

func TestRemoveProfileClearsCurrent(t *testing.T) {
	cfg := config.New()
	cfg.SetProfile("a", &config.Profile{Shop: "a.myshopify.com", AccessToken: "t"})
	cfg.SetProfile("b", &config.Profile{Shop: "b.myshopify.com", AccessToken: "t"})
	cfg.CurrentProfile = "a"

	if !cfg.RemoveProfile("a") {
		t.Fatal("RemoveProfile(a) = false, want true")
	}
	if cfg.CurrentProfile != "b" {
		t.Errorf("CurrentProfile = %q, want it to fall back to the only remaining profile", cfg.CurrentProfile)
	}
	if cfg.RemoveProfile("a") {
		t.Error("RemoveProfile(a) on a missing profile = true, want false")
	}
}

func TestCLIAccountsSurviveARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.SetPath(path)
	cfg.SetProfile("prod", &config.Profile{Shop: "acme.myshopify.com", AccessToken: "shpat_x"})
	cfg.CLIAccounts.Pending = []string{"work", "personal"}
	cfg.SetCLIAccount(config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

	if cfg.CLIAccounts.Current != "work" {
		t.Errorf("Current = %q, want the recorded profile", cfg.CLIAccounts.Current)
	}
	if got := cfg.PendingCLIAccounts(); len(got) != 1 || got[0] != "personal" {
		t.Errorf("PendingCLIAccounts() = %v, want the linked name removed", got)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	account, ok := reloaded.CLIAccount("work")
	if !ok || account.ShopifyAlias != "dev@example.test" {
		t.Errorf("CLIAccount(work) = %+v, %v; want it read back", account, ok)
	}
	if _, ok := reloaded.CLIAccount("personal"); ok {
		t.Error("CLIAccount(personal) reported a profile that is only pending")
	}
}

func TestClearCLIAccountsKeepsStoreProfiles(t *testing.T) {
	cfg := config.New()
	cfg.SetProfile("prod", &config.Profile{Shop: "acme.myshopify.com", AccessToken: "shpat_x"})
	cfg.SetCLIAccount(config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})
	cfg.CLIAccounts.LegacyImported = true

	cfg.ClearCLIAccounts()

	if len(cfg.CLIAccounts.Accounts) != 0 || cfg.CLIAccounts.Current != "" {
		t.Errorf("CLIAccounts = %+v, want them cleared", cfg.CLIAccounts)
	}
	if !cfg.CLIAccounts.LegacyImported {
		t.Error("LegacyImported = false, want the one-time import to stay done")
	}
	if cfg.Profiles["prod"] == nil {
		t.Error("store profiles were cleared along with the CLI accounts")
	}
}

// onDisk reads the config the way a run with no SHOPIFY_TOOLS_* set would see
// it. Reloading with the variables still exported just re-applies them, which
// hides what was actually written.
func onDisk(t *testing.T, path string) *config.Config {
	t.Helper()
	for _, key := range []string{"SHOP", "ACCESS_TOKEN", "API_VERSION", "PROFILE", "OUTPUT"} {
		t.Setenv(config.EnvPrefix+key, "")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(%s) returned error: %v", path, err)
	}
	return cfg
}

// The environment configures a run; it must not edit the file. An access token
// exported in CI ending up on disk is the case that matters most.
func TestEnvValuesAreNotWrittenToTheFile(t *testing.T) {
	t.Setenv(config.EnvPrefix+"SHOP", "from-env.myshopify.com")
	t.Setenv(config.EnvPrefix+"ACCESS_TOKEN", "shpat_from_ci")
	t.Setenv(config.EnvPrefix+"OUTPUT", "yaml")

	path := filepath.Join(t.TempDir(), "config.yaml")
	seeded := config.New()
	seeded.SetPath(path)
	seeded.SetProfile("production", &config.Profile{
		Shop: "acme.myshopify.com", AccessToken: "shpat_stored",
	})
	if err := seeded.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// The run itself still sees the environment.
	profile, err := cfg.Profile("")
	if err != nil {
		t.Fatalf("Profile() returned error: %v", err)
	}
	if profile.AccessToken != "shpat_from_ci" || profile.Shop != "from-env.myshopify.com" {
		t.Errorf("resolved profile = %+v, want the environment to win at runtime", profile)
	}
	if cfg.Defaults.Output != "yaml" {
		t.Errorf("output = %q, want the environment to win at runtime", cfg.Defaults.Output)
	}

	// Saving for an unrelated reason must not persist any of it.
	cfg.SetCLIAccount(config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() returned error: %v", err)
	}
	for _, unwanted := range []string{"shpat_from_ci", "from-env.myshopify.com"} {
		if strings.Contains(string(written), unwanted) {
			t.Errorf("the config file carries %q from the environment:\n%s", unwanted, written)
		}
	}

	stored := onDisk(t, path)
	if got := stored.Profiles["production"]; got == nil || got.AccessToken != "shpat_stored" {
		t.Errorf("stored profile = %+v, want its own token intact", got)
	}
	if stored.Defaults.Output == "yaml" {
		t.Error("the environment's output format was persisted")
	}
	if _, ok := stored.CLIAccount("work"); !ok {
		t.Error("the change that prompted the save was not written")
	}
}

// A profile that exists only because of the environment is not written at all,
// and nothing is left pointing at it.
func TestAnEnvOnlyProfileIsNotPersisted(t *testing.T) {
	t.Setenv(config.EnvPrefix+"SHOP", "from-env.myshopify.com")
	t.Setenv(config.EnvPrefix+"ACCESS_TOKEN", "shpat_from_ci")

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.CurrentProfile != "env" {
		t.Fatalf("CurrentProfile = %q, want the implicit env profile at runtime", cfg.CurrentProfile)
	}

	cfg.SetCLIAccount(config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	stored := onDisk(t, path)
	if len(stored.Profiles) != 0 {
		t.Errorf("profiles on disk = %+v, want none", stored.Profiles)
	}
	if stored.CurrentProfile != "" {
		t.Errorf("current profile = %q, want nothing pointing at the env profile", stored.CurrentProfile)
	}
}

// Reverting the environment must never undo a deliberate change.
func TestDeliberateChangesWinOverTheEnvOverlay(t *testing.T) {
	t.Setenv(config.EnvPrefix+"PROFILE", "staging")
	t.Setenv(config.EnvPrefix+"ACCESS_TOKEN", "shpat_from_ci")

	path := filepath.Join(t.TempDir(), "config.yaml")
	seeded := config.New()
	seeded.SetPath(path)
	seeded.SetProfile("staging", &config.Profile{
		Shop: "staging.myshopify.com", AccessToken: "shpat_old",
	})
	seeded.SetProfile("production", &config.Profile{
		Shop: "acme.myshopify.com", AccessToken: "shpat_prod",
	})
	seeded.SetCurrentProfile("production")
	if err := seeded.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	// What `auth login --profile-name staging` followed by `auth use staging`
	// does, with the environment naming that very profile.
	cfg.SetProfile("staging", &config.Profile{
		Shop: "staging.myshopify.com", AccessToken: "shpat_typed_by_hand",
	})
	cfg.SetCurrentProfile("staging")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	stored := onDisk(t, path)
	if got := stored.Profiles["staging"]; got == nil || got.AccessToken != "shpat_typed_by_hand" {
		t.Errorf("staging = %+v, want the token the user set", got)
	}
	if stored.CurrentProfile != "staging" {
		t.Errorf("current profile = %q, want the deliberate choice persisted", stored.CurrentProfile)
	}
	if got := stored.Profiles["production"]; got == nil || got.AccessToken != "shpat_prod" {
		t.Errorf("production = %+v, want it untouched", got)
	}
}
