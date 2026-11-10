package vendorplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func unmeasuredFixture() Model {
	return Model{ID: "unmeasured-fixture", Description: "Availability-only test model", Lifecycle: LifecycleCurrent,
		Unmeasured: &UnmeasuredClaim{Kind: UnmeasuredClaimKind, AsOf: "2026-11-10", Source: "test observer", Observation: "explicit probe returned ok"},
		Effort:     EffortDeclaration{Support: agentic.EffortSupportRequired, Vocabulary: []string{"high", "xhigh"}, Recommended: "high"},
		Systems:    []agentic.SystemID{"fixture-system"},
	}
}

// A valid unmeasured claim registers no numeric rank. Missing/contradictory
// claims, numeric ranks (including zero), and incomplete attribution refuse
// with ErrRankInvalid; validation leaves the input unchanged.
func TestUnmeasuredClaimValidation(t *testing.T) {
	valid := unmeasuredFixture()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Model){
		"positive numeric score": func(m *Model) {
			m.Rank = &CapabilityRank{Score: 43, Basis: []RankEvidence{{Source: "fixture", Observation: "invented score"}}}
		},
		"zero numeric score":     func(m *Model) { m.Rank = &CapabilityRank{Score: 0} },
		"negative numeric score": func(m *Model) { m.Rank = &CapabilityRank{Score: -1} },
		"ranked zero": func(m *Model) {
			m.Unmeasured = nil
			m.Rank = &CapabilityRank{Score: 0, Basis: []RankEvidence{{Source: "fixture", Observation: "zero"}}}
		},
		"no claim":               func(m *Model) { m.Unmeasured = nil },
		"unknown kind":           func(m *Model) { m.Unmeasured.Kind = "measured" },
		"missing date":           func(m *Model) { m.Unmeasured.AsOf = "" },
		"invalid date":           func(m *Model) { m.Unmeasured.AsOf = "2026-02-30" },
		"missing source":         func(m *Model) { m.Unmeasured.Source = " " },
		"missing observation":    func(m *Model) { m.Unmeasured.Observation = "\t" },
		"alias":                  func(m *Model) { m.AliasOf = "other-model" },
		"display recommendation": func(m *Model) { m.Recommended = true },
	} {
		t.Run(name, func(t *testing.T) {
			m := CloneModels([]Model{valid})[0]
			mutate(&m)
			before, _ := json.Marshal(m)
			if err := m.Validate(); !errors.Is(err, ErrRankInvalid) {
				t.Fatalf("got %v, want ErrRankInvalid", err)
			}
			after, _ := json.Marshal(m)
			if !bytes.Equal(before, after) {
				t.Fatal("validation mutated a refused declaration")
			}
		})
	}
	ranked := CloneModels([]Model{valid})[0]
	ranked.Unmeasured = nil
	ranked.Rank = &CapabilityRank{Score: 1, Basis: []RankEvidence{{Source: "fixture", Observation: "positive control"}}}
	if err := ranked.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Unmeasured models cannot enter ordering or Tied computation even if a
// caller bypasses registration and supplies a contradictory numeric rank.
// A ranked alias targeting an unmeasured identity also refuses exact-id bypass.
func TestUnmeasuredExcludedBeforeOrderingAndAliasResolution(t *testing.T) {
	u := unmeasuredFixture()
	a := CloneModels([]Model{u})[0]
	a.ID = "ranked-fixture"
	a.Unmeasured = nil
	a.Rank = &CapabilityRank{Score: 43, Basis: []RankEvidence{{Source: "fixture", Observation: "ranked control"}}}
	want := Lineup([]Model{a})
	for _, score := range []int{0, 43, 999} {
		for _, order := range []bool{false, true} {
			u.Rank = &CapabilityRank{Score: score}
			models := []Model{a, u}
			if order {
				models = []Model{u, a}
			}
			if got := Lineup(models); !reflect.DeepEqual(got, want) {
				t.Fatalf("unmeasured changed lineup or Tied: %+v", got)
			}
		}
	}
	u.Rank = nil
	if got := Lineup([]Model{u}); len(got) != 0 {
		t.Fatalf("unmeasured became top row: %+v", got)
	}
	a.AliasOf = u.ID
	if err := checkAliases([]Model{a, u}); !errors.Is(err, ErrAliasInvalid) {
		t.Fatalf("alias bypass: %v", err)
	}
	a.AliasOf = ""
	if err := checkAliases([]Model{a, u}); err != nil {
		t.Fatalf("positive control: %v", err)
	}
}

// Clones own both alternatives: mutations cannot reach an earlier snapshot.
func TestRankingClaimClonesAreDetached(t *testing.T) {
	u := unmeasuredFixture()
	a := CloneModels([]Model{u})[0]
	a.Unmeasured = nil
	a.Rank = &CapabilityRank{Score: 43, Basis: []RankEvidence{{Source: "fixture", Observation: "control"}}}
	source := []Model{u, a}
	before, _ := json.Marshal(source)
	copy := CloneModels(source)
	copy[0].Unmeasured.Source = "changed"
	copy[1].Rank.Score = 1
	copy[1].Rank.Basis[0].Source = "changed"
	after, _ := json.Marshal(source)
	if !bytes.Equal(before, after) {
		t.Fatal("clone changed earlier claims")
	}
}
