package account

import (
	"github.com/spf13/cobra"
)

// AccountList is the machine-readable result of `account list`.
type AccountList struct {
	Current  string        `json:"current" yaml:"current"`
	Accounts []AccountItem `json:"accounts" yaml:"accounts"`
}

// AccountItem is one saved profile.
type AccountItem struct {
	Name string `json:"name" yaml:"name"`
	// ShopifyAccount is the account the Shopify CLI knows this profile by.
	ShopifyAccount string `json:"shopify_account" yaml:"shopify_account"`
	Current        bool   `json:"current" yaml:"current"`
	// Linked is false for a profile that still has to be pointed at an
	// account, which happens once for profiles carried over from shopify-auth.
	Linked bool `json:"linked" yaml:"linked"`
}

// Headers implements output.Tabler.
func (l AccountList) Headers() []string { return []string{"", "NAME", "SHOPIFY ACCOUNT"} }

// Rows implements output.Tabler.
func (l AccountList) Rows() [][]string {
	rows := make([][]string, 0, len(l.Accounts))
	for _, a := range l.Accounts {
		marker := " "
		account := a.ShopifyAccount
		switch {
		case !a.Linked:
			marker, account = "!", "not linked yet; run `shopify-tools account use "+a.Name+"`"
		case a.Current:
			marker = "*"
		}
		rows = append(rows, []string{marker, a.Name, account})
	}
	return rows
}

func newListCommand(t *tool) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the saved profiles",
		Long: "List the saved profiles and the Shopify accounts they point at.\n\n" +
			"The Shopify CLI has no command to list its sessions, so this mapping is\n" +
			"kept locally. It contains no credentials.",
		Args:    cobra.NoArgs,
		Example: "  shopify-tools account list -o json",
		RunE: func(*cobra.Command, []string) error {
			accounts, current, err := t.store.List()
			if err != nil {
				return err
			}
			pending, err := t.store.Pending()
			if err != nil {
				return err
			}

			list := AccountList{Current: current}
			for _, account := range accounts {
				list.Accounts = append(list.Accounts, AccountItem{
					Name:           account.Name,
					ShopifyAccount: account.ShopifyAlias,
					Current:        account.Name == current,
					Linked:         true,
				})
			}
			for _, name := range pending {
				list.Accounts = append(list.Accounts, AccountItem{Name: name})
			}

			printer, err := t.f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(list)
		},
	}
}
