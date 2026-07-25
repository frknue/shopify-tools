package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/config"
)

type loginOptions struct {
	profileName string
	shop        string
	accessToken string
	apiVersion  string
	setCurrent  bool
	skipVerify  bool
}

func newLoginCommand(f *app.Factory) *cobra.Command {
	opts := &loginOptions{}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save credentials for a store",
		Long: `Save an Admin API access token for a store under a named profile.

The token is read, in order of precedence, from --access-token, the
SHOPIFY_TOOLS_ACCESS_TOKEN environment variable, or an interactive prompt.
It is written to the config file with owner-only permissions.`,
		Example: `  # interactive
  shopify-tools auth login --shop acme.myshopify.com

  # non-interactive, e.g. in CI
  shopify-tools auth login --shop acme --access-token "$TOKEN" --profile staging`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd.Context(), f, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.shop, "shop", "", "myshopify domain, e.g. acme.myshopify.com (required)")
	flags.StringVar(&opts.accessToken, "access-token", "", "Admin API access token; prompted for if omitted")
	flags.StringVar(&opts.apiVersion, "api-version", config.DefaultAPIVersion, "Admin API version to pin")
	flags.StringVar(&opts.profileName, "profile-name", "", "name to save this profile under (default: the shop's subdomain)")
	flags.BoolVar(&opts.setCurrent, "set-current", true, "make this the active profile")
	flags.BoolVar(&opts.skipVerify, "skip-verify", false, "do not call the API to verify the credentials")

	_ = cmd.MarkFlagRequired("shop")
	return cmd
}

func runLogin(ctx context.Context, f *app.Factory, opts *loginOptions) error {
	shop, err := config.NormalizeShop(opts.shop)
	if err != nil {
		return err
	}

	token := opts.accessToken
	if token == "" {
		token = os.Getenv(config.EnvPrefix + "ACCESS_TOKEN")
	}
	if token == "" {
		if token, err = promptSecret(f, "Admin API access token: "); err != nil {
			return err
		}
	}
	if token == "" {
		return errors.New("access token is empty")
	}

	name := opts.profileName
	if name == "" {
		name, _, _ = strings.Cut(shop, ".")
	}

	cfg, err := f.Config()
	if err != nil {
		return err
	}

	profile := &config.Profile{Shop: shop, AccessToken: token, APIVersion: opts.apiVersion}
	cfg.SetProfile(name, profile)
	if opts.setCurrent {
		cfg.CurrentProfile = name
	}

	if !opts.skipVerify {
		// Verify against the live API before persisting anything.
		f.Options.Profile = name
		if err := verify(ctx, f); err != nil {
			return fmt.Errorf("credential check failed: %w", err)
		}
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	if !f.Options.Quiet {
		fmt.Fprintf(f.IOStreams.ErrOut, "Saved profile %q for %s in %s\n", name, shop, cfg.Path())
	}
	return nil
}

// promptSecret reads a secret without echoing it, falling back to a plain read
// when stdin is not a terminal (piped input, CI).
func promptSecret(f *app.Factory, prompt string) (string, error) {
	if !f.IOStreams.IsStdinTTY() {
		line, err := bufio.NewReader(f.IOStreams.In).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read access token from stdin: %w", err)
		}
		return strings.TrimSpace(line), nil
	}

	fmt.Fprint(f.IOStreams.ErrOut, prompt)
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(f.IOStreams.ErrOut)
	if err != nil {
		return "", fmt.Errorf("read access token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
