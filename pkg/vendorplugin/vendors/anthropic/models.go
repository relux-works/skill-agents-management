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
// PORTED VERBATIM as well, since v0.2.0: the lineup state, the supersession,
// the display recommendation, the context window and the billing contract.
// The board-owned fields are pinned row by row against a frozen capture of the
// board's own table (pkg/vendorplugin/testdata/board-model-facts.json, read by
// pkg/vendorplugin/boardfacts_test.go), so a slipped digit in a price or a
// swapped lifecycle fails rather than passing quietly.
//
// NOT PORTED ANY MORE: the capability SCORE. Until the Bug Hunt Bench re-rank
// the score was the source registry's own PolicyRank — hand-ordered 85..10
// with nothing behind it but the author's placing. Every score below is now a
// bench point (planted bugs fixed out of 105, vendorplugin.bench.go): the three
// rows the leaderboard measured (fable-5-1 43, opus-5 27, opus-4-8 15, each at
// max) carry the exact count, and every other row carries a value explicitly
// marked interpolated between two named anchors. The registry's PolicyRank is
// still quoted on every row, because it is still true of the registry, but it
// is quoted as the registry's number on its own scale and never as the score.
// pkg/vendorplugin/bughunt_test.go holds each score against an independent
// transcription of the leaderboard.
//
// THE ONE ORDER THE BENCH CHANGED. The registry places opus-4-8 (70) above
// sonnet-5 (60). The leaderboard measured opus-4-8 at 15/105 and did not
// measure sonnet-5, whose interpolated 20 sits between opus-5's measured 27
// and opus-4-8's measured 15. So sonnet-5 now stands above opus-4-8, and that
// is the bench disagreeing with the registry rather than this file re-ordering
// on its own: the measured number is the evidence and the registry order is
// what an interpolated row keeps only where no measurement contradicts it.
//
// THE TIE. claude-haiku-4-5 and its dated snapshot claude-haiku-4-5-20251001
// both score 5, and the frozen v2 admission snapshot puts those two ids in ONE
// tier for exactly that reason — they are one model under two names. That tie
// is VISIBLE to a caller rather than flattened into positions 8 and 9, and the
// presentation order Lineup derives for them is declaration order and nothing
// more. Nothing admission-related reads either the score or the derived
// position: membership comes from the frozen snapshot
// (pkg/vendorplugin/v2snapshot.go), and a capability rank stays evidence.
//
// AUTHORED HERE, not ported: every Description. The source's rows carry a
// short display string and no what-is-this-model-best-for field at all, while
// the vendor contract requires one and refuses a blank. The descriptions below
// were written for this repository from the models' documented positioning.
// They are the ONLY field in this file that is not a source fact, and they must
// never be cited as one.

// sourceRegistry is the table the rows' PolicyRank was read from — the order
// the interpolated rows keep where no measurement contradicts it. It names the
// exact commit so a reader chasing a number has a revision to open rather than
// a moving target.
//
// The board-owned facts the rows gained in v0.2.0 — the PolicyRank again, the
// lifecycle, the supersession, the recommendation, the context window and the
// billing contract — were re-read from that same table at a LATER commit and
// pinned against a capture of it; testdata/board-model-facts.json records which.
// The two captures agree on every column they share, which is what makes them
// corroboration rather than two chances to be wrong.
const sourceRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (knownModels, commit ed4878123061b39fdae67160f6b5632117b48a2f)"

// lineup is the vendor-side evidence every rank in this file also rests on.
//
// It is a third entry beside the bench claim and the registry number rather
// than a replacement for either: the bench says how many bugs the row fixed or
// which two rows it was interpolated between, the registry says where the
// board's own table ordered it, and this says why that ordering is not merely
// an internal convention.
var lineup = vendorplugin.RankEvidence{
	Source:      "Anthropic's published Claude lineup, as the source registry's own row descriptions record it",
	Observation: "the lineup places fable-5-1 as the most capable model, opus-5 as the everyday complex-task model, sonnet-5 as the efficient one and haiku-4-5 as the fastest; the ordering below is that lineup, with each legacy id sitting where the source's score puts it",
}

// rank builds a capability rank: a bench-anchored score, the bench claim
// behind it, and the source registry's own PolicyRank for the row.
//
// The score and the bench claim are typed separately on purpose — a
// constructor that derived the observation from the score would agree with
// any score at all — and pkg/vendorplugin/bughunt_test.go holds the two
// against each other. policyRank is the registry's number and is quoted as
// such: still true of the registry, the order the interpolated rows keep, and
// never the score, which is why the sentence says whose scale it is on.
func rank(score int, bench vendorplugin.RankEvidence, policyRank int, note string, evidence vendorplugin.RankEvidence) vendorplugin.CapabilityRank {
	return vendorplugin.CapabilityRank{
		Score: score,
		Basis: []vendorplugin.RankEvidence{
			bench,
			{
				Source:      sourceRegistry,
				Observation: fmt.Sprintf("the row carries PolicyRank %d on the source registry's own per-vendor scale (the score is the bench value, not that number), %s", policyRank, note),
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
		Rank:        rank(43, vendorplugin.BughuntMeasured("max", 43), 85, "the highest of the nine anthropic rows, above the fable-5 it supersedes; its high setting fixed 33", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code"},
	},
	{
		ID:           "claude-fable-5",
		Description:  "Previous-generation Fable, for runs pinned to it; prefer claude-fable-5-1 for new work",
		Rank:         rank(36, vendorplugin.BughuntInterpolated("claude-fable-5-1", "claude-opus-5", "unmeasured; the predecessor of 5.1, placed above opus-5 as the registry orders it"), 80, "below fable-5-1 and above opus-5", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-fable-5-1",
		Effort:       effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-opus-5",
		Description: "The default for complex day-to-day engineering: multi-file changes, design work and reviews that need judgement",
		Rank:        rank(27, vendorplugin.BughuntMeasured("max", 27), 75, "below fable-5 and above opus-4-8; its high setting fixed 21", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Recommended: true,
		Systems:     []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-sonnet-5",
		Description: "Routine work at lower cost: scoped edits, tests and summaries where Opus-level judgement is not the bottleneck",
		Rank:        rank(20, vendorplugin.BughuntInterpolated("claude-opus-5", "claude-opus-4-8", "unmeasured; placed between the two measured opus rows, which puts it above opus-4-8 where the registry had it below"), 60, "below opus-4-8 and above opus-4-6", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("high", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-opus-4-8",
		Description:  "Previous-generation Opus, for runs pinned to it; prefer claude-opus-5 for new work",
		Rank:         rank(15, vendorplugin.BughuntMeasured("max", 15), 70, "below opus-5 and above sonnet-5 on the registry's scale; the bench measured this row below sonnet-5's interpolated 20, and the measured number wins", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-opus-5",
		Effort:       effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-opus-4-6",
		Description:  "Older top-tier Opus kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:         rank(12, vendorplugin.BughuntInterpolated("claude-opus-4-8", "claude-sonnet-4-6", "legacy and unmeasured; the registry order is kept"), 30, "below sonnet-5 and above sonnet-4-6", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-opus-5",
		Effort:       effortRequired("high", []string{"low", "medium", "high", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-sonnet-4-6",
		Description:  "Older balanced Sonnet kept for backward-compatible invocations; its effort vocabulary stops below xhigh",
		Rank:         rank(9, vendorplugin.BughuntInterpolated("claude-opus-4-6", "claude-haiku-4-5", "legacy and unmeasured; the registry order is kept"), 20, "below opus-4-6 and above the two haiku rows", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-sonnet-5",
		Effort:       effortRequired("medium", []string{"low", "medium", "high", "max"}),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:          "claude-haiku-4-5",
		Description: "Fast, cheap turns with no reasoning-effort axis: classification, extraction and short answers",
		Rank:        rank(5, vendorplugin.BughuntInterpolated("claude-sonnet-4-6", vendorplugin.BugHuntBenchFloor, "unmeasured; the lowest row of the registry order, tied with its dated snapshot claude-haiku-4-5-20251001"), 10, "the lowest of the nine anthropic rows, tied with its dated snapshot", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortNone(),
		Systems:     []agentic.SystemID{"claude-code", "pi-native"},
	},
	{
		ID:           "claude-haiku-4-5-20251001",
		Description:  "Dated snapshot of claude-haiku-4-5, for a run that must pin one exact build",
		Rank:         rank(5, vendorplugin.BughuntInterpolated("claude-sonnet-4-6", vendorplugin.BugHuntBenchFloor, "unmeasured; tied with claude-haiku-4-5, which it is a dated snapshot of"), 10, "the lowest of the nine anthropic rows, tied with claude-haiku-4-5, which it is a dated snapshot of", lineup),
		Lifecycle:    vendorplugin.LifecycleLegacy,
		SupersededBy: "claude-haiku-4-5",
		Effort:       effortNone(),
		Systems:      []agentic.SystemID{"claude-code", "pi-native"},
	},
}
