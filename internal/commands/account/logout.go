package account

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

const logoutLong = `Log out of every account the Shopify CLI holds a session for.

This is Shopify's own global logout: it ends all saved sessions, not only the
one the current profile points at. The local profile list is cleared with them.
Store credentials saved by ` + "`auth`" + ` are a different thing and are left alone.`

type logoutOptions struct {
	yes bool
}

func newLogoutCommand(t *tool) *cobra.Command {
	opts := &logoutOptions{}

	cmd := &cobra.Command{
		Use:     "logout",
		Short:   "Log out of every Shopify CLI account",
		Long:    logoutLong,
		Args:    cobra.NoArgs,
		Example: "  shopify-tools account logout --yes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.Context(), t, opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "do not ask for confirmation")
	// The standalone shopify-auth called it --force; keep it working.
	cmd.Flags().BoolVar(&opts.yes, "force", false, "deprecated alias for --yes")
	_ = cmd.Flags().MarkHidden("force")
	return cmd
}

func runLogout(ctx context.Context, t *tool, opts *logoutOptions) error {
	if !opts.yes {
		selection, err := t.selector.Select(ctx,
			"Log out of every Shopify CLI account?", []string{"Cancel", "Log out"})
		if err != nil {
			return err
		}
		if selection == 0 {
			t.status("Cancelled.\n")
			return nil
		}
	}

	if err := t.runner.Logout(ctx); err != nil {
		return err
	}
	if err := t.store.Clear(); err != nil {
		return fmt.Errorf("logged out, but clearing the local profiles failed: %w", err)
	}
	t.status("Logged out of every Shopify CLI account.\n")
	return nil
}
