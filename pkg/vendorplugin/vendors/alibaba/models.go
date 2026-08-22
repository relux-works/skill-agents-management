package alibaba

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE alibaba binding file: the one place this vendor's models, their ranks
// and the agentic systems that drive them are written down — including the
// cross-runtime row, which is the whole point of the architecture and would be
// worth nothing if it lived somewhere else.
//
// One binding file per vendor is enforced, not merely intended:
// pkg/agentic/singlesource_guard_test.go names this exact path as the home of
// the "alibaba models" fact, and a plugin id spelled in a composite literal in
// any other file of this package fails the module guard.
//
// # Provenance, field by field
//
// PORTED VERBATIM from skill-project-management: the model ids, the agentic
// systems each row declares, the effort vocabularies and the recommended
// efforts. A full-set pin (pkg/vendorplugin/sourceport_test.go) holds every one
// of them against a fixture captured from that repository's own sources, so a
// dropped or drifted row fails rather than passes quietly.
//
// DERIVED from the source: the capability rank POSITION. The source records a
// per-broker PolicyRank score where a higher number is more capable and ties
// are allowed; this layer records a position where 1 is most capable and ties
// are refused, because a lineup that cannot order itself is not a ranking. The
// positions below are the source's scores in descending order, ties broken by
// the source's own declaration order, and the score each position came from
// travels with it in the rank's basis. One tie exists here — qwen3.7-plus
// and qwen3.7-plus-via-codex both score 30, because the second deliberately
// mirrors the first's profile under another harness — and it is broken by
// declaration order. Nothing admission-related reads these positions: the
// frozen v2 snapshot (pkg/vendorplugin/v2snapshot.go) holds the qwen runtime's
// membership and has never held a row for the cross-runtime one, so the tie
// break cannot move who may spawn.
//
// AUTHORED HERE, not ported: every Description. The source's rows carry a
// short display string and no what-is-this-model-best-for field at all, while
// the vendor contract requires one and refuses a blank. The descriptions below
// were written for this repository from the models' documented positioning.
// They are the ONLY field in this file that is not a source fact, and they must
// never be cited as one.

// sourceRegistry is the table every rank position in this file was read from.
// It names the exact commit so a reader chasing a position has a revision to
// open rather than a moving target.
const sourceRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (knownModels, commit ed4878123061b39fdae67160f6b5632117b48a2f)"

// lineup is the vendor-side evidence every rank in this file also rests on.
//
// It is a second entry rather than a replacement for the score, because the two
// answer different objections: the score says what this repository's own
// registry recorded, and this says why that ordering is not merely an internal
// convention.
var lineup = vendorplugin.RankEvidence{
	Source:      "the Qwen Cloud Token Plan Team allowlist of 2026-07-21, as the source registry records it",
	Observation: "the allowlist carries exactly the five qwen-code rows below, with 3.8-max on preview terms above the 3.7 production pair and the 3.6 rows beneath them; the source's own Token Plan pricing block is attached to those five and to no other row",
}

// rank builds a capability rank that carries the source's own evidence.
//
// The contract refuses a rank with no observation behind it, and the
// observation here is not a restatement of the position: it is the source's
// PolicyRank score, the fact the position was derived FROM. A reader who
// distrusts a position can check it against a number in another repository
// rather than against this file's own opinion of itself.
func rank(position, policyRank int, note string, evidence vendorplugin.RankEvidence) vendorplugin.CapabilityRank {
	return vendorplugin.CapabilityRank{
		Position: position,
		Basis: []vendorplugin.RankEvidence{
			{
				Source:      sourceRegistry,
				Observation: fmt.Sprintf("the row carries PolicyRank %d, %s", policyRank, note),
			},
			evidence,
		},
	}
}

// effortRequired declares a model that must be given an explicit effort, with
// the words it accepts and the one the vendor recommends. Recommended is a
// DECLARATION and never a default: nothing substitutes it for a missing
// effort, and it reaches an operator through the refusal text instead.
func effortRequired(recommended string, vocabulary []string) vendorplugin.EffortDeclaration {
	return vendorplugin.EffortDeclaration{
		Support:     agentic.EffortSupportRequired,
		Vocabulary:  vocabulary,
		Recommended: recommended,
	}
}

// models is the declaration itself, most capable first.
var models = []vendorplugin.Model{
	{
		ID:          "qwen3.8-max-preview",
		Description: "The most capable Qwen row, on preview terms rather than production ones",
		Rank:        rank(1, 50, "the highest of the six alibaba rows", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"qwen-code"},
	},
	{
		ID:          "qwen3.7-max",
		Description: "Production flagship for the hardest Qwen work",
		Rank:        rank(2, 40, "below the preview flagship and above qwen3.7-plus", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"qwen-code"},
	},
	{
		ID:          "qwen3.7-plus",
		Description: "The balanced default: reasoning and vision over a one-million-token context",
		Rank:        rank(3, 30, "tied with qwen3.7-plus-via-codex, which mirrors its profile under another harness", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"qwen-code"},
	},
	{
		ID:          "qwen3.7-plus-via-codex",
		Description: "Evidence that a cross-runtime pair wires up, not a model to choose for real work: it mirrors qwen3.7-plus's profile under the codex harness and no vendor-captured Alibaba-via-codex model id has ever been observed",
		Rank:        rank(4, 30, "tied with qwen3.7-plus, whose profile it mirrors under the codex harness", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "qwen3.6-plus",
		Description: "Previous-generation balanced reasoning and vision, for invocations pinned to it",
		Rank:        rank(5, 20, "below the qwen3.7-plus pair and above qwen3.6-flash", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"qwen-code"},
	},
	{
		ID:          "qwen3.6-flash",
		Description: "The cheapest Qwen row: fast, high-volume turns",
		Rank:        rank(6, 10, "the lowest of the six alibaba rows", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"qwen-code"},
	},
}
