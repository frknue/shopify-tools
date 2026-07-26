package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/commands/account"
	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
)

// fakeRunner stands in for the Shopify CLI. No test may ever reach the real
// one: it would open a browser and change the developer's login.
type fakeRunner struct {
	selectedAlias string
	selectErr     error
	switchErr     error
	logoutErr     error
	selectCalls   int
	switched      []string
	logoutCalls   int
}

func (r *fakeRunner) SelectAccount(context.Context) (string, error) {
	r.selectCalls++
	return r.selectedAlias, r.selectErr
}

func (r *fakeRunner) SwitchAccount(_ context.Context, alias string) error {
	r.switched = append(r.switched, alias)
	return r.switchErr
}

func (r *fakeRunner) Logout(context.Context) error {
	r.logoutCalls++
	return r.logoutErr
}

// fakeSelector picks a fixed entry and records what it was offered.
type fakeSelector struct {
	choice  int
	err     error
	options []string
	title   string
}

func (s *fakeSelector) Select(_ context.Context, title string, options []string) (int, error) {
	s.title, s.options = title, options
	if s.err != nil {
		return 0, s.err
	}
	return s.choice, nil
}

type harness struct {
	cmd        *cobra.Command
	factory    *app.Factory
	stdout     *bytes.Buffer
	stderr     *bytes.Buffer
	configPath string
}

// newHarness builds the tool against a throwaway config file. It also points
// the legacy import at an empty directory, so a test never picks up the
// profiles of a shopify-auth installed on the machine running it.
func newHarness(t *testing.T, deps account.Deps, stdin string) *harness {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("SHOPIFY_AUTH_CONFIG", filepath.Join(dir, "no-legacy-config.json"))
	return newHarnessAt(t, filepath.Join(dir, "config.yaml"), deps, stdin)
}

// newHarnessAt builds the tool against an existing config path, so that a test
// can run a second command the way a second invocation of the CLI would.
func newHarnessAt(t *testing.T, configPath string, deps account.Deps, stdin string) *harness {
	t.Helper()

	io, stdout, stderr := iostreams.Test()
	io.In = strings.NewReader(stdin)

	factory := app.NewFactory(io)
	factory.Options.ConfigPath = configPath
	factory.Options.OutputFormat = "json"

	cmd := account.NewCommandWithDeps(factory, deps)
	cmd.SetOut(io.Out)
	cmd.SetErr(io.ErrOut)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	return &harness{cmd: cmd, factory: factory, stdout: stdout, stderr: stderr, configPath: configPath}
}

func (h *harness) run(t *testing.T, args ...string) error {
	t.Helper()
	h.cmd.SetArgs(args)
	return h.cmd.Execute()
}

// saved reads the profiles back from disk, which is what the next invocation
// of the CLI would see.
func (h *harness) saved(t *testing.T) config.CLIAccounts {
	t.Helper()
	cfg, err := config.Load(h.configPath)
	if err != nil {
		t.Fatalf("Load(%s) returned error: %v", h.configPath, err)
	}
	return cfg.CLIAccounts
}

// seed writes a config file with the given profiles already saved.
func seed(t *testing.T, path, current string, accounts ...config.CLIAccount) {
	t.Helper()
	saved := config.CLIAccounts{Current: current}
	saved.Accounts = append(saved.Accounts, accounts...)
	seedAccounts(t, path, saved)
}

// seedAccounts is seed for states that use is never asked to produce, such as
// a profile still waiting to be linked.
func seedAccounts(t *testing.T, path string, accounts config.CLIAccounts) {
	t.Helper()
	cfg := config.New()
	cfg.SetPath(path)
	cfg.CLIAccounts = accounts
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
}

func TestUseSwitchesToTheSavedAccount(t *testing.T) {
	runner := &fakeRunner{}
	h := newHarness(t, account.Deps{Runner: runner}, "")
	seed(t, h.configPath, "acme", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

	if err := h.run(t, "use", "work"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}

	if want := []string{"dev@example.test"}; !equal(runner.switched, want) {
		t.Errorf("switched accounts = %v, want %v", runner.switched, want)
	}
	if runner.selectCalls != 0 {
		t.Errorf("the account picker opened %d times for a saved profile", runner.selectCalls)
	}
	if saved := h.saved(t); saved.Current != "work" {
		t.Errorf("current profile = %q, want %q", saved.Current, "work")
	}
	if got := h.stderr.String(); !strings.Contains(got, `Using profile "work" (dev@example.test)`) {
		t.Errorf("stderr = %q, want it to name the profile and account", got)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want status messages on stderr only", h.stdout)
	}
}

func TestUseUnknownNameLinksWhateverShopifyConfirms(t *testing.T) {
	runner := &fakeRunner{selectedAlias: "dev@example.test"}
	h := newHarness(t, account.Deps{Runner: runner}, "")

	if err := h.run(t, "use", "work"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}

	if runner.selectCalls != 1 {
		t.Errorf("SelectAccount calls = %d, want 1", runner.selectCalls)
	}
	saved := h.saved(t)
	want := []config.CLIAccount{{Name: "work", ShopifyAlias: "dev@example.test"}}
	if !equal(saved.Accounts, want) {
		t.Errorf("saved accounts = %+v, want %+v", saved.Accounts, want)
	}
	if saved.Current != "work" {
		t.Errorf("current profile = %q, want %q", saved.Current, "work")
	}
}

func TestUseRelinksAProfileShopifyNoLongerKnows(t *testing.T) {
	runner := &fakeRunner{
		selectedAlias: "new@example.test",
		switchErr:     &account.AliasUnavailableError{Alias: "old@example.test"},
	}
	h := newHarness(t, account.Deps{Runner: runner}, "")
	seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "old@example.test"})

	if err := h.run(t, "use", "work"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}

	want := []config.CLIAccount{{Name: "work", ShopifyAlias: "new@example.test"}}
	if saved := h.saved(t); !equal(saved.Accounts, want) {
		t.Errorf("saved accounts = %+v, want the profile re-linked to %+v", saved.Accounts, want)
	}
	if got := h.stderr.String(); !strings.Contains(got, "Choose it again") {
		t.Errorf("stderr = %q, want an explanation before the picker opens", got)
	}
}

func TestUseDoesNotRecordAFailedSwitch(t *testing.T) {
	wantErr := errors.New("shopify exploded")
	runner := &fakeRunner{switchErr: wantErr}
	h := newHarness(t, account.Deps{Runner: runner}, "")
	seed(t, h.configPath, "other", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

	err := h.run(t, "use", "work")
	if !errors.Is(err, wantErr) {
		t.Fatalf("account use error = %v, want %v", err, wantErr)
	}
	if saved := h.saved(t); saved.Current != "other" {
		t.Errorf("current profile = %q, want the failed switch not to change it", saved.Current)
	}
	if runner.selectCalls != 0 {
		t.Error("a generic failure opened the account picker")
	}
}

func TestUseWithoutNameOffersTheSavedProfiles(t *testing.T) {
	runner := &fakeRunner{}
	selector := &fakeSelector{choice: 1}
	h := newHarness(t, account.Deps{Runner: runner, Selector: selector}, "")
	seed(t, h.configPath, "",
		config.CLIAccount{Name: "acme", ShopifyAlias: "dev@acme.test"},
		config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"},
	)

	if err := h.run(t, "use"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}

	wantOptions := []string{"acme (dev@acme.test)", "work (dev@example.test)", "Add account"}
	if !equal(selector.options, wantOptions) {
		t.Errorf("offered options = %v, want %v", selector.options, wantOptions)
	}
	if want := []string{"dev@example.test"}; !equal(runner.switched, want) {
		t.Errorf("switched accounts = %v, want %v", runner.switched, want)
	}
}

func TestUseWithoutNameCanAddAProfile(t *testing.T) {
	runner := &fakeRunner{selectedAlias: "dev@example.test"}
	selector := &fakeSelector{choice: 1} // "Add account"
	h := newHarness(t, account.Deps{Runner: runner, Selector: selector}, "work\n")
	seed(t, h.configPath, "", config.CLIAccount{Name: "acme", ShopifyAlias: "dev@acme.test"})

	if err := h.run(t, "use"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}

	saved := h.saved(t)
	if _, ok := findAccount(saved.Accounts, "work"); !ok {
		t.Errorf("saved accounts = %+v, want the new profile among them", saved.Accounts)
	}
	if saved.Current != "work" {
		t.Errorf("current profile = %q, want the new one", saved.Current)
	}
}

func TestUseAsksAgainForAnInvalidProfileName(t *testing.T) {
	runner := &fakeRunner{selectedAlias: "dev@example.test"}
	h := newHarness(t, account.Deps{Runner: runner}, "not a name\nwork\n")

	if err := h.run(t, "use"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}
	if got := h.stderr.String(); !strings.Contains(got, "Invalid profile name") {
		t.Errorf("stderr = %q, want the invalid name to be rejected", got)
	}
	if saved := h.saved(t); saved.Current != "work" {
		t.Errorf("current profile = %q, want %q", saved.Current, "work")
	}
}

func TestUseRejectsAnInvalidNameWithoutRunningShopify(t *testing.T) {
	runner := &fakeRunner{}
	h := newHarness(t, account.Deps{Runner: runner}, "")

	if err := h.run(t, "use", "bad name"); err == nil {
		t.Fatal("account use accepted a profile name with a space")
	}
	if runner.selectCalls != 0 || len(runner.switched) != 0 {
		t.Error("an invalid profile name reached the Shopify CLI")
	}
}

func TestListReportsProfilesAndTheCurrentOne(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
	seed(t, h.configPath, "work",
		config.CLIAccount{Name: "acme", ShopifyAlias: "dev@acme.test"},
		config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"},
	)

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}

	var got account.AccountList
	if err := json.Unmarshal(h.stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, h.stdout)
	}
	if got.Current != "work" || len(got.Accounts) != 2 {
		t.Fatalf("list = %+v, want both profiles and work current", got)
	}
	if got.Accounts[0].Name != "acme" || !got.Accounts[0].Linked || got.Accounts[0].Current {
		t.Errorf("first entry = %+v, want acme linked and not current", got.Accounts[0])
	}
	if !got.Accounts[1].Current {
		t.Errorf("second entry = %+v, want it marked current", got.Accounts[1])
	}
}

func TestListIsEmptyWithoutProfiles(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}
	if !strings.Contains(h.stdout.String(), `"accounts": null`) {
		t.Errorf("output = %s, want an empty list", h.stdout)
	}
}

func TestLogoutIsConfirmedFirst(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		runner := &fakeRunner{}
		selector := &fakeSelector{choice: 0} // "Cancel"
		h := newHarness(t, account.Deps{Runner: runner, Selector: selector}, "")
		seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

		if err := h.run(t, "logout"); err != nil {
			t.Fatalf("account logout returned error: %v (stderr: %s)", err, h.stderr)
		}
		if runner.logoutCalls != 0 {
			t.Error("logout ran after being cancelled")
		}
		if saved := h.saved(t); len(saved.Accounts) != 1 {
			t.Errorf("saved accounts = %+v, want them kept", saved.Accounts)
		}
	})

	t.Run("confirm", func(t *testing.T) {
		runner := &fakeRunner{}
		selector := &fakeSelector{choice: 1} // "Log out"
		h := newHarness(t, account.Deps{Runner: runner, Selector: selector}, "")
		seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

		if err := h.run(t, "logout"); err != nil {
			t.Fatalf("account logout returned error: %v (stderr: %s)", err, h.stderr)
		}
		if runner.logoutCalls != 1 {
			t.Errorf("Logout calls = %d, want 1", runner.logoutCalls)
		}
		if saved := h.saved(t); len(saved.Accounts) != 0 || saved.Current != "" {
			t.Errorf("saved accounts = %+v, want them cleared", saved)
		}
	})
}

func TestLogoutSkipsTheQuestionWhenTold(t *testing.T) {
	for _, flag := range []string{"--yes", "--force"} {
		t.Run(flag, func(t *testing.T) {
			runner := &fakeRunner{}
			selector := &fakeSelector{err: errors.New("the question was asked")}
			h := newHarness(t, account.Deps{Runner: runner, Selector: selector}, "")

			if err := h.run(t, "logout", flag); err != nil {
				t.Fatalf("account logout %s returned error: %v", flag, err)
			}
			if runner.logoutCalls != 1 {
				t.Errorf("Logout calls = %d, want 1", runner.logoutCalls)
			}
		})
	}
}

func TestLogoutKeepsTheStoreCredentials(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
	cfg := config.New()
	cfg.SetPath(h.configPath)
	cfg.SetProfile("production", &config.Profile{Shop: "acme.myshopify.com", AccessToken: "shpat_test"})
	cfg.SetCLIAccount(config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	if err := h.run(t, "logout", "--yes"); err != nil {
		t.Fatalf("account logout returned error: %v (stderr: %s)", err, h.stderr)
	}

	reloaded, err := config.Load(h.configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(reloaded.CLIAccounts.Accounts) != 0 {
		t.Errorf("cli accounts = %+v, want them cleared", reloaded.CLIAccounts.Accounts)
	}
	if p := reloaded.Profiles["production"]; p == nil || p.AccessToken != "shpat_test" {
		t.Errorf("store profile = %+v, want the Admin API credentials untouched", p)
	}
}

func TestLegacyProfilesAreImportedOnce(t *testing.T) {
	runner := &fakeRunner{}
	h := newHarness(t, account.Deps{Runner: runner, Selector: &fakeSelector{choice: 1}}, "")
	legacy := writeLegacyConfig(t, `{
		"version": 1,
		"accounts": [{"name": "work", "shopify_alias": "dev@example.test"}],
		"current": "work"
	}`)

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}
	saved := h.saved(t)
	want := []config.CLIAccount{{Name: "work", ShopifyAlias: "dev@example.test"}}
	if !equal(saved.Accounts, want) {
		t.Fatalf("saved accounts = %+v, want %+v imported from %s", saved.Accounts, want, legacy)
	}
	if saved.Current != "work" {
		t.Errorf("current profile = %q, want it carried over", saved.Current)
	}
	if !strings.Contains(h.stderr.String(), "Imported 1 profile") {
		t.Errorf("stderr = %q, want the import to be announced", h.stderr)
	}

	// A logout empties the list; the next invocation must not resurrect it
	// from the legacy file, which is still there.
	if err := h.run(t, "logout", "--yes"); err != nil {
		t.Fatalf("account logout returned error: %v", err)
	}
	next := newHarnessAt(t, h.configPath, account.Deps{Runner: runner}, "")
	if err := next.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v", err)
	}
	if again := next.saved(t); len(again.Accounts) != 0 {
		t.Errorf("saved accounts = %+v, want the import to run only once", again.Accounts)
	}
}

func TestLegacyNameOnlyProfilesNeedLinking(t *testing.T) {
	runner := &fakeRunner{selectedAlias: "dev@example.test"}
	h := newHarness(t, account.Deps{Runner: runner}, "")
	writeLegacyConfig(t, `{"accounts": ["work", "personal"]}`)

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}

	var list account.AccountList
	if err := json.Unmarshal(h.stdout.Bytes(), &list); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, h.stdout)
	}
	if len(list.Accounts) != 2 {
		t.Fatalf("list = %+v, want both names carried over", list.Accounts)
	}
	for _, item := range list.Accounts {
		if item.Linked {
			t.Errorf("%q reported as linked, want it pending a Shopify account", item.Name)
		}
	}

	// Using one links it to whatever Shopify confirms.
	if err := h.run(t, "use", "work"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}
	saved := h.saved(t)
	if linked, ok := findAccount(saved.Accounts, "work"); !ok || linked.ShopifyAlias != "dev@example.test" {
		t.Errorf("saved accounts = %+v, want work linked", saved.Accounts)
	}
	if len(saved.Pending) != 1 || saved.Pending[0] != "personal" {
		t.Errorf("pending = %v, want only personal left", saved.Pending)
	}
}

func TestLegacyImportIgnoresUnusableEntries(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
	writeLegacyConfig(t, `{
		"version": 1,
		"accounts": [
			{"name": "work", "shopify_alias": "dev@example.test"},
			{"name": "work", "shopify_alias": "duplicate@example.test"},
			{"name": "no alias", "shopify_alias": ""}
		],
		"pending_profiles": ["work", "legacy"],
		"current": "gone"
	}`)

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}

	saved := h.saved(t)
	want := []config.CLIAccount{{Name: "work", ShopifyAlias: "dev@example.test"}}
	if !equal(saved.Accounts, want) {
		t.Errorf("saved accounts = %+v, want %+v", saved.Accounts, want)
	}
	if !equal(saved.Pending, []string{"legacy"}) {
		t.Errorf("pending = %v, want only the unlinked name", saved.Pending)
	}
	if saved.Current != "" {
		t.Errorf("current = %q, want it dropped because no such profile exists", saved.Current)
	}
}

func TestNoLegacyConfigIsNotAnError(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}
	if _, err := os.Stat(h.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config file was written by a read-only command: %v", err)
	}
}

// The picker is replaced in the tests above; this one drives the real one,
// which falls back to a numbered list because the test streams are not a
// terminal.
func TestTheRealSelectorFallsBackToANumberedList(t *testing.T) {
	runner := &fakeRunner{}
	h := newHarness(t, account.Deps{Runner: runner}, "2\n")
	seed(t, h.configPath, "",
		config.CLIAccount{Name: "acme", ShopifyAlias: "dev@acme.test"},
		config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"},
	)

	if err := h.run(t, "use"); err != nil {
		t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
	}
	if got := h.stderr.String(); !strings.Contains(got, "2) work (dev@example.test)") {
		t.Errorf("stderr = %q, want a numbered list", got)
	}
	if want := []string{"dev@example.test"}; !equal(runner.switched, want) {
		t.Errorf("switched accounts = %v, want %v", runner.switched, want)
	}
}

func TestCurrentAccountIsReadFromTheShopifyCLIOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{
			name:   "plain line",
			output: "Current account: dev@example.test.\n",
			want:   "dev@example.test",
		},
		{
			name:   "with ansi decoration",
			output: "\x1b[32m✔\x1b[0m \x1b[1mCurrent account:\x1b[0m dev@example.test.\r\n",
			want:   "dev@example.test",
		},
		{
			name:   "the last one wins",
			output: "Current account: old@example.test.\nCurrent account: new@example.test.\n",
			want:   "new@example.test",
		},
		{
			name:    "nothing reported",
			output:  "Logged in.\n",
			wantErr: true,
		},
		{
			name:    "empty account",
			output:  "Current account: \n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := account.CurrentAccountFromOutput(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("CurrentAccountFromOutput(%q) = %q, want an error", tc.output, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CurrentAccountFromOutput(%q) returned error: %v", tc.output, err)
			}
			if got != tc.want {
				t.Errorf("CurrentAccountFromOutput(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

func writeLegacyConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	t.Setenv("SHOPIFY_AUTH_CONFIG", path)
	return path
}

func findAccount(accounts []config.CLIAccount, name string) (config.CLIAccount, bool) {
	for _, a := range accounts {
		if a.Name == name {
			return a, true
		}
	}
	return config.CLIAccount{}, false
}

func equal[T comparable](got, want []T) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Logging out must not first import the profiles it is about to delete.
func TestLogoutDoesNotImportLegacyProfilesFirst(t *testing.T) {
	runner := &fakeRunner{}
	h := newHarness(t, account.Deps{Runner: runner}, "")
	writeLegacyConfig(t, `{
		"version": 1,
		"accounts": [{"name": "work", "shopify_alias": "dev@example.test"}],
		"current": "work"
	}`)

	if err := h.run(t, "logout", "--yes"); err != nil {
		t.Fatalf("account logout returned error: %v (stderr: %s)", err, h.stderr)
	}
	if strings.Contains(h.stderr.String(), "Imported") {
		t.Errorf("stderr = %q, want no import while logging out", h.stderr)
	}

	next := newHarnessAt(t, h.configPath, account.Deps{Runner: runner}, "")
	if err := next.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v", err)
	}
	if saved := next.saved(t); len(saved.Accounts) != 0 {
		t.Errorf("saved accounts = %+v, want a logout to be final", saved.Accounts)
	}
}

func TestListRendersATable(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
	h.factory.Options.OutputFormat = "table"
	seedAccounts(t, h.configPath, config.CLIAccounts{
		Current:  "work",
		Accounts: []config.CLIAccount{{Name: "work", ShopifyAlias: "dev@example.test"}},
		Pending:  []string{"legacy"},
	})

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}

	lines := strings.Split(strings.TrimRight(h.stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("output has %d lines, want a header and two profiles:\n%s", len(lines), h.stdout)
	}
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "SHOPIFY ACCOUNT") {
		t.Errorf("header = %q, want the column names", lines[0])
	}
	if !strings.HasPrefix(lines[1], "*") || !strings.Contains(lines[1], "dev@example.test") {
		t.Errorf("current profile row = %q, want it marked with *", lines[1])
	}
	if !strings.HasPrefix(lines[2], "!") || !strings.Contains(lines[2], "not linked yet") {
		t.Errorf("pending profile row = %q, want it flagged as needing a link", lines[2])
	}
}

func TestQuietSilencesTheStatusButNotThePrompts(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
		h.factory.Options.Quiet = true
		seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

		if err := h.run(t, "use", "work"); err != nil {
			t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
		}
		if h.stderr.Len() != 0 {
			t.Errorf("stderr = %q, want --quiet to silence it", h.stderr)
		}
	})

	t.Run("prompt", func(t *testing.T) {
		h := newHarness(t, account.Deps{Runner: &fakeRunner{selectedAlias: "dev@example.test"}}, "")
		h.factory.Options.Quiet = true

		if err := h.run(t, "use", "work"); err != nil {
			t.Fatalf("account use returned error: %v (stderr: %s)", err, h.stderr)
		}
		if !strings.Contains(h.stderr.String(), "Creating profile") {
			t.Errorf("stderr = %q, want the lead-in to the picker even under --quiet", h.stderr)
		}
	})
}

func TestAPickerAnsweringOutOfRangeIsAnErrorNotAPanic(t *testing.T) {
	h := newHarness(t, account.Deps{
		Runner:   &fakeRunner{},
		Selector: &fakeSelector{choice: 99},
	}, "")
	seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

	if err := h.run(t, "use"); err == nil {
		t.Error("account use = nil error, want the out-of-range choice reported")
	}
}

// Ctrl-C at either prompt must stay a cancellation all the way out of the
// command, because that is what internal/cli maps onto exit code 130.
func TestCancellingAPromptStaysACancellation(t *testing.T) {
	for _, tc := range []struct{ name, arg string }{
		{name: "use", arg: "use"},
		{name: "logout", arg: "logout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			h := newHarness(t, account.Deps{
				Runner:   runner,
				Selector: &fakeSelector{err: context.Canceled},
			}, "")
			seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "dev@example.test"})

			err := h.run(t, tc.arg)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("account %s error = %v, want context.Canceled", tc.arg, err)
			}
			if runner.logoutCalls != 0 || len(runner.switched) != 0 {
				t.Error("a cancelled prompt still ran the Shopify CLI")
			}
		})
	}
}

// The tests above point SHOPIFY_AUTH_CONFIG at a fixture. This one checks the
// location the import actually uses on a user's machine, since getting it
// wrong would make the takeover silently never happen.
func TestLegacyProfilesAreFoundInTheDefaultLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("this platform has no user config dir: %v", err)
	}
	legacy := filepath.Join(configDir, "shopify-auth", "accounts.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatalf("create the legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacy,
		[]byte(`{"version":1,"accounts":[{"name":"work","shopify_alias":"dev@example.test"}]}`), 0o600); err != nil {
		t.Fatalf("write the legacy config: %v", err)
	}

	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
	t.Setenv("SHOPIFY_AUTH_CONFIG", "") // fall back to the default location

	if err := h.run(t, "list"); err != nil {
		t.Fatalf("account list returned error: %v (stderr: %s)", err, h.stderr)
	}
	want := []config.CLIAccount{{Name: "work", ShopifyAlias: "dev@example.test"}}
	if saved := h.saved(t); !equal(saved.Accounts, want) {
		t.Errorf("saved accounts = %+v, want %+v imported from %s", saved.Accounts, want, legacy)
	}
}

// Whatever the Shopify CLI reports is scraped from its terminal output, so a
// garbled account must never reach the config file.
func TestGarbageFromTheShopifyCLIIsNotSaved(t *testing.T) {
	runner := &fakeRunner{selectedAlias: "dev@example.test\x1b[2K"}
	h := newHarness(t, account.Deps{Runner: runner}, "")

	if err := h.run(t, "use", "work"); err == nil {
		t.Fatal("account use = nil error, want the control characters rejected")
	}
	if saved := h.saved(t); len(saved.Accounts) != 0 {
		t.Errorf("saved accounts = %+v, want nothing written", saved.Accounts)
	}
}

func TestAnUnreadableConfigIsReportedNotIgnored(t *testing.T) {
	h := newHarness(t, account.Deps{Runner: &fakeRunner{}}, "")
	if err := os.WriteFile(h.configPath, []byte("profiles: [this is not: valid yaml\n"), 0o600); err != nil {
		t.Fatalf("write the broken config: %v", err)
	}

	for _, args := range [][]string{{"list"}, {"use", "work"}, {"logout", "--yes"}} {
		err := h.run(t, args...)
		if err == nil {
			t.Errorf("account %v = nil error, want the broken config reported", args)
			continue
		}
		if !strings.Contains(err.Error(), "parse config") {
			t.Errorf("account %v error = %v, want it to name the problem", args, err)
		}
	}
}

// This tool has nothing to do with Admin API tokens, and its automatic import
// means even a read-only command can write the config file. Neither may put a
// token from the environment on disk.
func TestEnvCredentialsAreNotPersistedByThisTool(t *testing.T) {
	t.Setenv("SHOPIFY_TOOLS_SHOP", "acme.myshopify.com")
	t.Setenv("SHOPIFY_TOOLS_ACCESS_TOKEN", "shpat_from_ci")

	h := newHarness(t, account.Deps{Runner: &fakeRunner{selectedAlias: "dev@example.test"}}, "")
	writeLegacyConfig(t, `{"version":1,"accounts":[{"name":"old","shopify_alias":"old@example.test"}]}`)

	for _, args := range [][]string{{"list"}, {"use", "work"}} {
		if err := h.run(t, args...); err != nil {
			t.Fatalf("account %v returned error: %v (stderr: %s)", args, err, h.stderr)
		}
	}

	written, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatalf("ReadFile() returned error: %v", err)
	}
	if strings.Contains(string(written), "shpat_from_ci") {
		t.Errorf("the token from the environment was written to disk:\n%s", written)
	}
	// The tool's own writes still land.
	if saved := h.saved(t); saved.Current != "work" {
		t.Errorf("current profile = %q, want the tool's own change saved", saved.Current)
	}
}
