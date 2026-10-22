package agentic

import (
	"errors"
	"reflect"
	"testing"
)

type reportingPangolin struct {
	*pangolinSystem
	status  LocalProviderSettingsStatus
	reports int
}

func (s *reportingPangolin) LocalProviderSettingsStatus(LaunchRequest, LaunchMode) LocalProviderSettingsStatus {
	s.reports++
	return s.status
}

// The optional hook copies names into a successful plan; later plugin changes
// cannot change that report. Note-only and empty reports remain absent.
func TestLocalProviderSettingsReportCopiesNamesAndOmitsEmpty(t *testing.T) {
	for _, status := range []LocalProviderSettingsStatus{
		{DroppedKeys: []string{"model"}, Note: "Dropped model"},
		{},
		{Note: "no dropped keys"},
	} {
		sys := &reportingPangolin{pangolinSystem: newPangolin(), status: status}
		registry := NewRegistry()
		if err := registry.Register(sys); err != nil {
			t.Fatal(err)
		}
		plan, err := BuildPlan(registry, pangolinRequest(), LaunchModeExec)
		if err != nil {
			t.Fatal(err)
		}
		if sys.reports != 1 {
			t.Fatalf("report calls = %d, want 1", sys.reports)
		}
		if len(status.DroppedKeys) == 0 {
			if plan.CarriedSettings != nil {
				t.Fatal("empty names must omit carried settings")
			}
			continue
		}
		if plan.CarriedSettings == nil || !reflect.DeepEqual(*plan.CarriedSettings, status) {
			t.Fatalf("status = %+v, want %+v", plan.CarriedSettings, status)
		}
		sys.status.DroppedKeys[0] = "changed"
		if plan.CarriedSettings.DroppedKeys[0] != "model" {
			t.Fatal("plan report aliases the plugin's names")
		}
	}
}

// A failed plan never invokes the informational hook or returns a partial plan.
func TestLocalProviderSettingsReporterDoesNotRunOnRefusal(t *testing.T) {
	sys := &reportingPangolin{pangolinSystem: newPangolin()}
	registry := NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatal(err)
	}
	req := pangolinRequest()
	req.Model.ID = ""
	plan, err := BuildPlan(registry, req, LaunchModeExec)
	if !errors.Is(err, ErrModelMissing) || !reflect.DeepEqual(plan, Plan{}) || sys.reports != 0 {
		t.Fatalf("refused plan = %+v, err = %v, report calls = %d", plan, err, sys.reports)
	}
}
