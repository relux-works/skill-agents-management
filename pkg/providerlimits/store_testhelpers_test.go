package providerlimits

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"

	// The home rule for a harness is declared by that harness's agentic system
	// plugin, and DefaultProviderHome resolves through agentic.Default. Each
	// plugin registers itself in init(), so a test that asks for a home needs
	// it compiled in.
	//
	// ALL SIX are imported, not just the two this plane classifies. The four
	// others are what make the refusal arm reachable: gemini, agy, muse and
	// qwen declare neither a home variable nor a default, and a test set that
	// left them out would never exercise the branch where a guessed home would
	// be invisible.
	//
	// They are blank imports rather than hand-written fakes, because a fake
	// would carry its own copy of CODEX_HOME and ~/.codex and be the second
	// table this port removed.
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// homeEnvVarFor reads the environment variable a runtime's harness looks in,
// from the plugin that declares it. A test that spelled "CODEX_HOME" would be
// the shadow table this port removed, one test file over.
func homeEnvVarFor(t *testing.T, runtime string) string {
	t.Helper()
	declaration, ok := vendorplugin.Default.RuntimeDeclarationOf(vendorplugin.RuntimeID(runtime))
	if !ok {
		t.Fatalf("runtime %q is not declared, so no plugin names its home variable", runtime)
	}
	system, ok := agentic.Default.Lookup(declaration.System)
	if !ok {
		t.Fatalf("agentic system %q has no registered plugin", declaration.System)
	}
	variable := system.Capabilities().HomeEnvVar
	if variable == "" {
		t.Fatalf("agentic system %q declares no home environment variable", declaration.System)
	}
	return variable
}

// testClock is a manually advanced clock. Every store test drives time
// explicitly, because the lease lifecycle is entirely about instants.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(at time.Time) *testClock { return &testClock{now: at} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *testClock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// warnSink collects operator warnings so a test can assert what an operator would
// have been told, not merely what the state file says.
type warnSink struct {
	mu       sync.Mutex
	messages []string
}

func (w *warnSink) Warn(message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, message)
}

func (w *warnSink) All() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.messages...)
}

func (w *warnSink) Contains(substr string) bool {
	for _, message := range w.All() {
		if strings.Contains(message, substr) {
			return true
		}
	}
	return false
}

func (w *warnSink) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = nil
}

// stubLiveness is the injected run-owner arm.
type stubLiveness struct {
	mu    sync.Mutex
	alive bool
	known bool
}

func (s *stubLiveness) set(alive, known bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alive, s.known = alive, known
}

func (s *stubLiveness) Alive(Lease) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive, s.known
}

type storeFixture struct {
	store    *Store
	clock    *testClock
	warnings *warnSink
	liveness *stubLiveness
	identity Identity
	root     string
}

// newStoreFixture builds a store rooted in a scratch directory with a frozen
// clock. The identity is a fabricated provider home; nothing inside it exists,
// which is itself part of the contract — the home is never opened.
func newStoreFixture(t *testing.T, mutators ...func(*Options)) *storeFixture {
	t.Helper()
	root := t.TempDir()
	clock := newTestClock(time.Date(2026, 8, 1, 1, 53, 40, 0, time.UTC))
	warnings := &warnSink{}
	liveness := &stubLiveness{}
	opts := Options{
		Layout:      LayoutAt(root),
		Now:         clock.Now,
		Warn:        warnings.Warn,
		RunLiveness: liveness,
	}
	for _, mutate := range mutators {
		mutate(&opts)
	}
	store, err := NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	identity, err := IdentityFor(ProviderCodex, root+"/codex-home")
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	return &storeFixture{
		store:    store,
		clock:    clock,
		warnings: warnings,
		liveness: liveness,
		identity: identity,
		root:     root,
	}
}

// suppress drives a group to suppressed(step 0) through the production path: a
// real classification of the real captured Codex tail, converted into an
// Observation the module's own API produced.
func (f *storeFixture) suppress(t *testing.T, group string) {
	t.Helper()
	f.observeQuota(t, group, nil, "RUN-260801-c95402")
}

func (f *storeFixture) observeQuota(t *testing.T, group string, held *Lease, runID string) error {
	t.Helper()
	return f.observeQuotaFor(t, f.identity, group, held, runID)
}

// observeQuotaFor is observeQuota against an explicit identity, so a test can
// record a suppression on a second identity sharing the same store root.
func (f *storeFixture) observeQuotaFor(t *testing.T, identity Identity, group string, held *Lease, runID string) error {
	t.Helper()
	class := ClassifyCodexPrompt(CodexPromptResult{
		ExitCode: 1,
		Now:      f.clock.Now(),
		Log:      readFixture(t, "codex-usage-limit-RUN-260801-a714a6.log"),
	})
	obs, ok := class.QuotaObservation(Evidence{RunID: runID, Model: "gpt-5.6-luna"})
	if !ok {
		t.Fatal("the captured Codex tail must yield a provider-quota observation")
	}
	return f.store.Observe(identity, group, held, obs)
}

// escalateToMaxStep drives a group to the top of the ladder through the
// production path only: wait out each window, claim the probe, fail it. It is
// the shape a genuinely exhausted subscription produces over a day.
func (f *storeFixture) escalateToMaxStep(t *testing.T, group string) {
	t.Helper()
	f.suppress(t, group)
	maxStep := len(f.store.Ladder()) - 1
	for step := 1; step <= maxStep; step++ {
		f.clock.Set(f.nextProbeAt(t, group))
		runID := runIDForIndex(step)
		lease, granted := f.claim(t, group, runID)
		if !granted || !lease.Held() {
			t.Fatalf("escalating to step %d: the probe must be claimable at next_probe_at", step)
		}
		if err := f.observeQuota(t, group, &lease, runID); err != nil {
			t.Fatalf("escalating to step %d: Observe: %v", step, err)
		}
	}
	if got := f.record(t, group).BackoffStep; got != maxStep {
		t.Fatalf("fixture failed to reach the max step: step = %d, want %d", got, maxStep)
	}
}

func (f *storeFixture) record(t *testing.T, group string) GroupRecord {
	t.Helper()
	record, ok := f.store.GroupRecordFor(f.identity, group)
	if !ok {
		t.Fatalf("group %s has no record", group)
	}
	return record
}

// nextProbeAt and storedLease fail the test rather than panicking when the field
// is absent. A nil dereference in a test aborts the whole binary and hides every
// test after it, which would quietly weaken the mutation study.
func (f *storeFixture) nextProbeAt(t *testing.T, group string) time.Time {
	t.Helper()
	record := f.record(t, group)
	if record.NextProbeAt == nil {
		t.Fatalf("group %s has no next_probe_at: %+v", group, record)
	}
	return *record.NextProbeAt
}

func (f *storeFixture) storedLease(t *testing.T, group string) Lease {
	t.Helper()
	record := f.record(t, group)
	if record.ProbeLease == nil {
		t.Fatalf("group %s holds no probe lease: %+v", group, record)
	}
	return *record.ProbeLease
}

func (f *storeFixture) claim(t *testing.T, group, runID string) (Lease, bool) {
	t.Helper()
	claimant := Claimant{
		RunID:        runID,
		OwnerKind:    OwnerProcess,
		PID:          1,
		PIDStartTime: f.clock.Now(),
	}
	lease, granted, err := f.store.ClaimProbe(f.identity, group, claimant)
	if err != nil {
		t.Fatalf("ClaimProbe: %v", err)
	}
	return lease, granted
}

// claimConcurrently races n callers at one group.
//
// It reports leased grants and unleased grants separately, because the two mean
// very different things: a leased grant is the claim gate working, while an
// unleased grant means the group read as available and the gate was gone.
func (f *storeFixture) claimConcurrently(t *testing.T, group string, n int) (leased int, unleased int, leases []Lease) {
	t.Helper()
	return f.claimConcurrentlyFor(t, f.identity, group, n)
}

// claimConcurrentlyFor is claimConcurrently against an explicit identity, so a
// test can race callers at a foreign provider's group.
func (f *storeFixture) claimConcurrentlyFor(t *testing.T, identity Identity, group string, n int) (leased int, unleased int, leases []Lease) {
	t.Helper()
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		start  = make(chan struct{})
		claims = make([]Lease, n)
		flags  = make([]bool, n)
	)
	for i := range n {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			claimant := Claimant{
				RunID:        runIDForIndex(index),
				OwnerKind:    OwnerProcess,
				PID:          1000 + index,
				PIDStartTime: f.clock.Now(),
			}
			<-start
			lease, ok, err := f.store.ClaimProbe(identity, group, claimant)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("ClaimProbe(%d): %v", index, err)
				return
			}
			claims[index], flags[index] = lease, ok
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range n {
		switch {
		case flags[i] && claims[i].Held():
			leased++
			leases = append(leases, claims[i])
		case flags[i]:
			unleased++
		}
	}
	return leased, unleased, leases
}

func runIDForIndex(index int) string {
	const digits = "0123456789abcdef"
	return "RUN-260802-c" + string([]byte{digits[index/16], digits[index%16]}) + "0000"
}

func selfPID() int { return os.Getpid() }

// liveChild is a genuinely running process, used where a test must distinguish
// "the owner is alive" from "the owner is gone". A fabricated pid cannot make that
// distinction, and the renewal path is only reachable with a real one.
type liveChild struct {
	pid       int
	startTime time.Time
	cmd       *exec.Cmd
	once      sync.Once
}

func startLiveChild(t *testing.T) *liveChild {
	t.Helper()
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a live child process on this platform: %v", err)
	}
	child := &liveChild{pid: cmd.Process.Pid, cmd: cmd}
	start, err := ProcessStartTime(child.pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Skipf("cannot read a process start time on this platform: %v", err)
	}
	child.startTime = start
	t.Cleanup(func() { child.kill(t) })
	return child
}

func (c *liveChild) kill(t *testing.T) {
	t.Helper()
	c.once.Do(func() {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
		// Wait until the pid is genuinely gone, so a following liveness check is
		// not racing the reap.
		for range 200 {
			if !processAlive(c.pid) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}
