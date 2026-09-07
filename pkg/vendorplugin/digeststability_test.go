package vendorplugin_test

import (
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// The admitted-pair digest is a FROZEN COMPATIBILITY SURFACE — invariant 3 of
// docs/architecture.md — and v0.2.0 put six new fields on the row it is
// computed from. This file is the pin that they did not reach it.
//
// # Why the existing pins are not enough
//
// admissionpin_test.go already holds the real rows' digests against values the
// BOARD's own binary printed, so a field that leaked into the hash would fail
// there too. That is a fine backstop and a poor statement: it fails once, on
// one commit, with a message about a digest rather than about a field, and it
// would go on being the only guard the day somebody added a seventh field.
//
// What is pinned here is the PROPERTY: the digest covers what a launch can
// actually do — which model, which effort word — and nothing about how a model
// is presented. A lifecycle, a display recommendation, a context window, a
// price and a capability score can all be corrected at any time, and correcting
// one must not move who may spawn. That is the same rule the frozen v2 snapshot
// exists to enforce one level up, stated where the bytes are produced.
//
// The mutants are DELIBERATELY EXTREME — a legacy lifecycle on every row, every
// price doubled, the whole lineup re-scored — because a subtle mutant that
// happened not to change the digest would prove nothing about the class.

// digestMutants are the presentation facts that must not reach the hash, each
// with a transform that would be impossible to miss if it did.
var digestMutants = map[string]func(vendorplugin.Model) vendorplugin.Model{
	"every row marked legacy": func(m vendorplugin.Model) vendorplugin.Model {
		m.Lifecycle = vendorplugin.LifecycleLegacy
		// SupersededBy has to travel with it or Validate refuses the row for
		// contradicting itself, which would fail the mutant harness rather
		// than the digest. Clearing it keeps the mutant legal and still
		// changes the field under test on every row.
		m.SupersededBy = ""
		return m
	},
	"every row's supersession cleared": func(m vendorplugin.Model) vendorplugin.Model {
		m.SupersededBy = ""
		return m
	},
	"every row marked not recommended": func(m vendorplugin.Model) vendorplugin.Model {
		m.Recommended = false
		return m
	},
	"every row given a context window": func(m vendorplugin.Model) vendorplugin.Model {
		m.ContextWindowTokens = 12_345
		return m
	},
	"every row given a cache budget": func(m vendorplugin.Model) vendorplugin.Model {
		value := int64(6_442_450_944)
		m.CacheBudgetBytes = &value
		return m
	},
	"every price doubled": func(m vendorplugin.Model) vendorplugin.Model {
		if m.Pricing == nil {
			return m
		}
		contract := m.Pricing.Clone()
		for i := range contract.Plans {
			contract.Plans[i].MonthlyUSD *= 2
			contract.Plans[i].MonthlyCredits *= 2
		}
		m.Pricing = contract
		return m
	},
	"every billing contract dropped": func(m vendorplugin.Model) vendorplugin.Model {
		m.Pricing = nil
		return m
	},
	"every capability score flattened to one": func(m vendorplugin.Model) vendorplugin.Model {
		m.Rank.Score = 1
		return m
	},
	"every usage description replaced": func(m vendorplugin.Model) vendorplugin.Model {
		m.Description = "a model"
		return m
	},
}

// TestThePresentationFieldsDoNotReachTheDigest drives the REAL expansion —
// ExpandV2Ceiling over the real frozen ceilings — with each presentation fact
// mutated across every row of every vendor, and requires the canonical
// serialization to come back byte-identical.
//
// The serialization is compared before the digest on purpose. A digest
// comparison alone says "something moved"; comparing the bytes first means the
// failure output is the two serializations, which is where a reader can see
// WHICH field leaked.
func TestThePresentationFieldsDoNotReachTheDigest(t *testing.T) {
	fixture := loadSourceAdmission(t)
	baseline := isolatedRegistry(t, nil)

	for name, mutate := range digestMutants {
		t.Run(name, func(t *testing.T) {
			mutations := map[vendorplugin.VendorID]func(vendorplugin.Model) vendorplugin.Model{}
			for vendor := range portedVendors {
				mutations[vendor] = mutate
			}
			mutated := isolatedRegistry(t, mutations)

			for runtime, pinned := range fixture.Ceilings {
				ceiling := vendorplugin.V2Ceiling{
					Runtime:        vendorplugin.RuntimeID(runtime),
					Model:          vendorplugin.ModelID(pinned.Model),
					ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
					Effort:         pinned.Effort,
				}
				before, err := vendorplugin.ExpandV2Ceiling(baseline, ceiling)
				if err != nil {
					t.Fatalf("expanding %s against the unmutated registry: %v", runtime, err)
				}
				after, err := vendorplugin.ExpandV2Ceiling(mutated, ceiling)
				if err != nil {
					t.Fatalf("expanding %s with %s: %v", runtime, name, err)
				}
				if before.CanonicalSerialization() != after.CanonicalSerialization() {
					t.Errorf("%s changed runtime %s's canonical serialization; the digest is a frozen compatibility surface and a presentation fact must not be able to move it\nbefore:\n%q\nafter:\n%q",
						name, runtime, before.CanonicalSerialization(), after.CanonicalSerialization())
				}
				if after.Digest() != pinned.AdmittedPairs.Digest {
					t.Errorf("%s moved runtime %s's digest to %s; the board's own binary printed %s",
						name, runtime, after.Digest(), pinned.AdmittedPairs.Digest)
				}
			}
		})
	}
}

// TestTheDigestStillMovesOnTheFactsItCovers narrows the gate.
//
// The test above proves eight facts do not reach the digest. On its own that is
// equally consistent with a digest that covers NOTHING — a serializer that
// emitted a constant would pass every one of those subtests. So the two facts
// the digest is FOR are mutated here and must move it: the model id and the
// effort vocabulary.
//
// admissionpin_test.go carries the same idea over the real ceilings; this one
// sits beside the stability claim so the pair cannot be read apart.
func TestTheDigestStillMovesOnTheFactsItCovers(t *testing.T) {
	fixture := loadSourceAdmission(t)
	baseline := isolatedRegistry(t, nil)

	covered := map[string]func(vendorplugin.Model) vendorplugin.Model{
		"a model id respelled": func(m vendorplugin.Model) vendorplugin.Model {
			m.ID = m.ID + "-x"
			// The supersession and the billing contract have to follow the id.
			// A successor no row answers to and a contract whose plans never
			// name the model it hangs off are both refused before the digest is
			// ever computed — those two refusals firing here is the pair of new
			// gates working, and respelling both is what keeps this a test of
			// the DIGEST rather than of them.
			if m.SupersededBy != "" {
				m.SupersededBy = m.SupersededBy + "-x"
			}
			// AliasOf follows the id for exactly the same reason: checkAliases
			// refuses an alias whose target no row in the lineup answers to,
			// and that refusal happens at REGISTRATION — before a ceiling is
			// ever expanded — so leaving it behind would kill the mutant
			// harness in isolatedRegistry and report nothing about the digest.
			// openai's `astra` row is the one this reaches today.
			if m.AliasOf != "" {
				m.AliasOf = m.AliasOf + "-x"
			}
			if m.Pricing != nil {
				contract := m.Pricing.Clone()
				for i := range contract.Plans {
					contract.Plans[i].ApplicableModelIDs = append(contract.Plans[i].ApplicableModelIDs, m.ID)
				}
				m.Pricing = contract
			}
			return m
		},
		"an effort vocabulary narrowed by one word": func(m vendorplugin.Model) vendorplugin.Model {
			if len(m.Effort.Vocabulary) < 2 {
				return m
			}
			m.Effort.Vocabulary = append([]string(nil), m.Effort.Vocabulary[1:]...)
			if m.Effort.Recommended == m.Effort.Vocabulary[0] {
				return m
			}
			// The recommendation must stay inside its own vocabulary or the
			// row is refused before the digest is ever computed.
			m.Effort.Recommended = m.Effort.Vocabulary[len(m.Effort.Vocabulary)-1]
			return m
		},
	}

	for name, mutate := range covered {
		t.Run(name, func(t *testing.T) {
			mutations := map[vendorplugin.VendorID]func(vendorplugin.Model) vendorplugin.Model{}
			for vendor := range portedVendors {
				mutations[vendor] = mutate
			}
			mutated := isolatedRegistry(t, mutations)

			moved := 0
			for runtime, pinned := range fixture.Ceilings {
				ceiling := vendorplugin.V2Ceiling{
					Runtime:        vendorplugin.RuntimeID(runtime),
					Model:          vendorplugin.ModelID(pinned.Model),
					ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
					Effort:         pinned.Effort,
				}
				before, err := vendorplugin.ExpandV2Ceiling(baseline, ceiling)
				if err != nil {
					t.Fatalf("expanding %s against the unmutated registry: %v", runtime, err)
				}
				after, err := vendorplugin.ExpandV2Ceiling(mutated, ceiling)
				if err != nil {
					// A respelled model id makes the snapshot unable to find
					// the bound model at all, which is the expansion refusing
					// rather than narrowing. That is the digest reacting to the
					// fact, so it counts.
					moved++
					continue
				}
				if before.Digest() != after.Digest() {
					moved++
				}
			}
			if moved == 0 {
				t.Fatalf("%s left every pinned digest unchanged; the digest does not cover this fact, so its stability under the presentation mutants proves nothing", name)
			}
		})
	}
}

// TestTheCanonicalSerializationCarriesOnlyPairs is the structural half, and it
// reads the bytes rather than reasoning about them.
//
// Every line of the serialization must be the provider or a model with its
// efforts. A field that leaked in would have to appear as text, and the values
// looked for below are ones no model id or effort word could be: a lifecycle
// state, a currency, a source URL, a retrieval date.
func TestTheCanonicalSerializationCarriesOnlyPairs(t *testing.T) {
	fixture := loadSourceAdmission(t)
	registry := isolatedRegistry(t, nil)

	forbidden := []string{
		string(vendorplugin.LifecycleCurrent),
		string(vendorplugin.LifecyclePreview),
		string(vendorplugin.LifecycleLegacy),
		"token-plan-team-seat-credits",
		"USD",
		"https://",
		"2026-07-21",
		"recommended",
	}

	for runtime, pinned := range fixture.Ceilings {
		set, err := vendorplugin.ExpandV2Ceiling(registry, vendorplugin.V2Ceiling{
			Runtime:        vendorplugin.RuntimeID(runtime),
			Model:          vendorplugin.ModelID(pinned.Model),
			ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
			Effort:         pinned.Effort,
		})
		if err != nil {
			t.Fatalf("expanding %s: %v", runtime, err)
		}
		serialized := set.CanonicalSerialization()
		for _, needle := range forbidden {
			if strings.Contains(serialized, needle) {
				t.Errorf("runtime %s's canonical serialization contains %q; the digest is over (model, effort) pairs and nothing else\n%q", runtime, needle, serialized)
			}
		}
	}
}
