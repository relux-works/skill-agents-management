package vendorplugin_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"

	// The four vendor plugins under test, imported for their registration side
	// effect. This is the PRODUCTION path: a binary gets its vendors by
	// importing them, and every assertion below reads vendorplugin.Default
	// rather than a registry the test assembled, so what is pinned is what a
	// binary would actually launch through.
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/alibaba"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/anthropic"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/google"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)

// This file is the full-set pin: every row of the extraction source's model
// registry, held against what the four vendor plugins declare.
//
// The fixtures it reads are CAPTURED, not written here.
// .scripts/capture-model-registry.sh reads skill-project-management's own
// sources for the registry and runs that repository's OWN BINARY for the
// admitted-pair digests, recording the source commit and a sha256 for every
// file it read. That distinction is the whole value of the digest half: a
// digest recomputed beside the port and pinned by the port would prove only
// that the port agrees with itself.

const (
	sourceRegistryFixture = "testdata/source-model-registry.json"
	admittedPairsFixture  = "testdata/source-admitted-pairs.json"
)

// sourceModel is one knownModels row as the fixture carries it.
type sourceModel struct {
	ID                string   `json:"id"`
	Runtime           string   `json:"runtime"`
	Broker            string   `json:"broker"`
	AgenticSystems    []string `json:"agentic_systems"`
	PolicyRank        int      `json:"policy_rank"`
	Lifecycle         string   `json:"lifecycle"`
	SupersededBy      string   `json:"superseded_by"`
	Recommended       bool     `json:"recommended"`
	Reasoning         string   `json:"reasoning"`
	SupportedEfforts  []string `json:"supported_efforts"`
	RecommendedEffort string   `json:"recommended_effort"`
}

type sourceRegistry struct {
	Provenance struct {
		SourceRepo   string `json:"source_repo"`
		SourceCommit string `json:"source_commit"`
	} `json:"provenance"`
	ModelCount int           `json:"model_count"`
	Models     []sourceModel `json:"models"`
	V2Snapshot struct {
		RolloutVersion string                `json:"rollout_version"`
		Tiers          map[string][][]string `json:"tiers"`
	} `json:"v2_admission_snapshot"`
}

type sourceAdmission struct {
	Provenance struct {
		SourceCommit string `json:"source_commit"`
		ConfigPath   string `json:"config_path"`
		ConfigSHA256 string `json:"config_sha256"`
	} `json:"provenance"`
	Authority       string `json:"authority"`
	SnapshotVersion string `json:"snapshot_version"`
	Ceilings        map[string]struct {
		Model          string `json:"model"`
		ModelCriterion string `json:"model_criterion"`
		Effort         string `json:"reasoning_effort"`
		AdmittedPairs  struct {
			Provider string `json:"provider"`
			Form     string `json:"form"`
			Digest   string `json:"digest"`
			Models   []struct {
				ID      string   `json:"id"`
				Efforts []string `json:"efforts"`
			} `json:"models"`
		} `json:"admitted_pairs"`
	} `json:"ceilings"`
}

// sourceModelCount is the row count stated independently of the fixture.
//
// It is a second, hand-written statement of the same number on purpose: a
// count read only from the fixture would agree with a fixture regenerated
// against a shrunken source, and the pin would then pass while silently
// describing a smaller table. Forty-five is the number the source table now
// states, and a regeneration that changes it has to be argued rather than
// absorbed. It moved from forty-three when the anthropic lineup gained
// claude-fable-5-1 and kept claude-fable-5 as a legacy row, and from
// forty-four when the muse runtime gained muse-spark-1.3-contributor and kept
// muse-spark-1.2-contributor as a legacy row.
const sourceModelCount = 45

// portedVendors are the vendor plugins this story ports, with the number of
// source rows each one owns. The counts are stated here for the same reason
// sourceModelCount is: a per-vendor total derived only from the fixture cannot
// notice a vendor's rows moving to another vendor.
var portedVendors = map[vendorplugin.VendorID]int{
	"anthropic": 9,
	"openai":    12,
	"alibaba":   6,
	"google":    15,
}

// unresolvedVendorRows are the source rows that belong to NO vendor plugin.
//
// muse's broker is recorded by the source as checked-and-unknown, and
// vendorplugin.VendorUnresolved is this module's carrying of that finding.
// Naming the three rows here rather than letting them fall off the end is the
// difference between "accounted for" and "dropped": 42 ported rows plus these
// three is the whole 45, and TestEverySourceRowIsAccountedFor adds it up.
var unresolvedVendorRows = []string{"muse-spark-1.3-contributor", "muse-spark", "muse-spark-1.2-contributor"}

func loadSourceRegistry(t *testing.T) sourceRegistry {
	t.Helper()
	var fixture sourceRegistry
	readFixture(t, sourceRegistryFixture, &fixture)
	if fixture.ModelCount != len(fixture.Models) {
		t.Fatalf("%s says it holds %d rows and carries %d; the capture is inconsistent with itself",
			sourceRegistryFixture, fixture.ModelCount, len(fixture.Models))
	}
	if fixture.Provenance.SourceCommit == "" {
		t.Fatalf("%s records no source commit; a fixture with no provenance is a claim about nothing", sourceRegistryFixture)
	}
	return fixture
}

func loadSourceAdmission(t *testing.T) sourceAdmission {
	t.Helper()
	var fixture sourceAdmission
	readFixture(t, admittedPairsFixture, &fixture)
	if len(fixture.Ceilings) == 0 {
		t.Fatalf("%s pins no ceiling; there is nothing to reproduce", admittedPairsFixture)
	}
	if fixture.Provenance.ConfigSHA256 == "" || fixture.Provenance.SourceCommit == "" {
		t.Fatalf("%s records no config hash or source commit; a digest with no provenance names no inputs", admittedPairsFixture)
	}
	return fixture
}

func readFixture(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("reading %s: %v; regenerate with .scripts/capture-model-registry.sh", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
}

// runtimeOf resolves the runtime a source row belongs to from the (agentic
// system, vendor) pair it declares, using the declarations the registry holds
// rather than a table of this test's own.
//
// The pair is unique across all seven runtimes the source registers, which is
// exactly why a runtime is declared as a pair in the first place: alibaba's
// rows split by harness into qwen and qwen-codex, google's into gemini and agy.
// A mapping written by hand here would be an eighth place the bindings live.
func runtimeOf(declarations []vendorplugin.RuntimeDeclaration, system agentic.SystemID, vendor vendorplugin.VendorID) (vendorplugin.RuntimeID, bool) {
	var found []vendorplugin.RuntimeID
	for _, declaration := range declarations {
		if declaration.System == system && declaration.Vendor == vendor {
			found = append(found, declaration.ID)
		}
	}
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}

// declaredRuntimes is every runtime a ported row can belong to: the six frozen
// ids, plus the cross-runtime the source's own repository config declares.
//
// qwen-codex is added here rather than seeded into the registry because it is
// not frozen and must not become so. The source keeps it deliberately
// non-builtin — it is that repository's worked example of an OPERATOR-declared
// runtime, and it reaches this module through the same public DeclareRuntime an
// operator's config would call. TestDeclaredCrossRuntimeResolvesAndLaunches
// drives that path for real.
func declaredRuntimes(t *testing.T) []vendorplugin.RuntimeDeclaration {
	t.Helper()
	return append(vendorplugin.FrozenRuntimes(), qwenCodexDeclaration())
}

// qwenCodexDeclaration is the source repository's own spawn.runtimes entry:
// `qwen-codex` = (codex harness x alibaba broker). Its provenance names where
// that binding was read, because a declaration whose broker nobody looked for
// is refused by Validate.
func qwenCodexDeclaration() vendorplugin.RuntimeDeclaration {
	return vendorplugin.RuntimeDeclaration{
		ID:     "qwen-codex",
		System: "codex",
		Vendor: "alibaba",
		Broker: vendorplugin.BrokerProvenance{
			Checked: []string{"skill-project-management task-board.config.json (spawn.runtimes.qwen-codex)"},
			Found:   "the repository config declares this runtime with agentic_system codex and broker alibaba",
		},
	}
}

// TestEverySourceRowIsAccountedFor is the count half of the full-set pin.
//
// It adds up rather than spot-checks: the ported vendors' declared totals plus
// the rows whose vendor the source never established must be the whole source
// table, and every one of those numbers is stated in this file independently of
// the fixture. A row silently moved from one vendor to another, or dropped
// entirely, fails here before any per-row comparison runs.
func TestEverySourceRowIsAccountedFor(t *testing.T) {
	fixture := loadSourceRegistry(t)
	if len(fixture.Models) != sourceModelCount {
		t.Fatalf("the source table has %d rows and this port pins %d; a changed count is a changed contract and has to be argued, not absorbed",
			len(fixture.Models), sourceModelCount)
	}

	ported := 0
	for _, count := range portedVendors {
		ported += count
	}
	if ported+len(unresolvedVendorRows) != sourceModelCount {
		t.Fatalf("the four vendors claim %d rows and %d are recorded as vendor-unresolved, which is %d of %d source rows; the remainder is a row nobody owns",
			ported, len(unresolvedVendorRows), ported+len(unresolvedVendorRows), sourceModelCount)
	}

	byBroker := map[string]int{}
	for _, model := range fixture.Models {
		byBroker[model.Broker]++
	}
	for vendor, want := range portedVendors {
		if got := byBroker[string(vendor)]; got != want {
			t.Errorf("the source table gives vendor %s %d rows and this port pins %d", vendor, got, want)
		}
	}
	if got := byBroker[""]; got != len(unresolvedVendorRows) {
		t.Errorf("the source table has %d rows with no established broker and this port names %d (%v)", got, len(unresolvedVendorRows), unresolvedVendorRows)
	}

	// The unresolved rows are named, not merely counted: a different pair of
	// rows losing their broker would otherwise pass.
	unresolved := map[string]bool{}
	for _, model := range fixture.Models {
		if model.Broker == "" {
			unresolved[model.ID] = true
		}
	}
	for _, id := range unresolvedVendorRows {
		if !unresolved[id] {
			t.Errorf("this port records %q as a vendor-unresolved row and the source table does not", id)
		}
		delete(unresolved, id)
	}
	for id := range unresolved {
		t.Errorf("the source table has a vendor-unresolved row %q that this port does not account for", id)
	}
}

// comparePort reports every disagreement between a source table and what the
// vendor plugins declare, in BOTH directions.
//
// It is a function rather than a body inside the pin test so the same
// comparison can be driven over a DRIFTED source table:
// TestTheFullSetPinFiresOnDrift narrows the gate by mutating the fixture and
// requiring the exact disagreement back. Without that, a green pin would be
// equally consistent with a comparison that never compares anything.
//
// Both directions are checked because one alone is not a pin: covering only the
// source admits an invented model, and grounding only the port admits a dropped
// one.
func comparePort(fixture sourceRegistry, declarations []vendorplugin.RuntimeDeclaration, models map[vendorplugin.VendorID][]vendorplugin.Model) []string {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	remaining := map[vendorplugin.VendorID]map[vendorplugin.ModelID]vendorplugin.Model{}
	for vendor, rows := range models {
		index := map[vendorplugin.ModelID]vendorplugin.Model{}
		for _, model := range rows {
			index[model.ID] = model
		}
		remaining[vendor] = index
	}

	// The systems the SOURCE table knows about. A system this module added
	// after the port — pi-native, which never existed in the extraction
	// source — is not a drift from the source and is filtered out of the
	// comparison below; TestTheSourcePortPinStillSeesASourceSystemDropped
	// proves the filter does not blind the pin to the systems it does own.
	sourceSystems := map[string]bool{}
	for _, source := range fixture.Models {
		for _, system := range source.AgenticSystems {
			sourceSystems[system] = true
		}
	}

	for _, source := range fixture.Models {
		if source.Broker == "" {
			continue
		}
		vendor := vendorplugin.VendorID(source.Broker)
		rows, ported := remaining[vendor]
		if !ported {
			report("source row %q names broker %q, which this port does not register as a vendor", source.ID, source.Broker)
			continue
		}
		model, declared := rows[vendorplugin.ModelID(source.ID)]
		if !declared {
			report("vendor %s does not declare source row %q", vendor, source.ID)
			continue
		}
		delete(rows, model.ID)

		gotSystems := make([]string, 0, len(model.Systems))
		var portedSystems []agentic.SystemID
		for _, system := range model.Systems {
			if !sourceSystems[string(system)] {
				continue
			}
			gotSystems = append(gotSystems, string(system))
			portedSystems = append(portedSystems, system)
		}
		if !equalStrings(gotSystems, source.AgenticSystems) {
			report("model %q declares systems %v and the source binds it to %v", source.ID, gotSystems, source.AgenticSystems)
		}

		// The runtime binding, DERIVED from the (system x vendor) pair rather
		// than restated: a table of runtimes written here would be one more
		// place the bindings live.
		if len(portedSystems) > 0 {
			got, unique := runtimeOf(declarations, portedSystems[0], vendor)
			switch {
			case !unique:
				report("model %q's pair (%s x %s) does not name exactly one declared runtime", source.ID, portedSystems[0], vendor)
			case string(got) != source.Runtime:
				report("model %q resolves to runtime %s through (%s x %s) and the source registers it under %s",
					source.ID, got, portedSystems[0], vendor, source.Runtime)
			}
		}

		wantSupport := agentic.EffortSupportNone
		if source.Reasoning == "ReasoningRequired" {
			wantSupport = agentic.EffortSupportRequired
		}
		if model.Effort.Support != wantSupport {
			report("model %q declares effort support %s and the source records %s", source.ID, model.Effort.Support, source.Reasoning)
		}
		if !equalStrings(model.Effort.Vocabulary, source.SupportedEfforts) {
			report("model %q accepts %v and the source accepts %v", source.ID, model.Effort.Vocabulary, source.SupportedEfforts)
		}
		if model.Effort.Recommended != source.RecommendedEffort {
			report("model %q recommends %q and the source recommends %q", source.ID, model.Effort.Recommended, source.RecommendedEffort)
		}
	}

	// Rows this module declares BEFORE the board's registry does have no source
	// row to be compared against, and are accounted for by name rather than
	// absorbed: declaredhere_test.go says what a name costs and what it does
	// not buy. Only a NAMED row is removed here, so the leftover sweep below
	// still reports any other undeclared model — the mutant that proves it is
	// TestAnUnnamedExtraRowIsStillRefusedByBothPins.
	sourceIDs := map[vendorplugin.ModelID]bool{}
	for _, source := range fixture.Models {
		sourceIDs[vendorplugin.ModelID(source.ID)] = true
	}
	for _, id := range sortedModelIDs(declaredHereRows) {
		vendor := declaredHereRows[id]
		rows, ported := remaining[vendor]
		switch {
		case !ported:
			report("this port records %q as declared by vendor %q, which is not a vendor it registers", id, vendor)
		case sourceIDs[id]:
			report("the source table now holds %q, which this port records as declared here; a row the source declares is a PORTED row, and it must carry the source's own score as evidence instead of a machine-local catalog read", id)
		default:
			if _, declared := rows[id]; !declared {
				report("this port records %q as a row vendor %s declares here, and that vendor declares no such model", id, vendor)
				continue
			}
			delete(rows, id)
		}
	}

	vendors := make([]vendorplugin.VendorID, 0, len(remaining))
	for vendor := range remaining {
		vendors = append(vendors, vendor)
	}
	sort.Slice(vendors, func(i, j int) bool { return vendors[i] < vendors[j] })
	for _, vendor := range vendors {
		leftover := make([]string, 0, len(remaining[vendor]))
		for id := range remaining[vendor] {
			leftover = append(leftover, string(id))
		}
		sort.Strings(leftover)
		for _, id := range leftover {
			report("vendor %s declares model %q, which is not a row of the source table", vendor, id)
		}
	}
	return problems
}

// portedModels reads the four vendors out of the DEFAULT registry, which is the
// registry a binary launches through.
func portedModels(t *testing.T) map[vendorplugin.VendorID][]vendorplugin.Model {
	t.Helper()
	out := map[vendorplugin.VendorID][]vendorplugin.Model{}
	for vendor := range portedVendors {
		plugin, registered := vendorplugin.Default.Lookup(vendor)
		if !registered {
			t.Fatalf("vendor %s is not registered in the default registry; importing its package is the production registration path and it did not happen", vendor)
		}
		out[vendor] = plugin.Models()
	}
	return out
}

// TestEverySourceModelRowIsPorted is the per-row half of the full-set pin.
func TestEverySourceModelRowIsPorted(t *testing.T) {
	problems := comparePort(loadSourceRegistry(t), declaredRuntimes(t), portedModels(t))
	for _, problem := range problems {
		t.Error(problem)
	}
}

// TestTheSourcePortPinStillSeesASourceSystemDropped is the narrowing for the
// source-systems filter above: a ported row that keeps pi-native and LOSES its
// source harness must still be reported, or the filter has turned the pin
// into one that accepts any row naming a post-port system.
func TestTheSourcePortPinStillSeesASourceSystemDropped(t *testing.T) {
	models := portedModels(t)
	rows := append([]vendorplugin.Model(nil), models["anthropic"]...)
	mutated := false
	for i := range rows {
		if rows[i].ID == "claude-opus-5" {
			rows[i].Systems = []agentic.SystemID{"pi-native"}
			mutated = true
		}
	}
	if !mutated {
		t.Fatal("fixture assumption broken: anthropic declares no claude-opus-5 row")
	}
	models["anthropic"] = rows
	problems := comparePort(loadSourceRegistry(t), declaredRuntimes(t), models)
	want := `model "claude-opus-5" declares systems [] and the source binds it to [claude-code]`
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return
		}
	}
	t.Fatalf("a row that dropped its source harness and kept pi-native was not reported; got %v", problems)
}

// TestTheFullSetPinFiresOnDrift narrows the gate instead of deleting it.
//
// Each subtest drifts the SOURCE table by one field — the shape a real drift
// takes when the extraction source moves and this port does not follow — and
// requires the pin to name that exact row. A pin that stayed silent on any of
// these would pass forever while describing a table that no longer exists.
//
// The mutants are the ways this port could go wrong that a count alone cannot
// see: a row dropped, a row invented, a vocabulary narrowed by one word, a
// recommendation moved, and a model rebound to another harness.
func TestTheFullSetPinFiresOnDrift(t *testing.T) {
	declarations := declaredRuntimes(t)
	models := portedModels(t)

	tests := []struct {
		name   string
		mutate func(*sourceRegistry)
		expect string
	}{
		{
			name: "a source row this port would then not cover",
			mutate: func(f *sourceRegistry) {
				f.Models = append(f.Models, sourceModel{
					ID: "claude-opus-9", Runtime: "claude", Broker: "anthropic",
					AgenticSystems: []string{"claude-code"}, PolicyRank: 999,
					Reasoning: "ReasoningRequired", SupportedEfforts: []string{"high"}, RecommendedEffort: "high",
				})
			},
			expect: `does not declare source row "claude-opus-9"`,
		},
		{
			name: "a source row dropped, leaving this port declaring one too many",
			mutate: func(f *sourceRegistry) {
				f.Models = removeSourceRow(f.Models, "gpt-5.6-sol")
			},
			expect: `declares model "gpt-5.6-sol", which is not a row of the source table`,
		},
		{
			name: "an effort vocabulary narrowed by exactly one word",
			mutate: func(f *sourceRegistry) {
				mutateSourceRow(f.Models, "gpt-5.6-sol", func(m *sourceModel) {
					m.SupportedEfforts = m.SupportedEfforts[:len(m.SupportedEfforts)-1]
				})
			},
			expect: `model "gpt-5.6-sol" accepts`,
		},
		{
			name: "a recommended effort moved",
			mutate: func(f *sourceRegistry) {
				mutateSourceRow(f.Models, "qwen3.7-plus", func(m *sourceModel) { m.RecommendedEffort = "max" })
			},
			expect: `model "qwen3.7-plus" recommends "xhigh" and the source recommends "max"`,
		},
		{
			name: "a model rebound to the other harness of the same vendor",
			mutate: func(f *sourceRegistry) {
				mutateSourceRow(f.Models, "gemini-2.5-pro", func(m *sourceModel) {
					m.AgenticSystems = []string{"antigravity"}
					m.Runtime = "agy"
				})
			},
			expect: `model "gemini-2.5-pro" declares systems [gemini-cli] and the source binds it to [antigravity]`,
		},
		{
			name: "a model's effort axis appearing where the source has none",
			mutate: func(f *sourceRegistry) {
				mutateSourceRow(f.Models, "claude-haiku-4-5", func(m *sourceModel) {
					m.Reasoning = "ReasoningRequired"
					m.SupportedEfforts = []string{"low", "high"}
					m.RecommendedEffort = "high"
				})
			},
			expect: `model "claude-haiku-4-5" declares effort support none and the source records ReasoningRequired`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := loadSourceRegistry(t)
			fixture.Models = append([]sourceModel(nil), fixture.Models...)
			tt.mutate(&fixture)

			problems := comparePort(fixture, declarations, models)
			if len(problems) == 0 {
				t.Fatalf("the drifted table produced no disagreement; the pin does not compare this field, so its silence on the real table proves nothing")
			}
			found := false
			for _, problem := range problems {
				if strings.Contains(problem, tt.expect) {
					found = true
				}
			}
			if !found {
				t.Fatalf("the pin fired but never named the drift: wanted a report containing %q, got %v", tt.expect, problems)
			}
		})
	}
}

func removeSourceRow(models []sourceModel, id string) []sourceModel {
	out := make([]sourceModel, 0, len(models))
	for _, model := range models {
		if model.ID != id {
			out = append(out, model)
		}
	}
	return out
}

func mutateSourceRow(models []sourceModel, id string, apply func(*sourceModel)) {
	for i := range models {
		if models[i].ID == id {
			models[i].SupportedEfforts = append([]string(nil), models[i].SupportedEfforts...)
			models[i].AgenticSystems = append([]string(nil), models[i].AgenticSystems...)
			apply(&models[i])
			return
		}
	}
	panic("mutateSourceRow: no such row " + id)
}

// TestPortedScoresAreTheSourceScoresAndTheDerivedOrderFollowsThem pins both
// halves of the rank: the ported fact and the derived presentation.
//
// The FACT half is a straight comparison — this layer now carries the source's
// own per-broker score, so a ported score that is not the source's score is a
// transcription error and nothing else. Until v0.2.0 this test could only check
// the ORDER, because the score was reshaped into a tie-free position on the way
// in and the reshape destroyed the ties.
//
// The DERIVED half is what Lineup produces from those scores: positions running
// 1..n with no gaps, in score-descending order with ties broken by declaration
// order, and every row on a shared score reporting Tied. That last assertion is
// the one the reshape used to make impossible, and it is the reason this story
// exists: the board's ranking consumers read ties, and a port that flattened
// them was lying about equality in the quietest possible way.
func TestPortedScoresAreTheSourceScoresAndTheDerivedOrderFollowsThem(t *testing.T) {
	fixture := loadSourceRegistry(t)
	for vendor := range portedVendors {
		plugin, registered := vendorplugin.Default.Lookup(vendor)
		if !registered {
			t.Fatalf("vendor %s is not registered", vendor)
		}
		for _, problem := range comparePortedLineup(fixture, vendor, plugin) {
			t.Error(problem)
		}
	}
}

// comparePortedLineup reports every disagreement between one vendor's source
// rows and the lineup its plugin derives.
//
// The whole lineup's POSITIONS are checked, rows declared ahead of the board's
// registry included: 1..n with no gaps is a property of Lineup itself and a
// declared row must not put a hole in it. Everything the SOURCE has an opinion
// about — the order, the scores, the ties — is then checked over the ported
// subsequence, because the source has no opinion about a row it does not carry.
//
// Tied is read off the derived lineup and compared against a tie computed from
// the SOURCE's scores. That is what makes a declared row landing on a ported
// score a failure rather than a silent reshaping: the added row flips its
// neighbour to Tied while the source's own numbers say it is alone.
//
// It is a function rather than a test body so a mutant can drive it over a
// reshaped row list — TestThePortedLineupPinFiresOnADeclaredRowThatDisturbsIt.
func comparePortedLineup(fixture sourceRegistry, vendor vendorplugin.VendorID, plugin vendorplugin.Vendor) []string {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	want := make([]sourceModel, 0)
	for _, model := range fixture.Models {
		if model.Broker == string(vendor) {
			want = append(want, model)
		}
	}
	sort.SliceStable(want, func(i, j int) bool { return want[i].PolicyRank > want[j].PolicyRank })

	// The position half restates a LineupOf invariant over the real production
	// rows, and it has NO mutant below, because a Vendor cannot produce one:
	// positions are derived inside LineupOf from the row list, so no reshaping
	// of that list makes them non-contiguous. It is kept as the check it always
	// was and is claimed as nothing more.
	full := vendorplugin.LineupOf(plugin)
	got := make([]vendorplugin.RankedModel, 0, len(full))
	for i, entry := range full {
		if entry.Position != i+1 {
			report("vendor %s's model %q sits at derived position %d where the ordering expects %d; positions must run 1..n with no gaps",
				vendor, entry.Model.ID, entry.Position, i+1)
		}
		if declaredHereRows[entry.Model.ID] == vendor {
			continue
		}
		got = append(got, entry)
	}

	if len(got) != len(want) {
		report("vendor %s declares %d models the source table should carry and the source table gives it %d", vendor, len(got), len(want))
		return problems
	}
	for i := range want {
		if string(got[i].Model.ID) != want[i].ID {
			report("vendor %s's derived lineup puts %q at ported position %d; the source's scores in descending declaration order put %q there",
				vendor, got[i].Model.ID, i+1, want[i].ID)
		}
		if got[i].Model.Rank.Score != want[i].PolicyRank {
			report("vendor %s's model %q carries score %d and the source scores it %d",
				vendor, got[i].Model.ID, got[i].Model.Rank.Score, want[i].PolicyRank)
		}
		// Tied is checked against the SOURCE's scores rather than against
		// the ported ones, so a port that dropped a tie by nudging one
		// score is reported here rather than agreeing with itself.
		shared := 0
		for _, other := range want {
			if other.PolicyRank == want[i].PolicyRank {
				shared++
			}
		}
		if wantTied := shared > 1; got[i].Tied != wantTied {
			report("vendor %s's model %q reports Tied=%v and the source's score %d is carried by %d of its rows",
				vendor, got[i].Model.ID, got[i].Tied, want[i].PolicyRank, shared)
		}
	}
	return problems
}

// TestThePortedLineupPinFiresOnADeclaredRowThatDisturbsIt narrows the gate that
// the skip in comparePortedLineup opened.
//
// Skipping a declared row from the ported comparison is correct and is also the
// exact place a bypass could hide, so each mutant reshapes the openai row list
// the way a careless addition would and requires the pin to name it.
//
// What is deliberately NOT a mutant here: a declared row scored BETWEEN two
// ported ones. That was tried and produced no disagreement, correctly — the
// ported subsequence keeps its relative order whatever a skipped row scores
// between two of them, and only a SHARED score changes what the source's own
// numbers say about a ported row. An interleaving mutant would have been a test
// asserting a failure the design does not have.
func TestThePortedLineupPinFiresOnADeclaredRowThatDisturbsIt(t *testing.T) {
	fixture := loadSourceRegistry(t)

	tests := []struct {
		name    string
		reshape func([]vendorplugin.Model) []vendorplugin.Model
		expect  string
	}{
		{
			// The comparison is keyed on the skipped row's NEIGHBOURS, not on
			// the skipped row: sol keeps 120, the declared row joins it there,
			// and the source's table says that score is carried by one row.
			name: "a declared row landing on a ported score",
			reshape: func(rows []vendorplugin.Model) []vendorplugin.Model {
				out := vendorplugin.CloneModels(rows)
				for i := range out {
					if declaredHereRows[out[i].ID] == "openai" {
						out[i].Rank.Score = 120
					}
				}
				return out
			},
			expect: `model "gpt-5.6-sol" reports Tied=true and the source's score 120 is carried by 1 of its rows`,
		},
		{
			// The skip is keyed on the NAME. A row nobody named has to stay in
			// the ported comparison and blow the count, or the skip would be a
			// standing allowance for any invented model.
			name: "a row nobody named, which the skip must not cover",
			reshape: func(rows []vendorplugin.Model) []vendorplugin.Model {
				out := vendorplugin.CloneModels(rows)
				extra := vendorplugin.CloneModels(rows)[0]
				extra.ID = "gpt-7-nobody-published-this"
				extra.Rank.Score = 5
				return append(out, extra)
			},
			expect: "vendor openai declares 13 models the source table should carry and the source table gives it 12",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireReport(t, comparePortedLineup(fixture, "openai", vendorRowsWith(t, "openai", tt.reshape)), tt.expect)
		})
	}
}

// TestTheSourceTiesSurviveThePort names the ties instead of counting them.
//
// A property test over "Tied agrees with the fixture" passes on a table with no
// ties at all, so it is equally consistent with a port that flattened every one
// of them and a fixture regenerated to match. These are the ties the board's
// own table carries, written down here independently of the fixture, exactly as
// sourceModelCount is: losing one has to be argued rather than absorbed.
func TestTheSourceTiesSurviveThePort(t *testing.T) {
	ties := map[vendorplugin.VendorID][][]string{
		"anthropic": {{"claude-haiku-4-5", "claude-haiku-4-5-20251001"}},
		"alibaba":   {{"qwen3.7-plus", "qwen3.7-plus-via-codex"}},
		"google": {
			{"gemini-3.1-pro-preview", "gemini-3.6-flash-high"},
			{"gemini-3.5-flash", "gemini-3.6-flash-medium"},
			{"gemini-3-flash-preview", "gemini-3.6-flash-low"},
			{"gemini-3.1-flash-lite", "gemini-3.5-flash-high"},
			{"gemini-2.5-pro", "gemini-3.5-flash-medium"},
		},
		// openai's twelve PORTED rows carry twelve distinct scores. The empty
		// entry is deliberate: it states that this vendor was checked and has
		// none, which is a different fact from this vendor being absent from
		// the map. TestADeclaredRowMayNotTieAPortedOne is what keeps it true
		// as rows are declared ahead of the board's registry.
		//
		// The vendor's fourteen DECLARED rows do carry one tie — `astra` and
		// `gpt-6-astra`, at 130 — and it is outside this map on purpose: this
		// map records ties THE SOURCE carries, and the source carries neither
		// row. That tie is an alias and its identity scoring the same, which
		// is one model under two names rather than an equality between two.
		"openai": {},
	}
	if len(ties) != len(portedVendors) {
		t.Fatalf("this test names ties for %d vendors and %d are ported; a vendor nobody checked could flatten every tie it has", len(ties), len(portedVendors))
	}

	for vendor, groups := range ties {
		plugin, registered := vendorplugin.Default.Lookup(vendor)
		if !registered {
			t.Fatalf("vendor %s is not registered", vendor)
		}
		scoreOf := map[vendorplugin.ModelID]int{}
		for _, model := range plugin.Models() {
			scoreOf[model.ID] = model.Rank.Score
		}
		for _, group := range groups {
			first, known := scoreOf[vendorplugin.ModelID(group[0])]
			if !known {
				t.Errorf("vendor %s does not declare %q, which this port records as one half of a tie", vendor, group[0])
				continue
			}
			for _, id := range group[1:] {
				score, known := scoreOf[vendorplugin.ModelID(id)]
				if !known {
					t.Errorf("vendor %s does not declare %q, which this port records as tied with %q", vendor, id, group[0])
					continue
				}
				if score != first {
					t.Errorf("vendor %s scores %q at %d and %q at %d; the source ties them, and a port that separates two equal models states an ordering nobody observed",
						vendor, group[0], first, id, score)
				}
			}
		}
	}
}

// TestEveryRankCarriesTheSourceEvidence is the "no rank without observation"
// rule held to the actual observation.
//
// CapabilityRank.Validate already refuses an empty basis, so a test that only
// checked for non-emptiness would be testing the type. What this checks is that
// the basis carries the fact the position was DERIVED from — the source's own
// score for that row — so a position invented here cannot borrow the
// credibility of one that was ported.
func TestEveryRankCarriesTheSourceEvidence(t *testing.T) {
	fixture := loadSourceRegistry(t)
	scores := map[string]int{}
	for _, model := range fixture.Models {
		scores[model.ID] = model.PolicyRank
	}

	for vendor := range portedVendors {
		plugin, _ := vendorplugin.Default.Lookup(vendor)
		for _, model := range plugin.Models() {
			if err := model.Rank.Validate(); err != nil {
				t.Errorf("vendor %s model %q: %v", vendor, model.ID, err)
				continue
			}
			score, known := scores[string(model.ID)]
			if !known {
				// No source row, so no source score to carry. That is either a
				// row nobody accounted for — the full-set pin reports it — or a
				// row this module declares first, and the one thing that must
				// not happen is the second silently escaping the evidence rule.
				// It is REDIRECTED rather than dropped: the mirror requirement
				// lives in TestEveryDeclaredHereRowRestsOnTheVendorCatalog, and
				// what is refused right here is such a row quoting a board
				// score no board file contains.
				if declaredHereRows[model.ID] != vendor {
					continue // reported by the full-set pin
				}
				for _, evidence := range model.Rank.Basis {
					if strings.Contains(evidence.Observation, portedScoreObservation) {
						t.Errorf("vendor %s model %q is declared ahead of the source registry and no row of it exists, yet a basis entry quotes %q; that is a number a reader opens the named file and does not find",
							vendor, model.ID, evidence.Observation)
					}
				}
				continue
			}
			needle := "PolicyRank " + strconv.Itoa(score)
			carried := false
			named := false
			for _, evidence := range model.Rank.Basis {
				if strings.Contains(evidence.Observation, needle) {
					carried = true
				}
				if strings.Contains(evidence.Source, "internal/spawn/models.go") {
					named = true
				}
			}
			if !carried {
				t.Errorf("vendor %s model %q scores %d and no basis entry records the source's own %q; the score would then rest on this repository's say-so",
					vendor, model.ID, model.Rank.Score, needle)
			}
			if !named {
				t.Errorf("vendor %s model %q carries no basis entry naming the source registry file; evidence a reader cannot open is prose", vendor, model.ID)
			}
		}
	}
}

// TestEveryModelHasAUsageDescription covers the field the source does not have.
func TestEveryModelHasAUsageDescription(t *testing.T) {
	for vendor := range portedVendors {
		plugin, _ := vendorplugin.Default.Lookup(vendor)
		for _, model := range plugin.Models() {
			if err := model.Description.Validate(); err != nil {
				t.Errorf("vendor %s model %q: %v", vendor, model.ID, err)
			}
		}
	}
}

// TestUsageDescriptionsAreMarkedAuthoredHere is AC3's second half, and it is
// deliberately a test rather than a convention.
//
// The descriptions are the only field in a binding file that is NOT a source
// fact. A reader who mistakes one for a ported fact will cite it as evidence
// about a vendor's model, so the provenance note is load-bearing documentation
// — and documentation nothing holds to the code is the exact failure this
// repository keeps writing tests against. Deleting the note fails here.
func TestUsageDescriptionsAreMarkedAuthoredHere(t *testing.T) {
	homes := map[vendorplugin.VendorID]string{
		"anthropic": "vendors/anthropic/models.go",
		"openai":    "vendors/openai/models.go",
		"alibaba":   "vendors/alibaba/models.go",
		"google":    "vendors/google/models.go",
	}
	for vendor, path := range homes {
		raw, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatalf("reading vendor %s's binding file %s: %v", vendor, path, err)
		}
		text := string(raw)
		for _, required := range []string{
			"AUTHORED HERE, not ported: every Description",
			"never be cited as one",
		} {
			if !strings.Contains(text, required) {
				t.Errorf("%s does not state %q; every Description in it is authorship rather than a ported fact, and a reader has no way to tell without the note", path, required)
			}
		}
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func formatPairs(set vendorplugin.AdmittedPairSet) string {
	return fmt.Sprintf("%s: %s", set.Provider, strings.Join(set.PinnedPairs(), " "))
}
