package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

// Production composition (without an injected binding factory) reads status
// once with a fixture executable. Exact provenance reaches Pi; absent/foreign
// evidence refuses before preflight or Pi execution. Missing catalog does not
// execute status at all. This is admission wiring, not real model inference.
func TestModelCheckCatalogProductionBinding(t *testing.T) {
	for _, mode := range []string{"attested", "absent-provenance", "foreign-catalog", "foreign-cache", "missing-catalog"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			catalog := filepath.Join(dir, "engines.toml")
			if err := os.WriteFile(catalog, []byte("[engines.model-check-test]\nengine = \"mlx\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := modelCheckTestConfig(true)
			cfg.EnginesConfig = catalog
			// Exercise a manually composed Config without an identity-only graph node:
			// the public binding must supply its concrete graph plugin independently.
			cfg.InferenceEngines = nil
			row := cfg.Runtimes[0].Models[modelCheckTestModel]
			row.Pointer.CuratorEnginesProject = dir
			cfg.Runtimes[0].Models[modelCheckTestModel] = row
			emittedCatalog, emittedCache := catalog, dir
			if mode == "foreign-catalog" {
				emittedCatalog = filepath.Join(dir, "B", "engines.toml")
			}
			if mode == "foreign-cache" {
				emittedCache = filepath.Join(dir, "B-cache")
			}
			body := modelCheckStatusBody(t, inferenceengine.ContractVersionV3, modelCheckHealthyRows())
			if mode != "absent-provenance" {
				var members map[string]any
				if err := json.Unmarshal(body, &members); err != nil {
					t.Fatal(err)
				}
				members["provenance"] = map[string]string{"contract_version": "status-provenance/v1", "engines_config": emittedCatalog, "cache_root": emittedCache, "engines_config_sha256": strings.Repeat("a", 64)}
				body, err = json.Marshal(members)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "missing-catalog" {
				cfg.EnginesConfig = ""
			}
			output, marker := filepath.Join(dir, "response.json"), filepath.Join(dir, "status-called")
			if err := os.WriteFile(output, body, 0600); err != nil {
				t.Fatal(err)
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			script := "#!/bin/sh\nprintf called > " + quote(marker) + "\nexec /bin/cat " + quote(output) + "\n"
			if err := execfixture.WriteFile(filepath.Join(dir, "curator-engines"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			reader := &modelCheckTestStatusReader{status: localruntime.Status{BrokerState: "serving", BrokerSource: localruntime.SourceAttested}}
			piCalls := 0
			setModelCheckTestDeps(t, localmodels.ConfigResult{Config: cfg}, reader, func(context.Context, agentic.Plan) modelCheckProcessResult {
				piCalls++
				return modelCheckProcessResult{Started: true, Output: []byte("expected-fragment-for-test")}
			})
			modelCheckDeps.newEngineBinding = newModelCheckEngineBinding
			modelCheckDeps.cacheRoot = func() (string, error) { return dir, nil }
			stdout, _, err := runRoot(t, modelCheckTestArgs(filepath.Join(dir, "evidence.json"), "10s")...)
			report := decodeModelCheckReport(t, stdout)
			_, statErr := os.Stat(marker)
			if mode == "attested" {
				if err != nil || report.Status != modelCheckStatusPassed || !report.PiStarted || piCalls != 1 || reader.calls != 1 || statErr != nil {
					t.Fatalf("attested report=%+v err=%v Pi=%d preflight=%d marker=%v", report, err, piCalls, reader.calls, statErr)
				}
			} else {
				wantReason := "engine_observation_unavailable"
				wantStatus := modelCheckStatusUnknown
				if mode == "missing-catalog" {
					wantReason = "configured_model_unavailable"
					wantStatus = modelCheckStatusFailed
				}
				if refusal := requireModelCheckRefusal(t, err); refusal.reason != wantReason {
					t.Fatalf("refusal %s want %s", refusal.reason, wantReason)
				}
				if report.Status != wantStatus || report.PiStarted || piCalls != 0 || reader.calls != 0 {
					t.Fatalf("refused report=%+v Pi=%d preflight=%d", report, piCalls, reader.calls)
				}
				if mode == "missing-catalog" {
					if !os.IsNotExist(statErr) {
						t.Fatalf("status executed without catalog: %v", statErr)
					}
				} else if statErr != nil {
					t.Fatalf("status fixture did not execute: %v", statErr)
				}
			}
		})
	}
}
