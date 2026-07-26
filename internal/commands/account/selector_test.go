package account_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/frknue/shopify-tools/internal/commands/account"
	"github.com/frknue/shopify-tools/internal/iostreams"
)

func TestNumberedSelectorRetriesInvalidInput(t *testing.T) {
	io, _, stderr := iostreams.Test()
	io.In = strings.NewReader("nope\n3\n2\n")

	selection, err := account.NewTerminalSelector(io).
		Select(context.Background(), "Choose", []string{"one", "two"})
	if err != nil {
		t.Fatalf("Select() returned error: %v", err)
	}
	if selection != 1 {
		t.Errorf("Select() = %d, want 1", selection)
	}
	if got := strings.Count(stderr.String(), "Enter a number"); got != 2 {
		t.Errorf("rejection messages = %d, want one per invalid answer (2)", got)
	}
}

func TestSelectorStopsOnACancelledContext(t *testing.T) {
	io, _, _ := iostreams.Test()
	io.In = strings.NewReader("1\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := account.NewTerminalSelector(io).Select(ctx, "Choose", []string{"one"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Select() error = %v, want context.Canceled", err)
	}
}

func TestSelectorRejectsAnEmptyOptionList(t *testing.T) {
	io, _, _ := iostreams.Test()
	if _, err := account.NewTerminalSelector(io).Select(context.Background(), "Choose", nil); err == nil {
		t.Error("Select() with no options = nil error, want one")
	}
}

// On a real terminal the picker is driven with the arrow keys, which means
// parsing escape sequences. These run it against a pseudo terminal.
func TestArrowSelectorOnATerminal(t *testing.T) {
	tests := []struct {
		name  string
		keys  string
		want  int
		wantE bool // want it cancelled
	}{
		{name: "down then enter", keys: "\x1b[B\r", want: 1},
		{name: "up wraps to the last option", keys: "\x1b[A\r", want: 2},
		{name: "vi keys", keys: "jj\r", want: 2},
		{name: "emacs keys", keys: "\x0e\r", want: 1},
		{name: "enter takes the first option", keys: "\r", want: 0},
		{name: "ctrl-c cancels", keys: "\x03", wantE: true},
		{name: "bare escape cancels", keys: "\x1b", wantE: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatalf("pty.Open() returned error: %v", err)
			}
			defer func() { _ = master.Close() }()
			defer func() { _ = slave.Close() }()

			io := iostreams.FromFiles(slave, slave, slave)
			if !io.IsStdinTTY() || !io.IsStderrTTY() {
				t.Fatal("the pseudo terminal was not detected as a terminal")
			}

			type outcome struct {
				selection int
				err       error
			}
			done := make(chan outcome, 1)
			go func() {
				selection, selectErr := account.NewTerminalSelector(io).
					Select(context.Background(), "Choose", []string{"one", "two", "three"})
				done <- outcome{selection: selection, err: selectErr}
			}()

			// Let the picker draw and switch the terminal to raw mode first.
			time.Sleep(50 * time.Millisecond)
			if _, err := master.WriteString(tc.keys); err != nil {
				t.Fatalf("writing %q returned error: %v", tc.keys, err)
			}

			select {
			case got := <-done:
				switch {
				case tc.wantE && !errors.Is(got.err, context.Canceled):
					t.Errorf("Select() error = %v, want a cancellation", got.err)
				case !tc.wantE && got.err != nil:
					t.Errorf("Select() returned error: %v", got.err)
				case !tc.wantE && got.selection != tc.want:
					t.Errorf("Select() = %d, want %d", got.selection, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the picker hung; it never read the keys")
			}
		})
	}
}

func TestNumberedSelectorReportsExhaustedInput(t *testing.T) {
	io, _, _ := iostreams.Test()
	io.In = strings.NewReader("") // a script that pipes nothing in

	_, err := account.NewTerminalSelector(io).
		Select(context.Background(), "Choose", []string{"one", "two"})
	if err == nil {
		t.Error("Select() with no input = nil error, want it to give up rather than spin")
	}
}
