package account_test

import (
	"strings"
	"testing"

	"github.com/frknue/shopify-tools/internal/commands/account"
)

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "letters", in: "work", wantErr: false},
		{name: "digits and separators", in: "acme-2.staging_1", wantErr: false},
		{name: "unicode letters", in: "büro", wantErr: false},
		{name: "empty", in: "", wantErr: true},
		{name: "space", in: "my profile", wantErr: true},
		{name: "newline", in: "work\nrm -rf /", wantErr: true},
		{name: "slash", in: "../escape", wantErr: true},
		{name: "at sign", in: "dev@example.test", wantErr: true},
		{name: "too long", in: strings.Repeat("a", 65), wantErr: true},
		{name: "longest allowed", in: strings.Repeat("a", 64), wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := account.ValidateProfileName(tc.in)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateProfileName(%q) = nil, want an error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateProfileName(%q) returned error: %v", tc.in, err)
			}
		})
	}
}

// The alias is scraped out of the Shopify CLI's terminal output, so it is
// never trusted as-is.
func TestValidateShopifyAlias(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "email", in: "dev@example.test", wantErr: false},
		{name: "spaces are allowed", in: "Dev Account", wantErr: false},
		{name: "empty", in: "", wantErr: true},
		{name: "escape sequence", in: "dev@example.test\x1b[2K", wantErr: true},
		{name: "newline", in: "dev@example.test\nmore", wantErr: true},
		{name: "too long", in: strings.Repeat("a", 321), wantErr: true},
		{name: "longest allowed", in: strings.Repeat("a", 320), wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := account.ValidateShopifyAlias(tc.in)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateShopifyAlias(%q) = nil, want an error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateShopifyAlias(%q) returned error: %v", tc.in, err)
			}
		})
	}
}
