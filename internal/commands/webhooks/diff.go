package webhooks

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/exitcode"
	"github.com/frknue/shopify-tools/internal/output"
)

type diffOptions struct {
	file     string
	prune    bool
	exitCode bool
}

func newDiffCommand(f *app.Factory) *cobra.Command {
	opts := &diffOptions{}

	cmd := &cobra.Command{
		Use:     "diff",
		Aliases: []string{"plan"},
		Short:   "Show what sync would change",
		Long: `Compare a manifest against the store and print the resulting plan,
without changing anything.

With --exit-code the command exits 1 when the store has drifted from the
manifest, which makes it usable as a CI check.`,
		Args: cobra.NoArgs,
		Example: `  shopify-tools webhooks diff --file webhooks.yaml
  shopify-tools webhooks diff --file webhooks.yaml --prune --exit-code`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd.Context(), f, opts)
		},
	}

	bindManifestFlags(cmd, &opts.file, &opts.prune)
	cmd.Flags().BoolVar(&opts.exitCode, "exit-code", false, "exit 1 when there are pending changes")
	return cmd
}

func runDiff(ctx context.Context, f *app.Factory, opts *diffOptions) error {
	plan, err := buildPlanFromManifest(ctx, f, opts.file, opts.prune)
	if err != nil {
		return err
	}

	if err := printPlan(f, plan); err != nil {
		return err
	}

	if opts.exitCode && plan.HasChanges() {
		return exitcode.New(exitcode.Error,
			fmt.Errorf("webhooks have drifted from %s: %s", opts.file, plan.Summary()))
	}
	return nil
}

// bindManifestFlags wires the flags shared by diff and sync.
func bindManifestFlags(cmd *cobra.Command, file *string, prune *bool) {
	cmd.Flags().StringVarP(file, "file", "f", "webhooks.yaml", "path to the webhook manifest")
	cmd.Flags().BoolVar(prune, "prune", false,
		"also remove subscriptions that the manifest does not declare")
	_ = cmd.MarkFlagFilename("file", "yaml", "yml")
}

// buildPlanFromManifest loads the manifest, reads live state and diffs them.
func buildPlanFromManifest(ctx context.Context, f *app.Factory, file string, prune bool) (Plan, error) {
	manifest, err := LoadManifest(file)
	if err != nil {
		return Plan{}, err
	}

	client, err := f.Client(ctx)
	if err != nil {
		return Plan{}, err
	}

	live, err := listSubscriptions(ctx, client, nil)
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(manifest.Webhooks, live, prune), nil
}

// printPlan renders a plan, hiding unchanged rows in table mode where they are
// noise, but keeping them in json/yaml where they are data.
func printPlan(f *app.Factory, plan Plan) error {
	printer, err := f.Printer()
	if err != nil {
		return err
	}

	if printer.Format() != output.FormatTable {
		return printer.Print(plan)
	}

	visible := Plan{Pruned: plan.Pruned}
	for _, c := range plan.Changes {
		if c.Action != ActionUnchanged {
			visible.Changes = append(visible.Changes, c)
		}
	}
	if len(visible.Changes) > 0 {
		if err := printer.Print(visible); err != nil {
			return err
		}
	}
	fmt.Fprintln(f.IOStreams.ErrOut, plan.Summary())
	return nil
}
