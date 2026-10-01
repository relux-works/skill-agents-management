package providerquota

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/providerlimits"
)

func testContext(t *testing.T) ParseContext {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return ParseContext{Runtime: "sample", ReadAt: now, RetrievedAt: now}
}
func goodRecord(t *testing.T, c ParseContext) QuotaRecord {
	t.Helper()
	r, err := Base(c, "session-push")
	if err != nil {
		t.Fatalf("Base: %v", err)
	}
	r.State = Exact
	r.Authenticated = AuthYes
	r.Inventory.Installed = true
	r.Windows = []QuotaWindow{{ID: "session", Minutes: 300, UsedPercent: Float(20), ObservedAt: Time(c.ReadAt)}, {ID: "weekly", Minutes: 10080, UsedPercent: Float(50), ObservedAt: Time(c.ReadAt.Add(-time.Minute))}}
	r, err = Finish(r, c)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return r
}
func requireReason(t *testing.T, err error, reason string) {
	t.Helper()
	var e *Refusal
	if !errors.As(err, &e) || e.Reason != reason {
		t.Fatalf("error = %v, want typed %s", err, reason)
	}
}
func TestStoreRoundTripByteStable(t *testing.T) {
	c := testContext(t)
	s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	r := goodRecord(t, c)
	if err := s.Write(c, r); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.layout.RecordFile(r.Key))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip differs: %v", got)
	}
	if err = s.Write(c, got); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.layout.RecordFile(r.Key))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("write/read/write changed bytes")
	}
}
func TestReview02ForgedRecordIsFailure(t *testing.T) {
	c := testContext(t)
	home := filepath.Join(t.TempDir(), "harness")
	c.Identity = &providerlimits.Identity{Provider: c.Runtime, Home: home, Key: providerlimits.IdentityKey(c.Runtime, home)}
	for _, tc := range []struct {
		name, reason string
		mutate       func(*QuotaRecord)
	}{
		{"key", "key_mismatch", func(r *QuotaRecord) { r.Key = "runtime-forged" }},
		{"digest", "digest_mismatch", func(r *QuotaRecord) { r.WindowsDigest = "sha256:forged" }},
		{"runtime", "key_mismatch", func(r *QuotaRecord) { r.Runtime = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return c.RetrievedAt }})
			if err != nil {
				t.Fatal(err)
			}
			r := goodRecord(t, c)
			tc.mutate(&r)
			if err := writeJSONAtomic(s.layout.RecordFile(*r.Identity), r); err != nil {
				t.Fatal(err)
			}
			got, err := s.Read(c)
			requireReason(t, err, tc.reason)
			if got.State != Unavailable || len(got.Windows) != 0 {
				t.Fatal("forged headroom returned")
			}
			b, err := json.Marshal(got.RouterProjection())
			if err != nil {
				t.Fatal(err)
			}
			if containsBytes(b, "remaining") {
				t.Fatal("failure exposed remaining")
			}
		})
	}
}
func containsBytes(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
func TestReview10ImpossibleNumbers(t *testing.T) {
	_ = testContext(t)
	for _, v := range []float64{-1, math.NaN(), math.Inf(1)} {
		w := QuotaWindow{}
		requireReason(t, Percent(&w, v), "out_of_range")
		if w.UsedPercent != nil {
			t.Fatal("refused percentage retained")
		}
	}
	for _, v := range []float64{-1, 1.5, math.NaN()} {
		w := QuotaWindow{}
		requireReason(t, Fraction(&w, v), "out_of_range")
	}
	// Amendment supersedes the old 140-percent refusal.
	w := QuotaWindow{}
	if err := Percent(&w, 140); err != nil || *w.UsedPercent != 140 || w.RemainingPercent != nil {
		t.Fatalf("over-quota clamped: %#v %v", w, err)
	}
}
func TestReview11ClockSkew(t *testing.T) {
	c := testContext(t)
	s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	r := goodRecord(t, c)
	for i := range r.Windows {
		r.Windows[i].ObservedAt = Time(c.RetrievedAt.Add(10 * time.Minute))
	}
	r.ObservedAt = oldest(r.Windows)
	r.WindowsDigest, _ = WindowsDigest(r.Windows)
	if err := writeJSONAtomic(s.layout.RecordFile(r.Key), r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(c)
	requireReason(t, err, "clock_skew")
	if len(got.Windows) != 0 {
		t.Fatal("future headroom returned")
	}
}
func TestMergeContracts(t *testing.T) {
	for _, kind := range []UpdateKind{Pull, Push, Failed} {
		t.Run(string(kind), func(t *testing.T) {
			c := testContext(t)
			old := goodRecord(t, c)
			next := c
			next.ReadAt = c.ReadAt.Add(5 * time.Minute)
			next.RetrievedAt = next.ReadAt
			in := goodRecord(t, next)
			in.Windows = in.Windows[:1]
			in, err := Finish(in, next)
			if err != nil {
				t.Fatal(err)
			}
			if kind == Failed {
				in, _ = Failure(next, "session-push", "transport_failed")
			}
			got, err := MergeRecord(old, in, kind, next)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case Pull:
				if len(got.Windows) != 1 || got.Windows[0].ID != "session" || !got.ObservedAt.Equal(next.ReadAt) {
					t.Fatalf("pull did not replace: %v", got)
				}
			case Push:
				if len(got.Windows) != 2 || !got.Windows[1].ObservedAt.Equal(*old.Windows[1].ObservedAt) || !got.ObservedAt.Equal(*old.ObservedAt) {
					t.Fatal("push refreshed untouched weekly window")
				}
			case Failed:
				if got.State != Unavailable || !reflect.DeepEqual(got.Windows, old.Windows) || !got.ObservedAt.Equal(*old.ObservedAt) || got.WindowsDigest != old.WindowsDigest || len(got.Failures) != 1 || !got.Failures[0].At.Equal(next.RetrievedAt) {
					t.Fatal("failure refreshed windows or lost failure time")
				}
			}
			got.Windows[0].UsedPercent = Float(99)
			if *old.Windows[0].UsedPercent != 20 {
				t.Fatal("merge aliased old record")
			}
		})
	}
}
func TestStoreMergeAndFailureBound(t *testing.T) {
	c := testContext(t)
	s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	r := goodRecord(t, c)
	got, err := s.Merge(c, r, Pull)
	if err != nil || len(got.Windows) != 2 {
		t.Fatalf("pull: %v", err)
	}
	failed, _ := Failure(c, "session-push", "transport_failed")
	for i := 0; i < MaxFailures+5; i++ {
		got, err = s.Merge(c, failed, Failed)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(got.Failures) != MaxFailures || !reflect.DeepEqual(got.Windows, r.Windows) || !got.ObservedAt.Equal(*r.ObservedAt) {
		t.Fatal("bounded failed store merge changed observations")
	}
}
func TestLockWinnerRetryAndDeadOwner(t *testing.T) {
	c := testContext(t)
	s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return c.RetrievedAt }, OwnerAlive: func(pid int) bool { return pid == os.Getpid() }})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.Acquire(context.Background(), "runtime-sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Acquire(context.Background(), lock.key); !errors.Is(err, ErrLocked) {
		t.Fatalf("second writer: %v", err)
	}
	if err = lock.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := s.Acquire(context.Background(), lock.key)
	if err != nil {
		t.Fatal(err)
	}
	if err = again.Release(); err != nil {
		t.Fatal(err)
	}
	path := s.layout.LockFile(lock.key)
	if err = writeJSONAtomic(path, lockOwner{PID: 123456789, AcquiredAt: func() *string { v := c.RetrievedAt.Add(-time.Hour).UTC().Format(time.RFC3339Nano); return &v }()}); err != nil {
		t.Fatal(err)
	}
	past := c.RetrievedAt.Add(-time.Hour)
	if err = os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if err = s.Write(c, goodRecord(t, c)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Failures) != 1 || got.Failures[0].Reason != "dead_owner_lock" {
		t.Fatal("dead lock break not recorded")
	}
}
func TestConcurrentWritersOneWinner(t *testing.T) {
	c := testContext(t)
	s, err := NewStore(LayoutAt(t.TempDir()), Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	release := make(chan struct{})
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lock, err := s.Acquire(context.Background(), "runtime-sample")
			results <- err
			if err == nil {
				<-release
				_ = lock.Release()
			}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if !(a == nil && errors.Is(b, ErrLocked) || b == nil && errors.Is(a, ErrLocked)) {
		t.Fatalf("winners: %v %v", a, b)
	}
	close(release)
	wg.Wait()
}
func TestRouterProjectionPrivacyAndCopy(t *testing.T) {
	c := testContext(t)
	r := goodRecord(t, c)
	r.HomeDisplay = "~/operator-only"
	r.HarnessVersion = "diagnostic-only"
	r.Checked = []string{"operator-only"}
	b, err := json.Marshal(r.RouterProjection())
	if err != nil {
		t.Fatal(err)
	}
	if containsBytes(b, "operator-only") || containsBytes(b, "diagnostic-only") {
		t.Fatal("operator fields reached router")
	}
	for _, v := range []string{"/private/location", "relative" + "/" + "location", "redacted" + "." + "invalid", "redacted" + "@" + "redacted", "https:" + "//" + "redacted", "192" + "." + "0" + "." + "2" + "." + "1", "Bearer credential", "sk-" + strings.Repeat("x", 16)} {
		bad := r
		bad.Windows = append([]QuotaWindow{}, r.Windows...)
		bad.Windows[0].Scope = v
		_, err := Finish(bad, c)
		requireReason(t, err, "privacy_violation")
		projection := bad.RouterProjection()
		if projection.State != Unavailable || len(projection.Windows) != 0 {
			t.Fatal("sensitive vendor text reached direct projection")
		}
	}
	projection := r.RouterProjection()
	*projection.Windows[0].UsedPercent = 90
	if *r.Windows[0].UsedPercent != 20 {
		t.Fatal("projection aliases record")
	}
}
func TestDigestOnlyWindowsAndOrder(t *testing.T) {
	c := testContext(t)
	r := goodRecord(t, c)
	a, err := WindowsDigest(r.Windows)
	if err != nil {
		t.Fatal(err)
	}
	r.Windows[0], r.Windows[1] = r.Windows[1], r.Windows[0]
	b, err := WindowsDigest(r.Windows)
	if err != nil || a != b {
		t.Fatal("digest depends on order")
	}
	r.RetrievedAt = r.RetrievedAt.Add(time.Minute)
	r.Failures = append(r.Failures, ReadFailure{Reason: "failed"})
	b, _ = WindowsDigest(r.Windows)
	if a != b {
		t.Fatal("non-window fields churn digest")
	}
}
func TestReview15CoverageRatio(t *testing.T) {
	c := testContext(t)
	a := goodRecord(t, c)
	unsupported := a
	unsupported.State = NotSupported
	unsupported.Runtime = "unsupported"
	failed := a
	failed.State = Unavailable
	failed.Runtime = "failed"
	got := Summarize([]QuotaRecord{a, unsupported, failed})
	if got.Readable != 1 || got.Of != 3 || !reflect.DeepEqual(got.Unsupported, []string{"unsupported"}) {
		t.Fatalf("ratio: %+v", got)
	}
}

func TestLockCorruptAcquiredAtKeepsLock(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, payload string
		broken        bool
	}{
		{"absent", `{"pid":12345}`, true},
		{"null", `{"pid":12345,"acquired_at":null}`, true},
		{"utc", `{"pid":12345,"acquired_at":"2026-10-01T11:00:00Z"}`, true},
		{"empty", `{"pid":12345,"acquired_at":""}`, false},
		{"garbage", `{"pid":12345,"acquired_at":"yesterday"}`, false},
		{"number", `{"pid":12345,"acquired_at":17}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout := LayoutAt(t.TempDir())
			if err := os.MkdirAll(layout.Root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := layout.LockFile("runtime-agy")
			if err := os.WriteFile(path, []byte(tc.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			old := now.Add(-time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			s, err := NewStore(layout, Options{Now: func() time.Time { return now }, OwnerAlive: func(int) bool { return false }})
			if err != nil {
				t.Fatal(err)
			}
			lock, err := s.Acquire(context.Background(), "runtime-agy")
			if tc.broken {
				if err != nil || lock == nil || !lock.BrokeDeadOwner {
					t.Fatalf("dead owner with acquired_at %s was not broken: %v", tc.payload, err)
				}
				return
			}
			if !errors.Is(err, ErrLocked) {
				t.Fatalf("corrupt acquired_at %s must keep the lock, got %v", tc.payload, err)
			}
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != tc.payload {
				t.Fatalf("corrupt lock was modified: %q %v", got, readErr)
			}
		})
	}
}
