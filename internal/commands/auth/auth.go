// Package auth manages the store credentials the other tools use.
//
// It is also the reference implementation for a tool package: one NewCommand
// constructor, one file per subcommand, an options struct per subcommand, and
// a run function that takes a context and writes through the factory's
// IOStreams. Nothing in here reads globals.
package auth

import (
	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

// NewCommand returns the `auth` tool.
func NewCommand(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Manage store credentials and profiles",
		Long:  "Authenticate against a Shopify store and manage the saved profiles.",
		// A tool with no default action shows its own help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newLoginCommand(f),
		newLogoutCommand(f),
		newListCommand(f),
		newStatusCommand(f),
		newUseCommand(f),
	)
	return cmd
}
