package webhooks

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

type createOptions struct {
	spec Spec
}

func newCreateCommand(f *app.Factory) *cobra.Command {
	opts := &createOptions{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a webhook subscription",
		Long: `Create a webhook subscription.

The endpoint may be an HTTPS URL, a Pub/Sub URI (pubsub://{project}:{topic}),
or an Amazon EventBridge event source ARN.`,
		Args: cobra.NoArgs,
		Example: `  shopify-tools webhooks create --topic orders/create --uri https://api.acme.dev/hooks/orders

  # only send the fields you care about, and only for active products
  shopify-tools webhooks create --topic PRODUCTS_UPDATE \
      --uri https://api.acme.dev/hooks/products \
      --include-field id --include-field title \
      --filter "status:active"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCreate(cmd.Context(), f, opts)
		},
	}

	bindSpecFlags(cmd, &opts.spec)
	_ = cmd.MarkFlagRequired("topic")
	_ = cmd.MarkFlagRequired("uri")
	registerTopicCompletion(cmd, f, "topic")
	return cmd
}

func runCreate(ctx context.Context, f *app.Factory, opts *createOptions) error {
	opts.spec.Normalize()
	if err := opts.spec.Validate(); err != nil {
		return err
	}

	client, err := f.Client(ctx)
	if err != nil {
		return err
	}

	sub, err := createSubscription(ctx, client, opts.spec)
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
		fmt.Fprintf(f.IOStreams.ErrOut, "Created %s -> %s\n", sub.Topic, sub.URI)
	}
	return nil
}

// bindSpecFlags wires the flags shared by create and update.
func bindSpecFlags(cmd *cobra.Command, spec *Spec) {
	flags := cmd.Flags()
	flags.StringVar(&spec.Topic, "topic", "", "webhook topic, e.g. ORDERS_CREATE or orders/create")
	flags.StringVar(&spec.URI, "uri", "", "https URL, pubsub://{project}:{topic}, or an EventBridge ARN")
	flags.StringVar(&spec.Format, "format", "", "payload format: JSON (default) or XML")
	flags.StringVar(&spec.Filter, "filter", "", `only deliver matching events, e.g. "status:active"`)
	flags.StringSliceVar(&spec.IncludeFields, "include-field", nil,
		"restrict the payload to this field; repeatable")
	flags.StringSliceVar(&spec.MetafieldNamespaces, "metafield-namespace", nil,
		"include metafields from this namespace; repeatable")

	_ = cmd.RegisterFlagCompletionFunc("format",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{"JSON", "XML"}, cobra.ShellCompDirectiveNoFileComp
		})
}

// registerTopicCompletion completes a topic flag from the live schema enum.
func registerTopicCompletion(cmd *cobra.Command, f *app.Factory, flag string) {
	_ = cmd.RegisterFlagCompletionFunc(flag,
		func(c *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			client, err := f.Client(c.Context())
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			topics, err := listTopics(c.Context(), client)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return topics, cobra.ShellCompDirectiveNoFileComp
		})
}
