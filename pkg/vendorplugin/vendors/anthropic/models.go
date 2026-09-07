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
// PORTED VERBATIM as well, since v0.2.0: the capability SCORE, the lineup
// state, the supersession, the display recommendation, the context window and
// the billing contract. Until v0.2.0 this file DERIVED a tie-free position from
// the score, and that reshape asserted an ordering nobody had observed wherever
// the source's scores tied. The score is now carried as it stands, ties and
// all, and the total order some callers need is derived by vendorplugin.Lineup
// from the list rather than declared per row — see lineup.go on why a position
// is presentation and a score is evidence.
//
// The board-owned fields are pinned row by row against a frozen capture of the
// board's own table (pkg/vendorplugin/testdata/board-model-facts.json, read by
// pkg/vendorplugin/boardfacts_test.go), so a slipped digit in a price or a
// swapped lifecycle fails rather than passing quietly.
//
// THE TIE. claude-haiku-4-5 and its dated snapshot claude-haiku-4-5-20251001
// both score 10, and the frozen v2 admission snapshot puts those two ids in ONE
// tier for exactly that reason — they are one model under two names. That tie
// is now VISIBLE to a caller rather than flattened into positions 7 and 8, and
// the presentation order Lineup derives for them is declaration order and
// nothing more. Nothing admission-related reads either the score or the derived
// position: membership comes from the frozen snapshot
// (pkg/vendorplugin/v2snapshot.go), and a capability rank stays evidence.
//
// AUTHORED HERE, not ported: every Description. The source's rows carry a
// short display string and no what-is-this-model-best-for field at all, while
// the vendor contract requires one and refuses a blank. The descriptions below
// were written for this repository from the models' documented positioning.
// They are the ONLY field in this file that is not a source fact, and they must
// never be cited as one.

// sourceRegistry is the table every capability score in this file was read
// from. It names the exact commit so a reader chasing a score has a revision to
// open rather than a moving target.
//
// The board-owned facts the rows gained in v0.2.0 — the score again, the
// lifecycle, the supersession, the recommendation, the context window and the
// billing contract — were re-read from that same table at a LATER commit and
// pinned against a capture of it; testdata/board-model-facts.json records which.
// The two captures agree on every column they share, which is what makes them
// corroboration rather than two chances to be wrong.
const sourceRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (knownModels, commit ed4878123061b39fdae67160f6b5632117b48a2f)"

// lineup is the vendor-side evidence every rank in this file also rests on.
//
// It is a second entry rather than a replacement for the score, because the two
// answer different objections: the score says what this repository's own
// registry recorded, and this says why that ordering is not merely an internal
// convention.
var lineup = vendorplugin.RankEvidence{
	Source:      "Anthropic's published Claude lineup, as the source registry's own row descriptions record it",
	Observation: "the lineup places fable-5-1 as the most capable model, opus-5 as the everyday complex-task model, sonnet-5 as the efficient one and haiku-4-5 as the fastest; the ordering below is that lineup, with each legacy id sitting where the source's score puts it",
}

// rank builds a capability rank that carries the source's own evidence.
//
// The contract refuses a rank with no observation behind it, and the
// observation here is not a restatement of the position: it is the source's
// PolicyRank score, the fact the position was derived FROM. A reader who
// distrusts a position can check it against a number in another repository
// rather than against this file's own opinion of itself.
func rank(score int, note string, evidence vendorplugin.RankEvidence) vendorplugin.CapabilityRank {
	return vendorplugin.CapabilityRank{
		Score: score,
		Basis: []vendorplugin.RankEvidence{
			{
				Source:      sourceRegistry,
				Observation: fmt.Sprintf("the row carries PolicyRank %d, %s", score, note),
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
//
// `pi-native` membership is CATALOG-VERIFIED, not inferred from the vendor: a
// row names it only when the id is present in the installed Pi built-in
// catalog (Pi 0.84.2, pi-ai providers/data/anthropic.json, api
// anthropic-messages), because native Pi resolves `anthropic/<id>` against
// that file and an absent id silently takes the custom-model fallback with a
// warning — an unverified launch. claude-fable-5-1 is absent from that
// catalog and therefore deliberately does NOT name pi-native; the launch is
// refused as ErrModelNotDrivenBySystem rather than guessed at. Re-verify the
// membership against the installed catalog bytes before changing it.
var models = []vendorplugin.Model{
	{
		ID:          "claude-fable-5-1",
		Description: "The hardest and longest-running work: deep refactors and multi-hour agent runs where a cheaper model stalls",
		Rank:        rank(85, "the highest of the nine anthropic rows, above the fable-5 it supersedes", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:           "claude-fable-5",
		Description:  "Previous-generation Fable, for runs pinned to it; prefer claude-fable-5-1 for new work",
		Rank:         rank(80, "below fable-5-1 and above opus-5", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-fable-5-1",
		Effort:       effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-opus-5",
		Description: "The default for complex day-to-day engineering: multi-file changes, design work and reviews that need judgement",
		Rank:        rank(75, "below fable-5 and above opus-4-8", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Recommended: true,
		Systems:     []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-opus-4-8",
		Description:  "Previous-generation Opus, for runs pinned to it; prefer claude-opus-5 for new work",
		Rank:         rank(70, "below opus-5 and above sonnet-5", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-opus-5",
		Effort:       effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-sonnet-5",
		Description: "Routine work at lower cost: scoped edits, tests and summaries where Opus-level judgement is not the bottleneck",
		Rank:        rank(60, "below opus-4-8 and above opus-4-6", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-opus-4-6",
		Description:  "Older top-tier Opus kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:         rank(30, "below sonnet-5 and above sonnet-4-6", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-opus-5",
		Effort:       effortRequired("high", []string{"low", "medium", "high", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-sonnet-4-6",
		Description:  "Older balanced Sonnet kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:         rank(20, "below opus-4-6 and above the two haiku rows", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-sonnet-5",
		Effort:       effortRequired("medium", []string{"low", "medium", "high", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-haiku-4-5",
		Description: "Fast, cheap turns with no reasoning-effort axis: classification, extraction and short answers",
		Rank:        rank(10, "the lowest of the nine anthropic rows, tied with its dated snapshot", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortNone(),
		Systems:     []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-haiku-4-5-20251001",
		Description:  "Dated snapshot of claude-haiku-4-5, for a run that must pin one exact build",
		Rank:         rank(10, "the lowest of the nine anthropic rows, tied with claude-haiku-4-5, which it is a dated snapshot of", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-haiku-4-5",
		Effort:       effortNone(),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
}
