package agentic_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
)

type managedNilSealer struct{ *codex.System }

func (managedNilSealer) SealManagedLaunch(agentic.LaunchRequest, agentic.Plan, agentic.ExecPlanVerifier) (agentic.ExecPlanVerifier, error) {
	return nil, nil
}

// Unsupported plugins refuse rather than dropping managed fields, and a trusted
// plugin violating the managed seal contract cannot return a successful plan.
func TestManagedPlannerContract(t *testing.T) {
	dir := t.TempDir()
	req := agentic.LaunchRequest{System: claude.New().ID(), Model: agentic.Model{ID: "test"}, ManagerToolDir: &dir}
	r := agentic.NewRegistry()
	if err := r.Register(claude.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.BuildPlan(r, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
		t.Fatalf("unsupported managed input admitted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r = agentic.NewRegistry()
	if err := r.Register(managedNilSealer{codex.New()}); err != nil {
		t.Fatal(err)
	}
	req.System = codex.New().ID()
	req.Env = []string{"PATH=" + dir}
	req.WorkDir = t.TempDir()
	if _, err := agentic.BuildPlan(r, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPluginContract) {
		t.Fatalf("missing managed seal admitted: %v", err)
	}
}
