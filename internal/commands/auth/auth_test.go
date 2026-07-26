package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/commands/auth"
	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
	"github.com/frknue/shopify-tools/internal/shopify"
)

// This test shows the intended way to test a tool in isolation: build the
// factory, override the dependency you want to fake, run the command.
func TestAuthStatusUsesInjectedClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"shop":{"name":"Acme Inc","myshopifyDomain":"acme.myshopify.com","plan":{"displayName":"Shopify Plus"}}}}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.SetPath(path)
	cfg.SetProfile("test", &config.Profile{
		Shop:        "acme.myshopify.com",
		AccessToken: "shpat_test",
		APIVersion:  "2026-04",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	io, stdout, stderr := iostreams.Test()
	factory := app.NewFactory(io)
	factory.Options.ConfigPath = path
	factory.Options.OutputFormat = "json"
	factory.ClientFunc = func(context.Context) (*shopify.Client, error) {
		return shopify.New("acme.myshopify.com", "shpat_test", "2026-04",
			shopify.WithBaseURL(srv.URL),
			shopify.WithHTTPClient(srv.Client()),
		)
	}

	cmd := auth.NewCommand(factory)
	cmd.SetArgs([]string{"status"})
	cmd.SetOut(io.Out)
	cmd.SetErr(io.ErrOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth status returned error: %v (stderr: %s)", err, stderr.String())
	}

	var got auth.Status
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.ShopName != "Acme Inc" || got.Plan != "Shopify Plus" || !got.Valid {
		t.Errorf("status = %+v, want the values returned by the fake API", got)
	}
}

func TestAuthStatusSurfacesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":"[API] Invalid API key or access token"}`))
	}))
	defer srv.Close()

	io, _, _ := iostreams.Test()
	factory := app.NewFactory(io)
	factory.ConfigFunc = func() (*config.Config, error) {
		cfg := config.New()
		cfg.SetProfile("test", &config.Profile{
			Shop: "acme.myshopify.com", AccessToken: "bad", APIVersion: "2026-04",
		})
		return cfg, nil
	}
	factory.ClientFunc = func(context.Context) (*shopify.Client, error) {
		return shopify.New("acme.myshopify.com", "bad", "2026-04",
			shopify.WithBaseURL(srv.URL),
			shopify.WithHTTPClient(srv.Client()),
			shopify.WithMaxRetries(0),
		)
	}

	cmd := auth.NewCommand(factory)
	cmd.SetArgs([]string{"status"})
	cmd.SetOut(io.Out)
	cmd.SetErr(io.ErrOut)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("auth status = nil error, want an unauthorized error")
	}
	apiErr, ok := shopify.AsAPIError(err)
	if !ok {
		t.Fatalf("error = %v (%T), want *shopify.APIError", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("IsUnauthorized() = false for status %d", apiErr.StatusCode)
	}
}

func TestAuthListIsEmptyWithoutProfiles(t *testing.T) {
	io, stdout, _ := iostreams.Test()
	factory := app.NewFactory(io)
	factory.Options.OutputFormat = "json"
	factory.ConfigFunc = func() (*config.Config, error) { return config.New(), nil }

	cmd := auth.NewCommand(factory)
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(io.Out)
	cmd.SetErr(io.ErrOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth list returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"profiles": null`) {
		t.Errorf("expected an empty profile list, got:\n%s", stdout.String())
	}
}
