// Package vendorplugin defines the model-vendor compatibility kind: the
// contract a vendor plugin implements, the registry that is the only place a
// vendor or runtime binding may live, and the declared (agentic system ×
// vendor) pairs those bindings resolve to. Vendor registrations publish their
// dependency edges into the general pkg/plugin graph.
//
// A vendor owns models, authentication and quota — anthropic, openai, alibaba,
// google. It does NOT own the harness: the binary, the argv grammar, the
// environment contract, the stdin transport and the reasoning-effort TRANSPORT
// all belong to pkg/agentic, and nothing here re-declares them. What a vendor
// declares about effort is the VOCABULARY (which words a model accepts) and
// the recommended value; how a word reaches a harness is the system's business
// and is not repeated in this package.
//
// # The compatibility dependency
//
// A vendor plugin depends on agentic-system plugins and declares which systems
// can drive each of its models. Registry.Register preserves and enforces that
// shipped edge: a vendor naming an
// agentic system that is not registered is REFUSED, with both identifiers in
// the error. The edge is what makes a cross-runtime combination — Qwen
// models under the Codex harness — a vendor declaring one more system rather
// than a core change.
//
// This edge is compatibility data, not a general registry direction rule.
// Other kinds may declare dependencies in either direction through pkg/plugin.
// BuildLaunch produces agentic.Plan and stops; process execution remains a
// consumer responsibility.
package vendorplugin

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// VendorID is the stable identifier of a vendor plugin, normalized to one
// spelling. On-disk limit state is keyed by (provider, home) — invariant 2 of
// docs/architecture.md — and the provider half is this value, so two spellings
// of one vendor silently orphan state exactly the way two spellings of a
// system do.
type VendorID string

// String renders the identifier for error text and CLI output.
func (id VendorID) String() string { return string(id) }

// ModelID is a vendor's own name for one model.
//
// Unlike a plugin identifier it is NOT folded. A model id is a foreign key
// into the vendor's API, and lowercasing or re-spelling it would send a name
// the vendor never issued. It is only checked for the shapes that could not be
// a name at all: blank, or carrying whitespace that a copy-paste dropped in.
type ModelID string

// String renders the identifier for error text and CLI output.
func (id ModelID) String() string { return string(id) }

// InvalidIDError says why an identifier was refused, naming the kind of id,
// the raw spelling, the rule it broke and an example of a legal spelling of
// that kind. The refusal has to be enough to fix the declaration from the
// error text alone.
type InvalidIDError struct {
	Kind    string
	Raw     string
	Reason  string
	Example string
}

func (e *InvalidIDError) Error() string {
	return fmt.Sprintf("invalid %s %q: %s; %s, for example %q", e.Kind, e.Raw, e.Reason, ident.Rule, e.Example)
}

// NormalizeVendorID folds a vendor identifier through the module's single
// identifier normalization (internal/ident).
func NormalizeVendorID(raw string) (VendorID, error) {
	normalized, reason, ok := ident.Normalize(raw)
	if !ok {
		return "", &InvalidIDError{Kind: "vendor id", Raw: raw, Reason: reason, Example: "anthropic"}
	}
	return VendorID(normalized), nil
}

// ValidateModelID refuses the spellings that cannot be a vendor's model name.
// It does not normalize: see ModelID.
func ValidateModelID(raw ModelID) error {
	value := string(raw)
	if strings.TrimSpace(value) == "" {
		return &InvalidIDError{Kind: "model id", Raw: value, Reason: "it is empty", Example: "claude-opus-4-5"}
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t\r\n") {
		return &InvalidIDError{Kind: "model id", Raw: value, Reason: "it carries whitespace, and a model id is sent to the vendor verbatim", Example: "claude-opus-4-5"}
	}
	return nil
}

// UsageDescription says what a model is BEST USED FOR, in an operator's
// language rather than a benchmark's.
//
// The zero value is INVALID and Registry.Register refuses it. That is the
// whole point of the named type: an operator choosing between six models reads
// this field, and a plugin that leaves it blank has published a model nobody
// can choose deliberately. A blank description is therefore a registration
// failure that names the vendor and the model, not a field that quietly
// renders as an empty column.
//
// What the type cannot do is judge the CONTENT — "a model" satisfies it. The
// silent-empty case is the one a type can close, and it is closed.
type UsageDescription string

// String renders the description for CLI output.
func (d UsageDescription) String() string { return string(d) }

// Validate refuses a description that says nothing.
func (d UsageDescription) Validate() error {
	if strings.TrimSpace(string(d)) == "" {
		return fmt.Errorf("%w: the field says what the model is best used for, and an operator choosing between models reads it", ErrDescriptionEmpty)
	}
	return nil
}

// RankEvidence is one observation a capability ranking rests on.
//
// Both fields are required. The source repository carries a recorded decision
// that ceiling convenience — "this model is the biggest one we have, so rank
// it first" — is not a ranking argument, and a rank with no observation behind
// it is that argument with the reasoning left out.
type RankEvidence struct {
	// Source names where the observation came from: an eval, a published
	// card, a measured run in this repository's own history.
	Source string
	// Observation is what that source actually showed.
	Observation string
}

// CapabilityRank is a model's standing in its OWN vendor's lineup, with the
// evidence for it.
//
// Scores are per-vendor and never comparable across vendors: nothing in this
// module can honestly say a vendor's top model beats another vendor's, and a
// type that invited the comparison would get one made.
//
// # Why a score and not a position
//
// This type carried a tie-free POSITION until v0.2.0, and the ordering it
// described was true while the claim it made about EQUALITY was not. The board
// that owns the same lineup records a score with genuine ties in it —
// claude-haiku-4-5 and its dated snapshot are one model under two names, and
// two google rows are ranked by two different harnesses' catalogues against
// the same broker — and the ranking consumers on that side read the ties. A
// position cannot express one: numbering two equal models 7 and 8 states that
// 7 is better, which is a fact nobody observed.
//
// So the DECLARATION is a score, ties legal, and the total order callers
// sometimes need is DERIVED from it — see Lineup. That keeps the invented half
// (which of two equals is printed first) out of the declaration, where it could
// be mistaken for evidence.
type CapabilityRank struct {
	// Score is the vendor's capability score for the model: higher is more
	// capable, and two models of one vendor MAY share one. A tie is a
	// statement that the two are equal, which is why it is legal here and
	// refused nowhere.
	//
	// Zero and below are refused. The scale's own units are the vendor's
	// business — the ported rows run 10..120 — but a model with no score at
	// all has not been placed in the lineup, and the zero value must not be
	// able to pass for the bottom of it.
	Score int
	// Basis is the evidence for the score. At least one entry is required: a
	// score is a claim about capability, and a claim with no observation
	// behind it is policy wearing a number. This requirement did not change
	// when the position became a score, because it was never the position it
	// was about.
	Basis []RankEvidence
}

// Validate refuses a rank that is out of range or has no evidence behind it.
func (r CapabilityRank) Validate() error {
	if r.Score < 1 {
		return fmt.Errorf("%w: score %d is not a place in the vendor's lineup; a higher score is a more capable model and the zero value is not the bottom of the scale", ErrRankInvalid, r.Score)
	}
	if len(r.Basis) == 0 {
		return fmt.Errorf("%w: score %d carries no evidence, and a ranking with no observation behind it is policy wearing a number", ErrRankInvalid, r.Score)
	}
	for i, evidence := range r.Basis {
		if strings.TrimSpace(evidence.Source) == "" {
			return fmt.Errorf("%w: score %d evidence %d names no source", ErrRankInvalid, r.Score, i)
		}
		if strings.TrimSpace(evidence.Observation) == "" {
			return fmt.Errorf("%w: score %d evidence %d from %q records no observation", ErrRankInvalid, r.Score, i, evidence.Source)
		}
	}
	return nil
}

// Lifecycle is a model's state in its provider's lineup.
//
// It is DISPLAY AND MIGRATION EVIDENCE ONLY, and that restriction is carried
// verbatim from the board that declared it first: no admission path reads it,
// and the frozen v2 snapshot (v2snapshot.go) says in its own words that
// "lifecycle, supersession, recommendation and display order carry no
// admission meaning whatsoever". A legacy model is not a refused model; it is
// one an operator should not reach for by default.
//
// The zero value is INVALID. "Nobody said" and "the provider still ships it"
// are different facts, and a blank that rendered as an empty column would let
// the first pass for the second.
type Lifecycle string

const (
	// LifecycleCurrent marks a model in the provider's current lineup.
	LifecycleCurrent Lifecycle = "current"
	// LifecyclePreview marks a pre-general-availability model.
	LifecyclePreview Lifecycle = "preview"
	// LifecycleLegacy marks a superseded model that stays registered for
	// backward-compatible invocations and existing ceilings.
	LifecycleLegacy Lifecycle = "legacy"
)

// String renders the lifecycle for CLI output.
func (l Lifecycle) String() string { return string(l) }

// Validate refuses a blank lifecycle and any word outside the three states.
//
// The closed set is deliberate. A free-form string would accept "deprecated",
// "sunset" and "eol" as three spellings of one state, and the day something
// switched on it, two of them would fall through.
func (l Lifecycle) Validate() error {
	switch l {
	case LifecycleCurrent, LifecyclePreview, LifecycleLegacy:
		return nil
	case "":
		return fmt.Errorf("%w: the model declares no lineup state; %q, %q and %q are the three, and a blank one renders as an empty column rather than as a fact",
			ErrLifecycleInvalid, LifecycleCurrent, LifecyclePreview, LifecycleLegacy)
	default:
		return fmt.Errorf("%w: %q is not one of %q, %q or %q", ErrLifecycleInvalid, string(l), LifecycleCurrent, LifecyclePreview, LifecycleLegacy)
	}
}

// PricingPlan is one subscription tier of a vendor's billing contract and the
// quota it publishes.
type PricingPlan struct {
	// Name is the vendor's own name for the tier, unique within a contract.
	Name string
	// MonthlyUSD is the regular monthly list price. It must be finite and
	// non-negative.
	MonthlyUSD float64
	// PromotionalMonthlyUSD is the current limited-time price, nil when the
	// vendor publishes none. A pointer rather than a zero float because a
	// promotion AT zero and no promotion at all are different offers.
	// A present value must be finite, non-negative, and below MonthlyUSD.
	PromotionalMonthlyUSD *float64
	// MonthlyCredits is the tier's published monthly credit allowance.
	MonthlyCredits int
	// ApplicableModelIDs is the allowlist the tier prices. It must name the
	// model the contract is attached to: a plan that prices five models and
	// hangs off a sixth is a billing claim about a model nobody sold.
	ApplicableModelIDs []ModelID
}

// Pricing is the vendor billing contract attached to a model.
//
// A NIL Pricing means the vendor's billing terms were never registered for the
// row — which is most of them. It does NOT mean the model is free, and nothing
// in this module may read a missing contract as a zero price: the board this
// was ported from states the same rule about its own subscription rows, whose
// per-token values are zero in client configuration precisely because the
// billing is credits-based.
type Pricing struct {
	// BillingModel is the stable identifier of the billing contract.
	BillingModel string
	// Edition is the vendor product edition whose allowlist and plans this
	// contract represents.
	Edition string
	// Currency is the ISO 4217 code the plan prices are quoted in.
	Currency string
	// QuotaPeriod is the vendor's quota reset period.
	QuotaPeriod string
	// HasFrequencyLimits says whether shorter rolling quota windows also apply.
	// It is a plain bool because false here is a positive finding — the vendor
	// publishes no shorter window — rather than an unread field.
	HasFrequencyLimits bool
	// Plans are the current subscription tiers. At least one is required: a
	// contract with no tier prices nothing.
	Plans []PricingPlan
	// SourceURL is the vendor pricing page the contract was registered from.
	// Required: a price nobody can check is a number this module made up.
	SourceURL string
	// AsOf is the YYYY-MM-DD date the vendor data was retrieved. Required for
	// the same reason SourceURL is — a price with no date is not a price, it
	// is a rumour.
	AsOf string
}

// asOfPattern is the retrieval-date shape. A free-form date string would
// accept "last July", and the field's whole job is to let a reader tell a
// stale price from a fresh one.
var asOfPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Validate refuses a billing contract that could not be quoted to anyone.
//
// The model id is passed in rather than read from a back-pointer because a
// contract does not own the row it hangs off; what it must do is NAME that row
// in at least one plan's allowlist.
func (p *Pricing) Validate(model ModelID) error {
	if p == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"billing model", p.BillingModel},
		{"edition", p.Edition},
		{"currency", p.Currency},
		{"quota period", p.QuotaPeriod},
		{"source URL", p.SourceURL},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: model %q's billing contract names no %s", ErrPricingInvalid, model, field.name)
		}
	}
	if !asOfPattern.MatchString(p.AsOf) {
		return fmt.Errorf("%w: model %q's billing contract records the retrieval date %q, which is not a YYYY-MM-DD date; a price with no date cannot be told from a stale one",
			ErrPricingInvalid, model, p.AsOf)
	}
	if len(p.Plans) == 0 {
		return fmt.Errorf("%w: model %q's billing contract publishes no plan, so it prices nothing", ErrPricingInvalid, model)
	}
	// The plan names are scanned against a slice rather than collected into a
	// set for the reason Model.Validate scans its systems: a handful of rows
	// costs nothing, and a map keyed by anything id-shaped is what the module
	// guard is for.
	seen := make([]string, 0, len(p.Plans))
	pricesThisModel := false
	for i, plan := range p.Plans {
		if strings.TrimSpace(plan.Name) == "" {
			return fmt.Errorf("%w: model %q's billing contract has an unnamed plan at index %d", ErrPricingInvalid, model, i)
		}
		for _, already := range seen {
			if already == plan.Name {
				return fmt.Errorf("%w: model %q's billing contract declares the plan %q twice", ErrPricingInvalid, model, plan.Name)
			}
		}
		seen = append(seen, plan.Name)
		// Ordinary ordering comparisons admit NaN, while infinities can
		// masquerade as non-negative prices. Refuse every non-finite value at
		// this canonical validation boundary so neither vendor registration nor
		// declaration-owned system-only authority can persist it.
		if math.IsNaN(plan.MonthlyUSD) || math.IsInf(plan.MonthlyUSD, 0) {
			return fmt.Errorf("%w: model %q's plan %q lists a non-finite monthly price of %v", ErrPricingInvalid, model, plan.Name, plan.MonthlyUSD)
		}
		if plan.MonthlyUSD < 0 {
			return fmt.Errorf("%w: model %q's plan %q lists a monthly price of %v", ErrPricingInvalid, model, plan.Name, plan.MonthlyUSD)
		}
		if plan.MonthlyCredits < 0 {
			return fmt.Errorf("%w: model %q's plan %q publishes %d monthly credits", ErrPricingInvalid, model, plan.Name, plan.MonthlyCredits)
		}
		if plan.PromotionalMonthlyUSD != nil {
			promotional := *plan.PromotionalMonthlyUSD
			if math.IsNaN(promotional) || math.IsInf(promotional, 0) {
				return fmt.Errorf("%w: model %q's plan %q lists a non-finite promotional price of %v", ErrPricingInvalid, model, plan.Name, promotional)
			}
			if promotional < 0 {
				return fmt.Errorf("%w: model %q's plan %q lists a promotional price of %v", ErrPricingInvalid, model, plan.Name, promotional)
			}
			// A "promotion" at or above list is the one shape that would
			// quietly overstate a discount everywhere this is displayed.
			if promotional >= plan.MonthlyUSD {
				return fmt.Errorf("%w: model %q's plan %q promotes %v against a list price of %v; a promotional price at or above the list price is not a promotion",
					ErrPricingInvalid, model, plan.Name, promotional, plan.MonthlyUSD)
			}
		}
		if len(plan.ApplicableModelIDs) == 0 {
			return fmt.Errorf("%w: model %q's plan %q prices no model", ErrPricingInvalid, model, plan.Name)
		}
		for _, id := range plan.ApplicableModelIDs {
			if err := ValidateModelID(id); err != nil {
				return fmt.Errorf("%w: model %q's plan %q: %w", ErrPricingInvalid, model, plan.Name, err)
			}
			if id == model {
				pricesThisModel = true
			}
		}
	}
	if !pricesThisModel {
		return fmt.Errorf("%w: model %q carries a billing contract whose plans price %v and never name it; a contract attached to a model it does not cover is a price for something else",
			ErrPricingInvalid, model, pricedModelIDs(p))
	}
	return nil
}

// pricedModelIDs renders every id a contract's plans price, for the refusal
// text. A refusal that said only "it does not cover this model" would leave the
// reader to open the declaration to find out what it does cover.
func pricedModelIDs(p *Pricing) []string {
	var ids []string
	for _, plan := range p.Plans {
		for _, id := range plan.ApplicableModelIDs {
			known := false
			for _, already := range ids {
				if already == string(id) {
					known = true
				}
			}
			if !known {
				ids = append(ids, string(id))
			}
		}
	}
	return ids
}

// Clone deep-copies a billing contract, including the promotional price behind
// each plan's pointer. A shallow copy would hand every caller the same *float64
// the declaration holds.
func (p *Pricing) Clone() *Pricing {
	if p == nil {
		return nil
	}
	copied := *p
	copied.Plans = make([]PricingPlan, 0, len(p.Plans))
	for _, plan := range p.Plans {
		copiedPlan := plan
		copiedPlan.ApplicableModelIDs = append([]ModelID(nil), plan.ApplicableModelIDs...)
		if plan.PromotionalMonthlyUSD != nil {
			promotional := *plan.PromotionalMonthlyUSD
			copiedPlan.PromotionalMonthlyUSD = &promotional
		}
		copied.Plans = append(copied.Plans, copiedPlan)
	}
	return &copied
}

// EffortDeclaration is the per-model reasoning-effort axis as the VENDOR owns
// it: whether the model has one, which words it accepts, and which word the
// vendor recommends.
//
// The transport — argv, stdin, or none — is the agentic system's declaration
// and is not repeated here. A launch needs both halves and gets them from
// their own owners: this package refuses an effort word outside the
// vocabulary, and agentic.BuildPlan refuses a system that cannot carry one.
//
// Recommended is a DECLARATION, not a default. Nothing in this package
// substitutes it for a missing effort: invariant 4 of docs/architecture.md
// makes effort required-or-absent precisely so that no default is injected
// anywhere, and a launch that silently ran at the vendor's recommendation
// instead of the operator's choice is the wrong-cost launch that invariant
// exists to close. The recommendation reaches the operator through the refusal
// text instead.
type EffortDeclaration struct {
	Support     agentic.EffortSupport
	Vocabulary  []string
	Recommended string
}

// Validate refuses a declaration that could not be honoured: a required axis
// with no words, a recommendation outside its own vocabulary, or a vocabulary
// attached to a model that has no effort axis at all.
func (e EffortDeclaration) Validate() error {
	switch e.Support {
	case agentic.EffortSupportRequired:
		if len(e.Vocabulary) == 0 {
			return fmt.Errorf("%w: the model requires an explicit effort and declares no words for one", ErrEffortDeclaration)
		}
		seen := map[string]bool{}
		for _, word := range e.Vocabulary {
			if strings.TrimSpace(word) == "" {
				return fmt.Errorf("%w: the vocabulary carries a blank word", ErrEffortDeclaration)
			}
			if seen[word] {
				return fmt.Errorf("%w: the vocabulary repeats %q", ErrEffortDeclaration, word)
			}
			seen[word] = true
		}
		if !e.Accepts(e.Recommended) {
			return fmt.Errorf("%w: the recommended effort %q is not one of %v", ErrEffortDeclaration, e.Recommended, e.Vocabulary)
		}
	case agentic.EffortSupportNone:
		if len(e.Vocabulary) > 0 || strings.TrimSpace(e.Recommended) != "" {
			return fmt.Errorf("%w: the model declares no effort axis but carries a vocabulary %v and a recommendation %q", ErrEffortDeclaration, e.Vocabulary, e.Recommended)
		}
	default:
		return fmt.Errorf("%w: %s is not a declared effort support", ErrEffortDeclaration, e.Support)
	}
	return nil
}

// Accepts reports whether a word is in this model's vocabulary. Comparison is
// exact on the trimmed word: effort vocabularies are short closed sets the
// vendor publishes, and folding "High" into "high" here would be a second
// normalization of a value this package does not own.
func (e EffortDeclaration) Accepts(word string) bool {
	trimmed := strings.TrimSpace(word)
	if trimmed == "" {
		return false
	}
	for _, declared := range e.Vocabulary {
		if declared == trimmed {
			return true
		}
	}
	return false
}

// SameAxis reports whether two declarations state the same effort contract:
// the same support, the same words in the same ORDER, and the same
// recommendation.
//
// Order matters here and does not in sameSystems, and the asymmetry is the
// point. A vocabulary is published, printed and offered to an operator in the
// order the vendor wrote it — the refusal text for an unknown word names it
// verbatim — so two orders are two different things a reader is told. A system
// list carries no such ordering claim.
func (e EffortDeclaration) SameAxis(other EffortDeclaration) bool {
	if e.Support != other.Support || e.Recommended != other.Recommended {
		return false
	}
	if len(e.Vocabulary) != len(other.Vocabulary) {
		return false
	}
	for i, word := range e.Vocabulary {
		if other.Vocabulary[i] != word {
			return false
		}
	}
	return true
}

// Model is one row of a vendor's model list: what it is called, what it is for,
// where it sits in the vendor's lineup and on what evidence, its lineup state,
// its effort axis, its context window, its billing contract, and the agentic
// systems that can drive it. Cache capacity is an optional declared fact: it
// is never inferred from any of those identities or from launch/runtime state.
//
// # The emptiness rules, in one place
//
// The following fields have a legal empty value, and each empty means
// something different, so each says what:
//
//   - SupersededBy empty: the registry records NO unambiguous same-family
//     replacement. That is not "there is none" — the Codex lineup renamed its
//     tiers rather than versioning them, so several legacy rows have a
//     successor nobody can name without guessing.
//   - Recommended false: the row is not this vendor's display pick for its
//     harness. It is a real value, not an unset one, and most rows carry it.
//   - ContextWindowTokens zero: no context window was recorded for the row. It
//     is NOT a window of zero tokens, and nothing may compute against it.
//   - CacheBudgetBytes nil: no cache budget was recorded for the row. It is
//     NOT an explicit zero-byte budget; a present value must be positive.
//   - Pricing nil: no billing contract was registered. It is NOT free use.
//
// The two fields with NO legal empty value are Lifecycle and Rank, and both
// refuse their zero value at registration rather than rendering it.
type Model struct {
	ID          ModelID
	Description UsageDescription
	Rank        CapabilityRank
	Lifecycle   Lifecycle
	Effort      EffortDeclaration

	// SupersededBy names the successor model when the vendor records an
	// unambiguous same-family replacement, and is empty otherwise. A row that
	// names one must be LEGACY: a current or preview model that has already
	// been replaced is a row contradicting itself, and the contradiction is
	// refused rather than displayed.
	//
	// Registry.Register additionally requires the successor to be a model the
	// SAME vendor declares. A successor nobody registers is a dangling pointer
	// an operator would follow to nothing.
	SupersededBy ModelID

	// AliasOf names the model this row is a SHORT SPELLING OF, and is empty
	// when the row is its own identity. An alias is a real, launchable row —
	// it is admitted, ranked, displayed and audited under its own id — and it
	// EXECUTES under the target: agentic.BuildPlan substitutes the target into
	// the launch request before any plugin surface sees it.
	//
	// The field exists because a floating alias is a name the operator uses and
	// the provider's backend does not have. Measured, not assumed: a spawn of
	// `muse-spark` at effort high reached muse's argv verbatim and the backend
	// refused it with "model muse-spark does not exist or you lack access",
	// while muse-spark-1.3-contributor ran the identical launch end to end.
	//
	// It is DECLARED, never derived. Nothing in this module infers an alias
	// from a shared prefix, a version suffix, an equal capability score or a
	// shared description: those are all spellings, and a launch redirected by a
	// spelling rule is a launch nobody authorized.
	//
	// checkAliases refuses a row this cannot be true of — see it for the four
	// rules and for what they deliberately do not cover.
	AliasOf ModelID

	// Recommended is the vendor's DISPLAY pick, and never a launch default.
	// Nothing in this module substitutes a recommended model for an unstated
	// one, for the same reason EffortDeclaration.Recommended is never
	// substituted for a missing effort: a launch that silently ran the display
	// pick instead of the operator's choice is the wrong-cost launch.
	//
	// At most one of a vendor's models may carry it PER AGENTIC SYSTEM, which
	// Registry.Register enforces. Per system rather than per vendor because a
	// vendor driving two harnesses has two lineups to pick from — google
	// recommends one row for gemini-cli and another for antigravity — and one
	// pick across both would leave one harness's operators with a
	// recommendation they cannot run.
	Recommended bool

	// ContextWindowTokens is the provider's maximum context window. Zero means
	// none was recorded; a negative is refused.
	ContextWindowTokens int

	// CacheBudgetBytes is the configured model-cache capacity in bytes, or nil
	// when the catalog records no such fact. A pointer preserves the distinction
	// between an omitted declaration and an explicit zero; zero and negative
	// values are refused. This is catalog metadata only. It does not shape a
	// launch and must never be inferred from model/publisher/family names,
	// context size, argv, availability or runtime status.
	CacheBudgetBytes *int64

	// Pricing is the vendor billing contract, or nil when none was registered.
	Pricing *Pricing

	// Publisher and Family are provenance, display and audit fields only —
	// never an admission input. Neither is read by Registry.Register, by
	// BuildLaunch's resolution, or by Vendor.Spawn/Availability: naming stays
	// orthogonal by construction, so a model's publisher or family can never
	// gate a launch. Empty is legal for both and means the fact was not
	// recorded, not that the model has none.
	Publisher string
	Family    string

	// Engine is the concrete inference-engine graph dependency required by
	// this model. The zero value preserves models with no engine requirement.
	// Publisher/family/model spelling never infer or replace this reference.
	Engine plugin.Ref

	// Systems are the agentic systems that can drive this model. It must be
	// non-empty — a model no harness can run is not a launchable declaration —
	// and every id must be registered in the agentic registry the vendor
	// registry was built against. That check is the dependency direction, and
	// Registry.Register is where it happens.
	Systems []agentic.SystemID
}

// Validate refuses a row that could not be launched or chosen. It does NOT
// check that the declared systems are registered, nor that a named successor
// is a registered model: both need a registry and belong to
// Registry.Register, which names both ids when it refuses.
func (m Model) Validate() error {
	if err := ValidateModelID(m.ID); err != nil {
		return err
	}
	if err := validateInferenceEngineRef("model "+m.ID.String(), m.Engine); err != nil {
		return err
	}
	if err := m.Description.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if err := m.Rank.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if err := m.Lifecycle.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if err := m.Effort.Validate(); err != nil {
		return fmt.Errorf("model %q: %w", m.ID, err)
	}
	if m.SupersededBy != "" {
		if err := ValidateModelID(m.SupersededBy); err != nil {
			return fmt.Errorf("%w: model %q names a successor that is not a usable model id: %w", ErrSupersessionInvalid, m.ID, err)
		}
		if m.SupersededBy == m.ID {
			return fmt.Errorf("%w: model %q names itself as its own successor", ErrSupersessionInvalid, m.ID)
		}
		if m.Lifecycle != LifecycleLegacy {
			return fmt.Errorf("%w: model %q is %s and names %q as its successor; a model that has already been replaced is legacy, and the two fields must not say different things",
				ErrSupersessionInvalid, m.ID, m.Lifecycle, m.SupersededBy)
		}
	}
	if m.AliasOf != "" {
		if err := ValidateModelID(m.AliasOf); err != nil {
			return fmt.Errorf("%w: model %q names an alias target that is not a usable model id: %w", ErrAliasInvalid, m.ID, err)
		}
		if m.AliasOf == m.ID {
			return fmt.Errorf("%w: model %q names itself as its own alias target", ErrAliasInvalid, m.ID)
		}
	}
	if m.ContextWindowTokens < 0 {
		return fmt.Errorf("%w: model %q declares a context window of %d tokens; zero means none was recorded and a negative means nothing at all",
			ErrModelInvalid, m.ID, m.ContextWindowTokens)
	}
	if m.CacheBudgetBytes != nil && *m.CacheBudgetBytes <= 0 {
		return fmt.Errorf("%w: model %q declares a cache budget of %d bytes; nil means none was recorded and a present value must be positive",
			ErrModelInvalid, m.ID, *m.CacheBudgetBytes)
	}
	if err := m.Pricing.Validate(m.ID); err != nil {
		return err
	}
	if len(m.Systems) == 0 {
		return fmt.Errorf("%w: model %q declares no agentic system that can drive it", ErrModelInvalid, m.ID)
	}
	// The declared systems are checked for duplicates against a slice rather
	// than a set keyed by SystemID. A map[SystemID]T anywhere outside the
	// agentic registry is what pkg/agentic/singlesource_guard_test.go fails
	// the build over, and the rule is right to be blunt about it: a set of
	// "systems this thing supports" is one refactor away from being a table of
	// what each of them does. A model declares a handful of systems, so the
	// scan costs nothing.
	seen := make([]agentic.SystemID, 0, len(m.Systems))
	for _, raw := range m.Systems {
		normalized, err := agentic.NormalizeSystemID(string(raw))
		if err != nil {
			return fmt.Errorf("%w: model %q declares an unusable agentic system: %w", ErrModelInvalid, m.ID, err)
		}
		if normalized != raw {
			return fmt.Errorf("%w: model %q declares agentic system %q, which normalizes to %q; declare the normalized spelling or the two name one system twice",
				ErrModelInvalid, m.ID, raw, normalized)
		}
		for _, already := range seen {
			if already == normalized {
				return fmt.Errorf("%w: model %q declares agentic system %q twice", ErrModelInvalid, m.ID, normalized)
			}
		}
		seen = append(seen, normalized)
	}
	return nil
}

// DrivenBy reports whether this model declared the given agentic system.
func (m Model) DrivenBy(id agentic.SystemID) bool {
	normalized, err := agentic.NormalizeSystemID(string(id))
	if err != nil {
		return false
	}
	for _, declared := range m.Systems {
		if declared == normalized {
			return true
		}
	}
	return false
}

// checkSupersession refuses a successor no model in the same list answers to.
//
// It is list-wide rather than per-row because that is what makes it a check at
// all: Model.Validate can see that "claude-opus-5" is a usable id, and only the
// list can see whether anybody declares it. A dangling successor is the shape
// an operator follows to nothing — the field's entire job is to hand them the
// next model to use.
//
// Same LIST rather than same vendor is deliberate: it is the rule a
// vendor-unresolved runtime's own rows are held to as well, and one function
// enforcing it in both places is one behaviour rather than two that drift.
func checkSupersession(models []Model) error {
	for _, model := range models {
		if model.SupersededBy == "" {
			continue
		}
		declared := false
		for _, candidate := range models {
			if candidate.ID == model.SupersededBy {
				declared = true
			}
		}
		if !declared {
			return fmt.Errorf("%w: model %q names %q as its successor and no model in the same lineup answers to that id; an operator following the field would find nothing",
				ErrSupersessionInvalid, model.ID, model.SupersededBy)
		}
	}
	return nil
}

// checkAliases refuses an alias row that could not mean what it says.
//
// It is list-wide for the same reason checkSupersession is: Model.Validate can
// see that "muse-spark-1.3-contributor" is a usable id, and only the list can
// see whether anybody declares it. Same LIST rather than same vendor, so one
// function holds a registered vendor's rows and a vendor-unresolved runtime's
// own rows to one rule — which matters here because the only alias in the
// module today lives in the second home.
//
// The four rules, and what each one closes:
//
//   - The target must be DECLARED IN THE SAME LIST. An alias pointing outside
//     it would substitute an id this registry cannot admit, rank or price, and
//     the launch would be the first thing to discover that.
//   - The target must not itself be an ALIAS. One hop is the whole contract:
//     agentic.Model.LaunchIdentity resolves exactly once, so a chain would
//     silently launch the middle of it.
//   - The alias must mirror the target's EFFORT declaration. The effort word is
//     validated against the REQUESTED row's vocabulary and then transported to
//     a process running the TARGET; a narrower or wider vocabulary on either
//     side means an accepted word the executing model refuses, or a refused
//     word it accepts.
//   - The alias must mirror the target's SYSTEMS. The harness is chosen from
//     the requested row and runs the target, so a row driving a system its
//     target never declared would launch the target on a harness nobody has
//     evidence runs it.
//
// STATED BOUND, so nobody reads this checker as covering more than it does: it
// does NOT hold the two rows' rank, lifecycle, context window, pricing,
// recommendation or description equal. Those are presentation and catalogue
// facts, none of them reaches argv or the admitted-pair digest, and pinning
// them here would be this checker asserting an editorial rule rather than a
// launch invariant. A pair that disagrees on them is legal and unchecked.
func checkAliases(models []Model) error {
	for _, model := range models {
		if model.AliasOf == "" {
			continue
		}
		var target Model
		targetDeclared := false
		for _, candidate := range models {
			if candidate.ID == model.AliasOf {
				target = candidate
				targetDeclared = true
			}
		}
		if !targetDeclared {
			return fmt.Errorf("%w: model %q is an alias of %q and no model in the same lineup answers to that id; a launch would substitute an identity this registry does not declare",
				ErrAliasInvalid, model.ID, model.AliasOf)
		}
		if target.AliasOf != "" {
			return fmt.Errorf("%w: model %q is an alias of %q, which is itself an alias of %q; resolution is one hop and a chain would launch the middle of it",
				ErrAliasInvalid, model.ID, target.ID, target.AliasOf)
		}
		if !model.Effort.SameAxis(target.Effort) {
			return fmt.Errorf("%w: model %q is an alias of %q and declares effort %s%v recommending %q while %q declares %s%v recommending %q; the word is validated against the alias and transported to the target",
				ErrAliasInvalid, model.ID, target.ID,
				model.Effort.Support, model.Effort.Vocabulary, model.Effort.Recommended,
				target.ID, target.Effort.Support, target.Effort.Vocabulary, target.Effort.Recommended)
		}
		if !sameSystems(model.Systems, target.Systems) {
			return fmt.Errorf("%w: model %q is an alias of %q and declares systems %v while %q declares %v; the harness is chosen from the alias and runs the target",
				ErrAliasInvalid, model.ID, target.ID, model.Systems, target.ID, target.Systems)
		}
	}
	return nil
}

// sameSystems reports whether two rows can be driven by exactly the same set of
// harnesses.
//
// Order-insensitive, because declaration order is presentation: two rows that
// list one system pair in two orders are drivable in the same places, and
// refusing that would be a style rule wearing a launch rule's error message.
// Scanned against slices rather than collected into a map[agentic.SystemID]bool
// for the reason Model.Validate already states.
func sameSystems(left, right []agentic.SystemID) bool {
	if len(left) != len(right) {
		return false
	}
	contains := func(haystack []agentic.SystemID, needle agentic.SystemID) bool {
		for _, candidate := range haystack {
			if candidate == needle {
				return true
			}
		}
		return false
	}
	for _, system := range left {
		if !contains(right, system) {
			return false
		}
	}
	return true
}

// checkRecommendations refuses two display picks for one agentic system.
//
// The pairs are scanned against a slice rather than collected into a
// map[agentic.SystemID]ModelID. That is not a style preference: a
// SystemID-keyed table in this package is exactly what
// pkg/agentic/singlesource_guard_test.go fails the build over, and the rule is
// right — a map of "which model is recommended per system" is one refactor
// away from being a table of what each system does. A vendor declares a
// handful of systems, so the scan costs nothing.
func checkRecommendations(models []Model) error {
	type pick struct {
		system agentic.SystemID
		model  ModelID
	}
	var picks []pick
	for _, model := range models {
		if !model.Recommended {
			continue
		}
		for _, system := range model.Systems {
			for _, already := range picks {
				if already.system == system {
					return fmt.Errorf("%w: %q and %q are both recommended for %q; a display pick that names two rows picks nothing, and the surfaces that read it would choose by iteration order",
						ErrRecommendationAmbiguous, already.model, model.ID, system)
				}
			}
			picks = append(picks, pick{system: system, model: model.ID})
		}
	}
	return nil
}

// Launchable projects the row onto the one vendor-shaped fact Layer 1 needs:
// the selected model and whether it requires an effort. Everything else — the
// description, the rank, the vocabulary — stays here, which is what keeps the
// two layers from re-declaring each other.
func (m Model) Launchable() agentic.Model {
	return agentic.Model{ID: string(m.ID), Effort: m.Effort.Support, AliasOf: string(m.AliasOf)}
}

// AvailabilityQuery is what a caller wants an availability verdict about.
//
// Model narrows the question to one row; empty asks about the vendor as a
// whole. The distinction is not cosmetic and it is the seam the local-model
// resource plane needs: a remote vendor answers per account, a local one
// answers per model, because whether a model is loaded and whether inference
// is running on it are per-model facts.
//
// Home is the harness configuration home the on-disk limit state is keyed by
// (invariant 2: IdentityKey(provider, home)). A caller asking about a
// non-default home must be able to say so, or the verdict is about a different
// state file than the launch will use.
type AvailabilityQuery struct {
	Model ModelID
	Home  string

	// Runtime says which declared RuntimeID is asking, for a vendor serving
	// more than one RuntimeID under one VendorID — google already serves both
	// gemini and agy this way. A vendor with only one runtime may ignore it;
	// a vendor with more than one uses it to disambiguate which of its own
	// declared runtimes' facts a caller wants, the same way Spawn's own
	// per-runtime lookup does. Empty is legal and means the caller did not
	// disambiguate.
	Runtime RuntimeID
}

// SpawnContext is everything a vendor gets for one launch: the resolved
// runtime, the model row that was selected from its own list, the effort value
// that survived vocabulary validation, and the caller's request.
//
// A vendor is handed the resolution rather than performing it, so a plugin
// cannot select a different model, a different harness or an unvalidated
// effort than the one the registry admitted.
type SpawnContext struct {
	Runtime Runtime
	Model   Model
	Effort  string
	Request SpawnRequest
}

// Vendor is the vendor plugin contract.
//
// Every method must be free of side effects other than reading what it was
// handed: BuildLaunch drives all of them to produce a plan, including in
// agentic.LaunchModeDryRun, and a dry run that authenticates, spends quota or
// starts a process is not a dry run.
type Vendor interface {
	// ID is the plugin's identity. It must be stable forever and must
	// normalize to itself. Registry.Register enforces the part of that a
	// registration-time check can reach — it reads ID() twice and refuses a
	// plugin that disagrees with itself, and refuses one whose id is not its
	// own normal form — exactly as agentic.Registry does for systems, and with
	// the same bound: stability past registration is contract, not enforcement.
	ID() VendorID

	// Models is the vendor's model list: capability rank with its evidence, a
	// usage description, the effort vocabulary and recommendation, and the
	// agentic systems that can drive each row. It must not vary between calls
	// — a registry that admitted a list and a launch that reads a different
	// one is the two-tables disease with the second table inside the plugin.
	Models() []Model

	// Availability is the structured verdict: healthy, limited until a stated
	// time, unreachable, or unknown — each with the evidence it rests on. It
	// is deliberately not a boolean; see availability.go.
	//
	// An error return means the verdict itself could not be produced, which is
	// a different fact from a verdict of unknown: unknown is an answer with
	// evidence about what was checked, an error is the absence of an answer.
	Availability(q AvailabilityQuery) (Availability, error)

	// Spawn is the vendor's half of one launch: it turns the resolved runtime,
	// model and effort into the agentic.LaunchRequest Layer 1 will plan. It
	// performs NO execution and no I/O.
	//
	// What it may add is vendor-owned: authentication environment, a
	// configuration home, whatever the vendor's API needs in the child. What
	// it may not do is redirect the launch — BuildLaunch refuses a request
	// that names a different system, model, effort, goal, budget, service tier
	// or composition than the one that was admitted, because a plugin that
	// could silently change any of those makes the admission meaningless.
	Spawn(sc SpawnContext) (agentic.LaunchRequest, error)
}
