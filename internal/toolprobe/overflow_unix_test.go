//go:build unix

package toolprobe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This selected helper deliberately leaves the probe group. It makes bounded
// teardown fail observably, without a sleep or reliance on fork scheduling.
func TestProbeEscapedPipeHolder(t *testing.T) {
	dir := os.Getenv("TOOLPROBE_ESCAPED_HOLDER_DIR")
	if dir == "" {
		return
	}
	if err := syscall.Setpgid(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.WriteString(strings.Repeat("x", maxVersionBytes+1)); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
}

// An overflow always terminates the probe group, even when the leader's
// exit and both pipe completions win the select race against the cap
// signal. The fixture's descendant holds neither output pipe — its stdio
// is redirected and its only wait is a private FIFO — so without the
// overflow kill it survives while the probe reports output-limited with
// teardown confirmed. Death is observed through the FIFO's EOF, and the
// fixture uses open rendezvous only; no sleep establishes state.
//
// One pass wins the select race only sometimes, so the pass repeats:
// without the kill a pass fails with probability about 1/4, and 40
// passes miss it with probability (3/4)^40, under 0.01%.
func TestProbeOverflowKillsGroupMemberHoldingNoPipe(t *testing.T) {
	for i := 0; i < 40; i++ {
		probeOverflowKillsDetachedMember(t)
	}
}

func probeOverflowKillsDetachedMember(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	death := filepath.Join(dir, "death")
	ready := filepath.Join(dir, "ready")
	pidFile := filepath.Join(dir, "pid")
	for _, fifo := range []string{death, ready} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("creating the overflow fixture FIFO: %v", err)
		}
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	stub := writeProbeStub(t,
		"(exec >/dev/null 2>&1; exec 9<> "+quote(death)+"; printf . > "+quote(ready)+"; IFS= read -r overflow_hold <&9) &\n"+
			"echo $! > "+quote(pidFile)+"\n"+
			"IFS= read -r overflow_ready < "+quote(ready)+"\n"+
			fmt.Sprintf("printf '%%%ds' ''\n", maxVersionBytes+1)+
			"exit 0\n")
	t.Cleanup(func() {
		// Reap a survivor only: after a proved death its pid is long
		// gone, and signalling it could reach an unrelated reuse.
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			return
		}
		if err := syscall.Kill(pid, 0); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	_, err := VersionOutput(context.Background(), stub, []string{})
	attempt := requireAttempt(t, err, agentic.ProbeStageVersion, false, true)
	if attempt.TeardownIncomplete {
		t.Fatal("overflow with drained pipes reported teardown-incomplete")
	}
	// The descendant held the death FIFO open; its EOF proves the group
	// kill reached a member that held neither probe pipe. The open never
	// blocks (O_NONBLOCK) and the read waits without polling; the timer
	// is a failure watchdog, never a state delay.
	r, err := os.OpenFile(death, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("opening the death FIFO: %v", err)
	}
	defer r.Close()
	if err := syscall.SetNonblock(int(r.Fd()), false); err != nil {
		t.Fatalf("blocking on the death FIFO: %v", err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 1))
		read <- err
	}()
	watchdog := time.NewTimer(30 * time.Second)
	defer watchdog.Stop()
	select {
	case err := <-read:
		if err == nil {
			t.Fatal("the death FIFO carried data no fixture writes")
		}
	case <-watchdog.C:
		t.Fatal("a group member holding neither pipe survived the overflow kill")
	}
}

func TestProbeOverflowRemainsLimitedWithUnconfirmedTeardown(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "hold"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(filepath.Join(dir, "pid"))
		if err != nil {
			t.Errorf("escaped holder never became ready: %v", err)
			return
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil || pid <= 0 {
			t.Errorf("invalid escaped holder pid: %q", data)
			return
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	})
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stub := writeProbeStub(t, "'"+strings.ReplaceAll(self, "'", "'\"'\"'")+"' -test.run='^TestProbeEscapedPipeHolder$' -test.timeout=1m &\nwait\n")
	_, err = VersionOutput(context.Background(), stub, []string{"TOOLPROBE_ESCAPED_HOLDER_DIR=" + dir})
	attempt := requireAttempt(t, err, agentic.ProbeStageVersion, false, true)
	if !attempt.TeardownIncomplete {
		t.Fatal("escaped pipe holder was reported as confirmed teardown")
	}
}
