package webhooks

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Spec is the desired state of one webhook subscription. It is what the
// manifest holds and what create/update send to the API.
type Spec struct {
	Topic               string   `json:"topic" yaml:"topic"`
	URI                 string   `json:"uri" yaml:"uri"`
	Format              string   `json:"format,omitempty" yaml:"format,omitempty"`
	Filter              string   `json:"filter,omitempty" yaml:"filter,omitempty"`
	IncludeFields       []string `json:"include_fields,omitempty" yaml:"include_fields,omitempty"`
	MetafieldNamespaces []string `json:"metafield_namespaces,omitempty" yaml:"metafield_namespaces,omitempty"`
}

// defaultFormat is what Shopify uses when the format is not specified.
const defaultFormat = "JSON"

// input renders the spec as a WebhookSubscriptionInput.
func (s Spec) input() map[string]any {
	in := map[string]any{"uri": s.URI}
	if s.Format != "" {
		in["format"] = s.Format
	}
	if s.Filter != "" {
		in["filter"] = s.Filter
	}
	if len(s.IncludeFields) > 0 {
		in["includeFields"] = s.IncludeFields
	}
	if len(s.MetafieldNamespaces) > 0 {
		in["metafieldNamespaces"] = s.MetafieldNamespaces
	}
	return in
}

// Normalize canonicalises user input so that "orders/create" and
// "ORDERS_CREATE" mean the same thing, and comparison is order-insensitive.
func (s *Spec) Normalize() {
	s.Topic = NormalizeTopic(s.Topic)
	s.URI = strings.TrimSpace(s.URI)
	s.Format = normalizeFormat(s.Format)
	s.Filter = strings.TrimSpace(s.Filter)
	s.IncludeFields = normalizeList(s.IncludeFields)
	s.MetafieldNamespaces = normalizeList(s.MetafieldNamespaces)
}

// Validate reports whether the spec can be sent to the API.
func (s Spec) Validate() error {
	if s.Topic == "" {
		return fmt.Errorf("topic is required")
	}
	if s.URI == "" {
		return fmt.Errorf("topic %s: uri is required", s.Topic)
	}
	if err := validateURI(s.URI); err != nil {
		return fmt.Errorf("topic %s: %w", s.Topic, err)
	}
	if s.Format != "" && s.Format != "JSON" && s.Format != "XML" {
		return fmt.Errorf("topic %s: format must be JSON or XML, got %q", s.Topic, s.Format)
	}
	return nil
}

// NormalizeTopic accepts the REST-style "orders/create" as well as the GraphQL
// enum "ORDERS_CREATE" and returns the enum form.
func NormalizeTopic(t string) string {
	t = strings.TrimSpace(t)
	t = strings.NewReplacer("/", "_", "-", "_", ".", "_", " ", "_").Replace(t)
	return strings.ToUpper(t)
}

func normalizeFormat(f string) string {
	return strings.ToUpper(strings.TrimSpace(f))
}

// normalizeList trims, drops empties and sorts, so that two lists with the
// same members compare equal regardless of the order they were written in.
func normalizeList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// validateURI accepts the three endpoint kinds the API supports: an HTTPS URL,
// a Pub/Sub URI, or an EventBridge ARN.
func validateURI(raw string) error {
	switch {
	case strings.HasPrefix(raw, "pubsub://"):
		// Expected shape is pubsub, then project id, colon, topic id.
		rest := strings.TrimPrefix(raw, "pubsub://")
		project, topic, ok := strings.Cut(rest, ":")
		if !ok || project == "" || topic == "" {
			return fmt.Errorf("invalid Pub/Sub uri %q: want pubsub://{project-id}:{topic-id}", raw)
		}
		return nil

	case strings.HasPrefix(raw, "arn:aws:events:"):
		return nil

	case strings.HasPrefix(raw, "https://"):
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return fmt.Errorf("invalid https uri %q", raw)
		}
		return nil

	case strings.HasPrefix(raw, "http://"):
		return fmt.Errorf("uri %q must use https; Shopify rejects plain http endpoints", raw)

	default:
		return fmt.Errorf("unsupported uri %q: want an https:// URL, pubsub://{project}:{topic}, or an EventBridge arn", raw)
	}
}

// toSpec projects an existing subscription onto the desired-state shape, so
// that live state and manifest state can be compared field by field.
func (s Subscription) toSpec() Spec {
	spec := Spec{
		Topic:               s.Topic,
		URI:                 s.URI,
		Format:              s.Format,
		Filter:              s.Filter,
		IncludeFields:       s.IncludeFields,
		MetafieldNamespaces: s.MetafieldNamespaces,
	}
	spec.Normalize()
	return spec
}

// key identifies a subscription for diffing. Topic plus endpoint is the only
// stable identity the API offers, so changing a uri reads as a replacement
// rather than an update.
func (s Spec) key() string { return s.Topic + "\x00" + s.URI }

// differences lists the fields in which want deviates from got, formatted for
// display. An empty result means the two are equivalent.
func differences(got, want Spec) []string {
	var diffs []string

	// An unset format means "JSON" on the API side; compare accordingly.
	gotFormat, wantFormat := got.Format, want.Format
	if gotFormat == "" {
		gotFormat = defaultFormat
	}
	if wantFormat == "" {
		wantFormat = defaultFormat
	}
	if gotFormat != wantFormat {
		diffs = append(diffs, fmt.Sprintf("format %s -> %s", gotFormat, wantFormat))
	}
	if got.Filter != want.Filter {
		diffs = append(diffs, fmt.Sprintf("filter %s -> %s", quoteOrNone(got.Filter), quoteOrNone(want.Filter)))
	}
	if !slices.Equal(got.IncludeFields, want.IncludeFields) {
		diffs = append(diffs, fmt.Sprintf("include_fields %s -> %s",
			listOrNone(got.IncludeFields), listOrNone(want.IncludeFields)))
	}
	if !slices.Equal(got.MetafieldNamespaces, want.MetafieldNamespaces) {
		diffs = append(diffs, fmt.Sprintf("metafield_namespaces %s -> %s",
			listOrNone(got.MetafieldNamespaces), listOrNone(want.MetafieldNamespaces)))
	}
	return diffs
}

func quoteOrNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return `"` + s + `"`
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "(none)"
	}
	return "[" + strings.Join(l, ",") + "]"
}
