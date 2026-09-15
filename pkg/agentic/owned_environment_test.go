package agentic

import (
	"errors"
	"reflect"
	"testing"
)

type ownedEnvironmentSystem struct {
	*pangolinSystem
	snapshot []string
	fail     bool
}

func (s *ownedEnvironmentSystem) PrepareLaunchRequest(req LaunchRequest, mode LaunchMode) (LaunchRequestPreparation, error) {
	return LaunchRequestPreparation{Prompt: []byte("prepared")}, nil
}
func (s *ownedEnvironmentSystem) ChildEnv(parent []string, req LaunchRequest) ([]string, error) {
	if parent != nil {
		return s.pangolinSystem.ChildEnv(parent, req)
	}
	if s.fail {
		return nil, errors.New("owned snapshot refused")
	}
	s.snapshot = []string{"Z_PROMPT=" + string(req.Prompt), "A_MODEL=" + req.Model.ID, "B_ALIAS=" + req.Model.AliasOf}
	return s.snapshot, nil
}
func TestBuildPlanWithEnvironmentPreparedAlias(t *testing.T) {
	s := &ownedEnvironmentSystem{pangolinSystem: newPangolin()}
	r := NewRegistry()
	if err := r.Register(s); err != nil {
		t.Fatal(err)
	}
	req := pangolinRequest()
	req.Model.AliasOf = "launch-identity"
	got, err := BuildPlanWithEnvironment(r, req, LaunchModeExec)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"A_MODEL=launch-identity", "B_ALIAS=", "Z_PROMPT=prepared"}
	if !reflect.DeepEqual(got.OwnedEnv, want) {
		t.Fatalf("snapshot = %v, want %v", got.OwnedEnv, want)
	}
	if s.snapshot[0] != "Z_PROMPT=prepared" {
		t.Fatal("sorting mutated plugin storage")
	}
	s.snapshot[0] = "mutated"
	if !reflect.DeepEqual(got.OwnedEnv, want) {
		t.Fatal("snapshot aliases plugin storage")
	}
	s.fail = true
	failed, err := BuildPlanWithEnvironment(r, req, LaunchModeExec)
	if err == nil || !reflect.DeepEqual(failed, PlanWithEnvironment{}) {
		t.Fatalf("snapshot failure admitted: %+v, %v", failed, err)
	}
	if _, err := BuildPlan(r, req, LaunchModeExec); err != nil {
		t.Fatalf("legacy unexpectedly requests owned env: %v", err)
	}
}
