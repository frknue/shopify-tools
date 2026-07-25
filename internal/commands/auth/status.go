package auth

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

// Status is the machine-readable result of `auth status`.
type Status struct {
	Profile    string `json:"profile" yaml:"profile"`
	Shop       string `json:"shop" yaml:"shop"`
	ShopName   string `json:"shop_name,omitempty" yaml:"shop_name,omitempty"`
	Plan       string `json:"plan,omitempty" yaml:"plan,omitempty"`
	APIVersion string `json:"api_version" yaml:"api_version"`
	Valid      bool   `json:"valid" yaml:"valid"`
}

// Headers implements output.Tabler.
func (s Status) Headers() []string {
	return []string{"PROFILE", "SHOP", "NAME", "PLAN", "API", "VALID"}
}

// Rows implements output.Tabler.
func (s Status) Rows() [][]string {
	return [][]string{{s.Profile, s.Shop, s.ShopName, s.Plan, s.APIVersion, fmt.Sprint(s.Valid)}}
}

func newStatusCommand(f *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Check that the active profile can reach the store",
		Args:    cobra.NoArgs,
		Example: "  shopify-tools auth status --profile staging -o json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), f)
		},
	}
}

func runStatus(ctx context.Context, f *app.Factory) error {
	profile, err := f.Profile()
	if err != nil {
		return err
	}

	status := Status{
		Profile:    profile.Name,
		Shop:       profile.Shop,
		APIVersion: profile.APIVersion,
	}

	shop, err := fetchShop(ctx, f)
	if err != nil {
		return err
	}
	status.ShopName = shop.Name
	status.Plan = shop.PlanDisplayName
	status.Valid = true

	printer, err := f.Printer()
	if err != nil {
		return err
	}
	return printer.Print(status)
}

// shopInfo is the slice of the Shop object this tool needs.
type shopInfo struct {
	Name            string `json:"name"`
	MyshopifyDomain string `json:"myshopifyDomain"`
	PlanDisplayName string `json:"planDisplayName"`
}

const shopQuery = `query ShopInfo {
  shop {
    name
    myshopifyDomain
    plan { displayName }
  }
}`

func fetchShop(ctx context.Context, f *app.Factory) (shopInfo, error) {
	client, err := f.Client(ctx)
	if err != nil {
		return shopInfo{}, err
	}

	var resp struct {
		Shop struct {
			Name            string `json:"name"`
			MyshopifyDomain string `json:"myshopifyDomain"`
			Plan            struct {
				DisplayName string `json:"displayName"`
			} `json:"plan"`
		} `json:"shop"`
	}
	if err := client.GraphQL(ctx, graphQLRequest(shopQuery), &resp); err != nil {
		return shopInfo{}, err
	}

	return shopInfo{
		Name:            resp.Shop.Name,
		MyshopifyDomain: resp.Shop.MyshopifyDomain,
		PlanDisplayName: resp.Shop.Plan.DisplayName,
	}, nil
}

// verify performs the cheapest authenticated call available.
func verify(ctx context.Context, f *app.Factory) error {
	_, err := fetchShop(ctx, f)
	return err
}
