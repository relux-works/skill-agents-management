package regress

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// CLASS 2 — runtime declarations and the F2 collision policy.
//
// A runtime is a stable id bound to one (agentic system x vendor) pair. The id
// feeds admitted-pair digests and limit-state filenames, so the two directions
// of a redeclaration are not symmetric and the asymmetry is the whole rule:
//
//   - A declaration MATCHING an existing one is legal and idempotent. Two
//     packages declaring the same pair have not disagreed, and refusing the
//     second would make declaration order load-bearing.
//   - A CONFLICTING declaration is refused and the FIRST declaration stands.
//     Letting the second win is a silent rebind, and a rebind orphans every
//     suppression filed under the old pair with no error anywhere.
//
// Both are checked here, and so is the thing a refusal alone does not
// establish: that the stored binding did not move while the caller was being
// told no.

// seededRegistry is a fresh registry carrying the frozen table, built through
// the same seed the default registry's init calls.
func seededRegistry(t *testing.T) *vendorplugin.Registry {
	t.Helper()
	registry := vendorplugin.NewRegistry(agentic.NewRegistry())
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding the frozen runtime table: %v", err)
	}
	return registry
}

// TestTheDefaultRegistryCarriesTheFrozenRuntimesBeforeAnyPluginRegisters is
// the production call site: vendorplugin's own init seeds the six, so every
// binary declares them whether or not their plugins are compiled in.
func TestTheDefaultRegistryCarriesTheFrozenRuntimesBeforeAnyPluginRegisters(t *testing.T) {
	frozen := vendorplugin.FrozenRuntimes()
	if len(frozen) == 0 {
		t.Fatal("the frozen runtime table is empty, so every check in this file would range over nothing")
	}
	for _, want := range frozen {
		got, declared := vendorplugin.Default.RuntimeDeclarationOf(want.ID)
		if !declared {
			t.Errorf("runtime %q is frozen and the default registry does not declare it", want.ID)
			continue
		}
		if !got.SameBinding(want) {
			t.Errorf("runtime %q is declared as (%s x %s); the frozen table binds it to (%s x %s)",
				want.ID, got.System, got.VendorLabel(), want.System, want.VendorLabel())
		}
	}
}

// TestAMatchingRedeclarationIsIdempotent is the accepting direction.
func TestAMatchingRedeclarationIsIdempotent(t *testing.T) {
	registry := seededRegistry(t)
	before := registry.RuntimeDeclarations()
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("re-seeding a registry that already carries the frozen table must be a no-op: %v", err)
	}
	after := registry.RuntimeDeclarations()
	if len(before) != len(after) {
		t.Fatalf("re-seeding changed the declaration count from %d to %d", len(before), len(after))
	}
	for i := range before {
		if !before[i].SameBinding(after[i]) {
			t.Errorf("re-seeding moved %q from (%s x %s) to (%s x %s)",
				before[i].ID, before[i].System, before[i].VendorLabel(), after[i].System, after[i].VendorLabel())
		}
	}
}

// TestARedeclarationDifferingOnlyInProvenanceIsStillIdempotent narrows the
// idempotency rule from the side that would break real callers.
//
// SameBinding compares (id, system, vendor) and deliberately not the broker
// provenance: two callers declaring the same pair with differently worded
// evidence have not disagreed about anything. A registry that compared the
// prose would refuse a legitimate second declaration, and this is the mutant
// that catches it.
func TestARedeclarationDifferingOnlyInProvenanceIsStillIdempotent(t *testing.T) {
	registry := seededRegistry(t)
	for _, original := range vendorplugin.FrozenRuntimes() {
		reworded := original
		reworded.Broker = vendorplugin.BrokerProvenance{
			Checked: []string{"a second caller that looked somewhere else"},
			Found:   original.Broker.Found,
		}
		if !original.VendorResolved() {
			reworded.Broker.Found = ""
		}
		if err := registry.DeclareRuntime(reworded); err != nil {
			t.Errorf("redeclaring %q with the same binding and different provenance was refused: %v", original.ID, err)
		}
	}
}

// TestAConflictingRedeclarationIsRefusedAndTheFirstStands is the refusing
// direction, on BOTH axes of the pair.
//
// The assertion that matters most is the last one in each case: after the
// refusal the stored declaration is still the original. A registry that
// returned an error and wrote anyway would pass an error check and orphan the
// state regardless.
func TestAConflictingRedeclarationIsRefusedAndTheFirstStands(t *testing.T) {
	// Spellings that name nothing in this module: no plugin, no frozen row,
	// no vendor. A declaration does not require its plugins to exist, so these
	// are legal declarations and illegal REdeclarations, which is exactly the
	// shape under test. Reusing a real id would make some frozen row conflict
	// with itself and quietly test nothing.
	const (
		foreignSystem = agentic.SystemID("opencode")
		foreignVendor = vendorplugin.VendorID("mistral")
	)
	// The rebound declarations drop the original's model rows, and the drop is
	// what keeps this a test of the CONFLICT rule rather than of the validation
	// in front of it. A second declaration claiming a different pair is not
	// claiming the first's rows: carried across a system rebind they would name
	// a harness the new pair cannot drive, and carried across a vendor rebind
	// they would be the resolved-runtime-declaring-models shape that Validate
	// refuses one step earlier. Either way the registry would answer "malformed"
	// where this test needs it to answer "already declared for another pair".
	for _, original := range vendorplugin.FrozenRuntimes() {
		conflicts := []struct {
			axis        string
			declaration vendorplugin.RuntimeDeclaration
		}{}
		if original.System != foreignSystem {
			rebound := original
			rebound.System = foreignSystem
			rebound.Models = nil
			conflicts = append(conflicts, struct {
				axis        string
				declaration vendorplugin.RuntimeDeclaration
			}{"system", rebound})
		}
		if original.Vendor != foreignVendor {
			rebound := original
			rebound.Vendor = foreignVendor
			rebound.Models = nil
			rebound.Broker = vendorplugin.BrokerProvenance{
				Checked: []string{"a second declaration that believes it knows better"},
				Found:   "asserted by the second declaration",
			}
			conflicts = append(conflicts, struct {
				axis        string
				declaration vendorplugin.RuntimeDeclaration
			}{"vendor", rebound})
		}
		if len(conflicts) == 0 {
			t.Fatalf("runtime %q could not be made to conflict on either axis; this case tests nothing", original.ID)
		}

		for _, conflict := range conflicts {
			t.Run(string(original.ID)+"/"+conflict.axis, func(t *testing.T) {
				registry := seededRegistry(t)
				err := registry.DeclareRuntime(conflict.declaration)
				if err == nil {
					t.Fatalf("rebinding runtime %q to (%s x %s) was accepted; every limit-state file and admitted-pair digest filed under the old pair is now orphaned",
						original.ID, conflict.declaration.System, conflict.declaration.VendorLabel())
				}
				if !errors.Is(err, vendorplugin.ErrRuntimeConflict) {
					t.Fatalf("rebinding %q was refused, but not as a conflict: %v", original.ID, err)
				}
				for _, want := range []string{
					string(original.ID),
					original.System.String(),
					original.VendorLabel(),
					conflict.declaration.System.String(),
					conflict.declaration.VendorLabel(),
				} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the conflict message does not name %q, so a reader cannot see which binding won: %v", want, err)
					}
				}
				stored, declared := registry.RuntimeDeclarationOf(original.ID)
				if !declared {
					t.Fatalf("runtime %q vanished from the registry when a conflicting declaration was refused", original.ID)
				}
				if !stored.SameBinding(original) {
					t.Errorf("the refused declaration won anyway: %q now reads (%s x %s), want the first declaration (%s x %s)",
						original.ID, stored.System, stored.VendorLabel(), original.System, original.VendorLabel())
				}
			})
		}
	}
}
