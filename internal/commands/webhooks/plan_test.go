package webhooks_test

import (
	"testing"

	"github.com/frknue/shopify-tools/internal/commands/webhooks"
)

func spec(topic, uri string) webhooks.Spec {
	s := webhooks.Spec{Topic: topic, URI: uri}
	s.Normalize()
	return s
}

func TestBuildPlanCreatesMissing(t *testing.T) {
	t.Parallel()

	plan := webhooks.BuildPlan(
		[]webhooks.Spec{spec("ORDERS_CREATE", "https://acme.dev/orders")},
		nil,
		false,
	)

	if len(plan.Changes) != 1 || plan.Changes[0].Action != webhooks.ActionCreate {
		t.Fatalf("plan = %+v, want a single create", plan.Changes)
	}
}

func TestBuildPlanLeavesMatchingAlone(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE",
		URI: "https://acme.dev/orders", Format: "JSON",
	}}

	plan := webhooks.BuildPlan([]webhooks.Spec{spec("ORDERS_CREATE", "https://acme.dev/orders")}, live, false)

	if len(plan.Changes) != 1 || plan.Changes[0].Action != webhooks.ActionUnchanged {
		t.Fatalf("plan = %+v, want a single unchanged", plan.Changes)
	}
	if plan.HasChanges() {
		t.Error("HasChanges() = true for an already-matching store")
	}
}

// An unset format in the manifest must not read as a change: the API defaults
// it to JSON, so comparing "" against "JSON" would make sync never converge.
func TestBuildPlanTreatsEmptyFormatAsJSON(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE",
		URI: "https://acme.dev/orders", Format: "JSON",
	}}
	desired := spec("ORDERS_CREATE", "https://acme.dev/orders") // no format set

	plan := webhooks.BuildPlan([]webhooks.Spec{desired}, live, false)

	if plan.HasChanges() {
		t.Errorf("an unset format was treated as drift: %+v", plan.Changes)
	}
}

func TestBuildPlanUpdatesChangedAttributes(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE",
		URI: "https://acme.dev/orders", Format: "JSON", Filter: "status:open",
	}}
	desired := webhooks.Spec{
		Topic: "ORDERS_CREATE", URI: "https://acme.dev/orders",
		Filter: "status:closed", IncludeFields: []string{"id"},
	}
	desired.Normalize()

	plan := webhooks.BuildPlan([]webhooks.Spec{desired}, live, false)

	if len(plan.Changes) != 1 || plan.Changes[0].Action != webhooks.ActionUpdate {
		t.Fatalf("plan = %+v, want a single update", plan.Changes)
	}
	if plan.Changes[0].ID != "gid://shopify/WebhookSubscription/1" {
		t.Errorf("update targets %q, want the existing subscription id", plan.Changes[0].ID)
	}
	if len(plan.Changes[0].Details) != 2 {
		t.Errorf("details = %v, want both filter and include_fields reported", plan.Changes[0].Details)
	}
}

// include_fields order is not meaningful; a reordered list is not drift.
func TestBuildPlanIgnoresIncludeFieldOrder(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE",
		URI: "https://acme.dev/orders", Format: "JSON",
		IncludeFields: []string{"total_price", "id"},
	}}
	desired := webhooks.Spec{
		Topic: "ORDERS_CREATE", URI: "https://acme.dev/orders",
		IncludeFields: []string{"id", "total_price"},
	}
	desired.Normalize()

	if plan := webhooks.BuildPlan([]webhooks.Spec{desired}, live, false); plan.HasChanges() {
		t.Errorf("reordered include_fields read as drift: %+v", plan.Changes)
	}
}

func TestBuildPlanKeepsUnknownWebhooksWithoutPrune(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/9", Topic: "CUSTOMERS_CREATE",
		URI: "https://other.dev/customers", Format: "JSON",
	}}
	desired := []webhooks.Spec{spec("ORDERS_CREATE", "https://acme.dev/orders")}

	plan := webhooks.BuildPlan(desired, live, false)
	for _, c := range plan.Changes {
		if c.Action == webhooks.ActionDelete {
			t.Fatalf("a delete was planned without --prune: %+v", c)
		}
	}

	pruned := webhooks.BuildPlan(desired, live, true)
	_, _, deletes, _ := pruned.Counts()
	if deletes != 1 {
		t.Errorf("delete count with --prune = %d, want 1", deletes)
	}
}

// A changed uri is a replace, not an in-place edit, because (topic, uri) is
// the identity.
func TestBuildPlanTreatsChangedURIAsReplacement(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE",
		URI: "https://old.dev/orders", Format: "JSON",
	}}
	desired := []webhooks.Spec{spec("ORDERS_CREATE", "https://new.dev/orders")}

	plan := webhooks.BuildPlan(desired, live, true)
	creates, updates, deletes, _ := plan.Counts()
	if creates != 1 || deletes != 1 || updates != 0 {
		t.Errorf("counts = %d create / %d update / %d delete, want 1 create and 1 delete",
			creates, updates, deletes)
	}
}

// Deletes come from a map; without sorting the plan would differ run to run.
func TestBuildPlanIsDeterministic(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{
		{ID: "gid://shopify/WebhookSubscription/3", Topic: "CUSTOMERS_CREATE", URI: "https://acme.dev/c"},
		{ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE", URI: "https://acme.dev/a"},
		{ID: "gid://shopify/WebhookSubscription/2", Topic: "ORDERS_CREATE", URI: "https://acme.dev/b"},
	}

	first := webhooks.BuildPlan(nil, live, true)
	for range 20 {
		next := webhooks.BuildPlan(nil, live, true)
		for i := range first.Changes {
			if first.Changes[i].ID != next.Changes[i].ID {
				t.Fatalf("plan order is not stable: %v vs %v", first.Changes, next.Changes)
			}
		}
	}
}

// The mirror of the case above: a subscription whose format the API omitted,
// against a manifest that spells JSON out. Both directions must normalise, or
// sync proposes the same no-op update forever.
func TestBuildPlanTreatsMissingLiveFormatAsJSON(t *testing.T) {
	t.Parallel()

	live := []webhooks.Subscription{{
		ID: "gid://shopify/WebhookSubscription/1", Topic: "ORDERS_CREATE",
		URI: "https://acme.dev/orders", // no format returned
	}}
	desired := webhooks.Spec{Topic: "ORDERS_CREATE", URI: "https://acme.dev/orders", Format: "JSON"}
	desired.Normalize()

	if plan := webhooks.BuildPlan([]webhooks.Spec{desired}, live, false); plan.HasChanges() {
		t.Errorf("a missing live format read as drift: %+v", plan.Changes)
	}
}
