package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func fixtureBytes(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	fixture := map[string]any{
		"runtime_key":    "local-qwen@/home/op/project",
		"profile_digest": "deadbeef",
		"broker":         map[string]any{"state": "serving", "source": "attested"},
		"sharing":        map[string]any{"configured": map[string]any{"max_leases": 3}},
		"runtime":        map[string]any{"pid": 1, "start_time": "2026-08-29T10:00:00Z"},
		"leases":         []any{},
	}
	if mutate != nil {
		mutate(fixture)
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// scriptedRunner is the fake commandRunner every test in this file drives.
// It records the exact args it was called with and can be scripted to
// return fixed output, an error, or to hang until ctx is done.
type scriptedRunner struct {
	calls    int
	lastCtx  context.Context
	lastArgs []string

	output []byte
	err    error
	hang   bool
}

func (s *scriptedRunner) run(ctx context.Context, name string, args []string) ([]byte, error) {
	s.calls++
	s.lastCtx = ctx
	s.lastArgs = append([]string{name}, args...)
	if s.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.output, s.err
}

// TestCLIStatusReaderHappyPath drives a successful read end to end and
// checks the args passed to the subprocess.
func TestCLIStatusReaderHappyPath(t *testing.T) {
	runner := &scriptedRunner{output: fixtureBytes(t, nil)}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run))

	status, err := reader.Status(context.Background(), StatusQuery{
		Runtime:            "local-qwen",
		Model:              "qwen-3.8-27b-mlx-8bit",
		AgentsInfraProject: "/home/op/project",
		AgentsInfraProfile: "local-qwen",
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.BrokerState != "serving" || status.BrokerSource != SourceAttested {
		t.Fatalf("got (%s, %s)", status.BrokerState, status.BrokerSource)
	}
	wantArgs := []string{"agents-infra", "runtime", "status", "--project", "/home/op/project", "--profile", "local-qwen", "--json"}
	if len(runner.lastArgs) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", runner.lastArgs, wantArgs)
	}
	for i := range wantArgs {
		if runner.lastArgs[i] != wantArgs[i] {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, runner.lastArgs[i], wantArgs[i], runner.lastArgs)
		}
	}
}

// TestCLIStatusReaderSubprocessErrorIsARefusal is case 20: a non-zero exit
// (surfaced by the runner as an error) is a read failure, never a stale
// answer.
func TestCLIStatusReaderSubprocessErrorIsARefusal(t *testing.T) {
	runner := &scriptedRunner{err: errors.New("exit status 1")}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run))
	_, err := reader.Status(context.Background(), StatusQuery{})
	if !errors.Is(err, ErrStatusReadFailed) {
		t.Fatalf("err = %v, want ErrStatusReadFailed", err)
	}
}

// TestCLIStatusReaderTruncatedJSONIsADecodeFailure is case 20's second
// sub-case.
func TestCLIStatusReaderTruncatedJSONIsADecodeFailure(t *testing.T) {
	runner := &scriptedRunner{output: []byte(`{"runtime_key": "x"`)}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run))
	_, err := reader.Status(context.Background(), StatusQuery{})
	if !errors.Is(err, ErrDecodeFailure) {
		t.Fatalf("err = %v, want ErrDecodeFailure", err)
	}
}

// TestCLIStatusReaderCallerCtxDeadlineFiring is case 20's third sub-case: a
// fake runner that hangs past the ctx the caller supplied must return
// promptly once that ctx is done, not hang forever.
func TestCLIStatusReaderCallerCtxDeadlineFiring(t *testing.T) {
	runner := &scriptedRunner{hang: true}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run), WithTimeout(time.Hour)) // the reader's OWN timeout must not be what fires
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := reader.Status(ctx, StatusQuery{})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrStatusReadFailed) {
		t.Fatalf("err = %v, want ErrStatusReadFailed", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Status took %v to return after the caller's ctx deadline; it is not honoring the caller-supplied ctx", elapsed)
	}
}

// TestCLIStatusReaderOwnTimeoutFiring proves the reader's own
// statusCommandTimeout-shaped bound is real even when the caller's ctx
// carries no deadline of its own.
func TestCLIStatusReaderOwnTimeoutFiring(t *testing.T) {
	runner := &scriptedRunner{hang: true}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run), WithTimeout(20*time.Millisecond))
	start := time.Now()
	_, err := reader.Status(context.Background(), StatusQuery{})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrStatusReadFailed) {
		t.Fatalf("err = %v, want ErrStatusReadFailed", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Status took %v; the reader's own timeout did not bound it", elapsed)
	}
}

// TestCLIStatusReaderSuccessThenFailureNeverReusesTheStaleAnswer is case
// 20's fourth sub-case: a second call after a successful first must return
// its own fresh error, never the memoized prior success — memoization
// belongs only to local-models.toml's loader, never to a live read.
func TestCLIStatusReaderSuccessThenFailureNeverReusesTheStaleAnswer(t *testing.T) {
	runner := &scriptedRunner{output: fixtureBytes(t, nil)}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run))
	if _, err := reader.Status(context.Background(), StatusQuery{}); err != nil {
		t.Fatalf("first Status: %v", err)
	}
	runner.output = nil
	runner.err = errors.New("broker unreachable")
	_, err := reader.Status(context.Background(), StatusQuery{})
	if !errors.Is(err, ErrStatusReadFailed) {
		t.Fatalf("second Status: err = %v, want ErrStatusReadFailed (a fresh failure, not the first call's cached success)", err)
	}
}

// TestCLIStatusReaderResponseCapIsExact is case 24(a): a response exceeding
// status_response_max_bytes by one byte is a decode failure.
func TestCLIStatusReaderResponseCapIsExact(t *testing.T) {
	body := fixtureBytes(t, nil)
	t.Run("at the cap succeeds", func(t *testing.T) {
		runner := &scriptedRunner{output: body}
		reader := NewCLIStatusReader(WithCommandRunner(runner.run), WithResponseCap(len(body)))
		if _, err := reader.Status(context.Background(), StatusQuery{}); err != nil {
			t.Fatalf("at cap: %v", err)
		}
	})
	t.Run("one byte over the cap is refused", func(t *testing.T) {
		runner := &scriptedRunner{output: body}
		reader := NewCLIStatusReader(WithCommandRunner(runner.run), WithResponseCap(len(body)-1))
		_, err := reader.Status(context.Background(), StatusQuery{})
		if !errors.Is(err, ErrStatusReadFailed) {
			t.Fatalf("over cap: err = %v, want ErrStatusReadFailed", err)
		}
	})
}

// TestCLIStatusReaderObservedRingBufferEvictsOldest is case 24(b), narrowed
// with an off-by-one boundary: the ring buffer evicts exactly the oldest
// entry at the cap.
func TestCLIStatusReaderObservedRingBufferEvictsOldest(t *testing.T) {
	runner := &scriptedRunner{}
	reader := NewCLIStatusReader(WithCommandRunner(runner.run), WithObservedCap(2))

	states := []string{"starting", "serving", "lingering"}
	var lastObserved []Observation
	for _, state := range states {
		runner.output = fixtureBytes(t, func(f map[string]any) {
			f["broker"] = map[string]any{"state": state, "source": "attested"}
		})
		status, err := reader.Status(context.Background(), StatusQuery{})
		if err != nil {
			t.Fatalf("Status(%s): %v", state, err)
		}
		lastObserved = status.Observed
	}
	if len(lastObserved) != 2 {
		t.Fatalf("Observed has %d entries, want exactly 2 (the cap)", len(lastObserved))
	}
	if !containsSubstring(lastObserved[0].Detail, "serving") || !containsSubstring(lastObserved[1].Detail, "lingering") {
		t.Fatalf("Observed = %+v; want the two MOST RECENT entries (serving, lingering), oldest (starting) evicted", lastObserved)
	}
}

func containsSubstring(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
