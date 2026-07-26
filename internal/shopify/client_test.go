package shopify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frknue/shopify-tools/internal/shopify"
)

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...shopify.Option) *shopify.Client {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	all := append([]shopify.Option{
		shopify.WithBaseURL(srv.URL),
		shopify.WithHTTPClient(srv.Client()),
	}, opts...)

	client, err := shopify.New("acme.myshopify.com", "shpat_test", "2026-04", all...)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	return client
}

func TestGraphQLDecodesData(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Shopify-Access-Token"); got != "shpat_test" {
			t.Errorf("access token header = %q, want shpat_test", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/admin/api/2026-04/graphql.json") {
			t.Errorf("path = %q, want the versioned graphql endpoint", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"shop":{"name":"Acme"}}}`))
	})

	var resp struct {
		Shop struct {
			Name string `json:"name"`
		} `json:"shop"`
	}
	if err := client.GraphQL(context.Background(), shopify.GraphQLRequest{Query: "{shop{name}}"}, &resp); err != nil {
		t.Fatalf("GraphQL() returned error: %v", err)
	}
	if resp.Shop.Name != "Acme" {
		t.Errorf("shop name = %q, want Acme", resp.Shop.Name)
	}
}

func TestGraphQLReturnsQueryErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Field 'nope' doesn't exist"}]}`))
	})

	err := client.GraphQL(context.Background(), shopify.GraphQLRequest{Query: "{nope}"}, nil)
	if err == nil {
		t.Fatal("GraphQL() = nil, want error")
	}
	var gqlErrs shopify.GraphQLErrors
	if !asGraphQLErrors(err, &gqlErrs) {
		t.Fatalf("error type = %T, want shopify.GraphQLErrors", err)
	}
	if len(gqlErrs) != 1 || !strings.Contains(gqlErrs[0].Message, "doesn't exist") {
		t.Errorf("unexpected graphql errors: %+v", gqlErrs)
	}
}

func TestAPIErrorCarriesStatusAndMessage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "req-123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":"[API] Invalid API key or access token"}`))
	})

	err := client.Do(context.Background(), http.MethodGet, "shop.json", nil, nil)
	apiErr, ok := shopify.AsAPIError(err)
	if !ok {
		t.Fatalf("error = %v (%T), want *shopify.APIError", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("IsUnauthorized() = false for status %d", apiErr.StatusCode)
	}
	if apiErr.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want req-123", apiErr.RequestID)
	}
	if !strings.Contains(apiErr.Message, "Invalid API key") {
		t.Errorf("Message = %q, want it to include the API message", apiErr.Message)
	}
}

func TestRetriesOnThrottle(t *testing.T) {
	var calls atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}, shopify.WithMaxRetries(2))

	if err := client.GraphQL(context.Background(), shopify.GraphQLRequest{Query: "{ok}"}, nil); err != nil {
		t.Fatalf("GraphQL() returned error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("request count = %d, want 2 (one throttled, one successful)", got)
	}
}

func TestContextCancellationIsPropagated(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := client.Do(ctx, http.MethodGet, "shop.json", nil, nil); err == nil {
		t.Fatal("Do() = nil, want a context error")
	}
}

func TestNewValidatesArguments(t *testing.T) {
	if _, err := shopify.New("", "token", "2026-04"); err == nil {
		t.Error("New() with empty shop = nil error, want error")
	}
	if _, err := shopify.New("acme.myshopify.com", "", "2026-04"); err == nil {
		t.Error("New() with empty token = nil error, want error")
	}
	if _, err := shopify.New("acme.myshopify.com", "token", ""); err == nil {
		t.Error("New() with empty api version = nil error, want error")
	}
}

func asGraphQLErrors(err error, target *shopify.GraphQLErrors) bool {
	if e, ok := err.(shopify.GraphQLErrors); ok { //nolint:errorlint // exact type check is the point
		*target = e
		return true
	}
	return false
}
