package providerquota

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestRound2ProjectionForgedFields(t *testing.T) {
	c := testContext(t)
	for _, tc := range []struct {
		name  string
		forge func(*QuotaRecord)
	}{
		{"digest", func(r *QuotaRecord) { r.WindowsDigest = "/private/fixture" }},
		{"valid-looking-digest", func(r *QuotaRecord) {
			r.WindowsDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		}},
		{"state", func(r *QuotaRecord) { r.State = State("/private/fixture") }},
		{"unknown-state", func(r *QuotaRecord) { r.State = State("forged") }},
		{"authentication", func(r *QuotaRecord) { r.Authenticated = AuthStatus("/private/fixture") }},
		{"identity", func(r *QuotaRecord) { r.Key = "/private/fixture" }},
		{"runtime", func(r *QuotaRecord) { r.Runtime = "/private/fixture" }},
		{"plan", func(r *QuotaRecord) { r.Plan = "/private/fixture" }},
		{"source", func(r *QuotaRecord) { r.Source = "/private/fixture" }},
		{"credits", func(r *QuotaRecord) { r.Credits = &Credits{Unit: "/private/fixture"} }},
		{"failure-source", func(r *QuotaRecord) { r.Failures = []ReadFailure{{Reason: "failed", Source: "/private/fixture"}} }},
		{"failure-reason", func(r *QuotaRecord) { r.Failures = []ReadFailure{{Reason: "/private/fixture"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := goodRecord(t, c)
			tc.forge(&r)
			p := r.RouterProjection()
			if p.State != Unavailable || len(p.Windows) != 0 || len(p.Failures) != 1 || p.Failures[0].Reason != "privacy_violation" {
				t.Fatalf("forged projection accepted: %#v", p)
			}
			b, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if containsBytes(b, "/private/fixture") {
				t.Fatal("forged field disclosed")
			}
		})
	}
}
func TestRound2ImportGuardHelperEscape(t *testing.T) {
	for _, harness := range []string{"codex", "claude", "agy", "muse"} {
		t.Run(harness, func(t *testing.T) {
			sources := map[string]string{
				"pkg/agentic/systems/" + harness + "/quota.go": `package fixture; import "github.com/relux-works/skill-agents-management/internal/reviewescape"`,
				"internal/reviewescape/helper.go":              `package reviewescape; import "os/exec"`,
			}
			if got := forbiddenImports(sources); len(got) != 1 || got[0] != "internal/reviewescape/helper.go" {
				t.Fatalf("%s: missed transitive exec: %v", harness, got)
			}
		})
	}
	// Same-package helper imports cannot evade the graph either.
	if len(forbiddenImports(map[string]string{"pkg/agentic/systems/muse/quota.go": "package muse", "pkg/agentic/systems/muse/helper.go": `package muse; import "os/exec"`})) != 1 {
		t.Fatal("missed same-package helper")
	}
}
func TestRound2PushReplacesCarriedWindow(t *testing.T) {
	c := testContext(t)
	old := goodRecord(t, c)
	next := c
	next.ReadAt = c.ReadAt.Add(5 * time.Minute)
	next.RetrievedAt = next.ReadAt
	in := goodRecord(t, next)
	in.Windows = in.Windows[:1]
	in.Windows[0].UsedPercent = Float(73)
	in.Windows[0].ResetsAt = Time(next.ReadAt.Add(time.Hour))
	var err error
	in, err = Finish(in, next)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(old.Windows[1])
	check := func(t *testing.T, got QuotaRecord) {
		t.Helper()
		if len(got.Windows) != 2 {
			t.Fatal("push dropped or added a window")
		}
		if !reflect.DeepEqual(got.Windows[0], in.Windows[0]) {
			t.Fatal("push did not replace carried percentage, reset and observation")
		}
		after, _ := json.Marshal(got.Windows[1])
		if string(before) != string(after) {
			t.Fatal("untouched window changed bytes")
		}
		if got.WindowsDigest == old.WindowsDigest {
			t.Fatal("push did not change digest")
		}
		digest, err := WindowsDigest(got.Windows)
		if err != nil || digest != got.WindowsDigest {
			t.Fatal("push digest invalid")
		}
	}
	got, err := MergeRecord(old, in, Push, next)
	if err != nil {
		t.Fatal(err)
	}
	check(t, got)
	s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return next.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Write(c, old); err != nil {
		t.Fatal(err)
	}
	got, err = s.Merge(next, in, Push)
	if err != nil {
		t.Fatal(err)
	}
	check(t, got)
	got, err = s.Read(next)
	if err != nil {
		t.Fatal(err)
	}
	check(t, got)
	added := in
	added.Windows = append([]QuotaWindow{}, in.Windows...)
	added.Windows[0].ID = "additional"
	added, err = Finish(added, next)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.Merge(next, added, Push)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Windows) != 3 {
		t.Fatal("push did not add new window")
	}
	for _, w := range got.Windows {
		if w.ID == "additional" && reflect.DeepEqual(w, added.Windows[0]) {
			return
		}
	}
	t.Fatal("added window lost")
}
func TestRound2StoreRequiresClock(t *testing.T) {
	c := testContext(t)
	layout := LayoutAt(t.TempDir())
	s, err := NewStore(layout, Options{})
	requireReason(t, err, "clock_required")
	if s != nil {
		t.Fatal("missing clock created usable store")
	}
	if _, err = os.Stat(layout.Root); !os.IsNotExist(err) {
		t.Fatal("missing clock touched state")
	}
	s, err = NewStore(layout, Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.Acquire(context.Background(), "runtime-sample")
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Release(); err != nil {
		t.Fatal(err)
	}
}
