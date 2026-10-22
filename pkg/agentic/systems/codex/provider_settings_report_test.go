package codex

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Both local binding paths and launch modes report unique dropped names while
// excluding the discarded values from the entire exported plan and its note.
func TestCarriedSettingsReportNamesWithoutValues(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		for _, binding := range []string{"id", "snapshot"} {
			t.Run(mode.String()+"/"+binding, func(t *testing.T) {
				home, work := t.TempDir(), t.TempDir()
				writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
				writeLocalCatalog(t, home, "Qwen3.8-27B-Q4_K_M", []string{"low", "high"})
				req := catalogRequest(t, home, work, "high")
				req.ServiceTier = "priority"
				if binding == "snapshot" {
					req = withSnapshot(req, mustSnapshot(t, req))
				}
				req.LocalProvider.Settings = []agentic.LocalProviderSetting{
					{Key: "service_tier", Value: `"operator-secret-tier-G23"`},
					{Key: "model", Value: `"operator-secret-model-G23"`},
					{Key: "model_reasoning_effort", Value: `"operator-secret-effort-G23"`},
					{Key: "model_reasoning_effort", Value: `"low"`},
					{Key: "model", Value: `"operator-secret-model-G23"`},
				}
				before := append([]agentic.LocalProviderSetting(nil), req.LocalProvider.Settings...)
				plan, err := buildCodexPlanForMode(t, req, mode)
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(plan)
				if err != nil {
					t.Fatal(err)
				}
				var exposed struct {
					CarriedSettings struct {
						DroppedKeys []string `json:"dropped_keys"`
						Note        string   `json:"note"`
					} `json:"carried_settings"`
				}
				if err := json.Unmarshal(data, &exposed); err != nil {
					t.Fatal(err)
				}
				want := []string{"model", "model_reasoning_effort", "service_tier"}
				if !reflect.DeepEqual(exposed.CarriedSettings.DroppedKeys, want) {
					t.Fatalf("dropped names = %q, want %q", exposed.CarriedSettings.DroppedKeys, want)
				}
				for _, name := range want {
					if !strings.Contains(exposed.CarriedSettings.Note, name) {
						t.Errorf("note does not name dropped key %q", name)
					}
				}
				for _, value := range []string{"operator-secret-model-G23", "operator-secret-effort-G23", "operator-secret-tier-G23", "low"} {
					if strings.Contains(string(data), value) || strings.Contains(exposed.CarriedSettings.Note, value) {
						t.Errorf("discarded value leaked into exported plan or note")
					}
				}
				if !reflect.DeepEqual(before, req.LocalProvider.Settings) {
					t.Fatal("reporting mutated caller settings")
				}
				if err := plan.VerifyBeforeExec(); err != nil {
					t.Fatalf("reported plan did not seal: %v", err)
				}
			})
		}
	}
}
