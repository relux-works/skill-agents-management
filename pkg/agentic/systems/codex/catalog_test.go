package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// writeLocalCatalog writes a minimal native catalog JSON file carrying one
// entry for slug with the given effort vocabulary, using the exact shape the
// plugin validates: Lite off, direct tools, multi-agent v1, no search, no
// reasoning-summary parameter. It returns the file basename for use as a
// relative model_catalog_json value.
func writeLocalCatalog(t *testing.T, home, slug string, vocab []string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/native-local-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	row := catalog["models"].([]any)[0].(map[string]any)
	row["slug"] = slug
	levels := make([]map[string]string, 0, len(vocab))
	for _, word := range vocab {
		levels = append(levels, map[string]string{"effort": word, "description": "test"})
	}
	row["supported_reasoning_levels"] = levels
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal test catalog: %v", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create Codex home: %v", err)
	}
	name := "catalog.local.json"
	if err := os.WriteFile(filepath.Join(home, name), data, 0o600); err != nil {
		t.Fatalf("write test catalog: %v", err)
	}
	return name
}

// writeLocalCatalogRaw writes the given bytes as the catalog file, for
// malformed-JSON and shape tests.
func writeLocalCatalogRaw(t *testing.T, home, name string, data []byte) {
	t.Helper()
	var delta map[string]any
	if json.Unmarshal(data, &delta) == nil {
		if rows, ok := delta["models"].([]any); ok {
			raw, err := os.ReadFile("testdata/native-local-catalog.json")
			if err != nil {
				t.Fatal(err)
			}
			var template map[string]any
			if err := json.Unmarshal(raw, &template); err != nil {
				t.Fatal(err)
			}
			for i, value := range rows {
				if row, ok := value.(map[string]any); ok {
					var full map[string]any
					bytes, _ := json.Marshal(template["models"].([]any)[0])
					json.Unmarshal(bytes, &full)
					// Fields under the local protocol gate retain omission/null negatives.
					for _, key := range []string{"slug", "use_responses_lite", "tool_mode", "multi_agent_version", "supports_search_tool", "supports_reasoning_summary_parameter", "supported_reasoning_levels"} {
						delete(full, key)
					}
					for key, v := range row {
						full[key] = v
					}
					if levels, ok := full["supported_reasoning_levels"].([]any); ok {
						for _, l := range levels {
							if level, ok := l.(map[string]any); ok {
								level["description"] = "test"
							}
						}
					}
					rows[i] = full
				}
			}
			data, _ = json.Marshal(delta)
		}
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create Codex home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, name), data, 0o600); err != nil {
		t.Fatalf("write test catalog: %v", err)
	}
}

// catalogRequest builds a local-provider launch for the test slug with the
// given effort, through the production BuildPlan entry point.
func catalogRequest(t *testing.T, home, workDir, effort string) agentic.LaunchRequest {
	t.Helper()
	req := providerRequest(t, home, workDir, "local-story")
	req.Effort = effort
	return req
}

// TestLocalLaunchPinsNativeCatalogMetadata is AC1's positive: an exec and a
// dry-run local launch pins the validated catalog path via -c
// model_catalog_json, and the pinned file carries ordinary Responses with
// direct tools for the selected slug — never the hosted built-in's Lite
// branch.
func TestLocalLaunchPinsNativeCatalogMetadata(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := catalogRequest(t, home, workDir, "low")
			plan, err := buildCodexPlanForMode(t, req, mode)
			if err != nil {
				t.Fatalf("BuildPlan(local, %s): %v", mode, err)
			}
			wantCatalog := effectiveCatalogPath(t, plan)
			if !containsPair(plan.Argv, "-c", `model_catalog_json="`+wantCatalog+`"`) {
				t.Fatalf("local %s argv %q does not pin the validated catalog path", mode, plan.Argv)
			}
			// The pinned file is the operator metadata the gate validated:
			// Lite off, direct, v1, no search, no summary.
			data, err := os.ReadFile(wantCatalog)
			if err != nil {
				t.Fatalf("read pinned catalog: %v", err)
			}
			var catalog codexCatalog
			if err := json.Unmarshal(data, &catalog); err != nil {
				t.Fatalf("parse pinned catalog: %v", err)
			}
			if len(catalog.Models) != 1 || catalog.Models[0].Slug != "Qwen3.8-27B-Q4_K_M" {
				t.Fatalf("pinned catalog carries %v, want the single test slug", catalog.Models)
			}
			entry := catalog.Models[0]
			if entry.UseResponsesLite == nil || *entry.UseResponsesLite {
				t.Fatalf("pinned catalog selects Lite: %+v", entry)
			}
			if entry.ToolMode != "direct" || entry.MultiAgentVersion != "v1" {
				t.Fatalf("pinned catalog is not direct/v1: %+v", entry)
			}
			if entry.SupportsSearchTool == nil || *entry.SupportsSearchTool {
				t.Fatalf("pinned catalog enables search: %+v", entry)
			}
			if entry.SupportsReasoningSummaryParameter == nil || *entry.SupportsReasoningSummaryParameter {
				t.Fatalf("pinned catalog enables reasoning summary: %+v", entry)
			}
		})
	}
}

// TestLocalLaunchRefusesWhenNativeMetadataAbsent is AC1's negative: without
// operator metadata the launch refuses typed before any process starts — it
// never falls back to the hosted slug's built-in Lite entry.
func TestLocalLaunchRefusesWhenNativeMetadataAbsent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		kind      agentic.LocalProviderRefusalKind
		want      error
		configure func(t *testing.T, req *agentic.LaunchRequest)
	}{
		{
			name: "missing_model_catalog_json_key",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"openai\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
			},
		},
		{
			name: "catalog_file_absent",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"openai\"\nmodel_catalog_json = \"missing.json\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
			},
		},
		{
			name: "model_entry_missing_for_slug",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				// Overwrite with a catalog that lacks the test slug.
				catalog := `{"models":[{"slug":"other-slug","display_name":"Other","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`
				writeLocalCatalogRaw(t, req.Home, "catalog.local.json", []byte(catalog))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
				home, workDir := t.TempDir(), t.TempDir()
				req := catalogRequest(t, home, workDir, "low")
				test.configure(t, &req)
				plan, err := buildCodexPlanForMode(t, req, mode)
				if !errors.Is(err, test.want) {
					t.Fatalf("BuildPlan(%s) error = %v, want %v", mode, err, test.want)
				}
				var refusal *agentic.LocalProviderRefusal
				if !errors.As(err, &refusal) || refusal.Kind != test.kind {
					t.Fatalf("BuildPlan(%s) error = %v, want typed kind %q", mode, err, test.kind)
				}
				if !reflect.DeepEqual(plan, agentic.Plan{}) {
					t.Fatalf("refused %s launch returned a plan: %#v", mode, plan)
				}
				if strings.Contains(err.Error(), home) {
					t.Fatalf("refusal leaked a private path: %v", err)
				}
			}
		})
	}
}

// TestLocalProviderEmptyModelRefusesDirectly drives the two empty-model
// gates that BuildPlan never reaches (it refuses ErrModelMissing first):
// System.Argv for a direct plugin holder and ReadProviderSnapshot for a
// direct constructor caller. Both refuse typed unbound before any file read.
func TestLocalProviderEmptyModelRefusesDirectly(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := catalogRequest(t, home, workDir, "low")
	req.Model.ID = "   "
	if _, err := New().Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("Argv(empty model) = %v, want %v", err, agentic.ErrLocalProviderUnbound)
	}
	if _, err := ReadProviderSnapshot(req); !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("ReadProviderSnapshot(empty model) = %v, want %v", err, agentic.ErrLocalProviderUnbound)
	}
	// No file was read: deleting the config still refuses unbound, never absent.
	if err := os.Remove(filepath.Join(home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("Argv(empty model, config deleted) = %v, want %v (a read would refuse absent instead)", err, agentic.ErrLocalProviderUnbound)
	}
	if _, err := ReadProviderSnapshot(req); !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("ReadProviderSnapshot(empty model, config deleted) = %v, want %v (a read would refuse absent instead)", err, agentic.ErrLocalProviderUnbound)
	}
}

// TestLocalLaunchRefusesHostedShapedMetadata proves each hosted-shaped value
// refuses unsupported: a catalog row that states Lite, code_mode_only, v2,
// search or reasoning-summary is a protocol the local engine cannot run.
func TestLocalLaunchRefusesHostedShapedMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(entry map[string]any)
	}{
		{name: "lite_on", mutate: func(entry map[string]any) { entry["use_responses_lite"] = true }},
		{name: "code_mode_only", mutate: func(entry map[string]any) { entry["tool_mode"] = "code_mode_only" }},
		{name: "multi_agent_v2", mutate: func(entry map[string]any) { entry["multi_agent_version"] = "v2" }},
		{name: "search_on", mutate: func(entry map[string]any) { entry["supports_search_tool"] = true }},
		{name: "reasoning_summary_on", mutate: func(entry map[string]any) { entry["supports_reasoning_summary_parameter"] = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			entry := map[string]any{
				"slug": "Qwen3.8-27B-Q4_K_M", "display_name": "Test",
				"use_responses_lite": false, "tool_mode": "direct", "multi_agent_version": "v1",
				"supports_search_tool": false, "supports_reasoning_summary_parameter": false,
				"supported_reasoning_levels": []any{map[string]any{"effort": "low"}},
			}
			test.mutate(entry)
			data, _ := json.Marshal(map[string]any{"models": []any{entry}})
			writeLocalCatalogRaw(t, home, "catalog.local.json", data)
			req := catalogRequest(t, home, workDir, "low")
			_, err := buildCodexPlan(t, req)
			if !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
				t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderUnsupported)
			}
			var refusal *agentic.LocalProviderRefusal
			if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderUnsupported {
				t.Fatalf("BuildPlan error = %v, want typed unsupported", err)
			}
		})
	}
}

// TestLocalLaunchRefusesMalformedCatalog proves malformed catalog shapes
// refuse malformed rather than launching with guessed metadata.
func TestLocalLaunchRefusesMalformedCatalog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		catalog string
		config  string
	}{
		{name: "catalog_json_malformed", catalog: "{broken\n"},
		{name: "lite_missing", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`},
		{name: "search_missing", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`},
		{name: "summary_missing", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`},
		{name: "tool_mode_missing", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`},
		{name: "multi_agent_missing", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`},
		{name: "vocab_empty", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[]}]}`},
		{name: "vocab_blank_word", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"  "}]}]}`},
		{name: "vocab_duplicate", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"},{"effort":"low"}]}]}`},
		{name: "duplicate_slugs", catalog: `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]},{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			writeLocalCatalogRaw(t, home, "catalog.local.json", []byte(test.catalog))
			req := catalogRequest(t, home, workDir, "low")
			_, err := buildCodexPlan(t, req)
			if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
			}
		})
	}
	t.Run("catalog_path_blank", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfigBody(t, home, "model_provider = \"openai\"\nmodel_catalog_json = \"   \"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
		req := catalogRequest(t, home, workDir, "low")
		_, err := buildCodexPlan(t, req)
		if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
		}
	})
	t.Run("catalog_path_empty_string", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfigBody(t, home, "model_provider = \"openai\"\nmodel_catalog_json = \"\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
		req := catalogRequest(t, home, workDir, "low")
		_, err := buildCodexPlan(t, req)
		if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
		}
	})
	t.Run("catalog_path_untrimmed", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfigBody(t, home, "model_provider = \"openai\"\nmodel_catalog_json = \" catalog.local.json\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
		writeLocalCatalog(t, home, "Qwen3.8-27B-Q4_K_M", []string{"low"})
		req := catalogRequest(t, home, workDir, "low")
		_, err := buildCodexPlan(t, req)
		if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
		}
	})
	t.Run("catalog_path_not_a_string", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfigBody(t, home, "model_provider = \"openai\"\nmodel_catalog_json = 123\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
		req := catalogRequest(t, home, workDir, "low")
		_, err := buildCodexPlan(t, req)
		if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
		}
	})
	t.Run("catalog_path_tilde_without_home", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfigBody(t, home, "model_provider = \"openai\"\nmodel_catalog_json = \"~/catalog.local.json\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
		req := catalogRequest(t, home, workDir, "low")
		req.Env = filterEnvKeys(req.Env, "HOME")
		_, err := buildCodexPlan(t, req)
		if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderMalformed)
		}
	})
	t.Run("catalog_unreadable_is_not_absence", func(t *testing.T) {
		t.Parallel()
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		if err := os.Remove(filepath.Join(home, "catalog.local.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(home, "catalog.local.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		req := catalogRequest(t, home, workDir, "low")
		_, err := buildCodexPlan(t, req)
		if !errors.Is(err, agentic.ErrLocalProviderReadFailed) {
			t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderReadFailed)
		}
	})
}

// TestLocalEffortOutOfVocabularyRefuses is AC2's negative: max on a low-only
// row refuses unsupported before launch, on both the ID and snapshot paths
// and for both exec and dry-run. The positive (low on low-only) and the
// trimming/case rules ride along.
func TestLocalEffortOutOfVocabularyRefuses(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			base := catalogRequest(t, home, workDir, "low")
			snapshot := mustSnapshot(t, base)

			// Positive control: low on a low-only row succeeds on both paths.
			for _, binding := range []struct {
				name string
				req  agentic.LaunchRequest
			}{
				{name: "id_path", req: base},
				{name: "snapshot_path", req: withSnapshot(base, snapshot)},
			} {
				if _, err := buildCodexPlanForMode(t, binding.req, mode); err != nil {
					t.Fatalf("BuildPlan(%s %s, low on low-only): %v", binding.name, mode, err)
				}
			}

			// Whitespace trims to the vocabulary word.
			trimmed := base
			trimmed.Effort = "  low\t"
			if _, err := buildCodexPlanForMode(t, trimmed, mode); err != nil {
				t.Fatalf("BuildPlan(ID %s, trimmed low): %v", mode, err)
			}
			trimmedSnap := withSnapshot(base, snapshot)
			trimmedSnap.Effort = "  low\t"
			if _, err := buildCodexPlanForMode(t, trimmedSnap, mode); err != nil {
				t.Fatalf("BuildPlan(snapshot %s, trimmed low): %v", mode, err)
			}

			// Out-of-vocabulary refuses on both paths.
			for _, effort := range []string{"", "max", "Low", "LOW", "low-high"} {
				idReq := base
				idReq.Effort = effort
				if _, err := buildCodexPlanForMode(t, idReq, mode); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
					t.Fatalf("BuildPlan(ID %s, effort %q) = %v, want %v", mode, effort, err, agentic.ErrLocalProviderUnsupported)
				}
				snapReq := withSnapshot(base, snapshot)
				snapReq.Effort = effort
				if _, err := buildCodexPlanForMode(t, snapReq, mode); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
					t.Fatalf("BuildPlan(snapshot %s, effort %q) = %v, want %v", mode, effort, err, agentic.ErrLocalProviderUnsupported)
				}
			}
		})
	}
}

// TestHostedEffortBehaviourUnchanged proves AC2's second half: a hosted
// launch (no binding) carries any effort word verbatim via -c, with no
// catalog pin and no vocabulary check.
func TestHostedEffortBehaviourUnchanged(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	req := providerRequest(t, home, workDir, "local-story")
	req.LocalProvider = nil
	req.Effort = "max"
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatalf("BuildPlan(hosted, max): %v", err)
	}
	if !containsPair(plan.Argv, "-c", `model_reasoning_effort="max"`) {
		t.Fatalf("hosted argv %q does not carry max verbatim", plan.Argv)
	}
	if containsArgPrefix(plan.Argv, "model_catalog_json=") || containsArgPrefix(plan.Argv, "model_provider=") {
		t.Fatalf("hosted argv carries a local pin: %q", plan.Argv)
	}
}

// TestHostedArgvByteIdenticalGolden pins the ordinary subscription grammar:
// with effort, profile and tier, the argv is exactly the pre-change bytes
// and carries no catalog override.
func TestHostedArgvByteIdenticalGolden(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	req := providerRequest(t, home, workDir, "local-story")
	req.LocalProvider = nil
	req.Effort = "high"
	req.Profile = "work"
	req.ServiceTier = "priority"
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatalf("BuildPlan(hosted): %v", err)
	}
	want := []string{
		"--search", "-a", "never",
		"-p", "work",
		"exec", "-m", "Qwen3.8-27B-Q4_K_M",
		"-c", `model_reasoning_effort="high"`,
		"-c", `service_tier="priority"`,
		bypassApprovalsAndSandboxFlag, "--skip-git-repo-check", "-C", workDir, "-",
	}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("hosted argv = %q, want byte-identical %q", plan.Argv, want)
	}
}

// TestSanitizedAdditionalToolsFixtureProvesLocalConfigNeverSelectsLite is
// AC3's first regression: the sanitized capture HAS the Lite-shaped
// additional_tools input item (proving the fixture is the failing shape),
// while the local launch's effective catalog states ordinary Responses with
// direct tools — the Codex 0.159 ordinary branch (client.rs AdditionalTools
// only on the Lite branch), which emits top-level tools and no
// additional_tools item.
func TestSanitizedAdditionalToolsFixtureProvesLocalConfigNeverSelectsLite(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/additional-tools-request.json")
	if err != nil {
		t.Fatalf("read sanitized fixture: %v", err)
	}
	var captured map[string]any
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatalf("parse sanitized fixture: %v", err)
	}
	inputs, ok := captured["input"].([]any)
	if !ok || len(inputs) == 0 {
		t.Fatalf("fixture has no input items")
	}
	first, ok := inputs[0].(map[string]any)
	if !ok || first["type"] != "additional_tools" {
		t.Fatalf("fixture item zero is not the Lite additional_tools shape: %v", inputs[0])
	}
	if _, hasTools := captured["tools"]; hasTools {
		t.Fatalf("fixture carries top-level tools; the Lite shape omits them")
	}

	// The local launch's effective config for the same slug selects the
	// ordinary branch.
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := catalogRequest(t, home, workDir, "low")
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatalf("BuildPlan(local): %v", err)
	}
	var catalogPath string
	for i := 0; i+1 < len(plan.Argv); i++ {
		if plan.Argv[i] == "-c" && strings.HasPrefix(plan.Argv[i+1], "model_catalog_json=") {
			catalogPath = strings.Trim(strings.TrimPrefix(plan.Argv[i+1], "model_catalog_json="), `"`)
		}
	}
	if catalogPath == "" {
		t.Fatalf("local argv carries no catalog pin: %q", plan.Argv)
	}
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read effective catalog: %v", err)
	}
	var catalog codexCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("parse effective catalog: %v", err)
	}
	if len(catalog.Models) != 1 {
		t.Fatalf("effective catalog carries %d models", len(catalog.Models))
	}
	entry := catalog.Models[0]
	if entry.UseResponsesLite == nil || *entry.UseResponsesLite || entry.ToolMode != "direct" {
		t.Fatalf("effective catalog can select Lite: %+v", entry)
	}
}

// TestSnapshotPathPerformsZeroCatalogReads deletes the catalog after the
// snapshot: the snapshot plan still succeeds on the pinned path and
// vocabulary, while the ID-only control refuses absent, proving the snapshot
// branch read nothing.
func TestSnapshotPathPerformsZeroCatalogReads(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := catalogRequest(t, home, workDir, "low")
			snapshot := mustSnapshot(t, req)
			if snapshot.CatalogDigest() == "" || snapshot.CatalogPath() == "" || len(snapshot.EffortVocabulary()) == 0 {
				t.Fatalf("snapshot carries no catalog attestation: %#v", snapshot)
			}

			if err := os.Remove(filepath.Join(home, "catalog.local.json")); err != nil {
				t.Fatalf("remove catalog after snapshot: %v", err)
			}

			plan, err := buildCodexPlanForMode(t, withSnapshot(req, snapshot), mode)
			if err != nil {
				t.Fatalf("BuildPlan(snapshot, catalog deleted) = %v; the snapshot path must not read the catalog", err)
			}
			if !containsPair(plan.Argv, "-c", `model_catalog_json="`+effectiveCatalogPath(t, plan)+`"`) {
				t.Fatalf("snapshot argv after deletion does not carry the pinned catalog: %q", plan.Argv)
			}

			if _, err := buildCodexPlanForMode(t, req, mode); !errors.Is(err, agentic.ErrLocalProviderAbsent) {
				t.Fatalf("BuildPlan(ID path, catalog deleted) = %v, want %v (the control must prove the deletion took effect)", err, agentic.ErrLocalProviderAbsent)
			}
		})
	}
}

// TestSnapshotModelMismatchRefuses proves a snapshot validated for one slug
// cannot be replayed for another: the plan refuses conflicting without
// reading any file.
func TestSnapshotModelMismatchRefuses(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	base := catalogRequest(t, home, workDir, "low")
	snapshot := mustSnapshot(t, base)

	replayed := withSnapshot(base, snapshot)
	replayed.Model.ID = "gpt-6-luna"
	plan, err := buildCodexPlan(t, replayed)
	if !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderConflicting)
	}
	var refusal *agentic.LocalProviderRefusal
	if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderConflicting {
		t.Fatalf("BuildPlan error = %v, want typed conflicting", err)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("refused launch returned a plan: %#v", plan)
	}
}

// TestCatalogArgvSpellingIsFrozen pins the absolute bytes of the catalog -c
// spelling through both the shared spelling and the production entry point.
func TestCatalogArgvSpellingIsFrozen(t *testing.T) {
	t.Parallel()
	want := []string{"-c", `model_catalog_json="/tmp/example home/catalog.json"`}
	if got := catalogArgv("/tmp/example home/catalog.json"); !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogArgv = %#v, want %#v", got, want)
	}
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, workDir, "low"))
	if err != nil {
		t.Fatalf("BuildPlan(ID path): %v", err)
	}
	if !containsPair(plan.Argv, "-c", `model_catalog_json="`+effectiveCatalogPath(t, plan)+`"`) {
		t.Fatalf("ID-only argv %q misses the catalog pin", plan.Argv)
	}
}

// TestReadProviderSnapshotCatalogRefusalParity holds the constructor to the
// ID-only plan path's typed catalog refusals: every catalog failure that
// refuses a plan must refuse the snapshot with the same kind.
func TestReadProviderSnapshotCatalogRefusalParity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		kind      agentic.LocalProviderRefusalKind
		want      error
		configure func(t *testing.T, req *agentic.LaunchRequest)
	}{
		{
			name: "missing_model_catalog_json_key",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"openai\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
			},
		},
		{
			name: "catalog_file_absent",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, "model_provider = \"openai\"\nmodel_catalog_json = \"missing.json\"\n\n[model_providers.local-story]\nname = \"task local provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n")
			},
		},
		{
			name: "catalog_json_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				writeLocalCatalogRaw(t, req.Home, "catalog.local.json", []byte("{broken\n"))
			},
		},
		{
			name: "model_entry_missing",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				catalog := `{"models":[{"slug":"other","display_name":"Other","use_responses_lite":false,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`
				writeLocalCatalogRaw(t, req.Home, "catalog.local.json", []byte(catalog))
			},
		},
		{
			name: "lite_on",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				catalog := `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":true,"tool_mode":"direct","multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`
				writeLocalCatalogRaw(t, req.Home, "catalog.local.json", []byte(catalog))
			},
		},
		{
			name: "tool_mode_missing",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				catalog := `{"models":[{"slug":"Qwen3.8-27B-Q4_K_M","use_responses_lite":false,"multi_agent_version":"v1","supports_search_tool":false,"supports_reasoning_summary_parameter":false,"supported_reasoning_levels":[{"effort":"low"}]}]}`
				writeLocalCatalogRaw(t, req.Home, "catalog.local.json", []byte(catalog))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home, workDir := t.TempDir(), t.TempDir()
			req := catalogRequest(t, home, workDir, "low")
			test.configure(t, &req)
			snapshot, constructorErr := ReadProviderSnapshot(req)
			if snapshot != nil {
				t.Fatalf("ReadProviderSnapshot returned a snapshot on a refused config: %#v", snapshot)
			}
			if !errors.Is(constructorErr, test.want) {
				t.Fatalf("ReadProviderSnapshot error = %v, want %v", constructorErr, test.want)
			}
			var constructorRefusal *agentic.LocalProviderRefusal
			if !errors.As(constructorErr, &constructorRefusal) || constructorRefusal.Kind != test.kind {
				t.Fatalf("ReadProviderSnapshot error = %v, want typed kind %q", constructorErr, test.kind)
			}
			_, planErr := buildCodexPlan(t, req)
			if !errors.Is(planErr, test.want) {
				t.Fatalf("BuildPlan error = %v, want %v (constructor/plan parity)", planErr, test.want)
			}
			var planRefusal *agentic.LocalProviderRefusal
			if !errors.As(planErr, &planRefusal) || planRefusal.Kind != test.kind {
				t.Fatalf("BuildPlan error = %v, want typed kind %q (constructor/plan parity)", planErr, test.kind)
			}
		})
	}
}

func effectiveCatalogPath(t *testing.T, plan agentic.Plan) string {
	t.Helper()
	for _, arg := range plan.Argv {
		if strings.HasPrefix(arg, "model_catalog_json=") {
			path, err := strconv.Unquote(strings.TrimPrefix(arg, "model_catalog_json="))
			if err != nil {
				t.Fatal(err)
			}
			return path
		}
	}
	t.Fatal("plan has no native catalog pin")
	return ""
}
