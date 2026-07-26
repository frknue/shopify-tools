package webhooks_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/commands/webhooks"
	"github.com/frknue/shopify-tools/internal/config"
	"github.com/frknue/shopify-tools/internal/iostreams"
	"github.com/frknue/shopify-tools/internal/shopify"
)

// fakeAPI is a minimal in-memory stand-in for the webhook endpoints, enough to
// exercise list/create/update/delete and therefore a whole sync.
type fakeAPI struct {
	mu       sync.Mutex
	subs     []map[string]any
	nextID   int
	calls    []string
	failNext string

	// paginate splits list responses across two pages, so the cursor loop is
	// actually exercised.
	paginate  bool
	afterSeen []string
	// createLimit fails creates after this many have succeeded (0 = never).
	createLimit int
	createCount int
	// lastUpdateInput records the WebhookSubscriptionInput of the last update.
	lastUpdateInput map[string]any
}

func newFakeAPI(existing ...map[string]any) *fakeAPI {
	return &fakeAPI{subs: existing, nextID: 100}
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("fake API received invalid JSON: %v", err)
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.Query, "query WebhookSubscriptionByID"):
			f.calls = append(f.calls, "get")
			id, _ := req.Variables["id"].(string)
			for _, sub := range f.subs {
				if sub["id"] == id {
					f.writeJSON(w, map[string]any{"webhookSubscription": sub})
					return
				}
			}
			f.writeJSON(w, map[string]any{"webhookSubscription": nil})

		case strings.Contains(req.Query, "query WebhookSubscriptions"):
			f.calls = append(f.calls, "list")
			after, _ := req.Variables["after"].(string)
			if f.paginate {
				f.afterSeen = append(f.afterSeen, after)
				if after == "" {
					f.writeJSON(w, map[string]any{"webhookSubscriptions": map[string]any{
						"nodes":    f.subs[:1],
						"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "cursor-1"},
					}})
					return
				}
				f.writeJSON(w, map[string]any{"webhookSubscriptions": map[string]any{
					"nodes":    f.subs[1:],
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				}})
				return
			}
			f.writeJSON(w, map[string]any{"webhookSubscriptions": map[string]any{
				"nodes":    f.subs,
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
			}})

		case strings.Contains(req.Query, "mutation WebhookSubscriptionCreate"):
			f.calls = append(f.calls, "create")
			if f.createLimit > 0 && f.createCount >= f.createLimit {
				f.writeJSON(w, map[string]any{"webhookSubscriptionCreate": map[string]any{
					"userErrors": []map[string]any{{"message": "quota exceeded"}},
				}})
				return
			}
			f.createCount++
			if f.failNext == "create" {
				f.writeJSON(w, map[string]any{"webhookSubscriptionCreate": map[string]any{
					"userErrors": []map[string]any{{"field": []string{"uri"}, "message": "is invalid"}},
				}})
				return
			}
			input, _ := req.Variables["webhookSubscription"].(map[string]any)
			sub := map[string]any{
				"id":     f.newID(),
				"topic":  req.Variables["topic"],
				"format": "JSON",
			}
			for k, v := range input {
				sub[k] = v
			}
			f.subs = append(f.subs, sub)
			f.writeJSON(w, map[string]any{"webhookSubscriptionCreate": map[string]any{
				"webhookSubscription": sub, "userErrors": []any{},
			}})

		case strings.Contains(req.Query, "mutation WebhookSubscriptionUpdate"):
			f.calls = append(f.calls, "update")
			id, _ := req.Variables["id"].(string)
			input, _ := req.Variables["webhookSubscription"].(map[string]any)
			f.lastUpdateInput = input
			for _, sub := range f.subs {
				if sub["id"] == id {
					for k, v := range input {
						sub[k] = v
					}
					f.writeJSON(w, map[string]any{"webhookSubscriptionUpdate": map[string]any{
						"webhookSubscription": sub, "userErrors": []any{},
					}})
					return
				}
			}
			f.writeJSON(w, map[string]any{"webhookSubscriptionUpdate": map[string]any{
				"userErrors": []map[string]any{{"message": "not found"}},
			}})

		case strings.Contains(req.Query, "mutation WebhookSubscriptionDelete"):
			f.calls = append(f.calls, "delete")
			id, _ := req.Variables["id"].(string)
			kept := f.subs[:0]
			for _, sub := range f.subs {
				if sub["id"] != id {
					kept = append(kept, sub)
				}
			}
			f.subs = kept
			f.writeJSON(w, map[string]any{"webhookSubscriptionDelete": map[string]any{
				"deletedWebhookSubscriptionId": id, "userErrors": []any{},
			}})

		case strings.Contains(req.Query, "query WebhookTopics"):
			f.calls = append(f.calls, "topics")
			f.writeJSON(w, map[string]any{"__type": map[string]any{"enumValues": []map[string]any{
				{"name": "ORDERS_CREATE"}, {"name": "ORDERS_UPDATED"}, {"name": "PRODUCTS_UPDATE"},
			}}})

		default:
			t.Errorf("fake API got an unexpected query: %s", req.Query)
			f.writeJSON(w, map[string]any{})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAPI) writeJSON(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (f *fakeAPI) newID() string {
	f.nextID++
	return "gid://shopify/WebhookSubscription/" + itoa(f.nextID)
}

func (f *fakeAPI) subscriptionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// run executes a webhooks subcommand against the fake API in json mode.
func run(t *testing.T, api *fakeAPI, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runFormat(t, api, "json", args...)
}

// runFormat is run with an explicit output format.
func runFormat(t *testing.T, api *fakeAPI, format string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	srv := api.server(t)

	streams, out, errOut := iostreams.Test()
	f := app.NewFactory(streams)
	f.Options.OutputFormat = format
	f.ConfigFunc = func() (*config.Config, error) {
		cfg := config.New()
		cfg.SetProfile("test", &config.Profile{
			Shop: "acme.myshopify.com", AccessToken: "shpat_test", APIVersion: "2026-04",
		})
		return cfg, nil
	}
	f.ClientFunc = func(context.Context) (*shopify.Client, error) {
		return shopify.New("acme.myshopify.com", "shpat_test", "2026-04",
			shopify.WithBaseURL(srv.URL), shopify.WithHTTPClient(srv.Client()))
	}

	cmd := webhooks.NewCommand(f)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestListReturnsSubscriptions(t *testing.T) {
	api := newFakeAPI(map[string]any{
		"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE",
		"uri": "https://acme.dev/orders", "format": "JSON",
	})

	stdout, _, err := run(t, api, "list")
	if err != nil {
		t.Fatalf("webhooks list returned error: %v", err)
	}

	var got webhooks.SubscriptionList
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(got.Webhooks) != 1 || got.Webhooks[0].URI != "https://acme.dev/orders" {
		t.Errorf("webhooks = %+v, want the one seeded subscription", got.Webhooks)
	}
}

func TestCreateRejectsPlainHTTPBeforeCallingTheAPI(t *testing.T) {
	api := newFakeAPI()

	_, _, err := run(t, api, "create", "--topic", "orders/create", "--uri", "http://acme.dev/orders")
	if err == nil {
		t.Fatal("create with an http uri = nil error, want a validation error")
	}
	if len(api.calls) != 0 {
		t.Errorf("the API was called %v despite invalid input; validation should happen first", api.calls)
	}
}

func TestSyncCreatesUpdatesAndConverges(t *testing.T) {
	api := newFakeAPI(map[string]any{
		// Same topic+uri as the manifest, but a stale filter -> update.
		"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE",
		"uri": "https://acme.dev/orders", "format": "JSON", "filter": "status:open",
	})

	manifest := writeManifest(t, `
webhooks:
  - topic: ORDERS_CREATE
    uri: https://acme.dev/orders
    filter: "status:closed"
  - topic: PRODUCTS_UPDATE
    uri: https://acme.dev/products
`)

	stdout, stderr, err := run(t, api, "sync", "--file", manifest, "--yes")
	if err != nil {
		t.Fatalf("sync returned error: %v (stderr: %s)", err, stderr)
	}

	var result webhooks.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(result.Created) != 1 || len(result.Updated) != 1 {
		t.Errorf("result = %+v, want 1 create and 1 update", result)
	}
	if api.subscriptionCount() != 2 {
		t.Errorf("store has %d subscriptions, want 2", api.subscriptionCount())
	}

	// Running again must be a no-op: that is the whole point of a sync.
	stdout, stderr, err = run(t, api, "sync", "--file", manifest, "--yes")
	if err != nil {
		t.Fatalf("second sync returned error: %v", err)
	}
	if !strings.Contains(stderr, "no changes") {
		t.Errorf("second sync was not a no-op; stderr = %q", stderr)
	}
	var second webhooks.Result
	_ = json.Unmarshal([]byte(stdout), &second)
	if len(second.Created)+len(second.Updated)+len(second.Deleted) != 0 {
		t.Errorf("second sync changed things: %+v", second)
	}
}

func TestSyncOnlyPrunesWhenAsked(t *testing.T) {
	seed := func() *fakeAPI {
		return newFakeAPI(map[string]any{
			"id": "gid://shopify/WebhookSubscription/9", "topic": "CUSTOMERS_CREATE",
			"uri": "https://other.dev/customers", "format": "JSON",
		})
	}
	manifest := writeManifest(t, "webhooks:\n  - topic: ORDERS_CREATE\n    uri: https://acme.dev/orders\n")

	api := seed()
	if _, _, err := run(t, api, "sync", "--file", manifest, "--yes"); err != nil {
		t.Fatalf("sync returned error: %v", err)
	}
	if api.subscriptionCount() != 2 {
		t.Errorf("sync without --prune removed the unmanaged webhook; count = %d, want 2",
			api.subscriptionCount())
	}

	api = seed()
	if _, _, err := run(t, api, "sync", "--file", manifest, "--prune", "--yes"); err != nil {
		t.Fatalf("sync --prune returned error: %v", err)
	}
	if api.subscriptionCount() != 1 {
		t.Errorf("sync --prune left %d subscriptions, want only the managed one",
			api.subscriptionCount())
	}
}

func TestSyncDryRunChangesNothing(t *testing.T) {
	api := newFakeAPI()
	manifest := writeManifest(t, "webhooks:\n  - topic: ORDERS_CREATE\n    uri: https://acme.dev/orders\n")

	if _, _, err := run(t, api, "sync", "--file", manifest, "--dry-run"); err != nil {
		t.Fatalf("sync --dry-run returned error: %v", err)
	}
	if api.subscriptionCount() != 0 {
		t.Errorf("--dry-run created %d subscriptions, want 0", api.subscriptionCount())
	}
	for _, call := range api.calls {
		if call != "list" {
			t.Errorf("--dry-run performed a %q call; it must only read", call)
		}
	}
}

func TestDiffExitCodeSignalsDrift(t *testing.T) {
	api := newFakeAPI()
	manifest := writeManifest(t, "webhooks:\n  - topic: ORDERS_CREATE\n    uri: https://acme.dev/orders\n")

	_, _, err := run(t, api, "diff", "--file", manifest, "--exit-code")
	if err == nil {
		t.Fatal("diff --exit-code = nil error while drifted, want a non-zero exit")
	}
	if !strings.Contains(err.Error(), "drifted") {
		t.Errorf("error = %q, want it to explain the drift", err)
	}
}

func TestSyncRefusesToPromptWithoutATerminal(t *testing.T) {
	api := newFakeAPI()
	manifest := writeManifest(t, "webhooks:\n  - topic: ORDERS_CREATE\n    uri: https://acme.dev/orders\n")

	// No --yes and no TTY: it must refuse rather than silently apply.
	_, _, err := run(t, api, "sync", "--file", manifest)
	if err == nil {
		t.Fatal("sync without --yes on a non-terminal = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %q, want it to point at --yes", err)
	}
	if api.subscriptionCount() != 0 {
		t.Error("sync applied changes without confirmation")
	}
}

func TestSurfacesUserErrors(t *testing.T) {
	api := newFakeAPI()
	api.failNext = "create"

	_, _, err := run(t, api, "create", "--topic", "ORDERS_CREATE", "--uri", "https://acme.dev/orders")
	if err == nil {
		t.Fatal("create = nil error, want the API userErrors surfaced")
	}
	if !strings.Contains(err.Error(), "uri: is invalid") {
		t.Errorf("error = %q, want the field and message from userErrors", err)
	}
}

func TestTopicsComeFromTheSchema(t *testing.T) {
	api := newFakeAPI()

	stdout, _, err := run(t, api, "topics", "--search", "order")
	if err != nil {
		t.Fatalf("topics returned error: %v", err)
	}

	var got webhooks.TopicList
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(got.Topics) != 2 {
		t.Errorf("topics = %v, want the two ORDERS_* entries", got.Topics)
	}
}

// The default, human-facing path: a table plan on stderr and a readable
// summary. Everything above runs in json mode, so this covers the other half.
func TestDiffRendersATableAndSummary(t *testing.T) {
	api := newFakeAPI(map[string]any{
		"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE",
		"uri": "https://acme.dev/orders", "format": "JSON", "filter": "status:open",
	})
	manifest := writeManifest(t, `
webhooks:
  - topic: ORDERS_CREATE
    uri: https://acme.dev/orders
    filter: "status:closed"
  - topic: PRODUCTS_UPDATE
    uri: https://acme.dev/products
`)

	streams, out, errOut := iostreams.Test()
	f := app.NewFactory(streams)
	srv := api.server(t)
	f.ConfigFunc = func() (*config.Config, error) {
		cfg := config.New()
		cfg.SetProfile("test", &config.Profile{
			Shop: "acme.myshopify.com", AccessToken: "shpat_test", APIVersion: "2026-04",
		})
		return cfg, nil
	}
	f.ClientFunc = func(context.Context) (*shopify.Client, error) {
		return shopify.New("acme.myshopify.com", "shpat_test", "2026-04",
			shopify.WithBaseURL(srv.URL), shopify.WithHTTPClient(srv.Client()))
	}

	cmd := webhooks.NewCommand(f)
	cmd.SetArgs([]string{"diff", "--file", manifest})
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("diff returned error: %v", err)
	}

	table := out.String()
	for _, want := range []string{"ACTION", "TOPIC", "create", "update", "PRODUCTS_UPDATE"} {
		if !strings.Contains(table, want) {
			t.Errorf("table output missing %q:\n%s", want, table)
		}
	}
	if !strings.Contains(errOut.String(), "1 to create, 1 to update") {
		t.Errorf("summary = %q, want a create/update tally", errOut.String())
	}
	// Unchanged rows are noise in a terminal; they belong to json output only.
	if strings.Contains(table, "unchanged") {
		t.Errorf("table shows unchanged rows:\n%s", table)
	}
}

func TestListFollowsPagination(t *testing.T) {
	api := newFakeAPI(
		map[string]any{"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE", "uri": "https://acme.dev/a", "format": "JSON"},
		map[string]any{"id": "gid://shopify/WebhookSubscription/2", "topic": "ORDERS_PAID", "uri": "https://acme.dev/b", "format": "JSON"},
	)
	api.paginate = true

	stdout, _, err := run(t, api, "list")
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}

	var got webhooks.SubscriptionList
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(got.Webhooks) != 2 {
		t.Errorf("collected %d webhooks, want both pages", len(got.Webhooks))
	}
	if len(api.afterSeen) != 2 || api.afterSeen[0] != "" || api.afterSeen[1] != "cursor-1" {
		t.Errorf("cursors sent = %q, want [\"\", \"cursor-1\"]", api.afterSeen)
	}
}

func TestGetReturnsOneSubscription(t *testing.T) {
	api := newFakeAPI(map[string]any{
		"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE",
		"uri": "https://acme.dev/orders", "format": "JSON",
	})

	// A bare numeric id must be accepted as well as a full gid.
	stdout, _, err := run(t, api, "get", "1")
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	var got webhooks.Subscription
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if got.Topic != "ORDERS_CREATE" {
		t.Errorf("topic = %q, want ORDERS_CREATE", got.Topic)
	}
}

func TestGetMissingSubscription(t *testing.T) {
	api := newFakeAPI()

	_, _, err := run(t, api, "get", "404")
	if err == nil {
		t.Fatal("get on a missing id = nil error, want a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to say not found", err)
	}
}

func TestUpdateRejectsTopicChange(t *testing.T) {
	api := newFakeAPI()

	_, _, err := run(t, api, "update", "1", "--topic", "ORDERS_PAID")
	if err == nil {
		t.Fatal("update --topic = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "cannot be changed") {
		t.Errorf("error = %q, want it to explain that topics are immutable", err)
	}
	if len(api.calls) != 0 {
		t.Errorf("the API was called %v; the guard must run first", api.calls)
	}
}

func TestUpdateRequiresAtLeastOneChange(t *testing.T) {
	api := newFakeAPI()

	_, _, err := run(t, api, "update", "1")
	if err == nil {
		t.Fatal("update with no flags = nil error, want a usage error")
	}
	if !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("error = %q, want it to say there is nothing to update", err)
	}
}

// Omitted flags must not be sent, or an update would silently clear fields the
// user never mentioned.
func TestUpdateSendsOnlyTheFlagsGiven(t *testing.T) {
	api := newFakeAPI(map[string]any{
		"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE",
		"uri": "https://acme.dev/orders", "format": "JSON", "filter": "status:open",
	})

	if _, _, err := run(t, api, "update", "1", "--uri", "https://acme.dev/orders-v2"); err != nil {
		t.Fatalf("update returned error: %v", err)
	}

	if len(api.lastUpdateInput) != 1 {
		t.Errorf("update sent %v, want only the uri field", api.lastUpdateInput)
	}
	if api.lastUpdateInput["uri"] != "https://acme.dev/orders-v2" {
		t.Errorf("uri = %v, want the new value", api.lastUpdateInput["uri"])
	}
}

func TestUpdateRejectsBadURIBeforeCallingTheAPI(t *testing.T) {
	api := newFakeAPI()

	if _, _, err := run(t, api, "update", "1", "--uri", "http://acme.dev/x"); err == nil {
		t.Fatal("update with an http uri = nil error, want a validation error")
	}
	if len(api.calls) != 0 {
		t.Errorf("the API was called %v despite invalid input", api.calls)
	}
}

func TestDeleteRemovesEachID(t *testing.T) {
	api := newFakeAPI(
		map[string]any{"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE", "uri": "https://acme.dev/a"},
		map[string]any{"id": "gid://shopify/WebhookSubscription/2", "topic": "ORDERS_PAID", "uri": "https://acme.dev/b"},
	)

	if _, _, err := run(t, api, "delete", "1", "2", "--yes"); err != nil {
		t.Fatalf("delete returned error: %v", err)
	}
	if api.subscriptionCount() != 0 {
		t.Errorf("%d subscriptions left, want both deleted", api.subscriptionCount())
	}
}

func TestDeleteRefusesWithoutConfirmation(t *testing.T) {
	api := newFakeAPI(map[string]any{
		"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE", "uri": "https://acme.dev/a",
	})

	_, _, err := run(t, api, "delete", "1")
	if err == nil {
		t.Fatal("delete without --yes on a non-terminal = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %q, want it to point at --yes", err)
	}
	if api.subscriptionCount() != 1 {
		t.Error("delete removed a subscription without confirmation")
	}
}

// When a change fails halfway, the user must still learn what did happen.
func TestSyncReportsWhatSucceededBeforeAFailure(t *testing.T) {
	api := newFakeAPI()
	api.createLimit = 1 // the second create fails

	manifest := writeManifest(t, `
webhooks:
  - topic: ORDERS_CREATE
    uri: https://acme.dev/a
  - topic: ORDERS_PAID
    uri: https://acme.dev/b
`)

	stdout, _, err := run(t, api, "sync", "--file", manifest, "--yes")
	if err == nil {
		t.Fatal("sync = nil error, want the failed create surfaced")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error = %q, want the API message", err)
	}

	var result webhooks.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("no machine-readable result on failure: %v\n%s", err, stdout)
	}
	if len(result.Created) != 1 {
		t.Errorf("result = %+v, want the one successful create reported", result)
	}
	if api.subscriptionCount() != 1 {
		t.Errorf("store has %d subscriptions, want the first create to have stuck", api.subscriptionCount())
	}
}

func TestCreateSucceeds(t *testing.T) {
	api := newFakeAPI()

	stdout, stderr, err := run(t, api, "create",
		"--topic", "orders/create",
		"--uri", "https://acme.dev/orders",
		"--include-field", "id",
		"--filter", "status:open")
	if err != nil {
		t.Fatalf("create returned error: %v", err)
	}

	var got webhooks.Subscription
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if got.Topic != "ORDERS_CREATE" {
		t.Errorf("topic = %q, want the normalised ORDERS_CREATE", got.Topic)
	}
	if api.subscriptionCount() != 1 {
		t.Errorf("store has %d subscriptions, want 1", api.subscriptionCount())
	}
	// Progress goes to stderr so that stdout stays pipeable.
	if !strings.Contains(stderr, "Created") {
		t.Errorf("stderr = %q, want a confirmation line", stderr)
	}
	if strings.Contains(stdout, "Created") {
		t.Errorf("the confirmation leaked into stdout:\n%s", stdout)
	}
}

// Every result type must render as a table, not just as JSON: a Rows/Headers
// mismatch would only ever show up in the default output mode.
func TestTableRenderingForEachResultType(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"list", []string{"list"}, []string{"ID", "TOPIC", "URI", "ORDERS_CREATE"}},
		{"get", []string{"get", "1"}, []string{"ID", "INCLUDE FIELDS", "ORDERS_CREATE"}},
		{"topics", []string{"topics"}, []string{"TOPIC", "PRODUCTS_UPDATE"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(map[string]any{
				"id": "gid://shopify/WebhookSubscription/1", "topic": "ORDERS_CREATE",
				"uri": "https://acme.dev/orders", "format": "JSON",
			})

			stdout, _, err := runFormat(t, api, "table", tt.args...)
			if err != nil {
				t.Fatalf("%v returned error: %v", tt.args, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("table output missing %q:\n%s", want, stdout)
				}
			}
		})
	}
}

func TestSyncResultRendersAsATable(t *testing.T) {
	api := newFakeAPI()
	manifest := writeManifest(t, "webhooks:\n  - topic: ORDERS_CREATE\n    uri: https://acme.dev/orders\n")

	stdout, _, err := runFormat(t, api, "table", "sync", "--file", manifest, "--yes")
	if err != nil {
		t.Fatalf("sync returned error: %v", err)
	}
	for _, want := range []string{"ACTION", "WEBHOOK", "created", "ORDERS_CREATE"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("sync table output missing %q:\n%s", want, stdout)
		}
	}
}
