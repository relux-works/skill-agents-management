package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// withVendorRegistry swaps this binary's Layer-2 registry for an isolated one,
// seeded with the frozen runtimes, for the duration of a test. Registration
// goes through the same public API a real plugin's init would use.
func withVendorRegistry(t *testing.T, systems *agentic.Registry) *vendorplugin.Registry {
	t.Helper()
	registry := vendorplugin.NewRegistry(systems)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("SeedFrozenRuntimes: %v", err)
	}
	previous := vendorRegistry
	vendorRegistry = registry
	t.Cleanup(func() { vendorRegistry = previous })
	return registry
}

// The binary ships with no vendor plugins yet — that is the next story — so
// the empty answer has to be a legitimate one that exits 0 rather than a
// failure to look.
func TestVendorsCommandEmptyListSucceeds(t *testing.T) {
	withVendorRegistry(t, agentic.NewRegistry())

	stdout, stderr, err := runRoot(t, "vendors")
	if err != nil {
		t.Fatalf("vendors command errored on an empty registry: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("stdout = %q, stderr = %q, want both empty", stdout, stderr)
	}
}

func TestVendorsCommandEmptyListEncodesAsEmptyArray(t *testing.T) {
	withVendorRegistry(t, agentic.NewRegistry())

	stdout, _, err := runRoot(t, "vendors", "--json")
	if err != nil {
		t.Fatalf("vendors --json errored: %v", err)
	}
	if stdout != "[]\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "[]\n")
	}
}

// The empty answer must mean "nothing is registered", not "this command always
// prints nothing", so a registered vendor has to come back out under its id.
func TestVendorsCommandListsRegisteredVendors(t *testing.T) {
	systems := agentic.NewRegistry()
	if err := systems.Register(stubSystem{id: "claude-code"}); err != nil {
		t.Fatalf("Register(claude-code): %v", err)
	}
	registry := withVendorRegistry(t, systems)
	if err := registry.Register(stubVendor{id: "anthropic", system: "claude-code"}); err != nil {
		t.Fatalf("Register(anthropic): %v", err)
	}

	stdout, _, err := runRoot(t, "vendors")
	if err != nil {
		t.Fatalf("vendors command failed: %v", err)
	}
	if stdout != "anthropic\n" {
		t.Errorf("stdout = %q, want the registered vendor id", stdout)
	}
}

// The six historical ids are declarations, present in every binary before any
// plugin registers. This is the operator-visible half of AC3.
func TestRuntimesCommandListsTheFrozenDeclarations(t *testing.T) {
	withVendorRegistry(t, agentic.NewRegistry())

	stdout, _, err := runRoot(t, "runtimes")
	if err != nil {
		t.Fatalf("runtimes command failed: %v", err)
	}
	want := strings.Join([]string{
		"agy\tantigravity\tgoogle",
		"claude\tclaude-code\tanthropic",
		"codex\tcodex\topenai",
		"gemini\tgemini-cli\tgoogle",
		"muse\tmuse\tvendor unresolved",
		"pi-anthropic\tpi-native\tanthropic",
		"pi-google\tpi-native\tgoogle",
		"pi-openai\tpi-native\topenai",
		"qwen\tqwen-code\talibaba",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%q\nwant =\n%q", stdout, want)
	}
}

// muse's row must carry the search behind its unresolved broker. A blank
// vendor column would read as a missing field; the checked list and the absent
// finding are what make it a statement.
func TestRuntimesJSONCarriesTheUnresolvedBrokerHonestly(t *testing.T) {
	withVendorRegistry(t, agentic.NewRegistry())

	stdout, _, err := runRoot(t, "runtimes", "--json")
	if err != nil {
		t.Fatalf("runtimes --json failed: %v", err)
	}
	var decoded []runtimeRow
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if len(decoded) != 9 {
		t.Fatalf("decoded %d runtimes, want the nine frozen ids", len(decoded))
	}
	for _, row := range decoded {
		if row.ID != "muse" {
			if !row.VendorResolved || row.Vendor == "" || row.BrokerFound == "" {
				t.Errorf("runtime %q = %#v, want a resolved vendor with the finding that established it", row.ID, row)
			}
			continue
		}
		if row.VendorResolved || row.Vendor != "" {
			t.Errorf("muse = %#v, want no vendor; the source records its broker as unknown", row)
		}
		if len(row.BrokerChecked) == 0 {
			t.Error("muse's row records no checked source, so its unresolved broker reads as an unfilled field")
		}
		if row.BrokerFound != "" {
			t.Errorf("muse claims %q established a vendor while naming none", row.BrokerFound)
		}
	}
}

// stubVendor is a minimal vendorplugin.Vendor. Its second job is the same as
// stubSystem's: proving the contract is implementable from OUTSIDE its own
// package, which is what a plugin in another repository will have to do.
type stubVendor struct {
	id     vendorplugin.VendorID
	system agentic.SystemID
}

func (v stubVendor) ID() vendorplugin.VendorID { return v.id }

func (v stubVendor) Models() []vendorplugin.Model {
	return []vendorplugin.Model{{
		ID:          "stub-1",
		Description: "the only model this stub declares",
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Rank: vendorplugin.CapabilityRank{
			Score: 1,
			Basis: []vendorplugin.RankEvidence{{
				Source:      "this test",
				Observation: "it is the only row, so it is the top of its own lineup",
			}},
		},
		Effort:  vendorplugin.EffortDeclaration{Support: agentic.EffortSupportNone},
		Systems: []agentic.SystemID{v.system},
	}}
}

func (v stubVendor) Availability(vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return vendorplugin.UnknownAfterCheck("this stub reads no source"), nil
}

func (v stubVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return agentic.LaunchRequest{
		System: sc.Runtime.SystemID,
		Model:  sc.Model.Launchable(),
		Effort: sc.Effort,
	}, nil
}
