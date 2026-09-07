package providerlimits

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

// F1 of TASK-260908-ggxfte: the native-Pi runtimes are FROZEN rows so that
// this package's broker resolution — which walks vendorplugin.FrozenRuntimes
// only — routes pi-anthropic / pi-openai to a classified broker, resolves an
// identity keyed on (runtime, home), and reads the state file instead of
// taking the unclassifiable carve-out. The production call site is the
// launcher's AvailabilityFor{Runtime:"pi-<vendor>", Home:<managed>, Model}
// before BuildLaunch (curator launcher SPEC §4.4).

func TestALimitedRecordOnPiAnthropicReadsAsLimitedFromItsOwnHome(t *testing.T) {
	f := newVerdictFixture(t, "pi-anthropic")
	group := UnmappedGroup("pi-anthropic", "claude-opus-5")
	record := suppressedRecord(f.clock.Now())
	record.Provider = "pi-anthropic"
	f.plantRecord(t, group, record)

	verdict := f.verdict(t, "claude-opus-5")
	if verdict.State != vendorplugin.AvailabilityLimited {
		t.Fatalf("pi-anthropic with a limited record read as %s; the frozen row is not routing to the state file\n%s", verdict.State, verdictDetails(verdict))
	}
	for _, checked := range verdict.Checked {
		if checked == SourceFrozenTable {
			t.Fatalf("Checked names the frozen table %v; the verdict came from the carve-out, not the state", verdict.Checked)
		}
	}
	if len(verdict.Checked) == 0 {
		t.Fatal("a Limited verdict names no checked source")
	}
}

func TestTheSameRecordIsInvisibleFromAnotherPiHome(t *testing.T) {
	f := newVerdictFixture(t, "pi-anthropic")
	group := UnmappedGroup("pi-anthropic", "claude-opus-5")
	record := suppressedRecord(f.clock.Now())
	record.Provider = "pi-anthropic"
	f.plantRecord(t, group, record)

	otherHome := filepath.Join(f.layout.Root, "another-pi-home")
	verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: "pi-anthropic", Model: "claude-opus-5", Home: otherHome})
	if err != nil {
		t.Fatalf("AvailabilityFor(other home): %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("a record in one Pi home read as %s from another; identity is (runtime, home)\n%s", verdict.State, verdictDetails(verdict))
	}
	// And from the wrapper-era `claude` runtime on the SAME home: a different
	// runtime id is a different identity, so the record is not shared.
	verdict, err = f.store.AvailabilityFor(VerdictQuery{Runtime: ProviderClaude, Model: "claude-opus-5", Home: f.home})
	if err != nil {
		t.Fatalf("AvailabilityFor(claude, same home): %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("a pi-anthropic record leaked into the claude runtime's verdict: %s", verdict.State)
	}
}

// TestAConsumerDeclaredPiRuntimeWouldTakeTheCarveOut is the narrowing that
// shows the frozen row is load-bearing. The same store, the same planted
// record, the same home, but a runtime id the frozen table does not carry —
// exactly what a consumer-declared pi-anthropic would be — reads Healthy from
// the frozen-table carve-out while its state file says Limited. That is the
// reviewer's observed failure, reproduced.
func TestAConsumerDeclaredPiRuntimeWouldTakeTheCarveOut(t *testing.T) {
	const consumerRuntime = "pi-anthropic-consumer"
	f := newVerdictFixture(t, consumerRuntime)
	group := UnmappedGroup(consumerRuntime, "claude-opus-5")
	record := suppressedRecord(f.clock.Now())
	record.Provider = consumerRuntime
	f.plantRecord(t, group, record)

	if HasClassifierForRuntime(consumerRuntime) {
		t.Fatal("fixture assumption broken: the consumer runtime must not be in the frozen table")
	}
	verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: consumerRuntime, Model: "claude-opus-5", Home: f.home})
	if err != nil {
		t.Fatalf("AvailabilityFor: %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityHealthy || len(verdict.Checked) != 1 || verdict.Checked[0] != SourceFrozenTable {
		t.Fatalf("the unfrozen runtime did not take the carve-out (%s, checked %v); the frozen-row argument would then rest on nothing", verdict.State, verdict.Checked)
	}
	if !HasClassifierForRuntime("pi-anthropic") || !HasClassifierForRuntime("pi-openai") {
		t.Fatal("pi-anthropic / pi-openai resolve to no classifier; the frozen rows are missing or bind the wrong broker")
	}
}

// TestPiGoogleStaysUnclassifiableAndIsNeverWritten: google has no classifier,
// so pi-google can never be suppressed, through the production write gate.
func TestPiGoogleStaysUnclassifiableAndIsNeverWritten(t *testing.T) {
	if HasClassifierForRuntime("pi-google") {
		t.Fatal("pi-google resolves to a classifier; google ships none")
	}
	f := newStoreFixture(t)
	identity, err := IdentityFor("pi-google", filepath.Join(f.root, "pi-home"))
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if class := ClassifyManaged("pi-google", ManagedSignalUsageLimited); class.Class != ClassNotLimit {
		t.Fatalf("ClassifyManaged(pi-google) = %q, want not-a-limit", class.Class)
	}
	forged := Observation{quota: true, Provider: "pi-google", Evidence: Evidence{RunID: "RUN-260908-pi-google"}}
	if err := f.store.Observe(identity, UnmappedGroup("pi-google", "gemini-3.1-pro-preview"), nil, forged); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("Observe(pi-google) = %v, want ErrUnsupportedProvider", err)
	}
	verdict, err := f.store.AvailabilityFor(VerdictQuery{Runtime: "pi-google", Model: "gemini-3.1-pro-preview", Home: filepath.Join(f.root, "pi-home")})
	if err != nil {
		t.Fatalf("AvailabilityFor(pi-google): %v", err)
	}
	if verdict.State != vendorplugin.AvailabilityHealthy {
		t.Fatalf("pi-google = %s", verdict.State)
	}
}

// TestObserveAdmitsPiAnthropicThroughTheProductionWriteGate: the write side
// of the same routing. ClassifyManaged mints a limit class for pi-anthropic
// (broker anthropic) and Store.Observe records it under the runtime id.
func TestObserveAdmitsPiAnthropicThroughTheProductionWriteGate(t *testing.T) {
	f := newStoreFixture(t)
	home := filepath.Join(f.root, "managed-pi-home")
	identity, err := IdentityFor("pi-anthropic", home)
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	class := ClassifyManaged("pi-anthropic", ManagedSignalUsageLimited)
	obs, ok := class.QuotaObservation(Evidence{RunID: "RUN-260908-pi-anthropic", Model: "claude-opus-5"})
	if !ok {
		t.Fatalf("ClassifyManaged(pi-anthropic, usage_limited) = %q; anthropic's classifier must apply through the frozen row", class.Class)
	}
	group := UnmappedGroup("pi-anthropic", "claude-opus-5")
	if err := f.store.Observe(identity, group, nil, obs); err != nil {
		t.Fatalf("Observe(pi-anthropic): %v", err)
	}
	record, exists := f.store.GroupRecordFor(identity, group)
	if !exists || record.Provider != "pi-anthropic" {
		t.Fatalf("record = %#v, exists %v; want one keyed under the runtime id", record, exists)
	}
}

// TestTheManagedHomeResolvesFromThePluginDeclaration: DefaultProviderHome walks
// runtime → system → capabilities, so a pi-* runtime resolves a home from
// PI_CODING_AGENT_DIR, else ~/.pi/agent, and never from a table here.
func TestTheManagedHomeResolvesFromThePluginDeclaration(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home, err := DefaultProviderHomeIn(vendorplugin.Default, agentic.Default, "pi-anthropic")
	if err != nil {
		t.Fatalf("DefaultProviderHome(pi-anthropic): %v", err)
	}
	if home != "~/.pi/agent" {
		t.Errorf("default home = %q, want ~/.pi/agent", home)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "/Users/op/.managed/pi")
	home, err = DefaultProviderHomeIn(vendorplugin.Default, agentic.Default, "pi-openai")
	if err != nil {
		t.Fatalf("DefaultProviderHome(pi-openai): %v", err)
	}
	if home != "/Users/op/.managed/pi" {
		t.Errorf("home = %q, want the managed PI_CODING_AGENT_DIR", home)
	}
}
