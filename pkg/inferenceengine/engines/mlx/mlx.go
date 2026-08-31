// Package mlx declares the configured MLX inference-engine plugin.
//
// The plugin is an identity and graph node only. It does not discover, start,
// connect to, or execute a local model runtime.
package mlx

import (
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// ID is the stable configured inference-engine identity.
const ID plugin.ID = "mlx"

// Plugin is the concrete MLX graph plugin. Its zero value is valid.
type Plugin struct{}

// New returns a concrete MLX plugin for registration in the generic graph.
func New() Plugin { return Plugin{} }

// PluginDeclaration implements plugin.Plugin without introducing an engine
// switch or a second registry.
func (Plugin) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: ID, Kind: inferenceengine.Kind}
}
