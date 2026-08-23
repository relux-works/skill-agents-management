package vendorplugin

import (
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestRegisteringAVendorNamingAnUnknownSystemIsRefused is AC2, and it is the
// negative test the whole layering rests on.
//
// If registration quietly succeeded, the vendor would sit in the registry
// declaring a harness nobody compiled in, and the failure would surface at
// launch — far from the declaration that caused it, and only for whoever
// happened to pick that model. The refusal names BOTH ids because the fix is
// either "register the system plugin" or "stop declaring it", and the operator
// cannot tell which without knowing both halves.
func TestRegisteringAVendorNamingAnUnknownSystemIsRefused(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[0].Systems = []agentic.SystemID{pangolinID, "opencode"}

	registry, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrUnknownAgenticSystem, "Register(vendor declaring an unregistered system)")
	requireMentions(t, err, string(narwhalID), "opencode", "narwhal-deep")

	if _, found := registry.Lookup(narwhalID); found {
		t.Fatal("the refused vendor is in the registry; the refusal was cosmetic")
	}
}

// The same refusal for a vendor whose ONLY declared system is unregistered:
// the check must not be satisfied by one system out of two resolving, which is
// the shape a loop with a misplaced early return produces.
func TestRegisteringAVendorWhoseOnlySystemIsUnknownIsRefused(t *testing.T) {
	vendor := newNarwhal()
	for i := range vendor.models {
		vendor.models[i].Systems = []agentic.SystemID{"opencode"}
	}

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrUnknownAgenticSystem, "Register(vendor whose only system is unregistered)")
	requireMentions(t, err, string(narwhalID), "opencode")
}

// The direction check must be reachable from the SECOND model too. A loop that
// checked only models[0] would pass every test written against a one-model
// double, which is why the double has two rows and this test moves the bad
// declaration to the last one.
func TestTheDependencyDirectionIsCheckedForEveryModel(t *testing.T) {
	vendor := newNarwhal()
	last := len(vendor.models) - 1
	vendor.models[last].Systems = []agentic.SystemID{"opencode"}

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrUnknownAgenticSystem, "Register(vendor whose last model names an unregistered system)")
	requireMentions(t, err, string(vendor.models[last].ID))
}

// A registry built without the agentic registry cannot answer the direction
// question at all, so it must refuse every vendor rather than admit one whose
// declared systems nobody checked. This is the bypass path around the gate:
// hand the constructor nothing and the check has no data to fail on.
func TestARegistryWithNoAgenticRegistryAdmitsNoVendor(t *testing.T) {
	registry := NewRegistry(nil)

	err := registry.Register(newNarwhal())
	requireErrorIs(t, err, ErrNoAgenticRegistry, "Register into a registry with no agentic layer")
	if len(registry.VendorIDs()) != 0 {
		t.Fatal("a vendor was admitted by a registry that cannot check its declared systems")
	}
}

// The happy path, so the refusals above mean "this declaration was rejected"
// rather than "nothing is ever accepted".
func TestRegisteringAVendorWhoseSystemsAreRegisteredSucceeds(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())

	if ids := registry.VendorIDs(); len(ids) != 1 || ids[0] != narwhalID {
		t.Fatalf("VendorIDs() = %v, want exactly the registered vendor", ids)
	}
	if _, found := registry.Lookup("NARWHAL"); !found {
		t.Error("a lookup under an unnormalized spelling missed the registration; the registry does not normalize on the way in")
	}
}

func TestRegisterRefusesNilAndDuplicates(t *testing.T) {
	registry := NewRegistry(systemsWithPangolin(t))

	requireErrorIs(t, registry.Register(nil), ErrNilVendor, "Register(nil)")

	if err := registry.Register(newNarwhal()); err != nil {
		t.Fatalf("Register(narwhal): %v", err)
	}
	requireErrorIs(t, registry.Register(newNarwhal()), ErrDuplicateVendor, "Register(narwhal) twice")
}

// A plugin that answers ID() differently across two reads would be registered
// under one name and held under another — one vendor with two names, and the
// limit-state file is keyed by the name.
func TestRegisterRefusesAnUnstableVendorID(t *testing.T) {
	vendor := newNarwhal()
	vendor.unstableID = "narwhal-eu"

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrUnstableVendorID, "Register(vendor whose ID() changes)")
	requireMentions(t, err, "narwhal", "narwhal-eu")
}

func TestRegisterRefusesAnUnnormalizedVendorID(t *testing.T) {
	vendor := newNarwhal()
	vendor.id = "Narwhal"

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	requireErrorIs(t, err, ErrUnnormalizedVendorID, "Register(vendor whose id is not its own normal form)")
	requireMentions(t, err, "Narwhal", "narwhal")
}

func TestRegisterRefusesAnUnusableVendorID(t *testing.T) {
	vendor := newNarwhal()
	vendor.id = "narwhal cloud"

	_, err := mustRegister(t, vendor, systemsWithPangolin(t))
	if err == nil {
		t.Fatal("a vendor id with a space was admitted")
	}
	requireMentions(t, err, "narwhal cloud", "lowercase")
}

// A model row an operator cannot choose deliberately is not a model row. The
// description is the field they read, and a blank one has to fail at
// registration rather than render as an empty column in a list of six.
func TestRegisterRefusesAModelWithNoUsageDescription(t *testing.T) {
	for _, blank := range []UsageDescription{"", "   ", "\t\n"} {
		vendor := newNarwhal()
		vendor.models[0].Description = blank

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		requireErrorIs(t, err, ErrDescriptionEmpty, "Register(model with description "+string(blank)+")")
		requireMentions(t, err, "narwhal-deep")
	}
}

// Ranking is never policy. A rank with no observation behind it is the
// ceiling-convenience argument — "it is the biggest one we have, so it is
// first" — with the reasoning left out, and the source repository has that
// recorded as a thing not to do.
//
// The blank tests are TrimSpace'd for the same reason the usage description's
// are: narrowed to == "", a basis of {Source: "   ", Observation: "\t"} passes
// and the rank is once again a number with nothing behind it — the refusal
// still exists and no longer covers the class it is for. So the whitespace-only
// spellings are pinned alongside the empty ones, at Model.Validate and at the
// production call site that has to refuse them, Registry.Register.
func TestRegisterRefusesARankWithNoEvidence(t *testing.T) {
	cases := map[string]CapabilityRank{
		"no basis at all":                   {Score: 10},
		"an empty basis":                    {Score: 10, Basis: []RankEvidence{}},
		"an evidence with no source":        {Score: 10, Basis: []RankEvidence{{Observation: "it feels stronger"}}},
		"an evidence that observed nothing": {Score: 10, Basis: []RankEvidence{{Source: "a hallway conversation"}}},
		"a source that is only whitespace":  {Score: 10, Basis: []RankEvidence{{Source: "   ", Observation: "41/50"}}},
		"an observation that is whitespace": {Score: 10, Basis: []RankEvidence{{Source: "eval", Observation: "\t"}}},
		"neither half more than whitespace": {Score: 10, Basis: []RankEvidence{{Source: " ", Observation: "\n "}}},
		"one sound evidence and one blank":  {Score: 10, Basis: []RankEvidence{{Source: "eval", Observation: "41/50"}, {Source: "  ", Observation: "  "}}},
		// The zero value must not pass for the bottom of the scale. A model
		// with no score has not been placed in the lineup, and admitting it
		// would put an unplaced row at the end of every derived ordering as
		// though somebody had put it there.
		"no score at all":                {Basis: []RankEvidence{{Source: "eval", Observation: "41/50"}}},
		"a score below the start of one": {Score: -1, Basis: []RankEvidence{{Source: "eval", Observation: "41/50"}}},
	}
	for name, rank := range cases {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			vendor.models[0].Rank = rank

			if err := vendor.models[0].Validate(); err == nil {
				t.Fatalf("Model.Validate admitted a model ranked with %s", name)
			}

			_, err := mustRegister(t, vendor, systemsWithPangolin(t))
			requireErrorIs(t, err, ErrRankInvalid, "Register(model ranked with "+name+")")
		})
	}
}

// A tie is a statement, so two models at one SCORE is admitted — and the
// derived ordering still has to be total.
//
// This replaced a refusal in v0.2.0 and the replacement is the point of the
// change: the board that owns the same lineups records genuine ties (an alias
// and its dated snapshot; two harnesses' catalogues scored against one broker),
// and a refusal here forced the declaration to invent an ordering nobody
// observed. What the tie must NOT do is leave a caller without an order, so
// this asserts both halves: the registration is admitted, and Lineup numbers
// the two rows 1 and 2 by declaration order while marking both tied.
func TestRegisterAdmitsTwoModelsAtOneScoreAndTheDerivedOrderStaysTotal(t *testing.T) {
	vendor := newNarwhal()
	vendor.models[1].Rank.Score = vendor.models[0].Rank.Score

	registry, err := mustRegister(t, vendor, systemsWithPangolin(t))
	if err != nil {
		t.Fatalf("Register(vendor scoring two models equally): %v; a tie is the vendor stating the two are equal, not a malformed lineup", err)
	}
	plugin, ok := registry.Lookup(narwhalID)
	if !ok {
		t.Fatalf("vendor %s is not registered after a successful Register", narwhalID)
	}

	ranked := LineupOf(plugin)
	if len(ranked) != 2 {
		t.Fatalf("Lineup returned %d rows for a two-model vendor", len(ranked))
	}
	if ranked[0].Position != 1 || ranked[1].Position != 2 {
		t.Errorf("the tied lineup derived positions %d and %d; a derived order must still be total and gapless",
			ranked[0].Position, ranked[1].Position)
	}
	if string(ranked[0].Model.ID) != "narwhal-deep" || string(ranked[1].Model.ID) != "narwhal-flat" {
		t.Errorf("the tie broke to %q then %q; declaration order puts narwhal-deep first",
			ranked[0].Model.ID, ranked[1].Model.ID)
	}
	for _, row := range ranked {
		if !row.Tied {
			t.Errorf("model %q sits at position %d on a shared score and does not report Tied; a caller reading the position alone would take an invented order for an observed one",
				row.Model.ID, row.Position)
		}
	}
}

// Effort is a required per-model axis and no default is injected anywhere, so
// a declaration that could not answer "which words does this model take" is
// refused at registration rather than discovered at launch.
func TestRegisterRefusesAContradictoryEffortDeclaration(t *testing.T) {
	cases := map[string]EffortDeclaration{
		"required with no vocabulary": {Support: agentic.EffortSupportRequired},
		"required with a recommendation outside its own vocabulary": {
			Support: agentic.EffortSupportRequired, Vocabulary: []string{"shallow", "deep"}, Recommended: "medium",
		},
		"required with no recommendation at all": {
			Support: agentic.EffortSupportRequired, Vocabulary: []string{"shallow", "deep"},
		},
		"required with a blank word": {
			Support: agentic.EffortSupportRequired, Vocabulary: []string{"deep", "  "}, Recommended: "deep",
		},
		"required with a repeated word": {
			Support: agentic.EffortSupportRequired, Vocabulary: []string{"deep", "deep"}, Recommended: "deep",
		},
		"no axis but a vocabulary anyway": {
			Support: agentic.EffortSupportNone, Vocabulary: []string{"deep"},
		},
		"no axis but a recommendation anyway": {
			Support: agentic.EffortSupportNone, Recommended: "deep",
		},
		"an effort support nobody declared": {Support: agentic.EffortSupport(9)},
	}
	for name, declaration := range cases {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			vendor.models[0].Effort = declaration

			_, err := mustRegister(t, vendor, systemsWithPangolin(t))
			requireErrorIs(t, err, ErrEffortDeclaration, "Register(model with effort "+name+")")
		})
	}
}

func TestRegisterRefusesUnusableModelRows(t *testing.T) {
	t.Run("no models at all", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models = nil

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		requireErrorIs(t, err, ErrNoModels, "Register(vendor with no models)")
	})

	t.Run("a blank model id", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models[0].ID = "   "

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		if err == nil {
			t.Fatal("a blank model id was admitted")
		}
	})

	t.Run("a model id carrying whitespace", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models[0].ID = "narwhal deep"

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		if err == nil {
			t.Fatal("a model id with a space was admitted; it is sent to the vendor verbatim")
		}
	})

	t.Run("the same model id twice", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models[1].ID = vendor.models[0].ID

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		requireErrorIs(t, err, ErrDuplicateModel, "Register(vendor declaring one model id twice)")
	})

	t.Run("a model no harness can drive", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models[0].Systems = nil

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		requireErrorIs(t, err, ErrModelInvalid, "Register(model declaring no systems)")
	})

	t.Run("a model declaring one system under two spellings", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models[0].Systems = []agentic.SystemID{pangolinID, pangolinID}

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		requireErrorIs(t, err, ErrModelInvalid, "Register(model declaring a system twice)")
	})

	t.Run("a model declaring an unnormalized system id", func(t *testing.T) {
		vendor := newNarwhal()
		vendor.models[0].Systems = []agentic.SystemID{"Pangolin"}

		_, err := mustRegister(t, vendor, systemsWithPangolin(t))
		requireErrorIs(t, err, ErrModelInvalid, "Register(model declaring an unnormalized system id)")
		requireMentions(t, err, "Pangolin", "pangolin")
	})
}

// VendorIDs hands out a copy: a caller that sorts, appends to or overwrites
// the answer must not be able to reorder or grow the registry through it.
func TestVendorIDsDoesNotAliasTheRegistry(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())

	ids := registry.VendorIDs()
	ids[0] = "mutated"

	if again := registry.VendorIDs(); len(again) != 1 || again[0] != narwhalID {
		t.Fatalf("VendorIDs() = %v after mutating a previous answer; the answer aliased the registry", again)
	}
}

// Both layers fold identifiers through the SAME rule (internal/ident), and
// this is what holds them to it. Two normalizations that agree today and drift
// tomorrow is the duplicate-charset failure invariant 5 of
// docs/architecture.md names — and the drift would be invisible until an id
// keyed two different limit-state files.
func TestBothLayersNormalizeIdentifiersIdentically(t *testing.T) {
	for _, raw := range []string{"claude-code", "  Codex  ", "ANTHROPIC", "a", "agy-2-preview"} {
		system, systemErr := agentic.NormalizeSystemID(raw)
		vendor, vendorErr := NormalizeVendorID(raw)
		runtime, runtimeErr := NormalizeRuntimeID(raw)
		if systemErr != nil || vendorErr != nil || runtimeErr != nil {
			t.Fatalf("%q: one layer refused what another accepted: system=%v vendor=%v runtime=%v", raw, systemErr, vendorErr, runtimeErr)
		}
		if string(system) != string(vendor) || string(vendor) != string(runtime) {
			t.Errorf("%q folded to system=%q vendor=%q runtime=%q; the layers do not share one normalization", raw, system, vendor, runtime)
		}
	}
	for _, raw := range []string{"", "claude code", "claude--code", "claude_code"} {
		_, systemErr := agentic.NormalizeSystemID(raw)
		_, vendorErr := NormalizeVendorID(raw)
		_, runtimeErr := NormalizeRuntimeID(raw)
		if systemErr == nil || vendorErr == nil || runtimeErr == nil {
			t.Errorf("%q: one layer admitted what another refused: system=%v vendor=%v runtime=%v", raw, systemErr, vendorErr, runtimeErr)
		}
	}
}

// The refusal has to be fixable from its own text, and each layer names its
// own kind of id and an example of one. A vendor author told "for example
// \"claude-code\"" has been given advice about the wrong layer.
func TestIdentifierRefusalsNameTheirOwnLayer(t *testing.T) {
	_, err := NormalizeVendorID("Narwhal Cloud")
	requireMentions(t, err, "vendor id", "Narwhal Cloud", "anthropic")

	_, err = NormalizeRuntimeID("Tusk Runtime")
	requireMentions(t, err, "runtime id", "Tusk Runtime", "claude")
}
