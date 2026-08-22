package anthropic

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE anthropic binding file: the one place this vendor's models, their ranks
// and the agentic systems that drive them are written down.
//
// One binding file per vendor is enforced, not merely intended:
// pkg/agentic/singlesource_guard_test.go names this exact path as the home of
// the "anthropic models" fact, and a plugin id spelled in a composite literal
// in any other file of this package fails the module guard. A second table of
// "which systems drive which anthropic model" is the shadow-binding disease
// the whole guard exists to prevent.
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
// travels with it in the rank's basis. One tie exists here — claude-haiku-4-5
// and its dated snapshot claude-haiku-4-5-20251001 both score 10 — and the
// frozen v2 admission snapshot puts those two ids in ONE tier for exactly that
// reason. Nothing admission-related reads these positions, which is what keeps
// the ordering of an alias pair from moving who may spawn: membership comes
// from that frozen snapshot (pkg/vendorplugin/v2snapshot.go), and a capability
// rank stays evidence.
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
	Source:      "Anthropic's published Claude lineup, as the source registry's own row descriptions record it",
	Observation: "the lineup places fable-5 as the most capable model, opus-5 as the everyday complex-task model, sonnet-5 as the efficient one and haiku-4-5 as the fastest; the ordering below is that lineup, with each legacy id sitting where the source's score puts it",
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

// effortNone declares a model with no reasoning-effort axis at all. Handing one
// an effort is refused rather than dropped, so a caller learns the parameter
// could not be honoured instead of paying for a turn that ignored it.
func effortNone() vendorplugin.EffortDeclaration {
	return vendorplugin.EffortDeclaration{Support: agentic.EffortSupportNone}
}

// models is the declaration itself, most capable first.
var models = []vendorplugin.Model{
	{
		ID:          "claude-fable-5",
		Description: "The hardest and longest-running work: deep refactors and multi-hour agent runs where a cheaper model stalls",
		Rank:        rank(1, 80, "the highest of the eight anthropic rows", lineup),
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-opus-5",
		Description: "The default for complex day-to-day engineering: multi-file changes, design work and reviews that need judgement",
		Rank:        rank(2, 75, "below fable-5 and above opus-4-8", lineup),
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-opus-4-8",
		Description: "Previous-generation Opus, for runs pinned to it; prefer claude-opus-5 for new work",
		Rank:        rank(3, 70, "below opus-5 and above sonnet-5", lineup),
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-sonnet-5",
		Description: "Routine work at lower cost: scoped edits, tests and summaries where Opus-level judgement is not the bottleneck",
		Rank:        rank(4, 60, "below opus-4-8 and above opus-4-6", lineup),
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-opus-4-6",
		Description: "Older top-tier Opus kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:        rank(5, 30, "below sonnet-5 and above sonnet-4-6", lineup),
		Effort:      effortRequired("high", []string{"low", "medium", "high", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-sonnet-4-6",
		Description: "Older balanced Sonnet kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:        rank(6, 20, "below opus-4-6 and above the two haiku rows", lineup),
		Effort:      effortRequired("medium", []string{"low", "medium", "high", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-haiku-4-5",
		Description: "Fast, cheap turns with no reasoning-effort axis: classification, extraction and short answers",
		Rank:        rank(7, 10, "the lowest of the eight anthropic rows, tied with its dated snapshot", lineup),
		Effort:      effortNone(),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:          "claude-haiku-4-5-20251001",
		Description: "Dated snapshot of claude-haiku-4-5, for a run that must pin one exact build",
		Rank:        rank(8, 10, "the lowest of the eight anthropic rows, tied with claude-haiku-4-5, which it is a dated snapshot of", lineup),
		Effort:      effortNone(),
		Systems:     []agentic.SystemID{"claude-code"},
	},
}
