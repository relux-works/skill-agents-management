package vendorplugin_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The muse HARNESS, imported here and nowhere in the vendor layer.
	//
	// No vendor drives it — the source records its broker as checked and never
	// established — so without this import the muse runtime would fail to
	// resolve for the uninteresting reason that its system plugin is not
	// compiled in, and the refusal this file cares about (the broker was never
	// established) would never be reached. Importing it makes the assertion
	// about the vendor layer instead of about the test binary's import list.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
)

// The release gate: admitted-pair digests, reproduced from the ported model
// rows and held against what the SOURCE BINARY printed for the SOURCE
// repository's own configuration.
//
// Why the digest is the gate rather than a nice-to-have. It hashes a canonical
// serialization of every admitted (model, effort) pair, so it is sensitive to
// exactly the thing that is hardest to eyeball across 41 hand-carried rows: one
// effort word gained or lost on one model. TestTheDigestFiresOnADriftedVocabulary
// narrows the gate to prove that sensitivity rather than asserting it.
//
// What the digest is NOT sensitive to is equally load-bearing:
// TestCapabilityRanksAreNotTheAdmissionAuthority reverses a whole vendor's
// lineup and requires the digest to hold. A rank is capability evidence; the
// day it starts deciding admission, a truthful re-rank silently moves who may
// spawn, which is the defect the extraction source spent a task removing.

// TestAdmittedPairDigestsReproduceTheSourceBinary is the pin itself.
func TestAdmittedPairDigestsReproduceTheSourceBinary(t *testing.T) {
	fixture := loadSourceAdmission(t)

	if fixture.SnapshotVersion != vendorplugin.V2SnapshotVersion {
		t.Fatalf("the source binary expanded through snapshot %q and this port carries %q; the two are different frozen tables and their digests are not comparable",
			fixture.SnapshotVersion, vendorplugin.V2SnapshotVersion)
	}

	runtimes := make([]string, 0, len(fixture.Ceilings))
	for runtime := range fixture.Ceilings {
		runtimes = append(runtimes, runtime)
	}
	sort.Strings(runtimes)

	for _, runtime := range runtimes {
		pinned := fixture.Ceilings[runtime]
		t.Run(runtime, func(t *testing.T) {
			set, err := vendorplugin.ExpandV2Ceiling(vendorplugin.Default, vendorplugin.V2Ceiling{
				Runtime:        vendorplugin.RuntimeID(runtime),
				Model:          vendorplugin.ModelID(pinned.Model),
				ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
				Effort:         pinned.Effort,
			})
			if err != nil {
				t.Fatalf("expanding the source repository's own %s ceiling: %v", runtime, err)
			}

			// The pairs are compared before the digest so a failure says WHICH
			// model drifted. A digest mismatch alone is a true statement that
			// tells nobody where to look.
			wantPairs := make([]string, 0)
			for _, model := range pinned.AdmittedPairs.Models {
				for _, effort := range model.Efforts {
					wantPairs = append(wantPairs, vendorplugin.AdmittedPair{Model: model.ID, Effort: effort}.String())
				}
			}
			sort.Strings(wantPairs)
			if got := set.PinnedPairs(); !reflect.DeepEqual(got, wantPairs) {
				t.Errorf("admitted pairs for %s differ from the source binary's:\n got %v\nwant %v", runtime, got, wantPairs)
			}
			if set.Provider != pinned.AdmittedPairs.Provider {
				t.Errorf("the set is labelled provider %q and the source binary labelled it %q; the label is hashed, so this alone changes the digest",
					set.Provider, pinned.AdmittedPairs.Provider)
			}
			if got := set.Digest(); got != pinned.AdmittedPairs.Digest {
				t.Errorf("digest for %s is %s and the source binary computed %s over %s (%s)\ncanonical serialization was:\n%q",
					runtime, got, pinned.AdmittedPairs.Digest, fixture.Provenance.ConfigPath, fixture.Provenance.ConfigSHA256, set.CanonicalSerialization())
			}
		})
	}
}

// TestTheDigestFiresOnADriftedVocabulary narrows the gate.
//
// Each mutant changes the ported model rows by ONE fact — the smallest change a
// hand-carried port could plausibly get wrong — and requires the digest to
// move. A digest that held through any of these would be pinning something
// other than the port.
func TestTheDigestFiresOnADriftedVocabulary(t *testing.T) {
	fixture := loadSourceAdmission(t)
	pinned, ok := fixture.Ceilings["codex"]
	if !ok {
		t.Fatal("the captured fixture pins no codex ceiling; this mutant set has nothing to drift")
	}
	ceiling := vendorplugin.V2Ceiling{
		Runtime:        "codex",
		Model:          vendorplugin.ModelID(pinned.Model),
		ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
		Effort:         pinned.Effort,
	}

	tests := []struct {
		name   string
		mutate func(vendorplugin.Model) vendorplugin.Model
	}{
		{
			name: "one effort word dropped from one model",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID != "gpt-5.3-codex" {
					return m
				}
				m.Effort.Vocabulary = dropWord(m.Effort.Vocabulary, "minimal")
				m.Effort.Recommended = "xhigh"
				return m
			},
		},
		{
			name: "one effort word gained by one model",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID != "gpt-5.6-sol" {
					return m
				}
				m.Effort.Vocabulary = append([]string{"minimal"}, m.Effort.Vocabulary...)
				return m
			},
		},
		{
			name: "one model losing its effort axis entirely",
			mutate: func(m vendorplugin.Model) vendorplugin.Model {
				if m.ID != "gpt-5.4-mini" {
					return m
				}
				m.Effort = vendorplugin.EffortDeclaration{Support: agentic.EffortSupportNone}
				return m
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := isolatedRegistry(t, map[vendorplugin.VendorID]func(vendorplugin.Model) vendorplugin.Model{
				"openai": tt.mutate,
			})
			set, err := vendorplugin.ExpandV2Ceiling(registry, ceiling)
			if err != nil {
				t.Fatalf("expanding the drifted registry: %v", err)
			}
			if got := set.Digest(); got == pinned.AdmittedPairs.Digest {
				t.Fatalf("the drifted rows still hash to the pinned digest %s; the digest does not cover this fact, so its stability on the real rows proves nothing about them\ncanonical serialization was:\n%q",
					got, set.CanonicalSerialization())
			}
		})
	}
}

// TestCapabilityRanksAreNotTheAdmissionAuthority is the inverse gate, and it is
// the reason this port carries a frozen tier table at all.
//
// It REVERSES a vendor's entire lineup — position 1 becomes position n — while
// leaving every id, vocabulary and system binding untouched, and requires the
// admitted set and its digest to be bit-identical. If a rank ever became the
// membership authority, this test fails, and it fails loudly enough to stop the
// exact regression the extraction source recorded: "adding a new model below an
// existing ceiling silently widened it".
func TestCapabilityRanksAreNotTheAdmissionAuthority(t *testing.T) {
	fixture := loadSourceAdmission(t)
	for runtime, pinned := range fixture.Ceilings {
		vendor := vendorOfRuntime(t, vendorplugin.RuntimeID(runtime))
		// The reversal is over the SCORES, and it is arithmetic on the real
		// ones rather than a fixed constant so that every row genuinely moves:
		// (max + min) - score maps the top of the lineup onto the bottom and
		// back, and it preserves the ties, which is what keeps this a pure
		// re-rank rather than a re-rank plus a tie break.
		scores := mustModels(t, vendorplugin.Default, vendor)
		low, high := scores[0].Rank.Score, scores[0].Rank.Score
		for _, model := range scores {
			if model.Rank.Score < low {
				low = model.Rank.Score
			}
			if model.Rank.Score > high {
				high = model.Rank.Score
			}
		}
		registry := isolatedRegistry(t, map[vendorplugin.VendorID]func(vendorplugin.Model) vendorplugin.Model{
			vendor: func(m vendorplugin.Model) vendorplugin.Model {
				m.Rank.Score = high + low - m.Rank.Score
				return m
			},
		})
		set, err := vendorplugin.ExpandV2Ceiling(registry, vendorplugin.V2Ceiling{
			Runtime:        vendorplugin.RuntimeID(runtime),
			Model:          vendorplugin.ModelID(pinned.Model),
			ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
			Effort:         pinned.Effort,
		})
		if err != nil {
			t.Fatalf("expanding %s with vendor %s re-ranked: %v", runtime, vendor, err)
		}
		if got := set.Digest(); got != pinned.AdmittedPairs.Digest {
			t.Errorf("reversing vendor %s's lineup moved runtime %s's admitted set: digest %s, pinned %s. A capability rank is evidence, and the day it decides admission a truthful re-rank changes who may spawn",
				vendor, runtime, got, pinned.AdmittedPairs.Digest)
		}
	}
}

// TestExpandRefusesRatherThanNarrowing covers the four ways an expansion can
// fail to answer, each of which must REFUSE rather than return a set.
//
// The shape is the point. An unanswerable ceiling that returned a smaller set
// looks exactly like a legitimate narrowing to everything downstream, and an
// unanswerable one that returned an empty set is one `len(...) == 0` away from
// reading as "no restriction". Both are how a gate quietly stops gating.
func TestExpandRefusesRatherThanNarrowing(t *testing.T) {
	tests := []struct {
		name    string
		ceiling vendorplugin.V2Ceiling
		wantErr error
	}{
		{
			name:    "a runtime the frozen snapshot never captured",
			ceiling: vendorplugin.V2Ceiling{Runtime: "gemini", Model: "gemini-3.5-flash", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual},
			wantErr: vendorplugin.ErrV2Unexpandable,
		},
		{
			name:    "a bound naming a model registered after the freeze",
			ceiling: vendorplugin.V2Ceiling{Runtime: "codex", Model: "gpt-9-future", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual},
			wantErr: vendorplugin.ErrV2Unexpandable,
		},
		{
			name:    "an effort bound the admission scale cannot place",
			ceiling: vendorplugin.V2Ceiling{Runtime: "codex", Model: "gpt-5.6-sol", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual, Effort: "brisk"},
			wantErr: vendorplugin.ErrV2Unexpandable,
		},
		{
			name:    "an equal criterion with no model to be equal to",
			ceiling: vendorplugin.V2Ceiling{Runtime: "codex", ModelCriterion: vendorplugin.ModelCriterionEqual},
			wantErr: vendorplugin.ErrV2Unexpandable,
		},
		{
			name:    "a runtime nobody declared at all",
			ceiling: vendorplugin.V2Ceiling{Runtime: "opencode", Model: "gpt-5.6-sol", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual},
			wantErr: vendorplugin.ErrUnknownRuntime,
		},
		{
			name:    "the runtime whose vendor was never established",
			ceiling: vendorplugin.V2Ceiling{Runtime: "muse", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual},
			wantErr: vendorplugin.ErrRuntimeVendorUnresolved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := vendorplugin.ExpandV2Ceiling(vendorplugin.Default, tt.ceiling)
			if err == nil {
				t.Fatalf("the expansion answered with %v instead of refusing; an unanswerable ceiling that returns a set is indistinguishable from a narrower one", formatPairs(set))
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("refused with %v, which is not %v; the four ways an expansion can fail are four different facts with four different fixes", err, tt.wantErr)
			}
			if !set.IsEmpty() {
				t.Fatalf("the refusal also returned pairs %v; a caller that ignored the error would act on them", set.PinnedPairs())
			}
		})
	}
}

// TestFrozenTiersMatchTheCapturedSnapshot pins the membership authority itself.
//
// The digests above would also reproduce if BOTH the tiers and the model rows
// drifted in compensating ways, which is unlikely but not impossible when a
// person carries two tables by hand. This compares the ported tiers directly
// against the ones captured from the source, tier for tier and in order.
func TestFrozenTiersMatchTheCapturedSnapshot(t *testing.T) {
	fixture := loadSourceRegistry(t)
	if fixture.V2Snapshot.RolloutVersion != vendorplugin.V2SnapshotVersion {
		t.Fatalf("the captured snapshot is %q and this port carries %q", fixture.V2Snapshot.RolloutVersion, vendorplugin.V2SnapshotVersion)
	}

	ported := map[string][][]string{}
	for _, runtime := range vendorplugin.V2SnapshotRuntimes() {
		tiers, ok := vendorplugin.V2SnapshotTiers(runtime)
		if !ok {
			t.Fatalf("V2SnapshotRuntimes lists %s and V2SnapshotTiers cannot answer for it", runtime)
		}
		ported[string(runtime)] = tiers
	}
	if !reflect.DeepEqual(ported, fixture.V2Snapshot.Tiers) {
		t.Fatalf("the ported v2 tiers differ from the captured ones:\n got %v\nwant %v", ported, fixture.V2Snapshot.Tiers)
	}

	// The runtimes the snapshot does NOT hold are asserted too. Their absence
	// is a finding — those runtimes had no v2 ceiling to grandfather — and a
	// port that quietly invented tiers for them would widen admission for a
	// provider the source refuses to answer for.
	for _, runtime := range []vendorplugin.RuntimeID{"gemini", "agy", "muse", "qwen-codex"} {
		if _, held := vendorplugin.V2SnapshotTiers(runtime); held {
			t.Errorf("the frozen snapshot answers for runtime %s, which the source's own capture never held", runtime)
		}
	}
}

// TestAliasTierAdmitsBothDirections holds the one place the port's derived
// ranks and the frozen snapshot genuinely disagree, and shows the snapshot
// winning.
//
// claude-haiku-4-5 and its dated snapshot score equally in the source, so the
// frozen table puts them in ONE tier and each admits the other under both
// directions. This port had to give them distinct capability positions — a
// lineup that cannot order itself is not a ranking — and if membership came
// from those positions, a ceiling bound at the dated id would admit one row
// where the source admits two. It does not, and this is where that is visible.
func TestAliasTierAdmitsBothDirections(t *testing.T) {
	for _, bound := range []vendorplugin.ModelID{"claude-haiku-4-5", "claude-haiku-4-5-20251001"} {
		set, err := vendorplugin.ExpandV2Ceiling(vendorplugin.Default, vendorplugin.V2Ceiling{
			Runtime:        "claude",
			Model:          bound,
			ModelCriterion: vendorplugin.ModelCriterionLessOrEqual,
		})
		if err != nil {
			t.Fatalf("expanding a claude ceiling bound at %s: %v", bound, err)
		}
		want := []string{"claude-haiku-4-5", "claude-haiku-4-5-20251001"}
		got := set.ModelIDs()
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("a ceiling bound at %s admits %v; the frozen tier holds both ids and admits %v", bound, got, want)
		}
	}
}

// TestEffortsUnderBoundRefusesToWidenOnAnUnplaceableBound covers the helper's
// own edges at the value level, including the two that decide whether a model
// appears in a digest at all.
func TestEffortsUnderBoundRefusesToWidenOnAnUnplaceableBound(t *testing.T) {
	required := vendorplugin.Model{
		ID:     "pangolin-thinker",
		Effort: vendorplugin.EffortDeclaration{Support: agentic.EffortSupportRequired, Vocabulary: []string{"max", "low", "high"}, Recommended: "high"},
	}
	effortless := vendorplugin.Model{ID: "pangolin-flat"}

	tests := []struct {
		name      string
		model     vendorplugin.Model
		bound     string
		criterion vendorplugin.EffortCriterion
		want      []string
	}{
		{"an empty bound admits the whole vocabulary, sorted", required, "", vendorplugin.EffortCriterionLessOrEqual, []string{"low", "high", "max"}},
		{"a ceiling admits at and below", required, "high", vendorplugin.EffortCriterionLessOrEqual, []string{"low", "high"}},
		{"a floor admits at and above", required, "high", vendorplugin.EffortCriterionGreaterOrEqual, []string{"high", "max"}},
		{"an unplaceable bound admits nothing rather than everything", required, "brisk", vendorplugin.EffortCriterionLessOrEqual, nil},
		{"a model with no effort axis admits exactly the empty effort", effortless, "high", vendorplugin.EffortCriterionLessOrEqual, []string{""}},
		{"a model with no effort axis is unaffected by an unplaceable bound", effortless, "brisk", vendorplugin.EffortCriterionLessOrEqual, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vendorplugin.EffortsUnderBound(tt.model, tt.bound, tt.criterion)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSortEffortsKeepsAWordTheScaleCannotPlace guards the quiet-shrink case: a
// vendor vocabulary word this module's scale has never heard of must survive
// into the digest rather than being dropped on the way.
func TestSortEffortsKeepsAWordTheScaleCannotPlace(t *testing.T) {
	got := vendorplugin.SortEfforts([]string{"zeta", "high", "alpha", "low"})
	want := []string{"low", "high", "alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v; an unplaced word dropped here is an admitted pair silently removed from a digest", got, want)
	}
}

// TestPinnedPairsRoundTrip holds the persisted form readable.
func TestPinnedPairsRoundTrip(t *testing.T) {
	set, err := vendorplugin.ExpandV2Ceiling(vendorplugin.Default, vendorplugin.V2Ceiling{
		Runtime: "claude", Model: "claude-opus-5", ModelCriterion: vendorplugin.ModelCriterionLessOrEqual, Effort: "high",
	})
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	for _, rendered := range set.PinnedPairs() {
		pair, err := vendorplugin.ParseAdmittedPair(rendered)
		if err != nil {
			t.Fatalf("parsing %q back: %v", rendered, err)
		}
		if pair.String() != rendered {
			t.Fatalf("%q round-tripped to %q", rendered, pair.String())
		}
		if !set.Contains(pair.Model, pair.Effort) {
			t.Fatalf("the set rendered %q and does not contain it", rendered)
		}
	}
}

// vendorOfRuntime answers which vendor serves a runtime, through the registry.
func vendorOfRuntime(t *testing.T, id vendorplugin.RuntimeID) vendorplugin.VendorID {
	t.Helper()
	declaration, declared := vendorplugin.Default.RuntimeDeclarationOf(id)
	if !declared {
		t.Fatalf("runtime %s is not declared", id)
	}
	return declaration.Vendor
}

func mustModels(t *testing.T, r *vendorplugin.Registry, id vendorplugin.VendorID) []vendorplugin.Model {
	t.Helper()
	plugin, ok := r.Lookup(id)
	if !ok {
		t.Fatalf("vendor %s is not registered", id)
	}
	return plugin.Models()
}

// writeStubBinary is the module's usual launch-test stub: a runnable file with
// the right name, so binary resolution lands on a layout this test built.
func writeStubBinary(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stub %s: %v", name, err)
	}
	return path
}

func dropWord(words []string, drop string) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		if word != drop {
			out = append(out, word)
		}
	}
	return out
}

// mutantVendor re-declares a real vendor plugin's models through a transform.
//
// It wraps the REGISTERED plugin rather than reimplementing one, so a mutant
// differs from production in exactly the fact under test and in nothing else. A
// hand-written fake here would drift from the real rows and the mutants would
// then be proving something about the fake.
type mutantVendor struct {
	inner  vendorplugin.Vendor
	mutate func(vendorplugin.Model) vendorplugin.Model
}

func (m mutantVendor) ID() vendorplugin.VendorID { return m.inner.ID() }

func (m mutantVendor) Models() []vendorplugin.Model {
	rows := m.inner.Models()
	if m.mutate == nil {
		return rows
	}
	out := make([]vendorplugin.Model, 0, len(rows))
	for _, row := range rows {
		out = append(out, m.mutate(row))
	}
	return out
}

func (m mutantVendor) Availability(q vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return m.inner.Availability(q)
}

func (m mutantVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return m.inner.Spawn(sc)
}

// isolatedRegistry builds a registry holding the four ported vendors, with the
// named ones passed through a transform. It never touches the default registry,
// so a mutant cannot leak into another test.
func isolatedRegistry(t *testing.T, mutations map[vendorplugin.VendorID]func(vendorplugin.Model) vendorplugin.Model) *vendorplugin.Registry {
	t.Helper()
	registry := vendorplugin.NewRegistry(agentic.Default)
	if err := vendorplugin.SeedFrozenRuntimes(registry); err != nil {
		t.Fatalf("seeding the frozen runtimes: %v", err)
	}
	ids := make([]vendorplugin.VendorID, 0, len(portedVendors))
	for vendor := range portedVendors {
		ids = append(ids, vendor)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, vendor := range ids {
		plugin, ok := vendorplugin.Default.Lookup(vendor)
		if !ok {
			t.Fatalf("vendor %s is not registered in the default registry", vendor)
		}
		if err := registry.Register(mutantVendor{inner: plugin, mutate: mutations[vendor]}); err != nil {
			t.Fatalf("registering vendor %s into the isolated registry: %v", vendor, err)
		}
	}
	return registry
}

// TestIsolatedRegistryMirrorsTheDefault keeps the mutant harness honest: with
// no mutation applied, the isolated registry must produce the same digests as
// the default one. Without this, a mutant "firing" could be the harness
// differing from production rather than the drift under test.
func TestIsolatedRegistryMirrorsTheDefault(t *testing.T) {
	fixture := loadSourceAdmission(t)
	registry := isolatedRegistry(t, nil)
	for runtime, pinned := range fixture.Ceilings {
		ceiling := vendorplugin.V2Ceiling{
			Runtime:        vendorplugin.RuntimeID(runtime),
			Model:          vendorplugin.ModelID(pinned.Model),
			ModelCriterion: vendorplugin.ModelCriterion(pinned.ModelCriterion),
			Effort:         pinned.Effort,
		}
		set, err := vendorplugin.ExpandV2Ceiling(registry, ceiling)
		if err != nil {
			t.Fatalf("expanding %s through the unmutated isolated registry: %v", runtime, err)
		}
		if got := set.Digest(); got != pinned.AdmittedPairs.Digest {
			t.Fatalf("the unmutated isolated registry hashes %s to %s and the source binary computed %s; the mutant harness differs from production, so nothing it reports is about the port",
				runtime, got, pinned.AdmittedPairs.Digest)
		}
	}
}

// TestDeclaredCrossRuntimeResolvesAndLaunches drives the PRODUCTION entry
// point for the one row that is the architecture's whole argument.
//
// qwen-codex is not a frozen runtime and must not become one: it is the
// source's own example of an OPERATOR-declared cross-runtime, and it reaches
// this module through the same public DeclareRuntime a config would call.
// Adding an Alibaba model under OpenAI's harness is therefore one declaration
// and one row in one vendor's binding file, with no core change — which is only
// a real claim if a launch actually comes out the other end, so this builds one
// through vendorplugin.BuildLaunch rather than inspecting the declaration.
func TestDeclaredCrossRuntimeResolvesAndLaunches(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	if err := registry.DeclareRuntime(qwenCodexDeclaration()); err != nil {
		t.Fatalf("declaring the cross-runtime: %v", err)
	}

	// A real launch resolves a real binary, so the harness gets a stub on a
	// PATH this test built rather than whatever the developer has installed.
	binDir := t.TempDir()
	workDir := t.TempDir()
	writeStubBinary(t, binDir, "codex")
	promptPath := filepath.Join(workDir, "assignment.md")
	if err := os.WriteFile(promptPath, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatalf("writing the assignment prompt: %v", err)
	}

	plan, err := vendorplugin.BuildLaunch(registry, vendorplugin.SpawnRequest{
		Runtime:    "qwen-codex",
		Model:      "qwen3.7-plus-via-codex",
		Effort:     "ultra",
		PromptPath: promptPath,
		WorkDir:    workDir,
		Env:        []string{"PATH=" + binDir},
	}, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("building a launch through the declared cross-runtime: %v", err)
	}
	if plan.System != "codex" {
		t.Errorf("the plan runs agentic system %q; the declaration binds qwen-codex to the codex harness", plan.System)
	}
	if !strings.Contains(strings.Join(plan.Argv, " "), "qwen3.7-plus-via-codex") {
		t.Errorf("the model never reached argv: %v", plan.Argv)
	}

	// `ultra` is the proof that the vocabulary followed the HARNESS and not the
	// vendor's other rows: qwen-code's own models stop at max, and this row
	// accepts ultra because it launches through the codex adapter's flag.
	if !strings.Contains(strings.Join(plan.Argv, " "), "ultra") {
		t.Errorf("the effort never reached argv: %v", plan.Argv)
	}
	for _, model := range mustModels(t, registry, "alibaba") {
		if model.ID != "qwen3.7-plus" {
			continue
		}
		if model.Effort.Accepts("ultra") {
			t.Errorf("the qwen-code row accepts %q too, so the cross-runtime row's vocabulary proves nothing about which layer it came from", "ultra")
		}
	}
}

// TestEveryFrozenRuntimeWithAVendorResolvesThroughTheDefaultRegistry drives the
// other production path: importing the four vendor packages must make every
// frozen runtime that HAS a vendor launchable, and must leave the one whose
// vendor was never established refusing with its own error.
func TestEveryFrozenRuntimeWithAVendorResolvesThroughTheDefaultRegistry(t *testing.T) {
	for _, declaration := range vendorplugin.FrozenRuntimes() {
		runtime, err := vendorplugin.Default.ResolveRuntime(declaration.ID)
		if !declaration.VendorResolved() {
			if !errors.Is(err, vendorplugin.ErrRuntimeVendorUnresolved) {
				t.Errorf("runtime %s has no established vendor and resolved with %v; an unresolved broker must stay a stated finding rather than becoming a missing plugin", declaration.ID, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("runtime %s did not resolve: %v", declaration.ID, err)
			continue
		}
		if runtime.VendorID != declaration.Vendor || runtime.Vendor == nil {
			t.Errorf("runtime %s resolved to vendor %q (%v)", declaration.ID, runtime.VendorID, runtime.Vendor)
		}
		if len(vendorplugin.RuntimeModels(runtime)) == 0 {
			t.Errorf("runtime %s resolves and its vendor declares no model that names the %s harness; the runtime is declared and unlaunchable", declaration.ID, runtime.SystemID)
		}
	}
}

// TestRuntimeModelsScopeToTheHarnessThatDrivesThem is the gate that keeps two
// runtimes over ONE vendor apart.
//
// alibaba and google each own rows reachable through two different harnesses.
// Indexing a vendor's whole list per runtime would give the qwen runtime a row
// only the codex harness can drive, and the gemini runtime the antigravity
// ones — admitted pairs no launch could ever make, in a set that still looks
// perfectly well-formed. The counts are stated here rather than derived so a
// leak has a number to fail against.
func TestRuntimeModelsScopeToTheHarnessThatDrivesThem(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	if err := registry.DeclareRuntime(qwenCodexDeclaration()); err != nil {
		t.Fatalf("declaring the cross-runtime: %v", err)
	}

	tests := []struct {
		runtime vendorplugin.RuntimeID
		system  agentic.SystemID
		count   int
		holds   vendorplugin.ModelID
		excedes vendorplugin.ModelID
	}{
		{runtime: "qwen", system: "qwen-code", count: 5, holds: "qwen3.7-plus", excedes: "qwen3.7-plus-via-codex"},
		{runtime: "qwen-codex", system: "codex", count: 1, holds: "qwen3.7-plus-via-codex", excedes: "qwen3.7-plus"},
		{runtime: "gemini", system: "gemini-cli", count: 7, holds: "gemini-2.5-pro", excedes: "gemini-3.6-flash-high"},
		{runtime: "agy", system: "antigravity", count: 8, holds: "gemini-3.6-flash-high", excedes: "gemini-2.5-pro"},
		{runtime: "codex", system: "codex", count: 12, holds: "gpt-5.6-sol", excedes: "qwen3.7-plus-via-codex"},
		{runtime: "claude", system: "claude-code", count: 8, holds: "claude-opus-5", excedes: "gpt-5.6-sol"},
	}
	for _, tt := range tests {
		t.Run(string(tt.runtime), func(t *testing.T) {
			resolved, err := registry.ResolveRuntime(tt.runtime)
			if err != nil {
				t.Fatalf("resolving %s: %v", tt.runtime, err)
			}
			if resolved.SystemID != tt.system {
				t.Fatalf("runtime %s runs on %q, not %q", tt.runtime, resolved.SystemID, tt.system)
			}
			models := vendorplugin.RuntimeModels(resolved)
			if len(models) != tt.count {
				ids := make([]string, 0, len(models))
				for id := range models {
					ids = append(ids, string(id))
				}
				sort.Strings(ids)
				t.Fatalf("runtime %s indexes %d models and its harness drives %d: %v", tt.runtime, len(models), tt.count, ids)
			}
			if _, held := models[tt.holds]; !held {
				t.Errorf("runtime %s does not index %q, which declares its harness", tt.runtime, tt.holds)
			}
			if _, leaked := models[tt.excedes]; leaked {
				t.Errorf("runtime %s indexes %q, which does not declare the %s harness; an admitted pair no launch could make is still an admitted pair",
					tt.runtime, tt.excedes, tt.system)
			}
		})
	}
}

// TestModelsAreCopiedOutOfEveryVendor holds the contract's "must not vary
// between calls" against the way it would actually break.
//
// Model carries three slices, and a shallow copy would let any caller ranging
// over Models() rebind a model for the whole process — `rows[0].Systems[0] =
// "muse"` would move a production row through a read-only-looking call. This
// writes through every slice the type has and requires the next read to be
// unchanged.
func TestModelsAreCopiedOutOfEveryVendor(t *testing.T) {
	for vendor := range portedVendors {
		plugin, ok := vendorplugin.Default.Lookup(vendor)
		if !ok {
			t.Fatalf("vendor %s is not registered", vendor)
		}
		before := plugin.Models()
		scribble := plugin.Models()
		if len(scribble) == 0 {
			t.Fatalf("vendor %s declares no model", vendor)
		}
		for i := range scribble {
			scribble[i].ID = "scribbled"
			scribble[i].Description = "scribbled"
			for j := range scribble[i].Systems {
				scribble[i].Systems[j] = "muse"
			}
			for j := range scribble[i].Effort.Vocabulary {
				scribble[i].Effort.Vocabulary[j] = "scribbled"
			}
			for j := range scribble[i].Rank.Basis {
				scribble[i].Rank.Basis[j].Observation = "scribbled"
			}
		}
		after := plugin.Models()
		if !reflect.DeepEqual(before, after) {
			t.Errorf("vendor %s answered Models() differently after a caller wrote through the previous answer; the list must not vary between reads", vendor)
		}
	}
}
