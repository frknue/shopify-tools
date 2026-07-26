package webhooks

import (
	"fmt"
	"sort"
	"strings"
)

// Action is what sync would do to one subscription.
type Action string

// The possible plan actions.
const (
	ActionCreate    Action = "create"
	ActionUpdate    Action = "update"
	ActionDelete    Action = "delete"
	ActionUnchanged Action = "unchanged"
)

// Change is one entry of a plan.
type Change struct {
	Action Action `json:"action" yaml:"action"`
	Topic  string `json:"topic" yaml:"topic"`
	URI    string `json:"uri" yaml:"uri"`
	// ID is the existing subscription, empty for creates.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Details explains an update, or why a delete was proposed.
	Details []string `json:"details,omitempty" yaml:"details,omitempty"`
	// Spec is the desired state; nil for deletes.
	Spec *Spec `json:"spec,omitempty" yaml:"spec,omitempty"`
}

// Plan is the full set of changes between the manifest and the live store.
type Plan struct {
	Changes []Change `json:"changes" yaml:"changes"`
	// Pruned reports whether deletes were included in this plan.
	Pruned bool `json:"pruned" yaml:"pruned"`
}

// Headers implements output.Tabler.
func (p Plan) Headers() []string { return []string{"ACTION", "TOPIC", "URI", "DETAIL"} }

// Rows implements output.Tabler.
func (p Plan) Rows() [][]string {
	rows := make([][]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		rows = append(rows, []string{string(c.Action), c.Topic, c.URI, strings.Join(c.Details, ", ")})
	}
	return rows
}

// Counts tallies the plan by action.
func (p Plan) Counts() (create, update, del, unchanged int) {
	for _, c := range p.Changes {
		switch c.Action {
		case ActionCreate:
			create++
		case ActionUpdate:
			update++
		case ActionDelete:
			del++
		case ActionUnchanged:
			unchanged++
		}
	}
	return create, update, del, unchanged
}

// HasChanges reports whether anything would actually be modified.
func (p Plan) HasChanges() bool {
	create, update, del, _ := p.Counts()
	return create+update+del > 0
}

// Summary renders a one-line tally, e.g. "2 to create, 1 to update".
func (p Plan) Summary() string {
	create, update, del, unchanged := p.Counts()
	parts := make([]string, 0, 4)
	if create > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", create))
	}
	if update > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", update))
	}
	if del > 0 {
		parts = append(parts, fmt.Sprintf("%d to delete", del))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("no changes; %d webhook(s) already match the manifest", unchanged)
	}
	if unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", unchanged))
	}
	return strings.Join(parts, ", ")
}

// BuildPlan diffs the desired manifest against the live subscriptions.
//
// Identity is (topic, uri): that is the only stable key the API exposes, so a
// changed uri is a create plus — when pruning — a delete, not an in-place
// update. Everything else (format, filter, include_fields, metafield
// namespaces) is an update.
//
// Deletes are only proposed when prune is true. Without it, sync leaves
// subscriptions the manifest does not mention alone, which is the safe default
// when several tools manage webhooks on the same store.
func BuildPlan(desired []Spec, live []Subscription, prune bool) Plan {
	plan := Plan{Pruned: prune}

	byKey := make(map[string]*Subscription, len(live))
	for i := range live {
		byKey[live[i].toSpec().key()] = &live[i]
	}

	matched := make(map[string]bool, len(desired))
	for _, want := range desired {
		key := want.key()
		existing, found := byKey[key]
		if !found {
			spec := want
			plan.Changes = append(plan.Changes, Change{
				Action: ActionCreate,
				Topic:  want.Topic,
				URI:    want.URI,
				Spec:   &spec,
			})
			continue
		}

		matched[key] = true
		spec := want
		if diffs := differences(existing.toSpec(), want); len(diffs) > 0 {
			plan.Changes = append(plan.Changes, Change{
				Action:  ActionUpdate,
				Topic:   want.Topic,
				URI:     want.URI,
				ID:      existing.ID,
				Details: diffs,
				Spec:    &spec,
			})
			continue
		}
		plan.Changes = append(plan.Changes, Change{
			Action: ActionUnchanged,
			Topic:  want.Topic,
			URI:    want.URI,
			ID:     existing.ID,
			Spec:   &spec,
		})
	}

	if prune {
		orphans := make([]*Subscription, 0)
		for key, sub := range byKey {
			if !matched[key] {
				orphans = append(orphans, sub)
			}
		}
		// Map iteration is random; sort so plans are reproducible.
		sort.Slice(orphans, func(i, j int) bool {
			if orphans[i].Topic != orphans[j].Topic {
				return orphans[i].Topic < orphans[j].Topic
			}
			return orphans[i].URI < orphans[j].URI
		})
		for _, sub := range orphans {
			plan.Changes = append(plan.Changes, Change{
				Action:  ActionDelete,
				Topic:   sub.Topic,
				URI:     sub.URI,
				ID:      sub.ID,
				Details: []string{"not in manifest"},
			})
		}
	}

	return plan
}
