package toolprobe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The release-triple grammar, pinned both ways: exact triples admit,
// and anything else — suffixes, missing parts, non-digits, surrounding
// text — refuses.
func TestIsReleaseTripleAdmitsExactTriplesOnly(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"2.1.261", "0.153.2", "0.84.2", "10.20.30", "01.02.03"} {
		if !IsReleaseTriple(s) {
			t.Errorf("IsReleaseTriple(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", " ", "2.1", "2.1.261.4", "2.1.x", "v2.1.261", "2.1.261-beta", "2. 1.261", "2..261", "codex-cli 0.153.4", "2.1.274 (Claude Code)"} {
		if IsReleaseTriple(s) {
			t.Errorf("IsReleaseTriple(%q) = true, want false", s)
		}
	}
}

func writeStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing the stub %s: %v", name, err)
	}
	return path
}

// The probe runs `<binary> --version` and returns stdout. The stub logs
// its argv, so the capture proves the probe's argv is exactly that —
// never a flag that could start a session.
func TestVersionOutputRunsVersionAndReturnsStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	capture := filepath.Join(dir, "argv.log")
	bin := writeStub(t, dir, "tool", "#!/bin/sh\necho \"$@\" > "+capture+"\necho 1.2.3\n")
	got, err := VersionOutput(context.Background(), bin, []string{"PATH=" + dir})
	if err != nil {
		t.Fatalf("VersionOutput: %v", err)
	}
	if string(got) != "1.2.3\n" {
		t.Errorf("stdout = %q, want %q", got, "1.2.3\n")
	}
	argv, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading the argv capture: %v", err)
	}
	if strings.TrimSpace(string(argv)) != "--version" {
		t.Errorf("the probe ran with argv %q, want exactly --version", strings.TrimSpace(string(argv)))
	}
}

// Stderr is discarded: a diagnostic there must not become part of the
// parsed answer.
func TestVersionOutputDiscardsStderr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := writeStub(t, dir, "tool", "#!/bin/sh\necho noise >&2\necho 1.2.3\n")
	got, err := VersionOutput(context.Background(), bin, []string{"PATH=" + dir})
	if err != nil {
		t.Fatalf("VersionOutput: %v", err)
	}
	if string(got) != "1.2.3\n" {
		t.Errorf("stdout = %q, want %q (stderr discarded)", got, "1.2.3\n")
	}
}

// A nil env runs the child with an EMPTY environment, never the
// process's own: the stub reports whether it inherited anything.
func TestVersionOutputNeverInheritsTheProcessEnvironment(t *testing.T) {
	// Sequential on purpose: t.Setenv refuses t.Parallel, and the point is
	// the process environment, which parallel tests share.
	dir := t.TempDir()
	bin := writeStub(t, dir, "tool", "#!/bin/sh\nif [ -z \"$TOOLPROBE_SENTINEL_XYZ\" ]; then echo clean; else echo dirty; fi\n")
	t.Setenv("TOOLPROBE_SENTINEL_XYZ", "1")
	got, err := VersionOutput(context.Background(), bin, nil)
	if err != nil {
		t.Fatalf("VersionOutput: %v", err)
	}
	if strings.TrimSpace(string(got)) != "clean" {
		t.Errorf("the child saw the process environment: %q", got)
	}
}

func TestVersionOutputRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := VersionOutput(context.Background(), "", []string{"PATH=" + dir}); err == nil {
		t.Error("an empty binary was admitted")
	}
	if _, err := VersionOutput(context.Background(), filepath.Join(dir, "missing"), []string{"PATH=" + dir}); err == nil {
		t.Error("a missing binary was admitted")
	}
	t.Run("a non-zero exit", func(t *testing.T) {
		t.Parallel()
		bin := writeStub(t, dir, "failing", "#!/bin/sh\necho 1.2.3\nexit 3\n")
		if _, err := VersionOutput(context.Background(), bin, []string{"PATH=" + dir}); err == nil {
			t.Error("a non-zero exit was admitted with its stdout")
		}
	})
	t.Run("an answer past the byte cap", func(t *testing.T) {
		t.Parallel()
		bin := writeStub(t, dir, "chatty", "#!/bin/sh\nhead -c 100000 /dev/zero | tr '\\0' '7'\n")
		if _, err := VersionOutput(context.Background(), bin, []string{"PATH=" + dir}); err == nil {
			t.Error("a 100KB answer was admitted")
		}
	})
	t.Run("a fired context", func(t *testing.T) {
		t.Parallel()
		bin := writeStub(t, dir, "sleeper", "#!/bin/sh\nsleep 30\n")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		if _, err := VersionOutput(ctx, bin, []string{"PATH=" + dir}); err == nil {
			t.Error("a hung subprocess was admitted")
		}
		if elapsed := time.Since(start); elapsed > 20*time.Second {
			t.Errorf("the probe waited %s for a hung subprocess; the caller ctx must bound it", elapsed)
		}
	})
}
