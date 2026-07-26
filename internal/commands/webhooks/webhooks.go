// Package webhooks manages a store's webhook subscriptions, either one at a
// time or declaratively from a manifest file.
//
// Scope caveat worth knowing: the Admin API only returns subscriptions that
// were created through the API by the *same* access token, and never the
// app-scoped ones declared in a shopify.app.toml. Every command here therefore
// operates on "the webhooks belonging to this profile's token".
package webhooks

import (
	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/output"
)

const toolLong = `Manage the webhook subscriptions of a store.

Subcommands cover both one-off changes (list, get, create, update, delete) and
declarative management: keep the desired webhooks in a YAML manifest, review
the difference with ` + "`diff`" + `, and apply it with ` + "`sync`" + `.

Note: the Admin API only exposes webhooks created via the API by the same
access token. Webhooks declared in an app's shopify.app.toml are managed by
Shopify and will not appear here.`

// NewCommand returns the `webhooks` tool.
func NewCommand(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "webhooks <command>",
		Aliases: []string{"webhook"},
		Short:   "Manage webhook subscriptions",
		Long:    toolLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newListCommand(f),
		newGetCommand(f),
		newCreateCommand(f),
		newUpdateCommand(f),
		newDeleteCommand(f),
		newTopicsCommand(f),
		newDiffCommand(f),
		newSyncCommand(f),
	)
	return cmd
}

// SubscriptionList is the result type for commands returning several
// subscriptions.
type SubscriptionList struct {
	Webhooks []Subscription `json:"webhooks" yaml:"webhooks"`
}

// Headers implements output.Tabler.
func (l SubscriptionList) Headers() []string {
	return []string{"ID", "TOPIC", "URI", "FORMAT", "FILTER"}
}

// Rows implements output.Tabler.
func (l SubscriptionList) Rows() [][]string {
	rows := make([][]string, 0, len(l.Webhooks))
	for i := range l.Webhooks {
		w := &l.Webhooks[i]
		rows = append(rows, []string{shortID(w.ID), w.Topic, w.URI, w.Format, w.Filter})
	}
	return rows
}

// Headers implements output.Tabler for a single subscription.
func (s Subscription) Headers() []string {
	return []string{"ID", "TOPIC", "URI", "FORMAT", "FILTER", "INCLUDE FIELDS"}
}

// Rows implements output.Tabler for a single subscription.
func (s Subscription) Rows() [][]string {
	return [][]string{{
		shortID(s.ID), s.Topic, s.URI, s.Format, s.Filter, listOrNone(s.IncludeFields),
	}}
}

// renderTable prints a value as a table on stderr, used for the plan preview
// that precedes a destructive action. Results always stay on stdout.
func renderTable(f *app.Factory, v any) error {
	return output.New(f.IOStreams.ErrOut, output.FormatTable).Print(v)
}

// shortID trims the gid prefix, which is noise in a terminal table.
func shortID(gid string) string {
	if i := lastSlash(gid); i >= 0 {
		return gid[i+1:]
	}
	return gid
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
