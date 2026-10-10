//go:build unix

package toolprobe

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

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
