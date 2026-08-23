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
// THE TIE. qwen3.7-plus and qwen3.7-plus-via-codex both score 30, because the
// second deliberately mirrors the first's profile under another harness. The
// tie is carried rather than broken in the declaration; Lineup breaks it by
// declaration order for callers that need a total order, and marks it tied so
// one that does not need one is not misled. Nothing admission-related reads
// either: the frozen v2 snapshot (pkg/vendorplugin/v2snapshot.go) holds the
// qwen runtime's membership and has never held a row for the cross-runtime
// one.
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

// qwenTokenPlanTeamPricing is the Qwen Cloud Token Plan Team billing contract
// the five qwen-code rows are sold under.
//
// PORTED from skill-project-management's board table (see this file's header),
// where it was a single helper attached to those same five rows. It is a
// function rather than a package-level var for the reason it was one there: a
// shared *Pricing hands every row the same pointer, and one caller writing
// through it would change the published price of five models at once.
//
// The contract is SUBSCRIPTION-and-credits based, which is why it carries plans
// and credit allowances rather than per-token rates. A caller must not read the
// absence of a per-token rate as free use, and must not read a nil Pricing on
// any other row as one either; see vendorplugin.Pricing.
//
// qwen3.7-plus-via-codex deliberately carries NO contract. It is the
// evidentiary cross-runtime row, it is not on the Token Plan allowlist, and
// attaching this contract to it would be a billing claim about a registration
// nobody sells.
func qwenTokenPlanTeamPricing() *vendorplugin.Pricing {
	// The allowlist the plans price: exactly the five qwen-code rows, in the
	// board table's own order. Every plan prices the same five, so the slice is
	// built once and copied into each — a shared backing array would let a
	// caller edit three plans by editing one.
	allowlist := func() []vendorplugin.ModelID {
		return []vendorplugin.ModelID{
			"qwen3.8-max-preview",
			"qwen3.7-max",
			"qwen3.7-plus",
			"qwen3.6-plus",
			"qwen3.6-flash",
		}
	}
	return &vendorplugin.Pricing{
		BillingModel:       "token-plan-team-seat-credits",
		Edition:            "team",
		Currency:           "USD",
		QuotaPeriod:        "monthly",
		HasFrequencyLimits: false,
		Plans: []vendorplugin.PricingPlan{
			{Name: "standard", MonthlyUSD: 30, PromotionalMonthlyUSD: promotional(20), MonthlyCredits: 25_000, ApplicableModelIDs: allowlist()},
			{Name: "pro", MonthlyUSD: 100, PromotionalMonthlyUSD: promotional(75), MonthlyCredits: 100_000, ApplicableModelIDs: allowlist()},
			{Name: "max", MonthlyUSD: 200, MonthlyCredits: 250_000, ApplicableModelIDs: allowlist()},
		},
		SourceURL: "https://docs.qwencloud.com/token-plan/overview",
		AsOf:      "2026-07-21",
	}
}

// promotional wraps a limited-time price. A plan with no promotion leaves the
// field nil rather than setting zero: a promotion AT zero and no promotion at
// all are different offers, and the pointer is what keeps them apart.
func promotional(price float64) *float64 { return &price }

// models is the declaration itself, most capable first.
var models = []vendorplugin.Model{
	{
		ID:                  "qwen3.8-max-preview",
		Description:         "The most capable Qwen row, on preview terms rather than production ones",
		Rank:                rank(50, "the highest of the six alibaba rows", lineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
	{
		ID:                  "qwen3.7-max",
		Description:         "Production flagship for the hardest Qwen work",
		Rank:                rank(40, "below the preview flagship and above qwen3.7-plus", lineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
	{
		ID:                  "qwen3.7-plus",
		Description:         "The balanced default: reasoning and vision over a one-million-token context",
		Rank:                rank(30, "tied with qwen3.7-plus-via-codex, which mirrors its profile under another harness", lineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		Recommended:         true,
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
	{
		ID:                  "qwen3.7-plus-via-codex",
		Description:         "Evidence that a cross-runtime pair wires up, not a model to choose for real work: it mirrors qwen3.7-plus's profile under the codex harness and no vendor-captured Alibaba-via-codex model id has ever been observed",
		Rank:                rank(30, "tied with qwen3.7-plus, whose profile it mirrors under the codex harness", lineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max", "ultra"}),
		ContextWindowTokens: 1_000_000,
		Systems:             []agentic.SystemID{"codex"},
	},
	{
		ID:                  "qwen3.6-plus",
		Description:         "Previous-generation balanced reasoning and vision, for invocations pinned to it",
		Rank:                rank(20, "below the qwen3.7-plus pair and above qwen3.6-flash", lineup),
		Lifecycle:           vendorplugin.LifecycleLegacy,
		SupersededBy:        "qwen3.7-plus",
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
	{
		ID:                  "qwen3.6-flash",
		Description:         "The cheapest Qwen row: fast, high-volume turns",
		Rank:                rank(10, "the lowest of the six alibaba rows", lineup),
		Lifecycle:           vendorplugin.LifecycleLegacy,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
}
