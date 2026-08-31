package claude

import (
	"github.com/relux-works/skill-agents-management/internal/mcpjson"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// GrammarMCPConfigJSON names the launch-composition argv shape claude accepts:
// exactly one `--mcp-config <json>` pair, whose JSON document declares the same
// MCP servers the composition carries as metadata.
//
// The identifier names the GRAMMAR, not this system, and that is deliberate:
// the source's CompositionGrammarMCPConfigJSON is shared by claude, qwen and
// agy, validated by one function for all three.
//
// That day has arrived. The grammar id, the flag and the validator now live in
// internal/mcpjson, and the qwen and agy plugins declare THIS id by reaching
// the same value — which is what the previous version of this comment promised
// and what a second copy of the rules would have quietly broken. The names
// below stay exported and spelled here so a caller holding only this package
// still has claude's grammar to compare against, and so this file remains the
// place a reader looks for claude's composition contract.
const GrammarMCPConfigJSON = mcpjson.Grammar

// mcpConfigFlag is the one top-level argument a claude composition prefix may
// carry.
const mcpConfigFlag = mcpjson.ConfigFlag

// validateComposition refuses a composition whose prefix does not match
// claude's grammar. It is the source's validateClaudeLaunchCompositionPrefix,
// ported rule for rule into internal/mcpjson; every refusal that function makes
// is documented on Validate there, and this plugin's composition_test.go drives
// each one through System.ValidateComposition rather than through the shared
// package, so what is proved is what a claude launch is actually held to.
func validateComposition(c agentic.Composition) error {
	return mcpjson.Validate(c)
}
