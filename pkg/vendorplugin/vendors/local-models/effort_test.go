package localmodels_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

const localEffortModel = "unique-local-model"

func effortConfig(t *testing.T, support, fields string) localmodels.Config {
	t.Helper()
	body := fmt.Sprintf(`
[runtimes.local-codex]
system = "codex"
[runtimes.local-codex.models.unique-local-model]
description = "Consumer-selected local model"
lifecycle = "current"
effort_support = %q
%s
[runtimes.local-codex.models.unique-local-model.pointer]
curator_engines_project = %q
curator_engines_profile = "local-codex"
`, support, fields, t.TempDir())
	cfg, err := localmodels.ParseConfig([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func effortRegistry(t *testing.T, cfg localmodels.Config) (*vendorplugin.Registry, error) {
	t.Helper()
	systems := agentic.NewRegistry()
	if err := systems.Register(codex.New()); err != nil {
		t.Fatal(err)
	}
	registry := vendorplugin.NewRegistry(systems)
	if err := registry.Register(localmodels.New(cfg)); err != nil {
		return registry, err
	}
	if err := registry.DeclareRuntime(vendorplugin.RuntimeDeclaration{
		ID: "local-codex", System: "codex", Vendor: localmodels.VendorID,
		Broker: vendorplugin.BrokerProvenance{Checked: []string{"test"}, Found: "test"},
	}); err != nil {
		t.Fatal(err)
	}
	return registry, nil
}

func TestLocalModelsEffortDeclarationCarrier(t *testing.T) {
	cfg := effortConfig(t, "required", `effort_vocabulary = ["low", "high"]
recommended_effort = "high"`)
	if _, err := effortRegistry(t, cfg); err != nil {
		t.Fatalf("Register(required): %v", err)
	}
	vendor := localmodels.New(cfg)
	model := vendor.Models()[0]
	want := vendorplugin.EffortDeclaration{Support: agentic.EffortSupportRequired, Vocabulary: []string{"low", "high"}, Recommended: "high"}
	if !reflect.DeepEqual(model.Effort, want) {
		t.Fatalf("Effort = %#v, want %#v", model.Effort, want)
	}
	launch, err := vendor.Spawn(vendorplugin.SpawnContext{
		Runtime: vendorplugin.Runtime{ID: "local-codex", SystemID: "codex"},
		Model:   model, Effort: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(launch.Model.EffortVocabulary, want.Vocabulary) {
		t.Fatalf("Spawn vocabulary = %v", launch.Model.EffortVocabulary)
	}
	launch.Model.EffortVocabulary[0] = "max"
	if !reflect.DeepEqual(model.Effort.Vocabulary, want.Vocabulary) {
		t.Fatal("Spawn vocabulary aliases its admitted model")
	}
	model.Effort.Vocabulary[0] = "max"
	if !reflect.DeepEqual(vendor.Models()[0].Effort, want) {
		t.Fatal("returned vocabulary aliases the vendor configuration")
	}
}

func TestLocalModelsRefusesContradictoryEffortDeclaration(t *testing.T) {
	for _, tc := range []struct{ name, support, fields string }{
		{"required_without_vocabulary", "required", ""},
		{"recommendation_outside_vocabulary", "required", `effort_vocabulary = ["low"]
recommended_effort = "max"`},
		{"none_with_vocabulary", "none", `effort_vocabulary = ["low"]`},
		{"none_with_recommendation", "none", `recommended_effort = "low"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := effortRegistry(t, effortConfig(t, tc.support, tc.fields))
			if !errors.Is(err, vendorplugin.ErrEffortDeclaration) {
				t.Fatalf("Register = %v, want ErrEffortDeclaration", err)
			}
		})
	}
}

func TestLocalModelsNoneEffortStaysEffortless(t *testing.T) {
	cfg := effortConfig(t, "none", "")
	registry, err := effortRegistry(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	model := localmodels.New(cfg).Models()[0]
	if model.Effort.Support != agentic.EffortSupportNone || len(model.Effort.Vocabulary) != 0 || model.Effort.Recommended != "" {
		t.Fatalf("none axis = %#v", model.Effort)
	}
	req := vendorplugin.SpawnRequest{Runtime: "local-codex", Model: localEffortModel, Effort: "low"}
	if _, err := vendorplugin.BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun); !errors.Is(err, vendorplugin.ErrEffortNotInVocabulary) {
		t.Fatalf("BuildLaunch(none, supplied effort) = %v", err)
	}
}

func localCodexRequest(t *testing.T) vendorplugin.SpawnRequest {
	t.Helper()
	home, work, bin := t.TempDir(), t.TempDir(), t.TempDir()
	// Use the same native schema fixture as the frozen catalog-binding tests.
	raw, err := os.ReadFile("../../../agentic/systems/codex/testdata/native-local-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	row := catalog["models"].([]any)[0].(map[string]any)
	row["slug"] = localEffortModel
	row["supported_reasoning_levels"] = []map[string]string{{"effort": "low", "description": "Low"}, {"effort": "high", "description": "High"}}
	raw, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	config := `model_provider = "openai"
model_catalog_json = "catalog.json"
[model_providers.local-story]
name = "Consumer local provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n: > \"$0.started\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return vendorplugin.SpawnRequest{
		Runtime: "local-codex", Model: localEffortModel, Effort: "low", Prompt: []byte("inspect the repository"),
		Home: home, WorkDir: work, Env: []string{"PATH=" + bin, "HOME=" + t.TempDir(), "CODEX_HOME=" + home},
		LocalProvider: &agentic.LocalProviderBinding{ID: "local-story"},
	}
}

func TestLaunchSelectsOnlyValidatedCodexLocalProvider(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun, agentic.LaunchModeExec} {
		for _, path := range []string{"id", "snapshot"} {
			t.Run(mode.String()+"/"+path, func(t *testing.T) {
				cfg := effortConfig(t, "required", `effort_vocabulary = ["low", "high"]
recommended_effort = "high"`)
				registry, err := effortRegistry(t, cfg)
				if err != nil {
					t.Fatal(err)
				}
				req := localCodexRequest(t)
				if path == "snapshot" {
					snapshot, err := codex.ReadProviderSnapshot(agentic.LaunchRequest{
						Home: req.Home, WorkDir: req.WorkDir, Env: req.Env, LocalProvider: req.LocalProvider,
						Model: agentic.Model{ID: string(req.Model)},
					})
					if err != nil {
						t.Fatal(err)
					}
					req.LocalProvider.Snapshot = snapshot
					// Snapshot planning must retain bytes rather than reread mutable inputs.
					if err := os.Remove(filepath.Join(req.Home, "config.toml")); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(filepath.Join(req.Home, "catalog.json")); err != nil {
						t.Fatal(err)
					}
				}
				plan, err := vendorplugin.BuildLaunch(context.Background(), registry, req, mode)
				if err != nil {
					t.Fatalf("BuildLaunch: %v", err)
				}
				argv := strings.Join(plan.Argv, "\n")
				for _, want := range []string{`model_provider="local-story"`, `model_providers.local-story.requires_openai_auth=false`, `model_reasoning_effort="low"`, localEffortModel, "model_catalog_json="} {
					if !strings.Contains(argv, want) {
						t.Fatalf("argv = %q, missing %q", plan.Argv, want)
					}
				}
				if err := plan.VerifyBeforeExec(); err != nil {
					t.Fatalf("VerifyBeforeExec: %v", err)
				}
				binary := filepath.Join(strings.TrimPrefix(req.Env[0], "PATH="), "codex")
				if _, err := os.Stat(binary + ".started"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("planning started native process: %v", err)
				}
				if mode == agentic.LaunchModeExec {
					command := exec.Command(plan.Binary, plan.Argv...)
					command.Env, command.Dir = plan.Env, plan.WorkDir
					if err := command.Run(); err != nil {
						t.Fatal(err)
					}
					if _, err := os.Stat(binary + ".started"); err != nil {
						t.Fatalf("exec plan did not run selected binary: %v", err)
					}
				}
				recommended := req
				recommended.Effort = "high"
				recommendedPlan, err := vendorplugin.BuildLaunch(context.Background(), registry, recommended, mode)
				if err != nil {
					t.Fatalf("explicit recommended effort: %v", err)
				}
				if !strings.Contains(strings.Join(recommendedPlan.Argv, "\n"), `model_reasoning_effort="high"`) {
					t.Fatalf("explicit high not transported: %v", recommendedPlan.Argv)
				}

				for _, tc := range []struct {
					effort string
					want   error
				}{{"", vendorplugin.ErrEffortMissing}, {"max", vendorplugin.ErrEffortNotInVocabulary}} {
					rejected := req
					rejected.Effort = tc.effort
					if _, err := vendorplugin.BuildLaunch(context.Background(), registry, rejected, mode); !errors.Is(err, tc.want) {
						t.Fatalf("BuildLaunch(effort %q) = %v, want %v", tc.effort, err, tc.want)
					}
				}
			})
		}
	}
}

func TestLocalModelsDeclaredVocabularyMustFitNativeCatalog(t *testing.T) {
	cfg := effortConfig(t, "required", `effort_vocabulary = ["low", "max"]
recommended_effort = "low"`)
	registry, err := effortRegistry(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeDryRun, agentic.LaunchModeExec} {
		for _, path := range []string{"id", "snapshot"} {
			t.Run(mode.String()+"/"+path, func(t *testing.T) {
				req := localCodexRequest(t)
				if path == "snapshot" {
					snapshot, err := codex.ReadProviderSnapshot(agentic.LaunchRequest{Home: req.Home, Env: req.Env, WorkDir: req.WorkDir, Model: agentic.Model{ID: string(req.Model)}, LocalProvider: req.LocalProvider})
					if err != nil {
						t.Fatal(err)
					}
					req.LocalProvider.Snapshot = snapshot
				}
				// low is supported by both axes; only the unselected max makes this row invalid.
				if _, err := vendorplugin.BuildLaunch(context.Background(), registry, req, mode); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
					t.Fatalf("BuildLaunch(unsupported declared max, selected low) = %v, want ErrLocalProviderUnsupported", err)
				}
			})
		}
	}
}
