package toolprobe

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func writeProbeStub(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe")
	script := "#!/bin/sh\n" + body
	if err := execfixture.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the probe stub: %v", err)
	}
	return path
}

func requireAttempt(t *testing.T, err error, stage string, timeout, limited bool) *agentic.ProbeExecutionError {
	t.Helper()
	var attempt *agentic.ProbeExecutionError
	if !errors.As(err, &attempt) {
		t.Fatalf("probe err = %v, want a *ProbeExecutionError", err)
	}
	if attempt.Stage != stage || attempt.Timeout != timeout || attempt.OutputLimited != limited {
		t.Fatalf("attempt = %+v, want stage %q timeout %v limited %v", attempt, stage, timeout, limited)
	}
	if len(attempt.Stdout) > agentic.ProbeEvidenceSampleBytes || len(attempt.Stderr) > agentic.ProbeEvidenceSampleBytes {
		t.Fatalf("attempt samples = %d/%d bytes, want at most %d each",
			len(attempt.Stdout), len(attempt.Stderr), agentic.ProbeEvidenceSampleBytes)
	}
	return attempt
}

func TestVersionOutputCapsDuringRead(t *testing.T) {
	t.Parallel()
	t.Run("exactly-at-cap", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "printf '%65536s' ''\n")
		out, err := VersionOutput(context.Background(), stub, []string{})
		if err != nil || len(out) != 65536 {
			t.Fatalf("VersionOutput at cap = (%d bytes, %v), want 65536 bytes", len(out), err)
		}
	})
	t.Run("one-past-cap", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "printf '%65537s' ''\n")
		_, err := VersionOutput(context.Background(), stub, []string{})
		requireAttempt(t, err, agentic.ProbeStageVersion, false, true)
	})
	t.Run("over-cap-then-holds", func(t *testing.T) {
		t.Parallel()
		gate := execfixture.NewGate(t)
		stub := writeProbeStub(t, "printf '%70000s' ''\n"+gate.Command()+"\n")
		_, err := VersionOutput(context.Background(), stub, []string{"PATH=/bin:/usr/bin"})
		attempt := requireAttempt(t, err, agentic.ProbeStageVersion, false, true)
		if attempt.TeardownIncomplete {
			t.Fatal("output cap left a running child or held pipe")
		}
	})
}

func TestProbeStdoutDrainsPastChildExit(t *testing.T) {
	t.Parallel()
	gate := execfixture.NewGate(t)
	stub := writeProbeStub(t, "("+gate.Command()+") &\nprintf '%s\\n' 'answer'\n")
	out, err := VersionOutput(context.Background(), stub, []string{"PATH=/bin:/usr/bin"})
	if err != nil || strings.TrimSpace(string(out)) != "answer" {
		t.Fatalf("VersionOutput with held pipe = (%q, %v), want the complete answer", out, err)
	}
}

func TestProbeStderrOverflowDiscardsWithoutFailing(t *testing.T) {
	t.Parallel()
	stub := writeProbeStub(t, "printf '%70000s' '' >&2\nprintf '%s\\n' 'answer'\n")
	out, err := VersionOutput(context.Background(), stub, []string{})
	if err != nil || strings.TrimSpace(string(out)) != "answer" {
		t.Fatalf("VersionOutput with stderr overflow = (%q, %v), want the answer", out, err)
	}
}

func TestProbeTimeoutAndAttemptSurface(t *testing.T) {
	t.Parallel()
	t.Run("hanging", func(t *testing.T) {
		t.Parallel()
		gate := execfixture.NewGate(t)
		stub := writeProbeStub(t, gate.Command()+"\n")
		_, err := VersionOutput(context.Background(), stub, []string{"PATH=/bin:/usr/bin"})
		attempt := requireAttempt(t, err, agentic.ProbeStageVersion, true, false)
		if !attempt.ExecAttempted || !attempt.ChildStarted {
			t.Fatalf("hanging attempt = %+v, want exec attempted and child started", attempt)
		}
	})
	t.Run("missing-binary", func(t *testing.T) {
		t.Parallel()
		_, err := VersionOutput(context.Background(), filepath.Join(t.TempDir(), "absent"), []string{})
		attempt := requireAttempt(t, err, agentic.ProbeStageVersion, false, false)
		if !attempt.ExecAttempted || attempt.ChildStarted {
			t.Fatalf("missing-binary attempt = %+v, want exec attempted without a child", attempt)
		}
	})
	t.Run("pre-cancelled", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "exit 0\n")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := VersionOutput(ctx, stub, []string{})
		attempt := requireAttempt(t, err, agentic.ProbeStageVersion, false, false)
		if attempt.ExecAttempted || attempt.ChildStarted {
			t.Fatalf("pre-cancelled attempt = %+v, want nothing attempted", attempt)
		}
	})
	t.Run("nonzero", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "printf '%s\\n' 'partial'\nexit 3\n")
		_, err := VersionOutput(context.Background(), stub, []string{})
		attempt := requireAttempt(t, err, agentic.ProbeStageVersion, false, false)
		if !attempt.ExecAttempted || !attempt.ChildStarted {
			t.Fatalf("nonzero attempt = %+v, want exec attempted and child started", attempt)
		}
		if !strings.Contains(attempt.Detail, "exit status 3") {
			t.Fatalf("nonzero detail = %q, want the exit status", attempt.Detail)
		}
		if strings.TrimSpace(string(attempt.Stdout)) != "partial" {
			t.Fatalf("nonzero stdout sample = %q, want the partial answer", attempt.Stdout)
		}
	})
}

func TestHelpOutputBounds(t *testing.T) {
	t.Parallel()
	t.Run("exactly-at-cap", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "printf '%1048576s' ''\n")
		out, err := HelpOutput(context.Background(), stub, []string{})
		if err != nil || len(out) != 1048576 {
			t.Fatalf("HelpOutput at cap = (%d bytes, %v), want 1048576 bytes", len(out), err)
		}
	})
	t.Run("small", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "printf '%s\\n' 'help'\n")
		out, err := HelpOutput(context.Background(), stub, []string{})
		if err != nil || strings.TrimSpace(string(out)) != "help" {
			t.Fatalf("HelpOutput = (%q, %v), want the help text", out, err)
		}
	})
	t.Run("one-past-cap", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "printf '%1048577s' ''\n")
		_, err := HelpOutput(context.Background(), stub, []string{})
		requireAttempt(t, err, agentic.ProbeStageHelp, false, true)
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		stub := writeProbeStub(t, "exit 0\n")
		_, err := HelpOutput(context.Background(), stub, []string{})
		attempt := requireAttempt(t, err, agentic.ProbeStageHelp, false, false)
		if !attempt.ExecAttempted || !attempt.ChildStarted {
			t.Fatalf("empty attempt = %+v, want exec attempted and child started", attempt)
		}
		if !strings.Contains(attempt.Detail, "printed no output") {
			t.Fatalf("empty detail = %q, want the no-output fact", attempt.Detail)
		}
	})
	t.Run("hanging", func(t *testing.T) {
		t.Parallel()
		gate := execfixture.NewGate(t)
		stub := writeProbeStub(t, gate.Command()+"\n")
		start := time.Now()
		_, err := HelpOutput(context.Background(), stub, []string{"PATH=/bin:/usr/bin"})
		requireAttempt(t, err, agentic.ProbeStageHelp, true, false)
		if elapsed := time.Since(start); elapsed > 60*time.Second {
			t.Fatalf("hanging help took %v, want the execution bound", elapsed)
		}
	})
}
