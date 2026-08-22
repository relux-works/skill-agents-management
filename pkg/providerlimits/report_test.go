package providerlimits

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// One projection feeds both the agent-facing query operation and the operator CLI,
// so the two surfaces cannot disagree.
func TestReportCarriesTheWholeProjection(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, granted := f.claim(t, codexPlan, "RUN-260802-0000c0")
	if !granted {
		t.Fatal("expected the claim")
	}
	report := f.store.ReportWithGroupTable([]Identity{f.identity}, MustGroupTable())
	if report.Version != SchemaVersion {
		t.Errorf("version = %d", report.Version)
	}
	if len(report.Ladder) != len(f.store.Ladder()) {
		t.Errorf("ladder = %v", report.Ladder)
	}
	if report.Bounds.ProbeLeaseMinutes != f.store.ProbeLeaseMinutes() {
		t.Errorf("bounds = %+v", report.Bounds)
	}
	if len(report.Groups) == 0 {
		t.Error("the group table must be reportable so an operator can see the membership behind a suppression")
	}
	if len(report.Identities) != 1 {
		t.Fatalf("identities = %d", len(report.Identities))
	}
	entry := report.Identities[0]
	if entry.Identity != f.identity.Key || entry.Provider != ProviderCodex || entry.HomeDisplay == "" {
		t.Fatalf("identity projection = %+v", entry)
	}
	if len(entry.Groups) != 1 {
		t.Fatalf("groups = %+v", entry.Groups)
	}
	group := entry.Groups[0]
	if group.Group != codexPlan || group.State != StateProbing {
		t.Errorf("group projection = %+v", group)
	}
	if group.Lease == nil || group.Lease.RunID != lease.RunID {
		t.Errorf("the lease must be visible so a stuck probe is visible rather than mysterious: %+v", group.Lease)
	}
	if group.Lease.ClaimDeadline.IsZero() {
		t.Error("the claim deadline must be visible")
	}
	if group.Evidence == nil || group.Evidence.RunID == "" {
		t.Errorf("evidence = %+v", group.Evidence)
	}
	if group.ProviderResetHint == nil || group.ProviderResetHint.Raw == "" {
		t.Errorf("the reset hint must be reported for diagnosis: %+v", group.ProviderResetHint)
	}
	if group.BackoffStepDuration == "" {
		t.Error("the effective step duration must be reported")
	}

	// The projection round-trips as JSON, because both surfaces emit it.
	//
	// "armed_token" is absent from this list and from the shape. The source
	// reports it; the fault injector behind it is not ported here, and a field
	// that could only ever be null is a surface promising nothing.
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshalling the report: %v", err)
	}
	for _, want := range []string{`"probe_lease"`, `"effective_state"`, `"provider_reset_hint"`, `"home_display"`} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("report JSON lacks %s:\n%s", want, payload)
		}
	}
}

// Reading the projection never takes a claim and never mutates state. A read
// surface that resolved a stale lease could hand out or withdraw a probe as a side
// effect of being looked at.
func TestReportNeverMutatesStateOrTakesAClaim(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)
	lease, _ := f.claim(t, codexPlan, "RUN-260802-0000c1")

	path := f.store.Layout().StateFile(f.identity.Key)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Push the clock past every deadline the lease has, which is exactly the
	// situation in which a resolving reader would write.
	f.clock.Set(lease.ClaimDeadline.Add(time.Hour))
	report := f.store.Report([]Identity{f.identity})
	_ = f.store.LoadIdentityState(f.identity)
	_, _ = f.store.GroupRecordFor(f.identity, codexPlan)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("a read mutated the state file:\n before %s\n after  %s", before, after)
	}
	group := report.Identities[0].Groups[0]
	if !group.LeaseExpired {
		t.Error("an expired lease must be reported as such rather than silently resolved")
	}
	if group.State != StateProbing {
		t.Errorf("state = %q; the read must report what is stored", group.State)
	}
}

func TestReportDerivesProbeEligibleWithoutWriting(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)

	report := f.store.Report([]Identity{f.identity})
	group := report.Identities[0].Groups[0]
	if group.EffectiveState != StateSuppressed || group.Claimable {
		t.Fatalf("before the step elapses: %+v", group)
	}

	f.clock.Advance(2 * time.Minute)
	report = f.store.Report([]Identity{f.identity})
	group = report.Identities[0].Groups[0]
	if group.State != StateSuppressed {
		t.Errorf("the stored state must still read suppressed until somebody writes: %q", group.State)
	}
	if group.EffectiveState != StateProbeEligible {
		t.Errorf("effective state = %q, want probe_eligible", group.EffectiveState)
	}
	if !group.Claimable {
		t.Error("an elapsed step is claimable by exactly one caller")
	}
	if group.EffectiveState == StateAvailable {
		t.Fatal("probe_eligible must never be reported as available")
	}
}

// An aged-out group reads as a claim gate at step 0, and says so. Without
// aged_out_at an operator sees a group that is suddenly probe_eligible at step 0
// with no evidence and nothing that says why — indistinguishable from a clear
// nobody ran.
func TestReportExplainsAnAgedOutGroup(t *testing.T) {
	f := newStoreFixture(t)
	f.escalateToMaxStep(t, codexPlan)
	f.clock.Advance(StaleRecordTTL + time.Hour)
	// Any write runs the sweep; this one is on a different group.
	if err := f.store.ClearGroup(f.identity, "codex-spark", "test"); err != nil {
		t.Fatalf("ClearGroup: %v", err)
	}

	report := f.store.Report([]Identity{f.identity})
	var group *GroupReport
	for i := range report.Identities[0].Groups {
		if report.Identities[0].Groups[i].Group == codexPlan {
			group = &report.Identities[0].Groups[i]
		}
	}
	if group == nil {
		t.Fatalf("the aged-out group must still be projected: %+v", report.Identities[0].Groups)
	}
	if group.AgedOutAt == nil {
		t.Errorf("the projection must explain why the step is 0: %+v", group)
	}
	if group.BackoffStep != 0 || group.EffectiveState != StateProbeEligible {
		t.Errorf("aged-out projection = %+v, want probe_eligible at step 0", group)
	}
	if !group.Claimable {
		t.Error("an aged-out group is claimable by exactly one caller")
	}
	if group.EffectiveState == StateAvailable {
		t.Fatal("ageing out must never project as available")
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshalling the report: %v", err)
	}
	if !strings.Contains(string(payload), `"aged_out_at"`) {
		t.Errorf("report JSON lacks aged_out_at:\n%s", payload)
	}
}

func TestReportOfAMachineWithNoState(t *testing.T) {
	f := newStoreFixture(t)
	report := f.store.Report(nil)
	if len(report.Identities) != 0 {
		t.Error("no identity was requested")
	}
	report = f.store.Report([]Identity{f.identity})
	if len(report.Identities) != 1 || len(report.Identities[0].Groups) != 0 {
		t.Fatalf("an identity with no state must project as an empty group list: %+v", report.Identities)
	}
}

func TestReportMarksASchemaAheadIdentity(t *testing.T) {
	f := newStoreFixture(t)
	path := f.store.Layout().StateFile(f.identity.Key)
	if err := os.MkdirAll(f.store.Layout().Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"groups":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := f.store.Report([]Identity{f.identity})
	if !report.Identities[0].SchemaAhead {
		t.Fatal("an operator must be told the file is newer than this binary understands")
	}
}
