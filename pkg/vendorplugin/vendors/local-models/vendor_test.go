package localmodels

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// fakePiSystem is the minimal agentic.System double this package's own tests
// need to register the "pi" system id — this package must never depend on
// the real pkg/agentic/systems/pi (that would be the wrong dependency
// direction: the pi system depends on this vendor's identity, not the other
// way round).
type fakePiSystem struct{}

func (fakePiSystem) ID() agentic.SystemID { return "pi" }
func (fakePiSystem) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{LaunchModes: []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun}}
}
func (fakePiSystem) ResolveBinary(agentic.LaunchRequest) (string, error) { return "agents-infra", nil }
func (fakePiSystem) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return nil, nil
}
func (fakePiSystem) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	return parent, nil
}
func (fakePiSystem) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}
func (fakePiSystem) ValidateComposition(agentic.Composition) error { return nil }

func registryWithPi(t *testing.T) *vendorplugin.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(fakePiSystem{}); err != nil {
		t.Fatalf("registering the fake pi system: %v", err)
	}
	return vendorplugin.NewRegistry(systems)
}

func validConfig() Config {
	return Config{Runtimes: []RuntimeEntry{
		{
			ID:     "local-qwen",
			System: "pi",
			Models: map[vendorplugin.ModelID]ModelEntry{
				"qwen-3.8-27b-mlx-8bit": {
					Description:         "Local Qwen3.8-27B, MLX 8-bit",
					Publisher:           "alibaba",
					Family:              "qwen",
					Lifecycle:           vendorplugin.LifecycleCurrent,
					ContextWindowTokens: 131072,
					EffortSupport:       agentic.EffortSupportNone,
					Pointer: Pointer{
						AgentsInfraProject: "/Users/op/skill-agents-management",
						AgentsInfraProfile: "local-qwen",
					},
				},
			},
		},
	}}
}

// twoRuntimeConfig is case 15/16's fixture: two declared RuntimeIDs sharing
// one (System, Vendor) pair, with a pointer under only ONE of them.
func twoRuntimeConfig() Config {
	cfg := validConfig()
	cfg.Runtimes = append(cfg.Runtimes, RuntimeEntry{
		ID:     "local-test-profile",
		System: "pi",
		Models: map[vendorplugin.ModelID]ModelEntry{}, // no pointer for qwen-3.8-27b-mlx-8bit under this runtime
	})
	return cfg
}

// TestRegisterRefusesAGenuinelyEmptyCatalog is case 4(d)'s loud half: a
// valid, non-absent, non-malformed Config that declares zero models is a
// real configuration error, refused by Registry.Register's own
// unconditional ErrNoModels — never silently folded into a permissive
// branch.
func TestRegisterRefusesAGenuinelyEmptyCatalog(t *testing.T) {
	registry := registryWithPi(t)
	vendor := New(Config{Runtimes: []RuntimeEntry{{ID: "local-qwen", System: "pi"}}})
	err := registry.Register(vendor)
	if !errors.Is(err, vendorplugin.ErrNoModels) {
		t.Fatalf("Register(zero-model config) = %v, want ErrNoModels", err)
	}
}

// TestRegisterAValidCatalogSucceeds is case 4(c)'s admission half.
func TestRegisterAValidCatalogSucceeds(t *testing.T) {
	registry := registryWithPi(t)
	vendor := New(validConfig())
	if err := registry.Register(vendor); err != nil {
		t.Fatalf("Register(valid config): %v", err)
	}
	models := vendor.Models()
	if len(models) != 1 || models[0].ID != "qwen-3.8-27b-mlx-8bit" {
		t.Fatalf("Models() = %+v, want exactly the one declared row", models)
	}
	if models[0].Publisher != "alibaba" || models[0].Family != "qwen" {
		t.Fatalf("Publisher/Family = %q/%q, want alibaba/qwen (provenance only, never gates anything)", models[0].Publisher, models[0].Family)
	}
}

// TestModelsIsAStableSnapshot pins Models()'s call-stability contract: two
// calls return an identical, independently-mutable snapshot.
func TestModelsIsAStableSnapshot(t *testing.T) {
	vendor := New(validConfig())
	first := vendor.Models()
	first[0].Systems[0] = "mutated"
	second := vendor.Models()
	if second[0].Systems[0] == "mutated" {
		t.Fatal("mutating one Models() call's slice affected a later call; Models() is not returning independent copies")
	}
}

// TestSpawnFillsAbsentProfileAndEnv is case 22(a).
func TestSpawnFillsAbsentProfileAndEnv(t *testing.T) {
	vendor := New(validConfig())
	runtime := vendorplugin.Runtime{ID: "local-qwen", SystemID: "pi", VendorID: VendorID, Vendor: vendor}
	model := vendor.Models()[0]

	run := agentic.RunContext{RunID: "RUN-1", TaskID: "TASK-1", BoardDir: "/work/.task-board", ContextID: "CTX-1"}
	launch, err := vendor.Spawn(vendorplugin.SpawnContext{
		Runtime: runtime,
		Model:   model,
		Request: vendorplugin.SpawnRequest{Env: []string{"PATH=/usr/bin"}, Run: run},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if launch.Profile != "local-qwen" {
		t.Fatalf("Profile = %q, want local-qwen", launch.Profile)
	}
	wantEnv := "AGENTS_INFRA_CALLER_CWD=/Users/op/skill-agents-management"
	if len(launch.Env) != 2 || launch.Env[0] != "PATH=/usr/bin" || launch.Env[1] != wantEnv {
		t.Fatalf("Env = %v, want [PATH=/usr/bin, %s]", launch.Env, wantEnv)
	}
	if launch.Runtime != "" {
		t.Fatalf("Runtime = %q, want empty — Spawn itself never sets it (BuildLaunch's own job)", launch.Runtime)
	}
	if launch.Run != run {
		t.Fatalf("Run = %#v, want typed caller context %#v", launch.Run, run)
	}
}

// TestSpawnAcceptsAnIdenticalPresentValue is case 22(b): both fields
// present and identical are accepted unchanged, with no second Env entry
// appended.
func TestSpawnAcceptsAnIdenticalPresentValue(t *testing.T) {
	vendor := New(validConfig())
	runtime := vendorplugin.Runtime{ID: "local-qwen", SystemID: "pi", VendorID: VendorID, Vendor: vendor}
	model := vendor.Models()[0]

	launch, err := vendor.Spawn(vendorplugin.SpawnContext{
		Runtime: runtime,
		Model:   model,
		Request: vendorplugin.SpawnRequest{
			Profile: "local-qwen",
			Env:     []string{"AGENTS_INFRA_CALLER_CWD=/Users/op/skill-agents-management"},
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(launch.Env) != 1 {
		t.Fatalf("Env = %v, want exactly the one caller-supplied entry with nothing appended", launch.Env)
	}
}

// TestSpawnRefusesAConflictingProfile is case 22(c).
func TestSpawnRefusesAConflictingProfile(t *testing.T) {
	vendor := New(validConfig())
	runtime := vendorplugin.Runtime{ID: "local-qwen", SystemID: "pi", VendorID: VendorID, Vendor: vendor}
	model := vendor.Models()[0]

	_, err := vendor.Spawn(vendorplugin.SpawnContext{
		Runtime: runtime,
		Model:   model,
		Request: vendorplugin.SpawnRequest{Profile: "some-other-profile"},
	})
	if !errors.Is(err, ErrProfileConflict) {
		t.Fatalf("Spawn(conflicting profile) = %v, want ErrProfileConflict", err)
	}
}

// TestSpawnRefusesAConflictingCallerCWD is case 22(d).
func TestSpawnRefusesAConflictingCallerCWD(t *testing.T) {
	vendor := New(validConfig())
	runtime := vendorplugin.Runtime{ID: "local-qwen", SystemID: "pi", VendorID: VendorID, Vendor: vendor}
	model := vendor.Models()[0]

	_, err := vendor.Spawn(vendorplugin.SpawnContext{
		Runtime: runtime,
		Model:   model,
		Request: vendorplugin.SpawnRequest{Env: []string{"AGENTS_INFRA_CALLER_CWD=/somewhere/else"}},
	})
	if !errors.Is(err, ErrCallerCWDConflict) {
		t.Fatalf("Spawn(conflicting env) = %v, want ErrCallerCWDConflict", err)
	}
}

// TestSpawnPreservesUnrelatedEnv is case 22(e).
func TestSpawnPreservesUnrelatedEnv(t *testing.T) {
	vendor := New(validConfig())
	runtime := vendorplugin.Runtime{ID: "local-qwen", SystemID: "pi", VendorID: VendorID, Vendor: vendor}
	model := vendor.Models()[0]

	launch, err := vendor.Spawn(vendorplugin.SpawnContext{
		Runtime: runtime,
		Model:   model,
		Request: vendorplugin.SpawnRequest{Env: []string{"PATH=/usr/bin", "HOME=/home/op"}},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if launch.Env[0] != "PATH=/usr/bin" || launch.Env[1] != "HOME=/home/op" {
		t.Fatalf("Env = %v; unrelated entries must survive verbatim", launch.Env)
	}
}

// TestSpawnRefusesACrossProfileModel is case 16(b): a runtime with no
// pointer for the requested model is refused by THIS vendor's own
// per-runtime config scope, never falling back to another runtime's
// pointer.
func TestSpawnRefusesACrossProfileModel(t *testing.T) {
	vendor := New(twoRuntimeConfig())
	// local-test-profile shares (System, Vendor) with local-qwen (case 15's
	// premise) but has no pointer for this model.
	runtime := vendorplugin.Runtime{ID: "local-test-profile", SystemID: "pi", VendorID: VendorID, Vendor: vendor}
	model := vendor.Models()[0] // whichever model Models() returns; both configs declare only one ID

	_, err := vendor.Spawn(vendorplugin.SpawnContext{Runtime: runtime, Model: model})
	if !errors.Is(err, ErrNoLocalPointer) {
		t.Fatalf("Spawn(cross-profile model) = %v, want ErrNoLocalPointer", err)
	}
}
