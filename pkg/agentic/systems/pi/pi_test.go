package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
)

// fakeStatusReader is the scriptable StatusReader §1 of the adversarial plan
// describes for this package's own tests: it records the exact StatusQuery
// of every call, and can be configured to return a fixed Status, an error,
// or to hang until ctx is done.
type fakeStatusReader struct {
	status localruntime.Status
	err    error
	hang   bool

	calls []localruntime.StatusQuery
}

func (f *fakeStatusReader) Status(ctx context.Context, query localruntime.StatusQuery) (localruntime.Status, error) {
	f.calls = append(f.calls, query)
	if f.hang {
		<-ctx.Done()
		return localruntime.Status{}, ctx.Err()
	}
	if f.err != nil {
		return localruntime.Status{}, f.err
	}
	return f.status, nil
}

func TestResolveBinaryIsAgentsInfraNotRawPi(t *testing.T) {
	system := New(&fakeStatusReader{})

	t.Run("empty PATH is refused", func(t *testing.T) {
		if binary, err := system.ResolveBinary(agentic.LaunchRequest{Env: []string{"PATH=" + t.TempDir()}}); err == nil {
			t.Fatalf("resolved %q against a PATH with nothing on it", binary)
		}
	})

	t.Run("resolves agents-infra, never raw pi", func(t *testing.T) {
		dir := t.TempDir()
		writeExecutable(t, filepath.Join(dir, "pi"))
		writeExecutable(t, filepath.Join(dir, "agents-infra"))
		binary, err := system.ResolveBinary(agentic.LaunchRequest{Env: []string{"PATH=" + dir}})
		if err != nil {
			t.Fatalf("ResolveBinary: %v", err)
		}
		if filepath.Base(binary) != "agents-infra" {
			t.Fatalf("resolved %q, want the agents-infra wrapper, not raw pi", binary)
		}
	})
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing fake executable %s: %v", path, err)
	}
}

func TestArgvBuildsThePinnedPrefixAndRequiresAProfile(t *testing.T) {
	system := New(&fakeStatusReader{})
	t.Run("no profile is refused", func(t *testing.T) {
		if _, err := system.Argv(agentic.LaunchRequest{}, agentic.LaunchModeExec); err == nil {
			t.Fatal("Argv with no profile returned no error")
		}
	})
	t.Run("pinned prefix", func(t *testing.T) {
		for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
			argv, err := system.Argv(agentic.LaunchRequest{Profile: "local-qwen"}, mode)
			if err != nil {
				t.Fatalf("Argv(%s): %v", mode, err)
			}
			want := []string{"pi", "--profile", "local-qwen", "--"}
			if len(argv) != len(want) {
				t.Fatalf("argv = %v, want %v", argv, want)
			}
			for i := range want {
				if argv[i] != want[i] {
					t.Fatalf("argv = %v, want %v", argv, want)
				}
			}
		}
	})
	t.Run("unsupported mode is refused", func(t *testing.T) {
		if _, err := system.Argv(agentic.LaunchRequest{Profile: "local-qwen"}, agentic.LaunchModeManagedSession); err == nil {
			t.Fatal("Argv(managed-session), an undeclared mode, returned no error")
		}
	})
}

func TestChildEnvPassesAgentsInfraCallerCWDThroughUnfiltered(t *testing.T) {
	system := New(&fakeStatusReader{})
	parent := []string{"PATH=/usr/bin", "AGENTS_INFRA_CALLER_CWD=/Users/op/project"}
	env, err := system.ChildEnv(parent, agentic.LaunchRequest{})
	if err != nil {
		t.Fatalf("ChildEnv: %v", err)
	}
	found := false
	for _, entry := range env {
		if entry == "AGENTS_INFRA_CALLER_CWD=/Users/op/project" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env = %v; AGENTS_INFRA_CALLER_CWD was stripped or rewritten", env)
	}
}

func TestStdinAttachesPromptPathThenPromptThenNothing(t *testing.T) {
	system := New(&fakeStatusReader{})

	payload, err := system.Stdin(agentic.LaunchRequest{})
	if err != nil || payload.Attached {
		t.Fatalf("no prompt at all: payload=%+v err=%v, want nothing attached", payload, err)
	}

	payload, err = system.Stdin(agentic.LaunchRequest{Prompt: []byte("do the thing")})
	if err != nil || !payload.Attached || string(payload.Bytes) != "do the thing" {
		t.Fatalf("Prompt bytes: payload=%+v err=%v", payload, err)
	}
}

func TestValidateCompositionRefusesEveryComposition(t *testing.T) {
	system := New(&fakeStatusReader{})
	if err := system.ValidateComposition(agentic.Composition{}); err != nil {
		t.Fatalf("zero composition refused: %v", err)
	}
	if err := system.ValidateComposition(agentic.Composition{Prefix: []string{"--x"}}); err == nil {
		t.Fatal("a non-zero composition was accepted by a system declaring no grammar")
	}
}
