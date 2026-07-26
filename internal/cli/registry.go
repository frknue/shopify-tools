package cli

import (
	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/commands/account"
	"github.com/frknue/shopify-tools/internal/commands/auth"
	"github.com/frknue/shopify-tools/internal/commands/webhooks"
)

// CommandFactory constructs one tool's command tree from the shared factory.
type CommandFactory func(*app.Factory) *cobra.Command

// toolCommands is the single registration point for tools.
//
// To add a tool:
//  1. create internal/commands/<tool>/ with a NewCommand(*app.Factory) *cobra.Command
//  2. add it to the slice below
//
// See docs/adding-a-tool.md.
func toolCommands() []CommandFactory {
	return []CommandFactory{
		account.NewCommand,
		auth.NewCommand,
		webhooks.NewCommand,
		// product.NewCommand,
		// order.NewCommand,
	}
}
