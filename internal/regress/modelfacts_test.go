package regress

import (
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// CLASS 5 — the model rows this binary actually carries, and the line between
// what they say and what a launch may do.
//
// v0.2.0 moved the board's remaining model facts here: the capability score
// with its ties, the lineup state, the supersession, the display
// recommendation, the context window and the billing contract. Two classes of
// failure came with them, and this file is the landing gate for both.
//
//   - ROWS GOING MISSING. The rows now live in TWO homes — four vendor plugins
//     and one vendor-unresolved runtime declaration — and the second home is
//     the one with no plugin behind it. A check that walked only the registered
//     vendors would report green on 42 of 45 rows forever.
//   - PRESENTATION DECIDING ADMISSION. Every one of the new fields is display
//     and migration evidence. The day one of them reaches an admitted-pair
//     digest, a truthful correction to a price or a lifecycle silently changes
//     who may spawn — which is the exact regression the extraction source spent
//     a task removing from its own ordered ceilings.
//
// pkg/vendorplugin holds both to the full fixture, mutant by mutant. What is
// here is the fast cross-cutting shape, driven through the same registry a
// binary launches through.

// boardModelCount is the number of rows the board's table carries, stated here
// independently of any fixture — this package reads none.
//
// It is a landing gate, so it is deliberately a number somebody has to change
// on purpose. A row added to a vendor without its counterpart leaving the board
// is a divergence between two repositories, and it must not be absorbable by a
// test that counts whatever it finds.
const boardModelCount = 45

// carriedRows collects every model row this binary carries, from both homes,
// and reports which home each came from.
func carriedRows(t *testing.T) (fromVendors, fromUnresolved []vendorplugin.Model) {
	t.Helper()
	for _, vendor := range realVendors(t) {
		fromVendors = append(fromVendors, vendor.Models()...)
	}
	for _, declaration := range vendorplugin.Default.RuntimeDeclarations() {
		if declaration.VendorResolved() && len(declaration.Models) > 0 {
			t.Errorf("runtime %q resolves to vendor %q and declares %d model rows of its own; that is two tables of one fact and Validate is supposed to refuse it",
				declaration.ID, declaration.Vendor, len(declaration.Models))
		}
		fromUnresolved = append(fromUnresolved, declaration.Models...)
	}
	return fromVendors, fromUnresolved
}

// TestEveryModelRowThisBinaryCarriesIsWholeAndAccountedFor is the missing-rows
// half.
//
// It adds the two homes up to the stated total, refuses a row that appears in
// both, and drives every row through the same Validate the registration path
// uses — so a row that reached this binary through the unresolved shape is held
// to the standard a vendor's row is.
func TestEveryModelRowThisBinaryCarriesIsWholeAndAccountedFor(t *testing.T) {
	fromVendors, fromUnresolved := carriedRows(t)
	if len(fromUnresolved) == 0 {
		t.Fatal("no vendor-unresolved runtime carries a model row, so the second home is untested; muse's two rows have no vendor plugin and are exactly what a vendors-only check would miss")
	}
	if total := len(fromVendors) + len(fromUnresolved); total != boardModelCount {
		t.Errorf("this binary carries %d model rows (%d from vendor plugins, %d from vendor-unresolved runtimes) and the board table has %d; a changed count is a divergence between two repositories and has to be argued",
			total, len(fromVendors), len(fromUnresolved), boardModelCount)
	}

	seen := make([]vendorplugin.ModelID, 0, boardModelCount)
	for _, model := range append(append([]vendorplugin.Model(nil), fromVendors...), fromUnresolved...) {
		if err := model.Validate(); err != nil {
			t.Errorf("a row this binary carries is not a usable declaration: %v", err)
		}
		for _, already := range seen {
			if already == model.ID {
				t.Errorf("model %q is carried twice; one row, one home", model.ID)
			}
		}
		seen = append(seen, model.ID)
	}
}

// TestTheSourceTiesAreStillVisibleInTheCarriedRows is the tie half of the same
// class, and it is here rather than only in the vendor package because a tie is
// exactly the kind of fact a well-meaning refactor flattens.
//
// The board's ranking consumers read scores WITH ties. A port that separated
// two equal models — by nudging a score, or by going back to a tie-free
// position — would state an ordering nobody observed, and every downstream
// display would carry the invention as though it were evidence.
func TestTheSourceTiesAreStillVisibleInTheCarriedRows(t *testing.T) {
	fromVendors, fromUnresolved := carriedRows(t)
	all := append(append([]vendorplugin.Model(nil), fromVendors...), fromUnresolved...)

	scoreOf := func(id vendorplugin.ModelID) (int, bool) {
		for _, model := range all {
			if model.ID == id {
				return model.Rank.Score, true
			}
		}
		return 0, false
	}

	// One tie per home, named rather than counted: the alias pair a vendor
	// owns, and the alias pair nobody owns.
	for _, tie := range [][2]vendorplugin.ModelID{
		{"claude-haiku-4-5", "claude-haiku-4-5-20251001"},
		{"muse-spark-1.3-contributor", "muse-spark"},
	} {
		left, known := scoreOf(tie[0])
		if !known {
			t.Errorf("this binary carries no model %q, which the board ties with %q", tie[0], tie[1])
			continue
		}
		right, known := scoreOf(tie[1])
		if !known {
			t.Errorf("this binary carries no model %q, which the board ties with %q", tie[1], tie[0])
			continue
		}
		if left != right {
			t.Errorf("%q scores %d and %q scores %d; the board records them equal, and separating two equal models asserts an ordering nobody observed",
				tie[0], left, tie[1], right)
		}
	}
}

// presentationFacts are the fields that must not be able to move an admitted
// set, each with a transform applied to EVERY row.
var presentationFacts = map[string]func(vendorplugin.Model) vendorplugin.Model{
	"a lineup state corrected": func(m vendorplugin.Model) vendorplugin.Model {
		m.Lifecycle = vendorplugin.LifecycleLegacy
		m.SupersededBy = ""
		return m
	},
	"a display recommendation withdrawn": func(m vendorplugin.Model) vendorplugin.Model {
		m.Recommended = false
		return m
	},
	"a capability score re-ranked": func(m vendorplugin.Model) vendorplugin.Model {
		m.Rank.Score = 1
		return m
	},
	"a published price corrected": func(m vendorplugin.Model) vendorplugin.Model {
		if m.Pricing == nil {
			return m
		}
		contract := m.Pricing.Clone()
		for i := range contract.Plans {
			contract.Plans[i].MonthlyUSD *= 3
		}
		m.Pricing = contract
		return m
	},
}

// TestCorrectingAPresentationFactCannotMoveAnAdmittedSet is the second half of
// the class, driven through the real ExpandV2Ceiling over a real frozen
// ceiling.
//
// The pairing this package insists on is inside the test: the same harness that
// shows the presentation facts leaving the digest alone also shows an effort
// vocabulary MOVING it. Without that second half, every subtest here would pass
// against a digest that covered nothing at all.
func TestCorrectingAPresentationFactCannotMoveAnAdmittedSet(t *testing.T) {
	ceiling := vendorplugin.V2Ceiling{
		Runtime:        "claude",
		Model:          "claude-sonnet-5",
		ModelCriterion: vendorplugin.ModelCriterionLessOrEqual,
		Effort:         "high",
	}
	baseline, err := vendorplugin.ExpandV2Ceiling(vendorplugin.Default, ceiling)
	if err != nil {
		t.Fatalf("expanding the baseline ceiling: %v", err)
	}
	if baseline.IsEmpty() {
		t.Fatal("the baseline ceiling admits no pair, so nothing below could show a digest moving or holding still")
	}

	for name, mutate := range presentationFacts {
		t.Run(name, func(t *testing.T) {
			registry := registryWithMutatedModels(t, mutate)
			set, err := vendorplugin.ExpandV2Ceiling(registry, ceiling)
			if err != nil {
				t.Fatalf("expanding with %s: %v", name, err)
			}
			if set.Digest() != baseline.Digest() {
				t.Errorf("%s moved the admitted set's digest from %s to %s; every one of these fields is display and migration evidence, and the day one decides admission a truthful correction changes who may spawn",
					name, baseline.Digest(), set.Digest())
			}
		})
	}

	t.Run("and the digest still moves on a fact it covers", func(t *testing.T) {
		registry := registryWithMutatedModels(t, func(m vendorplugin.Model) vendorplugin.Model {
			if m.Effort.Support != agentic.EffortSupportRequired || len(m.Effort.Vocabulary) < 2 {
				return m
			}
			m.Effort.Vocabulary = append([]string(nil), m.Effort.Vocabulary[1:]...)
			if m.Effort.Recommended != m.Effort.Vocabulary[0] {
				m.Effort.Recommended = m.Effort.Vocabulary[0]
			}
			return m
		})
		set, err := vendorplugin.ExpandV2Ceiling(registry, ceiling)
		if err != nil {
			t.Fatalf("expanding with a narrowed vocabulary: %v", err)
		}
		if set.Digest() == baseline.Digest() {
			t.Fatalf("narrowing every effort vocabulary by one word left the digest at %s; the digest covers nothing this harness can move, so its stability above proves nothing",
				set.Digest())
		}
	})
}

// mutatedVendor re-declares a registered plugin's rows through a transform.
//
// It wraps the REAL plugin rather than reimplementing one, so a mutant differs
// from production in exactly the fact under test. A hand-written fake here
// would drift from the real rows, and the mutants would then be proving
// something about the fake.
type mutatedVendor struct {
	inner  vendorplugin.Vendor
	mutate func(vendorplugin.Model) vendorplugin.Model
}

func (m mutatedVendor) ID() vendorplugin.VendorID { return m.inner.ID() }

func (m mutatedVendor) Models() []vendorplugin.Model {
	rows := vendorplugin.CloneModels(m.inner.Models())
	for i := range rows {
		rows[i] = m.mutate(rows[i])
	}
	return rows
}

func (m mutatedVendor) Availability(q vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return m.inner.Availability(q)
}

func (m mutatedVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return m.inner.Spawn(sc)
}

// registryWithMutatedModels builds an isolated registry carrying every real
// vendor with the transform applied. Isolated because the default registry is
// process-wide: mutating it would leak into every other test in this binary.
func registryWithMutatedModels(t *testing.T, mutate func(vendorplugin.Model) vendorplugin.Model) *vendorplugin.Registry {
	t.Helper()
	registry := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding the frozen runtimes: %v", err)
	}
	for _, vendor := range realVendors(t) {
		if err := registry.Register(mutatedVendor{inner: vendor, mutate: mutate}); err != nil {
			t.Fatalf("registering the mutated vendor %s: %v", vendor.ID(), err)
		}
	}
	return registry
}
