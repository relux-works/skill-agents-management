package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Claude resolves one way — PATH — so this file is short on purpose. What it
// has to prove is that the resolution reads the LAUNCH environment rather than
// the process's own, and that an absent PATH and a missing install stay
// different facts.

// TestResolutionReadsTheLaunchEnvironment is the port's one reshape in
// binary.go, measured through BuildPlan.
//
// The stub is written into a temp directory that is on the launch environment's
// PATH and on nothing else, so a plugin that consulted os.Environ would land on
// whatever claude the developer's machine has installed — or on nothing.
func TestResolutionReadsTheLaunchEnvironment(t *testing.T) {
	t.Parallel()
	binDir, workDir := tempSlot(t), tempSlot(t)
	want := writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Env = []string{"PATH=" + binDir}

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	if plan.Binary != want {
		t.Fatalf("resolved %q, want the stub at %q; resolution read something other than the launch environment", plan.Binary, want)
	}
	if strings.HasPrefix(plan.Binary, "/usr/") || strings.HasPrefix(plan.Binary, "/opt/") {
		t.Fatalf("resolution landed on %q, which is an installed binary rather than this test's stub", plan.Binary)
	}
}

// TestTheDryRunReportsTheLaunchTarget is the contract requirement the source
// violated and a parity capture caught: a dry run must name the executable a
// real launch would run, not a display placeholder.
func TestTheDryRunReportsTheLaunchTarget(t *testing.T) {
	t.Parallel()
	binDir, workDir := tempSlot(t), tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Env = []string{"PATH=" + binDir}

	launch := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	dry := buildParityPlan(t, New(), req, agentic.LaunchModeDryRun)
	if dry.Binary != launch.Binary {
		t.Errorf("the dry run reports %q while a launch would run %q", dry.Binary, launch.Binary)
	}
	if dry.Binary == executableName {
		t.Errorf("the dry run reported the bare program name %q rather than a resolved path; that is the source's own BuildArgs bug", dry.Binary)
	}
}

// TestAnAbsentPathIsNotAMissingBinary holds the distinction the resolution
// rests on: an absence and a failure to find are different facts, and a caller
// that curated an environment without PATH is not told its claude install is
// missing.
func TestAnAbsentPathIsNotAMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := resolveBinary([]string{"HOME=/nowhere"})
	if !errors.Is(err, ErrNoPathInLaunchEnvironment) {
		t.Errorf("resolveBinary with no PATH returned %v, want %v", err, ErrNoPathInLaunchEnvironment)
	}

	_, emptyErr := resolveBinary([]string{"PATH="})
	if emptyErr == nil {
		t.Fatal("resolveBinary with an empty PATH found a binary")
	}
	if errors.Is(emptyErr, ErrNoPathInLaunchEnvironment) {
		t.Error("an EMPTY PATH was reported as an ABSENT one; the caller asked to resolve against nothing and got told it supplied no environment")
	}
}

// TestACandidateMustBeExecutable narrows PATH resolution: a non-executable file
// with the right name is not a resolution, or a directory full of source files
// could shadow the real binary.
func TestACandidateMustBeExecutable(t *testing.T) {
	t.Parallel()
	shadow := tempSlot(t)
	if err := os.WriteFile(filepath.Join(shadow, executableName), []byte("not executable\n"), 0o644); err != nil {
		t.Fatalf("writing the non-executable candidate: %v", err)
	}
	installed := tempSlot(t)
	want := writeStubExecutable(t, installed, executableName)

	got, err := resolveBinary([]string{"PATH=" + shadow + string(os.PathListSeparator) + installed})
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != want {
		t.Errorf("resolveBinary = %q, want %q; a non-executable file with the right name shadowed the real one", got, want)
	}
}

// TestResolutionFailureIsARefusalNotAPlan closes the loop at the production
// boundary: a launch whose binary cannot be found produces an error, never a
// plan naming a program that does not exist.
func TestResolutionFailureIsARefusalNotAPlan(t *testing.T) {
	t.Parallel()
	req := parityRequest(tempSlot(t), parityPromptRunID, parityPromptTaskID)
	req.Env = []string{"PATH=" + tempSlot(t)}

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); err == nil {
		t.Fatal("a launch resolved a plan against a PATH containing no claude")
	}
}
