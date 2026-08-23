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
// THE FIVE TIES. Every one of them is a gemini-cli row scoring the same as an
// antigravity row: the source's scores are per BROKER and the two harnesses
// were ranked from two different catalogues against it, so 50, 45, 40, 35 and
// 30 each appear twice. Those pairs are NOT claims that the two rows are
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

// geminiLineup and agyCatalogue are the vendor-side evidence the ranks in this
// file rest on, and they are two entries rather than one because the two
// harnesses were ranked from two different published sources.
//
// Each is a second basis entry rather than a replacement for the score: the
// score says what this repository's own registry recorded, and these say what
// it recorded it from.
var geminiLineup = vendorplugin.RankEvidence{
	Source:      "Google's Gemini API model documentation, as the source registry cites it (the Gemini 3.1 Pro, 3.5 Flash and Gemma 4 model pages)",
	Observation: "the documentation puts Pro above Flash above Flash-Lite for reasoning-heavy work, and gives the hosted Gemma rows their own smaller 262,144-token window rather than Gemini's 1,048,576; the ordering of the gemini-cli rows below is that lineup",
}

var agyCatalogue = vendorplugin.RankEvidence{
	Source:      "the `agy models` catalogue captured from agy 1.1.14 on 2026-08-19, as the source registry cites it",
	Observation: "the catalogue publishes one id per (base model, effort) pair rather than an effort control, so Pro-high outranks Pro-low and each Flash generation's high/medium/low rows order among themselves; the ordering of the antigravity rows below is that catalogue",
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
		Rank:                rank(60, "the highest of the fifteen google rows", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.1-pro-low",
		Description:         "Pro-class quality when the turn does not need deep thinking: Gemini 3.1 Pro pinned to low effort",
		Rank:                rank(55, "below gemini-3.1-pro-high and above the rows tied at 50", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.1-pro-preview",
		Description:         "Preview Pro for complex reasoning and agentic work, on preview terms",
		Rank:                rank(50, "tied with gemini-3.6-flash-high, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
	{
		ID:                  "gemini-3.6-flash-high",
		Description:         "The everyday Antigravity pick: Gemini 3.6 Flash pinned to high effort",
		Rank:                rank(50, "tied with gemini-3.1-pro-preview, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		Recommended:         true,
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.5-flash",
		Description:         "The everyday Gemini CLI pick: stable frontier Flash for agentic and coding turns",
		Rank:                rank(45, "tied with gemini-3.6-flash-medium, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		Recommended:         true,
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
	{
		ID:                  "gemini-3.6-flash-medium",
		Description:         "Gemini 3.6 Flash pinned to medium effort, for routine turns",
		Rank:                rank(45, "tied with gemini-3.5-flash, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3-flash-preview",
		Description:         "Preview multimodal Flash for agentic and coding work, on preview terms",
		Rank:                rank(40, "tied with gemini-3.6-flash-low, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
	{
		ID:                  "gemini-3.6-flash-low",
		Description:         "The cheapest 3.6 row: Gemini 3.6 Flash pinned to low effort, for short mechanical turns",
		Rank:                rank(40, "tied with gemini-3-flash-preview, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.1-flash-lite",
		Description:         "Low-latency, high-volume lightweight work: classification, extraction, short answers",
		Rank:                rank(35, "tied with gemini-3.5-flash-high, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
	{
		ID:                  "gemini-3.5-flash-high",
		Description:         "Gemini 3.5 Flash pinned to high effort, one generation behind the 3.6 rows",
		Rank:                rank(35, "tied with gemini-3.1-flash-lite, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-2.5-pro",
		Description:         "Older stable thinking model, still the pick for long-form maths and STEM reasoning",
		Rank:                rank(30, "tied with gemini-3.5-flash-medium, which the antigravity catalogue contributes", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
	{
		ID:                  "gemini-3.5-flash-medium",
		Description:         "Gemini 3.5 Flash pinned to medium effort",
		Rank:                rank(30, "tied with gemini-2.5-pro, which the gemini-cli lineup contributes", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemini-3.5-flash-low",
		Description:         "The cheapest Antigravity row: Gemini 3.5 Flash pinned to low effort",
		Rank:                rank(25, "the lowest antigravity row, above the two hosted gemma rows", agyCatalogue),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 1_048_576,
		Systems:             []agentic.SystemID{"antigravity"},
	},
	{
		ID:                  "gemma-4-31b-it",
		Description:         "Hosted dense Gemma 4 instruction model: open-weights behaviour without hosting it yourself",
		Rank:                rank(20, "above gemma-4-26b-a4b-it and below every gemini row", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 262_144,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
	{
		ID:                  "gemma-4-26b-a4b-it",
		Description:         "Hosted mixture-of-experts Gemma 4 instruction model, cheaper per token than the dense row",
		Rank:                rank(10, "the lowest of the fifteen google rows", geminiLineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortNone(),
		ContextWindowTokens: 262_144,
		Systems:             []agentic.SystemID{"gemini-cli"},
	},
}
