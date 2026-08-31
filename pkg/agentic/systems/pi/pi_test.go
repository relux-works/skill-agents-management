package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestArgvBuildsExactProcessAContract(t *testing.T) {
	system := New(&fakeStatusReader{})
	t.Run("no profile is refused", func(t *testing.T) {
		if _, err := system.Argv(agentic.LaunchRequest{Prompt: []byte("turn")}, agentic.LaunchModeExec); !errors.Is(err, ErrProfileMissing) {
			t.Fatalf("Argv with no profile = %v, want ErrProfileMissing", err)
		}
	})
	t.Run("exec bytes", func(t *testing.T) {
		argv, err := system.Argv(agentic.LaunchRequest{Profile: "local-qwen", Prompt: []byte("-inspect @repo")}, agentic.LaunchModeExec)
		if err != nil {
			t.Fatalf("Argv(exec): %v", err)
		}
		want := []string{"pi", "spawn", "--profile", "local-qwen", "--prompt", "-inspect @repo", "--deadline", "30m", "--result-schema", "1"}
		if !reflect.DeepEqual(argv, want) {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	})
	t.Run("dry run uses placeholder without reading file", func(t *testing.T) {
		argv, err := system.Argv(agentic.LaunchRequest{Profile: "local-qwen", PromptPath: filepath.Join(t.TempDir(), "missing")}, agentic.LaunchModeDryRun)
		if err != nil {
			t.Fatalf("Argv(dry-run): %v", err)
		}
		want := []string{"pi", "spawn", "--profile", "local-qwen", "--prompt", "<prompt>", "--deadline", "30m", "--result-schema", "1"}
		if !reflect.DeepEqual(argv, want) {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	})
	t.Run("profile bytes are not normalized", func(t *testing.T) {
		argv, err := system.Argv(agentic.LaunchRequest{Profile: " Exact-Profile ", Prompt: []byte("turn")}, agentic.LaunchModeExec)
		if err != nil || argv[3] != " Exact-Profile " {
			t.Fatalf("argv=%v err=%v, want byte-exact profile", argv, err)
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

func TestPromptPathPrecedesPromptAndInvalidPromptsRefuse(t *testing.T) {
	system := New(&fakeStatusReader{})
	path := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(path, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	argv, err := system.Argv(agentic.LaunchRequest{Profile: "p", PromptPath: path, Prompt: []byte("fallback")}, agentic.LaunchModeExec)
	if err != nil || argv[5] != "from file" {
		t.Fatalf("PromptPath precedence argv=%v err=%v", argv, err)
	}
	for name, req := range map[string]agentic.LaunchRequest{
		"missing":      {Profile: "p"},
		"unreadable":   {Profile: "p", PromptPath: filepath.Join(t.TempDir(), "missing")},
		"invalid utf8": {Profile: "p", Prompt: []byte{0xff}},
		"nul":          {Profile: "p", Prompt: []byte("a\x00b")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := system.Argv(req, agentic.LaunchModeExec); !errors.Is(err, ErrTurnPromptInvalid) {
				t.Fatalf("Argv = %v, want ErrTurnPromptInvalid", err)
			}
		})
	}
}

func TestStdinIsAlwaysDetachedEOF(t *testing.T) {
	system := New(&fakeStatusReader{})

	payload, err := system.Stdin(agentic.LaunchRequest{Prompt: []byte("do the thing")})
	if err != nil || !reflect.DeepEqual(payload, agentic.StdinPayload{}) {
		t.Fatalf("stdin payload=%+v err=%v, want detached EOF", payload, err)
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
