package muse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestMuseHeadlessExecRefusesMissingBypass(t *testing.T) {
	testMuseMissingBypass(t, agentic.LaunchModeExec)
}

func TestMuseHeadlessDryRunRefusesMissingBypass(t *testing.T) {
	testMuseMissingBypass(t, agentic.LaunchModeDryRun)
}

func testMuseMissingBypass(t *testing.T, mode agentic.LaunchMode) {
	t.Helper()
	for _, suffix := range [][]string{nil, {"--", museYoloFlag}, {museYoloFlag + "=false"}} {
		t.Run(fmt.Sprint(suffix), func(t *testing.T) {
			req := launchRequest(t, "assignment")
			system := missingHeadlessBypass{System: New(), suffix: suffix}
			if err := checkMuseHeadlessRefusal(t, system, req, mode); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// This assertion drives the real registry/BuildPlan/SealExecPlan path for both
// the negative test and its seam mutant; no source text is rewritten.
func checkMuseHeadlessRefusal(t *testing.T, system agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	t.Helper()
	plan, err := tryBuildMusePlan(t, system, req, mode)
	if !errors.Is(err, ErrMuseHeadlessApproval) || !strings.Contains(err.Error(), "approval would block a headless child") {
		return fmt.Errorf("missing typed headless approval refusal: %v", err)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) {
		return errors.New("refused headless launch returned a plan")
	}
	return nil
}

func TestMuseHeadlessExecBypassPlanByteIdentical(t *testing.T) {
	testMuseUnchangedPlan(t, agentic.LaunchModeExec)
}

func TestMuseHeadlessDryRunBypassPlanByteIdentical(t *testing.T) {
	testMuseUnchangedPlan(t, agentic.LaunchModeDryRun)
}

func TestMuseInteractiveBypassGatePlanByteIdentical(t *testing.T) {
	testMuseUnchangedPlan(t, agentic.LaunchModeInteractive)
}

func testMuseUnchangedPlan(t *testing.T, mode agentic.LaunchMode) {
	t.Helper()
	postures := []agentic.PermissionMode{""}
	if mode == agentic.LaunchModeInteractive {
		postures = []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo}
	}
	for _, posture := range postures {
		t.Run(string(posture), func(t *testing.T) {
			req := launchRequest(t, "")
			req.PermissionMode = posture
			req.ToolRelease = verifiedMuseRelease
			before := buildMusePlan(t, musePriorPlanSealer{System: New()}, req, mode)
			after := buildMusePlan(t, New(), req, mode)
			beforeBytes, err := json.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			afterBytes, err := json.Marshal(after)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeBytes, afterBytes) || !reflect.DeepEqual(before, after) {
				t.Fatalf("%s plan changed at bypass gate: before=%s after=%s", mode, beforeBytes, afterBytes)
			}
		})
	}
}

func TestMuseHeadlessExecBypassNarrowingMutantKilled(t *testing.T) {
	req := launchRequest(t, "assignment")
	base := missingHeadlessBypass{System: New()}
	if err := checkMuseHeadlessRefusal(t, base, req, agentic.LaunchModeExec); err != nil {
		t.Fatalf("unmutated negative failed: %v", err)
	}
	mutant := museExecBypassCheckDropped{missingHeadlessBypass: base}
	if err := checkMuseHeadlessRefusal(t, mutant, req, agentic.LaunchModeExec); err == nil {
		t.Fatal("exec bypass check dropped: narrowing mutant survived TestMuseHeadlessExecRefusesMissingBypass assertion")
	} else if !strings.Contains(err.Error(), "missing typed headless approval refusal: <nil>") {
		t.Fatalf("mutant failed for an unrelated reason: %v", err)
	}
	// The mutant admits only unmanaged exec: the dry-run gate still refuses.
	if err := checkMuseHeadlessRefusal(t, mutant, req, agentic.LaunchModeDryRun); err != nil {
		t.Fatalf("mutant widened beyond its exec bound: %v", err)
	}
}

// Remove the generated posture through the existing Argv interface, retaining
// every other production surface and the real plan sealer.
type missingHeadlessBypass struct {
	*System
	suffix []string
}

func (s missingHeadlessBypass) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := s.System.Argv(req, mode)
	if err != nil || mode == agentic.LaunchModeInteractive {
		return args, err
	}
	args = slices.DeleteFunc(args, func(arg string) bool { return arg == museYoloFlag })
	return append(args, s.suffix...), nil
}

type museExecBypassCheckDropped struct{ missingHeadlessBypass }

func (s museExecBypassCheckDropped) SealExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
	if _, managed := plan.NetworkProvenanceSnapshot(); !managed && plan.Mode == agentic.LaunchModeExec {
		return nil, nil
	}
	return s.System.SealExecPlan(plan)
}

// The previous unmanaged sealer is the baseline; interactive seals are still
// built by the production implementation, including their unexported state.
type musePriorPlanSealer struct{ *System }

func (s musePriorPlanSealer) SealExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
	if _, managed := plan.NetworkProvenanceSnapshot(); !managed {
		return sealInteractiveExecPlan(plan)
	}
	return s.System.SealExecPlan(plan)
}
