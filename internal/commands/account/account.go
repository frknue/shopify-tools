// Package account switches the Shopify CLI between accounts using local
// profile names.
//
// It stores no credentials. The Shopify CLI owns and refreshes those; this
// tool records only the mapping between a profile name such as "work" and the
// account the Shopify CLI reports, then drives `shopify auth login` to switch.
// That keeps it independent of the CLI's private credential format and of the
// operating system keychain.
//
// The `auth` tool is the unrelated one: it holds Admin API tokens that the
// other tools in this binary call the API with.
package account

import (
	"bufio"
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

// Runner performs the Shopify CLI operations this tool delegates to. It is an
// interface so that tests can exercise the commands without a terminal, a
// login, or the Shopify CLI on PATH.
type Runner interface {
	// SelectAccount opens the Shopify CLI's own account picker and returns the
	// account it confirms as current.
	SelectAccount(ctx context.Context) (alias string, err error)
	// SwitchAccount selects an account the Shopify CLI already knows about.
	SwitchAccount(ctx context.Context, alias string) error
	// Logout ends every Shopify CLI session.
	Logout(ctx context.Context) error
}

// Selector asks the user to choose one of several options.
type Selector interface {
	Select(ctx context.Context, title string, options []string) (int, error)
}

// Deps holds the collaborators that talk to the terminal and to the Shopify
// CLI. Nil fields fall back to the real implementations; tests replace them,
// the way app.Factory's ClientFunc is replaced for the API.
type Deps struct {
	Runner   Runner
	Selector Selector
}

const accountLong = `Switch the Shopify CLI between accounts using memorable profile names.

Authentication is delegated to the Shopify CLI's own multi-account support, so
no access token is read, copied or stored here — only the mapping from a
profile name to the account Shopify confirms.

Requires the Shopify CLI on PATH:

  npm install -g @shopify/cli@latest`

// NewCommand returns the `account` tool.
func NewCommand(f *app.Factory) *cobra.Command { return NewCommandWithDeps(f, Deps{}) }

// NewCommandWithDeps is NewCommand with injectable collaborators. It is the
// seam tests use; production code calls NewCommand.
func NewCommandWithDeps(f *app.Factory, d Deps) *cobra.Command {
	t := newTool(f, d)

	cmd := &cobra.Command{
		Use:   "account <command>",
		Short: "Switch the Shopify CLI between accounts",
		Long:  accountLong,
		// A tool with no default action shows its own help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newUseCommand(t),
		newListCommand(t),
		newLogoutCommand(t),
	)
	return cmd
}

// tool bundles what every subcommand of this package needs.
type tool struct {
	f        *app.Factory
	store    *configStore
	runner   Runner
	selector Selector
	in       *bufio.Reader
}

func newTool(f *app.Factory, d Deps) *tool {
	t := &tool{
		f:        f,
		store:    newConfigStore(f),
		runner:   d.Runner,
		selector: d.Selector,
		in:       bufio.NewReader(f.IOStreams.In),
	}
	if t.runner == nil {
		t.runner = newExecRunner(f.IOStreams)
	}
	if t.selector == nil {
		t.selector = newTerminalSelector(f.IOStreams)
	}
	return t
}

// prompt writes an interactive line to stderr. Unlike status it is not
// silenced by --quiet, because the user is about to be asked something.
func (t *tool) prompt(format string, args ...any) {
	fmt.Fprintf(t.f.IOStreams.ErrOut, format, args...)
}

// status reports what happened, on stderr, so that stdout stays machine
// readable.
func (t *tool) status(format string, args ...any) {
	if t.f.Options.Quiet {
		return
	}
	fmt.Fprintf(t.f.IOStreams.ErrOut, format, args...)
}
