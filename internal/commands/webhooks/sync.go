package webhooks

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

type syncOptions struct {
	file   string
	prune  bool
	dryRun bool
	yes    bool
}

const syncLong = `Make the store's webhooks match a manifest.

The manifest is the desired state. Entries it declares are created, entries
that differ are updated, and — only with --prune — subscriptions the manifest
does not mention are deleted. Running sync twice makes no second round of
changes.

A subscription is identified by its topic and endpoint together, because that
is the only stable identity the API exposes. Changing a uri therefore reads as
a new subscription rather than an edit of the old one.

Manifest format:

    webhooks:
      - topic: ORDERS_CREATE
        uri: https://api.acme.dev/hooks/orders
        include_fields: [id, total_price]
      - topic: PRODUCTS_UPDATE
        uri: https://api.acme.dev/hooks/products
        filter: "status:active"`

func newSyncCommand(f *app.Factory) *cobra.Command {
	opts := &syncOptions{}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Apply a webhook manifest to the store",
		Long:  syncLong,
		Args:  cobra.NoArgs,
		Example: `  shopify-tools webhooks sync --file webhooks.yaml
  shopify-tools webhooks sync --file webhooks.yaml --prune --yes
  shopify-tools webhooks sync --file webhooks.yaml --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd.Context(), f, opts)
		},
	}

	bindManifestFlags(cmd, &opts.file, &opts.prune)
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "show the plan without applying it")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func runSync(ctx context.Context, f *app.Factory, opts *syncOptions) error {
	plan, err := buildPlanFromManifest(ctx, f, opts.file, opts.prune)
	if err != nil {
		return err
	}

	if opts.dryRun {
		return printPlan(f, plan)
	}
	if !plan.HasChanges() {
		fmt.Fprintln(f.IOStreams.ErrOut, plan.Summary())
		return printResult(f, Result{})
	}

	// Always show what is about to happen before touching anything.
	fmt.Fprintln(f.IOStreams.ErrOut, "Planned changes:")
	if err := printPlanTable(f, plan); err != nil {
		return err
	}

	if !opts.yes {
		ok, err := confirm(f, "Apply these changes?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(f.IOStreams.ErrOut, "Aborted.")
			return nil
		}
	}

	result, err := applyPlan(ctx, f, plan)
	// Report what did happen even when a later change failed.
	if printErr := printResult(f, result); printErr != nil && err == nil {
		return printErr
	}
	return err
}

// Result records what sync actually did.
type Result struct {
	Created []string `json:"created,omitempty" yaml:"created,omitempty"`
	Updated []string `json:"updated,omitempty" yaml:"updated,omitempty"`
	Deleted []string `json:"deleted,omitempty" yaml:"deleted,omitempty"`
}

// Headers implements output.Tabler.
func (r Result) Headers() []string { return []string{"ACTION", "WEBHOOK"} }

// Rows implements output.Tabler.
func (r Result) Rows() [][]string {
	rows := make([][]string, 0, len(r.Created)+len(r.Updated)+len(r.Deleted))
	for _, v := range r.Created {
		rows = append(rows, []string{"created", v})
	}
	for _, v := range r.Updated {
		rows = append(rows, []string{"updated", v})
	}
	for _, v := range r.Deleted {
		rows = append(rows, []string{"deleted", v})
	}
	return rows
}

// applyPlan executes a plan, stopping at the first failure and returning what
// it managed to apply.
func applyPlan(ctx context.Context, f *app.Factory, plan Plan) (Result, error) {
	var result Result

	client, err := f.Client(ctx)
	if err != nil {
		return result, err
	}

	for _, change := range plan.Changes {
		label := change.Topic + " -> " + change.URI

		switch change.Action {
		case ActionCreate:
			if _, err := createSubscription(ctx, client, *change.Spec); err != nil {
				return result, fmt.Errorf("create %s: %w", label, err)
			}
			result.Created = append(result.Created, label)

		case ActionUpdate:
			if _, err := updateSubscription(ctx, client, change.ID, *change.Spec); err != nil {
				return result, fmt.Errorf("update %s: %w", label, err)
			}
			result.Updated = append(result.Updated, label)

		case ActionDelete:
			if _, err := deleteSubscription(ctx, client, change.ID); err != nil {
				return result, fmt.Errorf("delete %s: %w", label, err)
			}
			result.Deleted = append(result.Deleted, label)

		case ActionUnchanged:
			// Nothing to do.
		}
	}
	return result, nil
}

func printResult(f *app.Factory, result Result) error {
	printer, err := f.Printer()
	if err != nil {
		return err
	}
	return printer.Print(result)
}

// printPlanTable renders the pending changes to stderr, so that the result on
// stdout stays clean for piping.
func printPlanTable(f *app.Factory, plan Plan) error {
	visible := Plan{Pruned: plan.Pruned}
	for _, c := range plan.Changes {
		if c.Action != ActionUnchanged {
			visible.Changes = append(visible.Changes, c)
		}
	}
	if err := renderTable(f, visible); err != nil {
		return err
	}
	fmt.Fprintln(f.IOStreams.ErrOut, plan.Summary())
	return nil
}
