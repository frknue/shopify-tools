package webhooks

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

type updateOptions struct {
	id   string
	spec Spec
}

func newUpdateCommand(f *app.Factory) *cobra.Command {
	opts := &updateOptions{}

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Change an existing webhook subscription",
		Long: `Change an existing webhook subscription.

Only the flags you pass are sent; anything you omit keeps its current value.
The topic of a subscription cannot be changed — delete it and create a new one.`,
		Args:    cobra.ExactArgs(1),
		Example: "  shopify-tools webhooks update 1234567890 --uri https://api.acme.dev/hooks/orders-v2",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.id = args[0]
			return runUpdate(cmd.Context(), f, cmd, opts)
		},
	}

	bindSpecFlags(cmd, &opts.spec)
	// The topic is fixed for the lifetime of a subscription.
	_ = cmd.Flags().MarkHidden("topic")
	return cmd
}

func runUpdate(ctx context.Context, f *app.Factory, cmd *cobra.Command, opts *updateOptions) error {
	if cmd.Flags().Changed("topic") {
		return fmt.Errorf("a subscription's topic cannot be changed; delete it and create a new one")
	}
	if !anyFlagChanged(cmd, "uri", "format", "filter", "include-field", "metafield-namespace") {
		return fmt.Errorf("nothing to update: pass at least one of --uri, --format, --filter, --include-field, --metafield-namespace")
	}

	opts.spec.Normalize()
	if opts.spec.URI != "" {
		if err := validateURI(opts.spec.URI); err != nil {
			return err
		}
	}
	if format := opts.spec.Format; format != "" && format != "JSON" && format != "XML" {
		return fmt.Errorf("format must be JSON or XML, got %q", format)
	}

	client, err := f.Client(ctx)
	if err != nil {
		return err
	}

	sub, err := updateSubscription(ctx, client, opts.id, opts.spec)
	if err != nil {
		return err
	}

	printer, err := f.Printer()
	if err != nil {
		return err
	}
	if err := printer.Print(sub); err != nil {
		return err
	}
	if !f.Options.Quiet {
		fmt.Fprintf(f.IOStreams.ErrOut, "Updated %s -> %s\n", sub.Topic, sub.URI)
	}
	return nil
}

func anyFlagChanged(cmd *cobra.Command, names ...string) bool {
	for _, n := range names {
		if cmd.Flags().Changed(n) {
			return true
		}
	}
	return false
}
