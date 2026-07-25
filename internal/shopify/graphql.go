package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GraphQLRequest is a single Admin GraphQL operation.
type GraphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// GraphQLError is one entry of the errors array in a GraphQL response.
type GraphQLError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

func (e GraphQLError) Error() string { return e.Message }

// GraphQLErrors is the aggregate error returned when a query reports errors.
type GraphQLErrors []GraphQLError

func (e GraphQLErrors) Error() string {
	msgs := make([]string, 0, len(e))
	for _, err := range e {
		msgs = append(msgs, err.Message)
	}
	return "shopify: graphql: " + strings.Join(msgs, "; ")
}

// GraphQL executes a query and unmarshals the `data` object into out.
func (c *Client) GraphQL(ctx context.Context, req GraphQLRequest, out any) error {
	u := c.baseURL.JoinPath("admin", "api", c.apiVersion, "graphql.json")

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("shopify: encode graphql request: %w", err)
	}

	resp, err := c.doWithRetry(ctx, http.MethodPost, u.String(), payload)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("shopify: read graphql response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return newAPIError(resp, data)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors GraphQLErrors   `json:"errors"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("shopify: decode graphql response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return envelope.Errors
	}
	if out == nil || len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("shopify: decode graphql data: %w", err)
	}
	return nil
}
