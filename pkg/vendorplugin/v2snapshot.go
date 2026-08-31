package vendorplugin

import (
	"fmt"
	"sort"
	"strings"
)

// The frozen spawn-policy-v2 admission snapshot, carried verbatim from the
// extraction source's tools/board-cli/internal/spawn/v2_admission_snapshot.go.
//
// # Why a frozen table and not the capability ranks in this package
//
// Until the source's v3 rollout, an ordered ceiling ("admit everything at or
// below gpt-5.6-sol") computed its member set from the mutable per-row
// capability rank. That made every registry edit an implicit POLICY change in
// repositories whose config never moved: registering a new model below an
// existing ceiling silently widened it, and a truthful re-rank silently moved
// the bound. The source froze the sets as they stood at rollout and made the
// frozen table the only membership authority for a v2 ordered or effort-only
// ceiling. This port keeps that split exactly, and it is why nothing in
// admission.go or in this file reads CapabilityRank: a rank here is capability
// EVIDENCE, and evidence that silently decided who may spawn would be the
// defect the source spent a task removing.
//
// The consequences the source states, unchanged here:
//
//   - A capability rank may be corrected at any time and no admitted set moves.
//   - A newly registered model id is ABSENT from this table and is therefore
//     refused by a v2 ordered ceiling until the operator migrates that ceiling
//     to an explicit allow set. Absence is the answer, not a gap to paper over.
//   - Lifecycle, supersession, recommendation and display order carry no
//     admission meaning whatsoever.
//
// # One fact, one file
//
// "Which models a frozen v2 bound admitted at rollout" is one fact and this
// file is the only place it is written down. It is held there structurally
// rather than by permission: the table below is a SLICE, and rewriting it as
// the map[RuntimeID][][]string it obviously wants to be fails
// pkg/agentic/singlesource_guard_test.go, because a second RuntimeID-keyed
// table is the shadow binding that guard exists to catch. Six rows scan for
// free, so the slice costs nothing to keep.

// V2SnapshotVersion identifies the frozen admission data. It changes only when
// a new compatibility snapshot is deliberately captured, and downstream
// migration evidence quotes it so a snapshot exclusion can be told apart from
// a registry problem.
const V2SnapshotVersion = "spawn-policy-v2-rollout-2026-07-28"

// v2SnapshotSource names where the tiers below were read from, so a reader
// chasing "why is gpt-5.2 below gpt-5.2-codex" gets a file to open.
const v2SnapshotSource = "skill-project-management tools/board-cli/internal/spawn/v2_admission_snapshot.go (v2AdmissionSnapshotRolloutTiers, captured from that repository's registry at commit 8ba6836)"

// v2Tiers is one runtime's frozen capability ordering, ASCENDING: Tiers[0] is
// the least capable. Ids inside one tier were mutually equal under v2
// comparison — an alias and its dated snapshot — so each admits the other
// under both ordered directions.
//
// This is a SLICE of rows rather than a map keyed by RuntimeID for the reason
// registry.go's frozenRuntimes is: a second RuntimeID-keyed table anywhere
// outside that file is the shadow binding the single-source guard fails the
// build over, and a table of six rows costs nothing to scan.
type v2Tiers struct {
	Runtime RuntimeID
	Tiers   [][]string
}

// v2RolloutTiers is the frozen data itself.
//
// Only three runtimes have rows, and the absence of the other three is a
// FINDING rather than an omission: the source's snapshot was captured when
// gemini, agy and muse had no v2 ceiling to grandfather, so a v2 ordered
// ceiling naming one of them has nothing to expand through. V2AdmittedModels
// says so explicitly instead of inventing a set from the live lineup.
var v2RolloutTiers = []v2Tiers{
	{
		Runtime: "codex",
		Tiers: [][]string{
			{"gpt-5.1-codex-mini"},
			{"gpt-5.2"},
			{"gpt-5.1-codex-max"},
			{"gpt-5.2-codex"},
			{"gpt-5.3-codex"},
			{"gpt-5.3-codex-spark"},
			{"gpt-5.4-mini"},
			{"gpt-5.4"},
			{"gpt-5.5"},
			{"gpt-5.6-luna"},
			{"gpt-5.6-terra"},
			{"gpt-5.6-sol"},
		},
	},
	{
		Runtime: "claude",
		Tiers: [][]string{
			{"claude-haiku-4-5", "claude-haiku-4-5-20251001"},
			{"claude-sonnet-4-6"},
			{"claude-opus-4-6"},
			{"claude-opus-4-8"},
			{"claude-sonnet-5"},
			{"claude-opus-5"},
			{"claude-fable-5"},
		},
	},
	{
		Runtime: "qwen",
		Tiers: [][]string{
			{"qwen3.6-flash"},
			{"qwen3.6-plus"},
			{"qwen3.7-plus"},
			{"qwen3.7-max"},
			{"qwen3.8-max-preview"},
		},
	},
}

// V2SnapshotRuntimes returns the runtimes the frozen snapshot can answer for,
// in declared order.
func V2SnapshotRuntimes() []RuntimeID {
	out := make([]RuntimeID, 0, len(v2RolloutTiers))
	for _, row := range v2RolloutTiers {
		out = append(out, row.Runtime)
	}
	return out
}

// V2SnapshotTiers returns one runtime's frozen tiers, ascending, deeply
// copied. The second return is false for a runtime the snapshot never
// captured.
func V2SnapshotTiers(runtime RuntimeID) ([][]string, bool) {
	for _, row := range v2RolloutTiers {
		if row.Runtime != runtime {
			continue
		}
		out := make([][]string, 0, len(row.Tiers))
		for _, tier := range row.Tiers {
			out = append(out, append([]string(nil), tier...))
		}
		return out, true
	}
	return nil, false
}

// V2SnapshotSource names the file the tiers were carried from.
func V2SnapshotSource() string { return v2SnapshotSource }

// V2AdmittedModels answers which model ids a spawn-policy-v2 bound admitted at
// rollout, lexicographically sorted.
//
// The second return is false when the snapshot cannot answer at all — an
// uncaptured runtime, or an equal-criterion bound with no model named. A false
// here is a refusal to guess, and a caller must surface it rather than falling
// back to the live lineup: falling back is precisely how a registry edit
// becomes a policy change.
//
//   - equal never consulted an ordering and still does not: it admits exactly
//     its configured model.
//   - An EMPTY bound is an effort-only ceiling and admits the runtime's whole
//     rollout universe.
//   - An ordered bound naming a model the snapshot does not hold is
//     unanswerable, which is how a model registered after the freeze is
//     refused rather than silently admitted.
func V2AdmittedModels(runtime RuntimeID, boundModel ModelID, criterion ModelCriterion) ([]string, bool) {
	tiers, captured := V2SnapshotTiers(runtime)
	if !captured {
		return nil, false
	}
	bound := strings.TrimSpace(string(boundModel))
	if criterion == ModelCriterionEqual {
		if bound == "" {
			return nil, false
		}
		return []string{bound}, true
	}

	universe := make([]string, 0)
	for _, tier := range tiers {
		universe = append(universe, tier...)
	}
	if bound == "" {
		sort.Strings(universe)
		return universe, true
	}

	boundTier := -1
	for index, tier := range tiers {
		for _, id := range tier {
			if id == bound {
				boundTier = index
			}
		}
	}
	if boundTier < 0 {
		return nil, false
	}
	admitted := make([]string, 0, len(universe))
	for index, tier := range tiers {
		if criterion == ModelCriterionGreaterOrEqual {
			if index >= boundTier {
				admitted = append(admitted, tier...)
			}
			continue
		}
		if index <= boundTier {
			admitted = append(admitted, tier...)
		}
	}
	sort.Strings(admitted)
	return admitted, true
}

// V2Ceiling is one already-validated spawn-policy-v2 ceiling as this package
// expands it: the runtime it applies to, the model bound and its direction,
// and the single shared reasoning-effort bound.
//
// It carries no roles, no provenance and no config keys. Reading and
// validating a repository's configuration is the config layer's job and stays
// in task-board for now; what this package owns is turning a decided ceiling
// into the exact pairs it admits.
type V2Ceiling struct {
	Runtime        RuntimeID
	Model          ModelID
	ModelCriterion ModelCriterion
	Effort         string
}

// ErrV2Unexpandable is returned when a ceiling cannot be expanded through the
// frozen snapshot, or when the expansion would admit nothing at all.
//
// An admission that admits nothing is refused rather than returned empty: an
// empty admitted set and a ceiling nobody could expand look identical to a
// caller downstream, and both of them would then be one `len(...) == 0` away
// from reading as "no restriction".
var ErrV2Unexpandable = fmt.Errorf("vendorplugin: spawn-policy-v2 ceiling cannot be expanded through the frozen admission snapshot")

// ExpandV2Ceiling turns a v2 ceiling into its admitted pair set, reading model
// MEMBERSHIP from the frozen snapshot and per-model effort VOCABULARIES from
// the vendor plugin that owns the runtime.
//
// The two halves come from two different places on purpose, and that is the
// whole shape of the port: the frozen snapshot decides who is in, and the
// vendor's own model row decides which effort words that member accepts. A
// ported vocabulary that drifted by one word changes the digest, which is what
// makes the pinned digests a test of the model port rather than of this
// function.
//
// The effort bound direction follows the model criterion exactly as the source
// derives it: an ordered-downwards ceiling reads its effort as a ceiling, an
// ordered-upwards one reads it as a floor. A pair set may not claim a
// narrower admission than the ceiling it was expanded from.
func ExpandV2Ceiling(r *Registry, ceiling V2Ceiling) (AdmittedPairSet, error) {
	if r == nil {
		return AdmittedPairSet{}, fmt.Errorf("vendorplugin: cannot expand a ceiling without a registry")
	}
	runtime, err := r.ResolveRuntime(ceiling.Runtime)
	if err != nil {
		return AdmittedPairSet{}, err
	}
	admitted, answerable := V2AdmittedModels(runtime.ID, ceiling.Model, ceiling.ModelCriterion)
	if !answerable {
		return AdmittedPairSet{}, fmt.Errorf("%w: runtime %s bound %q with criterion %q is not one the %s snapshot holds",
			ErrV2Unexpandable, runtime.ID, ceiling.Model, ceiling.ModelCriterion, V2SnapshotVersion)
	}

	effortCriterion := EffortCriterionLessOrEqual
	if ceiling.ModelCriterion == ModelCriterionGreaterOrEqual {
		effortCriterion = EffortCriterionGreaterOrEqual
	}

	byID := RuntimeModels(runtime)
	set := AdmittedPairSet{Provider: runtime.ID.String()}
	for _, id := range admitted {
		model, registered := byID[ModelID(id)]
		if !registered {
			return AdmittedPairSet{}, fmt.Errorf("%w: the snapshot admits %q for runtime %s and vendor %s does not register it under that runtime's agentic system",
				ErrV2Unexpandable, id, runtime.ID, runtime.VendorID)
		}
		efforts := EffortsUnderBound(model, ceiling.Effort, effortCriterion)
		if len(efforts) == 0 {
			continue
		}
		set.Models = append(set.Models, AdmittedModel{ID: id, Efforts: efforts})
	}
	if set.IsEmpty() {
		return AdmittedPairSet{}, fmt.Errorf("%w: runtime %s bound %q/%q with effort %q admits no (model, effort) pair",
			ErrV2Unexpandable, runtime.ID, ceiling.Model, ceiling.ModelCriterion, ceiling.Effort)
	}
	return set, nil
}

// RuntimeModels indexes the models the runtime's vendor declares that ALSO
// declare the runtime's agentic system.
//
// The second half of that sentence is what keeps two runtimes over one vendor
// apart. alibaba owns both the qwen-code rows and the single codex row, and
// google owns both the gemini-cli rows and the antigravity ones; indexing a
// vendor's whole list would give the qwen runtime a model only reachable
// through the codex harness, and its admitted set would then contain a pair no
// launch could ever make.
func RuntimeModels(runtime Runtime) map[ModelID]Model {
	models := map[ModelID]Model{}
	if runtime.Vendor == nil {
		return models
	}
	for _, model := range runtime.Vendor.Models() {
		if model.DrivenBy(runtime.SystemID) {
			models[model.ID] = model
		}
	}
	return models
}
