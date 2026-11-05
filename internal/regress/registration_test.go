package regress

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The six Layer-1 plugins and the four Layer-2 plugins, compiled in so
	// this package can drive the REAL registrations rather than a double. Each
	// import runs the plugin's own init, which is the production registration
	// path, into the package-level defaults.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"

	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/alibaba"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/anthropic"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/google"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)

// CLASS 1 — the dependency direction, enforced at registration.
//
// A vendor plugin depends on the agentic systems that drive its models, never
// the other way round, and vendorplugin.Registry.Register is where that is
// checked. This file drives the REAL vendor plugins into registries built
// against a deliberately empty agentic registry and requires the refusal —
// then registers the systems those same plugins name and requires the
// admission.
//
// Both halves are needed and neither is decoration. The refusal alone is
// satisfied by a registry that refuses everything; the admission alone is
// satisfied by a registry that checks nothing.

// realVendors is every Layer-2 plugin this binary carries, read from the
// default registry rather than listed here. A vendor added later is covered
// with no edit, and a list here would be the second binding the single-source
// guard exists to prevent.
func realVendors(t *testing.T) []vendorplugin.Vendor {
	t.Helper()
	ids := vendorplugin.Default.VendorIDs()
	if len(ids) == 0 {
		t.Fatal("no vendor plugin is compiled into this test binary, so every check below would range over nothing")
	}
	vendors := make([]vendorplugin.Vendor, 0, len(ids))
	for _, id := range ids {
		vendor, ok := vendorplugin.Default.Lookup(id)
		if !ok {
			t.Fatalf("the default registry lists %q and then cannot look it up", id)
		}
		vendors = append(vendors, vendor)
	}
	return vendors
}

// declaredSystems is the set of agentic systems a vendor's own model rows
// name, in declaration order and deduplicated. It is READ FROM THE PLUGIN, so
// a vendor that grows a harness is covered without this file knowing.
func declaredSystems(vendor vendorplugin.Vendor) []agentic.SystemID {
	var out []agentic.SystemID
	for _, model := range vendor.Models() {
		for _, system := range model.Systems {
			known := false
			for _, already := range out {
				if already == system {
					known = true
					break
				}
			}
			if !known {
				out = append(out, system)
			}
		}
	}
	return out
}

// TestAVendorNamingAnUnregisteredSystemIsRefusedNamingBothIDs is the refusal.
//
// The agentic registry is empty on purpose: every system every vendor names is
// therefore unregistered, and the message has to name the vendor AND the
// system. A refusal that named only one of them sends an operator to the wrong
// repository — "anthropic will not register" and "the claude-code plugin is
// not compiled in" are the same fact with two very different fixes.
func TestAVendorNamingAnUnregisteredSystemIsRefusedNamingBothIDs(t *testing.T) {
	for _, vendor := range realVendors(t) {
		t.Run(vendor.ID().String(), func(t *testing.T) {
			systems := declaredSystems(vendor)
			if len(systems) == 0 {
				t.Fatalf("%s declares no agentic system at all; there is no dependency here to check", vendor.ID())
			}

			registry := vendorplugin.NewRegistry(agentic.NewRegistry())
			err := registry.Register(vendor)
			if err == nil {
				t.Fatalf("%s registered against an EMPTY agentic registry; every model it carries now resolves to a harness nobody compiled in, and the failure will surface at launch", vendor.ID())
			}
			if !errors.Is(err, vendorplugin.ErrUnknownAgenticSystem) {
				t.Fatalf("%s was refused, but not as an unknown agentic system: %v", vendor.ID(), err)
			}
			if !strings.Contains(err.Error(), vendor.ID().String()) {
				t.Errorf("the refusal does not name the vendor %q: %v", vendor.ID(), err)
			}
			named := false
			for _, system := range systems {
				if strings.Contains(err.Error(), system.String()) {
					named = true
					break
				}
			}
			if !named {
				t.Errorf("the refusal names none of the systems %v that %s declares: %v", systems, vendor.ID(), err)
			}
			if _, registered := registry.Lookup(vendor.ID()); registered {
				t.Errorf("%s was refused and is in the registry anyway", vendor.ID())
			}
		})
	}
}

// TestAVendorWhoseSystemsAreRegisteredIsAdmitted is the narrowing, and it is
// what stops the test above from passing against a registry that refuses
// everything.
//
// The systems are taken from the DEFAULT agentic registry — the real plugin
// values, registered by their own init functions — and moved into an isolated
// registry, so the admission is the production pairing rather than a double
// standing in for one.
func TestAVendorWhoseSystemsAreRegisteredIsAdmitted(t *testing.T) {
	for _, vendor := range realVendors(t) {
		t.Run(vendor.ID().String(), func(t *testing.T) {
			systems := agentic.NewRegistry()
			for _, id := range declaredSystems(vendor) {
				plugin, ok := agentic.Default.Lookup(id)
				if !ok {
					t.Fatalf("%s declares agentic system %q and no such plugin is compiled in; the two layers disagree about what ships", vendor.ID(), id)
				}
				if err := systems.Register(plugin); err != nil {
					t.Fatalf("registering the real %q plugin into an isolated registry: %v", id, err)
				}
			}
			registry := vendorplugin.NewRegistry(systems)
			if err := registry.Register(vendor); err != nil {
				t.Fatalf("%s was refused with every system it names registered: %v", vendor.ID(), err)
			}
			if _, registered := registry.Lookup(vendor.ID()); !registered {
				t.Fatalf("%s registered without error and is not in the registry", vendor.ID())
			}
		})
	}
}

// TestAVendorRegistryWithNoAgenticRegistryRefusesEveryVendor is the third
// state, and the one an implementation is most likely to get wrong by being
// helpful: a registry with nothing to check against must refuse rather than
// admit, because admitting means the dependency check was skipped and nothing
// says so.
func TestAVendorRegistryWithNoAgenticRegistryRefusesEveryVendor(t *testing.T) {
	registry := vendorplugin.NewRegistry(nil)
	for _, vendor := range realVendors(t) {
		err := registry.Register(vendor)
		if !errors.Is(err, vendorplugin.ErrNoAgenticRegistry) {
			t.Errorf("registering %s into a registry with no agentic registry returned %v, want ErrNoAgenticRegistry", vendor.ID(), err)
		}
	}
}
