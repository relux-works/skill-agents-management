package agentic

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type graphPlugin struct{ declaration plugin.Declaration }

func (p graphPlugin) PluginDeclaration() plugin.Declaration { return p.declaration }

// withID builds a variant of the one test double rather than a second fake,
// so a refusal test and the seam test are measuring the same implementation.
func withID(id SystemID) *pangolinSystem {
	sys := newPangolin()
	sys.id = id
	return sys
}

// An empty registry is a state, not a failure: a binary with no plugins
// compiled in must be able to say so.
func TestEmptyRegistryAnswersNothingRegistered(t *testing.T) {
	registry := NewRegistry()
	if registry.Len() != 0 {
		t.Errorf("Len = %d, want 0", registry.Len())
	}
	if ids := registry.IDs(); len(ids) != 0 || ids == nil {
		t.Errorf("IDs = %#v, want a non-nil empty slice so a caller need not special-case the empty registry", ids)
	}
	if _, ok := registry.Lookup("codex"); ok {
		t.Error("Lookup found a system in an empty registry")
	}
}

func TestSystemRegistrationPublishesKindDataToTheGeneralGraph(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(newPangolin()); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	declaration, ok := registry.Graph().Declaration(plugin.ID(pangolinID))
	if !ok {
		t.Fatal("the compatibility registry accepted pangolin but did not publish it to the plugin graph")
	}
	if declaration.Kind != PluginKind {
		t.Fatalf("pangolin kind = %q, want %q", declaration.Kind, PluginKind)
	}
}

func TestSystemMayDeclareAnEngineDependencyWithoutChangingSystem(t *testing.T) {
	registry := NewRegistry()
	engine := graphPlugin{declaration: plugin.Declaration{ID: "llama-cpp", Kind: "inference-engine"}}
	if err := registry.RegisterPlugin(engine); err != nil {
		t.Fatalf("RegisterPlugin(engine): %v", err)
	}
	if err := registry.RegisterWithDependencies(newPangolin(), plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"}); err != nil {
		t.Fatalf("RegisterWithDependencies(pangolin -> engine): %v", err)
	}
	resolved, err := registry.Graph().Resolve(plugin.ID(pangolinID))
	if err != nil {
		t.Fatalf("Resolve(pangolin): %v", err)
	}
	if len(resolved.Dependencies) != 1 || resolved.Dependencies[0].Declaration.ID != "llama-cpp" {
		t.Fatalf("pangolin graph dependencies = %#v, want llama-cpp", resolved.Dependencies)
	}
}

// Duplicate registration is a bug, not a no-op. Same-binding idempotency is
// deliberately not offered here: silently accepting the second registration is
// how a shadow binding gets in without anyone reading a diff.
func TestRegisterRefusesDuplicateID(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(newPangolin()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := registry.Register(newPangolin())
	if !errors.Is(err, ErrDuplicateSystem) {
		t.Fatalf("second Register err = %v, want ErrDuplicateSystem", err)
	}
	if !strings.Contains(err.Error(), string(pangolinID)) {
		t.Errorf("duplicate error %q does not name the id that collided", err)
	}
	if registry.Len() != 1 {
		t.Errorf("Len = %d after a refused duplicate, want 1", registry.Len())
	}
}

// A second spelling of one id must never become a registration. System.ID is
// contracted to be its own normal form; the registry enforces that instead of
// quietly folding the spelling, because folding leaves the PLUGIN answering
// the raw spelling to everyone who holds it while the registry answers the
// folded one — two names for one system, which is the disease invariant 5
// exists to prevent, moved from a map key into the plugin.
//
// This test replaces an earlier one that registered "Pangolin" and expected
// "  PANGOLIN  " to collide as a duplicate. That shape is unreachable now:
// neither spelling registers at all, which is strictly stronger. The
// normalized-lookup half of the old assertion lives on in
// TestLookupNormalizesAndRefusesGarbage, and duplicate detection in
// TestRegisterRefusesDuplicateID.
func TestRegisterRefusesAnIDThatDoesNotNormalizeToItself(t *testing.T) {
	for _, raw := range []SystemID{"Pangolin", "  PANGOLIN  ", "pangolin "} {
		t.Run(string(raw), func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(withID(raw))
			if !errors.Is(err, ErrUnnormalizedSystemID) {
				t.Fatalf("Register(%q) err = %v, want ErrUnnormalizedSystemID", raw, err)
			}
			if !strings.Contains(err.Error(), string(raw)) {
				t.Errorf("refusal %q does not name the spelling the plugin returned", err)
			}
			if !strings.Contains(err.Error(), string(pangolinID)) {
				t.Errorf("refusal %q does not name the spelling it should have returned; the plugin author cannot fix the declaration from the error alone", err)
			}
			if registry.Len() != 0 {
				t.Errorf("Len = %d after a refused registration, want 0", registry.Len())
			}
			if _, ok := registry.Lookup(pangolinID); ok {
				t.Error("the refused plugin is reachable under its normalized id; the refusal registered it anyway")
			}
		})
	}
}

// The gate must refuse the spelling, not the plugin: the same double with the
// canonical id registers, so a green refusal above is not a registry that
// refuses everything.
func TestRegisterAcceptsTheNormalizedSpellingOfTheSameID(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(withID(pangolinID)); err != nil {
		t.Fatalf("Register(%q): %v", pangolinID, err)
	}
	ids := registry.IDs()
	if len(ids) != 1 || ids[0] != pangolinID {
		t.Errorf("IDs = %v, want exactly [%s]", ids, pangolinID)
	}
	sys, ok := registry.Lookup(pangolinID)
	if !ok {
		t.Fatalf("Lookup(%q) missed its own registration", pangolinID)
	}
	if sys.ID() != ids[0] {
		t.Errorf("the plugin answers %q while the registry holds %q; one system, two names", sys.ID(), ids[0])
	}
}

// An UNSTABLE plugin is the hole a single read left. The registry documented
// its normalization refusal as making two spellings of one system impossible;
// a plugin whose ID() answers "pangolin" once and "Pangolin" afterwards
// registered under the first answer, served the second to anyone holding it,
// and BuildPlan planned around the disagreement without a word. Register now
// reads ID() twice and refuses.
//
// The bound this test proves is exactly two reads, and no more: see
// TestRegisterCannotSeeAnIDThatFlipsAfterRegistration for the half that stays
// the plugin's obligation.
func TestRegisterRefusesAnUnstableID(t *testing.T) {
	registry := NewRegistry()
	sys := newPangolin()
	sys.unstableID = "Pangolin"

	err := registry.Register(sys)
	if !errors.Is(err, ErrUnstableSystemID) {
		t.Fatalf("Register(unstable ID) err = %v, want ErrUnstableSystemID", err)
	}
	for _, spelling := range []string{"pangolin", "Pangolin"} {
		if !strings.Contains(err.Error(), spelling) {
			t.Errorf("refusal %q does not name the answer %q; a plugin author cannot find an unstable ID() from an error that hides one of its values", err, spelling)
		}
	}
	if registry.Len() != 0 {
		t.Errorf("Len = %d after a refused registration, want 0", registry.Len())
	}
	for _, spelling := range []SystemID{"pangolin", "Pangolin"} {
		if _, ok := registry.Lookup(spelling); ok {
			t.Errorf("the refused plugin is reachable under %q; the refusal registered it anyway", spelling)
		}
	}
	if _, err := BuildPlan(registry, LaunchRequest{System: pangolinID, Model: Model{ID: "p-1"}}, LaunchModeExec); !errors.Is(err, ErrUnknownSystem) {
		t.Errorf("BuildPlan under a refused unstable plugin err = %v, want ErrUnknownSystem; the refusal did not reach the planner", err)
	}
}

// The gate must refuse instability, not the double: the SAME double with
// unstableID unset registers and plans. Without this the test above is equally
// satisfied by a Register that refuses everything.
func TestRegisterAcceptsTheSameDoubleWhenItsIDIsStable(t *testing.T) {
	registry := NewRegistry()
	sys := newPangolin()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register(stable double): %v", err)
	}
	if sys.calls["ID"] < 2 {
		t.Errorf("Register read ID() %d times, want at least 2; one read cannot see an id that disagrees with itself", sys.calls["ID"])
	}
}

// The other side of the bound, stated as a test so it is a known quantity
// rather than a hopeful reading of the docstring: a plugin that answers
// consistently through registration and flips LATER is admitted, and the
// registry and the plugin then disagree. That is the plugin breaking
// System.ID's stability contract, and no registration-time check can catch it
// — the honest place for it is the contract, not a claim that it cannot
// happen. If a future change starts catching this, this test fails and the
// promise on System.ID gets rewritten with it.
func TestRegisterCannotSeeAnIDThatFlipsAfterRegistration(t *testing.T) {
	registry := NewRegistry()
	sys := newPangolin()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register: %v", err)
	}
	sys.id = "Pangolin"

	held, ok := registry.Lookup(pangolinID)
	if !ok {
		t.Fatalf("Lookup(%q) missed a registration that succeeded", pangolinID)
	}
	if held.ID() == pangolinID {
		t.Fatalf("the plugin still answers %q after being mutated; this test no longer demonstrates the residual it names", pangolinID)
	}
	if registry.IDs()[0] != pangolinID {
		t.Fatalf("the registry key changed with the plugin; it is a copy of the id, not a live read")
	}
	// Recorded, not asserted as desirable: one system, two names, and the
	// planner plans on the registry's spelling.
	plan, err := BuildPlan(registry, LaunchRequest{System: pangolinID, Model: Model{ID: "p-1"}}, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.System != pangolinID {
		t.Fatalf("Plan.System = %q, want the registry's spelling %q", plan.System, pangolinID)
	}
}

func TestRegisterRefusesNilSystem(t *testing.T) {
	if err := NewRegistry().Register(nil); !errors.Is(err, ErrNilSystem) {
		t.Fatalf("Register(nil) err = %v, want ErrNilSystem", err)
	}
}

// Every spelling below would key downstream state — digests, limit-state
// filenames — under something no other component would reproduce.
func TestRegisterRefusesUnnormalizableIDs(t *testing.T) {
	cases := []struct {
		id     SystemID
		reason string
	}{
		{"", "empty"},
		{"   ", "whitespace only"},
		{"claude code", "an inner space"},
		{"claude_code", "an underscore"},
		{"claude--code", "an empty segment"},
		{"-codex", "a leading hyphen"},
		{"codex-", "a trailing hyphen"},
		{"claude.code", "a dot"},
		{"claudé", "a non-ASCII letter"},
		{"claude/code", "a path separator"},
	}
	for _, tc := range cases {
		t.Run(string(tc.id)+" ("+tc.reason+")", func(t *testing.T) {
			err := NewRegistry().Register(withID(tc.id))
			var invalid *InvalidSystemIDError
			if !errors.As(err, &invalid) {
				t.Fatalf("Register(%q) err = %v, want an InvalidSystemIDError", tc.id, err)
			}
			if !strings.Contains(err.Error(), "claude-code") {
				t.Errorf("refusal %q does not show an accepted spelling; an agent hitting this mid-run cannot fix the declaration from the error text", err)
			}
		})
	}
}

// A system that declares no launch mode can never launch. Admitting it defers
// the failure to the first launch attempt, where the cause is much further
// from the declaration that caused it.
func TestRegisterRefusesSystemWithNoLaunchModes(t *testing.T) {
	sys := newPangolin()
	sys.caps.LaunchModes = nil
	if err := NewRegistry().Register(sys); !errors.Is(err, ErrNoLaunchModes) {
		t.Fatalf("Register err = %v, want ErrNoLaunchModes", err)
	}
}

func TestRegisterRefusesUnknownLaunchMode(t *testing.T) {
	sys := newPangolin()
	sys.caps.LaunchModes = []LaunchMode{LaunchModeExec, LaunchMode(97)}
	err := NewRegistry().Register(sys)
	if !errors.Is(err, ErrInvalidLaunchMode) {
		t.Fatalf("Register err = %v, want ErrInvalidLaunchMode", err)
	}
	if !strings.Contains(err.Error(), "launch-mode(97)") {
		t.Errorf("refusal %q does not name the undeclared mode", err)
	}
}

// An undeclared transport would make CanCarry answer for something no plugin
// can implement, and the answer it gives — the default branch — is "cannot
// carry", which is a decided no derived from a value nobody defined.
func TestRegisterRefusesUnknownEffortTransport(t *testing.T) {
	sys := newPangolin()
	sys.caps.EffortTransport = EffortTransport(42)
	if err := NewRegistry().Register(sys); !errors.Is(err, ErrInvalidEffortTransport) {
		t.Fatalf("Register err = %v, want ErrInvalidEffortTransport", err)
	}
}

func TestLookupNormalizesAndRefusesGarbage(t *testing.T) {
	registry := registerPangolin(t, newPangolin())
	if _, ok := registry.Lookup("  PanGolin "); !ok {
		t.Error("Lookup did not normalize the identifier it was handed")
	}
	if _, ok := registry.Lookup("pango lin"); ok {
		t.Error("Lookup accepted an identifier that cannot name any registration")
	}
	if _, ok := registry.Lookup("pangolin-2"); ok {
		t.Error("Lookup matched a different identifier")
	}
}

func TestIDsAreSortedAndDoNotAliasTheRegistry(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []SystemID{"muse", "codex", "claude-code"} {
		if err := registry.Register(withID(id)); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
	}
	ids := registry.IDs()
	want := "[claude-code codex muse]"
	if fmt.Sprint(ids) != want {
		t.Fatalf("IDs = %v, want %s", ids, want)
	}
	ids[0] = "mutated"
	if fmt.Sprint(registry.IDs()) != want {
		t.Errorf("IDs aliased the registry: a caller overwrote a registration through the returned slice")
	}
}

// The registry is read by concurrent callers and written by plugin init; the
// race detector is what makes this assertion mean anything, so this test earns
// its keep under `go test -race` and costs almost nothing without it.
func TestRegistryIsSafeForConcurrentUse(t *testing.T) {
	registry := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = registry.Register(withID(SystemID(fmt.Sprintf("system-%d", i))))
			_, _ = registry.Lookup("system-0")
			_ = registry.IDs()
			_ = registry.Len()
		}(i)
	}
	wg.Wait()
	if registry.Len() != 8 {
		t.Errorf("Len = %d after 8 concurrent registrations, want 8", registry.Len())
	}
}

// The package-level Default exists so a plugin can register from init. It must
// be a real registry rather than a nil that every caller has to check.
func TestDefaultRegistryIsUsable(t *testing.T) {
	if Default == nil {
		t.Fatal("Default is nil; a plugin's init has nothing to register into")
	}
	if err := Default.Register(nil); !errors.Is(err, ErrNilSystem) {
		t.Errorf("Register(nil) through the package-level helper err = %v, want ErrNilSystem", err)
	}
}
