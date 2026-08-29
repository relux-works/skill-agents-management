package inferenceengine_test

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type llamaEngine struct{}

func (llamaEngine) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: "llama-cpp", Kind: inferenceengine.Kind}
}

func TestInferenceEngineIsANewKindTheRegistryDidNotNeedToLearn(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(llamaEngine{}); err != nil {
		t.Fatalf("Register(inference engine): %v", err)
	}
	declaration, ok := registry.Declaration("llama-cpp")
	if !ok {
		t.Fatal("the inference engine registered without error and cannot be resolved")
	}
	if declaration.Kind != inferenceengine.Kind {
		t.Fatalf("declaration.Kind = %q, want %q", declaration.Kind, inferenceengine.Kind)
	}
}

func TestNewPlanNodeTypesTheEngineProcess(t *testing.T) {
	node, err := inferenceengine.NewPlanNode(llamaEngine{}, agentic.ProcessPlan{
		Mode:   agentic.LaunchModeExec,
		Binary: "/opt/bin/llama-server",
		Argv:   []string{"--model", "weights.gguf"},
	})
	if err != nil {
		t.Fatalf("NewPlanNode: %v", err)
	}
	if node.ID != "llama-cpp" || node.Plugin.Kind != inferenceengine.Kind {
		t.Fatalf("node = %#v, want a typed llama-cpp inference-engine node", node)
	}
}

func TestConfiguredEngineUsesTheGenericGraphRefusals(t *testing.T) {
	t.Run("malformed identity", func(t *testing.T) {
		registry := plugin.NewRegistry()
		err := registry.Register(inferenceengine.NewConfigured(" MLX "))
		if !errors.Is(err, plugin.ErrInvalidDeclaration) {
			t.Fatalf("Register(malformed engine) = %v, want ErrInvalidDeclaration", err)
		}
	})

	t.Run("duplicate identity", func(t *testing.T) {
		registry := plugin.NewRegistry()
		if err := registry.Register(inferenceengine.NewConfigured("mlx")); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		err := registry.Register(inferenceengine.NewConfigured("mlx"))
		if !errors.Is(err, plugin.ErrDuplicatePlugin) {
			t.Fatalf("second Register = %v, want ErrDuplicatePlugin", err)
		}
	})

	t.Run("cycle", func(t *testing.T) {
		registry := plugin.NewRegistry()
		a := inferenceengine.NewConfigured("engine-a", plugin.Ref{ID: "engine-b", Kind: inferenceengine.Kind})
		b := inferenceengine.NewConfigured("engine-b", plugin.Ref{ID: "engine-a", Kind: inferenceengine.Kind})
		err := registry.RegisterAll(a, b)
		if !errors.Is(err, plugin.ErrDependencyCycle) {
			t.Fatalf("RegisterAll(cycle) = %v, want ErrDependencyCycle", err)
		}
	})
}
