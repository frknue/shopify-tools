package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

func newUseCommand(f *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "use <profile>",
		Short:   "Set the active store profile",
		Args:    cobra.ExactArgs(1),
		Example: "  shopify-tools auth use staging",
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			cfg, err := f.Config()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return cfg.ProfileNames(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found; run `shopify-tools auth list`", name)
			}

			cfg.SetCurrentProfile(name)
			if err := cfg.Save(); err != nil {
				return err
			}
			if !f.Options.Quiet {
				fmt.Fprintf(f.IOStreams.ErrOut, "Now using profile %q\n", name)
			}
			return nil
		},
	}
}
