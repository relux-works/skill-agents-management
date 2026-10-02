package benchdata

// Model is a data-only declaration consumed by the executable adapters.
// It contains no registration, validation or launch behavior.
type Model struct {
	ID, Description       string
	Rank                  CapabilityRank
	Lifecycle             string
	Effort                EffortDeclaration
	SupersededBy, AliasOf string
	Recommended           bool
	ContextWindowTokens   int
	Pricing               *Pricing
	Systems               []string
}

// CapabilityRank keeps the score independent of its benchmark observation.
type CapabilityRank struct {
	Score int
	Basis []RankEvidence
}

// RankEvidence includes the published prose and, for benchmark evidence, its
// typed claim. Both are constructed from the same declaration inputs.
type RankEvidence struct {
	Source, Observation string
	claim               *claim
}

type claim struct {
	kind, effort, benchID string
	fixed                 int
	cost                  *Cost
}

// EffortDeclaration is the model's published vocabulary and recommendation.
type EffortDeclaration struct {
	Support     string
	Vocabulary  []string
	Recommended string
}

const (
	EffortNone       = "none"
	EffortRequired   = "required"
	LifecycleCurrent = "current"
	LifecyclePreview = "preview"
	LifecycleLegacy  = "legacy"
)

// ModelID is the identifier spelling used in pricing allowlists.
type ModelID = string

// Pricing and PricingPlan retain the declaration-owned billing facts.
type Pricing struct {
	BillingModel, Edition, Currency, QuotaPeriod string
	HasFrequencyLimits                           bool
	Plans                                        []PricingPlan
	SourceURL, AsOf                              string
}
type PricingPlan struct {
	Name                  string
	MonthlyUSD            float64
	PromotionalMonthlyUSD *float64
	MonthlyCredits        int
	ApplicableModelIDs    []ModelID
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	value := *p
	return &value
}

func cloneCost(c *Cost) *Cost {
	if c == nil {
		return nil
	}
	return &Cost{TokensIn: clonePtr(c.TokensIn), TokensOut: clonePtr(c.TokensOut), USD: clonePtr(c.USD), WallS: clonePtr(c.WallS)}
}

func cloneModels(models []Model) []Model {
	out := append([]Model(nil), models...)
	for i := range out {
		m := &out[i]
		m.Systems = append([]string(nil), m.Systems...)
		m.Effort.Vocabulary = append([]string(nil), m.Effort.Vocabulary...)
		m.Rank.Basis = append([]RankEvidence(nil), m.Rank.Basis...)
		for j := range m.Rank.Basis {
			if c := m.Rank.Basis[j].claim; c != nil {
				copy := *c
				copy.cost = cloneCost(c.cost)
				m.Rank.Basis[j].claim = &copy
			}
		}
		if m.Pricing != nil {
			p := *m.Pricing
			p.Plans = append([]PricingPlan(nil), p.Plans...)
			for j := range p.Plans {
				p.Plans[j].PromotionalMonthlyUSD = clonePtr(p.Plans[j].PromotionalMonthlyUSD)
				p.Plans[j].ApplicableModelIDs = append([]ModelID(nil), p.Plans[j].ApplicableModelIDs...)
			}
			m.Pricing = &p
		}
	}
	return out
}
