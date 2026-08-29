package vendorplugin

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The refusals that came with the board facts in v0.2.0.
//
// Every field added then either has no legal empty value or has one that MEANS
// something, and each is held here at BOTH ends: Model.Validate, and the
// production call site that has to refuse it — Registry.Register for a vendor's
// rows, RuntimeDeclaration.Validate for a vendor-unresolved runtime's.
//
// Testing only Model.Validate would be testing the type. The registry is what a
// binary actually calls, and a check that exists in a method nobody reaches
// from the registration path promises nothing.

// TestRegisterRefusesAModelWithNoLineupState covers the field with no legal
// empty value.
//
// The zero value is the case that matters. A blank lifecycle would render as an
// empty column in every listing and read as "nobody said", and "nobody said"
// and "the provider still ships it" are different facts.
func TestRegisterRefusesAModelWithNoLineupState(t *testing.T) {
	cases := map[string]Lifecycle{
		"no lineup state at all":     "",
		"whitespace where one goes":  "  ",
		"a state nobody declared":    "deprecated",
		"a near-miss of a real one":  "Current",
		"a plausible fourth state":   "sunset",
		"the empty-looking sentinel": "unknown",
	}
	for name, lifecycle := range cases {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			vendor.models[0].Lifecycle = lifecycle

			if err := vendor.models[0].Validate(); err == nil {
				t.Fatalf("Model.Validate admitted a model with %s", name)
			}
			_, err := mustRegister(t, vendor, systemsWithPangolin(t))
			requireErrorIs(t, err, ErrLifecycleInvalid, "Register(model with "+name+")")
		})
	}
}

// TestRegisterAdmitsEveryDeclaredLineupState is the positive half, and it is
// here so the refusal above cannot be satisfied by a check that refuses
// everything.
func TestRegisterAdmitsEveryDeclaredLineupState(t *testing.T) {
	for _, lifecycle := range []Lifecycle{LifecycleCurrent, LifecyclePreview, LifecycleLegacy} {
		t.Run(string(lifecycle), func(t *testing.T) {
			vendor := newNarwhal()
			vendor.models[0].Lifecycle = lifecycle

			if _, err := mustRegister(t, vendor, systemsWithPangolin(t)); err != nil {
				t.Fatalf("Register(model marked %s): %v", lifecycle, err)
			}
		})
	}
}

// TestRegisterRefusesAnUnusableSupersession covers the field whose empty value
// is legal and whose non-empty value has to hold together.
//
// The last case is the one a type cannot catch on its own: a successor that IS
// a usable model id and that nobody declares. An operator reading the field
// follows it to a model, and a dangling one sends them nowhere.
func TestRegisterRefusesAnUnusableSupersession(t *testing.T) {
	cases := map[string]func(*narwhalVendor){
		"a successor that is not a usable id": func(v *narwhalVendor) {
			v.models[0].Lifecycle = LifecycleLegacy
			v.models[0].SupersededBy = "narwhal flat"
		},
		"a model naming itself": func(v *narwhalVendor) {
			v.models[0].Lifecycle = LifecycleLegacy
			v.models[0].SupersededBy = v.models[0].ID
		},
		"a current model that has already been replaced": func(v *narwhalVendor) {
			v.models[0].Lifecycle = LifecycleCurrent
			v.models[0].SupersededBy = v.models[1].ID
		},
		"a preview model that has already been replaced": func(v *narwhalVendor) {
			v.models[0].Lifecycle = LifecyclePreview
			v.models[0].SupersededBy = v.models[1].ID
		},
		"a successor no model in the lineup answers to": func(v *narwhalVendor) {
			v.models[0].Lifecycle = LifecycleLegacy
			v.models[0].SupersededBy = "narwhal-shallow"
		},
	}
	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			apply(vendor)

			_, err := mustRegister(t, vendor, systemsWithPangolin(t))
			requireErrorIs(t, err, ErrSupersessionInvalid, "Register(vendor with "+name+")")
		})
	}
}

// TestRegisterAdmitsALegacyModelNamingADeclaredSuccessor is the shape the
// ported rows actually use, so the refusals above cannot be passing by
// forbidding supersession outright.
func TestRegisterAdmitsALegacyModelNamingADeclaredSuccessor(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[1].Lifecycle = LifecycleLegacy
	vendor.models[1].SupersededBy = vendor.models[0].ID

	if _, err := mustRegister(t, vendor, systemsWithPangolin(t)); err != nil {
		t.Fatalf("Register(legacy model naming a declared successor): %v", err)
	}
}

// TestRegisterRefusesTwoRecommendationsForOneSystem holds the display pick to
// one row per harness.
//
// A pick that names two rows picks nothing, and every surface that reads it
// would then choose by iteration order — which is to say, at random, differently
// per Go release.
func TestRegisterRefusesTwoRecommendationsForOneSystem(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[0].Recommended = true
	vendor.models[1].Recommended = true

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrRecommendationAmbiguous, "Register(vendor recommending two models for one system)")
	requireMentions(t, err, "narwhal-deep", "narwhal-flat", string(pangolinID))
}

// TestRegisterAdmitsOneRecommendationPerSystem is the narrowing half: the rule
// is per SYSTEM, not per vendor, so a vendor driving two harnesses may pick one
// row for each. google does exactly this in production, and a per-vendor rule
// would have left one of its two harnesses with a recommendation it cannot run.
func TestRegisterAdmitsOneRecommendationPerSystem(t *testing.T) {
	const secondSystem = agentic.SystemID("aardvark")
	systems := systemsNamed(t, pangolinID, secondSystem)

	vendor := newNarwhal()
	vendor.models[0].Recommended = true
	vendor.models[1].Recommended = true
	vendor.models[1].Systems = []agentic.SystemID{secondSystem}

	if _, err := mustRegister(t, vendor, systems); err != nil {
		t.Fatalf("Register(vendor recommending one model per harness): %v; the rule is per system and this vendor drives two", err)
	}
}

// TestRegisterRefusesANegativeContextWindow keeps zero meaning "not recorded".
//
// Zero is a legal value with a stated meaning, so the refusal cannot be "falsy
// is refused"; it is specifically the values that could not be a window.
func TestRegisterRefusesANegativeContextWindow(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[0].ContextWindowTokens = -1

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrModelInvalid, "Register(model with a negative context window)")

	t.Run("zero is not recorded rather than refused", func(t *testing.T) {
		clean := newNarwhal()
		clean.models[0].ContextWindowTokens = 0
		if _, err := mustRegister(t, clean, systemsWithPangolin(t)); err != nil {
			t.Fatalf("Register(model with no recorded context window): %v; most ported rows have none and zero is the way that is said", err)
		}
	})
}

// TestRegisterRefusesAnUnusablePricingContract covers every way a billing
// contract can fail to be quotable.
//
// The promotional case is the one worth naming: a "promotion" at or above the
// list price is the single shape that would overstate a discount everywhere the
// contract is displayed, and it is the shape a copy-paste actually produces.
func TestRegisterRefusesAnUnusablePricingContract(t *testing.T) {
	cases := map[string]func(*Pricing){
		"no billing model":                  func(p *Pricing) { p.BillingModel = "" },
		"no edition":                        func(p *Pricing) { p.Edition = "  " },
		"no currency":                       func(p *Pricing) { p.Currency = "" },
		"no quota period":                   func(p *Pricing) { p.QuotaPeriod = "" },
		"no source URL":                     func(p *Pricing) { p.SourceURL = "   " },
		"no retrieval date":                 func(p *Pricing) { p.AsOf = "" },
		"a retrieval date that is not one":  func(p *Pricing) { p.AsOf = "last July" },
		"a retrieval date missing its day":  func(p *Pricing) { p.AsOf = "2026-07" },
		"no plans at all":                   func(p *Pricing) { p.Plans = nil },
		"an unnamed plan":                   func(p *Pricing) { p.Plans[0].Name = " " },
		"the same plan name twice":          func(p *Pricing) { p.Plans[1].Name = p.Plans[0].Name },
		"a negative monthly price":          func(p *Pricing) { p.Plans[0].MonthlyUSD = -10 },
		"a NaN monthly price":               func(p *Pricing) { p.Plans[0].MonthlyUSD = math.NaN() },
		"a positive infinite monthly price": func(p *Pricing) { p.Plans[0].MonthlyUSD = math.Inf(1) },
		"a negative infinite monthly price": func(p *Pricing) { p.Plans[0].MonthlyUSD = math.Inf(-1) },
		"a negative credit allowance":       func(p *Pricing) { p.Plans[0].MonthlyCredits = -1 },
		"a promotion above the list price":  func(p *Pricing) { p.Plans[0].PromotionalMonthlyUSD = float64Ptr(40) },
		"a promotion equal to the list one": func(p *Pricing) { p.Plans[0].PromotionalMonthlyUSD = float64Ptr(30) },
		"a negative promotion":              func(p *Pricing) { p.Plans[0].PromotionalMonthlyUSD = float64Ptr(-5) },
		"a NaN promotional price":           func(p *Pricing) { p.Plans[0].PromotionalMonthlyUSD = float64Ptr(math.NaN()) },
		"a positive infinite promotion":     func(p *Pricing) { p.Plans[0].PromotionalMonthlyUSD = float64Ptr(math.Inf(1)) },
		"a negative infinite promotion":     func(p *Pricing) { p.Plans[0].PromotionalMonthlyUSD = float64Ptr(math.Inf(-1)) },
		"a plan that prices nothing":        func(p *Pricing) { p.Plans[0].ApplicableModelIDs = nil },
		"a plan pricing an unusable id":     func(p *Pricing) { p.Plans[0].ApplicableModelIDs = []ModelID{"narwhal deep"} },
		"a contract for a different model": func(p *Pricing) {
			for i := range p.Plans {
				p.Plans[i].ApplicableModelIDs = []ModelID{"narwhal-flat"}
			}
		},
	}
	// Each case names the phrase its OWN refusal has to carry. Checking only
	// the sentinel error is not enough: every refusal in this file wraps
	// ErrPricingInvalid, so a broken check is caught by whichever downstream
	// one happens to fire next and the mutant survives looking dead. The
	// no-plans case is the one that actually did that — with its check
	// narrowed, the empty plan list made the names-this-model check fail
	// instead, and the sentinel was identical.
	reasons := map[string]string{
		"no billing model":                  "names no billing model",
		"no edition":                        "names no edition",
		"no currency":                       "names no currency",
		"no quota period":                   "names no quota period",
		"no source URL":                     "names no source URL",
		"no retrieval date":                 "not a YYYY-MM-DD date",
		"a retrieval date that is not one":  "not a YYYY-MM-DD date",
		"a retrieval date missing its day":  "not a YYYY-MM-DD date",
		"no plans at all":                   "publishes no plan, so it prices nothing",
		"an unnamed plan":                   "unnamed plan",
		"the same plan name twice":          "twice",
		"a negative monthly price":          "monthly price of -10",
		"a NaN monthly price":               "non-finite monthly price",
		"a positive infinite monthly price": "non-finite monthly price",
		"a negative infinite monthly price": "non-finite monthly price",
		"a negative credit allowance":       "-1 monthly credits",
		"a promotion above the list price":  "not a promotion",
		"a promotion equal to the list one": "not a promotion",
		"a negative promotion":              "promotional price of -5",
		"a NaN promotional price":           "non-finite promotional price",
		"a positive infinite promotion":     "non-finite promotional price",
		"a negative infinite promotion":     "non-finite promotional price",
		"a plan that prices nothing":        "prices no model",
		"a plan pricing an unusable id":     "it carries whitespace",
		"a contract for a different model":  "a price for something else",
	}
	if len(reasons) != len(cases) {
		t.Fatalf("%d cases and %d stated reasons; a case with no reason is one that could be passing on somebody else's refusal", len(cases), len(reasons))
	}

	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			contract := narwhalPricing()
			apply(contract)
			vendor.models[0].Pricing = contract

			if err := vendor.models[0].Validate(); err == nil {
				t.Fatalf("Model.Validate admitted a contract with %s", name)
			}
			_, err := mustRegister(t, vendor, systemsWithPangolin(t))
			requireErrorIs(t, err, ErrPricingInvalid, "Register(model priced with "+name+")")
			requireMentions(t, err, reasons[name])
		})
	}
}

// TestRegisterAdmitsAWellFormedPricingContract is the positive half, and it
// also pins the nil case: no contract is not a refusal, it is "none registered".
func TestRegisterAdmitsAWellFormedPricingContract(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[0].Pricing = narwhalPricing()
	if _, err := mustRegister(t, vendor, systemsWithPangolin(t)); err != nil {
		t.Fatalf("Register(model with a well-formed billing contract): %v", err)
	}

	t.Run("no contract is not free use, and not a refusal either", func(t *testing.T) {
		clean := newNarwhal()
		clean.models[0].Pricing = nil
		if _, err := mustRegister(t, clean, systemsWithPangolin(t)); err != nil {
			t.Fatalf("Register(model with no billing contract): %v; most ported rows have none", err)
		}
	})
}

// TestPricingIsDeepCopiedOutOfAVendor is the aliasing half of the contract.
//
// Pricing is the only pointer on a model row, so it is the only field where a
// caller writing through Models() reaches the plugin's own declaration. A
// shallow copy here would let one caller change a published price for the whole
// process, which is the same class of bug CloneModels' slice copies close.
func TestPricingIsDeepCopiedOutOfAVendor(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[0].Pricing = narwhalPricing()

	first := CloneModels(vendor.Models())
	first[0].Pricing.Plans[0].MonthlyUSD = 9999
	first[0].Pricing.Plans[0].ApplicableModelIDs[0] = "scribbled"
	*first[0].Pricing.Plans[0].PromotionalMonthlyUSD = 1

	second := CloneModels(vendor.Models())
	if got := second[0].Pricing.Plans[0].MonthlyUSD; got != 30 {
		t.Errorf("writing through one answer changed the plugin's published price to %v", got)
	}
	if got := second[0].Pricing.Plans[0].ApplicableModelIDs[0]; got != "narwhal-deep" {
		t.Errorf("writing through one answer changed the plugin's plan allowlist to %q", got)
	}
	if got := *second[0].Pricing.Plans[0].PromotionalMonthlyUSD; got != 20 {
		t.Errorf("writing through one answer changed the plugin's promotional price to %v", got)
	}
}

// narwhalPricing is a well-formed billing contract for the test vendor's top
// model. Every refusal case above starts from this and breaks exactly one
// thing, so a case that fails is failing on the field it names.
func narwhalPricing() *Pricing {
	return &Pricing{
		BillingModel:       "narwhal-seat-credits",
		Edition:            "team",
		Currency:           "USD",
		QuotaPeriod:        "monthly",
		HasFrequencyLimits: false,
		Plans: []PricingPlan{
			{Name: "standard", MonthlyUSD: 30, PromotionalMonthlyUSD: float64Ptr(20), MonthlyCredits: 25_000, ApplicableModelIDs: []ModelID{"narwhal-deep"}},
			{Name: "pro", MonthlyUSD: 100, MonthlyCredits: 100_000, ApplicableModelIDs: []ModelID{"narwhal-deep", "narwhal-flat"}},
		},
		SourceURL: "https://narwhal.example/pricing",
		AsOf:      "2026-08-24",
	}
}

func float64Ptr(value float64) *float64 { return &value }

// TestLineupDerivesATotalOrderFromScoresWithTies is the derived-position
// contract, over a lineup built to have every shape at once: a clear top, a
// three-way tie, and a clear bottom.
func TestLineupDerivesATotalOrderFromScoresWithTies(t *testing.T) {
	models := []Model{
		{ID: "top", Rank: CapabilityRank{Score: 90}},
		{ID: "tied-a", Rank: CapabilityRank{Score: 50}},
		{ID: "tied-b", Rank: CapabilityRank{Score: 50}},
		{ID: "tied-c", Rank: CapabilityRank{Score: 50}},
		{ID: "bottom", Rank: CapabilityRank{Score: 10}},
	}
	// Declared out of score order, so the ordering cannot pass by accident of
	// the input already being sorted.
	shuffled := []Model{models[2], models[4], models[0], models[1], models[3]}

	ranked := Lineup(shuffled)
	if len(ranked) != len(shuffled) {
		t.Fatalf("Lineup returned %d rows for %d models", len(ranked), len(shuffled))
	}

	wantOrder := []ModelID{"top", "tied-b", "tied-a", "tied-c", "bottom"}
	wantTied := []bool{false, true, true, true, false}
	for i, row := range ranked {
		if row.Position != i+1 {
			t.Errorf("row %d is at position %d; a derived order must run 1..n with no gaps", i, row.Position)
		}
		if row.Model.ID != wantOrder[i] {
			t.Errorf("position %d holds %q; score descending with ties broken by declaration order puts %q there",
				i+1, row.Model.ID, wantOrder[i])
		}
		if row.Tied != wantTied[i] {
			t.Errorf("model %q reports Tied=%v and its score is shared by %v other rows",
				row.Model.ID, row.Tied, wantTied[i])
		}
	}
}

// TestLineupDoesNotReachTheDeclarationThroughItsAnswer holds the same
// no-aliasing rule Vendor.Models() answers under. A caller ranging over a
// lineup must not be able to rebind a model for the whole process.
func TestLineupDoesNotReachTheDeclarationThroughItsAnswer(t *testing.T) {
	models := []Model{
		{ID: "a", Rank: CapabilityRank{Score: 20, Basis: []RankEvidence{{Source: "s", Observation: "o"}}}, Systems: []agentic.SystemID{"pangolin"}},
		{ID: "b", Rank: CapabilityRank{Score: 10, Basis: []RankEvidence{{Source: "s", Observation: "o"}}}, Systems: []agentic.SystemID{"pangolin"}},
	}
	ranked := Lineup(models)
	ranked[0].Model.ID = "scribbled"
	ranked[0].Model.Systems[0] = "muse"
	ranked[0].Model.Rank.Basis[0].Observation = "scribbled"

	if models[0].ID != "a" {
		t.Errorf("writing through the lineup renamed the declaration to %q", models[0].ID)
	}
	if models[0].Systems[0] != "pangolin" {
		t.Errorf("writing through the lineup rebound the declaration to %q", models[0].Systems[0])
	}
	if models[0].Rank.Basis[0].Observation != "o" {
		t.Errorf("writing through the lineup rewrote the declaration's evidence to %q", models[0].Rank.Basis[0].Observation)
	}
}

// TestDeclaringModelsOnAResolvedRuntimeIsRefused is the second-table rule.
//
// A runtime whose vendor is established reads its models from that vendor's
// plugin. A list on the declaration as well would be two declarations of one
// fact, agreeing right up until one of them was edited — which is the disease
// this whole module's single-source guard exists to prevent, one layer up.
func TestDeclaringModelsOnAResolvedRuntimeIsRefused(t *testing.T) {
	declaration := RuntimeDeclaration{
		ID:     "narwhal-runtime",
		System: "pangolin",
		Vendor: "narwhal",
		Broker: BrokerProvenance{Checked: []string{"this test"}, Found: "this test declares it"},
		Models: []Model{{
			ID:          "narwhal-deep",
			Description: "a model this declaration has no business declaring",
			Rank:        CapabilityRank{Score: 10, Basis: []RankEvidence{{Source: "this test", Observation: "the only row"}}},
			Lifecycle:   LifecycleCurrent,
			Systems:     []agentic.SystemID{"pangolin"},
		}},
	}
	err := declaration.Validate()
	if err == nil {
		t.Fatal("a resolved runtime was allowed to declare its own model rows; the vendor plugin and this list are now two tables of one fact")
	}
	if !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("the refusal is not an invalid-declaration error: %v", err)
	}
	for _, want := range []string{"narwhal-runtime", "narwhal", "two declarations of one fact"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q, so a reader cannot see why the list is not allowed: %v", want, err)
		}
	}

	t.Run("and dropping the rows makes the same declaration legal", func(t *testing.T) {
		declaration.Models = nil
		if err := declaration.Validate(); err != nil {
			t.Fatalf("the declaration is refused for a reason other than its model rows: %v", err)
		}
	})
}

// TestAVendorUnresolvedRuntimesRowsAreHeldToTheSameStandard is the other half.
//
// These are the rows with no plugin behind them, so a second, weaker admission
// path here would be weakest exactly where nothing else is looking. Every case
// is one Model.Validate refuses for a registered vendor, plus the two rules that
// only a runtime declaration can state.
func TestAVendorUnresolvedRuntimesRowsAreHeldToTheSameStandard(t *testing.T) {
	base := func() RuntimeDeclaration {
		return RuntimeDeclaration{
			ID:     "orphan",
			System: "pangolin",
			Vendor: VendorUnresolved,
			Broker: BrokerProvenance{Checked: []string{"this test looked and found nothing"}},
			Models: []Model{
				{
					ID:          "orphan-one",
					Description: "the first row of a runtime nobody owns",
					Rank:        CapabilityRank{Score: 10, Basis: []RankEvidence{{Source: "this test", Observation: "declared first"}}},
					Lifecycle:   LifecycleCurrent,
					Systems:     []agentic.SystemID{"pangolin"},
				},
				{
					ID:          "orphan-two",
					Description: "the second row of a runtime nobody owns",
					Rank:        CapabilityRank{Score: 10, Basis: []RankEvidence{{Source: "this test", Observation: "declared second"}}},
					Lifecycle:   LifecycleCurrent,
					Systems:     []agentic.SystemID{"pangolin"},
				},
			},
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("the well-formed unresolved declaration is refused, so every case below would fail for the wrong reason: %v", err)
	}

	cases := map[string]func(*RuntimeDeclaration){
		"a row with no usage description": func(d *RuntimeDeclaration) { d.Models[0].Description = "" },
		"a row with no lineup state":      func(d *RuntimeDeclaration) { d.Models[0].Lifecycle = "" },
		"a row with no score":             func(d *RuntimeDeclaration) { d.Models[0].Rank.Score = 0 },
		"a row whose score has no evidence behind it": func(d *RuntimeDeclaration) {
			d.Models[0].Rank.Basis = nil
		},
		"a row with a contradictory effort axis": func(d *RuntimeDeclaration) {
			d.Models[0].Effort = EffortDeclaration{Support: agentic.EffortSupportRequired}
		},
		"a row with a negative context window": func(d *RuntimeDeclaration) {
			d.Models[0].ContextWindowTokens = -1
		},
		"a row with an unusable billing contract": func(d *RuntimeDeclaration) {
			d.Models[0].Pricing = &Pricing{BillingModel: "orphan-credits"}
		},
		"the same model id twice": func(d *RuntimeDeclaration) { d.Models[1].ID = d.Models[0].ID },
		"two rows recommended for one harness": func(d *RuntimeDeclaration) {
			d.Models[0].Recommended = true
			d.Models[1].Recommended = true
		},
		"a successor no row answers to": func(d *RuntimeDeclaration) {
			d.Models[0].Lifecycle = LifecycleLegacy
			d.Models[0].SupersededBy = "orphan-three"
		},
		// The two rules only a runtime declaration can state.
		"a row the runtime's own harness cannot drive": func(d *RuntimeDeclaration) {
			d.Models[0].Systems = []agentic.SystemID{"aardvark"}
		},
		"a row declaring no harness at all": func(d *RuntimeDeclaration) {
			d.Models[0].Systems = nil
		},
	}
	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			declaration := base()
			apply(&declaration)

			err := declaration.Validate()
			if err == nil {
				t.Fatalf("an unresolved runtime was allowed to declare %s; these are the rows with no vendor plugin checking them, so a weaker path here is weakest where nothing else looks", name)
			}
			if !errors.Is(err, ErrRuntimeInvalid) {
				t.Fatalf("the refusal is not an invalid-declaration error: %v", err)
			}
			if !strings.Contains(err.Error(), "orphan") {
				t.Errorf("the refusal names neither the runtime nor the row: %v", err)
			}
		})
	}
}

// TestSeedingCarriesTheUnresolvedRuntimesRowsThroughTheRegistry drives the
// PRODUCTION path: the frozen table goes through the same public DeclareRuntime
// an operator's config would call, and the rows have to survive it — validated,
// stored, and deeply copied on the way back out.
//
// A model list that validated in isolation and was dropped by the registry
// would leave every reader of the muse runtime with an empty lineup and nothing
// failing.
func TestSeedingCarriesTheUnresolvedRuntimesRowsThroughTheRegistry(t *testing.T) {
	// The muse harness is registered so the resolution below fails on the
	// VENDOR rather than on a missing system. Those are two different refusals
	// and only one of them is the fact under test.
	registry := NewRegistry(systemsNamed(t, "muse"))
	if err := SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding the frozen runtimes: %v", err)
	}
	declaration, declared := registry.RuntimeDeclarationOf("muse")
	if !declared {
		t.Fatal("the muse runtime is not declared after seeding")
	}
	if declaration.VendorResolved() {
		t.Fatalf("the muse runtime resolved to vendor %q; its broker was looked for and never established", declaration.Vendor)
	}
	if len(declaration.Models) != 2 {
		t.Fatalf("the muse runtime carries %d model rows through the registry; the board's table gives it two", len(declaration.Models))
	}

	// Deeply copied: a caller writing through one read must not reach the
	// frozen table, exactly as with a vendor's Models().
	declaration.Models[0].ID = "scribbled"
	declaration.Models[0].Systems[0] = "codex"
	again, _ := registry.RuntimeDeclarationOf("muse")
	if again.Models[0].ID == "scribbled" || again.Models[0].Systems[0] == "codex" {
		t.Error("writing through one read of the muse declaration changed the frozen table")
	}

	// And it is still UNLAUNCHABLE. Carrying rows must not have turned an
	// unresolved runtime into a resolvable one.
	if _, err := registry.ResolveRuntime("muse"); !errors.Is(err, ErrRuntimeVendorUnresolved) {
		t.Fatalf("resolving muse returned %v; a runtime that carries model rows and no vendor is still a runtime nothing can launch through", err)
	}
}
