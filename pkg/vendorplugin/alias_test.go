package vendorplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THE ALIAS DECLARATION GATE.
//
// agentic.BuildPlan substitutes AliasOf into a launch without asking any
// question about it — it holds no catalogue and could not answer one. That
// makes THIS the layer where an alias is checked, and it makes every rule here
// load-bearing rather than tidy: a row admitted with a dangling target, a
// chain, a wider vocabulary or an extra harness produces a launch that runs a
// model the caller did not name, or runs it under a contract the caller was
// validated against and it does not have.
//
// Both production call sites are driven. Registry.Register is the vendor path;
// Registry.DeclareRuntime is the vendor-unresolved declaration path, and it is
// the one that matters most today because the only alias in the module lives
// there — a gate wired into Register alone would report green while leaving
// exactly the rows nobody else checks unchecked.

// aliasModels is a two-row lineup with a legal alias, so each test below
// breaks ONE rule and the refusal names that rule rather than a second defect.
func aliasModels() []Model {
	identity := Model{
		ID:          "narwhal-deep",
		Description: "the identity row",
		Lifecycle:   LifecycleCurrent,
		Rank:        CapabilityRank{Score: 20, Basis: []RankEvidence{{Source: "alias fixture", Observation: "the identity"}}},
		Effort: EffortDeclaration{
			Support:     agentic.EffortSupportRequired,
			Vocabulary:  []string{"shallow", "deep"},
			Recommended: "deep",
		},
		Systems: []agentic.SystemID{pangolinID},
	}
	alias := identity
	alias.ID = "narwhal"
	alias.Description = "the short spelling of the identity row"
	alias.AliasOf = "narwhal-deep"
	return []Model{identity, alias}
}

// registerAliasVendor drives Registry.Register — the production call site — with
// a lineup the caller has mutated.
func registerAliasVendor(t *testing.T, models []Model) error {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(newPangolinSystem()); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	vendor := newNarwhal()
	vendor.models = models
	return NewRegistry(systems).Register(vendor)
}

// declareAliasRuntime drives Registry.DeclareRuntime — the OTHER production call
// site — with the same lineup carried by a vendor-unresolved declaration.
func declareAliasRuntime(t *testing.T, models []Model) error {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(newPangolinSystem()); err != nil {
		t.Fatalf("Register(pangolin): %v", err)
	}
	return NewRegistry(systems).DeclareRuntime(RuntimeDeclaration{
		ID:     tuskID,
		System: pangolinID,
		Models: models,
		Broker: BrokerProvenance{
			// Checked with no Found: the vendor-unresolved shape the muse rows
			// have, which is the whole reason this call site needs its own gate.
			Checked: []string{"the alias fixture's own registration"},
		},
	})
}

// aliasMutants are the illegal declarations, each with the launch failure it
// would produce if the gate admitted it.
var aliasMutants = []struct {
	name    string
	mutate  func([]Model) []Model
	wantErr error
	// wantText is a fragment the refusal must carry, so a gate that fired for
	// an unrelated reason cannot pass as this one.
	wantText string
}{
	{
		name: "a target no model in the lineup declares",
		mutate: func(models []Model) []Model {
			models[1].AliasOf = "narwhal-deep-2"
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "narwhal-deep-2",
	},
	{
		name: "a target that is itself an alias",
		mutate: func(models []Model) []Model {
			chain := models[1]
			chain.ID = "n"
			chain.AliasOf = "narwhal"
			return append(models, chain)
		},
		wantErr:  ErrAliasInvalid,
		wantText: "one hop",
	},
	{
		name: "an alias naming itself",
		mutate: func(models []Model) []Model {
			models[1].AliasOf = models[1].ID
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "its own alias target",
	},
	{
		name: "an alias target that is not a usable model id",
		mutate: func(models []Model) []Model {
			models[1].AliasOf = "narwhal deep"
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "not a usable model id",
	},
	{
		name: "an alias accepting a word the identity does not",
		mutate: func(models []Model) []Model {
			models[1].Effort.Vocabulary = []string{"shallow", "deep", "deeper"}
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "deeper",
	},
	{
		name: "an alias missing a word the identity accepts",
		mutate: func(models []Model) []Model {
			models[1].Effort.Vocabulary = []string{"deep"}
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "transported to the target",
	},
	{
		name: "an alias recommending a different word",
		mutate: func(models []Model) []Model {
			models[1].Effort.Recommended = "shallow"
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "shallow",
	},
	{
		name: "an alias with no effort axis over an identity that requires one",
		mutate: func(models []Model) []Model {
			models[1].Effort = EffortDeclaration{Support: agentic.EffortSupportNone}
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "effort",
	},
	{
		name: "an alias driven by a harness the identity never declared",
		mutate: func(models []Model) []Model {
			models[1].Systems = []agentic.SystemID{pangolinID, "narwhal-cli"}
			models[0].Systems = []agentic.SystemID{pangolinID}
			return models
		},
		wantErr:  ErrAliasInvalid,
		wantText: "runs the target",
	},
}

func TestTheAliasGateRefusesWhatItMustReject(t *testing.T) {
	for _, mutant := range aliasMutants {
		t.Run(mutant.name, func(t *testing.T) {
			for site, register := range map[string]func(*testing.T, []Model) error{
				"Registry.Register":       registerAliasVendor,
				"Registry.DeclareRuntime": declareAliasRuntime,
			} {
				err := register(t, mutant.mutate(aliasModels()))
				if !errors.Is(err, mutant.wantErr) {
					t.Errorf("%s admitted it: err = %v, want %v", site, err, mutant.wantErr)
					continue
				}
				if !strings.Contains(err.Error(), mutant.wantText) {
					t.Errorf("%s refusal %q does not name %q, so a reader cannot tell which rule fired", site, err, mutant.wantText)
				}
			}
		})
	}
}

// TestTheAliasGateAdmitsALegalAlias proves the gate is REACHABLE rather than
// simply closed. Without it every subtest above would be equally satisfied by a
// checker that refused every lineup carrying an alias at all — and this module
// would then have no way to declare the one it needs.
func TestTheAliasGateAdmitsALegalAlias(t *testing.T) {
	if err := registerAliasVendor(t, aliasModels()); err != nil {
		t.Errorf("Registry.Register refused a legal alias: %v", err)
	}
	if err := declareAliasRuntime(t, aliasModels()); err != nil {
		t.Errorf("Registry.DeclareRuntime refused a legal alias: %v", err)
	}
}

// TestTheAliasGateIgnoresTheFactsItDoesNotCover states the bound rather than
// leaving a reader to infer it from a green run.
//
// checkAliases holds the effort axis and the agentic systems and NOTHING else.
// Rank, lifecycle, context window, pricing, the display recommendation and the
// description may all differ between an alias and its identity: none of them
// reaches argv or the admitted-pair digest, and a checker that pinned them
// would be enforcing an editorial rule under a launch rule's error message. If
// that ever becomes wrong, this test is the one that has to change.
func TestTheAliasGateIgnoresTheFactsItDoesNotCover(t *testing.T) {
	models := aliasModels()
	models[1].Rank = CapabilityRank{Score: 1, Basis: []RankEvidence{{Source: "the bound", Observation: "a deliberately different score"}}}
	models[1].Lifecycle = LifecycleLegacy
	models[1].SupersededBy = "narwhal-deep"
	models[1].ContextWindowTokens = 4096
	models[1].Description = "a description shared with nothing"

	if err := registerAliasVendor(t, models); err != nil {
		t.Errorf("Registry.Register refused a legal alias over facts this gate does not cover: %v", err)
	}
}

// TestBuildLaunchRefusesAVendorThatSetsTheAliasItself is the redirection gate.
//
// checkLaunchFidelity's whole subject is a vendor that ADDS to a launch versus
// one that REDIRECTS it, and AliasOf is now the most direct redirection there
// is: a plugin that could set it on the way out would choose which model
// actually executes, after admission has already passed on a different one.
func TestBuildLaunchRefusesAVendorThatSetsTheAliasItself(t *testing.T) {
	vendor := newNarwhal()
	vendor.spawnAliasOf = "narwhal-flat"
	registry := registerNarwhal(t, vendor)

	_, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeExec)
	if !errors.Is(err, ErrVendorContract) {
		t.Fatalf("err = %v, want ErrVendorContract", err)
	}
	if !strings.Contains(err.Error(), "narwhal-flat") {
		t.Errorf("refusal %q does not name the model the vendor tried to redirect onto", err)
	}
}

// TestBuildLaunchRefusesAVendorThatDropsADeclaredAlias is the same gate from
// the other side. A plugin that CLEARED the alias would launch the operator's
// short spelling at the provider — the exact failure the field exists to close
// — and every check above it would still be green.
func TestBuildLaunchRefusesAVendorThatDropsADeclaredAlias(t *testing.T) {
	vendor := newNarwhal()
	vendor.models = aliasModels()
	vendor.spawnDropAlias = true
	registry := registerNarwhal(t, vendor)

	request := narwhalRequest()
	request.Model = "narwhal"
	_, err := BuildLaunch(context.Background(), registry, request, agentic.LaunchModeExec)
	if !errors.Is(err, ErrVendorContract) {
		t.Fatalf("err = %v, want ErrVendorContract", err)
	}
	if !strings.Contains(err.Error(), "narwhal-deep") {
		t.Errorf("refusal %q does not name the identity the row declares", err)
	}
}

// TestTheAliasReachesArgvThroughAVendorPlugin proves the two layers are wired
// to each other, not just each to itself: Model.Launchable must carry AliasOf
// across the boundary or BuildPlan has nothing to substitute.
func TestTheAliasReachesArgvThroughAVendorPlugin(t *testing.T) {
	vendor := newNarwhal()
	vendor.models = aliasModels()
	registry := registerNarwhal(t, vendor)

	request := narwhalRequest()
	request.Model = "narwhal"
	plan, err := BuildLaunch(context.Background(), registry, request, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	for _, argument := range plan.Argv {
		if argument == "narwhal" {
			t.Fatalf("the alias spelling reached argv: %v", plan.Argv)
		}
	}
	want := agentic.ModelIdentity{Requested: "narwhal", Launched: "narwhal-deep"}
	if plan.ModelIdentity != want {
		t.Errorf("ModelIdentity = %#v, want %#v", plan.ModelIdentity, want)
	}
}
