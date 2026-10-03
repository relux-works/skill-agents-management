package vendorplugin_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/benchdata"
)

// This independent projection parses the adapters' public evidence grammar,
// rather than consulting benchdata's typed claims or its private constructors.
func adapterBenchProjection(t *testing.T) ([]benchdata.BugHuntRow, []benchdata.RegistryFact) {
	t.Helper()
	var models []vendorplugin.Model
	for _, id := range []vendorplugin.VendorID{"openai", "anthropic", "alibaba", "google"} {
		vendor, ok := vendorplugin.Default.Lookup(id)
		if !ok {
			t.Fatalf("missing adapter %s", id)
		}
		models = append(models, vendor.Models()...)
	}
	for _, runtime := range vendorplugin.FrozenRuntimes() {
		models = append(models, runtime.Models...)
	}
	costPattern := regexp.MustCompile(`at ([a-z]+) this row fixed [0-9]+/105 for \$([0-9.]+) list cost`)
	rows := []benchdata.BugHuntRow{}
	facts := []benchdata.RegistryFact{}
	for _, model := range models {
		efforts := append([]string(nil), model.Effort.Vocabulary...)
		if model.Effort.Support == agentic.EffortSupportNone {
			efforts = []string{"none"}
		}
		sort.Strings(efforts)
		facts = append(facts, benchdata.RegistryFact{ModelID: string(model.ID), Efforts: efforts})
		index := 0
		for _, evidence := range model.Rank.Basis {
			if evidence.Source != vendorplugin.BugHuntBenchSource {
				continue
			}
			parsed, err := vendorplugin.ParseBughuntEvidence(evidence)
			if err != nil {
				t.Fatal(err)
			}
			row := benchdata.BugHuntRow{Key: fmt.Sprintf("%s:%d", model.ID, index), ModelID: string(model.ID), Kind: string(parsed.Kind), Value: float64(model.Rank.Score), OriginRef: "bug-hunt:" + string(model.ID), SourceVersion: "bug-hunt-bench@2026-09-13"}
			index++
			switch parsed.Kind {
			case vendorplugin.BughuntMeasuredClaim:
				denominator := int64(105)
				row.Denominator = &denominator
				row.Value, row.Effort = float64(parsed.Fixed), parsed.Effort
				benchID := parsed.BenchID
				if benchID == "" {
					benchID = string(model.ID)
				}
				row.OriginRef = "bug-hunt:" + benchID + ":" + parsed.Effort
			case vendorplugin.BughuntInterpolatedClaim:
				denominator := int64(105)
				row.Denominator = &denominator
			case vendorplugin.BughuntCostClaim:
				match := costPattern.FindStringSubmatch(evidence.Observation)
				if match == nil {
					t.Fatalf("unrecognized cost prose on %s", model.ID)
				}
				usd, err := strconv.ParseFloat(match[2], 64)
				if err != nil {
					t.Fatal(err)
				}
				row.Effort, row.Value, row.Cost = match[1], usd, &benchdata.Cost{USD: &usd}
				row.OriginRef += ":" + row.Effort + ":cost"
			default:
				t.Fatalf("unhandled claim on %s", model.ID)
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	sort.Slice(facts, func(i, j int) bool { return facts[i].ModelID < facts[j].ModelID })
	return rows, facts
}

func compareBenchRows(got, want []benchdata.BugHuntRow) error {
	if len(got) != len(want) {
		return fmt.Errorf("row count: got %d, want %d", len(got), len(want))
	}
	byKey := map[string]benchdata.BugHuntRow{}
	for _, row := range got {
		if _, duplicate := byKey[row.Key]; duplicate {
			return fmt.Errorf("duplicate key %s", row.Key)
		}
		byKey[row.Key] = row
	}
	seen := map[string]bool{}
	for _, expected := range want {
		if seen[expected.Key] {
			return fmt.Errorf("duplicate expected key %s", expected.Key)
		}
		seen[expected.Key] = true
		actual, ok := byKey[expected.Key]
		if !ok {
			return fmt.Errorf("missing key %s", expected.Key)
		}
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("row %s differs: got %+v, want %+v", expected.Key, actual, expected)
		}
	}
	return nil
}

func TestBenchdataParityWithAdaptersAndPinnedExport(t *testing.T) {
	rows, facts := benchdata.BugHuntRows(), benchdata.RegistryFacts()
	adapterRows, adapterFacts := adapterBenchProjection(t)
	if err := compareBenchRows(rows, adapterRows); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(facts, adapterFacts) {
		t.Fatal("adapter effort vocabulary drift")
	}
	// Historical v0.5.32 consumer export: independent of the moved declarations.
	raw, err := os.ReadFile("testdata/bughunt-export.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Models []benchdata.RegistryFact `json:"models"`
		Rows   []struct {
			Key       string          `json:"key"`
			ModelID   string          `json:"model_name"`
			Effort    string          `json:"effort"`
			Kind      string          `json:"kind"`
			Value     float64         `json:"value"`
			Cost      *benchdata.Cost `json:"cost"`
			OriginRef string          `json:"origin_ref"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	historical := []benchdata.BugHuntRow{}
	for _, r := range fixture.Rows {
		row := benchdata.BugHuntRow{Key: r.Key, ModelID: r.ModelID, Effort: r.Effort, Kind: r.Kind, Value: r.Value, Cost: r.Cost, OriginRef: r.OriginRef, SourceVersion: "bug-hunt-bench@2026-09-13"}
		if row.Kind != "cost" {
			denominator := int64(105)
			row.Denominator = &denominator
		}
		historical = append(historical, row)
	}
	// The frozen export predates exactly the two Argon declarations. Pin their
	// additions independently; do not rewrite the historical capture.
	denominator := int64(105)
	for _, id := range []string{"argon", "gemini-4-argon"} {
		historical = append(historical, benchdata.BugHuntRow{
			Key: id + ":0", ModelID: id, Kind: "interpolated", Value: 45,
			Denominator: &denominator, OriginRef: "bug-hunt:" + id,
			SourceVersion: "bug-hunt-bench@2026-09-13",
		})
		fixture.Models = append(fixture.Models, benchdata.RegistryFact{ModelID: id, Efforts: []string{"none"}})
	}
	sort.Slice(fixture.Models, func(i, j int) bool { return fixture.Models[i].ModelID < fixture.Models[j].ModelID })
	if err := compareBenchRows(rows, historical); err != nil {
		t.Fatalf("historical export parity: %v", err)
	}
	if !reflect.DeepEqual(facts, fixture.Models) {
		t.Fatal("historical registry drift")
	}
	counts := map[string]int{}
	keys := map[string]bool{}
	for _, row := range rows {
		counts[row.Kind]++
		keys[row.Key] = true
	}
	if len(rows) != 60 || len(keys) != 60 || counts["measured"] != 12 || counts["interpolated"] != 47 || counts["cost"] != 1 {
		t.Fatalf("claim counts: %d rows, %d keys, %v", len(rows), len(keys), counts)
	}
	if len(facts) != 59 {
		t.Fatalf("registry count: %d", len(facts))
	}
	// Measured aliases retain shared source origins and remain separate keys.
	origins := map[string]string{}
	for _, row := range rows {
		origins[row.Key] = row.OriginRef
	}
	if origins["astra:0"] != origins["gpt-6-astra:0"] || origins["muse-spark:0"] != origins["muse-spark-1.3-contributor:0"] {
		t.Fatal("alias source origin drift")
	}
	t.Log("parity: 60 distinct keys; 12 measured / 47 interpolated / 1 cost; 59 registry facts; adapters and historical fixture plus exactly two Argon rows agree")
}

func TestBenchdataParityRejectsDropDuplicateAndFieldDrift(t *testing.T) {
	want := benchdata.BugHuntRows()
	got := benchdata.BugHuntRows()
	got[0] = got[1]
	if err := compareBenchRows(got, want); err == nil {
		t.Fatal("equal-count drop plus duplicate admitted")
	}
	got = benchdata.BugHuntRows()
	got[0].Key = "new:0"
	if err := compareBenchRows(got, want); err == nil {
		t.Fatal("key set drift admitted")
	}
	for _, mutate := range []func(*benchdata.BugHuntRow){
		func(r *benchdata.BugHuntRow) { r.ModelID = "other" },
		func(r *benchdata.BugHuntRow) { r.Effort = "other" },
		func(r *benchdata.BugHuntRow) { r.Kind = "forged" },
		func(r *benchdata.BugHuntRow) { r.Value++ },
		func(r *benchdata.BugHuntRow) { r.Denominator = nil },
		func(r *benchdata.BugHuntRow) { *r.Denominator = 104 },
		func(r *benchdata.BugHuntRow) { r.Cost = &benchdata.Cost{} },
		func(r *benchdata.BugHuntRow) { r.OriginRef = "other" },
		func(r *benchdata.BugHuntRow) { r.SourceVersion = "other" },
	} {
		got = benchdata.BugHuntRows()
		mutate(&got[0])
		if err := compareBenchRows(got, want); err == nil {
			t.Fatal("field drift admitted")
		}
	}
	got = benchdata.BugHuntRows()
	for i := range got {
		if got[i].Kind == "cost" {
			*got[i].Cost.USD = 0
			break
		}
	}
	if err := compareBenchRows(got, want); err == nil {
		t.Fatal("cost drift admitted")
	}
}
