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

type unstableDeclaredPlugin struct {
	declarations []plugin.Declaration
	reads        int
}

func (p *unstableDeclaredPlugin) PluginDeclaration() plugin.Declaration {
	declaration := p.declarations[p.reads]
	if p.reads < len(p.declarations)-1 {
		p.reads++
	}
	return declaration
}

func TestRegisterRefusesTypedNilPlugin(t *testing.T) {
	registry := plugin.NewRegistry()
	var typedNil *declaredPlugin

	err := registry.Register(typedNil)
	if !errors.Is(err, plugin.ErrNilPlugin) {
		t.Fatalf("Register(typed nil plugin) error = %v, want ErrNilPlugin", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after typed nil refusal, want zero", registry.Len())
	}
}

func TestNilRegistryRefusesRegistration(t *testing.T) {
	var registry *plugin.Registry

	err := registry.Register(node("inference-engine", "engine"))
	if err == nil {
		t.Fatal("Register into a nil registry returned nil")
	}
}

func TestRegisterAllRefusesUnnormalizedDependencyDeclaration(t *testing.T) {
	registry := plugin.NewRegistry()
	engine := node("inference-engine", "engine")
	environment := node("agent-environment", "worktree",
		plugin.Ref{ID: "Engine", Kind: "inference-engine"},
	)

	err := registry.RegisterAll(engine, environment)
	if !errors.Is(err, plugin.ErrInvalidDeclaration) {
		t.Fatalf("RegisterAll(unnormalized dependency id) error = %v, want ErrInvalidDeclaration", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after invalid declaration refusal, want zero", registry.Len())
	}
}

func TestRegisterAllRefusesDependencyChangesAcrossDeclarationReads(t *testing.T) {
	registry := plugin.NewRegistry()
	unstable := &unstableDeclaredPlugin{declarations: []plugin.Declaration{
		{
			ID:   "worktree",
			Kind: "agent-environment",
			Dependencies: []plugin.Ref{
				{ID: "engine", Kind: "inference-engine"},
			},
		},
		{ID: "worktree", Kind: "agent-environment"},
	}}

	err := registry.RegisterAll(node("inference-engine", "engine"), unstable)
	if !errors.Is(err, plugin.ErrUnstableDeclaration) {
		t.Fatalf("RegisterAll(plugin changing dependencies) error = %v, want ErrUnstableDeclaration", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after unstable declaration refusal, want zero", registry.Len())
	}
}

func TestRegisterAllRefusesRepeatedNonSelfDependency(t *testing.T) {
	registry := plugin.NewRegistry()
	environment := node("agent-environment", "worktree",
		plugin.Ref{ID: "engine", Kind: "inference-engine"},
		plugin.Ref{ID: "engine", Kind: "inference-engine"},
	)

	err := registry.RegisterAll(node("inference-engine", "engine"), environment)
	if !errors.Is(err, plugin.ErrDuplicateDependency) {
		t.Fatalf("RegisterAll(repeated non-self dependency) error = %v, want ErrDuplicateDependency", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after repeated dependency refusal, want zero", registry.Len())
	}
}

func TestResolveRefusesMissingPluginInNonEmptyRegistry(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(node("inference-engine", "engine")); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}

	_, err := registry.Resolve("missing-engine")
	if !errors.Is(err, plugin.ErrPluginNotRegistered) {
		t.Fatalf("Resolve(missing plugin in non-empty registry) error = %v, want ErrPluginNotRegistered", err)
	}
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

func TestRegisterRefusesALaterMissingDependencyInANonEmptyGraph(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(node("inference-engine", "engine")); err != nil {
		t.Fatalf("Register(engine): %v", err)
	}

	err := registry.Register(node("agent-environment", "worktree",
		plugin.Ref{ID: "engine", Kind: "inference-engine"},
		plugin.Ref{ID: "missing-weights", Kind: "weight-artifact"},
	))
	if !errors.Is(err, plugin.ErrMissingDependency) {
		t.Fatalf("Register(plugin whose later dependency is missing) error = %v, want ErrMissingDependency", err)
	}
	if registry.Len() != 1 {
		t.Fatalf("Len() = %d after refusal, want the pre-existing engine only", registry.Len())
	}
	if _, ok := registry.Lookup("worktree"); ok {
		t.Fatal("the plugin with a later missing dependency was registered")
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

func TestRegisterAllRefusesALaterKindMismatchAgainstABatchNode(t *testing.T) {
	registry := plugin.NewRegistry()
	engine := node("inference-engine", "engine")
	weights := node("weight-artifact", "weights")
	environment := node("agent-environment", "worktree",
		plugin.Ref{ID: "engine", Kind: "inference-engine"},
		plugin.Ref{ID: "weights", Kind: "artifact-store"},
	)

	err := registry.RegisterAll(engine, weights, environment)
	if !errors.Is(err, plugin.ErrUnsatisfiableDeclaration) {
		t.Fatalf("RegisterAll(batch with later kind mismatch) error = %v, want ErrUnsatisfiableDeclaration", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after refused kind-mismatched batch, want zero", registry.Len())
	}
}

func TestRegisterRefusesSameKindDuplicate(t *testing.T) {
	registry := plugin.NewRegistry()
	metrics := node("sidecar", "metrics")
	if err := registry.Register(metrics); err != nil {
		t.Fatalf("first Register(sidecar/metrics): %v", err)
	}

	err := registry.Register(metrics)
	if !errors.Is(err, plugin.ErrDuplicatePlugin) {
		t.Fatalf("second Register(sidecar/metrics) error = %v, want ErrDuplicatePlugin", err)
	}
	if registry.Len() != 1 {
		t.Fatalf("Len() = %d after refused same-kind duplicate, want one", registry.Len())
	}
}

func TestRegisterAllRefusesSameKindDuplicateWithinBatchAtomically(t *testing.T) {
	registry := plugin.NewRegistry()
	metrics := node("sidecar", "metrics")

	err := registry.RegisterAll(metrics, metrics)
	if !errors.Is(err, plugin.ErrDuplicatePlugin) {
		t.Fatalf("RegisterAll(sidecar/metrics twice) error = %v, want ErrDuplicatePlugin", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("Len() = %d after refused duplicate batch, want zero", registry.Len())
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

func TestRegisterAllRefusesSelfAndMultiHopCycles(t *testing.T) {
	tests := map[string][]declaredPlugin{
		"self cycle": {
			node("sidecar", "metrics", plugin.Ref{ID: "metrics", Kind: "sidecar"}),
		},
		"three node cycle": {
			node("sidecar", "a", plugin.Ref{ID: "b", Kind: "sidecar"}),
			node("sidecar", "b", plugin.Ref{ID: "c", Kind: "sidecar"}),
			node("sidecar", "c", plugin.Ref{ID: "a", Kind: "sidecar"}),
		},
	}
	for name, nodes := range tests {
		t.Run(name, func(t *testing.T) {
			registry := plugin.NewRegistry()
			plugins := make([]plugin.Plugin, len(nodes))
			for i := range nodes {
				plugins[i] = nodes[i]
			}
			err := registry.RegisterAll(plugins...)
			if !errors.Is(err, plugin.ErrDependencyCycle) {
				t.Fatalf("RegisterAll(%s) error = %v, want ErrDependencyCycle", name, err)
			}
			if registry.Len() != 0 {
				t.Fatalf("Len() = %d after refused %s, want zero", registry.Len(), name)
			}
		})
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

func TestRegisterAllPreservesExistingGraphWhenANewBatchIsRefused(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(node("artifact-store", "store")); err != nil {
		t.Fatalf("Register(pre-existing store): %v", err)
	}
	valid := node("inference-engine", "engine")
	invalid := node("weight-artifact", "weights",
		plugin.Ref{ID: "missing-store", Kind: "artifact-store"},
	)

	err := registry.RegisterAll(invalid, valid)
	if !errors.Is(err, plugin.ErrMissingDependency) {
		t.Fatalf("RegisterAll(invalid, valid) error = %v, want ErrMissingDependency", err)
	}
	if registry.Len() != 1 {
		t.Fatalf("Len() = %d after refused batch, want the pre-existing store only", registry.Len())
	}
	if _, ok := registry.Lookup("store"); !ok {
		t.Fatal("the refused batch removed the pre-existing graph")
	}
	for _, id := range []plugin.ID{"engine", "weights"} {
		if _, ok := registry.Lookup(id); ok {
			t.Fatalf("the refused batch partially registered %q", id)
		}
	}
}
