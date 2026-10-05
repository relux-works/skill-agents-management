package vendorplugin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)

// spawnTempDirPtr states a present per-launch temp dir on a spawn request.
// Absence is nil; a pointer to an empty string is present-empty and refuses.
func spawnTempDirPtr(value string) *string { return &value }

// codexTempDirRequest builds a native-subscription Codex spawn through the
// vendor layer's own registry, with a stub binary on PATH.
func codexTempDirRequest(t *testing.T, tempDir *string) (vendorplugin.SpawnRequest, string) {
	t.Helper()
	workDir, binDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write Codex stub: %v", err)
	}
	return vendorplugin.SpawnRequest{
		Runtime: "codex",
		Model:   "gpt-6-astra",
		Effort:  "high",
		Prompt:  []byte("write a marker"),
		WorkDir: workDir,
		TempDir: tempDir,
		Env:     []string{"PATH=" + binDir},
	}, workDir
}

// TestBuildLaunchCarriesTempDirToTheCodexChild proves the vendor layer
// forwards the host's per-launch temp dir unchanged: SpawnRequest.TempDir
// reaches the plan's child environment as the single TMPDIR entry.
func TestBuildLaunchCarriesTempDirToTheCodexChild(t *testing.T) {
	dir := t.TempDir()
	request, _ := codexTempDirRequest(t, spawnTempDirPtr(dir))
	plan, err := vendorplugin.BuildLaunch(context.Background(), vendorplugin.Default, request, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	var found []string
	for _, entry := range plan.Env {
		if name, value, ok := strings.Cut(entry, "="); ok && name == "TMPDIR" {
			found = append(found, value)
		}
	}
	if len(found) != 1 || found[0] != dir {
		t.Fatalf("plan TMPDIR entries = %v, want exactly [%s]", found, dir)
	}
}

// TestBuildLaunchLeavesTmpdirAloneWithoutTempDir proves the vendor layer
// adds no TMPDIR of its own: an absent TempDir keeps the child environment
// free of module-written entries.
func TestBuildLaunchLeavesTmpdirAloneWithoutTempDir(t *testing.T) {
	request, _ := codexTempDirRequest(t, nil)
	plan, err := vendorplugin.BuildLaunch(context.Background(), vendorplugin.Default, request, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	for _, entry := range plan.Env {
		if name, _, ok := strings.Cut(entry, "="); ok && name == "TMPDIR" {
			t.Fatalf("absent TempDir produced %q through BuildLaunch", entry)
		}
	}
}

// TestBuildLaunchRefusesRelativeTempDirTyped proves the layer-one shape
// gate reports through the vendor layer: a relative, blank, present-empty
// or non-clean TempDir refuses with the same typed refusal BuildPlan
// returns.
func TestBuildLaunchRefusesRelativeTempDirTyped(t *testing.T) {
	nonClean := filepath.Join(t.TempDir(), "run") + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "escape"
	for _, tempDir := range []*string{spawnTempDirPtr("relative/path"), spawnTempDirPtr("   "), spawnTempDirPtr(""), spawnTempDirPtr(nonClean)} {
		request, _ := codexTempDirRequest(t, tempDir)
		if _, err := vendorplugin.BuildLaunch(context.Background(), vendorplugin.Default, request, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
			t.Fatalf("BuildLaunch with TempDir %q err = %v, want ErrTempDirInvalid", *tempDir, err)
		}
	}
}
