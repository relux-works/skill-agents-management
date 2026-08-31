package mlx_test

import (
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine/engines/mlx"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

func TestPluginDeclaresTheStableMLXIdentity(t *testing.T) {
	declaration := mlx.New().PluginDeclaration()
	want := plugin.Declaration{ID: "mlx", Kind: inferenceengine.Kind}
	if declaration.ID != want.ID || declaration.Kind != want.Kind || len(declaration.Dependencies) != 0 {
		t.Fatalf("MLX declaration = %#v, want %#v", declaration, want)
	}
}

func TestPluginRegistersAndResolvesThroughTheGenericGraph(t *testing.T) {
	registry := plugin.NewRegistry()
	if err := registry.Register(mlx.New()); err != nil {
		t.Fatalf("Register(mlx): %v", err)
	}
	resolution, err := registry.Resolve(mlx.ID)
	if err != nil {
		t.Fatalf("Resolve(mlx): %v", err)
	}
	if _, ok := resolution.Plugin.(mlx.Plugin); !ok {
		t.Fatalf("resolved plugin = %T, want mlx.Plugin", resolution.Plugin)
	}
}
