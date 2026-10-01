package agy

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/quotatest"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

func TestQuotaGolden(t *testing.T) {
	c := quotatest.Context(t, "agy", false)
	r, err := NewWithRuntime(Runtime{Executable: "fixture", Version: MinimumQuotaVersion}).ParseQuota(quotatest.Fixture(t, "quota.json"), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 4 || r.State != providerquota.Exact || r.Confidence != "exact-fraction" || r.Plan != "" || r.Authenticated != providerquota.AuthYes || r.HarnessVersion != MinimumQuotaVersion || r.Key != "runtime-agy" {
		t.Fatalf("golden: %v", r)
	}
	for _, w := range r.Windows {
		if *w.UsedPercent != 0 || *w.RemainingPercent != 100 || w.ResetsAt == nil || (w.Scope != "Gemini Models" && w.Scope != "Claude and GPT models") {
			t.Fatal("fraction/group/reset lost")
		}
	}
}
func TestQuotaEnvelopeRefusals(t *testing.T) {
	c := quotatest.Context(t, "agy", false)
	for _, b := range []string{`{"status":"SUCCESS","num_turns":0}`, `{"status":"SUCCESS","num_turns":0,"command":{"name":"other"}}`, `{"status":"SUCCESS","num_turns":1,"command":{"name":"usage"}}`} {
		r, err := New().ParseQuota([]byte(b), c)
		if err == nil || len(r.Windows) != 0 || r.State != providerquota.Unavailable {
			t.Fatal("non-usage envelope accepted")
		}
	}
}
func TestQuotaUnknownWordNewGroupAndImpossibleFraction(t *testing.T) {
	c := quotatest.Context(t, "agy", false)
	b := bytes.ReplaceAll(quotatest.Fixture(t, "quota.json"), []byte(`"weekly"`), []byte(`"vendor-word"`))
	r, err := New().ParseQuota(b, c)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, w := range r.Windows {
		if w.Word == "vendor-word" {
			found++
			if w.Minutes != 0 {
				t.Fatal("invented duration")
			}
		}
	}
	if found != 2 {
		t.Fatal("lost unknown windows")
	}
	r, err = New().ParseQuota(bytes.ReplaceAll(b, []byte(`"remaining_fraction": 1`), []byte(`"remaining_fraction": 1.5`)), c)
	quotatest.Reason(t, r, err, "out_of_range")
	var e map[string]any
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	groups := e["command"].(map[string]any)["data"].(map[string]any)
	groups["groups"] = append(groups["groups"].([]any), map[string]any{"name": "Additional Models", "buckets": []any{map[string]any{"id": "additional", "window": "daily", "remaining_fraction": 0.2}}})
	b, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	r, err = New().ParseQuota(b, c)
	if err != nil || len(r.Windows) != 5 {
		t.Fatal("new group dropped")
	}
}
func TestQuotaFrozenPlanPreflightAndVersion(t *testing.T) {
	c := quotatest.Context(t, "agy", false)
	binary, env := quotatest.BinaryEnv(t, "agy")
	p, err := NewWithRuntime(Runtime{Executable: binary, Version: MinimumQuotaVersion}).QuotaPlan(providerquota.Request{Context: c, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary != binary || !reflect.DeepEqual(p.Argv, []string{"--output-format", "json", "--mode", "plan", "--print-timeout", "2m", "--print=/usage"}) || p.CwdPolicy != providerquota.ScratchCwd || !p.Ready {
		t.Fatal("attached --print grammar drift")
	}
	for _, v := range []string{"", "1.1.12", "1.1.26", "bad", "1.1.27garbage"} {
		_, err = NewWithRuntime(Runtime{Executable: binary, Version: v}).QuotaPlan(providerquota.Request{Context: c, Env: env})
		if err == nil {
			t.Fatalf("unsupported version %q accepted", v)
		}
	}
	if _, err = New().QuotaPlan(providerquota.Request{Context: c, Env: env}); err == nil {
		t.Fatal("display placeholder used as executable")
	}
	if !reflect.DeepEqual(RequiredQuotaFlags(), []string{"--output-format", "--mode", "--print-timeout", "--print="}) {
		t.Fatal("required flag pin lost")
	}
}

func TestRound2UnknownObservationTime(t *testing.T) {
	c := quotatest.Context(t, "agy", false)
	system := NewWithRuntime(Runtime{Executable: "fixture", Version: MinimumQuotaVersion})
	r, err := system.ParseQuota(quotatest.Fixture(t, "quota.json"), c)
	if err != nil {
		t.Fatal(err)
	}
	check := func(r providerquota.QuotaRecord) {
		if r.ObservedAt != nil {
			t.Fatal("aggregate uses retrieval as measurement time")
		}
		if len(r.Windows) == 0 {
			t.Fatal("fixture lost windows")
		}
		for _, w := range r.Windows {
			if w.ObservedAt != nil {
				t.Fatal("window uses retrieval as measurement time")
			}
		}
	}
	check(r)
	c.RetrievedAt = c.RetrievedAt.Add(2 * time.Hour)
	later, err := system.ParseQuota(quotatest.Fixture(t, "quota.json"), c)
	if err != nil {
		t.Fatal(err)
	}
	check(later)
	if later.WindowsDigest != r.WindowsDigest {
		t.Fatal("retrieval time changed window digest")
	}
}
