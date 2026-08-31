// Package inferenceengine defines the first plugin kind added to the general
// graph after agentic systems and model vendors. The graph package does not
// import or enumerate this package.
package inferenceengine

import (
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// Kind is declared here, as plugin data, rather than in the registry.
const Kind plugin.Kind = "inference-engine"

// Configured is an identity-only inference-engine plugin supplied by operator
// configuration. Process construction is deliberately separate: an engine may
// already be supervised outside the agent launch, or contribute a PlanNode
// through NewPlanNode when the consumer owns that lifecycle.
type Configured struct {
	id           plugin.ID
	dependencies []plugin.Ref
}

// NewConfigured declares one configured engine identity. The shared graph
// validates normalization, duplicate ids, missing/wrong-kind dependencies and
// cycles; this constructor does not duplicate those rules.
func NewConfigured(id plugin.ID, dependencies ...plugin.Ref) Configured {
	return Configured{id: id, dependencies: append([]plugin.Ref(nil), dependencies...)}
}

func (e Configured) PluginDeclaration() plugin.Declaration {
	return plugin.Declaration{ID: e.id, Kind: Kind, Dependencies: append([]plugin.Ref(nil), e.dependencies...)}
}

var ErrWrongKind = errors.New("inferenceengine: plugin does not declare the inference-engine kind")

// NewPlanNode turns an inference-engine declaration and its process value into
// a typed launch node. Process execution and supervision remain consumer work.
func NewPlanNode(engine plugin.Plugin, process agentic.ProcessPlan, dependencies ...agentic.PlanNodeID) (agentic.PlanNode, error) {
	if engine == nil {
		return agentic.PlanNode{}, plugin.ErrNilPlugin
	}
	declaration := engine.PluginDeclaration()
	if declaration.Kind != Kind {
		return agentic.PlanNode{}, fmt.Errorf("%w: %q declares %q", ErrWrongKind, declaration.ID, declaration.Kind)
	}
	return agentic.PlanNode{
		ID:        agentic.PlanNodeID(declaration.ID),
		Plugin:    declaration.Ref(),
		DependsOn: append([]agentic.PlanNodeID(nil), dependencies...),
		Process:   process,
	}, nil
}
