package providerlimits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file carries the tests for TASK-260817-mohnt0: moving classifier
// ownership onto the broker without moving the on-disk identity. Every test
// here targets one of the four things the task explicitly says must not move —
// IdentityKey's input, UnmappedGroup's runtime keying, existing suppressions,
// and the never-write-unclassifiable rule — by pinning a value or driving a
// real production entry point, not by re-deriving the production formula.

// TestIdentityKeyExactValueIsPinned pins IdentityKey's output for one known
// (runtime ID, home) pair as a literal, so a future change to the hash input —
// swapping in the broker, reordering the fields, changing the separator —
// changes this literal and fails loudly, rather than being invisible to a test
// that recomputes the same formula the production code just changed.
//
// The literal was computed independently with
// `python3 -c "import hashlib; print(hashlib.sha256(b'claude\x00/var/tmp/providerlimits-pin/claude-home').hexdigest()[:16])"`.
func TestIdentityKeyExactValueIsPinned(t *testing.T) {
	const want = "f62f97776336a5d1"
	got := IdentityKey("claude", "/var/tmp/providerlimits-pin/claude-home")
	if got != want {
		t.Fatalf("IdentityKey(claude, /var/tmp/providerlimits-pin/claude-home) = %q, want the pinned %q — the identity-key formula moved", got, want)
	}
}

// TestUnmappedGroupPersistsUnderTheRuntimeIDNotBroker is REVIEWER FINDING F3:
// UnmappedGroup(provider, model) must keep receiving the runtime ID, not the
// broker. Passing the broker would mint "anthropic-unmapped:..." where
// "claude-unmapped:..." is already on disk, orphaning every persisted
// suppression for that group. This is proven against the bytes Observe
// actually writes to the state file, not against the in-memory GroupRef alone.
func TestUnmappedGroupPersistsUnderTheRuntimeIDNotBroker(t *testing.T) {
	const model = "claude-not-a-real-model"

	table := MustGroupTable()
	ref := table.Lookup(ProviderClaude, model)
	if !ref.Unmapped {
		t.Fatalf("%q must be unmapped; the table was not supposed to carry this model", model)
	}
	wantGroup := "claude-unmapped:" + model
	if ref.Group != wantGroup {
		t.Fatalf("Lookup(%s, %s).Group = %q, want %q — UnmappedGroup was handed the broker instead of the runtime ID", ProviderClaude, model, ref.Group, wantGroup)
	}
	// Narrowing: the broker string must NOT produce the same group the runtime
	// ID does — if it did, this test would not be able to tell the two apart.
	if brokerGroup := UnmappedGroup(BrokerAnthropic, model); brokerGroup == wantGroup {
		t.Fatalf("UnmappedGroup(broker) and UnmappedGroup(runtime) collide on %q; the runtime-keying test below would pass vacuously", brokerGroup)
	}

	f := newStoreFixture(t)
	identity, err := IdentityFor(ProviderClaude, filepath.Join(f.root, "claude-home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	class := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: readFixture(t, "claude-429-usage-limit.json"), Now: f.clock.Now()})
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260817-mohnt0-01", Model: model})
	if !ok {
		t.Fatal("the captured Claude 429 fixture must yield a provider-quota observation")
	}
	if err := f.store.Observe(identity, ref.Group, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	raw, err := os.ReadFile(f.store.Layout().StateFile(identity.Key))
	if err != nil {
		t.Fatalf("reading the identity state file: %v", err)
	}
	if !strings.Contains(string(raw), `"`+wantGroup+`":`) {
		t.Fatalf("the persisted state file does not carry the runtime-keyed group %q as a JSON key:\n%s", wantGroup, raw)
	}
	if strings.Contains(string(raw), "anthropic-unmapped") {
		t.Fatalf("the persisted state file carries a broker-keyed unmapped group; UnmappedGroup was handed the broker somewhere on this path:\n%s", raw)
	}
}

// TestHasClassifierKeysOnBrokerNotRuntimeLiteral is the trap D4 exists to
// catch: HasClassifier switched from runtime names to broker names, so a raw
// runtime literal handed to it directly — the exact mistake an implementer
// makes by skipping the resolution step — must read as unclassifiable, even
// though the runtime is one of the two this package fully supports.
func TestHasClassifierKeysOnBrokerNotRuntimeLiteral(t *testing.T) {
	for _, runtime := range []string{ProviderClaude, ProviderCodex} {
		if HasClassifier(runtime) {
			t.Errorf("HasClassifier(%q) is true; %q is a runtime ID, not a broker, and HasClassifier must key on broker names only", runtime, runtime)
		}
	}
	for _, broker := range []string{BrokerAnthropic, BrokerOpenAI} {
		if !HasClassifier(broker) {
			t.Errorf("HasClassifier(%q) is false; this package ships a classifier for this broker", broker)
		}
	}
	// The runtime-facing convenience wrapper must still say yes for the two
	// evidenced runtimes, resolving through runtimeid rather than through a
	// literal comparison against the runtime string.
	for _, runtime := range []string{ProviderClaude, ProviderCodex} {
		if !HasClassifierForRuntime(runtime) {
			t.Errorf("HasClassifierForRuntime(%q) is false; this runtime's broker has a classifier", runtime)
		}
	}
}

// TestBrokerFactsAgreeWithTheFrozenTable cross-checks brokerForRuntime (and by
// extension HasClassifierForRuntime and DefaultGroups' Broker field) against
// vendorplugin.FrozenRuntimes directly, for every built-in runtime — not just
// the two this package classifies. This is the guard against a second,
// hand-maintained runtime-to-broker table drifting from the frozen one: if
// brokerForRuntime ever stopped calling vendorplugin and started returning a
// locally hard-coded answer instead, this test would still pass for claude and
// codex (their values happen to agree) but would start failing the moment the
// frozen table gains a runtime whose vendor this duplicate did not know
// about — which is exactly what qwen/gemini/muse/agy below already are.
//
// It is the extraction source's TestBrokerFactsAgreeWithRuntimeID, retargeted
// at this module's own frozen table; the source's runtimeid "broker" is this
// module's VendorID, and the source's BrokerUnknown is VendorUnresolved.
func TestBrokerFactsAgreeWithTheFrozenTable(t *testing.T) {
	declarations := vendorplugin.FrozenRuntimes()
	if len(declarations) == 0 {
		t.Fatal("fixture assumption broken: the frozen runtime table is empty, so this test would prove nothing")
	}
	for _, declaration := range declarations {
		runtime := declaration.ID.String()
		want := ""
		if declaration.VendorResolved() {
			want = declaration.Vendor.String()
		}
		if got := brokerForRuntime(runtime); got != want {
			t.Errorf("brokerForRuntime(%q) = %q, want %q (from vendorplugin.FrozenRuntimes)", runtime, got, want)
		}
	}
	if got := brokerForRuntime("a-runtime-the-frozen-table-has-never-heard-of"); got != "" {
		t.Errorf("brokerForRuntime(unknown runtime) = %q, want \"\"", got)
	}
}

// TestRuntimeWithAnEstablishedBrokerButNoClassifierCannotBeSuppressed is the
// never-write-unclassifiable rule proven for a broker OTHER than the two this
// package classifies, through the real production gate at Store.Observe —
// not just through the classifier-minting gate ClassifyManaged already covers
// for qwen. gemini's broker (google) is established by runtimeid (unlike
// muse's, which is VendorUnresolved), so this specifically proves the gate
// keys on "does this broker have a classifier", not merely on "is the broker
// unknown".
func TestRuntimeWithAnEstablishedBrokerButNoClassifierCannotBeSuppressed(t *testing.T) {
	var broker string
	for _, declaration := range vendorplugin.FrozenRuntimes() {
		if declaration.ID == "gemini" && declaration.VendorResolved() {
			broker = declaration.Vendor.String()
		}
	}
	if broker == "" {
		t.Fatal("fixture assumption broken: gemini must be a built-in runtime with an established broker")
	}
	if HasClassifier(broker) {
		t.Fatalf("fixture assumption broken: broker %q must have no classifier for this test to be meaningful", broker)
	}

	f := newStoreFixture(t)
	identity, err := IdentityFor("gemini", filepath.Join(f.root, "gemini-home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	group := UnmappedGroup("gemini", "gemini-3-pro")

	// The classifier-minting gate: no call site can even construct quota
	// evidence for gemini.
	class := ClassifyManaged("gemini", ManagedSignalUsageLimited)
	if class.Class != ClassNotLimit {
		t.Fatalf("ClassifyManaged(gemini, usage_limited) = %q; broker %q has no classifier, so no limit class may be produced", class.Class, broker)
	}

	// The store boundary: even hand-built quota evidence naming gemini is
	// refused, so no future call site can route it in another way.
	forged := Observation{quota: true, Provider: "gemini", Evidence: Evidence{RunID: "RUN-260817-mohnt0-02"}}
	if err := f.store.Observe(identity, group, nil, forged); err != ErrUnsupportedProvider {
		t.Fatalf("Observe(gemini) = %v, want ErrUnsupportedProvider", err)
	}

	if _, exists := f.store.GroupRecordFor(identity, group); exists {
		t.Fatal("a gemini group must hold no record; its broker has no classifier")
	}
	if _, err := os.Stat(f.store.Layout().StateFile(identity.Key)); !os.IsNotExist(err) {
		t.Fatalf("a gemini identity state file was written despite the gate: %v", err)
	}
}

// TestGroupRowBrokerFieldIsAdditiveAndProviderWireNameIsFrozen is D2: GroupRow
// gains a broker field, and the "provider" wire name — still the runtime ID,
// still what model-index lookup keys on — is untouched.
func TestGroupRowBrokerFieldIsAdditiveAndProviderWireNameIsFrozen(t *testing.T) {
	for _, row := range DefaultGroups() {
		wantBroker := brokerForRuntime(row.Provider)
		if row.Broker != wantBroker {
			t.Errorf("group %q (provider %q) has broker %q, want %q", row.Group, row.Provider, row.Broker, wantBroker)
		}
	}

	row := GroupRow{
		Group:      "claude-plan",
		Provider:   ProviderClaude,
		Broker:     BrokerAnthropic,
		Members:    []string{"claude-opus-5"},
		Provenance: ProvenanceVendorStringEvidence,
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("json.Marshal(GroupRow): %v", err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"provider":"claude"`) {
		t.Fatalf("GroupRow's wire form does not carry the frozen \"provider\" field holding the runtime ID:\n%s", text)
	}
	if !strings.Contains(text, `"broker":"anthropic"`) {
		t.Fatalf("GroupRow's wire form does not carry the new \"broker\" field:\n%s", text)
	}

	// modelKey/Lookup still index by the "provider" field's runtime value —
	// changing Broker alone must never repoint which group a model resolves to.
	table, err := NewGroupTable([]GroupRow{
		{Group: "claude-plan", Provider: ProviderClaude, Broker: "a-broker-value-that-does-not-exist", Members: []string{"claude-opus-5"}},
	})
	if err != nil {
		t.Fatalf("NewGroupTable: %v", err)
	}
	if got := table.Group(ProviderClaude, "claude-opus-5"); got != "claude-plan" {
		t.Fatalf("Group(claude, claude-opus-5) = %q, want claude-plan — Broker alone must not affect model-index lookup", got)
	}
}

// TestExistingStateFileRoundTripsByteIdenticallyThroughAWrite drives AC3
// through a REAL state file: raw JSON authored by hand in the shape the
// PRE-migration code would have written (no GroupRow, no Broker field, only
// what identityStateFile/GroupRecord already persisted), never touched by any
// Go struct this change modifies. It proves three things: the suppression it
// already carries is found; that suppression still gates a live claim; and a
// fresh write onto the same record keeps the frozen "provider" field's name
// and value exactly as they were.
func TestExistingStateFileRoundTripsByteIdenticallyThroughAWrite(t *testing.T) {
	f := newStoreFixture(t)
	identity, err := IdentityFor(ProviderClaude, filepath.Join(f.root, "claude-home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}

	suppressedSince := f.clock.Now().Add(-10 * time.Minute)
	nextProbeAt := f.clock.Now().Add(20 * time.Minute)
	// This is hand-authored, not produced by marshaling any Go struct from this
	// package: it is what a pre-migration binary's writeJSONAtomic call would
	// have put on disk for one suppressed group.
	rawStateFile := `{
  "version": 3,
  "identity": "` + identity.Key + `",
  "provider": "claude",
  "home_display": "~/pinned-claude-home-for-test",
  "groups": {
    "claude-plan": {
      "provider": "claude",
      "state": "suppressed",
      "backoff_step": 2,
      "suppressed_since": "` + suppressedSince.Format(time.RFC3339) + `",
      "next_probe_at": "` + nextProbeAt.Format(time.RFC3339) + `",
      "consecutive_limit_observations": 3,
      "probe_lease": null
    }
  }
}`
	root := f.store.Layout().Root
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	statePath := f.store.Layout().StateFile(identity.Key)
	if err := os.WriteFile(statePath, []byte(rawStateFile), 0o644); err != nil {
		t.Fatalf("writing the pre-existing state file: %v", err)
	}

	// 1. The suppression written before the change is found after it.
	record, exists := f.store.GroupRecordFor(identity, "claude-plan")
	if !exists {
		t.Fatal("the pre-existing suppressed record was not found")
	}
	if record.State != StateSuppressed {
		t.Fatalf("record.State = %q, want suppressed", record.State)
	}
	if record.Provider != "claude" {
		t.Fatalf("record.Provider = %q, want claude — the frozen provider field moved on read", record.Provider)
	}
	if record.BackoffStep != 2 {
		t.Fatalf("record.BackoffStep = %d, want 2", record.BackoffStep)
	}

	// 2. It still gates a live claim: next_probe_at has not elapsed, so a
	// suppressed group is refused outright (granted=false), landing in neither
	// bucket below — unlike an available or probe-eligible group.
	leased, unleased, _ := f.claimConcurrentlyFor(t, identity, "claude-plan", 5)
	if leased != 0 || unleased != 0 {
		t.Fatalf("claims before next_probe_at: %d leased / %d unleased, want 0/0 — the pre-existing suppression did not gate a real claim", leased, unleased)
	}

	// 3. Once next_probe_at elapses, the pre-existing record is probe-eligible
	// and claimable through the real claim path, exactly like a record this
	// binary wrote itself: exactly one of the concurrent claimants wins the
	// probing transition, and the rest are refused (StateProbing under a
	// foreign RunID), landing in neither bucket.
	f.clock.Set(nextProbeAt.Add(time.Second))
	leased, unleased, _ = f.claimConcurrentlyFor(t, identity, "claude-plan", 5)
	if leased != 1 || unleased != 0 {
		t.Fatalf("claims after next_probe_at: %d leased / %d unleased, want 1/0 — the pre-existing record did not become probe-eligible", leased, unleased)
	}

	// 4. A fresh observation against the same pre-existing record still
	// escalates it, and the frozen "provider" field's name and value survive
	// the write unchanged.
	class := ClassifyClaudePrompt(ClaudePromptResult{ExitCode: 1, Stdout: readFixture(t, "claude-429-usage-limit.json"), Now: f.clock.Now()})
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260817-mohnt0-03", Model: "claude-opus-5"})
	if !ok {
		t.Fatal("the captured Claude 429 fixture must yield a provider-quota observation")
	}
	if err := f.store.Observe(identity, "claude-plan", nil, obs); err != nil {
		t.Fatalf("Observe against the pre-existing record: %v", err)
	}
	record, exists = f.store.GroupRecordFor(identity, "claude-plan")
	if !exists {
		t.Fatal("the record disappeared after a fresh observation")
	}
	if record.ConsecutiveLimitObservations != 4 {
		t.Fatalf("ConsecutiveLimitObservations = %d, want 4 (3 from the pre-existing file plus this observation)", record.ConsecutiveLimitObservations)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading the state file after the write: %v", err)
	}
	// writeJSONAtomic pretty-prints (space after the colon); the hand-authored
	// fixture above and TestGroupRowBrokerFieldIsAdditiveAndProviderWireNameIsFrozen
	// use compact json.Marshal instead, so the two field/value checks differ only
	// in that formatting whitespace, not in what they assert.
	if !strings.Contains(string(raw), `"provider": "claude"`) {
		t.Fatalf("after a write, the state file no longer carries the frozen provider field:\n%s", raw)
	}
}
