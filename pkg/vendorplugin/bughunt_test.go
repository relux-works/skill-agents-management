package vendorplugin_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file is the bench consistency gate: every capability score in scope is
// held against the Bug Hunt Bench claim its own evidence makes, and that claim
// is held against an INDEPENDENT transcription of the leaderboard written
// here rather than read from the vendor files.
//
// Three statements per measured row — the score, the observation's count and
// this table's count — are what let a slipped digit fail. A constructor that
// derived the observation from the score would agree with any score; a table
// read from the vendor files would agree with any vendor file. The vendor
// files type the score and the claim separately (rank(score, bench, ...)),
// and this table was typed from the owner's leaderboard capture on
// 2026-09-14, not from the code.
//
// # Scope
//
// The four ported vendors and the vendor-unresolved muse runtime — every row
// whose score was re-anchored on the bench. local-models is deliberately
// outside: its rows are operator-populated from a machine-local file, score 1
// as a tie the vendor's own evidence explains, and nothing on the leaderboard
// measures them. That is a stated bound of this gate, not an oversight.
//
// # What an anchor may be
//
// An interpolated row names the two rows it sits between. Each anchor must
// resolve to one of exactly three things: a row of the SAME home (vendor or
// runtime), a model the leaderboard measured whether or not any lineup carries
// it (google's whole lineup hangs below gemini-3.8-flash, which neither
// harness catalogues), or the named bench floor. A vendor's lowest row has
// nothing under it but the floor, and google's top row has nothing over it but
// a leaderboard-only model; both are real facts about the table, and an
// anchor rule that could not express them would push the rows into claiming a
// neighbour they do not have. Anything else — a misspelling, a model nobody
// measured, a row of another vendor — is refused.

// bughuntLeaderboard is the leaderboard as the owner captured it: model,
// effort setting, planted bugs fixed out of 105. Twenty runs of sixty were
// visible, sorted by fixed descending, and these are those twenty. A row that
// cites a (model, effort) pair outside this table is citing a run nobody saw.
var bughuntLeaderboard = map[string]map[string]int{
	"gpt-6-astra":         {"max": 48, "xhigh": 43},
	"claude-fable-5-1":    {"max": 43, "high": 33},
	"gpt-5.6-sol":         {"max": 42, "high": 34},
	"gpt-5.6-luna":        {"max": 33},
	"muse-spark-1.3":      {"max": 33, "high": 19},
	"qwen3.8-max":         {"max": 28},
	"grok-4.6":            {"xhigh": 27, "medium": 22},
	"claude-opus-5":       {"max": 27, "high": 21},
	"deepseek-v4.1-flash": {"max": 24},
	"kimi-k3":             {"default": 21},
	"gemini-3.8-flash":    {"high": 20},
	"glm-5.3-flash":       {"max": 19},
	"glm-5.3":             {"max": 19},
	"claude-opus-4-8":     {"max": 15},
}

// bughuntSpellings maps a row id to the spelling the leaderboard measured it
// under, for the rows whose own id is not on the board. It is a hand-written
// statement independent of the vendor files' "measured as" text, so a row
// that was measured under another name and claims interpolation instead is
// caught rather than skipped: the gate knows the leaderboard measured it.
//
// Alias rows are NOT listed. An alias resolves through AliasOf to its
// identity, and the identity's spelling (or this map's entry for it) is the
// alias's bench spelling.
var bughuntSpellings = map[vendorplugin.ModelID]string{
	"qwen3.8-max-preview":        "qwen3.8-max",
	"muse-spark-1.3-contributor": "muse-spark-1.3",
	// The agy row for the one google model the leaderboard measured, declared
	// on 2026-09-23; `gemini-flash` resolves here through its AliasOf.
	"gemini-3.8-flash-high": "gemini-3.8-flash",
}

// benchRow is one row in scope with the home that declares it.
type benchRow struct {
	home  string
	model vendorplugin.Model
}

// benchScope collects every row the gate holds, from both homes, keyed by
// home so an anchor can be required to name a row of the SAME lineup.
func benchScope(t *testing.T) map[string][]vendorplugin.Model {
	t.Helper()
	scope := map[string][]vendorplugin.Model{}
	for vendor := range portedVendors {
		plugin, registered := vendorplugin.Default.Lookup(vendor)
		if !registered {
			t.Fatalf("vendor %s is not registered", vendor)
		}
		scope[string(vendor)] = plugin.Models()
	}
	for _, declaration := range vendorplugin.Default.RuntimeDeclarations() {
		if len(declaration.Models) > 0 {
			scope["runtime "+string(declaration.ID)] = declaration.Models
		}
	}
	return scope
}

// benchSpelling is the leaderboard spelling for a row: its own id, its
// spelling entry, or — for an alias — the identity's. It returns "" when the
// leaderboard has no run of the model under any spelling this gate knows.
func benchSpelling(model vendorplugin.Model) string {
	id := model.ID
	if model.AliasOf != "" {
		id = model.AliasOf
	}
	spelling := string(id)
	if mapped, ok := bughuntSpellings[id]; ok {
		spelling = mapped
	}
	if _, measured := bughuntLeaderboard[spelling]; measured {
		return spelling
	}
	return ""
}

// bestSetting returns the leaderboard's best effort setting for a spelling
// and the count at it.
func bestSetting(spelling string) (effort string, fixed int) {
	for e, f := range bughuntLeaderboard[spelling] {
		if f > fixed || (f == fixed && e < effort) {
			effort, fixed = e, f
		}
	}
	return effort, fixed
}

// resolveAnchor turns an anchor name into the score it stands for, or reports
// that it stands for nothing this gate recognises.
func resolveAnchor(home []vendorplugin.Model, anchor string) (int, bool) {
	if anchor == vendorplugin.BugHuntBenchFloor {
		return 0, true
	}
	for _, row := range home {
		if string(row.ID) == anchor {
			return row.Rank.Score, true
		}
	}
	if _, measured := bughuntLeaderboard[anchor]; measured {
		_, best := bestSetting(anchor)
		return best, true
	}
	return 0, false
}

// benchProblems is the gate itself, returning disagreements for one row
// instead of failing, so the mutants below can drive the SAME rule over a
// reshaped row and require the exact report back.
func benchProblems(homeName string, home []vendorplugin.Model, model vendorplugin.Model) []string {
	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s row %q: "+format, append([]any{homeName, model.ID}, args...)...))
	}

	var claims []vendorplugin.BughuntClaim
	for _, evidence := range model.Rank.Basis {
		if evidence.Source != vendorplugin.BugHuntBenchSource {
			continue
		}
		claim, err := vendorplugin.ParseBughuntEvidence(evidence)
		if err != nil {
			report("carries a bench-sourced observation the grammar refuses: %v", err)
			continue
		}
		if claim.Kind != vendorplugin.BughuntCostClaim {
			claims = append(claims, claim)
		}
	}

	spelling := benchSpelling(model)
	switch len(claims) {
	case 0:
		if spelling != "" {
			report("carries no bench claim, and the leaderboard measured it as %q; a measured row must cite the bench", spelling)
		} else {
			report("carries no bench claim; every score in scope is a bench point and a row without the bench source is a number on this repository's say-so")
		}
		return problems
	case 1:
	default:
		report("carries %d bench claims and a score is one claim", len(claims))
		return problems
	}
	claim := claims[0]

	switch claim.Kind {
	case vendorplugin.BughuntMeasuredClaim:
		benchID := claim.BenchID
		if benchID == "" {
			benchID = string(model.ID)
		}
		if spelling == "" {
			report("claims the leaderboard measured it as %q and this gate knows no such run; a measurement nobody saw is an interpolation with the word left off", benchID)
			return problems
		}
		if benchID != spelling {
			report("claims it was measured as %q and the leaderboard spelling for it is %q", benchID, spelling)
			return problems
		}
		recorded, ran := bughuntLeaderboard[spelling][claim.Effort]
		if !ran {
			report("claims fixed %d/105 at %s and the leaderboard has no %s run of %q", claim.Fixed, claim.Effort, claim.Effort, spelling)
			return problems
		}
		if recorded != claim.Fixed {
			report("claims fixed %d/105 at %s and the leaderboard records %d for %q at %s", claim.Fixed, claim.Effort, recorded, spelling, claim.Effort)
		}
		if bestEffort, best := bestSetting(spelling); claim.Effort != bestEffort {
			report("cites the %s setting (%d) and the leaderboard's best setting for %q is %s (%d); the capability claim is the best setting, other settings are cost evidence", claim.Effort, claim.Fixed, spelling, bestEffort, best)
		}
		if model.Rank.Score != claim.Fixed {
			report("scores %d and its bench claim says fixed %d/105; the score is the bench value and these two were typed separately so that exactly this disagreement fails", model.Rank.Score, claim.Fixed)
		}
	case vendorplugin.BughuntInterpolatedClaim:
		if spelling != "" {
			report("claims interpolation and the leaderboard measured it as %q; a measured row carries the measured count", spelling)
			return problems
		}
		for _, anchor := range []string{claim.Above, claim.Below} {
			if anchor == string(model.ID) {
				report("names itself as an anchor")
				return problems
			}
		}
		above, aboveOK := resolveAnchor(home, claim.Above)
		below, belowOK := resolveAnchor(home, claim.Below)
		if !aboveOK {
			report("is interpolated from anchor %q, which is neither a row of this lineup, a model the leaderboard measured, nor the bench floor", claim.Above)
		}
		if !belowOK {
			report("is interpolated from anchor %q, which is neither a row of this lineup, a model the leaderboard measured, nor the bench floor", claim.Below)
		}
		if !aboveOK || !belowOK {
			return problems
		}
		if !(above > model.Rank.Score && model.Rank.Score > below) {
			report("scores %d and claims interpolation between %q (%d) and %q (%d); an interpolated value sits strictly inside its interval", model.Rank.Score, claim.Above, above, claim.Below, below)
		}
	}
	return problems
}

// TestEveryScoreInScopeIsABenchClaim is the gate over the real rows: every
// vendor row and every muse row carries exactly one bench claim, a measured
// claim agrees with the leaderboard and the score, and an interpolated claim
// names two resolvable anchors that bracket the score.
func TestEveryScoreInScopeIsABenchClaim(t *testing.T) {
	scope := benchScope(t)
	if len(scope) != len(portedVendors)+1 {
		t.Fatalf("the gate covers %d homes and expects the %d ported vendors plus the muse runtime; a home nobody checked could carry any number", len(scope), len(portedVendors)+1)
	}
	rows := 0
	for homeName, home := range scope {
		for _, model := range home {
			rows++
			for _, problem := range benchProblems(homeName, home, model) {
				t.Error(problem)
			}
		}
	}
	// The count is stated independently so the gate cannot pass on a scope
	// that quietly lost a home: 45 source rows plus the two declared-here
	// openai rows.
	if want := sourceModelCount + len(declaredHereRows) - len(retiredHereRows); rows != want {
		t.Errorf("the gate walked %d rows and the module declares %d in scope", rows, want)
	}
}

// TestTheBenchGateFiresOnEveryClassItRefuses narrows the gate one class at a
// time. Each mutant is a real row reshaped the way one slip would reshape it,
// and the gate must name that slip; a gate that stayed green on any of them
// would be equally consistent with a gate that checks nothing.
//
// The first three are the token-preserving mutants for a gate that reads
// text: the observation keeps "fixed 42/105 at max" — the exact string the
// gate searches — and the SCORE moves; or it keeps "interpolated between"
// with both anchors intact and the score leaves the interval. A checker that
// stopped at "the token is present" passes both.
func TestTheBenchGateFiresOnEveryClassItRefuses(t *testing.T) {
	scope := benchScope(t)
	find := func(home string, id vendorplugin.ModelID) vendorplugin.Model {
		for _, model := range scope[home] {
			if model.ID == id {
				return vendorplugin.CloneModels([]vendorplugin.Model{model})[0]
			}
		}
		t.Fatalf("%s declares no %q", home, id)
		return vendorplugin.Model{}
	}
	replaceBench := func(model vendorplugin.Model, replacement ...vendorplugin.RankEvidence) vendorplugin.Model {
		kept := make([]vendorplugin.RankEvidence, 0, len(model.Rank.Basis))
		for _, evidence := range model.Rank.Basis {
			if evidence.Source != vendorplugin.BugHuntBenchSource {
				kept = append(kept, evidence)
			}
		}
		model.Rank.Basis = append(kept, replacement...)
		return model
	}

	tests := []struct {
		name   string
		home   string
		mutate func() vendorplugin.Model
		expect string
	}{
		{
			name: "the observation keeps fixed 42/105 and the score moves to 41",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-sol")
				m.Rank.Score = 41
				return m
			},
			expect: `scores 41 and its bench claim says fixed 42/105`,
		},
		{
			name: "the score keeps 42 and the observation says fixed 41/105",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-sol")
				return replaceBench(m, vendorplugin.BughuntMeasured("max", 41))
			},
			expect: `claims fixed 41/105 at max and the leaderboard records 42 for "gpt-5.6-sol" at max`,
		},
		{
			name: "the anchors stay and the interpolated score leaves the interval",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-terra")
				m.Rank.Score = 45
				return m
			},
			expect: `scores 45 and claims interpolation between "gpt-5.6-sol" (42) and "gpt-5.6-luna" (33)`,
		},
		{
			name: "an interpolated row names an anchor nobody declares or measured",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-terra")
				return replaceBench(m, vendorplugin.BughuntInterpolated("gpt-7-nobody-published-this", "gpt-5.6-luna", ""))
			},
			expect: `is interpolated from anchor "gpt-7-nobody-published-this", which is neither a row of this lineup`,
		},
		{
			name: "an interpolated row names another vendor's row as an anchor",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-terra")
				return replaceBench(m, vendorplugin.BughuntInterpolated("gpt-5.6-sol", "claude-sonnet-5", ""))
			},
			expect: `is interpolated from anchor "claude-sonnet-5", which is neither a row of this lineup`,
		},
		{
			name: "a measured row drops the bench source",
			home: "openai",
			mutate: func() vendorplugin.Model {
				return replaceBench(find("openai", "gpt-5.6-sol"))
			},
			expect: `carries no bench claim, and the leaderboard measured it as "gpt-5.6-sol"`,
		},
		{
			name: "an interpolated row drops the bench source",
			home: "openai",
			mutate: func() vendorplugin.Model {
				return replaceBench(find("openai", "gpt-5.6-terra"))
			},
			expect: `carries no bench claim; every score in scope is a bench point`,
		},
		{
			name: "a measured row keeps only its cost evidence",
			home: "openai",
			mutate: func() vendorplugin.Model {
				return replaceBench(find("openai", "gpt-5.6-luna"), vendorplugin.BughuntCost("at max this row fixed 33/105 for $1.80"))
			},
			expect: `carries no bench claim, and the leaderboard measured it as "gpt-5.6-luna"`,
		},
		{
			name: "a measured row claims interpolation instead",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-luna")
				return replaceBench(m, vendorplugin.BughuntInterpolated("gpt-5.6-terra", "gpt-5.5", ""))
			},
			expect: `claims interpolation and the leaderboard measured it as "gpt-5.6-luna"`,
		},
		{
			name: "a row measured under another spelling claims interpolation",
			home: "alibaba",
			mutate: func() vendorplugin.Model {
				m := find("alibaba", "qwen3.8-max-preview")
				return replaceBench(m, vendorplugin.BughuntInterpolated("qwen3.7-max", vendorplugin.BugHuntBenchFloor, ""))
			},
			expect: `claims interpolation and the leaderboard measured it as "qwen3.8-max"`,
		},
		{
			name: "an alias claims interpolation while its identity was measured",
			home: "runtime muse",
			mutate: func() vendorplugin.Model {
				m := find("runtime muse", "muse-spark")
				return replaceBench(m, vendorplugin.BughuntInterpolated("muse-spark-1.3-contributor", vendorplugin.BugHuntBenchFloor, ""))
			},
			expect: `claims interpolation and the leaderboard measured it as "muse-spark-1.3"`,
		},
		{
			name: "a measured row cites a setting that is not its best",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-6-astra")
				m.Rank.Score = 43
				return replaceBench(m, vendorplugin.BughuntMeasured("xhigh", 43))
			},
			expect: `cites the xhigh setting (43) and the leaderboard's best setting for "gpt-6-astra" is max (48)`,
		},
		{
			name: "a measured row cites a run the leaderboard does not have",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-luna")
				return replaceBench(m, vendorplugin.BughuntMeasured("high", 33))
			},
			expect: `claims fixed 33/105 at high and the leaderboard has no high run of "gpt-5.6-luna"`,
		},
		{
			name: "a row claims a measurement of a model nobody measured",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-terra")
				return replaceBench(m, vendorplugin.BughuntMeasured("max", 38))
			},
			expect: `claims the leaderboard measured it as "gpt-5.6-terra" and this gate knows no such run`,
		},
		{
			name: "a hand-typed observation that looks like a claim",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-sol")
				return replaceBench(m, vendorplugin.RankEvidence{Source: vendorplugin.BugHuntBenchSource, Observation: "fixed about 42/105 at max"})
			},
			expect: `carries a bench-sourced observation the grammar refuses`,
		},
		{
			name: "a measured claim counted out of the wrong total",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-sol")
				return replaceBench(m, vendorplugin.RankEvidence{Source: vendorplugin.BugHuntBenchSource, Observation: "fixed 42/100 at max"})
			},
			expect: `counts out of 100 and the bench plants 105`,
		},
		{
			name: "two bench claims on one row",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-sol")
				m.Rank.Basis = append(m.Rank.Basis, vendorplugin.BughuntMeasured("max", 42))
				return m
			},
			expect: `carries 2 bench claims and a score is one claim`,
		},
		{
			// The interval is OPEN: a score equal to an anchor is not between
			// two rows, it is a tie with one of them that the evidence does
			// not declare. A gate that read >= would admit this.
			name: "the interpolated score lands exactly on its upper anchor",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-terra")
				m.Rank.Score = 42
				return m
			},
			expect: `scores 42 and claims interpolation between "gpt-5.6-sol" (42) and "gpt-5.6-luna" (33)`,
		},
		{
			name: "an interpolated row anchored on itself",
			home: "openai",
			mutate: func() vendorplugin.Model {
				m := find("openai", "gpt-5.6-terra")
				return replaceBench(m, vendorplugin.BughuntInterpolated("gpt-5.6-terra", "gpt-5.6-luna", ""))
			},
			expect: `names itself as an anchor`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := benchProblems(tt.home, scope[tt.home], tt.mutate())
			if len(problems) == 0 {
				t.Fatalf("the gate admitted the mutant; it must report %q", tt.expect)
			}
			requireReport(t, problems, tt.expect)
		})
	}
}

// TestTheBenchGateAdmitsTheRealRowsAndOnlyThem is the positive control beside
// the mutants: a gate that refused everything would pass every subtest above,
// so one measured and one interpolated row are shown admitted unchanged, and
// a second setting of a measured model recorded as COST is shown admitted
// beside the claim rather than counted as a second claim.
func TestTheBenchGateAdmitsTheRealRowsAndOnlyThem(t *testing.T) {
	scope := benchScope(t)
	for _, id := range []vendorplugin.ModelID{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "astra"} {
		for _, model := range scope["openai"] {
			if model.ID != id {
				continue
			}
			if problems := benchProblems("openai", scope["openai"], model); len(problems) != 0 {
				t.Errorf("the gate refused the real row %q: %v", id, problems)
			}
		}
	}
}

// TestTheLineupOrderIsTheBenchOrder asserts each vendor's derived order from
// the rows' own bench claims rather than from a hand-written list: two
// measured rows must order as the leaderboard counted them, and every row
// must sit at or below the row before it. The interval half — an interpolated
// row between its anchors — is the gate above; this is the ORDER those
// intervals imply, read off LineupOf, the production order every consumer
// derives from.
func TestTheLineupOrderIsTheBenchOrder(t *testing.T) {
	scope := benchScope(t)
	for homeName, home := range scope {
		for _, problem := range lineupOrderProblems(homeName, home) {
			t.Error(problem)
		}
	}

	// The narrowing mutant: a measured row re-scored under a lower measured
	// row keeps every bench claim intact and moves only the ORDER, which
	// the gate above does not read (it reads each row alone) and this one
	// must. claude-fable-5-1 (measured 43) is dropped to 20 and now lists
	// below claude-opus-5 (measured 27).
	t.Run("a measured row listed below a lower-counted one", func(t *testing.T) {
		home := vendorplugin.CloneModels(scope["anthropic"])
		for i := range home {
			if home[i].ID == "claude-fable-5-1" {
				home[i].Rank.Score = 20
			}
		}
		requireReport(t, lineupOrderProblems("anthropic", home),
			`anthropic: "claude-opus-5" is listed above "claude-fable-5-1" and the leaderboard counted 27 against 43`)
	})
}

// lineupOrderProblems reads a home's derived order and reports every
// adjacent pair the leaderboard would order the other way.
func lineupOrderProblems(homeName string, home []vendorplugin.Model) []string {
	var problems []string
	ranked := vendorplugin.Lineup(home)
	for i := 1; i < len(ranked); i++ {
		prev, cur := ranked[i-1].Model, ranked[i].Model
		if prev.Rank.Score < cur.Rank.Score {
			problems = append(problems, fmt.Sprintf("%s: %q (%d) is listed above %q (%d)", homeName, prev.ID, prev.Rank.Score, cur.ID, cur.Rank.Score))
		}
		prevSpelling, curSpelling := benchSpelling(prev), benchSpelling(cur)
		if prevSpelling == "" || curSpelling == "" {
			continue
		}
		_, prevBest := bestSetting(prevSpelling)
		_, curBest := bestSetting(curSpelling)
		if prevBest < curBest {
			problems = append(problems, fmt.Sprintf("%s: %q is listed above %q and the leaderboard counted %d against %d", homeName, prev.ID, cur.ID, prevBest, curBest))
		}
	}
	return problems
}

// TestMeasuredRowsCompareAcrossVendorsThroughTheBench is the cross-vendor
// claim the contract now makes, held to the leaderboard: every measured row
// in scope, from every home, sorted by score, is the leaderboard's own order
// of those models. The named pair is the one the Story calls out — openai's
// gpt-6-astra (48) above anthropic's claude-fable-5-1 (43) — and it is the
// bench saying so, not this module.
func TestMeasuredRowsCompareAcrossVendorsThroughTheBench(t *testing.T) {
	scope := benchScope(t)
	var measured []benchRow
	byID := map[vendorplugin.ModelID]vendorplugin.Model{}
	for homeName, home := range scope {
		for _, model := range home {
			byID[model.ID] = model
			if benchSpelling(model) != "" {
				measured = append(measured, benchRow{home: homeName, model: model})
			}
		}
	}
	if len(measured) < 2 {
		t.Fatalf("only %d measured rows in scope; nothing to compare", len(measured))
	}
	sort.SliceStable(measured, func(i, j int) bool { return measured[i].model.Rank.Score > measured[j].model.Rank.Score })
	for i := 1; i < len(measured); i++ {
		a, b := measured[i-1], measured[i]
		_, aBest := bestSetting(benchSpelling(a.model))
		_, bBest := bestSetting(benchSpelling(b.model))
		if aBest < bBest {
			t.Errorf("%s %q (%d) sorts above %s %q (%d) and the leaderboard counted %d against %d",
				a.home, a.model.ID, a.model.Rank.Score, b.home, b.model.ID, b.model.Rank.Score, aBest, bBest)
		}
	}

	astra, fable := byID["gpt-6-astra"], byID["claude-fable-5-1"]
	if !(astra.Rank.Score > fable.Rank.Score) {
		t.Errorf("gpt-6-astra scores %d and claude-fable-5-1 scores %d; the leaderboard counted 48 against 43", astra.Rank.Score, fable.Rank.Score)
	}
	if astra.Rank.Score != 48 || fable.Rank.Score != 43 {
		t.Errorf("gpt-6-astra scores %d and claude-fable-5-1 %d; the bench values are 48 and 43", astra.Rank.Score, fable.Rank.Score)
	}
}

// TestLunaCarriesTheCostObservation holds the owner's observation to the row
// it was recorded on: gpt-5.6-luna cites, from the bench source, the equal
// count to gpt-5.6-sol at high for a nineteenth of the cost — as COST
// evidence, beside a measured claim of 33, with sol's capability standing
// above luna's unchanged.
func TestLunaCarriesTheCostObservation(t *testing.T) {
	scope := benchScope(t)
	var luna, sol vendorplugin.Model
	for _, model := range scope["openai"] {
		switch model.ID {
		case "gpt-5.6-luna":
			luna = model
		case "gpt-5.6-sol":
			sol = model
		}
	}
	cost := ""
	for _, evidence := range luna.Rank.Basis {
		claim, err := vendorplugin.ParseBughuntEvidence(evidence)
		if err == nil && claim.Kind == vendorplugin.BughuntCostClaim {
			cost = evidence.Observation
		}
	}
	if cost == "" {
		t.Fatal("gpt-5.6-luna carries no cost observation from the bench source")
	}
	for _, needle := range []string{"33/105", "$1.80", "gpt-5.6-sol", "34/105", "$33.92", "1/19"} {
		if !strings.Contains(cost, needle) {
			t.Errorf("the cost observation does not record %q: %q", needle, cost)
		}
	}
	if !(sol.Rank.Score > luna.Rank.Score) {
		t.Errorf("sol scores %d and luna %d; the cost observation must not move the capability order", sol.Rank.Score, luna.Rank.Score)
	}
}

// TestTheBenchGrammarRoundTrips holds the parser to the constructors, in both
// directions: every constructor's output parses back to the parts it was
// built from, and an observation from any other source is not a bench claim.
func TestTheBenchGrammarRoundTrips(t *testing.T) {
	cases := map[string]struct {
		evidence vendorplugin.RankEvidence
		want     vendorplugin.BughuntClaim
	}{
		"measured":       {vendorplugin.BughuntMeasured("max", 42), vendorplugin.BughuntClaim{Kind: vendorplugin.BughuntMeasuredClaim, Fixed: 42, Effort: "max"}},
		"measured as":    {vendorplugin.BughuntMeasuredAs("qwen3.8-max", "max", 28), vendorplugin.BughuntClaim{Kind: vendorplugin.BughuntMeasuredClaim, Fixed: 28, Effort: "max", BenchID: "qwen3.8-max"}},
		"interpolated":   {vendorplugin.BughuntInterpolated("a", "b", "kept order"), vendorplugin.BughuntClaim{Kind: vendorplugin.BughuntInterpolatedClaim, Above: "a", Below: "b"}},
		"to the floor":   {vendorplugin.BughuntInterpolated("gpt-5.2", vendorplugin.BugHuntBenchFloor, ""), vendorplugin.BughuntClaim{Kind: vendorplugin.BughuntInterpolatedClaim, Above: "gpt-5.2", Below: vendorplugin.BugHuntBenchFloor}},
		"from the bench": {vendorplugin.BughuntInterpolated("gemini-3.8-flash", "gemini-3.1-pro-low", "below the measured row"), vendorplugin.BughuntClaim{Kind: vendorplugin.BughuntInterpolatedClaim, Above: "gemini-3.8-flash", Below: "gemini-3.1-pro-low"}},
		"cost":           {vendorplugin.BughuntCost("cheap"), vendorplugin.BughuntClaim{Kind: vendorplugin.BughuntCostClaim}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := vendorplugin.ParseBughuntEvidence(tc.evidence)
			if err != nil {
				t.Fatalf("%q did not parse: %v", tc.evidence.Observation, err)
			}
			if got != tc.want {
				t.Errorf("%q parsed to %+v, want %+v", tc.evidence.Observation, got, tc.want)
			}
		})
	}
	t.Run("another source is not a bench claim", func(t *testing.T) {
		_, err := vendorplugin.ParseBughuntEvidence(vendorplugin.RankEvidence{Source: "somebody's recollection", Observation: "fixed 42/105 at max"})
		if err == nil {
			t.Fatal("an observation in the bench grammar from a non-bench source parsed as a bench claim")
		}
	})
}
