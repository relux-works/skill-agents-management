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
// PORTED VERBATIM as well, since v0.2.0: the lineup state, the supersession,
// the display recommendation, the context window and the billing contract.
// The board-owned fields are pinned row by row against a frozen capture of the
// board's own table (pkg/vendorplugin/testdata/board-model-facts.json, read by
// pkg/vendorplugin/boardfacts_test.go), so a slipped digit in a price or a
// swapped lifecycle fails rather than passing quietly.
//
// NOT PORTED ANY MORE: the capability SCORE. Until the Bug Hunt Bench re-rank
// the score was the source registry's own PolicyRank — hand-ordered 50..10
// with nothing behind it but the author's placing. Every score below is now a
// bench point (planted bugs fixed out of 105, vendorplugin.bench.go): the one
// row the leaderboard measured, qwen3.8-max-preview as `qwen3.8-max` at max
// (28), carries the exact count, and every other row carries a value
// explicitly marked interpolated between two named anchors, keeping the
// registry's order. The registry's PolicyRank is still quoted on every row,
// because it is still true of the registry, but as the registry's number on
// its own scale and never as the score. pkg/vendorplugin/bughunt_test.go holds
// each score against an independent transcription of the leaderboard.
//
// THE TIE. qwen3.7-plus and qwen3.7-plus-via-codex both score 18, because the
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

// sourceRegistry is the table the rows' PolicyRank was read from — the order
// the interpolated rows keep. It names the exact commit so a reader chasing a
// number has a revision to open rather than a moving target.
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
	Source:      "the Qwen Cloud Token Plan Team allowlist of 2026-07-21, as the source registry records it",
	Observation: "the allowlist carries exactly the five qwen-code rows below, with 3.8-max on preview terms above the 3.7 production pair and the 3.6 rows beneath them; the source's own Token Plan pricing block is attached to those five and to no other row",
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
		Rank:                rank(28, vendorplugin.BughuntMeasuredAs("qwen3.8-max", "max", 28), 50, "the highest of the six alibaba rows", lineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
	{
		ID:                  "qwen3.7-max",
		Description:         "Production flagship for the hardest Qwen work",
		Rank:                rank(22, vendorplugin.BughuntInterpolated("qwen3.8-max-preview", "qwen3.7-plus", "unmeasured; the registry order is kept"), 40, "below the preview flagship and above qwen3.7-plus", lineup),
		Lifecycle:           vendorplugin.LifecycleCurrent,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
	{
		ID:                  "qwen3.7-plus",
		Description:         "The balanced default: reasoning and vision over a one-million-token context",
		Rank:                rank(18, vendorplugin.BughuntInterpolated("qwen3.7-max", "qwen3.6-plus", "unmeasured; the registry order is kept, tied with qwen3.7-plus-via-codex"), 30, "tied with qwen3.7-plus-via-codex, which mirrors its profile under another harness", lineup),
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
		Rank:                rank(18, vendorplugin.BughuntInterpolated("qwen3.7-max", "qwen3.6-plus", "unmeasured; tied with qwen3.7-plus, whose profile it mirrors"), 30, "tied with qwen3.7-plus, whose profile it mirrors under the codex harness", lineup),
		Lifecycle:           vendorplugin.LifecyclePreview,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Systems:             []agentic.SystemID{"codex"},
	},
	{
		ID:                  "qwen3.6-plus",
		Description:         "Previous-generation balanced reasoning and vision, for invocations pinned to it",
		Rank:                rank(12, vendorplugin.BughuntInterpolated("qwen3.7-plus", "qwen3.6-flash", "legacy and unmeasured; the registry order is kept"), 20, "below the qwen3.7-plus pair and above qwen3.6-flash", lineup),
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
		Rank:                rank(8, vendorplugin.BughuntInterpolated("qwen3.6-plus", vendorplugin.BugHuntBenchFloor, "legacy and unmeasured; the lowest row of the registry order"), 10, "the lowest of the six alibaba rows", lineup),
		Lifecycle:           vendorplugin.LifecycleLegacy,
		Effort:              effortRequired("xhigh", []string{"low", "medium", "high", "xhigh", "max"}),
		ContextWindowTokens: 1_000_000,
		Pricing:             qwenTokenPlanTeamPricing(),
		Systems:             []agentic.SystemID{"qwen-code"},
	},
}
