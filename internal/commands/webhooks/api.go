package webhooks

import (
	"context"
	"fmt"
	"strings"

	"github.com/frknue/shopify-tools/internal/shopify"
)

// pageSize is the connection page size used when listing subscriptions.
// Shopify caps connections at 250.
const pageSize = 100

// Subscription is a webhook subscription as it exists on the store.
type Subscription struct {
	ID                  string   `json:"id" yaml:"id"`
	Topic               string   `json:"topic" yaml:"topic"`
	URI                 string   `json:"uri" yaml:"uri"`
	Format              string   `json:"format" yaml:"format"`
	Filter              string   `json:"filter,omitempty" yaml:"filter,omitempty"`
	IncludeFields       []string `json:"include_fields,omitempty" yaml:"include_fields,omitempty"`
	MetafieldNamespaces []string `json:"metafield_namespaces,omitempty" yaml:"metafield_namespaces,omitempty"`
	CreatedAt           string   `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt           string   `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// subscriptionFields is the selection set shared by every operation below.
const subscriptionFields = `id topic uri format filter includeFields metafieldNamespaces createdAt updatedAt`

const listQuery = `query WebhookSubscriptions($first: Int!, $after: String, $topics: [WebhookSubscriptionTopic!]) {
  webhookSubscriptions(first: $first, after: $after, topics: $topics) {
    nodes { ` + subscriptionFields + ` }
    pageInfo { hasNextPage endCursor }
  }
}`

const getQuery = `query WebhookSubscriptionByID($id: ID!) {
  webhookSubscription(id: $id) { ` + subscriptionFields + ` }
}`

const createMutation = `mutation WebhookSubscriptionCreate($topic: WebhookSubscriptionTopic!, $webhookSubscription: WebhookSubscriptionInput!) {
  webhookSubscriptionCreate(topic: $topic, webhookSubscription: $webhookSubscription) {
    webhookSubscription { ` + subscriptionFields + ` }
    userErrors { field message }
  }
}`

const updateMutation = `mutation WebhookSubscriptionUpdate($id: ID!, $webhookSubscription: WebhookSubscriptionInput!) {
  webhookSubscriptionUpdate(id: $id, webhookSubscription: $webhookSubscription) {
    webhookSubscription { ` + subscriptionFields + ` }
    userErrors { field message }
  }
}`

const deleteMutation = `mutation WebhookSubscriptionDelete($id: ID!) {
  webhookSubscriptionDelete(id: $id) {
    deletedWebhookSubscriptionId
    userErrors { field message }
  }
}`

// topicsQuery reads the topic enum straight off the schema, so the list is
// never stale against the API version in use.
const topicsQuery = `query WebhookTopics {
  __type(name: "WebhookSubscriptionTopic") { enumValues { name } }
}`

// userError mirrors the userErrors payload every mutation returns.
type userError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

// asError turns a userErrors array into a single error, or nil when empty.
func asError(op string, errs []userError) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		if len(e.Field) > 0 {
			msgs = append(msgs, fmt.Sprintf("%s: %s", strings.Join(e.Field, "."), e.Message))
			continue
		}
		msgs = append(msgs, e.Message)
	}
	return fmt.Errorf("%s: %s", op, strings.Join(msgs, "; "))
}

// listSubscriptions returns every subscription visible to the access token,
// following pagination. topics may be empty to list all.
func listSubscriptions(ctx context.Context, client *shopify.Client, topics []string) ([]Subscription, error) {
	var (
		all    []Subscription
		cursor *string
	)

	for {
		vars := map[string]any{"first": pageSize, "after": cursor}
		if len(topics) > 0 {
			vars["topics"] = topics
		}

		var resp struct {
			WebhookSubscriptions struct {
				Nodes    []Subscription `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"webhookSubscriptions"`
		}
		if err := client.GraphQL(ctx, shopify.GraphQLRequest{Query: listQuery, Variables: vars}, &resp); err != nil {
			return nil, err
		}

		all = append(all, resp.WebhookSubscriptions.Nodes...)
		if !resp.WebhookSubscriptions.PageInfo.HasNextPage {
			return all, nil
		}
		next := resp.WebhookSubscriptions.PageInfo.EndCursor
		cursor = &next
	}
}

// getSubscription fetches one subscription by ID.
func getSubscription(ctx context.Context, client *shopify.Client, id string) (*Subscription, error) {
	var resp struct {
		WebhookSubscription *Subscription `json:"webhookSubscription"`
	}
	vars := map[string]any{"id": normalizeID(id)}
	if err := client.GraphQL(ctx, shopify.GraphQLRequest{Query: getQuery, Variables: vars}, &resp); err != nil {
		return nil, err
	}
	if resp.WebhookSubscription == nil {
		return nil, fmt.Errorf("webhook subscription %s not found", id)
	}
	return resp.WebhookSubscription, nil
}

// createSubscription creates a subscription from a desired-state spec.
func createSubscription(ctx context.Context, client *shopify.Client, spec Spec) (*Subscription, error) {
	var resp struct {
		WebhookSubscriptionCreate struct {
			WebhookSubscription *Subscription `json:"webhookSubscription"`
			UserErrors          []userError   `json:"userErrors"`
		} `json:"webhookSubscriptionCreate"`
	}

	vars := map[string]any{"topic": spec.Topic, "webhookSubscription": spec.input()}
	if err := client.GraphQL(ctx, shopify.GraphQLRequest{Query: createMutation, Variables: vars}, &resp); err != nil {
		return nil, err
	}
	if err := asError("create "+spec.Topic, resp.WebhookSubscriptionCreate.UserErrors); err != nil {
		return nil, err
	}
	return resp.WebhookSubscriptionCreate.WebhookSubscription, nil
}

// updateSubscription applies a spec to an existing subscription.
func updateSubscription(ctx context.Context, client *shopify.Client, id string, spec Spec) (*Subscription, error) {
	var resp struct {
		WebhookSubscriptionUpdate struct {
			WebhookSubscription *Subscription `json:"webhookSubscription"`
			UserErrors          []userError   `json:"userErrors"`
		} `json:"webhookSubscriptionUpdate"`
	}

	vars := map[string]any{"id": normalizeID(id), "webhookSubscription": spec.input()}
	if err := client.GraphQL(ctx, shopify.GraphQLRequest{Query: updateMutation, Variables: vars}, &resp); err != nil {
		return nil, err
	}
	if err := asError("update "+id, resp.WebhookSubscriptionUpdate.UserErrors); err != nil {
		return nil, err
	}
	return resp.WebhookSubscriptionUpdate.WebhookSubscription, nil
}

// deleteSubscription removes a subscription and returns the deleted ID.
func deleteSubscription(ctx context.Context, client *shopify.Client, id string) (string, error) {
	var resp struct {
		WebhookSubscriptionDelete struct {
			DeletedWebhookSubscriptionID string      `json:"deletedWebhookSubscriptionId"`
			UserErrors                   []userError `json:"userErrors"`
		} `json:"webhookSubscriptionDelete"`
	}

	vars := map[string]any{"id": normalizeID(id)}
	if err := client.GraphQL(ctx, shopify.GraphQLRequest{Query: deleteMutation, Variables: vars}, &resp); err != nil {
		return "", err
	}
	if err := asError("delete "+id, resp.WebhookSubscriptionDelete.UserErrors); err != nil {
		return "", err
	}
	return resp.WebhookSubscriptionDelete.DeletedWebhookSubscriptionID, nil
}

// listTopics reads the WebhookSubscriptionTopic enum from the schema.
func listTopics(ctx context.Context, client *shopify.Client) ([]string, error) {
	var resp struct {
		Type *struct {
			EnumValues []struct {
				Name string `json:"name"`
			} `json:"enumValues"`
		} `json:"__type"`
	}
	if err := client.GraphQL(ctx, shopify.GraphQLRequest{Query: topicsQuery}, &resp); err != nil {
		return nil, err
	}
	if resp.Type == nil {
		return nil, fmt.Errorf("the API did not return the WebhookSubscriptionTopic enum")
	}

	topics := make([]string, 0, len(resp.Type.EnumValues))
	for _, v := range resp.Type.EnumValues {
		topics = append(topics, v.Name)
	}
	return topics, nil
}

// normalizeID accepts either a bare numeric ID or a full gid.
func normalizeID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "gid://") {
		return id
	}
	return "gid://shopify/WebhookSubscription/" + id
}
