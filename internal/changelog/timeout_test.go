package changelog_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestChangelogChildReportsNamedTimeout is the regression for unbounded test
// children: a fake child that blocks past the deadline (production call site
// runChangelogChild in bounded_test.go) must surface as a NAMED, typed
// timeout failure carrying the command, the elapsed time and the captured
// output — never as a hang, a kill, or an exit code. The child sleeps 45 s
// against a 5 s bound; sleep cannot finish early, and the 5 s bound leaves
// scheduling jitter far below the 30 s wall guard, so the guard cannot flake
// in either direction.
func TestChangelogChildReportsNamedTimeout(t *testing.T) {
	const timeout = 5 * time.Second
	const guard = 30 * time.Second
	start := time.Now()
	stdout, stderr, exit, err := runChangelogChild(t.TempDir(), fixtureEnv(t), timeout,
		"sh", "-c", "echo blocking-marker; sleep 45")
	elapsed := time.Since(start)
	timeoutErr, ok := asChangelogTimeout(err)
	if !ok {
		t.Fatalf("blocking child: want *changelogTimeoutError, got exit=%d err=%v (stdout %q stderr %q)",
			exit, err, stdout, stderr)
	}
	if exit == 0 {
		t.Errorf("blocking child: timeout reported exit 0; a timeout is never an exit code")
	}
	text := timeoutErr.Error()
	for _, want := range []string{"TIMEOUT", "sleep 45", "blocking-marker", changelogTimeoutSentinel} {
		if !strings.Contains(text, want) {
			t.Errorf("timeout names no %q:\n%s", want, text)
		}
	}
	if timeoutErr.elapsed < timeout {
		t.Errorf("timeout elapsed %s, want at least the %s bound", timeoutErr.elapsed, timeout)
	}
	if elapsed > guard {
		t.Errorf("blocking child took %s, want under %s", elapsed.Round(time.Millisecond), guard)
	}
	t.Logf("named timeout after %s (bound %s)", elapsed.Round(time.Millisecond), timeout)
}

// TestChangelogChildKillsDescendantAfterWaitDelay is the F2 regression: a
// child that exits while a same-group descendant holds the stdout pipe
// (`sh -c 'sleep 30 & echo $!'`) makes Run return on WaitDelay, well before
// the context deadline — and the descendant must still be reaped within the
// bound by the unconditional post-Run group cleanup (production call site
// runChangelogChild in bounded_test.go). Timing: the shell exits at once,
// WaitDelay returns at ~2 s, the bound is 3 s, the sleeper would live 30 s,
// and the liveness probe runs ~2 s after Run returned, so a surviving
// descendant is caught strictly past the deadline with seconds of margin on
// every edge. The deferred cleanup kills the captured pid even on failure,
// so a failing tree — or a mutant run that drops the cleanup — leaves no
// stray sleeper behind.
func TestChangelogChildKillsDescendantAfterWaitDelay(t *testing.T) {
	if !changelogProcessGroupsSupported() {
		t.Skip("post-Run group cleanup needs unix process groups")
	}
	const timeout = 3 * time.Second
	start := time.Now()
	stdout, stderr, exit, err := runChangelogChild(t.TempDir(), fixtureEnv(t), timeout,
		"sh", "-c", "sleep 30 & echo $!")
	pid := strings.TrimSpace(stdout)
	if pid == "" {
		t.Fatalf("descendant probe: no pid captured (exit %d err %v stdout %q stderr %q)",
			exit, err, stdout, stderr)
	}
	defer func() {
		_, _, _, _ = runChangelogChild("", fixtureEnv(t), 10*time.Second,
			"sh", "-c", "kill \"$1\" 2>/dev/null", "cleanup", pid)
	}()
	t.Logf("descendant probe: Run returned after %s (exit %d err %v)",
		time.Since(start).Round(time.Millisecond), exit, err)
	time.Sleep(2 * time.Second)
	_, _, code, probeErr := runChangelogChild("", fixtureEnv(t), 10*time.Second,
		"sh", "-c", "kill -0 \"$1\"", "probe", pid)
	if probeErr != nil {
		t.Fatalf("descendant probe: %v", probeErr)
	}
	if code == 0 {
		t.Fatalf("descendant %s still alive %s after the %s bound (a WaitDelay return must not strand a same-group descendant)",
			pid, time.Since(start).Round(time.Millisecond), timeout)
	}
	t.Logf("descendant %s reaped within the %s bound", pid, timeout)
}

// TestChangelogChildForeverLoopReportsNamedTimeout is the literal
// forever-blocking regression: a child that never terminates on its own
// (`while :; do sleep 1; done`) must surface as the same NAMED, typed
// timeout failure as the finite sleeper — command, elapsed time, captured
// output — within the bound. Production call site runChangelogChild in
// bounded_test.go.
func TestChangelogChildForeverLoopReportsNamedTimeout(t *testing.T) {
	const timeout = 5 * time.Second
	const guard = 30 * time.Second
	start := time.Now()
	stdout, stderr, exit, err := runChangelogChild(t.TempDir(), fixtureEnv(t), timeout,
		"sh", "-c", "echo forever-marker; while :; do sleep 1; done")
	elapsed := time.Since(start)
	timeoutErr, ok := asChangelogTimeout(err)
	if !ok {
		t.Fatalf("forever loop: want *changelogTimeoutError, got exit=%d err=%v (stdout %q stderr %q)",
			exit, err, stdout, stderr)
	}
	if exit == 0 {
		t.Errorf("forever loop: timeout reported exit 0; a timeout is never an exit code")
	}
	text := timeoutErr.Error()
	for _, want := range []string{"TIMEOUT", "while :", "forever-marker", changelogTimeoutSentinel} {
		if !strings.Contains(text, want) {
			t.Errorf("timeout names no %q:\n%s", want, text)
		}
	}
	if timeoutErr.elapsed < timeout {
		t.Errorf("timeout elapsed %s, want at least the %s bound", timeoutErr.elapsed, timeout)
	}
	if elapsed > guard {
		t.Errorf("forever loop took %s, want under %s", elapsed.Round(time.Millisecond), guard)
	}
	t.Logf("named timeout after %s (bound %s)", elapsed.Round(time.Millisecond), timeout)
}

// TestChangelogChildTimeoutEnvOverride pins the slow-host escape hatch: the
// CHANGELOG_TEST_CHILD_TIMEOUT duration overrides the per-run deadline, and
// empty, unparsable or non-positive values fall back to the default.
func TestChangelogChildTimeoutEnvOverride(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", defaultChangelogChildTimeout},
		{"duration", "90s", 90 * time.Second},
		{"minutes", "3m", 3 * time.Minute},
		{"unparsable", "soon", defaultChangelogChildTimeout},
		{"zero", "0", defaultChangelogChildTimeout},
		{"negative", "-5s", defaultChangelogChildTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(changelogChildTimeoutEnv, c.value)
			if got := changelogChildTimeout(); got != c.want {
				t.Errorf("changelogChildTimeout() with %s=%q = %s, want %s",
					changelogChildTimeoutEnv, c.value, got, c.want)
			}
		})
	}
	t.Run("env-var-bounds-child", func(t *testing.T) {
		t.Setenv(changelogChildTimeoutEnv, "100ms")
		start := time.Now()
		_, _, _, err := runChangelogChild(t.TempDir(), fixtureEnv(t), changelogChildTimeout(),
			"sh", "-c", "sleep 30")
		if _, ok := asChangelogTimeout(err); !ok {
			t.Fatalf("100ms env bound: want *changelogTimeoutError, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Errorf("100ms env bound took %s", elapsed.Round(time.Millisecond))
		}
	})
}

// TestFixtureEnvIsolatesGit pins the non-interactive fixture contract: hostile
// inherited values are stripped or overridden, prompts cannot block (no
// terminal, pager, editor or credential prompt), and no system, global or XDG
// configuration is read (isolated HOME/XDG_CONFIG_HOME).
func TestFixtureEnvIsolatesGit(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/host/global")
	t.Setenv("GIT_PAGER", "less")
	t.Setenv("SSH_ASKPASS", "/host/askpass")
	t.Setenv("SSH_ASKPASS_REQUIRE", "force")
	t.Setenv("TASK_BOARD_DIR", "/host/board")
	t.Setenv("BASH_ENV", "/host/env")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	env := fixtureEnv(t)
	last := map[string]string{}
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			last[key] = value
		}
	}
	for key, want := range map[string]string{
		"GIT_CONFIG_COUNT":    "0",
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_SYSTEM":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_PAGER":           "cat",
		"PAGER":               "cat",
		"GIT_EDITOR":          "true",
		"LC_ALL":              "C",
	} {
		if got, ok := last[key]; !ok || got != want {
			t.Errorf("fixtureEnv %s = %q, want %q", key, got, want)
		}
	}
	home, ok := last["HOME"]
	if !ok || home == "" || home == os.Getenv("HOME") {
		t.Errorf("fixtureEnv HOME = %q, want an isolated dir", home)
	} else if st, err := os.Stat(home); err != nil || !st.IsDir() {
		t.Errorf("fixtureEnv HOME = %q, want an existing dir: %v", home, err)
	}
	if last["XDG_CONFIG_HOME"] != home {
		t.Errorf("fixtureEnv XDG_CONFIG_HOME = %q, want the isolated %q", last["XDG_CONFIG_HOME"], home)
	}
	for key, value := range last {
		if strings.HasPrefix(key, "GIT_") {
			switch key {
			case "GIT_CONFIG_COUNT", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM",
				"GIT_CONFIG_NOSYSTEM", "GIT_TERMINAL_PROMPT", "GIT_PAGER", "GIT_EDITOR":
			default:
				t.Errorf("fixtureEnv leaks %s=%q", key, value)
			}
		}
		if strings.HasPrefix(key, "TASK_BOARD_") || key == "BASH_ENV" ||
			key == "SSH_ASKPASS" || key == "SSH_ASKPASS_REQUIRE" {
			t.Errorf("fixtureEnv leaks %s=%q", key, value)
		}
	}
}

// TestBoundedChildStdinIsDevNull proves children meet EOF on stdin, never the
// test process's input: a child that prompts reads EOF and continues.
func TestBoundedChildStdinIsDevNull(t *testing.T) {
	stdout, stderr, exit, err := runChangelogChild(t.TempDir(), fixtureEnv(t), 30*time.Second,
		"sh", "-c", "if read -r line; then echo \"READ:$line\"; else echo EOF; fi")
	if err != nil {
		t.Fatalf("stdin probe: %v", err)
	}
	if exit != 0 || !strings.Contains(stdout, "EOF") {
		t.Errorf("stdin probe exit %d, want EOF on /dev/null stdin: stdout %q stderr %q", exit, stdout, stderr)
	}
}

// TestChangelogChildDeadlineMutantsKilled proves the deadline is load-bearing
// and correctly sized. Each mutant splices exactly one line of
// runChangelogChild via a go overlay (the splice's only change) and must be
// killed by the named witness TestChangelogChildReportsNamedTimeout:
//   - no-deadline removes the deadline (WithTimeout -> WithCancel): the
//     blocking child runs its full 45 s sleep and trips the witness's wall
//     guard. A passing witness here means the deadline was decorative.
//   - deadline-relaxed-10x keeps the gate but weakens it tenfold: the 45 s
//     sleep completes inside the 50 s bound, so no timeout fires and the
//     witness fails on the missing typed error. This is the narrowing
//     direction: the gate stays present and admits the over-long wait.
func TestChangelogChildDeadlineMutantsKilled(t *testing.T) {
	const witness = "TestChangelogChildReportsNamedTimeout"
	mutants := []struct{ id, replace string }{
		{"no-deadline", "ctx, cancel := context.WithCancel(context.Background())"},
		{"deadline-relaxed-10x", "ctx, cancel := context.WithTimeout(context.Background(), 10*timeout)"},
	}
	for _, m := range mutants {
		t.Run(m.id, func(t *testing.T) {
			// Deliberately sequential: one overlay mutant run at a time.
			runDeadlineMutant(t, m.id, m.replace, witness)
		})
	}
}

// runOverlayMutant splices splices into file via a go overlay (the splices'
// only change), runs the package with -run runPattern under the per-run
// deadline, and returns the combined output and exit code for the caller to
// assert on. Each splice anchor must occur exactly once. A broken splice
// yields a loud failure here, never a false kill.
func runOverlayMutant(t *testing.T, id, file string, splices [][2]string, runPattern string) (string, int) {
	t.Helper()
	root := repoRoot(t)
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%s mutant: reading %s: %v", id, file, err)
	}
	mutated := source
	for _, splice := range splices {
		if n := bytes.Count(mutated, []byte(splice[0])); n != 1 {
			t.Fatalf("%s mutant: anchor count %d, want 1 (%q)", id, n, splice[0])
		}
		mutated = bytes.Replace(mutated, []byte(splice[0]), []byte(splice[1]), 1)
	}
	directory := t.TempDir()
	mutantPath := filepath.Join(directory, "mutant.go")
	if err := os.WriteFile(mutantPath, mutated, 0o600); err != nil {
		t.Fatalf("%s mutant: writing splice: %v", id, err)
	}
	overlay, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{file: mutantPath}})
	if err != nil {
		t.Fatalf("%s mutant: encoding overlay: %v", id, err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatalf("%s mutant: writing overlay: %v", id, err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("%s mutant: go binary not on PATH: %v", id, err)
	}
	// The mutant run itself is a bounded child (deadline, process-group kill,
	// WaitDelay); it inherits the real environment so the go toolchain keeps
	// its cache, home and module configuration.
	stdout, stderr, exit, err := runChangelogChild(root, os.Environ(), changelogChildTimeout(),
		"go", "test", "-mod=mod", "-count=1", "-v", "-overlay", overlayPath, "-run", runPattern, "./internal/changelog")
	if err != nil {
		t.Fatalf("%s mutant: mutant run failed to complete: %v", id, err)
	}
	return stdout + stderr, exit
}

// TestChangelogChildElapsedStartMutantKilled makes setup skew deterministic.
// Both real timeout witnesses must pass with 100ms of setup after the deadline
// is armed. Moving only the measurement start past that setup must fail their
// elapsed lower bounds; the deadline and typed timeout remain intact.
func TestChangelogChildElapsedStartMutantKilled(t *testing.T) {
	witnesses := []string{
		"TestChangelogChildReportsNamedTimeout",
		"TestChangelogChildForeverLoopReportsNamedTimeout",
	}
	pattern := "^(" + strings.Join(witnesses, "|") + ")$"
	file := filepath.Join(repoRoot(t), "internal", "changelog", "bounded_test.go")
	const run = "\trunErr := runChangelogRegistered(cmd)\n"
	const delayedRun = "\ttime.Sleep(100 * time.Millisecond)\n" + run
	control, exit := runOverlayMutant(t, "elapsed-setup-delay-control", file,
		[][2]string{{run, delayedRun}}, pattern)
	if exit != 0 {
		t.Fatalf("setup-delay control exit %d, want 0:\n%s", exit, control)
	}
	for _, witness := range witnesses {
		if !strings.Contains(control, "--- PASS: "+witness) {
			t.Fatalf("setup-delay control lacks named PASS for %s:\n%s", witness, control)
		}
	}
	mutant, exit := runOverlayMutant(t, "elapsed-start-after-setup", file,
		[][2]string{
			{"\tstart := time.Now()\n", ""},
			{run, "\ttime.Sleep(100 * time.Millisecond)\n\tstart := time.Now()\n" + run},
		}, pattern)
	if exit != 1 || !strings.Contains(mutant, "timeout elapsed") {
		t.Fatalf("elapsed-start-after-setup lacks elapsed-bound failure (exit %d, want 1):\n%s", exit, mutant)
	}
	for _, witness := range witnesses {
		if !strings.Contains(mutant, "--- FAIL: "+witness) {
			t.Fatalf("elapsed-start-after-setup mutant SURVIVED %s (exit %d):\n%s", witness, exit, mutant)
		}
		t.Logf("elapsed-start-after-setup mutant killed by %s (exit %d, expected-red); setup-delay control exit 0", witness, exit)
	}
}

func runDeadlineMutant(t *testing.T, id, replace, witness string) {
	t.Helper()
	file := filepath.Join(repoRoot(t), "internal", "changelog", "bounded_test.go")
	const find = "ctx, cancel := context.WithTimeout(context.Background(), timeout)"
	combined, exit := runOverlayMutant(t, id, file, [][2]string{{find, replace}}, "^"+witness+"$")
	if exit == 0 || !strings.Contains(combined, "--- FAIL: "+witness) {
		t.Fatalf("%s mutant SURVIVED behavioral test %s (exit %d):\n%s", id, witness, exit, combined)
	}
	t.Logf("%s mutant killed by %s (exit %d, expected-red)", id, witness, exit)
}

// TestChangelogPostRunCleanupMutantsKilled proves the unconditional post-Run
// group cleanup is load-bearing. Each mutant splices runChangelogChild via a
// go overlay (the splice's only change); the witness command runs the F2
// regression plus the plain deadline regression:
//   - post-run-cleanup-removed drops the post-Run group kill: the F2
//     descendant survives past the bound and the F2 witness must fail, while
//     the plain deadline witness still passes (the deadline path is
//     independently guarded by cmd.Cancel, so the mutant weakens only the
//     non-deadline returns — it does not break deadline behavior).
//   - post-run-cleanup-only-on-deadline keeps the cleanup but conditions it
//     on the expired deadline (the obvious weaker gate): a WaitDelay return
//     happens before the deadline, so the F2 descendant still survives and
//     the F2 witness must fail, while the plain deadline witness still
//     passes. This maps the boundary: the cleanup must be unconditional.
func TestChangelogPostRunCleanupMutantsKilled(t *testing.T) {
	if !changelogProcessGroupsSupported() {
		t.Skip("post-Run group cleanup needs unix process groups")
	}
	const (
		descendant = "TestChangelogChildKillsDescendantAfterWaitDelay"
		deadline   = "TestChangelogChildReportsNamedTimeout"
	)
	const cleanupCall = "\tcleanupChangelogChildProcessGroup(cmd)\n"
	mutants := []struct{ id, replace string }{
		{"post-run-cleanup-removed", "\t// post-Run group cleanup removed\n"},
		{"post-run-cleanup-only-on-deadline", "\tif ctx.Err() == context.DeadlineExceeded {\n\t\tcleanupChangelogChildProcessGroup(cmd)\n\t}\n"},
	}
	for _, m := range mutants {
		t.Run(m.id, func(t *testing.T) {
			// Deliberately sequential: one overlay mutant run at a time.
			file := filepath.Join(repoRoot(t), "internal", "changelog", "bounded_test.go")
			combined, _ := runOverlayMutant(t, m.id, file,
				[][2]string{{cleanupCall, m.replace}},
				"^("+descendant+"|"+deadline+")$")
			descendantFailed := strings.Contains(combined, "--- FAIL: "+descendant)
			deadlinePassed := strings.Contains(combined, "--- PASS: "+deadline)
			if !descendantFailed || !deadlinePassed {
				t.Fatalf("%s mutant: unexpected witness split (descendant FAIL=%t want true, deadline PASS=%t want true):\n%s",
					m.id, descendantFailed, deadlinePassed, combined)
			}
			t.Logf("%s mutant killed by the named witness split (expected-red)", m.id)
		})
	}
}

// runConcurrencyProbe runs contenders children (each `sh -c script`) at once
// through the production call site runChangelogChild in bounded_test.go and
// returns the peak simultaneous hold count observed through
// changelogChildSpanHook — which brackets exactly the semaphore hold window
// (after acquire, before release), i.e. the child lifetime. It also returns
// one line per child that did not exit 0 cleanly; a probe with failures
// proves nothing about the peak. The caller sets the cap env var first.
func runConcurrencyProbe(t *testing.T, contenders int, script string) (int32, []string) {
	t.Helper()
	var current, peak atomic.Int32
	prev := changelogChildSpanHook
	changelogChildSpanHook = func() func() {
		n := current.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		return func() { current.Add(-1) }
	}
	defer func() { changelogChildSpanHook = prev }()
	dir := t.TempDir()
	env := fixtureEnv(t)
	var wg sync.WaitGroup
	type result struct {
		exit int
		err  error
	}
	results := make([]result, contenders)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, exit, err := runChangelogChild(dir, env, 60*time.Second, "sh", "-c", script)
			results[i] = result{exit: exit, err: err}
		}()
	}
	wg.Wait()
	var failures []string
	for i, r := range results {
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("child %d: %v", i, r.err))
		} else if r.exit != 0 {
			failures = append(failures, fmt.Sprintf("child %d: exit %d", i, r.exit))
		}
	}
	return peak.Load(), failures
}

// TestChangelogChildConcurrencyCap is the aggregate fan-out regression: six
// contenders against a cap of two must never hold more than two children at
// once, and must still reach two (the cap admits the full allowance). No
// wall-clock assertion: the 2 s sleeps dwarf spawn jitter by three orders
// of magnitude, so a peak of 3 or 6 under a weakened gate is unambiguous in
// either direction. Deliberately sequential (no t.Parallel; t.Setenv is
// process-wide): it runs in the exclusive sequential phase, so no other
// test's children compete for the gate while it measures.
func TestChangelogChildConcurrencyCap(t *testing.T) {
	t.Setenv(changelogChildConcurrencyEnv, "2")
	peak, failures := runConcurrencyProbe(t, 6, "sleep 2")
	if len(failures) > 0 {
		t.Fatalf("concurrency probe: %d children failed:\n%s", len(failures), strings.Join(failures, "\n"))
	}
	if peak != 2 {
		t.Fatalf("concurrency probe: peak %d simultaneous children, want exactly 2", peak)
	}
	t.Logf("concurrency probe: peak %d of 6 contenders at cap 2", peak)
}

// TestChangelogChildConcurrencyEnvOverride pins the cap override: the
// CHANGELOG_TEST_CHILD_CONCURRENCY integer overrides the package-wide cap,
// and empty, unparsable or non-positive values fall back to the default.
// The live subtest proves the override binds real children (a cap of 1
// serializes three sleepers to a peak of 1); parsing alone would not prove
// the gate reads it.
func TestChangelogChildConcurrencyEnvOverride(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"empty", "", defaultChangelogChildConcurrency},
		{"one", "1", 1},
		{"four", "4", 4},
		{"unparsable", "many", defaultChangelogChildConcurrency},
		{"zero", "0", defaultChangelogChildConcurrency},
		{"negative", "-2", defaultChangelogChildConcurrency},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(changelogChildConcurrencyEnv, c.value)
			if got := changelogChildMaxConcurrent(); got != c.want {
				t.Errorf("changelogChildMaxConcurrent() with %s=%q = %d, want %d",
					changelogChildConcurrencyEnv, c.value, got, c.want)
			}
		})
	}
	t.Run("env-var-bounds-children", func(t *testing.T) {
		t.Setenv(changelogChildConcurrencyEnv, "1")
		peak, failures := runConcurrencyProbe(t, 3, "sleep 1")
		if len(failures) > 0 {
			t.Fatalf("cap-1 probe: %d children failed:\n%s", len(failures), strings.Join(failures, "\n"))
		}
		if peak != 1 {
			t.Fatalf("cap-1 probe: peak %d simultaneous children, want exactly 1", peak)
		}
	})
}

// TestChangelogChildOutputCapped is the bounded-capture regression: small
// outputs pass through exactly, while a 3 MiB stream is retained at the
// 1 MiB cap keeping its head and tail with a truncation marker between
// them. Production call site runChangelogChild in bounded_test.go. The
// length bound is a LITERAL: the overlaid mutant sources redefine the
// constant, so asserting against it would move the goalpost with the
// mutant.
func TestChangelogChildOutputCapped(t *testing.T) {
	dir := t.TempDir()
	env := fixtureEnv(t)
	stdout, stderr, exit, err := runChangelogChild(dir, env, 30*time.Second,
		"sh", "-c", "echo small-out; echo small-err >&2")
	if err != nil || exit != 0 || stdout != "small-out\n" || stderr != "small-err\n" {
		t.Fatalf("small output must pass through exactly: exit %d err %v stdout %q stderr %q",
			exit, err, stdout, stderr)
	}
	const script = "echo HEAD-OUT; head -c 3145728 /dev/zero | tr '\\0' 'o'; echo; echo TAIL-OUT; " +
		"echo HEAD-ERR >&2; head -c 3145728 /dev/zero | tr '\\0' 'e' >&2; echo >&2; echo TAIL-ERR >&2"
	stdout, stderr, exit, err = runChangelogChild(dir, env, 60*time.Second, "sh", "-c", script)
	if err != nil || exit != 0 {
		t.Fatalf("flood run: exit %d err %v", exit, err)
	}
	// 9 + 3MiB + 1 + 9 bytes per stream; the literal bound is the 1 MiB
	// cap plus marker slack.
	const wantTotal = "of 3145747 total"
	const wantMax = 1<<20 + 4096
	for name, triple := range map[string][3]string{
		"stdout": {stdout, "HEAD-OUT", "TAIL-OUT"},
		"stderr": {stderr, "HEAD-ERR", "TAIL-ERR"},
	} {
		out, head, tail := triple[0], triple[1], triple[2]
		if len(out) > wantMax {
			t.Errorf("%s retained %d bytes, want at most %d", name, len(out), wantMax)
		}
		for _, want := range []string{head, tail, "truncated", wantTotal} {
			if !strings.Contains(out, want) {
				t.Errorf("%s retains no %q (retained %d bytes)", name, want, len(out))
			}
		}
	}
	t.Logf("flood retained %d stdout + %d stderr bytes at the 1 MiB cap", len(stdout), len(stderr))
}

// TestChangelogConcurrencyMutantsKilled proves the package-wide admission
// cap is load-bearing. Each mutant splices bounded_test.go via a go overlay
// (the splice's only change) and must be killed by the named witness
// TestChangelogChildConcurrencyCap:
//   - concurrency-cap-removed drops the acquire/release pair: all six
//     contenders hold children at once and the peak-2 assertion must fail.
//   - concurrency-cap-plus-one keeps the gate but admits exactly one more
//     member (>= to >): three contenders overlap and the same assertion
//     must fail. This maps the boundary: the gate must admit exactly the
//     cap.
func TestChangelogConcurrencyMutantsKilled(t *testing.T) {
	const witness = "TestChangelogChildConcurrencyCap"
	mutants := []struct {
		id      string
		splices [][2]string
	}{
		{"concurrency-cap-removed", [][2]string{
			{"\tchangelogChildAcquire()\n", "\t// package-wide admission removed\n"},
			{"\tdefer changelogChildRelease()\n", "\t// package-wide release removed\n"},
		}},
		{"concurrency-cap-plus-one", [][2]string{
			{"\tfor g.active >= changelogChildMaxConcurrent() {\n", "\tfor g.active > changelogChildMaxConcurrent() {\n"},
		}},
	}
	for _, m := range mutants {
		t.Run(m.id, func(t *testing.T) {
			// Deliberately sequential: one overlay mutant run at a time.
			file := filepath.Join(repoRoot(t), "internal", "changelog", "bounded_test.go")
			combined, exit := runOverlayMutant(t, m.id, file, m.splices, "^"+witness+"$")
			if exit == 0 || !strings.Contains(combined, "--- FAIL: "+witness) {
				t.Fatalf("%s mutant SURVIVED behavioral test %s (exit %d):\n%s", m.id, witness, exit, combined)
			}
			t.Logf("%s mutant killed by %s (exit %d, expected-red)", m.id, witness, exit)
		})
	}
}

// TestChangelogOutputCapMutantsKilled proves the per-stream capture cap is
// load-bearing. Each mutant splices bounded_test.go via a go overlay (the
// splice's only change) and must be killed by the named witness
// TestChangelogChildOutputCapped:
//   - output-cap-removed swaps the capped buffers for unbounded ones: the
//     3 MiB flood is retained whole with no marker, so the length and
//     marker assertions must fail.
//   - output-cap-doubled keeps the gate but weakens the bound twofold: the
//     flood still truncates (head, tail and marker all present) and only
//     the literal length assertion must fail. This is the narrowing
//     direction: the gate stays present and admits the over-cap member.
func TestChangelogOutputCapMutantsKilled(t *testing.T) {
	const witness = "TestChangelogChildOutputCapped"
	mutants := []struct {
		id      string
		splices [][2]string
	}{
		{"output-cap-removed", [][2]string{
			{"\tstdout := newChangelogCappedBuffer(changelogChildStreamCap)\n\tstderr := newChangelogCappedBuffer(changelogChildStreamCap)\n",
				"\tstdout := bytes.NewBuffer(nil)\n\tstderr := bytes.NewBuffer(nil)\n"},
		}},
		{"output-cap-doubled", [][2]string{
			{"const changelogChildStreamCap = 1 << 20", "const changelogChildStreamCap = 2 << 20"},
		}},
	}
	for _, m := range mutants {
		t.Run(m.id, func(t *testing.T) {
			// Deliberately sequential: one overlay mutant run at a time.
			file := filepath.Join(repoRoot(t), "internal", "changelog", "bounded_test.go")
			combined, exit := runOverlayMutant(t, m.id, file, m.splices, "^"+witness+"$")
			if exit == 0 || !strings.Contains(combined, "--- FAIL: "+witness) {
				t.Fatalf("%s mutant SURVIVED behavioral test %s (exit %d):\n%s", m.id, witness, exit, combined)
			}
			t.Logf("%s mutant killed by %s (exit %d, expected-red)", m.id, witness, exit)
		})
	}
}
