package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The version grammar, pinned against the real capture first: `claude
// --version` prints `<triple> (Claude Code)` on stdout, exit 0
// (installed 2.1.274). Helper-direct and disclosed as a bound —
// parseToolRelease is the probe's pure half; the rows below drive the
// production prober through agentic.ProbeToolRelease against fake
// binaries, and never execute a real provider.
func TestParseToolReleaseReadsTheFirstField(t *testing.T) {
	t.Parallel()
	for out, want := range map[string]string{
		"2.1.274 (Claude Code)\n": "2.1.274",
		"2.1.261 (Claude Code)\n": "2.1.261",
		"2.1.261\n":               "2.1.261",
		"  2.1.261 (x)\nsecond\n": "2.1.261",
	} {
		if got, err := parseToolRelease([]byte(out)); err != nil || got != want {
			t.Errorf("parseToolRelease(%q) = (%q, %v), want (%q, nil)", out, got, err, want)
		}
	}
	for _, out := range []string{"", "\n", "claude\n", "(Claude Code) 2.1.274\n", "v2.1.261 (x)\n", "2.1 (x)\n"} {
		if got, err := parseToolRelease([]byte(out)); err == nil {
			t.Errorf("parseToolRelease(%q) = %q, want an error", out, got)
		}
	}
}

func writeScriptStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing the stub %s: %v", name, err)
	}
	return path
}

func probeEnv(binDir string) []string {
	return []string{"PATH=" + binDir}
}

// The probe resolves the binary through the launch PATH, runs it with
// exactly `--version`, and returns the parsed release.
func TestProbeToolReleaseEstablishesTheRelease(t *testing.T) {
	t.Parallel()
	binDir := tempSlot(t)
	capture := filepath.Join(binDir, "argv.log")
	writeScriptStub(t, binDir, executableName, "#!/bin/sh\necho \"$@\" > "+capture+"\necho '2.1.261 (Claude Code)'\n")

	release, err := agentic.ProbeToolRelease(context.Background(), New(), probeEnv(binDir))
	if err != nil {
		t.Fatalf("ProbeToolRelease: %v", err)
	}
	if release != "2.1.261" {
		t.Errorf("release = %q, want %q", release, "2.1.261")
	}
	argv, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading the argv capture: %v", err)
	}
	if strings.TrimSpace(string(argv)) != "--version" {
		t.Errorf("the probe ran with argv %q, want exactly --version", strings.TrimSpace(string(argv)))
	}
}

// Every detection failure is the undetected sentinel: an unresolvable
// binary, a missing PATH, a non-zero exit, an unparsable answer. The
// caller answers it with ToolRelease "" — yolo fails closed, native
// forwards — never by synthesizing a release.
func TestProbeToolReleaseFailsClosed(t *testing.T) {
	t.Parallel()
	t.Run("an unresolvable binary", func(t *testing.T) {
		t.Parallel()
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), probeEnv(tempSlot(t))); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
	t.Run("no PATH at all", func(t *testing.T) {
		t.Parallel()
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), []string{"HOME=/home/agent"}); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
	t.Run("a non-zero exit", func(t *testing.T) {
		t.Parallel()
		binDir := tempSlot(t)
		writeScriptStub(t, binDir, executableName, "#!/bin/sh\necho '2.1.261 (Claude Code)'\nexit 3\n")
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), probeEnv(binDir)); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
	t.Run("an unparsable answer", func(t *testing.T) {
		t.Parallel()
		binDir := tempSlot(t)
		writeScriptStub(t, binDir, executableName, "#!/bin/sh\necho 'something else entirely'\n")
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), probeEnv(binDir)); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
}
