package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

type fakeCLIStatusReader struct {
	status localruntime.Status
	err    error
}

func (f fakeCLIStatusReader) Status(context.Context, localruntime.StatusQuery) (localruntime.Status, error) {
	if f.err != nil {
		return localruntime.Status{}, f.err
	}
	return f.status, nil
}

// TestBuildLocalRuntimeReportAbsentAndMalformedAreDistinct is §6 slice 7's
// exact contract: absent and malformed local-models.toml must never be
// collapsed into one shape.
func TestBuildLocalRuntimeReportAbsentAndMalformedAreDistinct(t *testing.T) {
	absent := buildLocalRuntimeReport(context.Background(), localmodels.ConfigResult{Absent: true}, fakeCLIStatusReader{})
	if absent.Registered || absent.Reason != "absent" || absent.Error != "" {
		t.Fatalf("absent report = %+v, want {registered:false reason:absent}", absent)
	}

	malformedErr := errors.New("pointer.agents_infra_project is required")
	malformed := buildLocalRuntimeReport(context.Background(), localmodels.ConfigResult{Err: malformedErr}, fakeCLIStatusReader{})
	if malformed.Registered || malformed.Reason != "malformed" || malformed.Error != malformedErr.Error() {
		t.Fatalf("malformed report = %+v, want {registered:false reason:malformed error:%q}", malformed, malformedErr.Error())
	}

	if absent.Reason == malformed.Reason {
		t.Fatal("absent and malformed report the same reason")
	}
}

func validLocalRuntimeConfig() localmodels.Config {
	return localmodels.Config{Runtimes: []localmodels.RuntimeEntry{
		{
			ID:     "local-qwen",
			System: "pi",
			Models: map[vendorplugin.ModelID]localmodels.ModelEntry{
				"qwen-3.8-27b-mlx-8bit": {
					Pointer: localmodels.Pointer{
						AgentsInfraProject: "/Users/op/skill-agents-management",
						AgentsInfraProfile: "local-qwen",
					},
				},
			},
		},
	}}
}

// TestBuildLocalRuntimeReportValidConfigReportsLiveStatus is the third,
// registered shape: each declared pair's live status, read through the
// injected StatusReader.
func TestBuildLocalRuntimeReportValidConfigReportsLiveStatus(t *testing.T) {
	reader := fakeCLIStatusReader{status: localruntime.Status{BrokerState: "absent", BrokerSource: localruntime.SourceDetermined, MaxLeases: 1}}
	report := buildLocalRuntimeReport(context.Background(), localmodels.ConfigResult{Config: validLocalRuntimeConfig()}, reader)
	if !report.Registered || report.Reason != "" || report.Error != "" {
		t.Fatalf("report = %+v, want registered:true with no reason/error", report)
	}
	if len(report.Runtimes) != 1 {
		t.Fatalf("Runtimes = %+v, want exactly one row", report.Runtimes)
	}
	row := report.Runtimes[0]
	if row.Runtime != "local-qwen" || row.Model != "qwen-3.8-27b-mlx-8bit" {
		t.Fatalf("row = %+v, want local-qwen/qwen-3.8-27b-mlx-8bit", row)
	}
	if row.BrokerState != "absent" || row.BrokerSource != string(localruntime.SourceDetermined) {
		t.Fatalf("row = %+v, want the fake reader's broker state/source", row)
	}
}

// TestBuildLocalRuntimeReportStatusReadErrorIsPerRow proves one pair's
// failed status read never suppresses the whole report — it becomes that
// row's own Error field.
func TestBuildLocalRuntimeReportStatusReadErrorIsPerRow(t *testing.T) {
	reader := fakeCLIStatusReader{err: errors.New("agents-infra: not found")}
	report := buildLocalRuntimeReport(context.Background(), localmodels.ConfigResult{Config: validLocalRuntimeConfig()}, reader)
	if !report.Registered {
		t.Fatalf("report = %+v, want registered:true even when a status read fails", report)
	}
	if len(report.Runtimes) != 1 || report.Runtimes[0].Error == "" {
		t.Fatalf("Runtimes = %+v, want the read failure surfaced on the row", report.Runtimes)
	}
}

// TestLocalRuntimeStatusCommandJSONShapeIsValid is a thin CLI-wiring smoke:
// the real command, run end to end through cobra, produces valid JSON in
// one of the two documented "not registered" shapes (the test environment
// has no local-models.toml).
func TestLocalRuntimeStatusCommandJSONShapeIsValid(t *testing.T) {
	stdout, _, err := runRoot(t, "local-runtime", "status", "--json")
	if err != nil {
		t.Fatalf("local-runtime status --json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if _, ok := decoded["registered"]; !ok {
		t.Fatalf("decoded = %v, missing the required \"registered\" field", decoded)
	}
}
