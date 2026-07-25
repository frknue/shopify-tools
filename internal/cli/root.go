// Package cli assembles the command tree.
//
// The root command owns the global flags and the shared *app.Factory; every
// tool is a self-contained package under internal/commands that exposes a
// single NewCommand(*app.Factory) *cobra.Command constructor and is listed in
// registry.go. Adding a tool touches exactly those two places.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/buildinfo"
	"github.com/frknue/shopify-tools/internal/output"
)

const rootLong = `shopify-tools bundles the day-to-day Shopify utilities into one binary.

Each tool is a top-level command with its own subcommands, for example:

  shopify-tools auth login --shop acme.myshopify.com
  shopify-tools auth status

Credentials are resolved from, in increasing precedence:
config file, environment (SHOPIFY_TOOLS_*), command-line flags.`

// NewRootCommand builds the full command tree.
func NewRootCommand(f *app.Factory) *cobra.Command {
	opts := f.Options

	cmd := &cobra.Command{
		Use:   "shopify-tools",
		Short: "A toolbox for working with Shopify stores",
		Long:  rootLong,
		// Errors are rendered once, by main, with the right exit code.
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       buildinfo.Version(),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if opts.NoColor {
				f.IOStreams.SetColorEnabled(false)
			}
			if opts.OutputFormat != "" {
				if _, err := output.ParseFormat(opts.OutputFormat); err != nil {
					return err
				}
			}
			if opts.Verbose && opts.Quiet {
				return fmt.Errorf("--verbose and --quiet are mutually exclusive")
			}
			f.Logger().Debug("starting", "command", cmd.CommandPath(), "version", buildinfo.Version())
			return nil
		},
		// Running the bare binary should show help, not an error.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.SetIn(f.IOStreams.In)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)

	flags := cmd.PersistentFlags()
	flags.StringVar(&opts.ConfigPath, "config", "", "path to the config file (default: OS config dir)")
	flags.StringVarP(&opts.Profile, "profile", "p", "", "store profile to use")
	flags.StringVarP(&opts.OutputFormat, "output", "o", "", "output format: table, json, yaml")
	flags.DurationVar(&opts.Timeout, "timeout", 0, "timeout for API calls (default 30s)")
	flags.BoolVarP(&opts.Verbose, "verbose", "v", false, "enable debug logging on stderr")
	flags.BoolVarP(&opts.Quiet, "quiet", "q", false, "suppress non-essential output")
	flags.BoolVar(&opts.NoColor, "no-color", false, "disable colored output")

	registerCompletions(cmd, f)

	cmd.AddCommand(newVersionCommand(f))
	for _, newTool := range toolCommands() {
		cmd.AddCommand(newTool(f))
	}

	cmd.SetVersionTemplate("{{.Root.Short}}\n")
	return cmd
}

// registerCompletions wires shell completion for the global flags.
func registerCompletions(cmd *cobra.Command, f *app.Factory) {
	_ = cmd.RegisterFlagCompletionFunc("output",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return output.Formats(), cobra.ShellCompDirectiveNoFileComp
		})
	_ = cmd.RegisterFlagCompletionFunc("profile",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			cfg, err := f.Config()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return cfg.ProfileNames(), cobra.ShellCompDirectiveNoFileComp
		})
}
