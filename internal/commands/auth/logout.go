package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

func newLogoutCommand(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout [profile]",
		Short: "Remove a stored profile",
		Args:  cobra.MaximumNArgs(1),
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

			name := f.Options.Profile
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				name = cfg.CurrentProfile
			}
			if name == "" {
				return fmt.Errorf("no profile selected; pass a profile name")
			}

			if !cfg.RemoveProfile(name) {
				return fmt.Errorf("profile %q not found", name)
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			if !f.Options.Quiet {
				fmt.Fprintf(f.IOStreams.ErrOut, "Removed profile %q\n", name)
			}
			return nil
		},
	}
	return cmd
}
