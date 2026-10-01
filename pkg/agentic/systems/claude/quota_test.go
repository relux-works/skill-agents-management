package claude

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/quotatest"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

func quotaEnvelope(t *testing.T, text string, turns int, api float64) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"result": text, "num_turns": turns, "duration_api_ms": api, "is_error": false})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestQuotaGoldenAndCachedAge(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	c.ReadAt = time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)
	c.RetrievedAt = c.ReadAt.Add(time.Second)
	r, err := New().ParseQuota(quotatest.Fixture(t, "quota.json"), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 3 || r.State != providerquota.PercentOnly || r.Authenticated != providerquota.AuthYes || r.ObservedAt != nil {
		t.Fatalf("golden: %v", r)
	}
	want := map[string]struct {
		used    float64
		minutes int
		scope   string
	}{"Current session": {76, 300, ""}, "Current week (all models)": {54, 10080, ""}, "Current week (Fable)": {33, 10080, "Fable"}}
	for _, w := range r.Windows {
		v, ok := want[w.ID]
		if !ok || *w.UsedPercent != v.used || w.Minutes != v.minutes || w.Scope != v.scope || w.ObservedAt != nil {
			t.Fatal("lane/measurement contract drift")
		}
	}
	if len(r.Failures) != 1 || r.Failures[0].Reason != "measurement_time_unknown" {
		t.Fatal("missing explanation of unknown source age")
	}
	var e map[string]any
	if err = json.Unmarshal(quotatest.Fixture(t, "quota.json"), &e); err != nil {
		t.Fatal(err)
	}
	e["result"] = e["result"].(string) + "\nShowing last-known usage"
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := New().ParseQuota(b, c)
	if err != nil || cached.ObservedAt != nil {
		t.Fatal("cached retrieval became observation")
	}
	e["result"] = e["result"].(string) + "\nfooter churn"
	b, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	again, err := New().ParseQuota(b, c)
	if err != nil || again.WindowsDigest != r.WindowsDigest {
		t.Fatal("footer churned windows digest")
	}
}
func TestReview03SlashUnsupported(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	r, err := New().ParseQuota(quotaEnvelope(t, "Unknown command: /usage", 0, 0), c)
	quotatest.Reason(t, r, err, "slash_unsupported")
}
func TestReview04ModelTurnSpent(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	var e map[string]any
	if err := json.Unmarshal(quotatest.Fixture(t, "quota.json"), &e); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		turns int
		api   float64
	}{{1, 812}, {0, 812}, {1, 0}} {
		e["num_turns"] = tc.turns
		e["duration_api_ms"] = tc.api
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		r, err := New().ParseQuota(b, c)
		quotatest.Reason(t, r, err, "model_turn_spent")
	}
}
func TestQuotaResetRolloverZoneAndLaneDrift(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	text := quotatest.Fixture(t, "quota.json")
	r, err := New().ParseQuota(text, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range r.Windows {
		if w.ResetsAt.Year() != 2027 {
			t.Fatal("reset did not roll year forward")
		}
	}
	for _, tc := range []struct{ old, new, reason string }{{"Asia/Tbilisi", "Missing/Zone", "zone_unknown"}, {"Current session", "Session now", "lane_missing"}, {"76%", "-1%", "out_of_range"}, {"76%", "NaN%", "out_of_range"}} {
		r, err = New().ParseQuota(bytes.ReplaceAll(text, []byte(tc.old), []byte(tc.new)), c)
		quotatest.Reason(t, r, err, tc.reason)
	}
}
func TestQuotaStructuredSyntheticReport(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	b := []byte(`{"num_turns":0,"duration_api_ms":0,"result":"You are currently using your subscription to power your Claude Code usage","usage_report":{"observed_at":"2026-10-01T11:00:00Z","limits":[{"kind":"five_hour","group":"session","percent":28,"resets_at":"2026-10-01T17:00:00Z","observed_at":"2026-10-01T11:59:00Z"},{"kind":"seven_day","group":"weekly","percent":59,"resets_at":"2026-10-05T17:00:00Z"}]}}`)
	r, err := New().ParseQuota(b, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 2 || r.ObservedAt == nil || r.ObservedAt.Hour() != 11 || r.ObservedAt.Minute() != 0 || !r.RetrievedAt.Equal(c.RetrievedAt) {
		t.Fatal("structured source timestamps replaced by retrieval")
	}

	cached := bytes.Replace(b, []byte(subscriptionLine), []byte(subscriptionLine+" Showing last-known usage"), 1)
	unknown, err := New().ParseQuota(cached, c)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.ObservedAt != nil || unknown.Windows[0].ObservedAt != nil {
		t.Fatal("explicit cached fallback received a measurement time")
	}
}

func TestQuotaFrozenPlan(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	binary, env := quotatest.BinaryEnv(t, "claude")
	p, err := New().QuotaPlan(providerquota.Request{Context: c, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary != binary || !reflect.DeepEqual(p.Argv, []string{"-p", "--output-format", "json", "--no-session-persistence", "--setting-sources", "", "--strict-mcp-config", "/usage"}) || p.CwdPolicy != providerquota.ScratchCwd || p.HoldStdinOpen || !p.Ready {
		t.Fatal("frozen plan drift")
	}
}

func TestRound2RequiredUnscopedLanes(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	for _, tc := range []struct{ name, limits string }{
		{"missing-session", `[{"kind":"seven_day","percent":20}]`},
		{"scoped-session", `[{"kind":"five_hour","percent":20,"scope":"vendor-model"},{"kind":"seven_day","percent":20}]`},
		{"only-scoped-weekly", `[{"kind":"five_hour","percent":20},{"kind":"seven_day","percent":20,"scope":{"model":{"id":"vendor-model"}}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := []byte(`{"num_turns":0,"duration_api_ms":0,"result":"` + subscriptionLine + `","usage_report":{"limits":` + tc.limits + `}}`)
			r, err := New().ParseQuota(b, c)
			quotatest.Reason(t, r, err, "lane_missing")
		})
	}
	// Both mandatory lanes remain unscoped; extra vendor scopes stay verbatim.
	b := []byte(`{"num_turns":0,"duration_api_ms":0,"result":"` + subscriptionLine + `","usage_report":{"limits":[{"kind":"five_hour","percent":20},{"kind":"seven_day","percent":30,"scope":null},{"kind":"seven_day","percent":40,"scope":"Vendor Model"}]}}`)
	r, err := New().ParseQuota(b, c)
	if err != nil || len(r.Windows) != 3 {
		t.Fatalf("extra scoped row: %v %v", r, err)
	}
	found := false
	for _, w := range r.Windows {
		if w.Scope == "Vendor Model" {
			found = true
		}
	}
	if !found {
		t.Fatal("vendor scope lost")
	}
	for _, old := range []string{"Current session", "Current week (all models)"} {
		b := bytes.ReplaceAll(quotatest.Fixture(t, "quota.json"), []byte(old), []byte("Missing lane"))
		r, err := New().ParseQuota(b, c)
		quotatest.Reason(t, r, err, "lane_missing")
	}
}
