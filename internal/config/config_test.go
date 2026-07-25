package config_test

import (
	"errors"
	"os"
	"path/filepath"
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
		APIVersion:  "2025-07",
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
