package codex

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var carriedReportTamperCases = []string{"name", "append-name", "note", "delete", "inject"}

func carriedReportPlan(t *testing.T, mode agentic.LaunchMode, binding, change string) agentic.Plan {
	t.Helper()
	home, work := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1/v1", "responses", false)
	writeLocalCatalog(t, home, "Qwen3.8-27B-Q4_K_M", []string{"low", "high"})
	req := catalogRequest(t, home, work, "high")
	req.ServiceTier = "priority"
	if strings.HasPrefix(binding, "managed-") {
		manager := t.TempDir()
		req.ManagerToolDir = &manager
	}
	if strings.HasSuffix(binding, "snapshot") {
		req = withSnapshot(req, mustSnapshot(t, req))
	}
	if change != "inject" {
		req.LocalProvider.Settings = []agentic.LocalProviderSetting{
			{Key: "model", Value: `"discarded-model-seal-test"`},
			{Key: "model_reasoning_effort", Value: `"low"`},
			{Key: "service_tier", Value: `"default"`},
		}
	}
	plan, err := buildCodexPlanForMode(t, req, mode)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// Finalization cannot adopt an already-tampered report. Finalized and actual
// JSON-round-tripped guards must retain the same integrity check after overlays
// and import, including a report that was absent in the original plan.
func TestCarriedSettingsReportSurvivesSealLifecycle(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		for _, binding := range []string{"id", "snapshot", "managed-id", "managed-snapshot"} {
			for _, phase := range []string{"before-finalize", "finalized", "imported", "imported-finalized", "imported-process"} {
				for _, change := range carriedReportTamperCases {
					t.Run(mode.String()+"/"+binding+"/"+phase+"/"+change, func(t *testing.T) {
						plan := carriedReportPlan(t, mode, binding, change)
						if err := plan.VerifyBeforeExec(); err != nil {
							t.Fatalf("untampered base: %v", err)
						}
						if phase == "before-finalize" {
							if _, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, nil); err != nil {
								t.Fatalf("untampered finalization: %v", err)
							}
							tamperCarriedReport(&plan, change)
							refused, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, nil)
							if !errors.Is(err, agentic.ErrFinalizedProcessChanged) || !reflect.DeepEqual(refused, agentic.Plan{}) {
								t.Fatalf("finalizer adopted a changed report: err=%v, partial=%t", err, !reflect.DeepEqual(refused, agentic.Plan{}))
							}
							return
						}
						if phase == "finalized" || phase == "imported-finalized" {
							var err error
							overlays := agentic.FinalizeOverlays{NativeTail: []string{"--report-test-tail"}}
							if strings.HasPrefix(binding, "managed-") {
								// Managed tails are already fixed before planning; a
								// permitted fragment exercises its finalization path.
								overlays = agentic.FinalizeOverlays{FragmentEnv: []string{"REPORT_TEST_FINAL=1"}}
							}
							plan, err = agentic.FinalizePlan(plan, overlays, nil)
							if err != nil {
								t.Fatal(err)
							}
						}
						verify := func() error { return plan.VerifyBeforeExec() }
						if phase == "imported" || phase == "imported-finalized" || phase == "imported-process" {
							guard, err := plan.ExportSeal()
							if err != nil {
								t.Fatal(err)
							}
							wire, err := json.Marshal(guard)
							if err != nil {
								t.Fatal(err)
							}
							var decoded agentic.Seal
							if err := json.Unmarshal(wire, &decoded); err != nil {
								t.Fatal(err)
							}
							imported, err := agentic.ImportSeal(New(), decoded)
							if err != nil {
								t.Fatal(err)
							}
							verify = func() error { return imported.VerifyBeforeExec(plan) }
							if phase == "imported-process" {
								process, err := agentic.NewImportedProcess(New(), decoded, plan)
								if err != nil {
									t.Fatal(err)
								}
								verify = func() error { return process.VerifyBeforeExec(plan) }
							}
						}
						if err := verify(); err != nil {
							t.Fatalf("untampered %s: %v", phase, err)
						}
						tamperCarriedReport(&plan, change)
						if err := verify(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
							t.Fatalf("%s accepted report tamper, got %v", phase, err)
						}
					})
				}
			}
		}
	}
}

// No-drop catalog guards keep their prior selector shape. A present report
// binds only a digest, and an invalid imported digest refuses typed.
func TestCarriedSettingsSealOmissionAndMalformedBinding(t *testing.T) {
	for _, change := range []string{"inject", "note"} {
		plan := carriedReportPlan(t, agentic.LaunchModeExec, "id", change)
		guard, err := plan.ExportSeal()
		if err != nil {
			t.Fatal(err)
		}
		digest, present := guard.Data.Sealed.Selectors[carriedSettingsSealSelector]
		if change == "inject" {
			if present || len(guard.Data.Sealed.Selectors) != 0 {
				t.Fatal("absent report changed guard selectors")
			}
			continue
		}
		if _, valid := parseSealedDigest(digest); !present || !valid {
			t.Fatal("present report has no digest binding")
		}
		imported, err := agentic.ImportSeal(New(), guard)
		if err != nil {
			t.Fatal(err)
		}
		if err := imported.VerifyBeforeExec(plan); err != nil {
			t.Fatalf("untampered imported control: %v", err)
		}
		guard.Data.Sealed.Selectors[carriedSettingsSealSelector] = "invalid-report-digest"
		if _, err := New().ImportSealedData(*guard.Data.Sealed); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("malformed binding must refuse typed, got %v", err)
		}
	}
}

func tamperCarriedReport(plan *agentic.Plan, change string) {
	switch change {
	case "name":
		plan.CarriedSettings.DroppedKeys[0] = "changed"
	case "append-name":
		plan.CarriedSettings.DroppedKeys = append(plan.CarriedSettings.DroppedKeys, "changed")
	case "note":
		plan.CarriedSettings.Note = "changed"
	case "delete":
		plan.CarriedSettings = nil
	case "inject":
		plan.CarriedSettings = &agentic.LocalProviderSettingsStatus{DroppedKeys: []string{"model"}, Note: "changed"}
	}
}

// Untouched local plans verify, but editing names/note, removing a report, or
// injecting one into a no-drop plan must refuse typed in both modes/bindings.
// In-place name edits also prove the sealed slice does not alias the report.
func TestCarriedSettingsSealRejectsReportTampering(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		for _, binding := range []string{"id", "snapshot", "managed-id", "managed-snapshot"} {
			for _, change := range carriedReportTamperCases {
				t.Run(mode.String()+"/"+binding+"/"+change, func(t *testing.T) {
					plan := carriedReportPlan(t, mode, binding, change)
					if err := plan.VerifyBeforeExec(); err != nil {
						t.Fatalf("untampered control: %v", err)
					}
					tamperCarriedReport(&plan, change)
					if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
						t.Fatalf("report tamper must refuse conflicting, got %v", err)
					}
				})
			}
		}
	}
}
