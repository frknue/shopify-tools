package webhooks

import (
	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

func newGetCommand(f *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "get <id>",
		Short:   "Show one webhook subscription",
		Args:    cobra.ExactArgs(1),
		Example: "  shopify-tools webhooks get 1234567890 -o yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}

			sub, err := getSubscription(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}

			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(sub)
		},
	}
}
