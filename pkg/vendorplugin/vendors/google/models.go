package google

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE google binding file: the one place this vendor's models, their ranks and
// the agentic systems that drive them are written down — BOTH harnesses'
// worth. Splitting the gemini-cli rows from the antigravity ones into two files
// would look tidy and would be the second binding this module's guard exists to
// stop: one vendor's lineup is one fact, and a rank is only comparable because
// it is written in one place.
//
// One binding file per vendor is enforced, not merely intended:
// pkg/agentic/singlesource_guard_test.go names this exact path as the home of
// the "google models" fact, and a plugin id spelled in a composite literal in
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
// PORTED VERBATIM as well, since v0.2.0: the lineup state, the supersession,
// the display recommendation, the context window and the billing contract.
// The board-owned fields are pinned row by row against a frozen capture of the
// board's own table (pkg/vendorplugin/testdata/board-model-facts.json, read by
// pkg/vendorplugin/boardfacts_test.go), so a slipped digit in a price or a
// swapped lifecycle fails rather than passing quietly.
//
// NOT PORTED ANY MORE: the capability SCORE. Until the Bug Hunt Bench re-rank
// the score was the source registry's own PolicyRank — hand-ordered 60..10
// with nothing behind it but the author's placing. Every score below is now a
// bench point (planted bugs fixed out of 105, vendorplugin.bench.go), and
// every one of them is INTERPOLATED: the only google row the leaderboard
// measured, gemini-3.8-flash at high (20), is in neither harness's catalogue
// here, so the whole lineup is anchored BELOW that measured row, keeping the
// registry's order and its ties, down to the bench floor. The top row's upper
// anchor is therefore a bench id rather than a lineup id, and the consistency
// test resolves it against the leaderboard. If gemini-3.8-flash reaches the
// antigravity catalogue it is added at 20, measured. The registry's PolicyRank
// is still quoted on every row, because it is still true of the registry, but
// as the registry's number on its own scale and never as the score.
// pkg/vendorplugin/bughunt_test.go holds each score against an independent
// transcription of the leaderboard.
//
// THE FIVE TIES. Every one of them is a gemini-cli row scoring the same as an
// antigravity row: the source's scores are per BROKER and the two harnesses
// were ranked from two different catalogues against it, so 16, 14, 12, 10 and
// 8 each appear twice. Those pairs are NOT claims that the two rows are
// interchangeable — they are two harnesses' independent placements on one
// broker's scale — and carrying the score as it stands is what lets a caller
// see that rather than read an invented ordering. Within either harness the
// ordering is untouched: the seven gemini-cli scores are distinct and so are
// the eight antigravity ones. Nothing admission-related reads any of it: the
// frozen v2 snapshot (pkg/vendorplugin/v2snapshot.go) is the membership
// authority and holds no google rows.
//
// AUTHORED HERE, not ported: every Description. The source's rows carry a
// short display string and no what-is-this-model-best-for field at all, while
// the vendor contract requires one and refuses a blank. The descriptions below
// were written for this repository from the models' documented positioning.
// They are the ONLY field in this file that is not a source fact, and they must
// never be cited as one.

// sourceRegistry is the table the rows' PolicyRank was read from — the order
// and the ties the interpolated rows keep. It names the exact commit so a
// reader chasing a number has a revision to open rather than a moving target.
//
// The board-owned facts the rows gained in v0.2.0 — the PolicyRank again, the
// lifecycle, the supersession, the recommendation, the context window and the
// billing contract — were re-read from that same table at a LATER commit and
// pinned against a capture of it; testdata/board-model-facts.json records which.
// The two captures agree on every column they share, which is what makes them
// corroboration rather than two chances to be wrong.
const sourceRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (knownModels, commit ed4878123061b39fdae67160f6b5632117b48a2f)"

// geminiLineup and agyCatalogue are the vendor-side evidence the ranks in this
// file rest on, and they are two entries rather than one because the two
// harnesses were ranked from two different published sources.
//
// Each is a third basis entry beside the bench claim and the registry number
// rather than a replacement for either: the bench says which two rows the
// score was interpolated between, the registry says where the board's own
// table ordered the row, and these say what it ordered it from.
var geminiLineup = vendorplugin.RankEvidence{
	Source:      "Google's Gemini API model documentation, as the source registry cites it (the Gemini 3.1 Pro, 3.5 Flash and Gemma 4 model pages)",
	Observation: "the documentation puts Pro above Flash above Flash-Lite for reasoning-heavy work, and gives the hosted Gemma rows their own smaller 262,144-token window rather than Gemini's 1,048,576; the ordering of the gemini-cli rows below is that lineup",
}

var agyCatalogue = vendorplugin.RankEvidence{
	Source:      "the `agy models` catalogue captured from agy 1.1.14 on 2026-08-19, as the source registry cites it",
	Observation: "the catalogue publishes one id per (base model, effort) pair rather than an effort control, so Pro-high outranks Pro-low and each Flash generation's high/medium/low rows order among themselves; the ordering of the antigravity rows below is that catalogue",
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

// benchTop is the measured leaderboard row every google score hangs below. It
// is NOT a lineup row: neither harness's catalogue carries gemini-3.8-flash,
// so it is an anchor and nothing more.
const benchTop = "gemini-3.8-flash"

// effortNone declares a model with no reasoning-effort axis at all. Handing one
// an effort is refused rather than dropped, so a caller learns the parameter
// could not be honoured instead of paying for a turn that ignored it.
func effortNone() vendorplugin.EffortDeclaration {
	return vendorplugin.EffortDeclaration{Support: agentic.EffortSupportNone}
}

// models is the declaration itself, most capable first.
var models = []vendorplugin.Model{
	{
		ID:                  "gemini-3.1-pro-high",
		Description:         "The most capable Antigravity row: Gemini 3.1 Pro with the harness pinned to high effort",
		Rank:                rank(19, vendorplugin.BughuntInterpolated(benchTop, "gemini-3.1-pro-low", "unmeasured; the whole lineup is anchored below the one measured google row, gemini-3.8-flash at high (20/105), which neither harness catalogues"), 60, "the highest of the fifteen google rows", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.1-pro-low",
		Description:         "Pro-class quality when the turn does not need deep thinking: Gemini 3.1 Pro pinned to low effort",
		Rank:                rank(17, vendorplugin.BughuntInterpolated("gemini-3.1-pro-high", "gemini-3.1-pro-preview", "unmeasured; the registry order is kept"), 55, "below gemini-3.1-pro-high and above the rows tied at 50", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.1-pro-preview",
		Description:         "Preview Pro for complex reasoning and agentic work, on preview terms",
		Rank:                rank(16, vendorplugin.BughuntInterpolated("gemini-3.1-pro-low", "gemini-3.5-flash", "unmeasured; the registry order and its tie with gemini-3.6-flash-high are kept"), 50, "tied with gemini-3.6-flash-high, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
	{
		ID:                  "gemini-3.6-flash-high",
		Description:         "The everyday Antigravity pick: Gemini 3.6 Flash pinned to high effort",
		Rank:                rank(16, vendorplugin.BughuntInterpolated("gemini-3.1-pro-low", "gemini-3.5-flash", "unmeasured; the registry order and its tie with gemini-3.1-pro-preview are kept"), 50, "tied with gemini-3.1-pro-preview, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		Recommended:         true,
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.5-flash",
		Description:         "The everyday Gemini CLI pick: stable frontier Flash for agentic and coding turns",
		Rank:                rank(14, vendorplugin.BughuntInterpolated("gemini-3.1-pro-preview", "gemini-3-flash-preview", "unmeasured; the registry order and its tie with gemini-3.6-flash-medium are kept"), 45, "tied with gemini-3.6-flash-medium, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		Recommended:         true,
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
	{
		ID:                  "gemini-3.6-flash-medium",
		Description:         "Gemini 3.6 Flash pinned to medium effort, for routine turns",
		Rank:                rank(14, vendorplugin.BughuntInterpolated("gemini-3.1-pro-preview", "gemini-3-flash-preview", "unmeasured; the registry order and its tie with gemini-3.5-flash are kept"), 45, "tied with gemini-3.5-flash, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3-flash-preview",
		Description:         "Preview multimodal Flash for agentic and coding work, on preview terms",
		Rank:                rank(12, vendorplugin.BughuntInterpolated("gemini-3.5-flash", "gemini-3.1-flash-lite", "unmeasured; the registry order and its tie with gemini-3.6-flash-low are kept"), 40, "tied with gemini-3.6-flash-low, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
	{
		ID:                  "gemini-3.6-flash-low",
		Description:         "The cheapest 3.6 row: Gemini 3.6 Flash pinned to low effort, for short mechanical turns",
		Rank:                rank(12, vendorplugin.BughuntInterpolated("gemini-3.5-flash", "gemini-3.1-flash-lite", "unmeasured; the registry order and its tie with gemini-3-flash-preview are kept"), 40, "tied with gemini-3-flash-preview, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.1-flash-lite",
		Description:         "Low-latency, high-volume lightweight work: classification, extraction, short answers",
		Rank:                rank(10, vendorplugin.BughuntInterpolated("gemini-3-flash-preview", "gemini-2.5-pro", "unmeasured; the registry order and its tie with gemini-3.5-flash-high are kept"), 35, "tied with gemini-3.5-flash-high, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
	{
		ID:                  "gemini-3.5-flash-high",
		Description:         "Gemini 3.5 Flash pinned to high effort, one generation behind the 3.6 rows",
		Rank:                rank(10, vendorplugin.BughuntInterpolated("gemini-3-flash-preview", "gemini-2.5-pro", "unmeasured; the registry order and its tie with gemini-3.1-flash-lite are kept"), 35, "tied with gemini-3.1-flash-lite, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-2.5-pro",
		Description:         "Older stable thinking model, still the pick for long-form maths and STEM reasoning",
		Rank:                rank(8, vendorplugin.BughuntInterpolated("gemini-3.1-flash-lite", "gemini-3.5-flash-low", "unmeasured; the registry order and its tie with gemini-3.5-flash-medium are kept"), 30, "tied with gemini-3.5-flash-medium, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
	{
		ID:                  "gemini-3.5-flash-medium",
		Description:         "Gemini 3.5 Flash pinned to medium effort",
		Rank:                rank(8, vendorplugin.BughuntInterpolated("gemini-3.1-flash-lite", "gemini-3.5-flash-low", "unmeasured; the registry order and its tie with gemini-2.5-pro are kept"), 30, "tied with gemini-2.5-pro, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.5-flash-low",
		Description:         "The cheapest Antigravity row: Gemini 3.5 Flash pinned to low effort",
		Rank:                rank(7, vendorplugin.BughuntInterpolated("gemini-2.5-pro", "gemma-4-31b-it", "unmeasured; the registry order is kept"), 25, "the lowest antigravity row, above the two hosted gemma rows", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemma-4-31b-it",
		Description:         "Hosted dense Gemma 4 instruction model: open-weights behaviour without hosting it yourself",
		Rank:                rank(5, vendorplugin.BughuntInterpolated("gemini-3.5-flash-low", "gemma-4-26b-a4b-it", "unmeasured; the registry order is kept"), 20, "above gemma-4-26b-a4b-it and below every gemini row", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 262_144,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
	{
		ID:                  "gemma-4-26b-a4b-it",
		Description:         "Hosted mixture-of-experts Gemma 4 instruction model, cheaper per token than the dense row",
		Rank:                rank(3, vendorplugin.BughuntInterpolated("gemma-4-31b-it", vendorplugin.BugHuntBenchFloor, "unmeasured; the lowest row of the registry order"), 10, "the lowest of the fifteen google rows", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 262_144,
		Systems:             []agentic.SystemID{"gemini-cli", "pi-native"},
	},
}
