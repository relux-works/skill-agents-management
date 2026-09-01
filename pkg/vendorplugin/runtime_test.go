package vendorplugin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestTheSixHistoricalRuntimesAreSeededWithTheirFrozenBindings is AC3.
//
// The ids and their pairs are read from the extraction source's
// pkg/remoteconfig/runtimeid frozen table. docs/architecture.md says these ids
// remain valid forever — they feed admitted-pair digests, limit-state
// filenames and free-text records downstream — so this test pins the whole
// table rather than spot-checking it. A rebind here is a compatibility break,
// and it should read as one in the diff.
func TestTheSixHistoricalRuntimesAreSeededWithTheirFrozenBindings(t *testing.T) {
	want := []struct {
		id     RuntimeID
		system agentic.SystemID
		vendor VendorID
	}{
		{"agy", "antigravity", "google"},
		{"claude", "claude-code", "anthropic"},
		{"codex", "codex", "openai"},
		{"gemini", "gemini-cli", "google"},
		{"muse", "muse", VendorUnresolved},
		{"qwen", "qwen-code", "alibaba"},
	}

	registry := NewRegistry(agentic.NewRegistry())
	if err := SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("SeedFrozenRuntimes: %v", err)
	}

	declarations := registry.RuntimeDeclarations()
	if len(declarations) != len(want) {
		t.Fatalf("seeded %d runtimes, want %d: %v", len(declarations), len(want), declarations)
	}
	for i, expected := range want {
		got := declarations[i]
		if got.ID != expected.id || got.System != expected.system || got.Vendor != expected.vendor {
			t.Errorf("runtime %d = (%s: %s × %s), want (%s: %s × %s)",
				i, got.ID, got.System, got.VendorLabel(),
				expected.id, expected.system, RuntimeDeclaration{Vendor: expected.vendor}.VendorLabel())
		}
	}
}

// The default registry every binary carries is seeded at init, so the six ids
// exist before any plugin has registered. A declaration does not need its
// plugins present, and making it need them would put the seed order on the
// critical path of every binary.
func TestTheDefaultRegistryCarriesTheFrozenRuntimes(t *testing.T) {
	for _, id := range []RuntimeID{"claude", "codex", "qwen", "gemini", "agy", "muse"} {
		if _, declared := Default.RuntimeDeclarationOf(id); !declared {
			t.Errorf("the default registry does not declare runtime %q", id)
		}
	}
}

// muse's broker is recorded UNKNOWN with a checked-and-empty evidence list in
// the extraction source. Carrying that honestly means the declaration says
// "looked for, not found" rather than naming a plausible vendor — the binding
// keys limit state, and a guess there is a fabrication with consequences.
func TestMusesBrokerIsCarriedAsCheckedAndEmpty(t *testing.T) {
	declaration, declared := Default.RuntimeDeclarationOf("muse")
	if !declared {
		t.Fatal("muse is not declared")
	}
	if declaration.VendorResolved() {
		t.Fatalf("muse is bound to vendor %q; the source records its broker as unknown and nothing here may guess one", declaration.Vendor)
	}
	if len(declaration.Broker.Checked) == 0 {
		t.Error("muse's unresolved broker records no source; that is an unfinished declaration, not a finding")
	}
	if declaration.Broker.Found != "" {
		t.Errorf("muse records %q as having established a vendor while naming none", declaration.Broker.Found)
	}
}

// An unresolved vendor is a legal declaration and an UNLAUNCHABLE one, and the
// refusal is its own error: "the binding is unknown" and "the plugin is not
// compiled in" have different fixes, and collapsing them would tell an
// operator to install something nobody has identified.
func TestResolvingAnUnresolvedVendorRuntimeIsRefusedOnItsOwnTerms(t *testing.T) {
	registry := NewRegistry(systemsWithPangolin(t))
	if err := registry.DeclareRuntime(RuntimeDeclaration{
		ID:     "unbrokered",
		System: pangolinID,
		Vendor: VendorUnresolved,
		Broker: BrokerProvenance{Checked: []string{"the source's frozen table"}},
	}); err != nil {
		t.Fatalf("DeclareRuntime(unbrokered): %v", err)
	}

	_, err := registry.ResolveRuntime("unbrokered")
	requireErrorIs(t, err, ErrRuntimeVendorUnresolved, "ResolveRuntime(runtime with an unresolved broker)")
	requireMentions(t, err, "unbrokered", "the source's frozen table")

	if _, err := BuildLaunch(context.Background(), registry, SpawnRequest{Runtime: "unbrokered", Model: "anything"}, agentic.LaunchModeExec); err == nil {
		t.Fatal("a launch through an unresolved-vendor runtime was built; there is no vendor to pick a model from")
	}
}

// The four resolution refusals are four different facts, and a caller acts on
// each differently: declare the runtime, compile in the system plugin, compile
// in the vendor plugin, or find out who the broker is.
func TestResolutionRefusalsAreDistinctFacts(t *testing.T) {
	t.Run("never declared", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		_, err := registry.ResolveRuntime("nosuch")
		requireErrorIs(t, err, ErrUnknownRuntime, "ResolveRuntime(undeclared)")
	})

	t.Run("system plugin not registered", func(t *testing.T) {
		registry := NewRegistry(agentic.NewRegistry())
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("DeclareRuntime: %v", err)
		}
		_, err := registry.ResolveRuntime(tuskID)
		requireErrorIs(t, err, ErrRuntimeSystemUnregistered, "ResolveRuntime(system not compiled in)")
		requireMentions(t, err, string(pangolinID))
	})

	t.Run("vendor plugin not registered", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("DeclareRuntime: %v", err)
		}
		_, err := registry.ResolveRuntime(tuskID)
		requireErrorIs(t, err, ErrRuntimeVendorUnregistered, "ResolveRuntime(vendor not compiled in)")
		requireMentions(t, err, string(narwhalID))
	})

	t.Run("resolved", func(t *testing.T) {
		registry := registerNarwhal(t, newNarwhal())
		runtime, err := registry.ResolveRuntime(tuskID)
		if err != nil {
			t.Fatalf("ResolveRuntime(tusk): %v", err)
		}
		if runtime.System == nil || runtime.Vendor == nil {
			t.Fatal("a resolved runtime handed back a nil plugin; nothing downstream should have to null-check a launch")
		}
		if runtime.SystemID != pangolinID || runtime.VendorID != narwhalID {
			t.Fatalf("resolved (%s × %s), want (%s × %s)", runtime.SystemID, runtime.VendorID, pangolinID, narwhalID)
		}
	})

	// The four subtests above prove each branch is REACHABLE. Reachability is
	// not distinctness: alias any two of these sentinels together and every
	// positive assertion above still passes, while a caller that branches on
	// the difference silently stops being able to.
	//
	// The collapse that matters is ErrRuntimeVendorUnresolved into
	// ErrRuntimeVendorUnregistered. Their fixes are different — "find out who
	// the broker is" and "compile the vendor plugin in" — and muse is the
	// runtime carrying the first. Aliased, muse starts telling an operator to
	// install a vendor nobody has identified, which is the exact failure
	// VendorUnresolved exists to prevent. The port stories inherit this
	// branch, so what is pinned here is the difference itself.
	t.Run("no refusal answers to another refusal's sentinel", func(t *testing.T) {
		sentinels := map[string]error{
			"unknown runtime":      ErrUnknownRuntime,
			"system unregistered":  ErrRuntimeSystemUnregistered,
			"vendor unregistered":  ErrRuntimeVendorUnregistered,
			"vendor unresolved":    ErrRuntimeVendorUnresolved,
			"vendor lookup missed": ErrVendorNotRegistered,
		}
		cases := map[string]struct {
			want    string
			resolve func(t *testing.T) error
		}{
			"never declared": {
				want: "unknown runtime",
				resolve: func(t *testing.T) error {
					registry := NewRegistry(systemsWithPangolin(t))
					_, err := registry.ResolveRuntime("nosuch")
					return err
				},
			},
			"the system plugin is not compiled in": {
				want: "system unregistered",
				resolve: func(t *testing.T) error {
					registry := NewRegistry(agentic.NewRegistry())
					if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
						t.Fatalf("DeclareRuntime: %v", err)
					}
					_, err := registry.ResolveRuntime(tuskID)
					return err
				},
			},
			"the vendor plugin is not compiled in": {
				want: "vendor unregistered",
				resolve: func(t *testing.T) error {
					registry := NewRegistry(systemsWithPangolin(t))
					if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
						t.Fatalf("DeclareRuntime: %v", err)
					}
					_, err := registry.ResolveRuntime(tuskID)
					return err
				},
			},
			// Driven through SeedFrozenRuntimes rather than a hand-built
			// declaration: this is the shipped muse row, not a shape that
			// resembles it.
			"the frozen muse binding was never established": {
				want: "vendor unresolved",
				resolve: func(t *testing.T) error {
					registry := NewRegistry(systemsNamed(t, "muse"))
					if err := SeedFrozenRuntimes(registry); err != nil {
						t.Fatalf("SeedFrozenRuntimes: %v", err)
					}
					_, err := registry.ResolveRuntime("muse")
					return err
				},
			},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				err := tc.resolve(t)
				if err == nil {
					t.Fatalf("%s resolved without error", name)
				}
				if !errors.Is(err, sentinels[tc.want]) {
					t.Fatalf("%s: err = %v, want %v", name, err, sentinels[tc.want])
				}
				for label, other := range sentinels {
					if label == tc.want {
						continue
					}
					if errors.Is(err, other) {
						t.Errorf("COLLAPSE: %s also answers to %q (%v); a caller can no longer branch on the difference", name, label, other)
					}
				}
			})
		}
	})
}

// The F2 collision policy, both directions. A MATCHING redeclaration is legal
// and idempotent — two packages declaring the same pair have not disagreed —
// and a CONFLICTING one is refused, because a frozen id rebound to a different
// pair orphans the state keyed by it without a single error anywhere.
func TestRuntimeDeclarationCollisionPolicy(t *testing.T) {
	t.Run("a matching redeclaration is idempotent", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("first DeclareRuntime: %v", err)
		}
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("a matching redeclaration was refused: %v", err)
		}
		if declarations := registry.RuntimeDeclarations(); len(declarations) != 1 {
			t.Fatalf("declaring the same pair twice produced %d declarations", len(declarations))
		}
	})

	t.Run("a matching redeclaration with different provenance wording is still idempotent", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("first DeclareRuntime: %v", err)
		}
		reworded := tuskDeclaration()
		reworded.Broker = BrokerProvenance{Checked: []string{"a differently worded source"}, Found: "the same pair, said another way"}
		if err := registry.DeclareRuntime(reworded); err != nil {
			t.Fatalf("a redeclaration of the same BINDING was refused over its prose: %v", err)
		}
	})

	t.Run("a conflicting vendor is refused", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("first DeclareRuntime: %v", err)
		}
		conflicting := tuskDeclaration()
		conflicting.Vendor = "walrus"

		err := registry.DeclareRuntime(conflicting)
		requireErrorIs(t, err, ErrRuntimeConflict, "DeclareRuntime(same id, different vendor)")
		requireMentions(t, err, string(tuskID), string(narwhalID), "walrus")

		declaration, _ := registry.RuntimeDeclarationOf(tuskID)
		if declaration.Vendor != narwhalID {
			t.Fatalf("the conflicting declaration won: %s is now bound to %q", tuskID, declaration.Vendor)
		}
	})

	t.Run("a conflicting system is refused", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := registry.DeclareRuntime(tuskDeclaration()); err != nil {
			t.Fatalf("first DeclareRuntime: %v", err)
		}
		conflicting := tuskDeclaration()
		conflicting.System = "opencode"

		err := registry.DeclareRuntime(conflicting)
		requireErrorIs(t, err, ErrRuntimeConflict, "DeclareRuntime(same id, different system)")
		requireMentions(t, err, string(tuskID), string(pangolinID), "opencode")
	})

	t.Run("rebinding a frozen id is refused", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		if err := SeedFrozenRuntimes(registry); err != nil {
			t.Fatalf("SeedFrozenRuntimes: %v", err)
		}
		err := registry.DeclareRuntime(RuntimeDeclaration{
			ID:     "claude",
			System: "claude-code",
			Vendor: "anthropic-eu",
			Broker: BrokerProvenance{Checked: []string{"a well-meaning refactor"}, Found: "a regional account"},
		})
		requireErrorIs(t, err, ErrRuntimeConflict, "DeclareRuntime(rebinding a frozen id)")
	})

	t.Run("seeding twice is the idempotent case", func(t *testing.T) {
		registry := NewRegistry(agentic.NewRegistry())
		if err := SeedFrozenRuntimes(registry); err != nil {
			t.Fatalf("first seed: %v", err)
		}
		if err := SeedFrozenRuntimes(registry); err != nil {
			t.Fatalf("second seed: %v", err)
		}
		if declarations := registry.RuntimeDeclarations(); len(declarations) != len(frozenRuntimes) {
			t.Fatalf("seeding twice produced %d declarations, want %d", len(declarations), len(frozenRuntimes))
		}
	})
}

func TestSystemOnlyRuntimeRedeclarationRequiresEquivalentModelAuthority(t *testing.T) {
	t.Run("reordered rows are semantically equivalent", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		declaration := systemOnlyAuthorityDeclaration()
		if err := registry.DeclareRuntime(declaration); err != nil {
			t.Fatalf("first DeclareRuntime: %v", err)
		}

		reordered := declaration.clone()
		reordered.Models[0], reordered.Models[1] = reordered.Models[1], reordered.Models[0]
		if err := registry.DeclareRuntime(reordered); err != nil {
			t.Fatalf("reordered equivalent redeclaration was refused: %v", err)
		}
	})

	t.Run("exact validated pricing authority repeats idempotently", func(t *testing.T) {
		registry := NewRegistry(systemsWithPangolin(t))
		declaration := systemOnlyAuthorityDeclaration()
		declaration.Models[0].Pricing = systemOnlyAuthorityPricing(declaration.Models[0].ID)
		if err := registry.DeclareRuntime(declaration); err != nil {
			t.Fatalf("first DeclareRuntime with finite pricing: %v", err)
		}
		if err := registry.DeclareRuntime(declaration.clone()); err != nil {
			t.Fatalf("exact validated redeclaration with finite pricing: %v", err)
		}
	})

	t.Run("non-finite pricing is refused before declaration storage", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			value float64
		}{
			{"NaN", math.NaN()},
			{"positive infinity", math.Inf(1)},
			{"negative infinity", math.Inf(-1)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				registry := NewRegistry(systemsWithPangolin(t))
				declaration := systemOnlyAuthorityDeclaration()
				declaration.Models[0].Pricing = systemOnlyAuthorityPricing(declaration.Models[0].ID)
				declaration.Models[0].Pricing.Plans[0].MonthlyUSD = tc.value

				err := registry.DeclareRuntime(declaration)
				requireErrorIs(t, err, ErrPricingInvalid, "DeclareRuntime(system-only non-finite pricing)")
				requireMentions(t, err, "non-finite monthly price")
				if _, stored := registry.RuntimeDeclarationOf(declaration.ID); stored {
					t.Fatal("DeclareRuntime persisted a declaration after refusing its non-finite pricing")
				}
			})
		}
	})

	mutations := []struct {
		name   string
		mutate func(*RuntimeDeclaration)
	}{
		{"model id", func(d *RuntimeDeclaration) { d.Models[0].ID = "authority-one-renamed" }},
		{"usage description", func(d *RuntimeDeclaration) { d.Models[0].Description = "changed authority description" }},
		{"effort support", func(d *RuntimeDeclaration) {
			d.Models[0].Effort = EffortDeclaration{Support: agentic.EffortSupportNone}
		}},
		{"effort vocabulary", func(d *RuntimeDeclaration) {
			d.Models[0].Effort.Vocabulary = []string{"low", "medium", "high"}
		}},
		{"effort recommendation", func(d *RuntimeDeclaration) {
			d.Models[0].Effort.Recommended = "low"
		}},
		{"model system membership", func(d *RuntimeDeclaration) {
			d.Models[0].Systems = append(d.Models[0].Systems, "another-system")
		}},
		{"capability rank", func(d *RuntimeDeclaration) { d.Models[0].Rank.Score++ }},
		{"rank evidence", func(d *RuntimeDeclaration) { d.Models[0].Rank.Basis[0].Observation = "changed evidence" }},
		{"lifecycle", func(d *RuntimeDeclaration) { d.Models[0].Lifecycle = LifecyclePreview }},
		{"supersession", func(d *RuntimeDeclaration) {
			d.Models[0].Lifecycle = LifecycleLegacy
			d.Models[0].SupersededBy = d.Models[1].ID
		}},
		{"display recommendation", func(d *RuntimeDeclaration) { d.Models[0].Recommended = true }},
		{"context window", func(d *RuntimeDeclaration) { d.Models[0].ContextWindowTokens++ }},
		{"cache budget", func(d *RuntimeDeclaration) {
			value := int64(6_442_450_944)
			d.Models[0].CacheBudgetBytes = &value
		}},
		{"pricing", func(d *RuntimeDeclaration) {
			d.Models[0].Pricing = &Pricing{
				BillingModel: "subscription",
				Edition:      "fixture",
				Currency:     "USD",
				QuotaPeriod:  "month",
				Plans: []PricingPlan{{
					Name:               "fixture",
					MonthlyUSD:         1,
					MonthlyCredits:     1,
					ApplicableModelIDs: []ModelID{d.Models[0].ID},
				}},
				SourceURL: "https://example.invalid/pricing",
				AsOf:      "2026-08-29",
			}
		}},
		{"publisher", func(d *RuntimeDeclaration) { d.Models[0].Publisher = "another-publisher" }},
		{"family", func(d *RuntimeDeclaration) { d.Models[0].Family = "another-family" }},
	}

	for _, tc := range mutations {
		t.Run("changed "+tc.name+" conflicts", func(t *testing.T) {
			registry := NewRegistry(systemsWithPangolin(t))
			declaration := systemOnlyAuthorityDeclaration()
			if err := registry.DeclareRuntime(declaration); err != nil {
				t.Fatalf("first DeclareRuntime: %v", err)
			}

			conflicting := declaration.clone()
			tc.mutate(&conflicting)
			err := registry.DeclareRuntime(conflicting)
			requireErrorIs(t, err, ErrRuntimeConflict, "DeclareRuntime(system-only authority conflict)")

			stored, ok := registry.RuntimeDeclarationOf(declaration.ID)
			if !ok {
				t.Fatal("the winning declaration disappeared")
			}
			if !systemOnlyModelAuthorityEqual(stored.Models, declaration.Models) {
				t.Fatalf("the losing declaration mutated stored authority: %#v", stored.Models)
			}
		})
	}
}

func TestConcurrentSystemOnlyRuntimeConflictsKeepExactlyOneCompleteAuthority(t *testing.T) {
	const contenders = 24
	registry := NewRegistry(systemsWithPangolin(t))
	start := make(chan struct{})
	type result struct {
		index int
		err   error
	}
	results := make(chan result, contenders)
	var wg sync.WaitGroup

	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(index int) {
			declaration := systemOnlyAuthorityDeclaration()
			word := fmt.Sprintf("effort-%02d", index)
			declaration.Models[0].Effort.Vocabulary = []string{word}
			declaration.Models[0].Effort.Recommended = word
			<-start
			results <- result{index: index, err: registry.DeclareRuntime(declaration)}
			wg.Done()
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	winner := -1
	for result := range results {
		if result.err == nil {
			if winner >= 0 {
				t.Fatalf("more than one conflicting contender reported success: %d and %d", winner, result.index)
			}
			winner = result.index
			continue
		}
		requireErrorIs(t, result.err, ErrRuntimeConflict, "concurrent losing DeclareRuntime")
	}
	if winner < 0 {
		t.Fatal("no concurrent contender established authority")
	}

	winnerWord := fmt.Sprintf("effort-%02d", winner)
	stored, ok := registry.RuntimeDeclarationOf("system-authority")
	if !ok {
		t.Fatal("the winning declaration was not stored")
	}
	if got := stored.Models[0].Effort; len(got.Vocabulary) != 1 || got.Vocabulary[0] != winnerWord || got.Recommended != winnerWord {
		t.Fatalf("stored effort authority = %#v, want only winner %q", got, winnerWord)
	}

	request := narwhalRequest()
	request.Runtime = "system-authority"
	request.Model = "authority-one"
	for i := 0; i < contenders; i++ {
		word := fmt.Sprintf("effort-%02d", i)
		request.Effort = word
		_, err := BuildLaunch(context.Background(), registry, request, agentic.LaunchModeDryRun)
		if i == winner {
			if err != nil {
				t.Fatalf("BuildLaunch(winner effort %q): %v", word, err)
			}
			continue
		}
		requireErrorIs(t, err, ErrEffortNotInVocabulary, "BuildLaunch(losing effort authority)")
	}
}

func systemOnlyAuthorityDeclaration() RuntimeDeclaration {
	model := func(id ModelID, score int) Model {
		return Model{
			ID:                  id,
			Description:         UsageDescription("system-only authority row " + id),
			Rank:                CapabilityRank{Score: score, Basis: []RankEvidence{{Source: "test", Observation: "authority fixture"}}},
			Lifecycle:           LifecycleCurrent,
			Effort:              EffortDeclaration{Support: agentic.EffortSupportRequired, Vocabulary: []string{"low", "high"}, Recommended: "high"},
			ContextWindowTokens: 32_768,
			Publisher:           "fixture-publisher",
			Family:              "fixture-family",
			Systems:             []agentic.SystemID{pangolinID},
		}
	}
	return RuntimeDeclaration{
		ID:     "system-authority",
		System: pangolinID,
		Vendor: VendorUnresolved,
		Broker: BrokerProvenance{Checked: []string{"test fixture"}},
		Models: []Model{model("authority-one", 20), model("authority-two", 10)},
	}
}

func systemOnlyAuthorityPricing(model ModelID) *Pricing {
	promotional := 1.0
	return &Pricing{
		BillingModel: "subscription",
		Edition:      "fixture",
		Currency:     "USD",
		QuotaPeriod:  "month",
		Plans: []PricingPlan{{
			Name:                  "fixture",
			MonthlyUSD:            2,
			PromotionalMonthlyUSD: &promotional,
			MonthlyCredits:        1,
			ApplicableModelIDs:    []ModelID{model},
		}},
		SourceURL: "https://example.invalid/pricing",
		AsOf:      "2026-08-29",
	}
}

// A binding nobody looked for is not a declaration. Requiring the provenance
// is what makes "unresolved" a finding rather than an empty field somebody
// forgot to fill.
func TestRuntimeDeclarationValidation(t *testing.T) {
	base := tuskDeclaration()

	cases := map[string]func(d *RuntimeDeclaration){
		"a blank id":                     func(d *RuntimeDeclaration) { d.ID = "  " },
		"an unnormalized id":             func(d *RuntimeDeclaration) { d.ID = "Tusk" },
		"a blank system":                 func(d *RuntimeDeclaration) { d.System = "" },
		"an unnormalized system":         func(d *RuntimeDeclaration) { d.System = "Pangolin" },
		"an unnormalized vendor":         func(d *RuntimeDeclaration) { d.Vendor = "Narwhal" },
		"no provenance at all":           func(d *RuntimeDeclaration) { d.Broker = BrokerProvenance{} },
		"a blank checked source":         func(d *RuntimeDeclaration) { d.Broker.Checked = []string{" "} },
		"a named vendor with no finding": func(d *RuntimeDeclaration) { d.Broker.Found = "" },
		"an unresolved vendor that claims a finding": func(d *RuntimeDeclaration) {
			d.Vendor = VendorUnresolved
			d.Broker.Found = "we are fairly sure it is one of the big three"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			declaration := base
			declaration.Broker.Checked = append([]string(nil), base.Broker.Checked...)
			mutate(&declaration)

			registry := NewRegistry(systemsWithPangolin(t))
			if err := registry.DeclareRuntime(declaration); err == nil {
				t.Fatalf("DeclareRuntime admitted a declaration with %s", name)
			}
			if len(registry.RuntimeDeclarations()) != 0 {
				t.Fatalf("a declaration with %s reached the registry", name)
			}
		})
	}
}

// The frozen table and the declarations handed out of the registry are copies
// all the way down. A caller that edits the answer must not be able to rebind
// a runtime through it — the provenance slice is exactly the shared backing
// array that makes that possible without anyone noticing.
func TestFrozenRuntimesAndDeclarationsAreCopies(t *testing.T) {
	first := FrozenRuntimes()
	first[0].Vendor = "mutated"
	first[0].Broker.Checked[0] = "mutated"

	second := FrozenRuntimes()
	if second[0].Vendor == "mutated" || second[0].Broker.Checked[0] == "mutated" {
		t.Fatal("FrozenRuntimes() hands out the frozen table itself")
	}

	registry := registerNarwhal(t, newNarwhal())
	declaration, _ := registry.RuntimeDeclarationOf(tuskID)
	declaration.Vendor = "mutated"
	declaration.Broker.Checked[0] = "mutated"

	again, _ := registry.RuntimeDeclarationOf(tuskID)
	if again.Vendor == "mutated" || again.Broker.Checked[0] == "mutated" {
		t.Fatal("RuntimeDeclarationOf hands out the registry's own declaration")
	}
}
