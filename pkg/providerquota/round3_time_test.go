package providerquota_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

const timeHelperEnv = "W3_QUOTA_TZ_HELPER"

// Re-exec gives each parser an untouched Local and a benign, synthetic TZif
// file outside the checkout. Reflect reads Location's name without calling
// Location.String/Zone (which would initialize Local itself). Go's lazy Local
// starts with an empty name and sets it after reading TZ. A positive control
// intentionally invokes time.Time.UnmarshalJSON, verifies initialization and
// the fixture abbreviation, proving that the fixture and detector work. The
// parser modes must leave the name empty; old numeric-offset decoders fail.
// No global time/IO seam or assignment to time.Local is used.
func TestRound3WireTimestampsDoNotInitializeLocal(t *testing.T) {
	if mode := os.Getenv(timeHelperEnv); mode != "" {
		runQuotaTimeHelper(t, mode)
		return
	}
	root := t.TempDir()
	zone := filepath.Join(root, "fixture-zone")
	// TZif v1: no transitions, one UTC-offset type, one abbreviation.
	fixture := make([]byte, 44)
	copy(fixture, "TZif")
	binary.BigEndian.PutUint32(fixture[36:40], 1)
	binary.BigEndian.PutUint32(fixture[40:44], 7)
	fixture = append(fixture, 0, 0, 0, 0, 0, 0)
	fixture = append(fixture, []byte("W3ZONE\x00")...)
	if err := os.WriteFile(zone, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"control", "agy", "claude", "claude-prose", "window", "failure", "record", "projection", "lock"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(executable, "-test.run=^TestRound3WireTimestampsDoNotInitializeLocal$", "-test.count=1")
			cmd.Env = append(os.Environ(), timeHelperEnv+"="+mode, "TZ="+zone, "HOME="+root)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("isolated timestamp helper failed: %v\n%s", err, output)
			}
		})
	}
}

func runQuotaTimeHelper(t *testing.T, mode string) {
	t.Helper()
	localName := func() string {
		return reflect.ValueOf(time.Local).Elem().FieldByName("name").String()
	}
	if name := localName(); name != "" {
		t.Fatalf("helper Local was already initialized: %q", name)
	}
	if mode == "control" {
		var v time.Time
		if err := json.Unmarshal([]byte(`"2026-10-01T13:00:00+00:00"`), &v); err != nil {
			t.Fatal(err)
		}
		if localName() == "" {
			t.Fatal("positive control did not initialize Local")
		}
		if name, offset := v.In(time.Local).Zone(); name != "W3ZONE" || offset != 0 {
			t.Fatalf("positive control did not load fixture zone: %q %d", name, offset)
		}
		return
	}
	stamp := "2026-10-01T13:00:00+00:00"
	want := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	c := providerquota.ParseContext{Runtime: mode, ReadAt: want, RetrievedAt: want}
	var got *time.Time
	switch mode {
	case "agy":
		payload := []byte(`{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"fixture","buckets":[{"id":"fixture","window":"5h","remaining_fraction":0.5,"reset_time":"` + stamp + `"}]}]}}}`)
		r, err := agy.New().ParseQuota(payload, c)
		if err != nil || len(r.Windows) != 1 {
			t.Fatalf("agy numeric-offset parse failed: %v, %v", r, err)
		}
		got = r.Windows[0].ResetsAt
	case "claude":
		payload := []byte(`{"num_turns":0,"duration_api_ms":0,"result":"You are currently using your subscription to power your Claude Code usage","usage_report":{"observed_at":"` + stamp + `","limits":[{"kind":"five_hour","percent":20,"resets_at":"` + stamp + `","observed_at":"` + stamp + `"},{"kind":"seven_day","percent":30,"resets_at":"` + stamp + `"}]}}`)
		r, err := claude.New().ParseQuota(payload, c)
		if err != nil || len(r.Windows) != 2 || r.ObservedAt == nil || !r.ObservedAt.Equal(want) || r.Windows[1].ObservedAt == nil || !r.Windows[1].ObservedAt.Equal(want) {
			t.Fatalf("claude numeric-offset parse failed: %v, %v", r, err)
		}
		got = r.Windows[0].ResetsAt
	case "claude-prose":
		payload := []byte(`{"num_turns":0,"duration_api_ms":0,"result":"You are currently using your subscription to power your Claude Code usage\nCurrent session: 20% used · resets Oct 1 at 1:00pm (Etc/UTC)\nCurrent week (all models): 30% used · resets Oct 1 at 1:00pm (Etc/UTC)"}`)
		r, err := claude.New().ParseQuota(payload, c)
		if err != nil || len(r.Windows) != 2 {
			t.Fatalf("claude named-zone prose parse failed: %v, %v", r, err)
		}
		got = r.Windows[0].ResetsAt
	case "window":
		var w providerquota.QuotaWindow
		if err := json.Unmarshal([]byte(`{"resets_at":"`+stamp+`","observed_at":"`+stamp+`"}`), &w); err != nil || w.ObservedAt == nil || !w.ObservedAt.Equal(want) {
			t.Fatalf("window timestamps: %#v, %v", w, err)
		}
		got = w.ResetsAt
	case "failure":
		var f providerquota.ReadFailure
		if err := json.Unmarshal([]byte(`{"at":"`+stamp+`"}`), &f); err != nil {
			t.Fatal(err)
		}
		got = &f.At
	case "record":
		var r providerquota.QuotaRecord
		if err := json.Unmarshal([]byte(`{"retrieved_at":"`+stamp+`","observed_at":"`+stamp+`","windows":[{"resets_at":"`+stamp+`","observed_at":"`+stamp+`"}],"failures":[{"at":"`+stamp+`"}]}`), &r); err != nil || r.ObservedAt == nil || !r.ObservedAt.Equal(want) || len(r.Windows) != 1 || len(r.Failures) != 1 {
			t.Fatalf("record timestamps: %#v, %v", r, err)
		}
		got = &r.RetrievedAt
	case "projection":
		var p providerquota.Projection
		if err := json.Unmarshal([]byte(`{"retrieved_at":"`+stamp+`","observed_at":"`+stamp+`"}`), &p); err != nil || p.ObservedAt == nil || !p.ObservedAt.Equal(want) {
			t.Fatalf("projection timestamps: %#v, %v", p, err)
		}
		got = &p.RetrievedAt
	case "lock":
		// A stale lock written by an older owner with a numeric-offset time:
		// breaking it must not decode that time through ambient Local.
		layout := providerquota.LayoutAt(t.TempDir())
		if err := os.MkdirAll(layout.Root, 0o700); err != nil {
			t.Fatal(err)
		}
		path := layout.LockFile("runtime-agy")
		if err := os.WriteFile(path, []byte(`{"pid":999999,"acquired_at":"`+stamp+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		old := want.Add(-time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		s, err := providerquota.NewStore(layout, providerquota.Options{Now: func() time.Time { return want }, OwnerAlive: func(int) bool { return false }})
		if err != nil {
			t.Fatal(err)
		}
		lock, err := s.Acquire(context.Background(), "runtime-agy")
		if err != nil || !lock.BrokeDeadOwner {
			t.Fatalf("stale dead-owner lock was not broken: %v %#v", err, lock)
		}
		got = &want
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
	if name := localName(); name != "" {
		t.Fatalf("ambient Local initialized from TZ fixture: %q", name)
	}
	if got == nil || !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("timestamp did not preserve UTC instant: %v", got)
	}
}

func TestRound3TimestampDecoderValidation(t *testing.T) {
	for _, raw := range []string{`"invalid"`, `123`, `true`, `{}`} {
		for _, field := range []string{"resets_at", "observed_at"} {
			var w providerquota.QuotaWindow
			if err := json.Unmarshal([]byte(`{"`+field+`":`+raw+`}`), &w); err == nil {
				t.Fatalf("accepted malformed %s timestamp %s", field, raw)
			}
		}
		for _, field := range []string{"retrieved_at", "observed_at"} {
			var r providerquota.QuotaRecord
			var p providerquota.Projection
			payload := []byte(`{"` + field + `":` + raw + `}`)
			if json.Unmarshal(payload, &r) == nil || json.Unmarshal(payload, &p) == nil {
				t.Fatalf("accepted malformed record/projection %s timestamp %s", field, raw)
			}
		}
		var f providerquota.ReadFailure
		if json.Unmarshal([]byte(`{"at":`+raw+`}`), &f) == nil {
			t.Fatalf("accepted malformed failure timestamp %s", raw)
		}
	}
	for _, raw := range []string{`{}`, `{"resets_at":null,"observed_at":null}`} {
		var w providerquota.QuotaWindow
		if err := json.Unmarshal([]byte(raw), &w); err != nil || w.ResetsAt != nil || w.ObservedAt != nil {
			t.Fatalf("invented missing timestamps: %#v, %v", w, err)
		}
	}
	stamp := "2026-10-01T15:00:00.125+02:00"
	got, err := providerquota.ParseTimestamp(&stamp)
	if err != nil || got == nil || !got.Equal(time.Date(2026, 10, 1, 13, 0, 0, 125000000, time.UTC)) || got.Location() != time.UTC {
		t.Fatalf("offset/fraction conversion changed instant: %v, %v", got, err)
	}
}
