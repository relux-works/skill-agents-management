package openai

import (
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// THE openai binding file: the one place this vendor's models, their ranks and
// the agentic systems that drive them are written down.
//
// One binding file per vendor is enforced, not merely intended:
// pkg/agentic/singlesource_guard_test.go names this exact path as the home of
// the "openai models" fact, and a plugin id spelled in a composite literal in
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
// NO TIE EXISTS HERE: the thirteen openai rows carry thirteen distinct scores,
// so the derived presentation order is the score order with nothing invented in
// it. That is a fact about this vendor's table rather than a property of the
// type — the anthropic, alibaba and google files each carry real ties.
//
// # One row is NOT a port, and says so in its own evidence
//
// gpt-6-astra reached this repository BEFORE the board's registry, so no
// capture of that registry contains it and none can be made to. The row
// therefore rests on the vendor's own catalog on the operator's machine
// (codexCatalog below) and is built by declaredRank rather than rank: it
// carries no "the row carries PolicyRank N" observation, because there is no
// board row to have carried one. sourceport_test.go names it in
// declaredHereRows and holds both halves of that — the pins account for it
// explicitly, and a row that named the board registry it never came from
// fails TestEveryDeclaredHereRowRestsOnTheVendorCatalog.
//
// Fabricating the missing board row into testdata/source-model-registry.json
// would have been the cheaper move and is exactly what the pins exist to
// refuse: a capture with an invented row in it is self-minted evidence, and
// the rank basis quoting it would name a file and a commit a reader can open
// and not find the number in.
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
	Source:      "the OpenAI Codex model picker lineup, as the source registry's own row descriptions record it",
	Observation: "the picker presents sol as the latest frontier model, terra as the balanced everyday one and luna as the fast affordable one, then the general-purpose 5.5 and 5.4 rows, then the renamed tiers the source marks legacy; the ordering below is that presentation",
}

// codexCatalog is the vendor's own model catalog as the Codex CLI renders it,
// and the only evidence a row this repository declares FIRST can rest on.
//
// It is a MACHINE-LOCAL read rather than a published card, and naming the exact
// binary version and the date is what makes it checkable: `codex debug models`
// prints the catalog the installed CLI would actually launch through, so a
// reader can re-run one command and get the same JSON or find that the vendor
// moved.
const codexCatalog = "the OpenAI Codex CLI model catalog, read with `codex debug models` (codex-cli 0.153.4, 2026-09-05)"

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

// declaredRank builds a capability rank for a row NO capture of the board's
// registry contains, and it is a separate constructor rather than a flag on
// rank for one reason: rank's first basis entry states "the row carries
// PolicyRank N" and names the source registry file and commit. For a row the
// source registry has never held, that sentence is a fabrication a reader
// would only discover by opening the file. The two constructors make the
// difference structural — a ported row cannot lose its source evidence and a
// declared row cannot borrow it.
func declaredRank(score int, note string, evidence ...vendorplugin.RankEvidence) vendorplugin.CapabilityRank {
	basis := []vendorplugin.RankEvidence{{
		Source:      codexCatalog,
		Observation: note,
	}}
	return vendorplugin.CapabilityRank{Score: score, Basis: append(basis, evidence...)}
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
		ID:          "gpt-6-astra",
		Description: "The most capable Codex model: complex, demanding work that sol does not carry, at the highest cost per turn",
		// 130 rather than 125: the score is spaced by ten across this whole
		// vendor, and a gap left above sol is a gap the next generation can be
		// ranked into without re-scoring a row that did not change.
		//
		// It must not equal any ported score. An added row sharing 120 would
		// make sol report Tied — an equality the board never observed —
		// and TestADeclaredRowMayNotTieAPortedOne fails on it.
		Rank: declaredRank(130,
			"the catalog presents this row at picker priority 1, ahead of gpt-5.6-sol at 6, and describes it as \"Our most capable model for complex, demanding work.\"; its availability note calls it state-of-the-art in coding, computer use, science and professional work",
			vendorplugin.RankEvidence{
				Source:      sourceRegistry,
				Observation: "the source registry's highest openai row, gpt-5.6-sol, carries PolicyRank 120, and this row is scored above it; the source registry contains NO row for gpt-6-astra and this score is therefore not a ported one",
			}),
		Lifecycle: vendorplugin.LifecycleCurrent,
		Effort:    effortRequired("max", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		// NOT Recommended, deliberately. Recommended is the vendor's display
		// pick and at most one openai row may carry it; sol holds it, and
		// admitting a more capable model is not a decision to change what an
		// operator is steered towards by default.
		//
		// The catalog's own default_reasoning_level for this row is `medium`,
		// which is a DEFAULT and not this field's recommendation — the same
		// split every other row here already carries (the catalog defaults sol
		// to `low` while this file recommends `max`).
		//
		// The catalog states context_window 272000 and max_context_window
		// 872000 for this row. 272000 is declared because it is the window the
		// installed CLI runs the model with; 872000 is a ceiling a default
		// launch does not get, and declaring it would overstate what an
		// operator can plan a run around.
		ContextWindowTokens: 272_000,
		// Pricing stays nil. The catalog publishes this row's service tiers
		// (`priority`, "Fast", "2x speed, increased usage") and no price at
		// all, and no openai row here carries a billing contract. A contract
		// invented from a speed tier would be a price nobody published.
		Systems: []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.6-sol",
		Description: "The frontier Codex model: the hardest agentic coding work, at the highest cost per turn",
		Rank:        rank(120, "the highest of the twelve openai rows the board's registry declares, below the gpt-6-astra row this file declares from the vendor catalog", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("max", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		Recommended: true,
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.6-terra",
		Description: "Balanced agentic coding for everyday work; the usual pick when sol is more than the task needs",
		Rank:        rank(110, "below sol and above luna", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("max", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.6-luna",
		Description: "Fast and cheaper agentic coding, for high-volume or latency-sensitive turns",
		Rank:        rank(100, "below terra and above gpt-5.5", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("max", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.5",
		Description: "General frontier reasoning for complex coding and research; not coding-specialized",
		Rank:        rank(90, "below luna and above gpt-5.4", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.4",
		Description: "Solid everyday coding one generation behind gpt-5.5",
		Rank:        rank(80, "below gpt-5.5 and above gpt-5.4-mini", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.4-mini",
		Description: "Small and cheap: simple edits, boilerplate and mechanical work",
		Rank:        rank(70, "below gpt-5.4 and above the spark row", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.3-codex-spark",
		Description: "The lowest-latency coding row, for tight interactive loops rather than long autonomous runs",
		Rank:        rank(60, "the lowest of the current rows, above every legacy one", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.3-codex",
		Description: "Legacy coding-specialized frontier model for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(50, "the highest of the legacy rows", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.2-codex",
		Description: "Legacy agentic coding model for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(40, "below gpt-5.3-codex and above gpt-5.1-codex-max", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.1-codex-max",
		Description: "Legacy Codex flagship for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(30, "below gpt-5.2-codex and above gpt-5.2", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.2",
		Description: "Legacy general-purpose reasoning model for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(20, "below gpt-5.1-codex-max and above gpt-5.1-codex-mini", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.1-codex-mini",
		Description: "The cheapest row this vendor still registers, kept for invocations pinned to it",
		Rank:        rank(10, "the lowest of the twelve openai rows the board's registry declares", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("high", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
}
