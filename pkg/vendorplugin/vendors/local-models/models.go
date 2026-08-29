package localmodels

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// buildModels projects a Config's rows onto vendorplugin.Model, one row per
// (runtime, model) pair, each declaring the SINGLE agentic system its
// runtime entry names.
//
// Publisher and Family — alibaba/qwen for local-qwen's row — are provenance
// only and are never read by any admission or dispatch decision anywhere in
// this module; naming stays orthogonal by construction.
func buildModels(cfg Config) []vendorplugin.Model {
	var out []vendorplugin.Model
	for _, runtime := range cfg.Runtimes {
		for modelID, entry := range runtime.Models {
			out = append(out, vendorplugin.Model{
				ID:                  modelID,
				Description:         vendorplugin.UsageDescription(entry.Description),
				Rank:                localCapabilityRank(),
				Lifecycle:           entry.Lifecycle,
				Effort:              vendorplugin.EffortDeclaration{Support: entry.EffortSupport},
				ContextWindowTokens: entry.ContextWindowTokens,
				Publisher:           entry.Publisher,
				Family:              entry.Family,
				Systems:             []agentic.SystemID{runtime.System},
			})
		}
	}
	return out
}

// localCapabilityRank is the one rank every local-models row carries.
//
// A capability score is comparable only within one vendor's own lineup
// (CapabilityRank's own doc), and this vendor's whole catalog is
// operator-populated from a single machine-local file with no published
// benchmark to score rows against each other — there is exactly one thing to
// rank most local catalogs against: whether the operator bothered to point a
// runtime at it at all. Every row therefore carries the SAME score, which
// states a tie rather than inventing an ordering nobody observed.
func localCapabilityRank() vendorplugin.CapabilityRank {
	return vendorplugin.CapabilityRank{
		Score: 1,
		Basis: []vendorplugin.RankEvidence{{
			Source:      "local-models.toml",
			Observation: "the row exists in the operator's own local-models.toml; no comparative benchmark is available for locally-configured models",
		}},
	}
}

// runtimeEntry finds the declared runtime by id, scanning the slice
// linearly — Config.Runtimes is a slice rather than a map keyed by
// RuntimeID by construction (see RuntimeEntry's own doc), and a handful of
// declared runtimes costs nothing to scan.
func runtimeEntry(cfg Config, id vendorplugin.RuntimeID) (RuntimeEntry, bool) {
	for _, entry := range cfg.Runtimes {
		if entry.ID == id {
			return entry, true
		}
	}
	return RuntimeEntry{}, false
}

// pointerFor looks up the operator-populated pointer for one (runtime,
// model) pair, keyed by the SAME identifiers the vendor's own config scope
// carries — never falling back to any other runtime's pointer.
func pointerFor(cfg Config, runtime vendorplugin.RuntimeID, model vendorplugin.ModelID) (Pointer, bool) {
	entry, ok := runtimeEntry(cfg, runtime)
	if !ok {
		return Pointer{}, false
	}
	modelEntry, ok := entry.Models[model]
	if !ok {
		return Pointer{}, false
	}
	return modelEntry.Pointer, true
}
