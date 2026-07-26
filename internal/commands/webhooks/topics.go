package webhooks

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
)

// TopicList is the result of `webhooks topics`.
type TopicList struct {
	Topics []string `json:"topics" yaml:"topics"`
}

// Headers implements output.Tabler.
func (t TopicList) Headers() []string { return []string{"TOPIC"} }

// Rows implements output.Tabler.
func (t TopicList) Rows() [][]string {
	rows := make([][]string, 0, len(t.Topics))
	for _, topic := range t.Topics {
		rows = append(rows, []string{topic})
	}
	return rows
}

func newTopicsCommand(f *app.Factory) *cobra.Command {
	var search string

	cmd := &cobra.Command{
		Use:   "topics",
		Short: "List the webhook topics this API version supports",
		Long: `List the webhook topics this API version supports.

The list is read from the API schema itself, so it always matches the API
version the active profile is pinned to.`,
		Args:    cobra.NoArgs,
		Example: "  shopify-tools webhooks topics --search order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}

			topics, err := listTopics(cmd.Context(), client)
			if err != nil {
				return err
			}

			if search != "" {
				needle := strings.ToUpper(search)
				filtered := make([]string, 0, len(topics))
				for _, t := range topics {
					if strings.Contains(t, needle) {
						filtered = append(filtered, t)
					}
				}
				topics = filtered
			}

			printer, err := f.Printer()
			if err != nil {
				return err
			}
			return printer.Print(TopicList{Topics: topics})
		},
	}

	cmd.Flags().StringVar(&search, "search", "", "only show topics containing this substring")
	return cmd
}
