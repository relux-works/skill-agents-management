// Package paritycase is the machine state a launch-surface golden was captured
// under, reproduced on the machine running the test.
//
// # What it is and what it is not
//
// It lays down directories and stub executables, drives a plan through the REAL
// entry point (a real agentic.Registry plus agentic.BuildPlan), and reports
// environment differences. It builds no argv, resolves no binary and filters no
// environment: everything a port has to get right stays in the plugin, and
// nothing here may start to do any of it. That is the same boundary
// pkg/agentic/parity states for itself, one level down.
//
// # Why it is a package rather than test helpers
//
// Go has no way to share test-only code across packages, and these helpers live
// in one test binary per plugin — the same reason internal/argvguard is an
// ordinary package. Nothing in the shipped command tree imports this, so it is
// compiled and never linked into the binary.
//
// # Why it exists at all, and what is deliberately left alone
//
// The codex and claude plugins each carry their own copy of these helpers,
// written before there was a second plugin to share with. Four more copies
// would be six, and six copies of "how a golden's machine state is
// reproduced" is six places a subtle difference — a temp directory that is not
// canonicalized, a stub written without the execute bit — can hide while every
// suite stays green.
//
// The two accepted plugins are deliberately NOT rewritten onto this package.
// Their goldens pass against the helpers they were reviewed with, and
// rewriting the harness under an accepted parity proof would put the proof and
// the change in the same commit. The residual is stated rather than hidden:
// this module has two implementations of this scaffolding, and the next plugin
// to touch codex or claude for its own reasons is the one that should collapse
// them.
package paritycase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/parity"
)

// PromptFileName is the name the source's capture harness writes its assignment
// prompt under (writePromptFile, parity_capture_test.go). It is contract rather
// than convenience: muse's argv carries the prompt file PATH, so the name
// reaches the golden's surface.
const PromptFileName = "prompt.md"

// Dirs are this process's stand-ins for the machine-local directories a capture
// allocated, in the SAME order the capture allocated them.
//
// The source's captureBuildCommandSurface takes a bin directory and then a work
// directory; captureBuildArgsSurface takes only a work directory and resolves
// its binary through the PATH-seeded stub directory instead. A golden's
// capture.placeholders block is where the count and the kinds are read off
// rather than guessed.
type Dirs struct {
	Temp []string
	Stub string
}

// Substitutions renders the directories as the mask inputs parity.ComparePlan
// takes.
func (d Dirs) Substitutions() parity.Substitutions {
	return parity.Substitutions{TempSlots: d.Temp, StubBinDir: d.Stub}
}

// Make allocates a case's directories: tempSlots temp directories in order,
// plus the PATH-seeded stub directory when the case resolves through PATH.
func Make(t testing.TB, tempSlots int, usesStub bool) Dirs {
	t.Helper()
	dirs := Dirs{}
	for i := 0; i < tempSlots; i++ {
		dirs.Temp = append(dirs.Temp, TempSlot(t))
	}
	if usesStub {
		dirs.Stub = TempSlot(t)
	}
	return dirs
}

// TempSlot allocates one machine-local directory, canonicalized.
//
// The canonicalization is not cosmetic on macOS: t.TempDir hands back a path
// under /var, which is a symlink to /private/var. The substitution literal and
// the path under test have to be the same string for the mask to rewrite
// anything, and a plugin that canonicalizes — or a future check that starts to
// — would otherwise fail as a port bug rather than as a test measuring its own
// temp directory wrong.
func TempSlot(t testing.TB) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalizing a temp slot: %v", err)
	}
	return resolved
}

// WriteStubExecutable is the source's writeFakeExecutable: a runnable file with
// the right name, so binary resolution lands on a layout this test built rather
// than on whatever the developer's machine has installed.
func WriteStubExecutable(t testing.TB, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stub %s: %v", name, err)
	}
	return path
}

// WritePromptFile writes the assignment prompt where the capture wrote it.
func WritePromptFile(t testing.TB, workDir, body string) string {
	t.Helper()
	path := filepath.Join(workDir, PromptFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the prompt file: %v", err)
	}
	return path
}

// WithPathEntry returns a recorded parent environment with a PATH pointing at
// dirs.
//
// A golden records no PATH at all — the capture omitted it deliberately, and
// the harness excludes it from the environment diff on both sides — so adding
// one here changes no measurement. It is what makes binary resolution hermetic:
// a ported plugin resolves from the environment it is handed, so the launch
// environment IS the PATH under test.
func WithPathEntry(parent []string, dirs ...string) []string {
	return append(append([]string(nil), parent...), "PATH="+strings.Join(dirs, string(os.PathListSeparator)))
}

// BuildPlan drives production: register the real plugin into a real registry
// and call agentic.BuildPlan.
//
// Nothing here reaches into a plugin's methods directly — a helper that is
// unit-tested but called from nowhere promises nothing about the launch a
// caller would actually get.
func BuildPlan(t testing.TB, sys agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode) agentic.Plan {
	t.Helper()
	plan, err := TryBuildPlan(sys, req, mode)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// TryBuildPlan is BuildPlan without the fatal, for the tests whose subject IS
// the refusal. It registers through the public API exactly as BuildPlan does,
// so a refusal observed here is a refusal a caller would get.
func TryBuildPlan(sys agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	registry := agentic.NewRegistry()
	if err := registry.Register(sys); err != nil {
		return agentic.Plan{}, err
	}
	return agentic.BuildPlan(registry, req, mode)
}

// NamesField reports whether any difference was reported in field, so a mutant
// test can insist a planted defect surfaced where it was planted. A failure
// naming the wrong surface sends the next reader to the wrong file.
func NamesField(diffs []parity.Difference, field string) bool {
	for _, d := range diffs {
		if d.Field == field {
			return true
		}
	}
	return false
}

// WithoutKeys drops the named entries from a recorded parent environment,
// reproducing the fixture shape that existed before they were seeded.
//
// It takes the keys explicitly rather than defaulting to any list: every
// narrowing removes a DIFFERENT subset, and which subset is the whole claim
// each one makes.
func WithoutKeys(env []string, drop ...string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		dropped := false
		for _, name := range drop {
			if key == name {
				dropped = true
				break
			}
		}
		if !dropped {
			out = append(out, entry)
		}
	}
	return out
}

// Lookup returns the value of key in env and whether it was present at all.
// The two are different facts: a variable exported empty and a variable never
// exported produce different child behaviour, and one string cannot say which.
func Lookup(env []string, key string) (string, bool) {
	prefix := key + "="
	value, found := "", false
	for _, entry := range env {
		if entry == key {
			value, found = "", true
			continue
		}
		if strings.HasPrefix(entry, prefix) {
			value, found = strings.TrimPrefix(entry, prefix), true
		}
	}
	return value, found
}

// EnvMap renders an environment as key to value, last entry winning, which is
// what an exec'd child would see.
func EnvMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		out[key] = value
	}
	return out
}

// PrefixStripSystem is the port defect the goldens' near-miss bystanders exist
// to catch: filtering a whole family by PREFIX before handing the rest to the
// real filter.
//
// "Strip the CODEX_ family" is a reading a port author can arrive at honestly,
// and a real filter's blocked set is a KEY map, so the two differ only on keys
// that LOOK like the family without being in it. It wraps a real plugin and
// overrides exactly one method, so everything else about the plan stays correct
// and the comparison isolates the environment.
type PrefixStripSystem struct {
	agentic.System
	Prefixes []string
}

// ChildEnv strips by prefix, then defers to the real plugin.
func (p PrefixStripSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	kept := make([]string, 0, len(parent))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		over := false
		for _, prefix := range p.Prefixes {
			if strings.HasPrefix(key, prefix) {
				over = true
				break
			}
		}
		if !over {
			kept = append(kept, entry)
		}
	}
	return p.System.ChildEnv(kept, req)
}

// WholeEnvWipeSystem is the other permanent negative: a ChildEnv that returns
// only its own injections.
//
// It is written as the real ChildEnv over an EMPTY parent rather than as a
// hand-listed set of injections, which is what makes it a plausible mistake
// rather than a contrived one: it is what a port author writes after reading a
// golden whose env_removed covers its whole parent_env and concluding "this
// system keeps nothing of its parent".
type WholeEnvWipeSystem struct{ agentic.System }

// ChildEnv answers as the real plugin would if it had been handed nothing.
func (w WholeEnvWipeSystem) ChildEnv(_ []string, req agentic.LaunchRequest) ([]string, error) {
	return w.System.ChildEnv(nil, req)
}
