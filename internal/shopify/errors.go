package shopify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError describes a non-2xx response from the Admin API.
type APIError struct {
	StatusCode int
	Status     string
	RequestID  string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "shopify: api error %d", e.StatusCode)
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request id %s)", e.RequestID)
	}
	return b.String()
}

// IsUnauthorized reports whether the credentials were rejected.
func (e *APIError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

// IsNotFound reports whether the resource does not exist.
func (e *APIError) IsNotFound() bool { return e.StatusCode == http.StatusNotFound }

// IsThrottled reports whether the request hit the API rate limit.
func (e *APIError) IsThrottled() bool { return e.StatusCode == http.StatusTooManyRequests }

// AsAPIError extracts an *APIError from an error chain.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	ok := errors.As(err, &apiErr)
	return apiErr, ok
}

// newAPIError maps a failed HTTP response onto an APIError, unwrapping the
// several shapes Shopify uses for its `errors` field.
func newAPIError(resp *http.Response, body []byte) *APIError {
	e := &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		RequestID:  resp.Header.Get("X-Request-Id"),
		Body:       string(body),
	}

	var envelope struct {
		Errors any `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		e.Message = flattenErrors(envelope.Errors)
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(http.StatusText(resp.StatusCode))
	}
	return e
}

// flattenErrors renders string, []string and map[string][]string error bodies.
func flattenErrors(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s := flattenErrors(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		parts := make([]string, 0, len(t))
		for k, item := range t {
			if s := flattenErrors(item); s != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", k, s))
			}
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprint(t)
	}
}
