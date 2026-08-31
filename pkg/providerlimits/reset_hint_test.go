package providerlimits

import (
	"testing"
	"time"
)

// The observed 2026-08-01 hint pointed at Aug 5th — roughly three days out. Trusting
// it would have hidden a healthy provider for three extra days: probes on 2026-08-02
// succeeded with the weekly limit 91% unused. The hint must not extend anything.
func TestAugustFifthHintDoesNotExtendSuppression(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)

	record := f.record(t, codexPlan)
	want := f.clock.Now().Add(2 * time.Minute)
	if !record.NextProbeAt.Equal(want) {
		t.Fatalf("next_probe_at = %s, want the ladder's first step (%s); the provider's own date must never lengthen a suppression", record.NextProbeAt, want)
	}
	if record.ProviderResetHint == nil {
		t.Fatal("the hint must still be stored for diagnosis")
	}
	if record.ProviderResetHint.Used {
		t.Fatal("a hint later than the ladder step must be stored with used=false")
	}
	if record.ProviderResetHint.Parsed == nil {
		t.Fatal("the parsed instant is diagnostic and must be kept")
	}
	if !record.ProviderResetHint.Parsed.After(f.clock.Now().Add(48 * time.Hour)) {
		t.Fatalf("the fixture must carry the far-future Aug-5 hint, got %s", record.ProviderResetHint.Parsed)
	}
	if record.ProviderResetHint.Note != ResetHintNote {
		t.Errorf("note = %q", record.ProviderResetHint.Note)
	}

	// And the group is genuinely claimable two minutes later, not three days later.
	f.clock.Advance(2 * time.Minute)
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000d0"); !granted {
		t.Fatal("the group must be probe-eligible after the ladder step, whatever the hint said")
	}
}

// An earlier hint shortens the step, because it can only make the runtime try
// sooner. That is the same asymmetry the operator clear command uses.
func TestEarlierHintShortensTheStepAndIsMarkedUsed(t *testing.T) {
	f := newStoreFixture(t)
	class := ClassifyCodexPrompt(CodexPromptResult{
		ExitCode: 1,
		Now:      f.clock.Now(),
		Log:      []byte("ERROR: You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage or try again in 30 seconds.\n"),
	})
	if class.ResetHint == nil || class.ResetHint.Parsed == nil {
		t.Fatalf("hint = %+v", class.ResetHint)
	}
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260802-0000d1"})
	if !ok {
		t.Fatal("expected a provider-quota observation")
	}
	if err := f.store.Observe(f.identity, codexPlan, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	record := f.record(t, codexPlan)
	want := f.clock.Now().Add(30 * time.Second)
	if !record.NextProbeAt.Equal(want) {
		t.Fatalf("next_probe_at = %s, want the shortened %s", record.NextProbeAt, want)
	}
	if record.ProviderResetHint == nil || !record.ProviderResetHint.Used {
		t.Fatalf("a hint that bound the min() must be stored with used=true: %+v", record.ProviderResetHint)
	}
}

// A hint in the past cannot pull next_probe_at behind now.
func TestPastHintIsIgnored(t *testing.T) {
	f := newStoreFixture(t)
	past := f.clock.Now().Add(-72 * time.Hour)
	obs := mustQuotaObservation(t, Classification{
		Class:     ClassProviderQuota,
		Provider:  ProviderCodex,
		Marker:    "you've hit your usage limit",
		ResetHint: &ResetHint{Raw: "or try again at a moment long gone", Parsed: &past},
	})
	if err := f.store.Observe(f.identity, codexPlan, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	record := f.record(t, codexPlan)
	if record.NextProbeAt == nil || record.NextProbeAt.Before(f.clock.Now()) {
		t.Fatalf("next_probe_at = %s, before now (%s)", record.NextProbeAt, f.clock.Now())
	}
	if !record.NextProbeAt.Equal(f.clock.Now().Add(2 * time.Minute)) {
		t.Fatalf("next_probe_at = %s, want the ladder step", record.NextProbeAt)
	}
	if record.ProviderResetHint.Used {
		t.Fatal("a past hint must not be marked used")
	}
}

// An observation inside an existing suppression window records its evidence
// without escalating and without moving the window. That is the rule that bounds
// the ladder to one step per window however many concurrent failures arrive.
func TestObservationInsideTheWindowDoesNotEscalate(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	before := f.record(t, codexPlan)

	f.clock.Advance(30 * time.Second)
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000d2"); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	after := f.record(t, codexPlan)
	if after.BackoffStep != before.BackoffStep {
		t.Fatalf("backoff step went from %d to %d inside one window", before.BackoffStep, after.BackoffStep)
	}
	if before.NextProbeAt == nil || after.NextProbeAt == nil || !after.NextProbeAt.Equal(*before.NextProbeAt) {
		t.Fatalf("next_probe_at moved from %s to %s inside one window", before.NextProbeAt, after.NextProbeAt)
	}
	if after.ConsecutiveLimitObservations != before.ConsecutiveLimitObservations+1 {
		t.Fatalf("the observation itself must still be counted: %d", after.ConsecutiveLimitObservations)
	}
}

func mustQuotaObservation(t *testing.T, class Classification) Observation {
	t.Helper()
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260802-0000d3"})
	if !ok {
		t.Fatal("expected a provider-quota observation")
	}
	return obs
}
