package providerlimits

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Limit state is an optimisation and must never be able to prevent a spawn. Every
// row of the fail-open table is exercised here, and every one of them ends with a
// spawn that could still run.
func TestMissingFileReadsAllAvailableSilently(t *testing.T) {
	f := newStoreFixture(t)
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000b0"); !granted {
		t.Fatal("a machine with no state must admit everything")
	}
	if got := f.warnings.All(); len(got) != 0 {
		t.Fatalf("a missing file must be silent, got %v", got)
	}
}

// A corrupt file is quarantined and its records are LOST, which is not the same
// fact as their never having existed. The tombstone the quarantine leaves is
// asserted in lost_state_test.go; this test pins the quarantine mechanics.
func TestCorruptFileIsQuarantinedAndItsRecordsForgotten(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	path := f.store.Layout().StateFile(f.identity.Key)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A read does not quarantine: reads never write.
	if _, exists := f.store.GroupRecordFor(f.identity, codexPlan); exists {
		t.Fatal("a corrupt file must read as empty")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 0 {
		t.Fatal("a read must not quarantine, because reads must not mutate state")
	}

	// A write quarantines and continues.
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000b1"); err != nil {
		t.Fatalf("Observe over a corrupt file: %v", err)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("corrupt-file quarantine copies = %v (%v), want exactly one", matches, err)
	}
	if !f.warnings.Contains("quarantined as") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
	if got := f.record(t, codexPlan).BackoffStep; got != 0 {
		t.Fatalf("backoff step = %d, want 0: a quarantined file starts over", got)
	}
	// The escalation is forgotten, but the LOSS is not: the rewritten file still
	// reports an indeterminate read, so nothing downstream may read its group
	// map as a complete statement about the provider.
	if read := f.store.LoadIdentityState(f.identity).Read; read != StateReadDegraded {
		t.Fatalf("state read after a quarantine = %q, want %q", read, StateReadDegraded)
	}
}

// A newer schema is ignored, never migrated downward, and never overwritten.
func TestNewerSchemaIsIgnoredAndNeverOverwritten(t *testing.T) {
	f := newStoreFixture(t)
	path := f.store.Layout().StateFile(f.identity.Key)
	future := []byte(`{"version":99,"identity":"` + f.identity.Key + `","provider":"codex","groups":{"codex-plan":{"state":"suppressed","backoff_step":4}}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatal(err)
	}

	state := f.store.LoadIdentityState(f.identity)
	if !state.ReadOnly {
		t.Fatal("a newer file must be marked read-only")
	}
	if len(state.Groups) != 0 {
		t.Fatal("a newer file must be treated as empty")
	}
	// An empty state suppresses nothing, so the spawn proceeds.
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000b2"); !granted {
		t.Fatal("a newer file must not be able to subtract a group")
	}
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000b3"); !errors.Is(err, ErrReadOnlyState) {
		t.Fatalf("Observe = %v, want ErrReadOnlyState", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(future) {
		t.Fatalf("the newer file was rewritten:\n before %s\n after  %s", future, after)
	}
	if !f.warnings.Contains("newer than this binary understands") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
}

// A caller that cannot take the lock within the bounded wait proceeds on a stale
// read and skips its write, so it cannot hold a lease. It therefore treats
// probe_eligible as NOT claimable: fail-open means "do not gain a probe", never
// "probe anyway".
func TestLockTimeoutIsNotGrantedAClaim(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) { o.LockTimeout = 40 * time.Millisecond })
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	// Hold the identity lock from outside, as another process would.
	lockPath := f.store.Layout().LockFile(f.identity.Key)
	held, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	info := lockInfo{PID: selfPID(), AcquiredAt: f.clock.Now()}
	payload, _ := json.Marshal(info)
	_, _ = held.Write(payload)
	_ = held.Close()
	defer func() { _ = os.Remove(lockPath) }()

	lease, granted := f.claim(t, codexPlan, "RUN-260802-0000b4")
	if granted || lease.Held() {
		t.Fatal("a caller that timed out on the lock must NOT be granted a claim")
	}
	if !f.warnings.Contains("lock busy") {
		t.Errorf("warnings = %v", f.warnings.All())
	}
	// The group is untouched: the refusal wrote nothing.
	if got := f.record(t, codexPlan).State; got == StateProbing {
		t.Fatal("a refused claim must not have written a lease")
	}

	// An observation under a busy lock is lost, not fatal, and never blocks.
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000b5"); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Observe = %v, want ErrLockTimeout", err)
	}
}

// A lock whose pid is dead and whose file is older than the break age is broken
// with a warning, so a crashed holder cannot wedge the machine.
func TestStaleLockIsBrokenWithAWarning(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) { o.LockTimeout = 50 * time.Millisecond })
	lockPath := f.store.Layout().LockFile(f.identity.Key)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	dead := lockInfo{PID: 0x7ffffffe, AcquiredAt: f.clock.Now()}
	payload, _ := json.Marshal(dead)
	if err := os.WriteFile(lockPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockBreakAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000b6"); err != nil {
		t.Fatalf("Observe over a stale lock: %v", err)
	}
	if !f.warnings.Contains("was held by dead pid") {
		t.Errorf("breaking a lock must warn; warnings = %v", f.warnings.All())
	}
	if _, exists := f.store.GroupRecordFor(f.identity, codexPlan); !exists {
		t.Fatal("the write must have landed after the stale lock was broken")
	}
}

// A lock held by a LIVE pid is never broken, however old it is.
func TestLiveLockIsNotBroken(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) { o.LockTimeout = 40 * time.Millisecond })
	lockPath := f.store.Layout().LockFile(f.identity.Key)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	child := startLiveChild(t)
	alive := lockInfo{PID: child.pid, PIDStartTime: child.startTime, AcquiredAt: f.clock.Now()}
	payload, _ := json.Marshal(alive)
	if err := os.WriteFile(lockPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockBreakAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000b7"); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Observe = %v, want ErrLockTimeout: a live holder must keep its lock", err)
	}
}

// The locking unit is one identity file: two providers never contend.
func TestTwoProvidersNeverContend(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(Options{Layout: LayoutAt(root), LockTimeout: 40 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	codex, err := IdentityFor(ProviderCodex, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	claude, err := IdentityFor(ProviderClaude, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if store.Layout().LockFile(codex.Key) == store.Layout().LockFile(claude.Key) {
		t.Fatal("two providers must not share a lock file")
	}

	// Hold the Codex lock and prove a Claude write still lands.
	codexLock := store.Layout().LockFile(codex.Key)
	if err := os.MkdirAll(filepath.Dir(codexLock), 0o700); err != nil {
		t.Fatal(err)
	}
	handle, err := os.OpenFile(codexLock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	defer func() { _ = os.Remove(codexLock) }()

	if err := store.ClearGroup(claude, "claude-plan", "test"); err != nil {
		t.Fatalf("a Claude write contended with a held Codex lock: %v", err)
	}
}

// Two repositories sharing one home serialise correctly: the same identity means
// the same lock.
func TestTwoRepositoriesSharingOneHomeSerialise(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	newStore := func() *Store {
		store, err := NewStore(Options{Layout: LayoutAt(root)})
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		return store
	}
	repoA, repoB := newStore(), newStore()
	identity, err := IdentityFor(ProviderCodex, home)
	if err != nil {
		t.Fatal(err)
	}
	if repoA.Layout().LockFile(identity.Key) != repoB.Layout().LockFile(identity.Key) {
		t.Fatal("one home shared by two repositories must serialise on one lock")
	}

	class := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: You've hit your usage limit.\n")})
	obs, _ := class.QuotaObservation(Evidence{RunID: "RUN-260802-0000b8"})
	if err := repoA.Observe(identity, codexPlan, nil, obs); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	// The suppression the first repository recorded is visible to the second.
	if _, exists := repoB.GroupRecordFor(identity, codexPlan); !exists {
		t.Fatal("machine-scoped state must be shared across repositories")
	}
}

// Concurrency at max_parallel scale: twenty concurrent writers, all serialised, no
// lost record and no corrupt file.
func TestConcurrentWritersAtMaxParallelScale(t *testing.T) {
	f := newStoreFixture(t, func(o *Options) { o.LockTimeout = 5 * time.Second })
	const parallel = 20
	groups := []string{codexPlan, "codex-spark", "claude-plan", "claude-usage-credits"}

	class := ClassifyCodexPrompt(CodexPromptResult{ExitCode: 1, Log: []byte("ERROR: You've hit your usage limit.\n")})
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range parallel {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			obs, _ := class.QuotaObservation(Evidence{RunID: runIDForIndex(index)})
			<-start
			if err := f.store.Observe(f.identity, groups[index%len(groups)], nil, obs); err != nil {
				t.Errorf("Observe(%d): %v", index, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	state := f.store.LoadIdentityState(f.identity)
	if len(state.Groups) != len(groups) {
		t.Fatalf("state holds %d groups, want %d: a concurrent write was lost", len(state.Groups), len(groups))
	}
	for _, group := range groups {
		record, ok := state.Groups[group]
		if !ok {
			t.Fatalf("group %s is missing", group)
		}
		if record.State != StateSuppressed {
			t.Errorf("group %s state = %q", group, record.State)
		}
		if record.ConsecutiveLimitObservations != parallel/len(groups) {
			t.Errorf("group %s observations = %d, want %d", group, record.ConsecutiveLimitObservations, parallel/len(groups))
		}
	}
	// The file is still valid JSON of the expected schema.
	data, err := os.ReadFile(f.store.Layout().StateFile(f.identity.Key))
	if err != nil {
		t.Fatal(err)
	}
	var file identityStateFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("the state file is not valid JSON after %d concurrent writers: %v", parallel, err)
	}
	if file.Version != SchemaVersion {
		t.Errorf("version = %d, want %d", file.Version, SchemaVersion)
	}
}

// A write failure loses the observation and is never fatal.
func TestWriteFailureIsWarnedNotFatal(t *testing.T) {
	f := newStoreFixture(t)
	// Make the state directory unwritable so the atomic write cannot land.
	if err := os.MkdirAll(f.store.Layout().Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if err := os.Chmod(f.store.Layout().Root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.store.Layout().Root, 0o700) })

	err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000b9")
	if err == nil {
		t.Fatal("a write failure must be reported to the caller so it can warn")
	}
	if !errors.Is(err, ErrStateWriteFailed) && !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want a fail-open sentinel", err)
	}
	if len(f.warnings.All()) == 0 {
		t.Error("a write failure must warn")
	}
}

// A claim that could not be persisted is refused: an unpersisted lease would gate
// nobody, so every concurrent spawn would probe. This is the one fail-open row
// that must refuse rather than admit.
func TestUnpersistableClaimIsRefused(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	f.clock.Advance(2 * time.Minute)

	// The lock is taken successfully and the write then fails, which is the shape a
	// full or read-only volume produces and which directory permissions cannot
	// reproduce (they break the lock first).
	f.store.writeState = func(string, any) error { return errors.New("disk full") }

	lease, granted := f.claim(t, codexPlan, "RUN-260802-0000ba")
	if granted || lease.Held() {
		t.Fatal("a claim whose lease could not be persisted must be refused")
	}
	if !f.warnings.Contains("was not persisted") {
		t.Errorf("warnings = %v", f.warnings.All())
	}

	// And nothing was recorded, so the next caller sees the group exactly as before.
	f.store.writeState = writeJSONAtomic
	if got := f.record(t, codexPlan).State; got == StateProbing {
		t.Fatalf("state = %q; a refused claim must leave no lease behind", got)
	}
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000bc"); !granted {
		t.Fatal("with the write path restored, exactly one caller must still be able to probe")
	}
}

// An available group is granted even when the state cannot be written: a write
// failure must never subtract a group that nothing suppressed.
func TestUnpersistableWriteStillAdmitsAnAvailableGroup(t *testing.T) {
	f := newStoreFixture(t)
	f.store.writeState = func(string, any) error { return errors.New("disk full") }
	if _, granted := f.claim(t, codexPlan, "RUN-260802-0000bd"); !granted {
		t.Fatal("limit state is an optimisation and must never be able to prevent a spawn")
	}
}

// GC failure is a warning, not a spawn blocker.
func TestGarbageCollectionFailureIsNotFatal(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	// A file that looks like an identity state but is not parseable is skipped
	// rather than fatal.
	junk := filepath.Join(f.store.Layout().Root, "deadbeefdeadbeef.state.json")
	if err := os.WriteFile(junk, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities: %v", err)
	}
	if err := f.observeQuota(t, codexPlan, nil, "RUN-260802-0000bb"); err != nil {
		t.Fatalf("Observe after a GC hiccup: %v", err)
	}
	// A missing root is not an error either.
	empty, err := NewStore(Options{Layout: LayoutAt(filepath.Join(t.TempDir(), "never-created"))})
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.CollectIdentities(); err != nil {
		t.Fatalf("CollectIdentities on a machine with no state: %v", err)
	}
}

func TestStateFileHasNoSecretsAndReadableShape(t *testing.T) {
	f := newStoreFixture(t)
	f.suppress(t, codexPlan)
	data, err := os.ReadFile(f.store.Layout().StateFile(f.identity.Key))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version": 3`, `"state": "suppressed"`, `"backoff_step"`, `"next_probe_at"`, `"probe_lease": null`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("state file does not carry %s:\n%s", want, data)
		}
	}
}
