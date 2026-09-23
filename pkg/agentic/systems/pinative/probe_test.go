package pinative

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The version grammar, pinned against the real capture first: `pi
// --version` prints the bare triple on stdout, exit 0 (`0.84.2` at
// installed 0.84.2). Helper-direct and disclosed as a bound —
// parseToolRelease is the probe's pure half; the rows below drive the
// production prober through agentic.ProbeToolRelease against fake
// binaries, and never execute a real provider.
func TestParseToolReleaseReadsTheBareTriple(t *testing.T) {
	for out, want := range map[string]string{
		"0.84.2\n":  "0.84.2",
		"  0.84.2 ": "0.84.2",
	} {
		if got, err := parseToolRelease([]byte(out)); err != nil || got != want {
			t.Errorf("parseToolRelease(%q) = (%q, %v), want (%q, nil)", out, got, err, want)
		}
	}
	for _, out := range []string{"", "\n", "pi 0.84.2\n", "v0.84.2\n", "0.84\n"} {
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

// The probe resolves the raw `pi` binary through the launch PATH, runs
// it with exactly `--version`, and returns the parsed release.
func TestProbeToolReleaseEstablishesTheRelease(t *testing.T) {
	binDir := t.TempDir()
	capture := filepath.Join(binDir, "argv.log")
	writeScriptStub(t, binDir, "pi", "#!/bin/sh\necho \"$@\" > "+capture+"\necho '0.84.2'\n")

	release, err := agentic.ProbeToolRelease(context.Background(), New(), []string{"PATH=" + binDir})
	if err != nil {
		t.Fatalf("ProbeToolRelease: %v", err)
	}
	if release != "0.84.2" {
		t.Errorf("release = %q, want %q", release, "0.84.2")
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
// binary, a non-zero exit, an unparsable answer. The caller answers it
// with ToolRelease "" — yolo fails closed, native forwards — never by
// synthesizing a release.
func TestProbeToolReleaseFailsClosed(t *testing.T) {
	t.Run("an unresolvable binary", func(t *testing.T) {
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), []string{"PATH=" + t.TempDir()}); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
	t.Run("a non-zero exit", func(t *testing.T) {
		binDir := t.TempDir()
		writeScriptStub(t, binDir, "pi", "#!/bin/sh\necho '0.84.2'\nexit 3\n")
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), []string{"PATH=" + binDir}); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
	t.Run("an unparsable answer", func(t *testing.T) {
		binDir := t.TempDir()
		writeScriptStub(t, binDir, "pi", "#!/bin/sh\necho 'something else entirely'\n")
		if _, err := agentic.ProbeToolRelease(context.Background(), New(), []string{"PATH=" + binDir}); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
}
