package webhooks

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

type listOptions struct {
	topics []string
}

func newListCommand(f *app.Factory) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the webhook subscriptions of this access token",
		Args:    cobra.NoArgs,
		Example: `  shopify-tools webhooks list
  shopify-tools webhooks list --topic orders/create --topic ORDERS_UPDATED
  shopify-tools webhooks list -o json | jq '.webhooks[].uri'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), f, opts)
		},
	}

	cmd.Flags().StringSliceVar(&opts.topics, "topic", nil,
		"only list these topics; repeatable, accepts ORDERS_CREATE or orders/create")
	registerTopicCompletion(cmd, f, "topic")
	return cmd
}

func runList(ctx context.Context, f *app.Factory, opts *listOptions) error {
	client, err := f.Client(ctx)
	if err != nil {
		return err
	}

	topics := make([]string, 0, len(opts.topics))
	for _, t := range opts.topics {
		topics = append(topics, NormalizeTopic(t))
	}

	subs, err := listSubscriptions(ctx, client, topics)
	if err != nil {
		return err
	}

	printer, err := f.Printer()
	if err != nil {
		return err
	}
	return printer.Print(SubscriptionList{Webhooks: subs})
}
