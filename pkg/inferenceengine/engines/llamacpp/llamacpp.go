// Package llamacpp declares the llama-cpp engine graph and observation kind.
// It does not discover or execute an engine process.
package llamacpp

import (
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// ID is the stable configured llama-cpp inference-engine identity.
const ID plugin.ID = "llama-cpp"

// Plugin declares graph identity and observation kind; its zero value is valid.
type Plugin struct{}

// New returns a concrete llama-cpp graph plugin.
func New() Plugin { return Plugin{} }

// PluginDeclaration implements the generic graph contract.
func (Plugin) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: ID, Kind: inferenceengine.Kind}
}

// ObservationKind binds llama-cpp to the GGUF-server readings contract.
func (Plugin) ObservationKind() inferenceengine.EngineKind {
	return inferenceengine.EngineKindGGUFServer
}
