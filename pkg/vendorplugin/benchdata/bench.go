// Package benchdata owns the release-declared model and Bug Hunt Bench facts.
// Its accessors use only compiled data and never register or load a plugin.
package benchdata

import (
	"strconv"
	"strings"
)

const (
	// BugHuntBenchSource is the benchmark citation shared with adapter evidence.
	BugHuntBenchSource = "Bug Hunt Bench leaderboard, https://bughunt.productcompass.pm/ (updated 2026-09-13): planted bugs fixed out of 105, verified blind, unplanted fixes never counted"
	// BugHuntBenchTotal is a metric scale, not an interpolation sample count.
	BugHuntBenchTotal = 105
	// BugHuntBenchFloor names the bottom interpolation anchor.
	BugHuntBenchFloor = "the bench floor (0/105)"
	// BugHuntSourceVersion identifies the publication, independently of module tags.
	BugHuntSourceVersion = "bug-hunt-bench@2026-09-13"
)

// Cost preserves absent readings as nil and explicit zero readings as zero.
type Cost struct {
	TokensIn  *int64   `json:"tokens_in,omitempty"`
	TokensOut *int64   `json:"tokens_out,omitempty"`
	USD       *float64 `json:"usd,omitempty"`
	WallS     *float64 `json:"wall_s,omitempty"`
}

// BugHuntRow is one source claim. An empty Effort means unknown; Denominator
// is the fixed metric's scale for measured and interpolated rows, not a sample
// count. Cost rows have no denominator.
type BugHuntRow struct {
	Key           string  `json:"key"`
	ModelID       string  `json:"model_id"`
	Effort        string  `json:"effort,omitempty"`
	Kind          string  `json:"kind"`
	Value         float64 `json:"value"`
	Denominator   *int64  `json:"denominator,omitempty"`
	Cost          *Cost   `json:"cost,omitempty"`
	OriginRef     string  `json:"origin_ref"`
	SourceVersion string  `json:"source_version"`
}

// RegistryFact publishes model effort vocabulary; explicit none is retained.
type RegistryFact struct {
	ModelID string   `json:"model_id"`
	Efforts []string `json:"efforts"`
}

// BughuntMeasured constructs prose and typed evidence from independent inputs.
func BughuntMeasured(effort string, fixed int) RankEvidence {
	return measuredEvidence("", effort, fixed, false)
}

// BughuntMeasuredAs retains the leaderboard spelling used by an alias.
func BughuntMeasuredAs(benchID, effort string, fixed int) RankEvidence {
	return measuredEvidence(benchID, effort, fixed, true)
}

func measuredEvidence(benchID, effort string, fixed int, as bool) RankEvidence {
	observation := "fixed " + strconv.Itoa(fixed) + "/" + strconv.Itoa(BugHuntBenchTotal) + " at " + effort
	if as {
		observation += ", measured as " + benchID
	}
	return RankEvidence{Source: BugHuntBenchSource, Observation: observation, claim: &claim{kind: "measured", effort: effort, benchID: benchID, fixed: fixed}}
}

// BughuntInterpolated keeps effort unknown regardless of registry vocabulary.
func BughuntInterpolated(above, below, note string) RankEvidence {
	observation := "interpolated between " + above + " and " + below
	if strings.TrimSpace(note) != "" {
		observation += "; " + note
	}
	return RankEvidence{Source: BugHuntBenchSource, Observation: observation, claim: &claim{kind: "interpolated"}}
}

// bughuntCost keeps the numeric list cost separate from the capability score.
// The original prose remains independently parseable for parity checks.
func bughuntCost(effort string, usd float64, observation string) RankEvidence {
	return RankEvidence{Source: BugHuntBenchSource, Observation: "cost: " + observation, claim: &claim{kind: "cost", effort: effort, cost: &Cost{USD: &usd}}}
}

func allModels() []Model {
	out := OpenAIModels()
	out = append(out, AnthropicModels()...)
	out = append(out, AlibabaModels()...)
	out = append(out, GoogleModels()...)
	return append(out, MuseModels()...)
}

// BugHuntRows returns deep copies sorted lexicographically by Key. Keys retain
// the model id and zero-based index among that model's benchmark claims, so
// unrelated model additions cannot renumber existing keys. Alias measurements
// share the original result's OriginRef. No observation effort is inferred.
func BugHuntRows() []BugHuntRow {
	rows := []BugHuntRow{}
	for _, model := range allModels() {
		index := 0
		for _, evidence := range model.Rank.Basis {
			c := evidence.claim
			if c == nil {
				continue
			}
			row := BugHuntRow{Key: model.ID + ":" + strconv.Itoa(index), ModelID: model.ID, Kind: c.kind, Value: float64(model.Rank.Score), OriginRef: "bug-hunt:" + model.ID, SourceVersion: BugHuntSourceVersion}
			index++
			if c.kind != "cost" {
				denominator := int64(BugHuntBenchTotal)
				row.Denominator = &denominator
			}
			switch c.kind {
			case "measured":
				row.Value = float64(c.fixed)
				row.Effort = c.effort
				benchID := c.benchID
				if benchID == "" {
					benchID = model.ID
				}
				row.OriginRef = "bug-hunt:" + benchID + ":" + c.effort
			case "cost":
				row.Effort = c.effort
				row.Cost = cloneCost(c.cost)
				row.Value = *row.Cost.USD
				row.OriginRef += ":" + c.effort + ":cost"
			}
			rows = append(rows, row)
		}
	}
	// Insertion sort avoids adding a capability-bearing dependency for a small table.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Key < rows[j-1].Key; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	return rows
}

// RegistryFacts returns deep copies sorted lexicographically by ModelID;
// Efforts are also lexicographically sorted. Locally configured models are
// outside this compiled, release-declared registry.
func RegistryFacts() []RegistryFact {
	facts := []RegistryFact{}
	for _, model := range allModels() {
		efforts := append([]string(nil), model.Effort.Vocabulary...)
		if model.Effort.Support == EffortNone {
			efforts = []string{"none"}
		}
		for i := 1; i < len(efforts); i++ {
			for j := i; j > 0 && efforts[j] < efforts[j-1]; j-- {
				efforts[j], efforts[j-1] = efforts[j-1], efforts[j]
			}
		}
		facts = append(facts, RegistryFact{ModelID: model.ID, Efforts: efforts})
	}
	for i := 1; i < len(facts); i++ {
		for j := i; j > 0 && facts[j].ModelID < facts[j-1].ModelID; j-- {
			facts[j], facts[j-1] = facts[j-1], facts[j]
		}
	}
	return facts
}
