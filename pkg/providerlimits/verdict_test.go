package providerlimits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// The verdict seam, and mostly its NEGATIVES.
//
// AvailabilityFor is a gate in the sense the evidence contract means: callers
// act on Serviceable(), so the failure that matters is not "it refused
// something it should have admitted" but "it admitted something it must
// reject". Every arm that could produce a wrong HEALTHY is attacked here, and
// each of those tests fails if the mapping widens.

// verdictFixture is a store over a scratch layout with a driven clock, plus the
// identity whose file the tests plant.
type verdictFixture struct {
	store    *Store
	layout   Layout
	identity Identity
	home     string
	clock    *testClock
	warnings *warnSink
}

func newVerdictFixture(t *testing.T, provider string) *verdictFixture {
	t.Helper()
	root := t.TempDir()
	layout := LayoutAt(root)
	clock := newTestClock(time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	warnings := &warnSink{}
	store, err := NewStore(Options{Layout: layout, Now: clock.Now, Warn: warnings.Warn})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	home := filepath.Join(root, provider+"-home")
	identity, err := IdentityFor(provider, home)
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return &verdictFixture{store: store, layout: layout, identity: identity, home: home, clock: clock, warnings: warnings}
}

// plant writes raw bytes as this identity's state file.
func (f *verdictFixture) plant(t *testing.T, raw string) {
	t.Helper()
	if err := os.WriteFile(f.layout.StateFile(f.identity.Key), []byte(raw), 0o600); err != nil {
		t.Fatalf("planting state: %v", err)
	}
}

// plantRecord writes one group record through the file encoder the write path
// uses, so the planted bytes are bytes the production writer could have made.
func (f *verdictFixture) plantRecord(t *testing.T, group string, record GroupRecord) {
	t.Helper()
	file := identityStateFile{
		Version:     SchemaVersion,
		Identity:    f.identity.Key,
		Provider:    f.identity.Provider,
		HomeDisplay: f.identity.HomeDisplay,
		Groups:      map[string]*GroupRecord{group: &record},
	}
	if err := writeJSONAtomic(f.layout.StateFile(f.identity.Key), &file); err != nil {
		t.Fatalf("planting record: %v", err)
	}
}

func (f *verdictFixture) verdict(t *testing.T, model string) vendorplugin.Availability {
	t.Helper()
	verdict, err := f.store.AvailabilityFor(VerdictQuery{
		Runtime: f.identity.Provider, Model: model, Home: f.home,
	})
	if err != nil {
		t.Fatalf("AvailabilityFor: %v", err)
	}
	if err := verdict.Validate(); err != nil {
		t.Fatalf("the verdict contradicts its own evidence and CheckAvailability would refuse it: %v\n%s", err, verdictDetails(verdict))
	}
	return verdict
}

// suppressedRecord is a plain active suppression: step 1, window open.
func suppressedRecord(now time.Time) GroupRecord {
	since := now.Add(-time.Minute)
	next := now.Add(4 * time.Minute)
	observed := now.Add(-time.Minute)
	return GroupRecord{
		Provider:                     ProviderClaude,
		State:                        StateSuppressed,
		BackoffStep:                  1,
		SuppressedSince:              &since,
		NextProbeAt:                  &next,
		ConsecutiveLimitObservations: 2,
		LastObservationAt:            &observed,
		Evidence: &Evidence{
			RunID:   "RUN-260822-verdict",
			Model:   "claude-opus-5",
			Marker:  "api_error_status 429",
			Excerpt: "API Error: Request rejected (429) · You've reached your usage limit.",
		},
	}
}

// --- the fail-open decision, preserved -------------------------------------

// TestAnAbsentStateFileReadsHealthy is invariant 2, carried rather than
// re-decided: a machine that never recorded state for this identity has a
// PROVEN absence, and the plane is an optimisation that must not refuse a
// launch because its own bookkeeping is missing.
//
// The verdict is Healthy AND it names a source, because the contract refuses a
// health claim that read nothing. What satisfies the evidence discipline here
// is the state read itself: the file was looked for at a known path and found
// not to exist.
func TestAnAbsentStateFileReadsHealthy(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	if _, err := os.Stat(f.layout.StateFile(f.identity.Key)); !os.IsNotExist(err) {
		t.Fatalf("fixture assumption broken: a state file already exists (%v)", err)
	}
	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("an absent state file read as %s; invariant 2 says it reads healthy", verdict.State)
	}
	if !verdict.Serviceable() {
		t.Error("an absent state file is not serviceable")
	}
	if len(verdict.Checked) == 0 {
		t.Error("the healthy verdict names no source; a health claim that read nothing is a guess")
	}
	if !strings.Contains(strings.Join(verdict.Checked, " "), f.identity.Key) {
		t.Errorf("the verdict does not name the identity it read: %v", verdict.Checked)
	}
}

// TestARecordedAvailableGroupReadsHealthy is the other determinate healthy arm.
func TestARecordedAvailableGroupReadsHealthy(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	success := f.clock.Now().Add(-time.Hour)
	f.plantRecord(t, "claude-plan", GroupRecord{
		Provider: ProviderClaude, State: StateAvailable, LastSuccessAt: &success, LastObservationAt: &success,
	})
	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("an available group read as %s", verdict.State)
	}
}

// --- the corrupt-state fix, carried onto the verdict ------------------------

// TestAnUnreadableStateNeverReadsHealthy is THE negative of this task.
//
// The extraction source's commit "Stop reading an unreadable limit state as an
// empty one" exists because an unreadable file handed back an empty group map,
// and an empty group map renders as "nothing is suppressed" — a claim about the
// provider that a failed read never established. The verdict is where that
// mistake would reappear, because Healthy is exactly "nothing is suppressed".
//
// Every indeterminate read is driven, including the two that would be easiest
// to wave through: a file whose bytes parse but carry a loss tombstone, and one
// written by a newer build that is intact and simply unreadable HERE.
func TestAnUnreadableStateNeverReadsHealthy(t *testing.T) {
	newer := SchemaVersion + 1
	cases := []struct {
		name  string
		want  StateRead
		bytes string
	}{
		{
			name:  "corrupt",
			want:  StateReadCorrupt,
			bytes: `{"version":3,"groups":{"claude-plan":`,
		},
		{
			name:  "not an object at all",
			want:  StateReadCorrupt,
			bytes: `["this is an array, not a state file"]`,
		},
		{
			name:  "schema ahead",
			want:  StateReadSchemaAhead,
			bytes: `{"version":` + itoa(newer) + `,"identity":"x","provider":"claude","groups":{}}`,
		},
		{
			name:  "degraded by a loss tombstone",
			want:  StateReadDegraded,
			bytes: `{"version":3,"identity":"x","provider":"claude","groups":{},"degraded_since":"2026-08-22T11:59:00Z"}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newVerdictFixture(t, ProviderClaude)
			f.plant(t, testCase.bytes)
			if read := f.store.LoadIdentityState(f.identity).Read; read != testCase.want {
				t.Fatalf("fixture assumption broken: the state read is %q, this case needs %q", read, testCase.want)
			}
			verdict := f.verdict(t, "claude-opus-5")
			if verdict.State == vendorplugin.AvailabilityHealthy {
				t.Fatalf("a %s state file read as HEALTHY; a failed read was laundered into a proven absence", testCase.name)
			}
			if verdict.Serviceable() {
				t.Fatalf("a %s state file reads as serviceable", testCase.name)
			}
			if verdict.State != vendorplugin.AvailabilityUnknown {
				t.Errorf("a %s state file read as %s, want unknown", testCase.name, verdict.State)
			}
			if len(verdict.Failures) == 0 {
				t.Error("the verdict carries no read failure, so a caller cannot tell a failed read from a checked-and-empty one")
			}
			if len(verdict.Checked) == 0 {
				t.Error("the verdict names no checked source, so it is indistinguishable from a question nobody asked")
			}
		})
	}
}

// TestAnUnreadableFileIsUnreadableRatherThanAbsent covers the arm the other
// cases cannot reach through bytes: a file that EXISTS and cannot be opened.
//
// It is skipped for root, which can read a 0o000 file.
func TestAnUnreadableFileIsUnreadableRatherThanAbsent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a mode-0 file is still readable, so this arm cannot be reached")
	}
	f := newVerdictFixture(t, ProviderClaude)
	path := f.layout.StateFile(f.identity.Key)
	f.plantRecord(t, "claude-plan", suppressedRecord(f.clock.Now()))
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadUnreadable {
		t.Fatalf("fixture assumption broken: the state read is %q, want %q", read, StateReadUnreadable)
	}
	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State != vendorplugin.AvailabilityUnknown || verdict.Serviceable() {
		t.Fatalf("an unreadable state file produced %s (serviceable=%v)", verdict.State, verdict.Serviceable())
	}
}

// TestADegradedReadDoesNotTurnAMissingRecordIntoHealth is the subtler half of
// the same fix.
//
// A degraded file PARSES. Whatever groups it still carries were written after
// the loss and are proven — but the ABSENCE of a group is not, because the
// records that used to be there were destroyed. A mapping that answered the
// per-group question from "is there a record" would report Healthy for every
// group the lost file used to suppress.
func TestADegradedReadDoesNotTurnAMissingRecordIntoHealth(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	f.plant(t, `{"version":3,"identity":"x","provider":"claude","groups":{},"degraded_since":"2026-08-22T11:59:00Z"}`)
	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State == vendorplugin.AvailabilityHealthy {
		t.Fatal("a group with no record under a degraded read was reported healthy; the missing record is the shape of the hole the loss left, not a proven absence")
	}

	// Narrowing: past the horizon the tombstone is spent and the same file DOES
	// read healthy. Without this, a mapping that reported unknown forever would
	// pass the assertion above while bricking the identity.
	f.clock.Advance(LostStateHorizon + time.Minute)
	recovered := f.verdict(t, "claude-opus-5")
	if recovered.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("past the lost-state horizon the verdict is still %s; the loss must clear by the clock", recovered.State)
	}
}

// TestTheVerdictPathNeverWrites is the read-surface contract.
//
// A verdict that took a probe claim would hand out or withdraw a probe as a
// side effect of being looked at, and a verdict that quarantined would destroy
// the evidence a later operator needs. The whole state directory is compared
// before and after, over every arm including the corrupt one — the arm on which
// the WRITE path deliberately does quarantine.
func TestTheVerdictPathNeverWrites(t *testing.T) {
	for name, planted := range map[string]string{
		"a healthy record":   "",
		"a corrupt file":     `{"version":3,"groups":`,
		"a schema-ahead":     `{"version":99,"identity":"x","provider":"claude","groups":{}}`,
		"a live suppression": "record",
	} {
		t.Run(name, func(t *testing.T) {
			f := newVerdictFixture(t, ProviderClaude)
			switch planted {
			case "":
				f.plantRecord(t, "claude-plan", GroupRecord{Provider: ProviderClaude, State: StateAvailable})
			case "record":
				f.plantRecord(t, "claude-plan", suppressedRecord(f.clock.Now()))
			default:
				f.plant(t, planted)
			}
			before := snapshotDir(t, f.layout.Root)
			if _, err := f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Model: "claude-opus-5", Home: f.home}); err != nil {
				t.Fatalf("AvailabilityFor: %v", err)
			}
			if _, err := f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Home: f.home}); err != nil {
				t.Fatalf("AvailabilityFor (identity-wide): %v", err)
			}
			after := snapshotDir(t, f.layout.Root)
			if before != after {
				t.Errorf("the verdict path changed the state directory.\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// --- the limited-until verdict ----------------------------------------------

// TestASuppressionReadsAsLimitedUntilWithItsWindowAndEvidence is the positive
// that the negatives are worth having around.
func TestASuppressionReadsAsLimitedUntilWithItsWindowAndEvidence(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	record := suppressedRecord(f.clock.Now())
	f.plantRecord(t, "claude-plan", record)

	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State != vendorplugin.AvailabilityLimited {
		t.Fatalf("an active suppression read as %s", verdict.State)
	}
	if !verdict.Until.Equal(*record.NextProbeAt) {
		t.Errorf("Until = %s, want the record's next_probe_at %s", verdict.Until, *record.NextProbeAt)
	}
	if verdict.Serviceable() {
		t.Error("a suppressed group reads as serviceable")
	}
	assertVerdictCarriesEvidenceOf(t, verdict, record)

	// The ladder step reaches the verdict, so an operator reading it can see
	// WHY the window is the length it is.
	if !strings.Contains(verdictDetails(verdict), f.store.ladderStep(record.BackoffStep).String()) {
		t.Errorf("the verdict does not name the backoff step duration:\n%s", verdictDetails(verdict))
	}
}

// TestASuppressionWithNoWindowIsUnknownNotAFabricatedLimit is the narrowing on
// the limited arm.
//
// A caller is entitled to schedule a retry off Until. A suppression with no
// next_probe_at cannot answer "until when", and inventing one — now, or now
// plus the step — would hand the caller a time nothing established. The
// contract refuses it from the other side too, so this also pins that the
// mapping does not try.
func TestASuppressionWithNoWindowIsUnknownNotAFabricatedLimit(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	record := suppressedRecord(f.clock.Now())
	record.NextProbeAt = nil
	f.plantRecord(t, "claude-plan", record)

	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State == vendorplugin.AvailabilityLimited {
		t.Fatalf("a suppression with no window produced a limited verdict clearing at %s, which nothing established", verdict.Until)
	}
	if verdict.State != vendorplugin.AvailabilityUnknown {
		t.Errorf("verdict = %s, want unknown", verdict.State)
	}
	if !verdict.Until.IsZero() {
		t.Errorf("a non-limited verdict carries a clear time of %s", verdict.Until)
	}
	if verdict.Serviceable() {
		t.Error("a suppressed group with no window reads as serviceable")
	}
}

// TestProviderResetProseNeverBecomesTheClearTime is the rule the source states
// in ResetHintNote: a provider's own prose is diagnostic, it may SHORTEN a
// backoff step through min() and can never extend or suppress. If it reached
// Until, vendor prose would be setting the retry schedule.
func TestProviderResetProseNeverBecomesTheClearTime(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	record := suppressedRecord(f.clock.Now())
	hinted := f.clock.Now().Add(72 * time.Hour)
	record.ProviderResetHint = &ResetHint{Raw: "your limit resets in three days", Parsed: &hinted, Note: ResetHintNote}
	f.plantRecord(t, "claude-plan", record)

	verdict := f.verdict(t, "claude-opus-5")
	if verdict.Until.Equal(hinted) {
		t.Fatal("the parsed reset hint became the clear time; provider prose can never extend a suppression")
	}
	if !verdict.Until.Equal(*record.NextProbeAt) {
		t.Errorf("Until = %s, want the armed window %s", verdict.Until, *record.NextProbeAt)
	}
	details := verdictDetails(verdict)
	if !strings.Contains(details, "your limit resets in three days") {
		t.Errorf("the hint is not reported as an observation at all, so an operator loses it:\n%s", details)
	}
	if !strings.Contains(details, ResetHintNote) {
		t.Errorf("the hint is reported without the note that says it is diagnostic:\n%s", details)
	}
}

// --- probe-gated groups ------------------------------------------------------

// TestAProbeEligibleGroupIsNotHealthy is the collapse the source warns about:
// probe_eligible is "admitted to a claim holder only, and stays subtracted for
// every other caller", and treating it as available "is what lets every
// concurrent preflight admit an exhausted group at once".
func TestAProbeEligibleGroupIsNotHealthy(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	record := suppressedRecord(f.clock.Now())
	elapsed := f.clock.Now().Add(-time.Minute)
	record.NextProbeAt = &elapsed
	f.plantRecord(t, "claude-plan", record)

	if got := record.EffectiveState(f.clock.Now()); got != StateProbeEligible {
		t.Fatalf("fixture assumption broken: the record reads %q, want %q", got, StateProbeEligible)
	}
	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State == vendorplugin.AvailabilityHealthy || verdict.Serviceable() {
		t.Fatalf("a probe-eligible group read as %s (serviceable=%v); it is admitted to exactly one caller, and only by winning a claim", verdict.State, verdict.Serviceable())
	}
	if verdict.State == vendorplugin.AvailabilityLimited {
		t.Fatalf("a probe-eligible group produced a limited verdict clearing at %s, which is in the past — a caller would retry immediately and skip the claim", verdict.Until)
	}
	if !strings.Contains(verdictDetails(verdict), "claim") {
		t.Errorf("the verdict does not say a claim is what resolves this:\n%s", verdictDetails(verdict))
	}
}

// TestAGroupSomebodyElseIsProbingIsNotHealthy covers the fourth group state.
func TestAGroupSomebodyElseIsProbingIsNotHealthy(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	record := suppressedRecord(f.clock.Now())
	record.State = StateProbing
	claimed := f.clock.Now().Add(-time.Minute)
	record.ProbeLease = &Lease{
		Group: "claude-plan", Identity: f.identity.Key, RunID: "RUN-260822-other",
		OwnerKind: OwnerRun, ClaimedAt: claimed, ExpiresAt: f.clock.Now().Add(9 * time.Minute),
	}
	f.plantRecord(t, "claude-plan", record)

	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State == vendorplugin.AvailabilityHealthy || verdict.Serviceable() {
		t.Fatalf("a group another run holds the probe on read as %s (serviceable=%v)", verdict.State, verdict.Serviceable())
	}
	if !strings.Contains(verdictDetails(verdict), "RUN-260822-other") {
		t.Errorf("the verdict does not name the run holding the lease:\n%s", verdictDetails(verdict))
	}
}

// --- the unclassifiable carve-out -------------------------------------------

// TestARuntimeWithNoClassifierReadsHealthyEvenWhenTheStateIsCorrupt carries the
// source's own fail-open carve-out: a runtime whose broker has no classifier
// can never be suppressed, so no state file ever gated it, so a lost file lost
// nothing about it.
//
// It is checked against a CORRUPT file on purpose. That is the arm where the
// carve-out is load-bearing rather than incidental: without it the corrupt-read
// rule above would downgrade a runtime the file could never have said anything
// about.
func TestARuntimeWithNoClassifierReadsHealthyEvenWhenTheStateIsCorrupt(t *testing.T) {
	for _, runtime := range []string{"gemini", "agy", "muse", "qwen", "qwen-codex"} {
		t.Run(runtime, func(t *testing.T) {
			if HasClassifierForRuntime(runtime) {
				t.Skipf("%s has a classifier; this case is about the runtimes that do not", runtime)
			}
			f := newVerdictFixture(t, ProviderClaude)
			f.plant(t, `{"version":3,"groups":`)
			verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: runtime, Model: "whatever-model"})
			if err != nil {
				t.Fatalf("AvailabilityFor(%s): %v", runtime, err)
			}
			if err := verdict.Validate(); err != nil {
				t.Fatalf("the verdict contradicts its own evidence: %v", err)
			}
			if verdict.State != vendorplugin.AvailabilityHealthy {
				t.Fatalf("runtime %s read as %s; nothing can suppress a broker with no classifier", runtime, verdict.State)
			}
			if len(verdict.Checked) == 0 || verdict.Checked[0] != SourceFrozenTable {
				t.Errorf("the verdict does not name the frozen table as what it read: %v", verdict.Checked)
			}
		})
	}
}

// TestTheCarveOutDoesNotCoverAClassifiedRuntime is the narrowing on the
// carve-out. Without it, a carve-out that fired for every runtime would pass
// every assertion above and disable the whole plane.
func TestTheCarveOutDoesNotCoverAClassifiedRuntime(t *testing.T) {
	for _, runtime := range []string{ProviderClaude, ProviderCodex} {
		if !HasClassifierForRuntime(runtime) {
			t.Fatalf("fixture assumption broken: %s must have a classifier", runtime)
		}
		f := newVerdictFixture(t, runtime)
		f.plant(t, `{"version":3,"groups":`)
		verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: runtime, Model: "claude-opus-5", Home: f.home})
		if err != nil {
			t.Fatalf("AvailabilityFor: %v", err)
		}
		if verdict.State == vendorplugin.AvailabilityHealthy {
			t.Fatalf("runtime %s took the unclassifiable carve-out on a corrupt file; the plane is disabled for a runtime it classifies", runtime)
		}
	}
}

// --- the runtime-wide question ----------------------------------------------

// TestOneExhaustedGroupDoesNotMakeTheWholeRuntimeUnavailable pins the
// runtime-wide reading: claude has two groups, and one of them being exhausted
// leaves the other serving.
func TestOneExhaustedGroupDoesNotMakeTheWholeRuntimeUnavailable(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	f.plantRecord(t, "claude-plan", suppressedRecord(f.clock.Now()))

	verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Home: f.home})
	if err != nil {
		t.Fatalf("AvailabilityFor: %v", err)
	}
	if err := verdict.Validate(); err != nil {
		t.Fatalf("the verdict contradicts its own evidence: %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("one exhausted group of two made the whole runtime read %s; the other group can still serve", verdict.State)
	}
	if !strings.Contains(verdictDetails(verdict), "claude-plan") {
		t.Errorf("the runtime-wide verdict does not name the subtracted group, so a caller cannot see what it lost:\n%s", verdictDetails(verdict))
	}

	// And the per-model question about the exhausted group is still Limited.
	// Without this the runtime-wide healthy answer would be indistinguishable
	// from a mapping that lost the suppression entirely.
	perModel := f.verdict(t, "claude-opus-5")
	if perModel.State != vendorplugin.AvailabilityLimited {
		t.Fatalf("the per-model question about the exhausted group read %s", perModel.State)
	}
}

// TestEveryGroupExhaustedReadsLimitedAtTheEarliestClearTime is the arm where
// the runtime as a whole really cannot serve.
func TestEveryGroupExhaustedReadsLimitedAtTheEarliestClearTime(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	now := f.clock.Now()
	late := suppressedRecord(now)
	lateAt := now.Add(30 * time.Minute)
	late.NextProbeAt = &lateAt
	early := suppressedRecord(now)
	earlyAt := now.Add(3 * time.Minute)
	early.NextProbeAt = &earlyAt

	file := identityStateFile{
		Version: SchemaVersion, Identity: f.identity.Key, Provider: ProviderClaude,
		HomeDisplay: f.identity.HomeDisplay,
		Groups:      map[string]*GroupRecord{"claude-plan": &late, "claude-usage-credits": &early},
	}
	if err := writeJSONAtomic(f.layout.StateFile(f.identity.Key), &file); err != nil {
		t.Fatalf("planting: %v", err)
	}

	verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Home: f.home})
	if err != nil {
		t.Fatalf("AvailabilityFor: %v", err)
	}
	if err := verdict.Validate(); err != nil {
		t.Fatalf("the verdict contradicts its own evidence: %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityLimited {
		t.Fatalf("every group of the runtime is exhausted and the verdict is %s", verdict.State)
	}
	if !verdict.Until.Equal(earlyAt) {
		t.Errorf("Until = %s, want the EARLIEST clear time %s; a caller sleeping to the later one skips the group that recovered first", verdict.Until, earlyAt)
	}
}

// --- validity across every arm ----------------------------------------------

// TestEveryVerdictThisSeamCanProduceValidates drives every branch and requires
// each answer to satisfy the vendor contract.
//
// A verdict that fails Validate is REFUSED by CheckAvailability, so a mapping
// that produced one would silently turn into "this vendor cannot answer at
// all" — a failure mode no per-arm assertion above would catch, because each of
// them checks the state and not the shape.
func TestEveryVerdictThisSeamCanProduceValidates(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	elapsed := now.Add(-time.Minute)
	probing := suppressedRecord(now)
	probing.State = StateProbing
	probing.ProbeLease = &Lease{RunID: "RUN-x", OwnerKind: OwnerRun, ClaimedAt: elapsed, ExpiresAt: now.Add(time.Minute)}
	eligible := suppressedRecord(now)
	eligible.NextProbeAt = &elapsed
	windowless := suppressedRecord(now)
	windowless.NextProbeAt = nil
	bare := suppressedRecord(now)
	bare.Evidence = nil
	bare.LastObservationAt = nil
	bare.SuppressedSince = nil
	empty := suppressedRecord(now)
	empty.Evidence = &Evidence{}

	arms := map[string]func(*verdictFixture){
		"absent": func(*verdictFixture) {},
		"available record": func(f *verdictFixture) {
			f.plantRecord(t, "claude-plan", GroupRecord{Provider: ProviderClaude, State: StateAvailable})
		},
		"suppressed":           func(f *verdictFixture) { f.plantRecord(t, "claude-plan", suppressedRecord(now)) },
		"suppressed bare":      func(f *verdictFixture) { f.plantRecord(t, "claude-plan", bare) },
		"suppressed no fields": func(f *verdictFixture) { f.plantRecord(t, "claude-plan", empty) },
		"windowless":           func(f *verdictFixture) { f.plantRecord(t, "claude-plan", windowless) },
		"probe eligible":       func(f *verdictFixture) { f.plantRecord(t, "claude-plan", eligible) },
		"probing":              func(f *verdictFixture) { f.plantRecord(t, "claude-plan", probing) },
		"corrupt":              func(f *verdictFixture) { f.plant(t, `{"version":3,`) },
		"schema ahead":         func(f *verdictFixture) { f.plant(t, `{"version":99,"groups":{}}`) },
		"degraded": func(f *verdictFixture) {
			f.plant(t, `{"version":3,"groups":{},"degraded_since":"2026-08-22T11:59:00Z"}`)
		},
		"empty groups": func(f *verdictFixture) { f.plant(t, `{"version":3,"groups":{}}`) },
	}
	for name, plant := range arms {
		for _, model := range []string{"claude-opus-5", ""} {
			label := name + "/model=" + model
			if model == "" {
				label = name + "/runtime-wide"
			}
			t.Run(label, func(t *testing.T) {
				f := newVerdictFixture(t, ProviderClaude)
				plant(f)
				verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Model: model, Home: f.home})
				if err != nil {
					t.Fatalf("AvailabilityFor: %v", err)
				}
				if err := verdict.Validate(); err != nil {
					t.Fatalf("the verdict does not satisfy the vendor contract, so CheckAvailability would refuse it: %v\n%s", err, verdictDetails(verdict))
				}
			})
		}
	}
}

// TestAVerdictIsRefusedRatherThanGuessedWhenTheQueryCannotResolve pins the
// error return: an unanswerable QUESTION is not a verdict of unknown.
//
// One error arm is reachable from a query alone — a blank runtime names
// nothing. The other two are reachable only through registries a caller
// supplies: a declared runtime whose agentic system plugin is not compiled in,
// and a runtime whose harness declares no home rule at all. Both are driven
// through DefaultProviderHomeIn in identity_test.go rather than faked here,
// because AvailabilityFor answers an unclassifiable runtime before it ever
// resolves a home, and every runtime this plane DOES classify has a home rule.
func TestAVerdictIsRefusedRatherThanGuessedWhenTheQueryCannotResolve(t *testing.T) {
	f := newVerdictFixture(t, ProviderClaude)
	for _, runtime := range []string{"", "   ", "\t"} {
		if _, err := f.store.AvailabilityFor(VerdictQuery{Runtime: runtime}); err == nil {
			t.Errorf("AvailabilityFor with runtime %q produced a verdict instead of an error", runtime)
		}
	}
	// Narrowing: a runtime that DOES resolve must not take the same path.
	if _, err := f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Home: f.home}); err != nil {
		t.Fatalf("a resolvable query was refused: %v", err)
	}
}

// --- driven through the real vendor entry point ------------------------------

// limitPlaneVendor is a vendor plugin whose Availability is the ported plane.
//
// It exists so the verdict is proven through the PRODUCTION call site —
// vendorplugin.CheckAvailability — rather than only through the plane's own
// function. CheckAvailability is what a caller deciding whether to queue
// actually calls, and it applies Availability.Validate to whatever a plugin
// answers, so a verdict this seam produces has to survive that gate to be worth
// anything.
type limitPlaneVendor struct {
	store *Store
	home  string
}

func (v *limitPlaneVendor) ID() vendorplugin.VendorID { return "anthropic" }

func (v *limitPlaneVendor) Models() []vendorplugin.Model {
	return []vendorplugin.Model{{
		ID:          "claude-opus-5",
		Description: "the model the limit-plane seam is exercised through in tests",
		Rank: vendorplugin.CapabilityRank{
			Position: 1,
			Basis: []vendorplugin.RankEvidence{{
				Source:      "pkg/providerlimits/verdict_test.go",
				Observation: "the only row this double declares, so its position carries no claim beyond being first",
			}},
		},
		Effort: vendorplugin.EffortDeclaration{
			Support:     agentic.EffortSupportRequired,
			Vocabulary:  []string{"low", "medium", "high"},
			Recommended: "high",
		},
		Systems: []agentic.SystemID{"claude-code"},
	}}
}

// Availability is the seam: the vendor asks the plane and returns what it says.
func (v *limitPlaneVendor) Availability(q vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	home := q.Home
	if home == "" {
		home = v.home
	}
	return v.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Model: string(q.Model), Home: home})
}

// Spawn is a passthrough. The seam under test is Availability; a vendor that
// shaped a launch here would be adding behaviour this task has no evidence for.
func (v *limitPlaneVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return agentic.LaunchRequest{
		System:      sc.Runtime.SystemID,
		Model:       sc.Model.Launchable(),
		Effort:      sc.Effort,
		PromptPath:  sc.Request.PromptPath,
		Prompt:      sc.Request.Prompt,
		WorkDir:     sc.Request.WorkDir,
		Home:        sc.Request.Home,
		Env:         sc.Request.Env,
		Goal:        sc.Request.Goal,
		Budget:      sc.Request.Budget,
		ServiceTier: sc.Request.ServiceTier,
		Composition: sc.Request.Composition,
	}, nil
}

// TestTheVerdictSurvivesTheVendorContractsOwnGate drives the plane through
// vendorplugin.CheckAvailability on both a healthy and a corrupt state.
func TestTheVerdictSurvivesTheVendorContractsOwnGate(t *testing.T) {
	for name, corrupt := range map[string]bool{"healthy": false, "corrupt": true} {
		t.Run(name, func(t *testing.T) {
			f := newVerdictFixture(t, ProviderClaude)
			if corrupt {
				f.plant(t, `{"version":3,"groups":`)
			}
			registry := vendorplugin.NewRegistry(agentic.Default)
			if err := registry.Register(&limitPlaneVendor{store: f.store, home: f.home}); err != nil {
				t.Fatalf("registering the limit-plane vendor: %v", err)
			}
			verdict, err := vendorplugin.CheckAvailability(registry, "anthropic", vendorplugin.AvailabilityQuery{
				Model: "claude-opus-5", Home: f.home,
			})
			if err != nil {
				t.Fatalf("CheckAvailability refused the plane's verdict: %v", err)
			}
			if corrupt {
				if verdict.Serviceable() {
					t.Fatal("a corrupt state file reached a caller as serviceable through the real vendor entry point")
				}
			} else if !verdict.Serviceable() {
				t.Fatal("an absent state file reached a caller as not serviceable through the real vendor entry point")
			}
		})
	}
}

// --- helpers -----------------------------------------------------------------

// snapshotDir renders a directory tree as name+size+content-hash lines, so a
// before/after comparison catches a write, a rename and a deletion alike.
func snapshotDir(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			lines = append(lines, path+" <unreadable: "+readErr.Error()+">")
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		lines = append(lines, rel+" "+itoa(len(data))+" "+string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func itoa(value int) string {
	data, _ := json.Marshal(value)
	return string(data)
}
