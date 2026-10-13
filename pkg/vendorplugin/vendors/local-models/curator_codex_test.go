package localmodels_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

// curatorCodexRuntimes declares the codex local-codex runtime from the Curator
// catalog, mirroring effortConfig's required-effort row without engine refs.
func curatorCodexRuntimes(project string) string {
	return `schema_version = 1

[runtimes.local-codex]
system = "codex"

[runtimes.local-codex.models."unique-local-model"]
description = "Consumer-selected local model"
lifecycle = "current"
effort_support = "required"
effort_vocabulary = ["low", "high"]
recommended_effort = "high"

[runtimes.local-codex.models."unique-local-model".pointer]
curator_engines_project = "` + project + `"
curator_engines_profile = "local-codex"
`
}

// TestBuildLaunchDryRunPlansCodexLocalProviderFromCuratorCatalog is AC1 (codex
// half): curator-resolved rows for a codex runtime plan a dry-run launch
// through the real vendorplugin.BuildLaunch entry point with the validated
// private local-provider binding.
func TestBuildLaunchDryRunPlansCodexLocalProviderFromCuratorCatalog(t *testing.T) {
	curatorDir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(curatorDir, "runtimes.toml"), []byte(curatorCodexRuntimes(project)), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warning, err := localmodels.LoadAt(curatorDir, filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadAt: %v", err)
	}
	if warning != nil {
		t.Fatalf("curator-only resolution carries a warning: %v", warning.Warning())
	}
	registry, err := effortRegistry(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := localCodexRequest(t)
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
	if err != nil {
		t.Fatalf("BuildLaunch dry-run: %v", err)
	}
	argv := strings.Join(plan.Argv, "\n")
	for _, want := range []string{`model_provider="local-story"`, `model_reasoning_effort="low"`, localEffortModel} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv = %q, missing %q", plan.Argv, want)
		}
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec: %v", err)
	}
	foundEnv := false
	for _, entry := range plan.Env {
		if entry == "CURATOR_ENGINES_PROJECT_DIR="+project {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("plan.Env = %v; missing curator pointer project %q", plan.Env, project)
	}
}
