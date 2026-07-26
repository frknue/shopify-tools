package webhooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frknue/shopify-tools/internal/commands/webhooks"
)

func TestNormalizeTopic(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"orders/create", "ORDERS_CREATE"},
		{"ORDERS_CREATE", "ORDERS_CREATE"},
		{"products-update", "PRODUCTS_UPDATE"},
		{" orders/paid ", "ORDERS_PAID"}, // surrounding whitespace is trimmed
	}

	for _, tt := range tests {
		if got := webhooks.NormalizeTopic(tt.in); got != tt.want {
			t.Errorf("NormalizeTopic(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSpecValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    webhooks.Spec
		wantErr string
	}{
		{
			name: "https endpoint",
			spec: webhooks.Spec{Topic: "ORDERS_CREATE", URI: "https://acme.dev/hooks"},
		},
		{
			name: "pubsub endpoint",
			spec: webhooks.Spec{Topic: "ORDERS_CREATE", URI: "pubsub://my-project:my-topic"},
		},
		{
			name: "eventbridge arn",
			spec: webhooks.Spec{Topic: "ORDERS_CREATE", URI: "arn:aws:events:us-east-1::event-source/aws.partner/shopify.com/1/x"},
		},
		{
			name:    "plain http is rejected with a reason",
			spec:    webhooks.Spec{Topic: "ORDERS_CREATE", URI: "http://acme.dev/hooks"},
			wantErr: "https",
		},
		{
			name:    "malformed pubsub",
			spec:    webhooks.Spec{Topic: "ORDERS_CREATE", URI: "pubsub://my-project"},
			wantErr: "Pub/Sub",
		},
		{
			name:    "missing uri",
			spec:    webhooks.Spec{Topic: "ORDERS_CREATE"},
			wantErr: "uri is required",
		},
		{
			name:    "missing topic",
			spec:    webhooks.Spec{URI: "https://acme.dev/hooks"},
			wantErr: "topic is required",
		},
		{
			name:    "bad format",
			spec:    webhooks.Spec{Topic: "ORDERS_CREATE", URI: "https://acme.dev/hooks", Format: "CSV"},
			wantErr: "JSON or XML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := tt.spec
			s.Normalize()
			err := s.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() returned error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "webhooks.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestLoadManifest(t *testing.T) {
	t.Parallel()

	path := writeManifest(t, `
webhooks:
  - topic: orders/create
    uri: https://acme.dev/hooks/orders
    include_fields: [total_price, id]
  - topic: PRODUCTS_UPDATE
    uri: https://acme.dev/hooks/products
    filter: "status:active"
`)

	m, err := webhooks.LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() returned error: %v", err)
	}
	if len(m.Webhooks) != 2 {
		t.Fatalf("loaded %d webhooks, want 2", len(m.Webhooks))
	}
	if m.Webhooks[0].Topic != "ORDERS_CREATE" {
		t.Errorf("topic = %q, want it normalised to ORDERS_CREATE", m.Webhooks[0].Topic)
	}
	if got := m.Webhooks[0].IncludeFields; got[0] != "id" || got[1] != "total_price" {
		t.Errorf("include_fields = %v, want them sorted for stable comparison", got)
	}
}

func TestLoadManifestRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ body, wantErr string }{
		"unknown key": {
			body:    "webhooks:\n  - topic: ORDERS_CREATE\n    url: https://acme.dev/x\n",
			wantErr: "field url not found",
		},
		"duplicate entry": {
			body: `webhooks:
  - topic: ORDERS_CREATE
    uri: https://acme.dev/x
  - topic: orders/create
    uri: https://acme.dev/x
`,
			wantErr: "both declare",
		},
		"empty": {
			body:    "webhooks: []\n",
			wantErr: "declares no webhooks",
		},
		"invalid entry": {
			body:    "webhooks:\n  - topic: ORDERS_CREATE\n    uri: ftp://acme.dev/x\n",
			wantErr: "unsupported uri",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := webhooks.LoadManifest(writeManifest(t, tt.body))
			if err == nil {
				t.Fatalf("LoadManifest() = nil, want an error mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	t.Parallel()

	_, err := webhooks.LoadManifest(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want a clear 'does not exist' message", err)
	}
}
