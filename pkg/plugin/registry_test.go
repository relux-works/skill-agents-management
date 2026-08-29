package plugin_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type declaredPlugin struct {
	declaration plugin.Declaration
}

func (p declaredPlugin) PluginDeclaration() plugin.Declaration { return p.declaration }

func node(kind plugin.Kind, id plugin.ID, dependencies ...plugin.Ref) declaredPlugin {
	return declaredPlugin{declaration: plugin.Declaration{
		ID:           id,
		Kind:         kind,
		Dependencies: dependencies,
	}}
}

func TestRegisterAllResolvesDeclaredEdgesWithoutKnowingKindDirection(t *testing.T) {
	const (
		vendorKind plugin.Kind = "model-vendor"
		systemKind plugin.Kind = "agentic-system"
	)
	registry := plugin.NewRegistry()

	// This is the reverse of the historical vendor -> system rule. The graph
	// accepts it because the declaration, not a layer number, owns direction.
	vendor := node(vendorKind, "narwhal")
	system := node(systemKind, "pangolin", plugin.Ref{ID: "narwhal", Kind: vendorKind})
	if err := registry.RegisterAll(vendor, system); err != nil {
		t.Fatalf("RegisterAll(vendor, system depending on vendor): %v", err)
	}

	resolved, err := registry.Resolve("pangolin")
	if err != nil {
		t.Fatalf("Resolve(pangolin): %v", err)
	}
	if len(resolved.Dependencies) != 1 || resolved.Dependencies[0].Declaration.ID != "narwhal" {
		t.Fatalf("resolved dependencies = %#v, want narwhal", resolved.Dependencies)
	}
	if got := fmt.Sprint(registry.TopologicalOrder()); got != "[narwhal pangolin]" {
		t.Fatalf("TopologicalOrder() = %s, want dependencies before dependants", got)
	}
}

func TestInferenceEngineCanBeSharedWithoutBreakingVendorToSystemSemantics(t *testing.T) {
	const (
		engineKind plugin.Kind = "inference-engine"
		systemKind plugin.Kind = "agentic-system"
		vendorKind plugin.Kind = "model-vendor"
	)
	registry := plugin.NewRegistry()
	engine := node(engineKind, "llama-cpp")
	system := node(systemKind, "pi",
		plugin.Ref{ID: "llama-cpp", Kind: engineKind},
	)
	vendor := node(vendorKind, "local-models",
		plugin.Ref{ID: "pi", Kind: systemKind},
		plugin.Ref{ID: "llama-cpp", Kind: engineKind},
	)

	if err := registry.RegisterAll(vendor, system, engine); err != nil {
		t.Fatalf("RegisterAll(local-model graph): %v", err)
	}
	if got := fmt.Sprint(registry.TopologicalOrder()); got != "[llama-cpp pi local-models]" {
		t.Fatalf("TopologicalOrder() = %s, want engine, system, vendor", got)
	}
}

func TestRegisterRefusesMissingDependencyAtTheProductionEntryPoint(t *testing.T) {
	registry := plugin.NewRegistry()
	err := registry.Register(node("agent-environment", "worktree",
		plugin.Ref{ID: "missing-engine", Kind: "inference-engine"},
	))
	if !errors.Is(err, plugin.ErrMissingDependency) {
		t.Fatalf("Register(plugin with missing dependency) error = %v, want ErrMissingDependency", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after refusal, want atomic zero", registry.Len())
	}
}

func TestRegisterRefusesDependencyWhoseDeclaredKindCannotBeSatisfied(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(node("model-vendor", "broker")); err != nil {
		t.Fatalf("Register(broker): %v", err)
	}

	err := registry.Register(node("agent-environment", "worktree",
		plugin.Ref{ID: "broker", Kind: "inference-engine"},
	))
	if !errors.Is(err, plugin.ErrUnsatisfiableDeclaration) {
		t.Fatalf("Register(kind-mismatched dependency) error = %v, want ErrUnsatisfiableDeclaration", err)
	}
	if _, ok := registry.Lookup("worktree"); ok {
		t.Fatal("the refused kind-mismatched plugin is in the registry")
	}
}

func TestRegisterAllRefusesCycleAtomically(t *testing.T) {
	registry := plugin.NewRegistry()
	a := node("sidecar", "a", plugin.Ref{ID: "b", Kind: "sidecar"})
	b := node("sidecar", "b", plugin.Ref{ID: "a", Kind: "sidecar"})

	err := registry.RegisterAll(a, b)
	if !errors.Is(err, plugin.ErrDependencyCycle) {
		t.Fatalf("RegisterAll(cycle) error = %v, want ErrDependencyCycle", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after cyclic batch refusal, want atomic zero", registry.Len())
	}
}

func TestRegisterAllRefusesAValidNodeAlongsideAnInvalidOneAtomically(t *testing.T) {
	registry := plugin.NewRegistry()
	valid := node("inference-engine", "llama-cpp")
	invalid := node("weight-artifact", "weights",
		plugin.Ref{ID: "missing-store", Kind: "artifact-store"},
	)

	err := registry.RegisterAll(valid, invalid)
	if !errors.Is(err, plugin.ErrMissingDependency) {
		t.Fatalf("RegisterAll(valid, invalid) error = %v, want ErrMissingDependency", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after refused transaction, want zero", registry.Len())
	}
}
