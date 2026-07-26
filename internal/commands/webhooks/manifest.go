package webhooks

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Manifest is the declarative desired state, read from a YAML file:
//
//	webhooks:
//	  - topic: ORDERS_CREATE
//	    uri: https://api.acme.dev/hooks/orders
//	    include_fields: [id, total_price]
//	  - topic: PRODUCTS_UPDATE
//	    uri: https://api.acme.dev/hooks/products
//	    filter: "status:active"
type Manifest struct {
	Webhooks []Spec `json:"webhooks" yaml:"webhooks"`
}

// LoadManifest reads, normalises and validates a manifest file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own manifest
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("manifest %s does not exist", path)
		}
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a typo in a key is an error, not a silently ignored field
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	if len(m.Webhooks) == 0 {
		return nil, fmt.Errorf("manifest %s declares no webhooks", path)
	}

	seen := make(map[string]int, len(m.Webhooks))
	for i := range m.Webhooks {
		m.Webhooks[i].Normalize()
		if err := m.Webhooks[i].Validate(); err != nil {
			return nil, fmt.Errorf("manifest %s, entry %d: %w", path, i+1, err)
		}
		key := m.Webhooks[i].key()
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("manifest %s: entries %d and %d both declare %s -> %s",
				path, prev+1, i+1, m.Webhooks[i].Topic, m.Webhooks[i].URI)
		}
		seen[key] = i
	}
	return &m, nil
}
