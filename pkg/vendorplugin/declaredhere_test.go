package vendorplugin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file owns ONE fact: which model rows this module declares that no
// capture of the board's registry contains, and what such a row has to carry
// instead of the source evidence a ported row carries.
//
// # Why the concept has to exist at all
//
// Both full-set pins are bijections. sourceport_test.go requires every vendor
// row to be a row of testdata/source-model-registry.json and vice versa, and
// boardfacts_test.go does the same against testdata/board-model-facts.json.
// That was exactly right while every row here was a port. It stops being a
// complete description the first time a model reaches THIS repository before
// the board's — the direction the swap made normal, since v0.2.0 the board
// maps its registry out of these plugins rather than declaring it.
//
// # The move this file exists to refuse
//
// The cheap way to make a new row green is to hand-write it into
// testdata/source-model-registry.json. The fixture is a CAPTURE of another
// repository's sources at a named commit with a recorded sha256, so a row
// added to it by hand is a claim about a file that does not contain it — and
// the rank basis a ported row must carry ("the row carries PolicyRank N",
// naming internal/spawn/models.go) would then quote a number a reader opens
// that file and does not find. That is self-minted evidence with a provenance
// block on top, which is worse than none.
//
// So a row declared here is NOT exempt from the pins. It is held by different
// evidence and accounted for by name:
//
//   - it is named below, against the vendor that declares it;
//   - the vendor must actually declare it (TestEveryDeclaredHereRowIsDeclared);
//   - it must rest on the vendor's own catalog and must NOT quote a board
//     PolicyRank (TestEveryDeclaredHereRowRestsOnTheVendorCatalog);
//   - it must not share a score with a ported row of the same vendor, which
//     would make that ported row report a tie nobody observed
//     (TestADeclaredRowMayNotTieAPortedOne);
//   - and the moment either capture starts carrying it, the pins report it and
//     the list has to shrink — the entry is a bridge, not a permanent bypass.
//
// A row NOT named below is still reported by both pins exactly as before.
// TestAnUnnamedExtraRowIsStillRefusedByBothPins is the mutant that says so.

// declaredHereRows maps a row this module declares first to the vendor that
// declares it.
//
// Two entries today, and they are ONE model under two spellings.
//
// gpt-6-astra was published by OpenAI and read off the installed Codex CLI's
// own catalog; the board's registry has no row for it and neither capture can
// be regenerated into one. `astra` is the short spelling of that same head,
// declared here with AliasOf and published by nobody — the catalog carries no
// such slug — so no capture will ever carry it either.
//
// The two share a capability score, which is legal precisely because both are
// named here: TestADeclaredRowMayNotTieAPortedOne forbids a declared row tying
// a PORTED one, because that would publish an equality no capture recorded. A
// tie between an alias and its own identity records nothing about two models.
var declaredHereRows = map[vendorplugin.ModelID]vendorplugin.VendorID{
	"gpt-6-astra": "openai",
	"astra":       "openai",
}

// declaredHereCatalogEvidence is the substring a declared-here row's basis must
// name: the vendor surface the row was actually read from.
const declaredHereCatalogEvidence = "codex debug models"

// portedScoreObservation is the sentence rank() puts on a PORTED row. A
// declared-here row carrying it is quoting a board score that does not exist.
const portedScoreObservation = "the row carries PolicyRank "

func sortedModelIDs(rows map[vendorplugin.ModelID]vendorplugin.VendorID) []vendorplugin.ModelID {
	out := make([]vendorplugin.ModelID, 0, len(rows))
	for id := range rows {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// declaredHereModel finds a named row in the vendor that is supposed to declare
// it.
func declaredHereModel(t *testing.T, id vendorplugin.ModelID) (vendorplugin.Model, bool) {
	t.Helper()
	vendor, named := declaredHereRows[id]
	if !named {
		t.Fatalf("%q is not a declared-here row", id)
	}
	plugin, registered := vendorplugin.Default.Lookup(vendor)
	if !registered {
		t.Fatalf("vendor %s is not registered in the default registry", vendor)
	}
	for _, model := range plugin.Models() {
		if model.ID == id {
			return model, true
		}
	}
	return vendorplugin.Model{}, false
}

// TestEveryDeclaredHereRowIsDeclared closes the direction a name alone cannot:
// an entry left behind after its row was deleted would quietly widen both pins
// by one row forever.
func TestEveryDeclaredHereRowIsDeclared(t *testing.T) {
	if len(declaredHereRows) == 0 {
		t.Skip("no row is declared ahead of the board's registry")
	}
	for _, id := range sortedModelIDs(declaredHereRows) {
		if _, declared := declaredHereModel(t, id); !declared {
			t.Errorf("%q is recorded as a row vendor %s declares here and that vendor declares no such model; the entry widens both full-set pins and covers nothing",
				id, declaredHereRows[id])
		}
	}
}

// TestEveryDeclaredHereRowRestsOnTheVendorCatalog is the evidence rule for a
// row with no source row, and it is the mirror of
// TestEveryRankCarriesTheSourceEvidence rather than an exemption from it.
//
// Both halves matter. The row must NAME the vendor surface it was read from, so
// the score is checkable by re-running one command; and it must NOT carry the
// ported-row sentence, so a reader who sees "PolicyRank" in a basis can trust
// that the number is in the file the basis names.
func TestEveryDeclaredHereRowRestsOnTheVendorCatalog(t *testing.T) {
	if len(declaredHereRows) == 0 {
		t.Skip("no row is declared ahead of the board's registry")
	}
	for _, id := range sortedModelIDs(declaredHereRows) {
		model, declared := declaredHereModel(t, id)
		if !declared {
			continue // reported by TestEveryDeclaredHereRowIsDeclared
		}
		if err := model.Rank.Validate(); err != nil {
			t.Errorf("declared-here model %q: %v", id, err)
			continue
		}
		named, borrowed := false, ""
		for _, evidence := range model.Rank.Basis {
			if strings.Contains(evidence.Source, declaredHereCatalogEvidence) {
				named = true
			}
			if strings.Contains(evidence.Observation, portedScoreObservation) {
				borrowed = evidence.Observation
			}
		}
		if !named {
			t.Errorf("declared-here model %q carries no basis entry naming %q; a row no capture holds and no vendor surface names is ranked on this repository's say-so",
				id, declaredHereCatalogEvidence)
		}
		if borrowed != "" {
			t.Errorf("declared-here model %q quotes a ported score observation (%q); the source registry has no row for it, so the number is one a reader opens that file and does not find",
				id, borrowed)
		}
	}
}

// TestADeclaredRowMayNotTieAPortedOne is a gate rather than an aesthetic rule.
//
// Tied is DERIVED over a vendor's whole lineup, so a row added here on a score
// a ported row already carries flips that ported row to Tied — an equality the
// board never recorded, published to every ranking consumer as though it had
// been observed. The scores are spaced by ten across every vendor precisely so
// a new row never has to land on one.
func TestADeclaredRowMayNotTieAPortedOne(t *testing.T) {
	if len(declaredHereRows) == 0 {
		t.Skip("no row is declared ahead of the board's registry")
	}
	for _, id := range sortedModelIDs(declaredHereRows) {
		vendor := declaredHereRows[id]
		added, declared := declaredHereModel(t, id)
		if !declared {
			continue // reported by TestEveryDeclaredHereRowIsDeclared
		}
		plugin, _ := vendorplugin.Default.Lookup(vendor)
		for _, other := range plugin.Models() {
			if other.ID == id || declaredHereRows[other.ID] == vendor {
				continue
			}
			if other.Rank.Score == added.Rank.Score {
				t.Errorf("declared-here model %q scores %d and vendor %s's ported row %q carries the same score; the derived lineup would then report %q tied, which is an equality no capture of the board's table records",
					id, added.Rank.Score, vendor, other.ID, other.ID)
			}
		}
	}
}

// relistVendor re-declares a registered plugin's whole ROW LIST.
//
// mutantVendor transforms rows one at a time and therefore cannot add or drop
// one, which is exactly the drift the declared-here accounting has to survive.
// It wraps the real plugin so a mutant differs from production in the row list
// and in nothing else.
type relistVendor struct {
	inner vendorplugin.Vendor
	rows  []vendorplugin.Model
}

func (v relistVendor) ID() vendorplugin.VendorID { return v.inner.ID() }

func (v relistVendor) Models() []vendorplugin.Model { return vendorplugin.CloneModels(v.rows) }

func (v relistVendor) Availability(q vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	return v.inner.Availability(q)
}

func (v relistVendor) Spawn(sc vendorplugin.SpawnContext) (agentic.LaunchRequest, error) {
	return v.inner.Spawn(sc)
}

// vendorRowsWith returns a vendor's real rows with one transform applied to the
// LIST, and a Vendor that declares them.
func vendorRowsWith(t *testing.T, vendor vendorplugin.VendorID, reshape func([]vendorplugin.Model) []vendorplugin.Model) vendorplugin.Vendor {
	t.Helper()
	plugin, registered := vendorplugin.Default.Lookup(vendor)
	if !registered {
		t.Fatalf("vendor %s is not registered", vendor)
	}
	return relistVendor{inner: plugin, rows: reshape(plugin.Models())}
}

// dropRow removes one row from a list, failing if it was not there.
func dropRow(t *testing.T, rows []vendorplugin.Model, id vendorplugin.ModelID) []vendorplugin.Model {
	t.Helper()
	out := make([]vendorplugin.Model, 0, len(rows))
	for _, row := range rows {
		if row.ID != id {
			out = append(out, row)
		}
	}
	if len(out) == len(rows) {
		t.Fatalf("no row %q to drop", id)
	}
	return out
}

// TestAnUnnamedExtraRowIsStillRefusedByBothPins is the narrowing mutant for the
// whole mechanism.
//
// The accounting added to comparePort and compareBoardFacts skips rows that are
// NAMED. A skip written one line too wide would skip every leftover instead,
// and both bijections would then admit any invented model in silence while
// still passing on the real tree. This adds a row nobody named and requires
// both pins to name it.
func TestAnUnnamedExtraRowIsStillRefusedByBothPins(t *testing.T) {
	const invented vendorplugin.ModelID = "gpt-7-nobody-published-this"

	openai := vendorRowsWith(t, "openai", func(rows []vendorplugin.Model) []vendorplugin.Model {
		extra := rows[0]
		extra.ID = invented
		extra.Rank.Score = 999
		return append(append([]vendorplugin.Model(nil), rows...), extra)
	})

	byVendor := portedModels(t)
	byVendor["openai"] = openai.Models()
	sourceProblems := comparePort(loadSourceRegistry(t), declaredRuntimes(t), byVendor)
	requireReport(t, sourceProblems, `declares model "gpt-7-nobody-published-this", which is not a row of the source table`)

	rows := portedRows(t)
	rows[invented] = openai.Models()[len(openai.Models())-1]
	boardProblems := compareBoardFacts(loadBoardFacts(t), rows)
	requireReport(t, boardProblems, `this module declares model "gpt-7-nobody-published-this", which is not a row of the board table`)
}

// TestADeclaredHereRowThatDisappearsIsReported is the other direction: the name
// stays, the row goes. Without this the list could outlive its rows and quietly
// grant both pins a permanent allowance of n rows.
func TestADeclaredHereRowThatDisappearsIsReported(t *testing.T) {
	if len(declaredHereRows) == 0 {
		t.Skip("no row is declared ahead of the board's registry")
	}
	id := sortedModelIDs(declaredHereRows)[0]
	vendor := declaredHereRows[id]

	byVendor := portedModels(t)
	byVendor[vendor] = vendorRowsWith(t, vendor, func(rows []vendorplugin.Model) []vendorplugin.Model {
		return dropRow(t, rows, id)
	}).Models()

	problems := comparePort(loadSourceRegistry(t), declaredRuntimes(t), byVendor)
	requireReport(t, problems, "declares here, and that vendor declares no such model")
}

// TestADeclaredHereRowTheSourceCatchesUpWithIsReported is the bridge's expiry.
//
// When the board's registry finally declares the row, the entry must not keep
// exempting it: a ported row carries the source's own score as evidence, and a
// row that stayed on this list would go on resting on a machine-local catalog
// read while a real capture of the board's number existed. The pin says so
// rather than absorbing it.
func TestADeclaredHereRowTheSourceCatchesUpWithIsReported(t *testing.T) {
	if len(declaredHereRows) == 0 {
		t.Skip("no row is declared ahead of the board's registry")
	}
	id := sortedModelIDs(declaredHereRows)[0]
	vendor := declaredHereRows[id]
	model, declared := declaredHereModel(t, id)
	if !declared {
		t.Fatalf("%q is named as declared here and vendor %s does not declare it", id, vendor)
	}

	t.Run("the source capture", func(t *testing.T) {
		fixture := loadSourceRegistry(t)
		fixture.Models = append(append([]sourceModel(nil), fixture.Models...), sourceModel{
			ID: string(id), Runtime: "codex", Broker: string(vendor),
			AgenticSystems: []string{"codex"}, PolicyRank: model.Rank.Score,
			Reasoning:        "ReasoningRequired",
			SupportedEfforts: model.Effort.Vocabulary, RecommendedEffort: model.Effort.Recommended,
		})
		requireReport(t, comparePort(fixture, declaredRuntimes(t), portedModels(t)),
			"which this port records as declared here")
	})

	t.Run("the board capture", func(t *testing.T) {
		fixture := loadBoardFacts(t)
		fixture.Models = append(append([]boardModel(nil), fixture.Models...), boardModel{
			ID: string(id), Runtime: "codex", PolicyRank: model.Rank.Score,
			Lifecycle: string(model.Lifecycle), ContextWindowTokens: model.ContextWindowTokens,
		})
		requireReport(t, compareBoardFacts(fixture, portedRows(t)),
			"which this port records as declared here")
	})
}

// requireReport fails unless one of the reported problems contains want.
func requireReport(t *testing.T, problems []string, want string) {
	t.Helper()
	if len(problems) == 0 {
		t.Fatalf("the drifted input produced no disagreement at all; the comparison does not cover this shape, so its silence on the real tree proves nothing (wanted %q)", want)
	}
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return
		}
	}
	t.Fatalf("the comparison fired but never named the drift: wanted a report containing %q, got %v", want, problems)
}

// TestGPT6AstraLaunchesThroughTheRealEntryPointAtEveryProbedEffort drives the
// production call site — vendorplugin.BuildLaunch, the one entry point every
// spawn goes through — rather than reading the declaration back.
//
// A row can be registered, ranked, pinned and displayed while being
// unlaunchable: the runtime index is scoped to the harness a row names, the
// effort has to be transportable by that harness, and the word has to survive
// into argv. This asserts the effort reaches `-c model_reasoning_effort=<word>`
// for EVERY word the vendor catalog says the model accepts, so a vocabulary
// transcribed one word wrong fails here and not in a spawn six weeks from now.
func TestGPT6AstraLaunchesThroughTheRealEntryPointAtEveryProbedEffort(t *testing.T) {
	const model vendorplugin.ModelID = "gpt-6-astra"

	registry := isolatedRegistry(t, nil)
	binDir := t.TempDir()
	writeStubBinary(t, binDir, "codex")
	workDir := t.TempDir()
	promptPath := filepath.Join(workDir, "assignment.md")
	if err := os.WriteFile(promptPath, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}

	resolved, err := registry.ResolveRuntime("codex")
	if err != nil {
		t.Fatalf("ResolveRuntime(codex): %v", err)
	}
	row, indexed := vendorplugin.RuntimeModels(resolved)[model]
	if !indexed {
		t.Fatalf("the codex runtime does not index %q; a row its harness cannot reach is a declaration nothing can launch", model)
	}
	// The probed vocabulary, written down here as the catalog gave it and
	// compared as a LIST rather than a count — `codex debug models` returns
	// supported_reasoning_levels in this order for this row.
	//
	// It is a t.Errorf and not a t.Fatalf on purpose. A fatal here would
	// pre-empt the refusal subtests below, and a vocabulary widened by one word
	// is exactly the mutant whose damage those subtests exist to show: the
	// count would fail, the negative would never run, and the report would say
	// nothing about `minimal` having become launchable.
	probedVocabulary := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	if !equalStrings(row.Effort.Vocabulary, probedVocabulary) {
		t.Errorf("%q accepts %v and the catalog probe recorded %v; the vocabulary is what the vendor published for THIS row",
			model, row.Effort.Vocabulary, probedVocabulary)
	}

	launch := func(effort string) (agentic.Plan, error) {
		return vendorplugin.BuildLaunch(context.Background(), registry, vendorplugin.SpawnRequest{
			Runtime: "codex", Model: model, Effort: effort,
			PromptPath: promptPath, Prompt: []byte("do the thing\n"),
			WorkDir: workDir, Env: []string{"PATH=" + binDir},
			Run: agentic.RunContext{
				RunID: "RUN-ASTRA", TaskID: "TASK-260905-1BU2RB",
				BoardDir: filepath.Join(workDir, ".task-board"), ContextID: "CTX-ASTRA",
			},
		}, agentic.LaunchModeDryRun)
	}

	for _, effort := range row.Effort.Vocabulary {
		t.Run(effort, func(t *testing.T) {
			plan, err := launch(effort)
			if err != nil {
				t.Fatalf("BuildLaunch(codex, %s, %s): %v", model, effort, err)
			}
			if plan.System != "codex" {
				t.Errorf("plan.System = %q, want the harness the row declares", plan.System)
			}
			argv := strings.Join(plan.Argv, " ")
			// `-m <id>` and `-c model_reasoning_effort="<word>"`, quotes
			// included: codex's own argv grammar, asserted as the harness
			// actually spells it rather than as a reader would guess.
			if !strings.Contains(argv, "-m "+string(model)) {
				t.Errorf("plan.Argv = %v, want the model id verbatim; a row whose id does not reach argv launches something else", plan.Argv)
			}
			if want := `model_reasoning_effort="` + effort + `"`; !strings.Contains(argv, want) {
				t.Errorf("plan.Argv = %v, want %q; the word the operator chose has to reach the child, and an argv that drops it runs the vendor's default cost silently", plan.Argv, want)
			}
		})
	}

	// The negatives. A vocabulary is only a vocabulary if something outside it
	// is refused, and these are the two words that would plausibly be tried:
	// `minimal`, which this vendor's five LEGACY rows do accept and this row
	// does not, and the empty effort, which the row's Required support forbids.
	t.Run("minimal, which the legacy openai rows accept and this one does not", func(t *testing.T) {
		if _, err := launch("minimal"); !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) {
			t.Fatalf("BuildLaunch(codex, %s, minimal) = %v, want ErrEffortNotInVocabulary; the vocabulary is per MODEL, and admitting a sibling row's word sends the vendor one it never published for this model", model, err)
		}
	})
	t.Run("no effort at all", func(t *testing.T) {
		if _, err := launch(""); !errors.Is(err, vendorplugin.ErrEffortMissing) {
			t.Fatalf("BuildLaunch(codex, %s, \"\") = %v, want ErrEffortMissing; nothing may substitute the recommended word for an unstated one", model, err)
		}
	})
}
