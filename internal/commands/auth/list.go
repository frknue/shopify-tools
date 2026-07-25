package auth

import (
	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

// ProfileList is the machine-readable result of `auth list`.
type ProfileList struct {
	Current  string        `json:"current" yaml:"current"`
	Profiles []ProfileItem `json:"profiles" yaml:"profiles"`
}

// ProfileItem is one configured store profile.
type ProfileItem struct {
	Name       string `json:"name" yaml:"name"`
	Shop       string `json:"shop" yaml:"shop"`
	APIVersion string `json:"api_version" yaml:"api_version"`
	Current    bool   `json:"current" yaml:"current"`
	HasToken   bool   `json:"has_token" yaml:"has_token"`
}

// Headers implements output.Tabler.
func (l ProfileList) Headers() []string { return []string{"", "NAME", "SHOP", "API VERSION", "TOKEN"} }

// Rows implements output.Tabler.
func (l ProfileList) Rows() [][]string {
	rows := make([][]string, 0, len(l.Profiles))
	for _, p := range l.Profiles {
		marker := " "
		if p.Current {
			marker = "*"
		}
		token := "missing"
		if p.HasToken {
			token = "set"
		}
		rows = append(rows, []string{marker, p.Name, p.Shop, p.APIVersion, token})
	}
	return rows
}

func newListCommand(f *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the configured store profiles",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}

			list := ProfileList{Current: cfg.CurrentProfile}
			for _, name := range cfg.ProfileNames() {
				p := cfg.Profiles[name]
				apiVersion := p.APIVersion
				if apiVersion == "" {
					apiVersion = "(default)"
				}
				list.Profiles = append(list.Profiles, ProfileItem{
					Name:       name,
					Shop:       p.Shop,
					APIVersion: apiVersion,
					Current:    name == cfg.CurrentProfile,
					HasToken:   p.AccessToken != "",
				})
			}

			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(list)
		},
	}
}
