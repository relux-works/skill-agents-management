package providerquota_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/internal/quotatest"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

// Wrapping the closed System erases its optional Reader without inventing
// methods on agentic.System itself.
type withoutReader struct{ agentic.System }
type countingReader struct {
	agentic.System
	calls *atomic.Int32
}

func (s countingReader) QuotaPlan(providerquota.Request) (providerquota.QuotaPlan, error) {
	s.calls.Add(1)
	return providerquota.QuotaPlan{Binary: "fixture", CwdPolicy: providerquota.ScratchCwd}, nil
}
func (s countingReader) ParseQuota([]byte, providerquota.ParseContext) (providerquota.QuotaRecord, error) {
	s.calls.Add(1)
	return providerquota.QuotaRecord{}, nil
}
func TestReaderOptionalDispatch(t *testing.T) {
	c := quotatest.Context(t, "sample", false)
	result, err := providerquota.Dispatch(withoutReader{claude.New()}, providerquota.Request{Context: c, Basis: "public-study"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan != nil || result.Record.State != providerquota.NotSupported || len(result.Record.Checked) != 0 || result.Record.Basis == "" || result.Record.Authenticated != providerquota.AuthUnknown {
		t.Fatal("optional absence is not truthful")
	}
	if _, err = providerquota.Dispatch(withoutReader{claude.New()}, providerquota.Request{Context: c}); err == nil {
		t.Fatal("unsupported without basis accepted")
	}
	var calls atomic.Int32
	result, err = providerquota.Dispatch(countingReader{claude.New(), &calls}, providerquota.Request{Context: c})
	if err != nil || result.Plan == nil || calls.Load() != 1 || result.Plan.Parse == nil {
		t.Fatal("Reader not dispatched to plan")
	}
}
func TestBuildPlanAllSystemsNeverCallsReader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	_, env := quotatest.BinaryEnv(t, "unused")
	for _, name := range []string{"codex", "claude", "muse", "gemini", "qwen", "pi"} {
		path := root + string(os.PathSeparator) + name
		if err := execfixture.WriteFile(path, []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env[0] = "PATH=" + root
	for _, id := range agentic.Default.IDs() {
		t.Run(string(id), func(t *testing.T) {
			sys, ok := agentic.Default.Lookup(id)
			if !ok {
				t.Fatal("registered id absent")
			}
			var calls atomic.Int32
			wrapped := countingReader{sys, &calls}
			registry := agentic.NewRegistry()
			if err := registry.Register(wrapped); err != nil {
				t.Fatal(err)
			}
			req := agentic.LaunchRequest{System: id, Model: agentic.Model{ID: "fixture", Effort: agentic.EffortSupportNone}, SystemModelIdentity: "fixture/fixture", Env: env, Prompt: []byte("fixture"), PromptPath: root + "/prompt", WorkDir: root}
			// A dry-run refusal is also process-free; models and optional transports
			// have their own acceptance suites. The Reader must never be consulted.
			_, _ = agentic.BuildPlan(registry, req, agentic.LaunchModeDryRun)
			if calls.Load() != 0 {
				t.Fatal("dry run invoked quota Reader")
			}
		})
	}
}
func TestReview01QuotaDoesNotChangeLaunchPlans(t *testing.T) {
	c := quotatest.Context(t, "claude", true)
	sys := claude.New()
	_, env := quotatest.BinaryEnv(t, "claude")
	registry := agentic.NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatal(err)
	}
	req := agentic.LaunchRequest{System: sys.ID(), Model: agentic.Model{ID: "fixture", Effort: agentic.EffortSupportNone}, Env: env, Prompt: []byte("fixture"), WorkDir: t.TempDir()}
	baseline, err := agentic.BuildPlan(registry, req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	store, err := providerquota.NewStore(providerquota.LayoutAt(t.TempDir()), providerquota.Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"absent", "exhausted", "stale", "failed"} {
		t.Run(state, func(t *testing.T) {
			if state != "absent" {
				r, _ := providerquota.Base(c, "claude-print-usage")
				r.State = providerquota.PercentOnly
				r.Windows = []providerquota.QuotaWindow{{ID: "session", Minutes: 300, UsedPercent: providerquota.Float(100), ObservedAt: providerquota.Time(c.ReadAt)}}
				if state == "stale" {
					r.Windows[0].ObservedAt = providerquota.Time(c.ReadAt.Add(-31 * time.Minute))
				}
				if state == "failed" {
					r, _ = providerquota.Failure(c, "claude-print-usage", "read_failure")
				} else {
					r, err = providerquota.Finish(r, c)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err = store.Write(c, r); err != nil {
					t.Fatal(err)
				}
			}
			got, err := agentic.BuildPlan(registry, req, agentic.LaunchModeDryRun)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, b) {
				t.Fatal("quota changed module launch plan")
			}
		})
	}
}
func TestReview07CachedReadNeverPlans(t *testing.T) {
	c := quotatest.Context(t, "sample", false)
	s, err := providerquota.NewStore(providerquota.LayoutAt(t.TempDir()), providerquota.Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := providerquota.Base(c, "session-push")
	r.State = providerquota.Exact
	r.Windows = []providerquota.QuotaWindow{{ID: "weekly", Minutes: 10080, UsedPercent: providerquota.Float(50), ObservedAt: providerquota.Time(c.ReadAt.Add(-31 * time.Minute))}}
	r, err = providerquota.Finish(r, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Write(c, r); err != nil {
		t.Fatal(err)
	}
	a, err := s.Read(c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if a.WindowsDigest != r.WindowsDigest || !reflect.DeepEqual(a, b) {
		t.Fatal("cached read refreshed expired record")
	}
}

// This fake consumer exercises the production lock/read/commit protocol only.
// It counts a fixture read, not a process start. Executor coverage is Lane 2.
func TestReview08OneFixtureReadPerTTLUnderConcurrency(t *testing.T) {
	c := quotatest.Context(t, "sample", false)
	store, err := providerquota.NewStore(providerquota.LayoutAt(t.TempDir()), providerquota.Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	var starts atomic.Int32
	var wg sync.WaitGroup
	result := make(chan providerquota.QuotaRecord, 2)
	errs := make(chan error, 2)
	read := func() (providerquota.QuotaRecord, error) {
		deadline := time.Now().Add(3 * time.Second)
		for {
			lock, err := store.Acquire(context.Background(), "runtime-sample")
			if errors.Is(err, providerquota.ErrLocked) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
				continue
			}
			if err != nil {
				return providerquota.QuotaRecord{}, err
			}
			defer lock.Release()
			cached, err := store.Read(c)
			if err == nil && c.RetrievedAt.Sub(cached.RetrievedAt) <= time.Duration(cached.TTLS)*time.Second {
				return cached, nil
			}
			if !errors.Is(err, providerquota.ErrAbsent) {
				return cached, err
			}
			starts.Add(1)
			r, err := providerquota.Base(c, "session-push")
			if err != nil {
				return r, err
			}
			r.State = providerquota.Exact
			r.Windows = []providerquota.QuotaWindow{{ID: "session", UsedPercent: providerquota.Float(20), ObservedAt: providerquota.Time(c.ReadAt)}}
			r, err = providerquota.Finish(r, c)
			if err != nil {
				return r, err
			}
			return r, store.Commit(lock, c, r)
		}
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := read(); result <- r; errs <- err }()
	}
	wg.Wait()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	a, b := <-result, <-result
	if starts.Load() != 1 || !reflect.DeepEqual(a, b) {
		t.Fatal("lock protocol duplicated fixture read")
	}
}
func TestReview09FailureDoesNotBreakDispatch(t *testing.T) {
	c := quotatest.Context(t, "agy", false)
	binary, env := quotatest.BinaryEnv(t, "agy")
	supported := agy.NewWithRuntime(agy.Runtime{Executable: binary, Version: agy.MinimumQuotaVersion})
	result, err := providerquota.Dispatch(supported, providerquota.Request{Context: c, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := result.Plan.Parse([]byte(`{"status":"ERROR","num_turns":0}`), c)
	if err == nil || failed.State != providerquota.Unavailable {
		t.Fatal("fixture failure not reported")
	}
	records := []providerquota.QuotaRecord{{State: providerquota.Exact}, {State: providerquota.PercentOnly}, failed}
	coverage := providerquota.Summarize(records)
	if coverage.Readable != 2 || coverage.Of != 3 || len(coverage.Unsupported) != 0 {
		t.Fatal("read failure poisoned coverage")
	}
}
func TestReview13CachedStoreIsReadOnly(t *testing.T) {
	c := quotatest.Context(t, "sample", false)
	layout := providerquota.LayoutAt(t.TempDir())
	s, err := providerquota.NewStore(layout, providerquota.Options{Now: func() time.Time { return c.RetrievedAt }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(c); !errors.Is(err, providerquota.ErrAbsent) {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.Root); !os.IsNotExist(err) {
		t.Fatal("cached read created state")
	}
	for _, sys := range []agentic.System{codex.New(), claude.New(), muse.New(), agy.NewWithRuntime(agy.Runtime{Executable: "fixture", Version: agy.MinimumQuotaVersion})} {
		if _, ok := sys.(providerquota.Reader); !ok {
			t.Fatalf("%T lacks declared refresh Reader", sys)
		}
	}
}
