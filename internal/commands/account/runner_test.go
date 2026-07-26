package account_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/frknue/shopify-tools/internal/commands/account"
	"github.com/frknue/shopify-tools/internal/iostreams"
)

// fakeShopifyCLI stands in for the Shopify CLI. Installing it as the only
// entry on PATH is what keeps these tests from touching a real login.
const fakeShopifyCLI = `#!/bin/sh
if [ "$1" != "auth" ]; then
  exit 2
fi
case "$2" in
  login)
    if [ -n "$FAKE_SHOPIFY_FAILS" ]; then
      printf 'the CLI gave up\n' >&2
      exit 3
    fi
    if [ -n "$FAKE_SHOPIFY_SAYS_NOTHING" ]; then
      printf 'Logged in.\n'
      exit 0
    fi
    case "$3" in
      "")
        printf '? Which account would you like to use?\n\033[32m\342\234\224 Current account: selected@example.test.\033[0m\n'
        ;;
      --alias=known@example.test)
        printf '\342\234\224 Current account: known@example.test.\n'
        ;;
      --alias=mismatch@example.test)
        printf '\342\234\224 Current account: another@example.test.\n'
        ;;
      --alias=silent@example.test)
        printf 'Logged in.\n'
        ;;
      --alias=slow@example.test)
        # An absolute path: PATH holds nothing but this fixture.
        /bin/sleep 5
        ;;
      *)
        printf 'unknown alias\n' >&2
        exit 1
        ;;
    esac
    ;;
  logout)
    printf 'Logged out of all accounts.\n'
    ;;
  *)
    exit 2
    ;;
esac
`

// installFakeShopify puts the fake first and only on PATH.
func installFakeShopify(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Shopify CLI fixture is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shopify"), []byte(fakeShopifyCLI), 0o700); err != nil {
		t.Fatalf("install the fake Shopify CLI: %v", err)
	}
	t.Setenv("PATH", dir)
}

// newRunner returns the real runner plus the buffer the Shopify CLI's own
// output is forwarded to.
func newRunner(t *testing.T) (account.Runner, *bytes.Buffer) {
	t.Helper()
	io, _, stderr := iostreams.Test()
	io.In = strings.NewReader("")
	return account.NewExecRunner(io), stderr
}

func TestSwitchAccountAcceptsOnlyTheAccountItAskedFor(t *testing.T) {
	installFakeShopify(t)

	t.Run("the CLI confirms that account", func(t *testing.T) {
		runner, _ := newRunner(t)
		if err := runner.SwitchAccount(context.Background(), "known@example.test"); err != nil {
			t.Errorf("SwitchAccount() returned error: %v", err)
		}
	})

	// A switch that "succeeds" while landing on a different account, or on no
	// account at all, must not be trusted: the profile would then point at
	// someone else's store.
	for _, alias := range []string{
		"mismatch@example.test", // exits 0, reports another account
		"silent@example.test",   // exits 0, reports nothing
		"missing@example.test",  // exits non-zero
	} {
		t.Run(alias, func(t *testing.T) {
			runner, _ := newRunner(t)
			err := runner.SwitchAccount(context.Background(), alias)

			var unavailable *account.AliasUnavailableError
			if !errors.As(err, &unavailable) {
				t.Fatalf("SwitchAccount(%q) error = %v, want AliasUnavailableError", alias, err)
			}
			if unavailable.Alias != alias {
				t.Errorf("reported account = %q, want %q", unavailable.Alias, alias)
			}
		})
	}
}

func TestSelectAccountCapturesWhatTheCLIConfirms(t *testing.T) {
	installFakeShopify(t)
	runner, forwarded := newRunner(t)

	alias, err := runner.SelectAccount(context.Background())
	if err != nil {
		t.Fatalf("SelectAccount() returned error: %v", err)
	}
	if alias != "selected@example.test" {
		t.Errorf("SelectAccount() = %q, want the account the CLI reported", alias)
	}
	if !strings.Contains(forwarded.String(), "Which account") {
		t.Errorf("the CLI's own prompt was not shown to the user: %q", forwarded)
	}
}

func TestLogoutRunsTheCLI(t *testing.T) {
	installFakeShopify(t)
	runner, forwarded := newRunner(t)

	if err := runner.Logout(context.Background()); err != nil {
		t.Fatalf("Logout() returned error: %v", err)
	}
	if !strings.Contains(forwarded.String(), "Logged out") {
		t.Errorf("the CLI's output was not shown to the user: %q", forwarded)
	}
}

// Ctrl-C during a switch must read as a cancellation, not as an account that
// went missing — the CLI maps the former onto exit code 130.
func TestInterruptedSwitchIsACancellation(t *testing.T) {
	installFakeShopify(t)
	runner, _ := newRunner(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := runner.SwitchAccount(ctx, "slow@example.test")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SwitchAccount() error = %v, want context.Canceled", err)
	}
	var unavailable *account.AliasUnavailableError
	if errors.As(err, &unavailable) {
		t.Error("an interrupted switch was mistaken for a missing account, which would re-link the profile")
	}
}

func TestAMissingShopifyCLIIsExplained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Shopify CLI fixture is a shell script")
	}
	t.Setenv("PATH", t.TempDir())
	runner, _ := newRunner(t)

	for name, err := range map[string]error{
		"SwitchAccount": runner.SwitchAccount(context.Background(), "known@example.test"),
		"Logout":        runner.Logout(context.Background()),
	} {
		if err == nil || !strings.Contains(err.Error(), "not found in PATH") {
			t.Errorf("%s() error = %v, want it to say the Shopify CLI is missing", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), "npm install") {
			t.Errorf("%s() error = %v, want it to say how to install the CLI", name, err)
		}
	}
}

func TestSelectAccountSurfacesWhatWentWrong(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		wantMsg string
	}{
		{
			name:    "the CLI fails",
			env:     "FAKE_SHOPIFY_FAILS",
			wantMsg: "shopify auth login",
		},
		{
			name:    "the CLI reports no account",
			env:     "FAKE_SHOPIFY_SAYS_NOTHING",
			wantMsg: "did not report the selected account",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installFakeShopify(t)
			t.Setenv(tc.env, "1")
			runner, _ := newRunner(t)

			alias, err := runner.SelectAccount(context.Background())
			if err == nil {
				t.Fatalf("SelectAccount() = %q, want an error", alias)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("SelectAccount() error = %v, want it to mention %q", err, tc.wantMsg)
			}
		})
	}
}

func TestAnUnavailableAccountExplainsItself(t *testing.T) {
	err := &account.AliasUnavailableError{Alias: "dev@example.test"}
	for _, want := range []string{"dev@example.test", "unavailable", "link the profile again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, want it to mention %q", err.Error(), want)
		}
	}
}
