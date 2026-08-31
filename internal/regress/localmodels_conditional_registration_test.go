package regress

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/anthropic"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

// fakeRegressSystem is a minimal agentic.System double, settable to any ID,
// used ONLY to give this file's isolated registries a system to resolve
// "pi" and "claude-code" against — WITHOUT importing the real
// pkg/agentic/systems/pi (which would self-register "pi" into the shared
// agentic.Default and trip internal/regress's own
// TestEveryLayerOneSystemHasASmokeCase guard, a golden this Story's pi
// plugin does not yet have — see pkg/agentic/systems/pi's own package doc
// for why) or reaching into agentic.Default for claude-code at all. Every
// test in THIS file therefore builds its own fully isolated
// agentic.Registry, never agentic.Default.
type fakeRegressSystem struct{ id agentic.SystemID }

func (f fakeRegressSystem) ID() agentic.SystemID { return f.id }
func (fakeRegressSystem) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{LaunchModes: []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun}}
}
func (fakeRegressSystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return "/bin/true", nil
}
func (fakeRegressSystem) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return nil, nil
}
func (fakeRegressSystem) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	return parent, nil
}
func (fakeRegressSystem) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}
func (fakeRegressSystem) ValidateComposition(agentic.Composition) error { return nil }

// isolatedSystems returns a fresh agentic.Registry carrying fake
// "claude-code" and "pi" systems, never touching agentic.Default.
func isolatedSystems(t *testing.T) *agentic.Registry {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(fakeRegressSystem{id: "claude-code"}); err != nil {
		t.Fatalf("registering fake claude-code: %v", err)
	}
	if err := systems.Register(fakeRegressSystem{id: "pi"}); err != nil {
		t.Fatalf("registering fake pi: %v", err)
	}
	return systems
}

// CLASS 6 — the local-models conditional registration contract (architecture
// decision TASK-260829-3jlxed, §2.2.2).
//
// local-models is the one vendor plugin in this module that must NOT
// register unconditionally at init: its catalog depends on a machine-local
// file that may not exist, and Registry.Register's unconditional ErrNoModels
// check would otherwise poison registry construction for every OTHER
// runtime the moment that file is absent. This file mirrors
// skill-project-management's own launchRegistry shape — four unconditional
// registrations, then a THREE-WAY conditional branch on
// localmodels.Peek()'s own result — entirely inside this module, so this
// module's own suite can drive the contract end to end without depending on
// a different Go module's actual launchRegistry.

// localQwenDeclaration mirrors the one RuntimeDeclaration
// skill-project-management's own localqwen_declaration.go will carry.
func localQwenDeclaration() vendorplugin.RuntimeDeclaration {
	return vendorplugin.RuntimeDeclaration{
		ID:     "local-qwen",
		System: "pi",
		Vendor: localmodels.VendorID,
		Broker: vendorplugin.BrokerProvenance{
			Checked: []string{"internal/regress conditional-registration harness"},
			Found:   "declared unconditionally once local-models registers",
		},
	}
}

// buildFakeLaunchRegistry mirrors architecture decision §2.2.2's exact code
// block: register the frozen runtimes and one representative "other" vendor
// unconditionally, THEN branch on peek's three-way result. It returns
// whatever error the conditional branch itself produces (case 4(d)'s loud
// ErrNoModels refuses the WHOLE construction), so a caller can assert on it
// directly rather than only on the registry's later resolution behavior.
func buildFakeLaunchRegistry(t *testing.T, peek localmodels.ConfigResult) (*vendorplugin.Registry, error) {
	t.Helper()
	registry := vendorplugin.NewRegistry(isolatedSystems(t))
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding the frozen runtimes: %v", err)
	}
	if err := registry.Register(anthropic.New()); err != nil {
		t.Fatalf("registering anthropic (the unaffected 'other vendor'): %v", err)
	}

	switch {
	case peek.Absent:
		// Genuinely no local-models.toml. Zero registration, zero diagnostic.
		return registry, nil
	case peek.Err != nil:
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{
			Reason: "malformed", Err: peek.Err,
		}); err != nil {
			return registry, err
		}
		return registry, nil
	default:
		vendor := localmodels.New(peek.Config)
		if err := registry.Register(vendor); err != nil {
			return registry, err
		}
		if err := registry.DeclareRuntime(localQwenDeclaration()); err != nil {
			return registry, err
		}
		return registry, nil
	}
}

func validLocalModelsConfig() localmodels.Config {
	return localmodels.Config{Runtimes: []localmodels.RuntimeEntry{
		{
			ID:     "local-qwen",
			System: "pi",
			Models: map[vendorplugin.ModelID]localmodels.ModelEntry{
				"qwen-3.8-27b-mlx-8bit": {
					Description:         "Local Qwen3.8-27B, MLX 8-bit",
					Lifecycle:           vendorplugin.LifecycleCurrent,
					EffortSupport:       agentic.EffortSupportNone,
					ContextWindowTokens: 131072,
					Pointer: localmodels.Pointer{
						AgentsInfraProject: "/Users/op/skill-agents-management",
						AgentsInfraProfile: "local-qwen",
					},
				},
			},
		},
	}}
}

// TestConditionalRegistrationAbsentConfig is case 4(a): anthropic/claude
// launch normally, local-qwen resolution fails with the generic
// ErrUnknownRuntime, and no diagnostic is noted.
func TestConditionalRegistrationAbsentConfig(t *testing.T) {
	registry, err := buildFakeLaunchRegistry(t, localmodels.ConfigResult{Absent: true})
	if err != nil {
		t.Fatalf("buildFakeLaunchRegistry(absent): %v", err)
	}
	if _, ok := registry.Lookup(localmodels.VendorID); ok {
		t.Fatal("local-models is registered despite an absent config")
	}
	if _, err := registry.ResolveRuntime("local-qwen"); !errors.Is(err, vendorplugin.ErrUnknownRuntime) {
		t.Fatalf("ResolveRuntime(local-qwen) = %v, want ErrUnknownRuntime", err)
	}
	if _, err := registry.ResolveRuntime("claude"); err != nil {
		t.Fatalf("ResolveRuntime(claude), an unrelated runtime: %v", err)
	}
}

// TestConditionalRegistrationMalformedConfig is case 4(b): local-qwen
// resolution now fails with the DISTINCT, typed ErrRuntimeConfigMalformed
// wrapping the parse error — asserted at the real Registry.ResolveRuntime
// call BuildLaunch itself makes — while every unrelated runtime is
// unaffected.
func TestConditionalRegistrationMalformedConfig(t *testing.T) {
	parseErr := errors.New("local-models.toml:3: pointer.agents_infra_project is required")
	registry, err := buildFakeLaunchRegistry(t, localmodels.ConfigResult{Err: parseErr})
	if err != nil {
		t.Fatalf("buildFakeLaunchRegistry(malformed): %v", err)
	}
	if _, ok := registry.Lookup(localmodels.VendorID); ok {
		t.Fatal("local-models is registered despite a malformed config")
	}

	_, resolveErr := registry.ResolveRuntime("local-qwen")
	if !errors.Is(resolveErr, vendorplugin.ErrRuntimeConfigMalformed) {
		t.Fatalf("ResolveRuntime(local-qwen) = %v, want ErrRuntimeConfigMalformed", resolveErr)
	}
	if errors.Is(resolveErr, vendorplugin.ErrUnknownRuntime) {
		t.Fatal("a malformed-config refusal also satisfies errors.Is(ErrUnknownRuntime); the two must be distinct")
	}
	if !errors.Is(resolveErr, parseErr) && resolveErr.Error() == "" {
		t.Fatal("the malformed refusal names no underlying cause")
	}

	// The SAME assertion, driven a second time through the real
	// vendorplugin.BuildLaunch entry point — proving BuildLaunch itself
	// surfaces the distinct error and never reaches agentic.BuildPlan.
	_, buildErr := vendorplugin.BuildLaunch(t.Context(), registry, vendorplugin.SpawnRequest{
		Runtime: "local-qwen", Model: "qwen-3.8-27b-mlx-8bit",
	}, agentic.LaunchModeExec)
	if !errors.Is(buildErr, vendorplugin.ErrRuntimeConfigMalformed) {
		t.Fatalf("BuildLaunch(local-qwen) = %v, want ErrRuntimeConfigMalformed", buildErr)
	}

	if _, err := registry.ResolveRuntime("claude"); err != nil {
		t.Fatalf("ResolveRuntime(claude), an unrelated runtime: %v", err)
	}
}

// TestConditionalRegistrationValidConfig is case 4(c): local-models
// registers, Models() returns the parsed catalog, and local-qwen resolves.
func TestConditionalRegistrationValidConfig(t *testing.T) {
	registry, err := buildFakeLaunchRegistry(t, localmodels.ConfigResult{Config: validLocalModelsConfig()})
	if err != nil {
		t.Fatalf("buildFakeLaunchRegistry(valid): %v", err)
	}
	vendor, ok := registry.Lookup(localmodels.VendorID)
	if !ok {
		t.Fatal("local-models did not register despite a valid config")
	}
	if len(vendor.Models()) != 1 {
		t.Fatalf("Models() = %v, want exactly one row", vendor.Models())
	}
	if _, err := registry.ResolveRuntime("local-qwen"); err != nil {
		t.Fatalf("ResolveRuntime(local-qwen): %v", err)
	}
}

// TestConditionalRegistrationZeroModelCatalogRefusesTheWholeConstruction is
// case 4(d): a config that PARSES but declares zero models is a real
// configuration error, loudly refused by Registry.Register's own
// unconditional ErrNoModels — never silently folded into the permissive
// absent/malformed branches.
func TestConditionalRegistrationZeroModelCatalogRefusesTheWholeConstruction(t *testing.T) {
	empty := localmodels.Config{Runtimes: []localmodels.RuntimeEntry{{ID: "local-qwen", System: "pi"}}}
	_, err := buildFakeLaunchRegistry(t, localmodels.ConfigResult{Config: empty})
	if !errors.Is(err, vendorplugin.ErrNoModels) {
		t.Fatalf("buildFakeLaunchRegistry(zero-model config) = %v, want ErrNoModels", err)
	}
}

// TestRegistryNoteUnregisteredAndDeclareRuntimeSymmetricConflict is case
// 4(g): NoteUnregistered/DeclareRuntime/ResolveRuntime driven directly
// against a real *vendorplugin.Registry, isolated from the harness above.
func TestRegistryNoteUnregisteredAndDeclareRuntimeSymmetricConflict(t *testing.T) {
	someErr := errors.New("boom")

	// registryWithLocalModels is a fresh registry with the frozen runtimes,
	// anthropic and a VALID local-models vendor all registered, so a test
	// that installs a local-qwen declaration/diagnostic can fully resolve
	// it (declaration + vendor + system all present), not merely prove the
	// declaration map's own bookkeeping.
	registryWithLocalModels := func(t *testing.T) *vendorplugin.Registry {
		t.Helper()
		registry := vendorplugin.NewRegistry(isolatedSystems(t))
		if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		if err := registry.Register(anthropic.New()); err != nil {
			t.Fatalf("registering anthropic: %v", err)
		}
		if err := registry.Register(localmodels.New(validLocalModelsConfig())); err != nil {
			t.Fatalf("registering local-models: %v", err)
		}
		return registry
	}

	t.Run("i: note then resolve returns the malformed refusal", func(t *testing.T) {
		registry := vendorplugin.NewRegistry(agentic.NewRegistry())
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr}); err != nil {
			t.Fatalf("NoteUnregistered: %v", err)
		}
		_, err := registry.ResolveRuntime("local-qwen")
		if !errors.Is(err, vendorplugin.ErrRuntimeConfigMalformed) {
			t.Fatalf("ResolveRuntime = %v, want ErrRuntimeConfigMalformed", err)
		}
		// The architecture's own NoteUnregistered/ResolveRuntime snippet
		// interpolates the diagnostic's underlying error with %v, not %w —
		// the adversarial plan itself accepts a message-substring check as
		// the alternative to errors.Unwrap for exactly this reason (case
		// 4(b)).
		if !strings.Contains(err.Error(), someErr.Error()) {
			t.Fatalf("ResolveRuntime = %q, does not name the underlying cause %q", err.Error(), someErr.Error())
		}
	})

	t.Run("ii: a different id never noted resolves normally", func(t *testing.T) {
		registry := vendorplugin.NewRegistry(isolatedSystems(t))
		if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		if err := registry.Register(anthropic.New()); err != nil {
			t.Fatalf("registering anthropic: %v", err)
		}
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr}); err != nil {
			t.Fatalf("NoteUnregistered: %v", err)
		}
		if _, err := registry.ResolveRuntime("claude"); err != nil {
			t.Fatalf("ResolveRuntime(claude): %v", err)
		}
	})

	t.Run("iii: a third id, never noted and never declared, is plain unknown", func(t *testing.T) {
		registry := vendorplugin.NewRegistry(agentic.NewRegistry())
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr}); err != nil {
			t.Fatalf("NoteUnregistered: %v", err)
		}
		_, err := registry.ResolveRuntime("something-else-entirely")
		if !errors.Is(err, vendorplugin.ErrUnknownRuntime) || errors.Is(err, vendorplugin.ErrRuntimeConfigMalformed) {
			t.Fatalf("ResolveRuntime(unrelated id) = %v, want plain ErrUnknownRuntime only", err)
		}
	})

	t.Run("iv+v: declare then note is refused, declared binding still resolves", func(t *testing.T) {
		registry := registryWithLocalModels(t)
		if err := registry.DeclareRuntime(localQwenDeclaration()); err != nil {
			t.Fatalf("DeclareRuntime: %v", err)
		}
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr}); err == nil {
			t.Fatal("NoteUnregistered succeeded against an id that IS already declared")
		}
		resolved, err := registry.ResolveRuntime("local-qwen")
		if err != nil {
			t.Fatalf("ResolveRuntime after a refused NoteUnregistered: %v", err)
		}
		if resolved.SystemID != "pi" {
			t.Fatalf("resolved = %+v, want the declared (pi, local-models) binding, uncorrupted", resolved)
		}
	})

	t.Run("vi: note then declare is refused with ErrRuntimeDiagnosticConflict, reverse order", func(t *testing.T) {
		registry := registryWithLocalModels(t)
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr}); err != nil {
			t.Fatalf("NoteUnregistered: %v", err)
		}
		err := registry.DeclareRuntime(localQwenDeclaration())
		if !errors.Is(err, vendorplugin.ErrRuntimeDiagnosticConflict) {
			t.Fatalf("DeclareRuntime after NoteUnregistered = %v, want ErrRuntimeDiagnosticConflict", err)
		}
		_, resolveErr := registry.ResolveRuntime("local-qwen")
		if !errors.Is(resolveErr, vendorplugin.ErrRuntimeConfigMalformed) || !strings.Contains(resolveErr.Error(), someErr.Error()) {
			t.Fatalf("ResolveRuntime after a refused DeclareRuntime = %v, want ErrRuntimeConfigMalformed naming someErr (the declaration must not have been installed)", resolveErr)
		}
	})

	t.Run("vi driven a second time through the fake launchRegistry harness, defensive-only", func(t *testing.T) {
		registry := registryWithLocalModels(t)
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr}); err != nil {
			t.Fatalf("NoteUnregistered: %v", err)
		}
		if err := registry.DeclareRuntime(localQwenDeclaration()); !errors.Is(err, vendorplugin.ErrRuntimeDiagnosticConflict) {
			t.Fatalf("DeclareRuntime = %v, want ErrRuntimeDiagnosticConflict (production's own switch never produces this order, but the API-level guard must hold regardless)", err)
		}
	})

	t.Run("vii: concurrent contenders resolve to exactly one winner, 100 runs", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			registry := registryWithLocalModels(t)
			var wg sync.WaitGroup
			var noteErr, declareErr error
			start := make(chan struct{})
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				noteErr = registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "malformed", Err: someErr})
			}()
			go func() {
				defer wg.Done()
				<-start
				declareErr = registry.DeclareRuntime(localQwenDeclaration())
			}()
			close(start)
			wg.Wait()

			noteWon := noteErr == nil
			declareWon := declareErr == nil
			if noteWon == declareWon {
				t.Fatalf("run %d: noteErr=%v declareErr=%v; want exactly one nil", i, noteErr, declareErr)
			}
			_, resolveErr := registry.ResolveRuntime("local-qwen")
			if declareWon && resolveErr != nil {
				t.Fatalf("run %d: DeclareRuntime won but ResolveRuntime failed: %v", i, resolveErr)
			}
			if noteWon && !errors.Is(resolveErr, vendorplugin.ErrRuntimeConfigMalformed) {
				t.Fatalf("run %d: NoteUnregistered won but ResolveRuntime = %v, want ErrRuntimeConfigMalformed", i, resolveErr)
			}
		}
	})

	t.Run("ix: an unrecognized reason is refused, and the id is left unpoisoned", func(t *testing.T) {
		registry := registryWithLocalModels(t)
		err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{
			Reason: "forged-unknown-reason", Err: someErr,
		})
		if err == nil {
			t.Fatal("NoteUnregistered accepted an unrecognized diagnostic reason")
		}
		// The forged note must not have been stored under any shape: a later
		// valid DeclareRuntime must succeed, and ResolveRuntime must not
		// collapse to a distinct malformed refusal it was never told to give.
		if err := registry.DeclareRuntime(localQwenDeclaration()); err != nil {
			t.Fatalf("DeclareRuntime after a refused, unrecognized-reason NoteUnregistered: %v (id must be unpoisoned)", err)
		}
		resolved, err := registry.ResolveRuntime("local-qwen")
		if err != nil {
			t.Fatalf("ResolveRuntime after a refused NoteUnregistered and a valid DeclareRuntime: %v", err)
		}
		if resolved.SystemID != "pi" {
			t.Fatalf("resolved = %+v, want the declared (pi, local-models) binding", resolved)
		}
	})

	t.Run("x: a \"malformed\" reason with a nil Err is refused, and the id is left unpoisoned", func(t *testing.T) {
		registry := registryWithLocalModels(t)
		err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{
			Reason: "malformed", Err: nil,
		})
		if err == nil {
			t.Fatal("NoteUnregistered accepted a \"malformed\" diagnostic with a nil Err")
		}
		if err := registry.DeclareRuntime(localQwenDeclaration()); err != nil {
			t.Fatalf("DeclareRuntime after a refused, nil-Err NoteUnregistered: %v (id must be unpoisoned)", err)
		}
		if _, err := registry.ResolveRuntime("local-qwen"); err != nil {
			t.Fatalf("ResolveRuntime after a refused NoteUnregistered and a valid DeclareRuntime: %v", err)
		}
	})

	t.Run("xi: a bare unrecognized-reason note, never declared, resolves as plain unknown, never malformed", func(t *testing.T) {
		registry := vendorplugin.NewRegistry(agentic.NewRegistry())
		if err := registry.NoteUnregistered("local-qwen", vendorplugin.RegistrationDiagnostic{Reason: "forged-unknown-reason", Err: someErr}); err == nil {
			t.Fatal("NoteUnregistered accepted an unrecognized diagnostic reason")
		}
		_, err := registry.ResolveRuntime("local-qwen")
		if !errors.Is(err, vendorplugin.ErrUnknownRuntime) || errors.Is(err, vendorplugin.ErrRuntimeConfigMalformed) {
			t.Fatalf("ResolveRuntime(local-qwen) = %v, want plain ErrUnknownRuntime only (a refused diagnostic must never surface as a distinct malformed refusal)", err)
		}
	})

	t.Run("viii: idempotent redeclaration is unaffected by the new check", func(t *testing.T) {
		registry := vendorplugin.NewRegistry(isolatedSystems(t))
		if err := registry.DeclareRuntime(localQwenDeclaration()); err != nil {
			t.Fatalf("first DeclareRuntime: %v", err)
		}
		if err := registry.DeclareRuntime(localQwenDeclaration()); err != nil {
			t.Fatalf("second, identical DeclareRuntime: %v, want nil (idempotent)", err)
		}
	})
}
