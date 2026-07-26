package account

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/xpty"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"github.com/frknue/shopify-tools/internal/iostreams"
)

// ansiEscapePattern matches the colour and cursor sequences the Shopify CLI
// draws its picker with, so that its output can be read as plain text. The
// ranges are the parameter, intermediate and final bytes of an ECMA-48 escape
// sequence, plus the operating system commands the CLI sets the title with.
var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\][^\a]*(?:\a|\x1b\\))`)

// drainTimeout bounds how long the pseudo terminal is drained after the child
// exits, so a wedged pipe cannot hang the CLI.
const drainTimeout = 250 * time.Millisecond

// AliasUnavailableError reports that the Shopify CLI no longer knows an
// account a profile points at. The saved profile is then re-linked instead of
// failing, because the session may simply have been logged out elsewhere.
type AliasUnavailableError struct {
	Alias string
}

func (e *AliasUnavailableError) Error() string {
	return fmt.Sprintf("shopify account %q is unavailable; link the profile again", e.Alias)
}

// execRunner drives the real Shopify CLI.
type execRunner struct {
	streams *iostreams.IOStreams
}

func newExecRunner(streams *iostreams.IOStreams) *execRunner {
	return &execRunner{streams: streams}
}

// SelectAccount runs `shopify auth login` on a pseudo terminal so that the
// Shopify CLI draws its own account picker, and returns the account it
// confirms. A pseudo terminal is needed because the CLI refuses to prompt when
// its output is a pipe — and a pipe is what capturing the answer requires.
func (r *execRunner) SelectAccount(ctx context.Context) (alias string, err error) {
	path, err := shopifyPath()
	if err != nil {
		return "", err
	}

	width, height := r.terminalSize()
	pseudoTerminal, err := xpty.NewPty(width, height)
	if err != nil {
		return "", fmt.Errorf("create terminal for Shopify CLI: %w", err)
	}
	defer func() {
		if closeErr := pseudoTerminal.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			err = errors.Join(err, fmt.Errorf("close Shopify CLI terminal: %w", closeErr))
		}
	}()

	if fd, ok := fdOf(r.streams.In); ok && r.streams.IsStdinTTY() {
		previousState, rawErr := term.MakeRaw(fd)
		if rawErr != nil {
			return "", fmt.Errorf("prepare terminal input: %w", rawErr)
		}
		defer func() {
			if restoreErr := term.Restore(fd, previousState); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("restore terminal input: %w", restoreErr))
			}
		}()
	}

	cancelableInput, err := cancelreader.NewReader(r.streams.In)
	if err != nil {
		return "", fmt.Errorf("prepare terminal reader: %w", err)
	}
	defer func() {
		if closeErr := cancelableInput.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close terminal reader: %w", closeErr))
		}
	}()

	cmd := exec.CommandContext(ctx, path, "auth", "login") //nolint:gosec // path is resolved from PATH by exec.LookPath
	if err := pseudoTerminal.Start(cmd); err != nil {
		return "", fmt.Errorf("start Shopify CLI: %w", err)
	}

	// On Unix, closing the parent's slave descriptor lets the master receive
	// EOF once Shopify exits. The child keeps its own duplicated descriptor.
	if unixPTY, ok := pseudoTerminal.(*xpty.UnixPty); ok {
		_ = unixPTY.Slave().Close()
	}

	inputDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(pseudoTerminal, cancelableInput)
		close(inputDone)
	}()

	// The picker is a prompt, so it goes to stderr; a copy is kept to read the
	// account back out of it.
	var captured bytes.Buffer
	outputDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(io.MultiWriter(r.streams.ErrOut, &captured), pseudoTerminal)
		outputDone <- copyErr
	}()

	waitErr := xpty.WaitProcess(ctx, cmd)
	cancelableInput.Cancel()

	var outputErr error
	select {
	case outputErr = <-outputDone:
	case <-time.After(drainTimeout):
		_ = pseudoTerminal.Close()
		outputErr = <-outputDone
	}
	select {
	case <-inputDone:
	case <-time.After(drainTimeout):
	}

	if waitErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("shopify auth login: %w", waitErr)
	}

	alias, parseErr := currentAccountFromOutput(captured.String())
	if parseErr != nil {
		if outputErr != nil && !errors.Is(outputErr, os.ErrClosed) {
			return "", fmt.Errorf("read Shopify CLI output: %w", outputErr)
		}
		return "", parseErr
	}
	return alias, nil
}

// SwitchAccount selects a session the Shopify CLI already holds. It runs
// non-interactively: an account that is gone must be picked again, not
// silently logged into.
func (r *execRunner) SwitchAccount(ctx context.Context, alias string) error {
	path, err := shopifyPath()
	if err != nil {
		return err
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "auth", "login", "--alias="+alias) //nolint:gosec // path is resolved from PATH by exec.LookPath
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &AliasUnavailableError{Alias: alias}
	}

	// Trust the switch only when Shopify says it selected that very account.
	selected, err := currentAccountFromOutput(stdout.String() + "\n" + stderr.String())
	if err != nil || selected != alias {
		return &AliasUnavailableError{Alias: alias}
	}
	return nil
}

// Logout ends every Shopify CLI session, not only the current one: that is
// what `shopify auth logout` does.
func (r *execRunner) Logout(ctx context.Context) error {
	path, err := shopifyPath()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, "auth", "logout") //nolint:gosec // path is resolved from PATH by exec.LookPath
	cmd.Stdin = r.streams.In
	cmd.Stdout = r.streams.ErrOut
	cmd.Stderr = r.streams.ErrOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("shopify auth logout: %w", err)
	}
	return nil
}

// terminalSize returns the size to give the pseudo terminal, defaulting to the
// conventional 80x24 when the output is redirected.
func (r *execRunner) terminalSize() (width, height int) {
	width, height = 80, 24
	if fd, ok := fdOf(r.streams.ErrOut); ok && r.streams.IsStderrTTY() {
		if w, h, err := term.GetSize(fd); err == nil {
			width, height = w, h
		}
	}
	return width, height
}

func shopifyPath() (string, error) {
	path, err := exec.LookPath("shopify")
	if err != nil {
		return "", errors.New("shopify CLI was not found in PATH; install it with `npm install -g @shopify/cli@latest`")
	}
	return path, nil
}

// currentAccountFromOutput reads the account out of the Shopify CLI's own
// "Current account: …" line, scanning backwards so the last one wins.
func currentAccountFromOutput(output string) (string, error) {
	const label = "Current account:"

	cleaned := ansiEscapePattern.ReplaceAllString(output, "")
	lines := strings.FieldsFunc(cleaned, func(character rune) bool {
		return character == '\r' || character == '\n'
	})
	for index := len(lines) - 1; index >= 0; index-- {
		position := strings.Index(lines[index], label)
		if position == -1 {
			continue
		}
		alias := strings.TrimSpace(lines[index][position+len(label):])
		alias = strings.TrimSpace(strings.TrimSuffix(alias, "."))
		if alias != "" {
			return alias, nil
		}
	}
	return "", errors.New("shopify CLI did not report the selected account")
}
