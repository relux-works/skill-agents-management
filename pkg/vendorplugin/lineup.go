package vendorplugin

import "sort"

// The DERIVED total order over a vendor's models.
//
// A CapabilityRank declares a SCORE, and scores tie. Some callers need a total
// order anyway — printing a picker, numbering a listing, choosing which of two
// equal rows to show first — and this file is the one place that order is
// produced, so a caller cannot invent a second one and no declaration has to
// carry an invented number to satisfy it.
//
// # What the position is, and what it is not
//
// A position is PRESENTATION. It is a fact about this list, not about the
// models: where two rows share a score, the one printed first is simply the one
// the vendor declared first, and swapping the two declarations swaps the
// positions without a single observation having changed. That is exactly why
// Position is not a field. A number sitting in a declaration next to an
// evidence-bearing score reads as a second evidence-bearing fact, and this
// module's history is one long argument that a number nobody observed must not
// be able to pass for one that was.
//
// Tied says the distinction out loud, so a caller that wants to render "7=" or
// refuse to break a tie at all has the fact rather than having to recompute it.

// RankedModel is one model with its derived presentation position.
type RankedModel struct {
	// Model is the declaration, unchanged.
	Model Model
	// Position is 1 for the most capable model in the list, counting up with
	// no gaps. It is presentation and not evidence; see this file's comment.
	Position int
	// Tied reports whether another model in the same list carries this model's
	// score. When it is true, the position separating the two came from
	// declaration order and nothing observed it.
	Tied bool
}

// Lineup orders models by capability score descending, breaking ties by
// DECLARATION ORDER, and numbers them 1..n.
//
// The tie break is declaration order rather than model id because id order is
// alphabetical noise that LOOKS principled: "claude-haiku-4-5" before
// "claude-haiku-4-5-20251001" is a fact about strings. Declaration order is at
// least the order a human wrote the lineup in, and it is stable across a
// rename.
//
// The input is not modified and the models are deep-copied on the way out, so a
// caller ranging over the answer cannot reach a plugin's own table through it —
// the same rule Vendor.Models() answers under.
func Lineup(models []Model) []RankedModel {
	scores := make([]int, 0, len(models))
	for _, model := range models {
		scores = append(scores, model.Rank.Score)
	}

	// The declaration index travels with each row so the sort's tie break is
	// the declaration order rather than sort.SliceStable's promise about it.
	// The two agree today; writing the index down means they cannot stop
	// agreeing because somebody swapped in a faster sort.
	type entry struct {
		model    Model
		declared int
	}
	entries := make([]entry, 0, len(models))
	for i, model := range CloneModels(models) {
		entries = append(entries, entry{model: model, declared: i})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].model.Rank.Score != entries[j].model.Rank.Score {
			return entries[i].model.Rank.Score > entries[j].model.Rank.Score
		}
		return entries[i].declared < entries[j].declared
	})

	ranked := make([]RankedModel, 0, len(entries))
	for i, e := range entries {
		tied := false
		for j, score := range scores {
			if j != e.declared && score == e.model.Rank.Score {
				tied = true
			}
		}
		ranked = append(ranked, RankedModel{Model: e.model, Position: i + 1, Tied: tied})
	}
	return ranked
}

// LineupOf is Lineup over a registered vendor's own list.
//
// It exists so a caller asking "where does this vendor's lineup put its
// models" does not have to remember to call Models() first — and so that
// question has ONE answer, rather than one per caller that reached for
// sort.Slice.
func LineupOf(vendor Vendor) []RankedModel {
	if vendor == nil {
		return []RankedModel{}
	}
	return Lineup(vendor.Models())
}
