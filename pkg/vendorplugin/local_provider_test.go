package vendorplugin_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	_ "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/openai"
)

func TestBuildLaunchCarriesCodexLocalProviderBindingToTheCLI(t *testing.T) {
	home, workDir, binDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write Codex stub: %v", err)
	}
	config := `model_provider = "openai"

[model_providers.local-proof]
name = "Task local proof"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write private Codex config: %v", err)
	}
	request := vendorplugin.SpawnRequest{
		Runtime:       "codex",
		Model:         "gpt-6-astra",
		Effort:        "high",
		Prompt:        []byte("write a marker"),
		WorkDir:       workDir,
		Home:          home,
		LocalProvider: &agentic.LocalProviderBinding{ID: "local-proof"},
		Env:           []string{"PATH=" + binDir, "HOME=" + filepath.Dir(home), "CODEX_HOME=" + home},
	}
	localPlan, err := vendorplugin.BuildLaunch(context.Background(), vendorplugin.Default, request, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(local Codex provider): %v", err)
	}
	if !containsProviderConfig(localPlan.Argv, `model_provider="local-proof"`) ||
		!containsProviderConfig(localPlan.Argv, `model_providers.local-proof.base_url="http://127.0.0.1:38171/v1"`) ||
		!containsProviderConfig(localPlan.Argv, `model_providers.local-proof.requires_openai_auth=false`) {
		t.Fatalf("BuildLaunch argv does not pin the private local provider: %q", localPlan.Argv)
	}

	nativeRequest := request
	nativeRequest.LocalProvider = nil
	nativePlan, err := vendorplugin.BuildLaunch(context.Background(), vendorplugin.Default, nativeRequest, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(native subscription): %v", err)
	}
	if containsProviderConfig(nativePlan.Argv, "model_provider=") {
		t.Fatalf("native subscription argv was switched by the private local table: %q", nativePlan.Argv)
	}
	if !reflect.DeepEqual(nativePlan.Argv, []string{
		"--search", "-a", "never", "exec", "-m", "gpt-6-astra",
		"-c", `model_reasoning_effort="high"`, bypassFlag(),
		"--skip-git-repo-check", "-C", workDir, "-",
	}) {
		t.Fatalf("native subscription argv changed: %q", nativePlan.Argv)
	}
}

func TestBuildLaunchPinsLocalProviderHomeForTheCodexChild(t *testing.T) {
	home, operatorHome, workDir, binDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write Codex stub: %v", err)
	}
	config := `model_provider = "openai"

[model_providers.local-proof]
name = "Task local proof"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write validated private Codex config: %v", err)
	}
	decoyHome := filepath.Join(operatorHome, ".codex")
	if err := os.MkdirAll(decoyHome, 0o700); err != nil {
		t.Fatalf("create operator default Codex home: %v", err)
	}
	decoyConfig := `model_provider = "local-proof"

[model_providers.local-proof]
name = "unvalidated home"
base_url = "http://127.0.0.1:38172/v1"
wire_api = "responses"
requires_openai_auth = false
env_key = "OPENAI_API_KEY"

[model_providers.local-proof.http_headers]
X-Unvalidated-Home = "operator-default"
`
	if err := os.WriteFile(filepath.Join(decoyHome, "config.toml"), []byte(decoyConfig), 0o600); err != nil {
		t.Fatalf("write operator default Codex config: %v", err)
	}

	plan, err := vendorplugin.BuildLaunch(context.Background(), vendorplugin.Default, vendorplugin.SpawnRequest{
		Runtime:       "codex",
		Model:         "gpt-6-astra",
		Effort:        "high",
		Prompt:        []byte("write a marker"),
		WorkDir:       workDir,
		Home:          home,
		LocalProvider: &agentic.LocalProviderBinding{ID: "local-proof"},
		Env:           []string{"PATH=" + binDir, "HOME=" + operatorHome},
	}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(local Codex provider with no incoming CODEX_HOME): %v", err)
	}
	var gotHome string
	for _, entry := range plan.Env {
		if key, value, found := strings.Cut(entry, "="); found && key == "CODEX_HOME" {
			gotHome = value
		}
	}
	if gotHome != home {
		t.Fatalf("BuildLaunch child CODEX_HOME = %q, want validated binding home %q", gotHome, home)
	}
	if !containsProviderConfig(plan.Argv, `model_providers.local-proof.base_url="http://127.0.0.1:38171/v1"`) {
		t.Fatalf("BuildLaunch did not pin the provider validated from the private home: %q", plan.Argv)
	}
}

func containsProviderConfig(args []string, expected string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "-c" && args[index+1] == expected {
			return true
		}
	}
	return false
}

func bypassFlag() string { return "--dangerously-bypass-approvals-and-sandbox" }
