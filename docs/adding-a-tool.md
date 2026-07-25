# Adding a tool

A "tool" is a top-level command such as `auth`, `product` or `theme`. Each one
lives in its own package under `internal/commands/` and is wired in exactly one
place. Adding a tool never requires touching another tool.

## 1. Create the package

```
internal/commands/product/
  product.go   // NewCommand: the tool itself, adds its subcommands
  list.go      // one file per subcommand
  get.go
  product_test.go
```

`product.go`:

```go
// Package product inspects and edits the store's products.
package product

import (
	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

// NewCommand returns the `product` tool.
func NewCommand(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "product <command>",
		Short: "Inspect and edit products",
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newListCommand(f))
	return cmd
}
```

## 2. Write a subcommand

Every subcommand follows the same four-part shape: an options struct, a
constructor that binds flags, a `run` function that takes a `context.Context`,
and a result type that implements `output.Tabler`.

```go
type listOptions struct {
	limit  int
	status string
}

func newListCommand(f *app.Factory) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List products",
		Args:    cobra.NoArgs,
		Example: "  shopify-tools product list --limit 50 -o json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), f, opts)
		},
	}

	cmd.Flags().IntVar(&opts.limit, "limit", 25, "maximum number of products")
	cmd.Flags().StringVar(&opts.status, "status", "", "filter by status: active, draft, archived")
	return cmd
}

func runList(ctx context.Context, f *app.Factory, opts *listOptions) error {
	client, err := f.Client(ctx)
	if err != nil {
		return err
	}

	var resp productsResponse
	if err := client.GraphQL(ctx, shopify.GraphQLRequest{
		Query:     productsQuery,
		Variables: map[string]any{"first": opts.limit},
	}, &resp); err != nil {
		return err
	}

	printer, err := f.Printer()
	if err != nil {
		return err
	}
	return printer.Print(resp.toResult())
}
```

## 3. Register it

Add one line to `internal/cli/registry.go`:

```go
func toolCommands() []CommandFactory {
	return []CommandFactory{
		auth.NewCommand,
		product.NewCommand, // <-- here
	}
}
```

That is the whole wiring. Global flags, config loading, output formats, exit
codes and shell completion come from the root command for free.

## Rules the codebase follows

- **Never touch globals.** Everything a command needs comes from `*app.Factory`.
  That is what makes commands testable.
- **Never write to `os.Stdout` directly.** Use `f.IOStreams.Out` for results and
  `f.IOStreams.ErrOut` for progress/status, so that `-o json` output stays
  pipeable.
- **Never format output by hand.** Build a result type, implement
  `output.Tabler`, and hand it to `f.Printer()`. `--output json|yaml` then works
  automatically.
- **Always thread `cmd.Context()`** into anything doing I/O, so Ctrl-C cancels
  in-flight requests.
- **Return errors, don't exit.** `internal/cli/errors.go` maps them onto exit
  codes; wrap with `cli.NewExitError(code, err)` when a command needs a specific
  one.
- **Keep API knowledge local.** `internal/shopify` handles transport only;
  GraphQL documents and response structs belong to the tool that uses them.

## Testing a tool

```go
io, stdout, _ := iostreams.Test()
f := app.NewFactory(io)
f.Options.OutputFormat = "json"
f.ConfigFunc = func() (*config.Config, error) { return testConfig(), nil }
f.ClientFunc = func(context.Context) (*shopify.Client, error) { return fakeClient(srv), nil }

cmd := product.NewCommand(f)
cmd.SetArgs([]string{"list"})
err := cmd.Execute()
```

See `internal/commands/auth/auth_test.go` for a working example, and
`internal/cli/root_test.go` for full end-to-end runs through the root command.
