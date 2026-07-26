package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/config"
)

const useLong = `Point the Shopify CLI at the account saved under a profile name.

Without a name the saved profiles are listed to choose from. A name that is not
saved yet creates the profile: Shopify's own account picker opens, and whatever
account it confirms is linked to that name — there is no alias to retype.

Every switch is verified against the account the Shopify CLI reports back. If
Shopify no longer knows the saved account, the picker opens once so the profile
can be re-linked.`

func newUseCommand(t *tool) *cobra.Command {
	return &cobra.Command{
		Use:     "use [profile]",
		Short:   "Point the Shopify CLI at a profile's account",
		Long:    useLong,
		Args:    cobra.MaximumNArgs(1),
		Example: "  shopify-tools account use work\n  shopify-tools account use",
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			names, err := t.store.Names()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = strings.TrimSpace(args[0])
			}
			return runUse(cmd.Context(), t, name)
		},
	}
}

func runUse(ctx context.Context, t *tool, name string) error {
	if name == "" {
		chosen, err := chooseProfile(ctx, t)
		if err != nil {
			return err
		}
		name = chosen
	}
	if err := validateProfileName(name); err != nil {
		return fmt.Errorf("invalid profile name %q: %w", name, err)
	}

	account, found, err := t.store.Find(name)
	if err != nil {
		return err
	}
	if found && account.ShopifyAlias != "" {
		return switchTo(ctx, t, account)
	}

	if found {
		t.prompt("Profile %q is not linked yet. Choose or log in to its Shopify account.\n", name)
	} else {
		t.prompt("Creating profile %q. Choose or log in to its Shopify account.\n", name)
	}
	return linkProfile(ctx, t, name)
}

// chooseProfile shows the saved profiles and returns the one picked. It asks
// for a new name when there is nothing saved yet or "Add account" is chosen.
func chooseProfile(ctx context.Context, t *tool) (string, error) {
	accounts, _, err := t.store.List()
	if err != nil {
		return "", err
	}
	pending, err := t.store.Pending()
	if err != nil {
		return "", err
	}
	if len(accounts) == 0 && len(pending) == 0 {
		t.prompt("No profiles saved yet. Let's add one.\n")
		return promptName(t)
	}

	labels := make([]string, 0, len(accounts)+len(pending)+1)
	names := make([]string, 0, len(accounts)+len(pending))
	for _, account := range accounts {
		label := account.Name
		if account.ShopifyAlias != account.Name {
			label += " (" + account.ShopifyAlias + ")"
		}
		labels = append(labels, label)
		names = append(names, account.Name)
	}
	for _, name := range pending {
		labels = append(labels, name+" (not linked yet)")
		names = append(names, name)
	}
	labels = append(labels, "Add account")

	selection, err := t.selector.Select(ctx, "Choose a profile", labels)
	if err != nil {
		return "", fmt.Errorf("choose profile: %w", err)
	}
	if selection < 0 || selection >= len(labels) {
		return "", fmt.Errorf("the profile picker returned choice %d of %d", selection, len(labels))
	}
	if selection == len(names) {
		return promptName(t)
	}
	return names[selection], nil
}

// switchTo points the Shopify CLI at a saved account, re-linking the profile
// when Shopify no longer knows that account.
func switchTo(ctx context.Context, t *tool, account config.CLIAccount) error {
	if err := t.runner.SwitchAccount(ctx, account.ShopifyAlias); err != nil {
		var unavailable *AliasUnavailableError
		if !errors.As(err, &unavailable) {
			return err
		}
		t.prompt("The saved Shopify account for %q is unavailable. Choose it again.\n", account.Name)
		return linkProfile(ctx, t, account.Name)
	}

	// Recording again marks the profile as the current one.
	if err := t.store.Record(account); err != nil {
		return fmt.Errorf("account switched, but updating profile %q failed: %w", account.Name, err)
	}
	t.status("Using profile %s.\n", describe(account))
	return nil
}

// linkProfile runs Shopify's own picker and saves the account it confirms.
func linkProfile(ctx context.Context, t *tool, name string) error {
	alias, err := t.runner.SelectAccount(ctx)
	if err != nil {
		return err
	}
	if err := validateShopifyAlias(alias); err != nil {
		return err
	}

	account := config.CLIAccount{Name: name, ShopifyAlias: alias}
	if err := t.store.Record(account); err != nil {
		return fmt.Errorf("account switched, but saving profile %q failed: %w", name, err)
	}
	t.status("Saved profile %s.\n", describe(account))
	return nil
}

// promptName asks for a profile name until a usable one is given.
func promptName(t *tool) (string, error) {
	for {
		t.prompt("Profile name: ")
		line, err := t.in.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return "", fmt.Errorf("read profile name: %w", err)
		}

		name := strings.TrimSpace(line)
		invalid := validateProfileName(name)
		if invalid == nil {
			return name, nil
		}
		t.prompt("Invalid profile name: %v\n", invalid)
	}
}

// describe names a profile without repeating an account it is named after.
func describe(account config.CLIAccount) string {
	if account.Name == account.ShopifyAlias {
		return fmt.Sprintf("%q", account.Name)
	}
	return fmt.Sprintf("%q (%s)", account.Name, account.ShopifyAlias)
}
