package muse

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/quotatest"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

func TestReview14AbsentUsageUnknownAuthentication(t *testing.T) {
	c := quotatest.Context(t, "muse", false)
	r, err := New().ParseQuota(quotatest.RPC(t, "", []byte(`{}`)), c)
	quotatest.Reason(t, r, err, "no_observation")
	if r.Key != "runtime-muse" || r.Authenticated != providerquota.AuthUnknown || r.ObservedAt != nil {
		t.Fatal("absent usage invented auth or age")
	}
}
func TestQuotaSyntheticPresentUsage(t *testing.T) {
	c := quotatest.Context(t, "muse", false)
	r, err := New().ParseQuota(quotatest.RPC(t, "", quotatest.Fixture(t, "quota-synthetic.json")), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 2 || r.State != providerquota.LastObserved || r.Authenticated != providerquota.AuthUnknown || r.Plan != "subscription" || r.ObservedAt == nil || r.ObservedAt.UnixMilli() != 1790856000000 {
		t.Fatalf("synthetic golden: %v", r)
	}
	if *r.Windows[1].UsedPercent != 140 || r.Windows[1].RemainingPercent != nil || r.Windows[1].Minutes != 300 || r.Windows[0].Minutes != 10080 {
		t.Fatal("over-quota/duration changed")
	}
	for _, w := range r.Windows {
		if !w.ObservedAt.Equal(time.UnixMilli(1790856000000)) {
			t.Fatal("arrival stamp changed")
		}
	}
}
func TestQuotaMSPPlanAndDecisionBoundary(t *testing.T) {
	c := quotatest.Context(t, "muse", false)
	binary, env := quotatest.BinaryEnv(t, "muse")
	env = append(env, "MUSE_NO_AUTO_UPDATE=0")
	p, err := New().QuotaPlan(providerquota.Request{Context: c, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary != binary || !reflect.DeepEqual(p.Argv, []string{"serve"}) || p.CwdPolicy != providerquota.ScratchCwd || !p.HoldStdinOpen || !p.Ready || p.RefusalReason != "" {
		t.Fatal("verified JSONL plan drift")
	}
	found := false
	for _, entry := range p.Env {
		if entry == "MUSE_NO_AUTO_UPDATE=1" {
			found = true
		}
		if entry == "MUSE_NO_AUTO_UPDATE=0" {
			t.Fatal("auto-update remained on")
		}
	}
	if !found {
		t.Fatal("update prevention missing")
	}
	if !bytes.Contains(p.Stdin, []byte(`"name":"task_board"`)) || !bytes.Contains(p.AfterInitialize, []byte(`"method":"usage/read"`)) || bytes.Contains(p.AfterInitialize, []byte("session/start")) {
		t.Fatal("MSP schema/zero-inference plan drift")
	}
	var _ providerquota.Reader = (*System)(nil)
}

func TestQuotaFreshHostJSONL(t *testing.T) {
	c := quotatest.Context(t, "muse", false)
	_, env := quotatest.BinaryEnv(t, "muse")
	p, err := New().QuotaPlan(providerquota.Request{Context: c, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	stdout := quotatest.Fixture(t, "quota-fresh-host.jsonl")
	first := bytes.IndexByte(stdout, '\n') + 1
	if first == 0 || p.Complete(stdout[:first]) || p.Complete(stdout[:len(stdout)-1]) || !p.Complete(stdout) {
		t.Fatal("completion must wait for the complete usage/read JSONL frame")
	}
	r, err := New().ParseQuota(stdout, c)
	quotatest.Reason(t, r, err, "no_observation")
	if r.Key != "runtime-muse" || r.Source != "msp-usage-read" || r.Authenticated != providerquota.AuthUnknown || r.ObservedAt != nil || r.Plan != "" {
		t.Fatal("fresh host invented usage, authentication or age")
	}
}

const quotaSchemaFingerprint = "sha256:61afea3112e0906e9dc3a536144278a74cb4b36fc6e20901a91d4432ba3568e2"

func quotaSchemaDefinitions(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var schema struct {
		Definitions map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	return schema.Definitions
}

// These definitions are pinned from Muse 1.4.2; SubscriptionUsage and
// UsageReadResult are unchanged from 1.4.1.
func TestQuotaPinnedSchema(t *testing.T) {
	schema := quotaSchemaDefinitions(t, quotatest.Fixture(t, "quota-schema.json"))
	for _, name := range []string{"InitializeParams", "ClientInfo", "UsageReadResult", "SubscriptionUsage", "SubscriptionUsageWindow", "SubscriptionUsageWeekly"} {
		if len(schema[name]) == 0 {
			t.Fatalf("missing schema definition: %s", name)
		}
	}
	var manifest struct {
		Fingerprint  string `json:"fingerprint"`
		Version      int    `json:"schemaVersion"`
		Experimental bool   `json:"experimental"`
	}
	if err := json.Unmarshal(quotatest.Fixture(t, "quota-schema-manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Fingerprint != quotaSchemaFingerprint || manifest.Version != 1 || manifest.Experimental {
		t.Fatal("stable Muse 1.4.2 schema manifest drift")
	}
}

// A consumer supplies an offline schema export; this test never runs Muse.
func TestQuotaSchemaExportDiff(t *testing.T) {
	dir := os.Getenv("PROVIDERQUOTA_MUSE_SCHEMA_DIR")
	if dir == "" {
		t.Skip("no offline schema export supplied; no harness execution in tests")
	}
	b, err := os.ReadFile(filepath.Join(dir, "msp.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := quotaSchemaDefinitions(t, b)
	for name, want := range quotaSchemaDefinitions(t, quotatest.Fixture(t, "quota-schema.json")) {
		var a, z any
		if json.Unmarshal(got[name], &a) != nil || json.Unmarshal(want, &z) != nil || !reflect.DeepEqual(a, z) {
			t.Fatalf("offline schema drift in %s", name)
		}
	}
	b, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a, z any
	if json.Unmarshal(b, &a) != nil || json.Unmarshal(quotatest.Fixture(t, "quota-schema-manifest.json"), &z) != nil || !reflect.DeepEqual(a, z) {
		t.Fatal("offline schema manifest drift")
	}
}
func TestQuotaMalformedUsage(t *testing.T) {
	c := quotatest.Context(t, "muse", false)
	for _, tc := range []struct{ before, after, reason string }{{`"usedPercent": 140`, `"usedPercent": -1`, "out_of_range"}, {`"observedAtMs": 1790856000000`, `"observedAtMs": 1790856600000`, "clock_skew"}, {`"weekly": {`, `"other": {`, "payload_missing"}} {
		b := bytes.ReplaceAll(quotatest.Fixture(t, "quota-synthetic.json"), []byte(tc.before), []byte(tc.after))
		r, err := New().ParseQuota(quotatest.RPC(t, "", b), c)
		quotatest.Reason(t, r, err, tc.reason)
	}
}
