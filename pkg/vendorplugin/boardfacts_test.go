package vendorplugin_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file is the TRANSITIONAL transcription pin for the board-owned model
// facts this module took over in v0.2.0: the capability score with its ties,
// the lineup state, the supersession, the display recommendation, the context
// window, the billing contract, and the muse rows' effort axis.
//
// # Why it exists and when it dies
//
// Forty-three rows' worth of numbers moved from one repository to another by
// hand, and the table has grown since. A slipped digit in a price, a swapped lifecycle or a dropped tie would
// compile, register, launch and read correctly forever. So every ported value
// is held against a frozen capture of the board's own table
// (testdata/board-model-facts.json, captured by
// .scripts/capture-board-model-facts.sh from the board BINARY at a named
// commit) and a disagreement fails.
//
// It is transitional and must not outlive the swap. When the board reads these
// facts from this module instead of declaring them, the fixture describes this
// module's own output and the pin becomes a test of self-agreement. DELETE both
// at that point rather than regenerating them — the fixture's own provenance
// block says the same thing, so neither can be quietly kept.
//
// # What it deliberately does NOT pin
//
// The board's projection joins its rows to this module's vendor plugins, so its
// agentic systems and its whole effort axis come BACK from here for every row
// whose broker is established. Pinning those against this module would be the
// port agreeing with itself, and the fixture segregates them under
// joined_from_vendor_module so that cannot happen by accident. They are pinned
// against a DIFFERENT capture, taken from the board's sources before the join
// existed — see sourceport_test.go.
//
// The muse rows are the exception the board itself names: no vendor owns them,
// so their effort axis is the board's own declaration and is pinned here.
//
// Descriptions are not in the fixture at all. This module authors its own and
// the board's display texts die with its half; a copy of them in testdata would
// be an invitation to cite one as a ported fact.
//
// Model.AliasOf is not in the fixture either, and for the SAME reason rather
// than by oversight. The board's table has no alias column: it carried
// muse-spark as an ordinary row and put that spelling straight into argv, which
// is the bug the field exists to close. Adding an alias_of key to a capture of
// the board's own projection would be inventing a source fact — the one thing
// this file exists to make impossible — so the field is authored here, checked
// by checkAliases where the rows are registered, and pinned end to end by
// muse_alias_test.go instead.

const boardFactsFixture = "testdata/board-model-facts.json"

// boardPricingPlan is one subscription tier as the board projects it.
type boardPricingPlan struct {
	Name                  string   `json:"name"`
	MonthlyUSD            float64  `json:"monthlyUsd"`
	PromotionalMonthlyUSD *float64 `json:"promotionalMonthlyUsd"`
	MonthlyCredits        int      `json:"monthlyCredits"`
	ApplicableModelIDs    []string `json:"applicableModelIds"`
}

// boardPricing is one billing contract as the board projects it.
type boardPricing struct {
	BillingModel       string             `json:"billingModel"`
	Edition            string             `json:"edition"`
	Currency           string             `json:"currency"`
	QuotaPeriod        string             `json:"quotaPeriod"`
	HasFrequencyLimits bool               `json:"hasFrequencyLimits"`
	Plans              []boardPricingPlan `json:"plans"`
	SourceURL          string             `json:"sourceUrl"`
	AsOf               string             `json:"asOf"`
}

// boardJoined are the columns the projection reads back from this module. They
// are decoded so the fixture round-trips, and used only for the muse rows.
type boardJoined struct {
	Broker            string   `json:"broker"`
	AgenticSystems    []string `json:"agentic_systems"`
	Reasoning         string   `json:"reasoning"`
	SupportedEfforts  []string `json:"supported_efforts"`
	RecommendedEffort string   `json:"recommended_effort"`
}

// boardModel is one modelRegistrations row as the board projects it.
type boardModel struct {
	ID                  string        `json:"id"`
	Runtime             string        `json:"runtime"`
	PolicyRank          int           `json:"policy_rank"`
	Lifecycle           string        `json:"lifecycle"`
	SupersededBy        string        `json:"superseded_by"`
	Recommended         bool          `json:"recommended"`
	ContextWindowTokens int           `json:"context_window_tokens"`
	Pricing             *boardPricing `json:"pricing"`
	Joined              boardJoined   `json:"joined_from_vendor_module"`
}

type boardFacts struct {
	Provenance struct {
		SourceCommit   string `json:"source_commit"`
		RegistrySHA256 string `json:"registry_sha256"`
		CapturedFrom   string `json:"captured_from"`
		Lifetime       string `json:"lifetime"`
	} `json:"provenance"`
	ModelCount int          `json:"model_count"`
	Models     []boardModel `json:"models"`
}

func loadBoardFacts(t *testing.T) boardFacts {
	t.Helper()
	var fixture boardFacts
	readFixture(t, boardFactsFixture, &fixture)
	if fixture.ModelCount != len(fixture.Models) {
		t.Fatalf("%s says it holds %d rows and carries %d; the capture is inconsistent with itself",
			boardFactsFixture, fixture.ModelCount, len(fixture.Models))
	}
	if fixture.Provenance.SourceCommit == "" || fixture.Provenance.RegistrySHA256 == "" {
		t.Fatalf("%s records no source commit or registry hash; a fixture with no provenance is a claim about nothing", boardFactsFixture)
	}
	return fixture
}

// TestTheBoardFixtureDeclaresItsOwnMortality holds the one thing a transitional
// artefact must never lose: the statement that it is one.
//
// A fixture whose header quietly became a permanent-looking capture would be
// regenerated forever against a board that had stopped owning these facts, and
// the pin would go on passing while comparing this module to itself. The
// sentence is load-bearing documentation and nothing but a test holds it.
func TestTheBoardFixtureDeclaresItsOwnMortality(t *testing.T) {
	fixture := loadBoardFacts(t)
	for _, required := range []string{"TRANSITIONAL", "dies with the board", "delete this fixture"} {
		if !strings.Contains(fixture.Provenance.Lifetime, required) {
			t.Errorf("%s's provenance does not state %q; a transitional fixture that stops saying so gets regenerated forever", boardFactsFixture, required)
		}
	}
	if !strings.Contains(fixture.Provenance.CapturedFrom, "models()") {
		t.Errorf("%s does not record that it came from the board's own q 'models()' projection; a capture that names no producer names no evidence", boardFactsFixture)
	}
}

// TestTheBoardFixtureAndTheOlderSourceCaptureAgree is corroboration, and it is
// the reason two captures are kept rather than one.
//
// They were taken from different places at different commits by different
// means: the older one text-parses the board's Go sources at the commit before
// the effort axis moved here, and this one runs the board's binary at trunk. On
// the columns they share — the id set, the score, the lifecycle, the
// supersession and the recommendation — they must agree, and if they ever stop
// agreeing, the board's own facts moved and one of the two captures is stale.
// A single capture could not notice that at all.
func TestTheBoardFixtureAndTheOlderSourceCaptureAgree(t *testing.T) {
	board := loadBoardFacts(t)
	older := loadSourceRegistry(t)

	byID := map[string]sourceModel{}
	for _, model := range older.Models {
		byID[model.ID] = model
	}
	if len(byID) != len(board.Models) {
		t.Fatalf("the older capture holds %d rows and the board capture holds %d", len(byID), len(board.Models))
	}
	for _, row := range board.Models {
		old, known := byID[row.ID]
		if !known {
			t.Errorf("the board capture holds %q and the older source capture does not", row.ID)
			continue
		}
		if old.PolicyRank != row.PolicyRank {
			t.Errorf("row %q scores %d in the board capture and %d in the older source capture", row.ID, row.PolicyRank, old.PolicyRank)
		}
		if old.Runtime != row.Runtime {
			t.Errorf("row %q is registered under runtime %q in the board capture and %q in the older one", row.ID, row.Runtime, old.Runtime)
		}
		// The older capture read the Go SOURCE, so its lifecycle is the
		// constant's name; the board binary projects the constant's value. The
		// two spell one fact, and stripping the prefix is the whole of the
		// translation between them.
		if stripped := strings.TrimPrefix(old.Lifecycle, "ModelLifecycle"); !strings.EqualFold(stripped, row.Lifecycle) {
			t.Errorf("row %q is %q in the board capture and %q in the older source capture", row.ID, row.Lifecycle, old.Lifecycle)
		}
		if old.SupersededBy != row.SupersededBy {
			t.Errorf("row %q names successor %q in the board capture and %q in the older one", row.ID, row.SupersededBy, old.SupersededBy)
		}
		if old.Recommended != row.Recommended {
			t.Errorf("row %q is recommended=%v in the board capture and %v in the older one", row.ID, row.Recommended, old.Recommended)
		}
	}
}

// portedRows is every row this module declares, from BOTH homes: the four
// vendor plugins and the vendor-unresolved runtime declaration.
//
// Reading both is the whole point. The muse rows are the ones with no plugin
// behind them, so a pin that walked only the registered vendors would leave
// exactly the unowned rows unchecked while reporting 42 of 45 green.
func portedRows(t *testing.T) map[vendorplugin.ModelID]vendorplugin.Model {
	t.Helper()
	rows := map[vendorplugin.ModelID]vendorplugin.Model{}
	for vendor := range portedVendors {
		plugin, registered := vendorplugin.Default.Lookup(vendor)
		if !registered {
			t.Fatalf("vendor %s is not registered in the default registry", vendor)
		}
		for _, model := range plugin.Models() {
			rows[model.ID] = model
		}
	}
	for _, declaration := range vendorplugin.Default.RuntimeDeclarations() {
		for _, model := range declaration.Models {
			if _, already := rows[model.ID]; already {
				t.Errorf("model %q is declared both by a vendor plugin and by runtime %q's own list; one row, one home", model.ID, declaration.ID)
			}
			rows[model.ID] = model
		}
	}
	return rows
}

// compareBoardFacts reports every disagreement between the board's table and
// what this module declares, in BOTH directions.
//
// It is a function rather than a test body so the same comparison can be driven
// over a DRIFTED fixture: TestTheBoardFactsPinFiresOnEveryPortedField narrows
// the gate by mutating one field at a time and requiring the exact
// disagreement back. Without that, a green pin would be equally consistent with
// a comparison that compares nothing.
func compareBoardFacts(fixture boardFacts, rows map[vendorplugin.ModelID]vendorplugin.Model) []string {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	remaining := map[vendorplugin.ModelID]bool{}
	for id := range rows {
		remaining[id] = true
	}

	for _, want := range fixture.Models {
		id := vendorplugin.ModelID(want.ID)
		got, declared := rows[id]
		if !declared {
			report("the board table holds row %q and this module declares no such model", want.ID)
			continue
		}
		delete(remaining, id)

		if got.Rank.Score != want.PolicyRank {
			report("model %q scores %d and the board scores it %d", want.ID, got.Rank.Score, want.PolicyRank)
		}
		if string(got.Lifecycle) != want.Lifecycle {
			report("model %q is %q and the board records %q", want.ID, got.Lifecycle, want.Lifecycle)
		}
		if string(got.SupersededBy) != want.SupersededBy {
			report("model %q names successor %q and the board names %q", want.ID, got.SupersededBy, want.SupersededBy)
		}
		if got.Recommended != want.Recommended {
			report("model %q is recommended=%v and the board records recommended=%v", want.ID, got.Recommended, want.Recommended)
		}
		if got.ContextWindowTokens != want.ContextWindowTokens {
			report("model %q declares a %d-token context window and the board declares %d", want.ID, got.ContextWindowTokens, want.ContextWindowTokens)
		}
		problems = append(problems, comparePricing(want.ID, got.Pricing, want.Pricing)...)

		// The muse rows' effort axis is the board's own declaration — no
		// vendor owns those rows — so it is a ported fact here and pinned.
		// Every other row's axis comes back from this module through the
		// projection and is deliberately not compared; see this file's header.
		if want.Joined.Broker == "" {
			problems = append(problems, compareUnresolvedEffort(want, got)...)
		}
	}

	leftover := make([]string, 0, len(remaining))
	for id := range remaining {
		leftover = append(leftover, string(id))
	}
	sort.Strings(leftover)
	for _, id := range leftover {
		report("this module declares model %q, which is not a row of the board table", id)
	}
	return problems
}

// compareUnresolvedEffort holds a vendor-unresolved row's effort axis to the
// board's own declaration of it.
//
// "No vendor owns this model" is not evidence that the model has no effort
// axis, so the axis is a stated fact on both sides and a disagreement about it
// is a port error like any other.
func compareUnresolvedEffort(want boardModel, got vendorplugin.Model) []string {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	wantSupport := agentic.EffortSupportNone
	if want.Joined.Reasoning == "required" {
		wantSupport = agentic.EffortSupportRequired
	}
	if got.Effort.Support != wantSupport {
		report("vendor-unresolved model %q declares effort support %s and the board records %q", want.ID, got.Effort.Support, want.Joined.Reasoning)
	}
	if !equalStrings(got.Effort.Vocabulary, want.Joined.SupportedEfforts) {
		report("vendor-unresolved model %q accepts %v and the board accepts %v", want.ID, got.Effort.Vocabulary, want.Joined.SupportedEfforts)
	}
	if got.Effort.Recommended != want.Joined.RecommendedEffort {
		report("vendor-unresolved model %q recommends %q and the board recommends %q", want.ID, got.Effort.Recommended, want.Joined.RecommendedEffort)
	}
	return problems
}

// comparePricing holds a billing contract field by field, plan by plan.
//
// Every field is compared rather than a digest of the whole struct, because a
// digest tells a reader that something moved and not what: a price, a credit
// allowance and a retrieval date are three different mistakes with three
// different fixes, and the one a transcription actually makes is a single digit.
func comparePricing(id string, got *vendorplugin.Pricing, want *boardPricing) []string {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	switch {
	case got == nil && want == nil:
		return nil
	case got == nil:
		report("model %q carries no billing contract and the board registers the %q contract for it", id, want.BillingModel)
		return problems
	case want == nil:
		report("model %q carries the %q billing contract and the board registers none for it; a price nobody published is one this module invented", id, got.BillingModel)
		return problems
	}

	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{"billing model", got.BillingModel, want.BillingModel},
		{"edition", got.Edition, want.Edition},
		{"currency", got.Currency, want.Currency},
		{"quota period", got.QuotaPeriod, want.QuotaPeriod},
		{"source URL", got.SourceURL, want.SourceURL},
		{"retrieval date", got.AsOf, want.AsOf},
	} {
		if field.got != field.want {
			report("model %q's contract states %s %q and the board states %q", id, field.name, field.got, field.want)
		}
	}
	if got.HasFrequencyLimits != want.HasFrequencyLimits {
		report("model %q's contract states frequency limits=%v and the board states %v", id, got.HasFrequencyLimits, want.HasFrequencyLimits)
	}
	if len(got.Plans) != len(want.Plans) {
		report("model %q's contract publishes %d plans and the board publishes %d", id, len(got.Plans), len(want.Plans))
		return problems
	}
	for i := range want.Plans {
		gotPlan, wantPlan := got.Plans[i], want.Plans[i]
		if gotPlan.Name != wantPlan.Name {
			report("model %q's plan %d is %q and the board's is %q", id, i, gotPlan.Name, wantPlan.Name)
			continue
		}
		if gotPlan.MonthlyUSD != wantPlan.MonthlyUSD {
			report("model %q's plan %q lists %v/month and the board lists %v", id, wantPlan.Name, gotPlan.MonthlyUSD, wantPlan.MonthlyUSD)
		}
		if gotPlan.MonthlyCredits != wantPlan.MonthlyCredits {
			report("model %q's plan %q publishes %d monthly credits and the board publishes %d", id, wantPlan.Name, gotPlan.MonthlyCredits, wantPlan.MonthlyCredits)
		}
		switch {
		case gotPlan.PromotionalMonthlyUSD == nil && wantPlan.PromotionalMonthlyUSD != nil:
			report("model %q's plan %q publishes no promotional price and the board publishes %v", id, wantPlan.Name, *wantPlan.PromotionalMonthlyUSD)
		case gotPlan.PromotionalMonthlyUSD != nil && wantPlan.PromotionalMonthlyUSD == nil:
			report("model %q's plan %q promotes %v and the board publishes no promotional price", id, wantPlan.Name, *gotPlan.PromotionalMonthlyUSD)
		case gotPlan.PromotionalMonthlyUSD != nil && *gotPlan.PromotionalMonthlyUSD != *wantPlan.PromotionalMonthlyUSD:
			report("model %q's plan %q promotes %v and the board promotes %v", id, wantPlan.Name, *gotPlan.PromotionalMonthlyUSD, *wantPlan.PromotionalMonthlyUSD)
		}
		gotIDs := make([]string, 0, len(gotPlan.ApplicableModelIDs))
		for _, applicable := range gotPlan.ApplicableModelIDs {
			gotIDs = append(gotIDs, string(applicable))
		}
		if !equalStrings(gotIDs, wantPlan.ApplicableModelIDs) {
			report("model %q's plan %q prices %v and the board prices %v", id, wantPlan.Name, gotIDs, wantPlan.ApplicableModelIDs)
		}
	}
	return problems
}

// TestEveryBoardFactIsPorted is the pin itself, over every row of the table.
func TestEveryBoardFactIsPorted(t *testing.T) {
	fixture := loadBoardFacts(t)
	if len(fixture.Models) != sourceModelCount {
		t.Fatalf("the board table has %d rows and this port pins %d; a changed count is a changed contract and has to be argued, not absorbed",
			len(fixture.Models), sourceModelCount)
	}
	for _, problem := range compareBoardFacts(fixture, portedRows(t)) {
		t.Error(problem)
	}
}

// TestTheBoardFactsPinFiresOnEveryPortedField narrows the gate rather than
// deleting it.
//
// There is one mutant per ported field, plus the two directions a row can go
// missing, because a pin is only worth what it can catch: a comparison that
// silently skipped Pricing.AsOf would stay green forever on a date this module
// made up. Each subtest drifts the BOARD fixture by exactly one value — the
// shape a real transcription slip takes — and requires the pin to name that
// exact row and field.
func TestTheBoardFactsPinFiresOnEveryPortedField(t *testing.T) {
	rows := portedRows(t)

	tests := []struct {
		name   string
		mutate func(*boardFacts)
		expect string
	}{
		{
			name:   "a capability score moved by one",
			mutate: func(f *boardFacts) { mutateBoardRow(f, "gpt-5.6-sol", func(m *boardModel) { m.PolicyRank = 121 }) },
			expect: `model "gpt-5.6-sol" scores 120 and the board scores it 121`,
		},
		{
			name: "a tie broken on the board side",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "claude-haiku-4-5-20251001", func(m *boardModel) { m.PolicyRank = 5 })
			},
			expect: `model "claude-haiku-4-5-20251001" scores 10 and the board scores it 5`,
		},
		{
			name: "a lifecycle swapped",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "gpt-5.3-codex", func(m *boardModel) { m.Lifecycle = "current" })
			},
			expect: `model "gpt-5.3-codex" is "legacy" and the board records "current"`,
		},
		{
			name: "a supersession moved to another successor",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "claude-opus-4-6", func(m *boardModel) { m.SupersededBy = "claude-fable-5" })
			},
			expect: `model "claude-opus-4-6" names successor "claude-opus-5" and the board names "claude-fable-5"`,
		},
		{
			name:   "a supersession dropped",
			mutate: func(f *boardFacts) { mutateBoardRow(f, "qwen3.6-plus", func(m *boardModel) { m.SupersededBy = "" }) },
			expect: `model "qwen3.6-plus" names successor "qwen3.7-plus" and the board names ""`,
		},
		{
			name:   "a display recommendation moved to another row",
			mutate: func(f *boardFacts) { mutateBoardRow(f, "gemini-2.5-pro", func(m *boardModel) { m.Recommended = true }) },
			expect: `model "gemini-2.5-pro" is recommended=false and the board records recommended=true`,
		},
		{
			name:   "a recommendation dropped",
			mutate: func(f *boardFacts) { mutateBoardRow(f, "claude-opus-5", func(m *boardModel) { m.Recommended = false }) },
			expect: `model "claude-opus-5" is recommended=true and the board records recommended=false`,
		},
		{
			name: "a context window off by a factor of a thousand",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "gemma-4-31b-it", func(m *boardModel) { m.ContextWindowTokens = 262 })
			},
			expect: `model "gemma-4-31b-it" declares a 262144-token context window and the board declares 262`,
		},
		{
			name: "a context window appearing where the board records none",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "gpt-5.5", func(m *boardModel) { m.ContextWindowTokens = 400_000 })
			},
			expect: `model "gpt-5.5" declares a 0-token context window and the board declares 400000`,
		},
		{
			name: "a monthly price off by a digit",
			mutate: func(f *boardFacts) {
				mutateBoardPlan(f, "qwen3.7-plus", "pro", func(p *boardPricingPlan) { p.MonthlyUSD = 1000 })
			},
			expect: `model "qwen3.7-plus"'s plan "pro" lists 100/month and the board lists 1000`,
		},
		{
			name: "a promotional price moved",
			mutate: func(f *boardFacts) {
				mutateBoardPlan(f, "qwen3.7-max", "standard", func(p *boardPricingPlan) { *p.PromotionalMonthlyUSD = 25 })
			},
			expect: `model "qwen3.7-max"'s plan "standard" promotes 20 and the board promotes 25`,
		},
		{
			name: "a promotional price dropped",
			mutate: func(f *boardFacts) {
				mutateBoardPlan(f, "qwen3.6-flash", "pro", func(p *boardPricingPlan) { p.PromotionalMonthlyUSD = nil })
			},
			expect: `model "qwen3.6-flash"'s plan "pro" promotes 75 and the board publishes no promotional price`,
		},
		{
			name: "a credit allowance moved",
			mutate: func(f *boardFacts) {
				mutateBoardPlan(f, "qwen3.6-plus", "max", func(p *boardPricingPlan) { p.MonthlyCredits = 25_000 })
			},
			expect: `model "qwen3.6-plus"'s plan "max" publishes 250000 monthly credits and the board publishes 25000`,
		},
		{
			name: "a plan allowlist narrowed by one model",
			mutate: func(f *boardFacts) {
				mutateBoardPlan(f, "qwen3.8-max-preview", "standard", func(p *boardPricingPlan) {
					p.ApplicableModelIDs = p.ApplicableModelIDs[:len(p.ApplicableModelIDs)-1]
				})
			},
			expect: `model "qwen3.8-max-preview"'s plan "standard" prices`,
		},
		{
			name: "a retrieval date moved",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "qwen3.7-plus", func(m *boardModel) { m.Pricing.AsOf = "2026-08-01" })
			},
			expect: `model "qwen3.7-plus"'s contract states retrieval date "2026-07-21" and the board states "2026-08-01"`,
		},
		{
			name: "a billing contract appearing where the board registers none",
			mutate: func(f *boardFacts) {
				var contract *boardPricing
				for _, row := range f.Models {
					if row.ID == "qwen3.7-plus" {
						contract = row.Pricing
					}
				}
				mutateBoardRow(f, "qwen3.7-plus-via-codex", func(m *boardModel) { m.Pricing = contract })
			},
			expect: `model "qwen3.7-plus-via-codex" carries no billing contract and the board registers the "token-plan-team-seat-credits" contract for it`,
		},
		{
			name: "a vendor-unresolved row's effort axis appearing",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "muse-spark-1.2-contributor", func(m *boardModel) {
					m.Joined.Reasoning = "required"
					m.Joined.SupportedEfforts = []string{"low", "high"}
					m.Joined.RecommendedEffort = "high"
				})
			},
			expect: `vendor-unresolved model "muse-spark-1.2-contributor" declares effort support none and the board records "required"`,
		},
		{
			// The converse, and it is the direction that got dangerous when
			// muse-spark-1.3-contributor gained a required axis. An axis
			// silently DISAPPEARING is what a fixture regenerated against a
			// board that had not shipped 1.3 yet would look like, and the row
			// would then launch at the harness default with nobody's
			// configured word reaching it.
			name: "a vendor-unresolved row's effort axis disappearing",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "muse-spark-1.3-contributor", func(m *boardModel) {
					m.Joined.Reasoning = "none"
					m.Joined.SupportedEfforts = nil
					m.Joined.RecommendedEffort = ""
				})
			},
			expect: `vendor-unresolved model "muse-spark-1.3-contributor" declares effort support required and the board records "none"`,
		},
		{
			// Presence is not the same fact as CONTENT. A vocabulary narrowed
			// to what the installed muse 1.0.2 CLI accepts — dropping `max`,
			// which that build has no word for — is the most plausible edit
			// anyone will make to this row, and it is a MODEL fact being
			// rewritten to match a harness build number.
			name: "a vendor-unresolved row's vocabulary narrowed to the installed CLI",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "muse-spark-1.3-contributor", func(m *boardModel) {
					m.Joined.SupportedEfforts = []string{"high", "xhigh"}
				})
			},
			expect: `vendor-unresolved model "muse-spark-1.3-contributor" accepts [high xhigh max] and the board accepts [high xhigh]`,
		},
		{
			// The alias and its identity must not drift apart: they are one
			// model reached by two names, so a recommendation moved on one
			// side alone is a launch that costs differently depending on how
			// it was spelled.
			name: "the alias's recommended effort moved off its identity",
			mutate: func(f *boardFacts) {
				mutateBoardRow(f, "muse-spark", func(m *boardModel) { m.Joined.RecommendedEffort = "max" })
			},
			expect: `vendor-unresolved model "muse-spark" recommends "high" and the board recommends "max"`,
		},
		{
			name: "a board row this module would then not cover",
			mutate: func(f *boardFacts) {
				f.Models = append(f.Models, boardModel{ID: "muse-spark-2", Runtime: "muse", PolicyRank: 20, Lifecycle: "current"})
			},
			expect: `the board table holds row "muse-spark-2" and this module declares no such model`,
		},
		{
			name: "a board row dropped, leaving this module declaring one too many",
			mutate: func(f *boardFacts) {
				kept := make([]boardModel, 0, len(f.Models))
				for _, row := range f.Models {
					if row.ID != "muse-spark-1.2-contributor" {
						kept = append(kept, row)
					}
				}
				f.Models = kept
			},
			expect: `this module declares model "muse-spark-1.2-contributor", which is not a row of the board table`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := loadBoardFacts(t)
			tt.mutate(&fixture)

			problems := compareBoardFacts(fixture, rows)
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

// mutateBoardRow drifts one row of a freshly loaded fixture.
//
// It deep-copies the row's mutable innards first, so a subtest cannot reach
// through a shared slice or pointer and drift a fixture another subtest is
// about to load. (Each subtest loads its own; the copy is what keeps that true
// rather than merely likely.)
func mutateBoardRow(fixture *boardFacts, id string, apply func(*boardModel)) {
	for i := range fixture.Models {
		if fixture.Models[i].ID != id {
			continue
		}
		row := &fixture.Models[i]
		row.Joined.SupportedEfforts = append([]string(nil), row.Joined.SupportedEfforts...)
		row.Joined.AgenticSystems = append([]string(nil), row.Joined.AgenticSystems...)
		if row.Pricing != nil {
			contract := *row.Pricing
			contract.Plans = append([]boardPricingPlan(nil), row.Pricing.Plans...)
			for j := range contract.Plans {
				contract.Plans[j].ApplicableModelIDs = append([]string(nil), contract.Plans[j].ApplicableModelIDs...)
				if contract.Plans[j].PromotionalMonthlyUSD != nil {
					promotional := *contract.Plans[j].PromotionalMonthlyUSD
					contract.Plans[j].PromotionalMonthlyUSD = &promotional
				}
			}
			row.Pricing = &contract
		}
		apply(row)
		return
	}
	panic("mutateBoardRow: no such row " + id)
}

func mutateBoardPlan(fixture *boardFacts, id, plan string, apply func(*boardPricingPlan)) {
	mutateBoardRow(fixture, id, func(row *boardModel) {
		if row.Pricing == nil {
			panic("mutateBoardPlan: row " + id + " carries no billing contract")
		}
		for i := range row.Pricing.Plans {
			if row.Pricing.Plans[i].Name == plan {
				apply(&row.Pricing.Plans[i])
				return
			}
		}
		panic("mutateBoardPlan: row " + id + " has no plan " + plan)
	})
}
