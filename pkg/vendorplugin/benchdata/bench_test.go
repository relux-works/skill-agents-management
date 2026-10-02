package benchdata

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

func TestAccessorsReturnIndependentCopies(t *testing.T) {
	wantRows, wantFacts := BugHuntRows(), RegistryFacts()
	beforeRows, _ := json.Marshal(wantRows)
	beforeFacts, _ := json.Marshal(wantFacts)
	rows, facts := BugHuntRows(), RegistryFacts()
	for i := range rows {
		rows[i].Key, rows[i].ModelID, rows[i].Effort, rows[i].Kind = "mutated", "mutated", "mutated", "mutated"
		rows[i].Value, rows[i].OriginRef, rows[i].SourceVersion = -1, "mutated", "mutated"
		if rows[i].Denominator != nil {
			*rows[i].Denominator = -1
		}
		if rows[i].Cost != nil {
			*rows[i].Cost.USD = -1
			rows[i].Cost.TokensIn = new(int64)
		}
	}
	for i := range facts {
		facts[i].ModelID = "mutated"
		for j := range facts[i].Efforts {
			facts[i].Efforts[j] = "mutated"
		}
	}
	afterRows, _ := json.Marshal(BugHuntRows())
	afterFacts, _ := json.Marshal(RegistryFacts())
	if string(afterRows) != string(beforeRows) || string(afterFacts) != string(beforeFacts) {
		t.Fatal("mutation changed later accessor results")
	}
	// Two already-returned values must not share storage either.
	earlierRows, _ := json.Marshal(wantRows)
	earlierFacts, _ := json.Marshal(wantFacts)
	if string(earlierRows) != string(beforeRows) || string(earlierFacts) != string(beforeFacts) {
		t.Fatal("mutation changed earlier results")
	}
}

func TestConcurrentAccessors(t *testing.T) {
	var wantRows []BugHuntRow
	var wantFacts []RegistryFact
	rawRows, _ := json.Marshal(BugHuntRows())
	rawFacts, _ := json.Marshal(RegistryFacts())
	if err := json.Unmarshal(rawRows, &wantRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawFacts, &wantFacts); err != nil {
		t.Fatal(err)
	}
	var callers sync.WaitGroup
	for i := 0; i < 16; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for n := 0; n < 20; n++ {
				rows, facts := BugHuntRows(), RegistryFacts()
				if !reflect.DeepEqual(rows, wantRows) || !reflect.DeepEqual(facts, wantFacts) {
					t.Error("concurrent access changed data")
					return
				}
				for j := range rows {
					if rows[j].Denominator != nil {
						*rows[j].Denominator = -1
					}
					if rows[j].Cost != nil {
						*rows[j].Cost.USD = -1
					}
				}
				facts[0].Efforts[0] = "mutated"
			}
		}()
	}
	callers.Wait()
}

func TestEveryNestedCostPointerIsCopied(t *testing.T) {
	tokensIn, tokensOut := int64(0), int64(12)
	usd, wall := 0.0, 3.0
	original := &Cost{TokensIn: &tokensIn, TokensOut: &tokensOut, USD: &usd, WallS: &wall}
	copy := cloneCost(original)
	*copy.TokensIn, *copy.TokensOut, *copy.USD, *copy.WallS = 1, 2, 3, 4
	if tokensIn != 0 || tokensOut != 12 || usd != 0 || wall != 3 {
		t.Fatal("nested pointers shared")
	}
	if cloneCost(nil) != nil {
		t.Fatal("missing cost became present")
	}
	empty := cloneCost(&Cost{})
	if empty.TokensIn != nil || empty.TokensOut != nil || empty.USD != nil || empty.WallS != nil {
		t.Fatal("missing cost fields became present")
	}
	zero := bughuntCost("max", 0, "explicit zero")
	if zero.claim.cost.USD == nil || *zero.claim.cost.USD != 0 {
		t.Fatal("zero cost lost")
	}
}

func TestModelDeclarationsAreIndependentCopies(t *testing.T) {
	for _, accessor := range []func() []Model{OpenAIModels, AnthropicModels, AlibabaModels, GoogleModels, MuseModels} {
		want, models := accessor(), accessor()
		beforeModels, _ := json.Marshal(want)
		beforeRows, _ := json.Marshal(BugHuntRows())
		for i := range models {
			m := &models[i]
			m.ID, m.AliasOf, m.Rank.Score = "mutated", "mutated", -1
			m.Systems[0] = "mutated"
			for j := range m.Effort.Vocabulary {
				m.Effort.Vocabulary[j] = "mutated"
			}
			for j := range m.Rank.Basis {
				m.Rank.Basis[j].Observation = "mutated"
				if c := m.Rank.Basis[j].claim; c != nil {
					c.kind = "mutated"
					if c.cost != nil {
						*c.cost.USD = -1
					}
				}
			}
			if m.Pricing != nil {
				m.Pricing.Currency = "mutated"
				for j := range m.Pricing.Plans {
					p := &m.Pricing.Plans[j]
					p.Name, p.ApplicableModelIDs[0] = "mutated", "mutated"
					if p.PromotionalMonthlyUSD != nil {
						*p.PromotionalMonthlyUSD = -1
					}
				}
			}
		}
		afterModels, _ := json.Marshal(accessor())
		afterRows, _ := json.Marshal(BugHuntRows())
		if string(afterModels) != string(beforeModels) || string(afterRows) != string(beforeRows) {
			t.Fatal("model projection shares mutable declarations")
		}
	}
}

func TestJSONContractAndDeterministicOrdering(t *testing.T) {
	rows, facts := BugHuntRows(), RegistryFacts()
	for i, row := range rows {
		if i > 0 && rows[i-1].Key >= row.Key {
			t.Fatal("keys not strictly sorted")
		}
		if row.SourceVersion != "bug-hunt-bench@2026-09-13" {
			t.Fatal("source version drift")
		}
		if row.Kind == "cost" {
			if row.Denominator != nil || row.Cost == nil || row.Cost.USD == nil || row.Value != *row.Cost.USD {
				t.Fatalf("invalid cost: %+v", row)
			}
		} else {
			if row.Denominator == nil || *row.Denominator != 105 || row.Cost != nil {
				t.Fatalf("invalid scale: %+v", row)
			}
			if row.Kind == "interpolated" && row.Effort != "" {
				t.Fatal("interpolation effort fabricated")
			}
			if row.Kind == "measured" && row.Effort == "" {
				t.Fatal("measurement effort missing")
			}
		}
	}
	for i, fact := range facts {
		if i > 0 && facts[i-1].ModelID >= fact.ModelID {
			t.Fatal("model ids not strictly sorted")
		}
		for j := 1; j < len(fact.Efforts); j++ {
			if fact.Efforts[j-1] >= fact.Efforts[j] {
				t.Fatal("efforts not strictly sorted")
			}
		}
	}
	denominator, tokens := int64(105), int64(0)
	zero := 0.0
	row := BugHuntRow{Key: "model:0", ModelID: "model", Kind: "cost", Value: 0, Denominator: &denominator, Cost: &Cost{TokensIn: &tokens, TokensOut: &tokens, USD: &zero, WallS: &zero}, OriginRef: "origin", SourceVersion: "source"}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"key", "model_id", "kind", "value", "denominator", "cost", "origin_ref", "source_version"} {
		if _, ok := object[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
	if _, ok := object["effort"]; ok {
		t.Fatal("unknown effort should be absent")
	}
	if len(object) != 8 {
		t.Fatalf("unexpected JSON fields: %s", raw)
	}
	var cost map[string]json.RawMessage
	if err := json.Unmarshal(object["cost"], &cost); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tokens_in", "tokens_out", "usd", "wall_s"} {
		if string(cost[key]) != "0" {
			t.Errorf("zero %s missing: %s", key, raw)
		}
	}
	var decoded BugHuntRow
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, row) {
		t.Fatal("JSON round trip drift")
	}
	raw, err = json.Marshal(RegistryFact{ModelID: "model", Efforts: []string{"none"}})
	if err != nil || string(raw) != `{"model_id":"model","efforts":["none"]}` {
		t.Fatalf("registry JSON: %s, %v", raw, err)
	}
}
