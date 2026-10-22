package codex

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Carried operator model settings must not become second writers, even when
// they disagree with the plan. This exercises both modes and binding paths.
func TestCarriedSettingsPlanOwnedHaveOneWriter(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		for _, binding := range []string{"id", "snapshot"} {
			for _, row := range []struct{ key, value string }{
				{"model", `"operator-model"`},
				{"model_reasoning_effort", `"low"`},
				{"service_tier", `"default"`},
			} {
				t.Run(mode.String()+"/"+binding+"/"+row.key, func(t *testing.T) {
					home, work := t.TempDir(), t.TempDir()
					config := writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
					writeLocalCatalog(t, home, "Qwen3.8-27B-Q4_K_M", []string{"low", "high"})
					// The carrier represents the operator config's top-level value.
					writeProviderConfigBody(t, home, row.key+" = "+row.value+"\n"+config)
					req := catalogRequest(t, home, work, "high")
					req.ServiceTier = "priority"
					if binding == "snapshot" {
						req = withSnapshot(req, mustSnapshot(t, req))
					}
					// Even a duplicate matching the planned value is dropped: equality
					// must not create another writer. The carrier can combine layers.
					matching := map[string]string{"model": strconv.Quote(req.Model.ID), "model_reasoning_effort": `"high"`, "service_tier": `"priority"`}[row.key]
					req.LocalProvider.Settings = []agentic.LocalProviderSetting{{Key: row.key, Value: row.value}, {Key: row.key, Value: matching}}
					before := append([]agentic.LocalProviderSetting(nil), req.LocalProvider.Settings...)
					plan, err := buildCodexPlanForMode(t, req, mode)
					if err != nil {
						t.Fatalf("plan-owned setting must be dropped: %v", err)
					}
					for key, want := range map[string]string{"model": req.Model.ID, "model_reasoning_effort": "high", "service_tier": "priority"} {
						got := settingsWriters(plan.Argv, key)
						if !reflect.DeepEqual(got, []string{want}) {
							t.Errorf("%s writers = %q, want exactly [%s]", key, got, want)
						}
					}
					if !reflect.DeepEqual(before, req.LocalProvider.Settings) {
						t.Fatal("caller settings mutated")
					}
					if err := plan.VerifyBeforeExec(); err != nil {
						t.Fatalf("dropped setting did not seal: %v", err)
					}
				})
			}
		}
	}
}

// These real and future model keys have no sealed writer. Plausible prefixes
// and other plan-owned families never widen the cosmetic carry allow-list.
func TestCarriedSettingsUnownedModelKeysStillRefuse(t *testing.T) {
	for _, key := range []string{"model_reasoning_summary", "model_future_x", "model_catalog_json", "model_provider", "base_url", "auth", "sandbox_mode", "network_access", "tools.exec"} {
		t.Run(key, func(t *testing.T) {
			home, work := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
			req := settingsRequest(t, home, work, []agentic.LocalProviderSetting{{Key: key, Value: "true"}})
			plan, err := buildCodexPlan(t, req)
			requireLocalSettingsRefusal(t, err, agentic.LocalProviderUnsupported, agentic.ErrLocalProviderUnsupported, key, "true")
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatal("refusal returned a partial plan")
			}
		})
	}
}

// Ownership is conditional on an actual emitted override, not the vocabulary
// of keys this harness could emit for another request.
func TestCarriedSettingsAbsentCompanionsStillRefuse(t *testing.T) {
	for _, key := range []string{"model_reasoning_effort", "service_tier"} {
		t.Run(key, func(t *testing.T) {
			req := settingsRequest(t, t.TempDir(), t.TempDir(), []agentic.LocalProviderSetting{{Key: key, Value: `"operator"`}})
			req.Effort, req.ServiceTier = "", "unknown-tier"
			_, err := buildCodexPlan(t, req)
			requireLocalSettingsRefusal(t, err, agentic.LocalProviderUnsupported, agentic.ErrLocalProviderUnsupported, key, `"operator"`)
		})
	}
}

func settingsWriters(argv []string, key string) []string {
	var result []string
	for i := 0; i+1 < len(argv); i++ {
		if key == "model" && (argv[i] == "-m" || argv[i] == "--model") {
			result = append(result, argv[i+1])
		}
		if argv[i] == "-c" && strings.HasPrefix(argv[i+1], key+"=") {
			value := strings.TrimPrefix(argv[i+1], key+"=")
			if unquoted, err := strconv.Unquote(value); err == nil {
				value = unquoted
			}
			result = append(result, value)
		}
	}
	return result
}

// Frozen before the implementation change at 938675b5: every exported plan
// byte for a cosmetic-only carrier remains identical (fixture paths normalized).
func TestCarriedSettingsWithoutOwnedKeysMatchBaseline(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
	req := settingsRequest(t, home, work, []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "true"}})
	// Use the stub's exact PATH, independent of the host environment.
	layout := newStubLayout(t)
	writeStubExecutable(t, layout.binDir, executableName)
	req.Env = []string{"PATH=" + layout.binDir, "CODEX_HOME=" + home}
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	plan.Binary = "<binary>"
	for i, entry := range plan.Env {
		plan.Env[i] = strings.NewReplacer(layout.binDir, "<bin>", home, "<home>").Replace(entry)
	}
	catalog := effectiveCatalogPath(t, plan)
	for i, arg := range plan.Argv {
		plan.Argv[i] = strings.NewReplacer(catalog, "<catalog>", work, "<work>").Replace(arg)
	}
	plan.Home, plan.WorkDir = "<home>", "<work>"
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/carried-settings-baseline.json")
	if err != nil {
		t.Fatalf("baseline missing: %v\n%s", err, data)
	}
	if string(data)+"\n" != string(want) {
		t.Fatalf("plan changed from pre-fix baseline:\n%s", data)
	}
}
