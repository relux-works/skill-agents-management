package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/quotatest"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

func TestQuotaGoldenAndSyntheticFourWindows(t *testing.T) {
	c := quotatest.Context(t, "codex", true)
	payload := quotatest.Fixture(t, "quota.json")
	r, err := New().ParseQuota(quotatest.RPC(t, c.Identity.Home, payload), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 3 || r.State != providerquota.Exact || r.Confidence != "exact" || r.Plan != "pro" || r.Authenticated != providerquota.AuthYes || r.HarnessVersion != "0.153.4" {
		t.Fatalf("captured fixture: %v", r)
	}
	for _, w := range r.Windows {
		if *w.UsedPercent != 0 || w.ObservedAt == nil || !w.ObservedAt.Equal(c.ReadAt) {
			t.Fatal("lost live percentages/source read time")
		}
	}
	if r.Windows[0].ID != "codex" || r.Windows[0].Minutes != 10080 || r.Windows[0].ResetsAt.Unix() != 1789389163 || r.Windows[1].ID != "codex_bengalfox" || r.Windows[1].Scope != "GPT-5.3-Codex-Spark" || r.Windows[1].Minutes != 300 || r.Windows[2].Minutes != 10080 {
		t.Fatal("vendor windows changed")
	}
	// The literal probe has three windows. This additional four-window case is
	// synthetic, not a new observation: both default slots are present.
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(payload, &doc); err != nil {
		t.Fatal(err)
	}
	var byID map[string]map[string]any
	if err = json.Unmarshal(doc["rateLimitsByLimitId"], &byID); err != nil {
		t.Fatal(err)
	}
	byID["codex"]["secondary"] = map[string]any{"usedPercent": 28, "windowDurationMins": 300}
	doc["rateLimitsByLimitId"], err = json.Marshal(byID)
	if err != nil {
		t.Fatal(err)
	}
	synthetic, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	r, err = New().ParseQuota(quotatest.RPC(t, c.Identity.Home, synthetic), c)
	if err != nil || len(r.Windows) != 4 {
		t.Fatalf("synthetic four windows: %v %v", r, err)
	}
}
func TestReview05AbsentWindowIsNotZero(t *testing.T) {
	c := quotatest.Context(t, "codex", true)
	payload := []byte(`{"rateLimits":{"limitId":"codex","primary":null,"secondary":{"usedPercent":0}}}`)
	r, err := New().ParseQuota(quotatest.RPC(t, c.Identity.Home, payload), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 1 || r.Windows[0].Kind != "secondary" || *r.Windows[0].UsedPercent != 0 || r.Windows[0].Minutes != 0 || r.Windows[0].ResetsAt != nil {
		t.Fatalf("invented absent fields: %v", r)
	}
}
func TestReview06HomeMismatch(t *testing.T) {
	c := quotatest.Context(t, "codex", true)
	r, err := New().ParseQuota(quotatest.RPC(t, filepath.Join(t.TempDir(), "other"), quotatest.Fixture(t, "quota.json")), c)
	quotatest.Reason(t, r, err, "home_mismatch")
}
func TestQuotaNumbersAndAuthentication(t *testing.T) {
	c := quotatest.Context(t, "codex", true)
	for _, tc := range []struct{ payload, reason string }{{`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":-1}}}`, "out_of_range"}, {`{"rateLimits":{"limitId":"codex","primary":{}}}`, "percent_missing"}} {
		r, err := New().ParseQuota(quotatest.RPC(t, c.Identity.Home, []byte(tc.payload)), c)
		quotatest.Reason(t, r, err, tc.reason)
	}
	r, err := New().ParseQuota(quotatest.RPC(t, c.Identity.Home, []byte(`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":140}}}`)), c)
	if err != nil || *r.Windows[0].UsedPercent != 140 {
		t.Fatalf("amended over-quota support: %v", err)
	}
	raw := quotatest.RPC(t, c.Identity.Home, []byte(`{}`))
	i := bytes.Index(raw, []byte(`{"id":2`))
	raw = append(raw[:i], []byte("{\"id\":2,\"error\":{\"code\":401}}\n")...)
	r, err = New().ParseQuota(raw, c)
	quotatest.Reason(t, r, err, "unauthenticated")
	if r.Authenticated != providerquota.AuthNo {
		t.Fatal("401 not authentication no")
	}
}
func TestQuotaFrozenPlanAndFakePipeCompletion(t *testing.T) {
	c := quotatest.Context(t, "codex", true)
	binary, env := quotatest.BinaryEnv(t, "codex")
	plan, err := New().QuotaPlan(providerquota.Request{Context: c, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Binary != binary || !reflect.DeepEqual(plan.Argv, []string{"-s", "read-only", "-a", "never", "app-server"}) || plan.CwdPolicy != providerquota.ScratchCwd || !plan.HoldStdinOpen || !plan.Ready {
		t.Fatal("frozen plan drift")
	}
	init := quotatest.RPC(t, c.Identity.Home, []byte(`{}`))
	first := bytes.SplitAfter(init, []byte{'\n'})[0]
	if plan.Complete(first) || plan.Complete([]byte(`{"id":2,"result":{}}`)) || !plan.Complete([]byte("{\"id\":2,\"result\":{}}\n")) {
		t.Fatal("completion predicate drift")
	}
	input, writer := io.Pipe()
	output, serverWriter := io.Pipe()
	defer input.Close()
	defer writer.Close()
	defer output.Close()
	defer serverWriter.Close()
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(input)
		if !scanner.Scan() {
			done <- io.ErrUnexpectedEOF
			return
		}
		if _, e := serverWriter.Write(first); e != nil {
			done <- e
			return
		}
		for i := 0; i < 2; i++ {
			if !scanner.Scan() {
				done <- io.ErrUnexpectedEOF
				return
			}
		}
		if _, e := serverWriter.Write([]byte("{\"id\":2,\"result\":{}}\n")); e != nil {
			done <- e
			return
		}
		for scanner.Scan() {
		}
		done <- scanner.Err()
	}()
	go func() { _, _ = writer.Write(plan.Stdin) }()
	scanner := bufio.NewScanner(output)
	var accumulated []byte
	if !scanner.Scan() {
		t.Fatal("initialize not received")
	}
	accumulated = append(accumulated, scanner.Bytes()...)
	accumulated = append(accumulated, '\n')
	if plan.Complete(accumulated) {
		t.Fatal("stdin closed at initialize")
	}
	go func() { _, _ = writer.Write(plan.AfterInitialize) }()
	if !scanner.Scan() {
		t.Fatal("quota response not received")
	}
	accumulated = append(accumulated, scanner.Bytes()...)
	accumulated = append(accumulated, '\n')
	if !plan.Complete(accumulated) {
		t.Fatal("completion absent")
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fake server deadlocked waiting for stdin EOF")
	}
}
func quotaSchemaExcerpt(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var s struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
		Properties  json.RawMessage            `json:"properties"`
		Required    json.RawMessage            `json:"required"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return map[string]json.RawMessage{"GetAccountRateLimitsResponse.properties": s.Properties, "GetAccountRateLimitsResponse.required": s.Required, "RateLimitSnapshot": s.Definitions["RateLimitSnapshot"], "RateLimitWindow": s.Definitions["RateLimitWindow"]}
}
func TestQuotaPinnedSchema(t *testing.T) {
	s := quotaSchemaExcerpt(t, quotatest.Fixture(t, "quota-schema.json"))
	for name, b := range s {
		if len(b) == 0 {
			t.Fatalf("schema definition missing: %s", name)
		}
	}
	var window struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(s["RateLimitWindow"], &window); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(window.Required, []string{"usedPercent"}) {
		t.Fatalf("optional fields changed: %v", window.Required)
	}
}

// A consumer exports the schema offline and supplies its directory. This test
// never discovers or executes an installed harness, even when one is present.
func TestQuotaSchemaExportDiff(t *testing.T) {
	dir := os.Getenv("PROVIDERQUOTA_CODEX_SCHEMA_DIR")
	if dir == "" {
		t.Skip("no offline schema export supplied; no live/schema harness execution in tests")
	}
	b, err := os.ReadFile(filepath.Join(dir, "v2", "GetAccountRateLimitsResponse.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, want := quotaSchemaExcerpt(t, b), quotaSchemaExcerpt(t, quotatest.Fixture(t, "quota-schema.json"))
	for key := range want {
		var a, z any
		if json.Unmarshal(got[key], &a) != nil || json.Unmarshal(want[key], &z) != nil || !reflect.DeepEqual(a, z) {
			t.Fatalf("offline schema drift in %s", key)
		}
	}
}

func TestRound2OptionalLegacyLimitID(t *testing.T) {
	c := quotatest.Context(t, "codex", true)
	for _, id := range []string{"absent", "null"} {
		for _, withMap := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/map=%t", id, withMap), func(t *testing.T) {
				legacy := map[string]any{"primary": map[string]any{"usedPercent": 12, "windowDurationMins": 300}}
				if id == "null" {
					legacy["limitId"] = nil
				}
				payload := map[string]any{"rateLimits": legacy}
				if withMap {
					payload["rateLimitsByLimitId"] = map[string]any{"vendor-key": map[string]any{"limitName": "Vendor Scope", "primary": map[string]any{"usedPercent": 37, "windowDurationMins": 300}}}
				}
				b, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				r, err := New().ParseQuota(quotatest.RPC(t, c.Identity.Home, b), c)
				if !withMap {
					quotatest.Reason(t, r, err, "limit_id_missing")
					return
				}
				if err != nil || len(r.Windows) != 1 || r.Windows[0].ID != "vendor-key" || r.Windows[0].Label != "Vendor Scope" || r.Windows[0].Scope != "Vendor Scope" || *r.Windows[0].UsedPercent != 37 {
					t.Fatalf("optional legacy id rejected map: %v %v", r, err)
				}
			})
		}
	}
	// A present map excludes even a usable legacy view with a different id.
	payload := []byte(`{"rateLimits":{"limitId":"legacy","primary":{"usedPercent":12}},"rateLimitsByLimitId":{"vendor-key":{"primary":{"usedPercent":37}}}}`)
	r, err := New().ParseQuota(quotatest.RPC(t, c.Identity.Home, payload), c)
	if err != nil || len(r.Windows) != 1 || r.Windows[0].ID != "vendor-key" {
		t.Fatalf("map not authoritative: %v %v", r, err)
	}
	payload = []byte(`{"rateLimits":{"limitId":"legacy","primary":{"usedPercent":12}}}`)
	r, err = New().ParseQuota(quotatest.RPC(t, c.Identity.Home, payload), c)
	if err != nil || len(r.Windows) != 1 || r.Windows[0].ID != "legacy" {
		t.Fatalf("legacy keyed fallback: %v %v", r, err)
	}
}

func TestRound3RelativeManagedPlanSurvivesScratchCwd(t *testing.T) {
	planning := t.TempDir()
	canonical, err := filepath.EvalSymlinks(planning)
	if err != nil {
		t.Fatal(err)
	}
	planning = canonical
	t.Chdir(planning)
	pkg, triple, ok := platformPackageForHost()
	if !ok {
		t.Skip("no published managed package for this platform")
	}
	checked := filepath.Join(planning, "package", "node_modules", pkg, "vendor", triple, "bin", executableName)
	if err := os.MkdirAll(filepath.Dir(checked), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checked, []byte("inert fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := New().QuotaPlan(providerquota.Request{Env: []string{"HOME=" + t.TempDir(), "PATH=bin", managedPackageRootEnv + "=package"}})
	if err != nil || !plan.Ready || plan.CwdPolicy != providerquota.ScratchCwd || plan.Binary != checked || !filepath.IsAbs(plan.Binary) {
		t.Fatalf("plan lost checked absolute managed binary: %#v, %v", plan, err)
	}
	t.Chdir(t.TempDir())
	b, err := os.ReadFile(plan.Binary)
	if err != nil || string(b) != "inert fixture" {
		t.Fatalf("managed binary changed meaning in scratch cwd: %q, %v", b, err)
	}
}
