package account_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/commands/account"
	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
)

// The tests in this file run the tool the way a person does: NewCommand with
// no fakes injected, on a real pseudo terminal, driven by keystrokes. Only the
// Shopify CLI itself is a fixture. They are what covers the composition — the
// picker handing a name to the runner handing a result to the config file —
// which the unit tests each cover only one piece of.
type terminalHarness struct {
	master     *os.File
	slave      *os.File
	configPath string
	done       chan error

	mu   sync.Mutex
	seen bytes.Buffer
}

func newTerminalHarness(t *testing.T) *terminalHarness {
	t.Helper()
	installFakeShopify(t) // also skips this test on Windows

	dir := t.TempDir()
	t.Setenv("SHOPIFY_AUTH_CONFIG", filepath.Join(dir, "no-legacy-config.json"))

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open() returned error: %v", err)
	}
	h := &terminalHarness{
		master:     master,
		slave:      slave,
		configPath: filepath.Join(dir, "config.yaml"),
		done:       make(chan error, 1),
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	// Drain the terminal, or a full buffer would block the command's writes.
	go func() {
		buf := make([]byte, 512)
		for {
			n, readErr := master.Read(buf)
			if n > 0 {
				h.mu.Lock()
				h.seen.Write(buf[:n])
				h.mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	return h
}

// start runs the command in the background, exactly as the root command would.
func (h *terminalHarness) start(t *testing.T, args ...string) {
	t.Helper()

	io := iostreams.FromFiles(h.slave, h.slave, h.slave)
	factory := app.NewFactory(io)
	factory.Options.ConfigPath = h.configPath

	cmd := account.NewCommand(factory)
	cmd.SetArgs(args)
	cmd.SetOut(io.Out)
	cmd.SetErr(io.ErrOut)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	go func() { h.done <- cmd.Execute() }()
}

func (h *terminalHarness) output() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seen.String()
}

// waitFor blocks until the terminal shows text, so that keys are only sent
// once whatever is meant to read them is listening.
func (h *terminalHarness) waitFor(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(h.output(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%q never appeared on the terminal; it showed:\n%s", text, h.output())
}

func (h *terminalHarness) press(t *testing.T, keys string) {
	t.Helper()
	if _, err := h.master.WriteString(keys); err != nil {
		t.Fatalf("writing %q to the terminal returned error: %v", keys, err)
	}
}

func (h *terminalHarness) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-h.done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatalf("the command never finished; the terminal showed:\n%s", h.output())
		return nil
	}
}

// saved reads back what the next invocation of the CLI would see.
func (h *terminalHarness) saved(t *testing.T) config.CLIAccounts {
	t.Helper()
	cfg, err := config.Load(h.configPath)
	if err != nil {
		t.Fatalf("Load(%s) returned error: %v", h.configPath, err)
	}
	return cfg.CLIAccounts
}

// The whole of `account use`: pick a profile with the arrow keys, have the
// Shopify CLI switch to its account, and save the choice.
func TestUseOnATerminalPicksAProfileAndSwitchesToIt(t *testing.T) {
	h := newTerminalHarness(t)
	seed(t, h.configPath, "",
		config.CLIAccount{Name: "acme", ShopifyAlias: "other@example.test"},
		config.CLIAccount{Name: "work", ShopifyAlias: "known@example.test"},
	)

	h.start(t, "use")
	h.waitFor(t, "Choose a profile")
	h.press(t, "\x1b[B\r") // down to the second profile, then Enter

	if err := h.wait(t); err != nil {
		t.Fatalf("account use returned error: %v\nterminal:\n%s", err, h.output())
	}

	saved := h.saved(t)
	if saved.Current != "work" {
		t.Errorf("current profile = %q, want the one the arrow keys landed on", saved.Current)
	}
	// A profile the fixture does not know would have been re-linked to
	// selected@example.test instead, so this also proves the right one was
	// handed to the Shopify CLI.
	if linked, ok := findAccount(saved.Accounts, "work"); !ok || linked.ShopifyAlias != "known@example.test" {
		t.Errorf("saved accounts = %+v, want work still pointing at known@example.test", saved.Accounts)
	}
	if got := h.output(); !strings.Contains(got, `Using profile "work"`) {
		t.Errorf("terminal = %q, want it to confirm the switch", got)
	}
}

// The whole of creating a profile: the Shopify CLI's own picker runs on a
// pseudo terminal of its own, and whatever it confirms is saved.
func TestUseOnATerminalCreatesAProfileFromShopifysPicker(t *testing.T) {
	h := newTerminalHarness(t)

	h.start(t, "use", "fresh")

	if err := h.wait(t); err != nil {
		t.Fatalf("account use returned error: %v\nterminal:\n%s", err, h.output())
	}

	want := []config.CLIAccount{{Name: "fresh", ShopifyAlias: "selected@example.test"}}
	if saved := h.saved(t); !equal(saved.Accounts, want) {
		t.Errorf("saved accounts = %+v, want %+v", saved.Accounts, want)
	}
	// The Shopify CLI's own prompt has to reach the user, or they would be
	// answering a picker they cannot see.
	if got := h.output(); !strings.Contains(got, "Which account") {
		t.Errorf("terminal = %q, want the Shopify CLI's prompt forwarded to it", got)
	}
}

// The whole of `account use` when there is nothing saved yet: type a name,
// then let Shopify's picker fill in the account.
func TestUseOnATerminalAsksForANameWhenNothingIsSaved(t *testing.T) {
	h := newTerminalHarness(t)

	h.start(t, "use")
	h.waitFor(t, "Profile name:")
	h.press(t, "typed\r")

	if err := h.wait(t); err != nil {
		t.Fatalf("account use returned error: %v\nterminal:\n%s", err, h.output())
	}

	want := []config.CLIAccount{{Name: "typed", ShopifyAlias: "selected@example.test"}}
	if saved := h.saved(t); !equal(saved.Accounts, want) {
		t.Errorf("saved accounts = %+v, want %+v", saved.Accounts, want)
	}
}

// The whole of `account logout`, including answering its confirmation.
func TestLogoutOnATerminalIsConfirmedWithTheArrowKeys(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		h := newTerminalHarness(t)
		seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "known@example.test"})

		h.start(t, "logout")
		h.waitFor(t, "Log out of every Shopify CLI account?")
		h.press(t, "\r") // Enter on "Cancel", the first option

		if err := h.wait(t); err != nil {
			t.Fatalf("account logout returned error: %v\nterminal:\n%s", err, h.output())
		}
		if saved := h.saved(t); len(saved.Accounts) != 1 {
			t.Errorf("saved accounts = %+v, want them kept", saved.Accounts)
		}
	})

	t.Run("confirm", func(t *testing.T) {
		h := newTerminalHarness(t)
		seed(t, h.configPath, "work", config.CLIAccount{Name: "work", ShopifyAlias: "known@example.test"})

		h.start(t, "logout")
		h.waitFor(t, "Log out of every Shopify CLI account?")
		h.press(t, "\x1b[B\r") // down to "Log out", then Enter

		if err := h.wait(t); err != nil {
			t.Fatalf("account logout returned error: %v\nterminal:\n%s", err, h.output())
		}
		if saved := h.saved(t); len(saved.Accounts) != 0 {
			t.Errorf("saved accounts = %+v, want them cleared", saved.Accounts)
		}
		if got := h.output(); !strings.Contains(got, "Logged out of all accounts") {
			t.Errorf("terminal = %q, want the Shopify CLI's own logout output", got)
		}
	})
}
