package vendorplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Admitted-pair sets and their digests, carried from the extraction source's
// pkg/remoteconfig/spawn_admission.go.
//
// A digest is a FROZEN COMPATIBILITY SURFACE — invariant 3 of
// docs/architecture.md — so every byte that reaches the hash is defined here
// rather than left to a serializer's defaults: the provider label, the model
// order, the effort order, the separators and the line endings. Downstream
// snapshots pin these values, and a silent change is a compatibility break
// that looks like a refactor.
//
// The whole file is deliberately free of policy. It expands a ceiling that has
// already been decided into the exact set of (model, effort) pairs that
// ceiling admits, and it hashes that set. WHICH models a ceiling reaches is a
// separate question with a separate authority — v2snapshot.go — and the
// separation is the source's own hard-won one: capability rank is evidence,
// not an admission authority, so nothing here consults a rank.

// effortOrder is the ascending reasoning-effort ordering admission bounds are
// resolved against, carried verbatim from the source's spawnPolicyEffortOrder.
//
// It is ONE ordering across every vendor, and that is not the same claim as
// "every vendor accepts these words". A vendor's own vocabulary is its
// EffortDeclaration and is per model; this is the scale a ceiling's
// "less_or_equal medium" is measured on, and two vendors that both spell
// "high" have to place it at the same height or one config value would mean
// two different bounds. A word outside this list can therefore be a legal
// vendor vocabulary entry and still be unorderable as a BOUND, which is
// exactly what EffortRank's second return value says.
var effortOrder = []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

// EffortOrder returns the ascending ordering, freshly copied so a caller
// cannot reorder admission through the answer.
func EffortOrder() []string { return append([]string(nil), effortOrder...) }

// EffortRank returns a word's position on the admission scale. The second
// return is false for a word the scale does not place, which a caller must
// treat as "cannot be ordered" rather than as position zero: zero is
// "minimal", the most permissive bound there is.
func EffortRank(word string) (int, bool) {
	for rank, candidate := range effortOrder {
		if word == candidate {
			return rank, true
		}
	}
	return 0, false
}

// ModelCriterion is the direction a ceiling's model bound is read in.
type ModelCriterion string

const (
	// ModelCriterionEqual admits exactly the configured model and never
	// consults an ordering at all.
	ModelCriterionEqual ModelCriterion = "equal"
	// ModelCriterionLessOrEqual admits the bound model and everything at or
	// below it.
	ModelCriterionLessOrEqual ModelCriterion = "less_or_equal"
	// ModelCriterionGreaterOrEqual admits the bound model and everything at or
	// above it.
	ModelCriterionGreaterOrEqual ModelCriterion = "greater_or_equal"
)

// EffortCriterion is the direction a ceiling's effort bound is read in. There
// is no "equal" here for the same reason the source has none on this axis: a
// single-bound form states a ceiling or a floor, and an exact-effort admission
// is expressed per entry, not per provider.
type EffortCriterion string

const (
	// EffortCriterionLessOrEqual reads the bound as a ceiling.
	EffortCriterionLessOrEqual EffortCriterion = "less_or_equal"
	// EffortCriterionGreaterOrEqual reads the bound as a floor.
	EffortCriterionGreaterOrEqual EffortCriterion = "greater_or_equal"
)

// AdmittedPair is one exact (model, effort) admission. Effort is the empty
// string for a model with no reasoning-effort axis — NOT a missing value: such
// a model admits exactly one pair, and that pair's effort component is empty.
type AdmittedPair struct {
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

// String renders one pair in the canonical "model:effort" pin form. A model
// with no effort axis renders as the bare model id.
func (p AdmittedPair) String() string {
	if p.Effort == "" {
		return p.Model
	}
	return p.Model + ":" + p.Effort
}

// ParseAdmittedPair reads the canonical pin form back.
func ParseAdmittedPair(raw string) (AdmittedPair, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return AdmittedPair{}, fmt.Errorf("admitted pair must be a non-empty %q or %q string", "model", "model:effort")
	}
	model, effort, found := strings.Cut(trimmed, ":")
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	if model == "" || (found && effort == "") {
		return AdmittedPair{}, fmt.Errorf("admitted pair %q must be %q or %q", raw, "model", "model:effort")
	}
	return AdmittedPair{Model: model, Effort: effort}, nil
}

// AdmittedModel is one admitted model with every effort admitted for it.
// Efforts is exactly [""] for a model with no reasoning-effort axis.
type AdmittedModel struct {
	ID      string   `json:"id"`
	Efforts []string `json:"efforts"`
}

// AdmittedPairSet is the canonical admission representation.
//
// Provider is the RUNTIME id, not the vendor id, and the distinction is
// load-bearing rather than cosmetic: the frozen digests downstream were hashed
// over "codex" and "claude", and the same models reached through a different
// runtime — alibaba's rows under the codex harness — are a different account,
// a different quota and a different admitted set. Hashing a vendor id here
// would silently merge two of those.
type AdmittedPairSet struct {
	Provider string          `json:"provider"`
	Models   []AdmittedModel `json:"models"`
}

// Contains reports whether the exact (model, effort) pair is admitted.
func (s AdmittedPairSet) Contains(model, effort string) bool {
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	for _, admitted := range s.Models {
		if admitted.ID != model {
			continue
		}
		for _, candidate := range admitted.Efforts {
			if candidate == effort {
				return true
			}
		}
		return false
	}
	return false
}

// ContainsModel reports whether any pair admits the model.
func (s AdmittedPairSet) ContainsModel(model string) bool {
	model = strings.TrimSpace(model)
	for _, admitted := range s.Models {
		if admitted.ID == model {
			return true
		}
	}
	return false
}

// ModelIDs returns the admitted model identifiers in set order.
func (s AdmittedPairSet) ModelIDs() []string {
	ids := make([]string, 0, len(s.Models))
	for _, admitted := range s.Models {
		ids = append(ids, admitted.ID)
	}
	return ids
}

// Pairs flattens the set in its own model order, efforts as stored.
func (s AdmittedPairSet) Pairs() []AdmittedPair {
	pairs := make([]AdmittedPair, 0, len(s.Models))
	for _, admitted := range s.Models {
		for _, effort := range admitted.Efforts {
			pairs = append(pairs, AdmittedPair{Model: admitted.ID, Effort: effort})
		}
	}
	return pairs
}

// IsEmpty reports whether the set admits no pair at all.
func (s AdmittedPairSet) IsEmpty() bool {
	for _, admitted := range s.Models {
		if len(admitted.Efforts) > 0 {
			return false
		}
	}
	return true
}

// CanonicalSerialization renders the set with models sorted by id and efforts
// in ascending admission order.
//
// This exact string is what Digest hashes, and it is normalized rather than
// written in the order the expansion happened to produce so that a pin is
// reproducible from the config regardless of declaration order. The shape —
// provider line, then one tab-separated line per model with its efforts joined
// by commas — is the extraction source's, byte for byte, because the digests
// downstream were taken over it.
func (s AdmittedPairSet) CanonicalSerialization() string {
	models := append([]AdmittedModel(nil), s.Models...)
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	var builder strings.Builder
	builder.WriteString(s.Provider)
	builder.WriteString("\n")
	for _, admitted := range models {
		builder.WriteString(admitted.ID)
		builder.WriteString("\t")
		builder.WriteString(strings.Join(SortEfforts(admitted.Efforts), ","))
		builder.WriteString("\n")
	}
	return builder.String()
}

// Digest returns the sha256 pin over the canonical serialization.
func (s AdmittedPairSet) Digest() string {
	sum := sha256.Sum256([]byte(s.CanonicalSerialization()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PinnedPairs renders the set as the canonical, sorted pin pair list persisted
// beside the digest.
func (s AdmittedPairSet) PinnedPairs() []string {
	pairs := s.Pairs()
	rendered := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		rendered = append(rendered, pair.String())
	}
	sort.Strings(rendered)
	return rendered
}

// SortEfforts orders effort words by the admission scale, stably, with words
// the scale does not place sorted after the ones it does and lexicographically
// among themselves.
//
// The unplaced-word tail is not decoration. A vendor may publish a vocabulary
// word this scale has never heard of, and dropping it here would silently
// shrink an admitted set; sorting it to the end keeps it in the digest and
// makes the ordering total either way.
func SortEfforts(efforts []string) []string {
	sorted := append([]string(nil), efforts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, leftKnown := EffortRank(sorted[i])
		right, rightKnown := EffortRank(sorted[j])
		if leftKnown && rightKnown {
			return left < right
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return sorted[i] < sorted[j]
	})
	return sorted
}

// EffortsUnderBound returns the efforts one model admits under a single shared
// bound, in ascending order.
//
//   - A model with NO effort axis admits exactly [""], whatever the bound says.
//     That is not the bound being ignored: such a model has one pair, and its
//     effort component is empty. Returning nil instead would drop the model out
//     of the admitted set entirely, which is how a haiku row would vanish from
//     a claude ceiling.
//   - An empty bound admits the model's whole vocabulary.
//   - A bound word the admission scale cannot place admits NOTHING, rather than
//     falling back to the whole vocabulary. A bound nobody can order is not a
//     permissive bound; it is an unusable one, and widening on it would turn a
//     typo in a config into an unrestricted ceiling.
func EffortsUnderBound(model Model, bound string, criterion EffortCriterion) []string {
	if model.Effort.Support != agentic.EffortSupportRequired {
		return []string{""}
	}
	supported := SortEfforts(model.Effort.Vocabulary)
	bound = strings.TrimSpace(bound)
	if bound == "" {
		return supported
	}
	boundRank, known := EffortRank(bound)
	if !known {
		return nil
	}
	admitted := make([]string, 0, len(supported))
	for _, effort := range supported {
		rank, ok := EffortRank(effort)
		if !ok {
			continue
		}
		if criterion == EffortCriterionGreaterOrEqual {
			if rank < boundRank {
				continue
			}
		} else if rank > boundRank {
			continue
		}
		admitted = append(admitted, effort)
	}
	return admitted
}
