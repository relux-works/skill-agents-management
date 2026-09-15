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
// PORTED VERBATIM as well, since v0.2.0: the lineup state, the supersession,
// the display recommendation, the context window and the billing contract.
// The board-owned fields are pinned row by row against a frozen capture of the
// board's own table (pkg/vendorplugin/testdata/board-model-facts.json, read by
// pkg/vendorplugin/boardfacts_test.go), so a slipped digit in a price or a
// swapped lifecycle fails rather than passing quietly.
//
// NOT PORTED ANY MORE: the capability SCORE. Until the Bug Hunt Bench re-rank
// the score was the source registry's own PolicyRank — hand-ordered decades
// 120..10 with nothing behind them but the author's placing. Every score below
// is now a bench point (planted bugs fixed out of 105, vendorplugin.bench.go):
// a row the leaderboard measured carries the exact count at its best effort
// setting, and a row it did not measure carries a value explicitly marked
// interpolated between two named anchors. The registry's PolicyRank is still
// quoted on every ported row, because it is still true of the registry and it
// is the ORDER the interpolated rows keep — but it is quoted as the registry's
// number on the registry's own scale, never as the score. The score is the
// bench value, and pkg/vendorplugin/bughunt_test.go holds each one against an
// independent transcription of the leaderboard.
//
// NO TIE AMONG THE PORTED ROWS: the twelve ported openai rows carry twelve
// distinct scores, so the derived presentation order is the score order with
// nothing invented in it. The one tie this vendor carries is `astra` with
// `gpt-6-astra`, an alias and its identity rather than two models. That is a
// fact about this vendor's table rather than a property of the type — the
// anthropic, alibaba and google files each carry real ties.
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

// sourceRegistry is the table the ported rows' PolicyRank was read from — the
// order the interpolated rows keep. It names the exact commit so a reader
// chasing a number has a revision to open rather than a moving target.
//
// The board-owned facts the rows gained in v0.2.0 — the PolicyRank again, the
// lifecycle, the supersession, the recommendation, the context window and the
// billing contract — were re-read from that same table at a LATER commit and
// pinned against a capture of it; testdata/board-model-facts.json records which.
// The two captures agree on every column they share, which is what makes them
// corroboration rather than two chances to be wrong.
const sourceRegistry = "skill-project-management tools/board-cli/internal/spawn/models.go (knownModels, commit ed4878123061b39fdae67160f6b5632117b48a2f)"

// lineup is the vendor-side evidence every ported rank in this file also
// rests on.
//
// It is a third entry beside the bench claim and the registry number rather
// than a replacement for either: the bench says how many bugs the row fixed or
// which two rows it was interpolated between, the registry says where the
// board's own table ordered it, and this says why that ordering is not merely
// an internal convention.
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

// codexCatalogAliasProbe is a SECOND read of the same catalog, recorded
// separately because it establishes a different kind of fact.
//
// codexCatalog above is quoted for what the catalog CONTAINS. This one is
// quoted for what it does NOT: at this version, on this date, the catalog
// carries nine rows — gpt-6-astra, gpt-reserve, gpt-5.6-sol, gpt-5.6-terra,
// gpt-5.6-luna, gpt-5.5, gpt-5.4-mini, gpt-5.3-codex-spark and
// codex-auto-review — and none of them is spelled `astra`. That absence is the
// whole justification for the `astra` row declaring AliasOf, so it must rest on
// a read somebody actually performed rather than on the earlier entry's date.
//
// The two versions differ (0.153.2 here, 0.153.4 above) and that is stated
// rather than smoothed over: they are two reads, and a single entry claiming
// both would be one of them invented.
const codexCatalogAliasProbe = "the OpenAI Codex CLI model catalog, read with `codex debug models` (codex-cli 0.153.2, 2026-09-07)"

// rank builds a capability rank for a PORTED row: a bench-anchored score, the
// bench claim behind it, and the source registry's own PolicyRank for the row.
//
// The score and the bench claim are typed separately on purpose — a
// constructor that derived the observation from the score would agree with
// any score at all — and pkg/vendorplugin/bughunt_test.go holds the two
// against each other. policyRank is the registry's number and is quoted as
// such: it is still true of the registry, it is the order every interpolated
// row keeps, and it must never be mistaken for the score, which is why the
// sentence says whose scale it is on.
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

// declaredRank builds a capability rank for a row NO capture of the board's
// registry contains, and it is a separate constructor rather than a flag on
// rank for one reason: rank's registry entry states "the row carries
// PolicyRank N" and names the source registry file and commit. For a row the
// source registry has never held, that sentence is a fabrication a reader
// would only discover by opening the file. The two constructors make the
// difference structural — a ported row cannot lose its registry evidence and
// a declared row cannot borrow it. The bench claim is carried by both: the
// leaderboard measured gpt-6-astra, and the bench is the one source a
// declared row and a ported row share.
func declaredRank(score int, bench vendorplugin.RankEvidence, note string, evidence ...vendorplugin.RankEvidence) vendorplugin.CapabilityRank {
	basis := []vendorplugin.RankEvidence{
		bench,
		{Source: codexCatalog, Observation: note},
	}
	return vendorplugin.CapabilityRank{Score: score, Basis: append(basis, evidence...)}
}

// withCost appends a cost observation to a rank. It is a separate step rather
// than a parameter of rank so the capability claim stays the first bench
// entry and a cost reading can never be the only bench evidence a row has.
func withCost(r vendorplugin.CapabilityRank, cost vendorplugin.RankEvidence) vendorplugin.CapabilityRank {
	r.Basis = append(r.Basis, cost)
	return r
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

// astraEffort is the astra head's effort axis, declared ONCE and shared by the
// identity row and its alias.
//
// It is a function rather than a package-level value for the reason the muse
// pair already states: the two rows are one model reached by two names, the
// registration refuses a pair whose axes disagree (checkAliases), and a second
// literal here would be a second place for the vocabulary to drift into that
// refusal. A function also hands each row its own backing array, so nothing
// that edits one row's vocabulary can reach the other's.
func astraEffort() vendorplugin.EffortDeclaration {
	return effortRequired("max", []string{"low", "medium", "high", "xhigh", "max", "ultra"})
}

// models is the declaration itself, most capable first.
var models = []vendorplugin.Model{
	{
		ID:          "gpt-6-astra",
		Description: "The most capable Codex model: complex, demanding work that sol does not carry, at the highest cost per turn",
		// 48 is the bench count at max, the best of this row's two measured
		// settings (xhigh fixed 43); the leaderboard's top row, and the one
		// number in this file nothing was interpolated from.
		//
		// It must not equal any ported score. An added row landing on a
		// ported row's bench value would make that row report Tied — an
		// equality the leaderboard never observed — and
		// TestADeclaredRowMayNotTieAPortedOne fails on it.
		Rank: declaredRank(48, vendorplugin.BughuntMeasured("max", 48),
			"the catalog presents this row at picker priority 1, ahead of gpt-5.6-sol at 6, and describes it as \"Our most capable model for complex, demanding work.\"; its availability note calls it state-of-the-art in coding, computer use, science and professional work",
			vendorplugin.RankEvidence{
				Source:      sourceRegistry,
				Observation: "the source registry's highest openai row, gpt-5.6-sol, carries PolicyRank 120 on the registry's own scale, and this row stands above it on the bench (48 to sol's 42); the source registry contains NO row for gpt-6-astra and this score is therefore not a ported one",
			}),
		Lifecycle: vendorplugin.LifecycleCurrent,
		Effort:    astraEffort(),
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
		// THE FLOATING SPELLING, and the one row here that the vendor does not
		// publish at all.
		//
		// `astra` is the name an operator, a spawn ceiling and a board config
		// reach for when they mean "the current astra head". The vendor has no
		// such id: the catalog probe recorded in codexCatalogAliasProbe lists
		// nine slugs and the only astra spelling among them is `gpt-6-astra`.
		// A launch that put `astra` on argv would therefore ask the provider
		// for a model it never published — the failure the muse-spark alias was
		// MEASURED making ("model muse-spark does not exist or you lack
		// access") before AliasOf existed to prevent it.
		//
		// AliasOf is what closes that, and where it closes it matters:
		// agentic.BuildPlan substitutes the identity ONCE, after every contract
		// refusal and before the first plugin surface, so binary resolution,
		// argv, the child environment and stdin are all built from
		// `gpt-6-astra`, while Plan.ModelIdentity keeps `astra` as Requested
		// for the audit trail and cost attribution. A substitution applied to
		// argv alone would still describe a model the provider does not have.
		//
		// DECLARED, never derived. Nothing in this module resolves a name by
		// prefix, suffix, version, score or description, and this row is not
		// the exception that starts: `astra` reaches `gpt-6-astra` because
		// this line says so, and a future `gpt-7-astra` does not inherit the
		// spelling by looking like it — somebody has to move this field.
		ID: "astra",
		// "the current astra head" is what the alias promises, and the
		// description says so rather than naming a version an edit could leave
		// stale. What it must NOT promise is a vendor id: this spelling is
		// admitted and audited under its own name and EXECUTES as gpt-6-astra.
		Description: "The short spelling of the current astra head, for an invocation that names the model without its generation; it executes as gpt-6-astra",
		// The identity's score, because this is the identity under another
		// name. The two rows therefore TIE, and that tie is an identity rather
		// than a judgement about two models — the same shape the muse pair
		// carries. It ties no PORTED row: 48 belongs to the pair alone, and
		// TestADeclaredRowMayNotTieAPortedOne is what keeps that true. The
		// bench claim names gpt-6-astra as the measured spelling, because the
		// leaderboard has no row called `astra` either.
		Rank: declaredRank(48, vendorplugin.BughuntMeasuredAs("gpt-6-astra", "max", 48),
			"this row is a short spelling of gpt-6-astra and carries that row's score; the catalog presents gpt-6-astra at picker priority 1 and publishes no separate row for the spelling `astra`, so there is no second capability to score",
			vendorplugin.RankEvidence{
				Source:      codexCatalogAliasProbe,
				Observation: "the catalog's nine rows are gpt-6-astra, gpt-reserve, gpt-5.6-sol, gpt-5.6-terra, gpt-5.6-luna, gpt-5.5, gpt-5.4-mini, gpt-5.3-codex-spark and codex-auto-review; NONE is spelled `astra`, which is why this row declares AliasOf instead of reaching argv under its own id",
			}),
		AliasOf:   "gpt-6-astra",
		Lifecycle: vendorplugin.LifecycleCurrent,
		// The identity's axis, from the one constructor both rows call.
		// checkAliases refuses a pair whose axes disagree, because the word is
		// validated against the REQUESTED row and transported to a process
		// running the TARGET.
		Effort: astraEffort(),
		// NOT Recommended, for the same reason gpt-6-astra is not: sol holds
		// this vendor's display pick, and at most one openai row may carry it.
		//
		// The context window is the identity's, transcribed rather than
		// derived: it is one model, so an alias declaring a different window
		// would be describing a run nobody can have.
		ContextWindowTokens: 272_000,
		Systems:             []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.6-sol",
		Description: "The frontier Codex model: the hardest agentic coding work, at the highest cost per turn",
		Rank:        rank(42, vendorplugin.BughuntMeasured("max", 42), 120, "the highest of the twelve openai rows the board's registry declares, below the gpt-6-astra row this file declares from the vendor catalog; the bench agrees on the order (astra 48, sol 42) and its high setting fixed 34", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("max", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		Recommended: true,
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.6-terra",
		Description: "Balanced agentic coding for everyday work; the usual pick when sol is more than the task needs",
		Rank:        rank(38, vendorplugin.BughuntInterpolated("gpt-5.6-sol", "gpt-5.6-luna", "the leaderboard has no terra run and the vendor tiers it as the balanced row between the two measured ones"), 110, "below sol and above luna", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("max", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.6-luna",
		Description: "Fast and cheaper agentic coding, for high-volume or latency-sensitive turns",
		// The owner's observation, kept as COST evidence on this row and not as
		// a capability claim: luna at max fixes as many bugs as sol at high
		// for a nineteenth of the money, which makes it the best price/quality
		// row this vendor has and changes nothing about the capability order
		// (sol at max, 42, stands above luna at max, 33). A reader choosing a
		// model sees both facts side by side; a caller sorting by score sees
		// the second only.
		Rank: withCost(rank(33, vendorplugin.BughuntMeasured("max", 33), 100, "below terra and above gpt-5.5", lineup),
			vendorplugin.BughuntCost("at max this row fixed 33/105 for $1.80 list cost, equal to gpt-5.6-sol at high (34/105 for $33.92) at 1/19 of the cost — $0.05 per fix against sol/max's $1.66; the best price/quality row OpenAI has, and cost evidence only: the capability order is unchanged, sol at max (42) above luna at max (33)")),
		Lifecycle: vendorplugin.LifecycleCurrent,
		Effort:    effortRequired("max", []string{"low", "medium", "high", "xhigh", "max"}),
		Systems:   []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.5",
		Description: "General frontier reasoning for complex coding and research; not coding-specialized",
		Rank:        rank(28, vendorplugin.BughuntInterpolated("gpt-5.6-luna", "gpt-5.4", "unmeasured; the registry order is kept and the value is spaced evenly below luna's measured 33"), 90, "below luna and above gpt-5.4", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.4",
		Description: "Solid everyday coding one generation behind gpt-5.5",
		Rank:        rank(24, vendorplugin.BughuntInterpolated("gpt-5.5", "gpt-5.4-mini", "unmeasured; the registry order is kept"), 80, "below gpt-5.5 and above gpt-5.4-mini", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.4-mini",
		Description: "Small and cheap: simple edits, boilerplate and mechanical work",
		Rank:        rank(20, vendorplugin.BughuntInterpolated("gpt-5.4", "gpt-5.3-codex-spark", "unmeasured; the registry order is kept"), 70, "below gpt-5.4 and above the spark row", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.3-codex-spark",
		Description: "The lowest-latency coding row, for tight interactive loops rather than long autonomous runs",
		Rank:        rank(16, vendorplugin.BughuntInterpolated("gpt-5.4-mini", "gpt-5.3-codex", "unmeasured; the registry order is kept"), 60, "the lowest of the current rows, above every legacy one", lineup),
		Lifecycle:   vendorplugin.LifecycleCurrent,
		Effort:      effortRequired("xhigh", []string{"low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.3-codex",
		Description: "Legacy coding-specialized frontier model for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(14, vendorplugin.BughuntInterpolated("gpt-5.3-codex-spark", "gpt-5.2-codex", "legacy and unmeasured; the registry order is kept"), 50, "the highest of the legacy rows", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.2-codex",
		Description: "Legacy agentic coding model for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(12, vendorplugin.BughuntInterpolated("gpt-5.3-codex", "gpt-5.1-codex-max", "legacy and unmeasured; the registry order is kept"), 40, "below gpt-5.3-codex and above gpt-5.1-codex-max", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.1-codex-max",
		Description: "Legacy Codex flagship for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(10, vendorplugin.BughuntInterpolated("gpt-5.2-codex", "gpt-5.2", "legacy and unmeasured; the registry order is kept"), 30, "below gpt-5.2-codex and above gpt-5.2", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
	{
		ID:          "gpt-5.2",
		Description: "Legacy general-purpose reasoning model for invocations pinned to it; it still accepts the minimal effort",
		Rank:        rank(8, vendorplugin.BughuntInterpolated("gpt-5.1-codex-max", "gpt-5.1-codex-mini", "legacy and unmeasured; the registry order is kept"), 20, "below gpt-5.1-codex-max and above gpt-5.1-codex-mini", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("xhigh", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex", "pi-native"},
	},
	{
		ID:          "gpt-5.1-codex-mini",
		Description: "The cheapest row this vendor still registers, kept for invocations pinned to it",
		Rank:        rank(6, vendorplugin.BughuntInterpolated("gpt-5.2", vendorplugin.BugHuntBenchFloor, "legacy and unmeasured; the lowest row of the registry order"), 10, "the lowest of the twelve openai rows the board's registry declares", lineup),
		Lifecycle:   vendorplugin.LifecycleLegacy,
		Effort:      effortRequired("high", []string{"minimal", "low", "medium", "high", "xhigh"}),
		Systems:     []agentic.SystemID{"codex"},
	},
}
