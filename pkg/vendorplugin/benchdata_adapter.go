package vendorplugin

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/benchdata"
)

// ModelsFromBenchdata projects data-only declarations onto the existing vendor
// API without registration. Every mutable field is copied into adapter ownership.
func ModelsFromBenchdata(facts []benchdata.Model) []Model {
	out := make([]Model, len(facts))
	for i, fact := range facts {
		support := agentic.EffortSupportNone
		if fact.Effort.Support == benchdata.EffortRequired {
			support = agentic.EffortSupportRequired
		}
		model := Model{
			ID: ModelID(fact.ID), Description: UsageDescription(fact.Description),
			Rank: CapabilityRank{Score: fact.Rank.Score}, Lifecycle: Lifecycle(fact.Lifecycle),
			Effort:       EffortDeclaration{Support: support, Vocabulary: append([]string(nil), fact.Effort.Vocabulary...), Recommended: fact.Effort.Recommended},
			SupersededBy: ModelID(fact.SupersededBy), AliasOf: ModelID(fact.AliasOf), Recommended: fact.Recommended,
			ContextWindowTokens: fact.ContextWindowTokens,
		}
		for _, evidence := range fact.Rank.Basis {
			model.Rank.Basis = append(model.Rank.Basis, RankEvidence{Source: evidence.Source, Observation: evidence.Observation})
		}
		for _, system := range fact.Systems {
			model.Systems = append(model.Systems, agentic.SystemID(system))
		}
		if fact.Pricing != nil {
			p := fact.Pricing
			model.Pricing = &Pricing{BillingModel: p.BillingModel, Edition: p.Edition, Currency: p.Currency, QuotaPeriod: p.QuotaPeriod, HasFrequencyLimits: p.HasFrequencyLimits, SourceURL: p.SourceURL, AsOf: p.AsOf}
			for _, plan := range p.Plans {
				converted := PricingPlan{Name: plan.Name, MonthlyUSD: plan.MonthlyUSD, MonthlyCredits: plan.MonthlyCredits}
				if plan.PromotionalMonthlyUSD != nil {
					value := *plan.PromotionalMonthlyUSD
					converted.PromotionalMonthlyUSD = &value
				}
				for _, id := range plan.ApplicableModelIDs {
					converted.ApplicableModelIDs = append(converted.ApplicableModelIDs, ModelID(id))
				}
				model.Pricing.Plans = append(model.Pricing.Plans, converted)
			}
		}
		out[i] = model
	}
	return out
}
