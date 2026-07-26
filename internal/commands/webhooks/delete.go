package webhooks

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

func newDeleteCommand(f *app.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:     "delete <id>...",
		Aliases: []string{"rm"},
		Short:   "Delete webhook subscriptions",
		Args:    cobra.MinimumNArgs(1),
		Example: "  shopify-tools webhooks delete 1234567890 --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			client, err := f.Client(ctx)
			if err != nil {
				return err
			}

			if !yes {
				ok, err := confirm(f, fmt.Sprintf("Delete %d webhook subscription(s)?", len(args)))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(f.IOStreams.ErrOut, "Aborted.")
					return nil
				}
			}

			for _, id := range args {
				deleted, err := deleteSubscription(ctx, client, id)
				if err != nil {
					return err
				}
				if !f.Options.Quiet {
					fmt.Fprintf(f.IOStreams.ErrOut, "Deleted %s\n", shortID(deleted))
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
