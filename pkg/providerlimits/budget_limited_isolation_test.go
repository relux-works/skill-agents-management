package providerlimits

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// TestBudgetLimitedChangesNoGroupRecord is AC 13's negative test.
//
// `budgetLimited` reports exhaustion of a cap THIS RUNTIME imposed on ONE run:
// codexHost.ApplyBoardGoal sends params["tokenBudget"] = revision.Budget.Limit
// from a board goal's GoalBudget{Kind: tokens}. It therefore proves nothing
// about the shared subscription, and letting it suppress a group would let a
// single small-budget run block every unrelated spawn on that pool.
//
// The test drives the whole route a caller would take — classify the managed
// signal, try to convert it into an Observation, try to Observe it — and
// asserts the group record is byte-identical afterwards.
func TestBudgetLimitedChangesNoGroupRecord(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(Options{Layout: LayoutAt(root)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Setenv(homeEnvVarFor(t, ProviderCodex), filepath.Join(root, "codex-home"))
	identity, err := ResolveIdentity(ProviderCodex)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	const group = "openai-plan"

	before, existedBefore := store.GroupRecordFor(identity, group)

	classification := ClassifyManaged(ProviderCodex, ManagedSignalBudgetLimited)
	if classification.IsProviderQuota() {
		t.Fatal("budget_limited was classified as shared-subscription quota evidence")
	}
	if !classification.IsRunBudget() {
		t.Fatalf("budget_limited class = %q, want run_budget", classification.Class)
	}

	obs, ok := classification.QuotaObservation(Evidence{RunID: "RUN-260803-0000bud", Model: "gpt-5-codex"})
	if ok {
		t.Fatal("a run-budget classification minted a quota observation")
	}
	if err := store.Observe(identity, group, nil, obs); !errors.Is(err, ErrNotProviderQuota) {
		t.Fatalf("Observe(budget_limited) = %v, want ErrNotProviderQuota", err)
	}

	after, existedAfter := store.GroupRecordFor(identity, group)
	if existedAfter != existedBefore {
		t.Fatalf("budget_limited created a group record: existed %v -> %v", existedBefore, existedAfter)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("budget_limited changed the group record:\nbefore %+v\nafter  %+v", before, after)
	}
	if state := store.LoadIdentityState(identity); len(state.Groups) != 0 {
		t.Fatalf("budget_limited wrote %d group records", len(state.Groups))
	}
}

// TestUsageLimitedStillSuppressesTheGroup narrows the exclusion above: removing
// the cap we impose is not the same as ignoring a limit the provider imposes.
// If this test ever fails, the budget_limited exclusion has been over-applied
// and real subscription exhaustion has stopped suppressing anything.
func TestUsageLimitedStillSuppressesTheGroup(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(Options{Layout: LayoutAt(root)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Setenv(homeEnvVarFor(t, ProviderCodex), filepath.Join(root, "codex-home"))
	identity, err := ResolveIdentity(ProviderCodex)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	const group = "openai-plan"

	classification := ClassifyManaged(ProviderCodex, ManagedSignalUsageLimited)
	if !classification.IsProviderQuota() {
		t.Fatalf("usage_limited class = %q, want provider_quota", classification.Class)
	}
	obs, ok := classification.QuotaObservation(Evidence{RunID: "RUN-260803-0000usg", Model: "gpt-5-codex"})
	if !ok {
		t.Fatal("usage_limited failed to mint a quota observation")
	}
	if err := store.Observe(identity, group, nil, obs); err != nil {
		t.Fatalf("Observe(usage_limited): %v", err)
	}
	record, existed := store.GroupRecordFor(identity, group)
	if !existed || record.SuppressedSince == nil {
		t.Fatalf("usage_limited did not suppress the group: existed=%v record=%+v", existed, record)
	}
}
