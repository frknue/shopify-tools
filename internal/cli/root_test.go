package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/cli"
	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
)

// execute runs the CLI exactly the way main does, but against buffers.
// This is the pattern every command test should follow.
func execute(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	io, out, errOut := iostreams.Test()
	factory := app.NewFactory(io)
	root := cli.NewRootCommand(factory)
	root.SetArgs(args)

	code := cli.HandleError(io, root.Execute())
	return out.String(), errOut.String(), code
}

func TestRootHelpSucceeds(t *testing.T) {
	stdout, _, code := execute(t, "--help")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitOK)
	}
	if !strings.Contains(stdout, "auth") {
		t.Errorf("help output does not list the auth tool:\n%s", stdout)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	_, stderr, code := execute(t, "definitely-not-a-tool")
	if code == cli.ExitOK {
		t.Fatal("exit code = 0 for an unknown command, want non-zero")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr = %q, want it to mention the unknown command", stderr)
	}
}

func TestInvalidOutputFormatIsRejected(t *testing.T) {
	_, stderr, code := execute(t, "version", "--output", "xml")
	if code == cli.ExitOK {
		t.Fatal("exit code = 0 for an invalid --output, want non-zero")
	}
	if !strings.Contains(stderr, "invalid output format") {
		t.Errorf("stderr = %q, want it to explain the invalid format", stderr)
	}
}

func TestVersionJSON(t *testing.T) {
	stdout, _, code := execute(t, "version", "--output", "json")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("version --output json did not emit valid JSON: %v\n%s", err, stdout)
	}
	if got["version"] == "" {
		t.Error("version field is empty")
	}
}

func TestAuthListReadsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

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

	stdout, stderr, code := execute(t, "auth", "list", "--config", path)
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "acme.myshopify.com") || !strings.Contains(stdout, "staging") {
		t.Errorf("auth list output missing the configured profile:\n%s", stdout)
	}
}

func TestMissingProfileExitsWithConfigCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.yaml")

	_, stderr, code := execute(t, "auth", "status", "--config", path)
	if code != cli.ExitConfigDefect {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, cli.ExitConfigDefect, stderr)
	}
	if !strings.Contains(stderr, "auth login") {
		t.Errorf("stderr = %q, want it to point at `auth login`", stderr)
	}
}
