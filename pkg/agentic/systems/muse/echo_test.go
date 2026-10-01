package muse

import (
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// These cases drive the production BuildPlan -> System.Argv -> Args path.
// Non-empty effort attacks the Meta-only flag guard, rather than relying on
// an effortless request that the previous builder already handled.
func TestMuseBuildPlanEchoInteractiveOmitsMetaFlags(t *testing.T) {
	for _, posture := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		for _, identity := range []string{"echo", " echo "} {
			t.Run(string(posture)+"/"+identity, func(t *testing.T) {
				req := launchRequest(t, "")
				req.Model.ID = identity
				req.Effort = "high"
				req.PermissionMode = posture
				req.ToolRelease = verifiedMuseRelease
				req.NativeArgs = []string{"--", "inspect this workspace"}
				plan := buildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
				want := []string{"--provider", "echo", "--workspace", req.WorkDir}
				if posture == agentic.PermissionModeYolo {
					want = append(want, "--yolo")
				}
				want = append(want, req.NativeArgs...)
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("BuildPlan echo argv = %#v, want %#v", plan.Argv, want)
				}
			})
		}
	}
}

func TestMuseBuildPlanEchoExecAndDryRunOmitMetaFlags(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			req := launchRequest(t, "offline assignment")
			req.Model.ID = "echo"
			req.Effort = "high"
			prompt := req.PromptPath
			if mode == agentic.LaunchModeDryRun {
				req.PromptPath = ""
				prompt = "<prompt-file>"
			}
			plan := buildMusePlan(t, New(), req, mode)
			want := []string{"exec", "--json", "--yolo", "--provider", "echo", "--workspace", req.WorkDir, "--prompt-file", prompt}
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("BuildPlan echo argv = %#v, want %#v", plan.Argv, want)
			}
		})
	}
}

// Only the exact pseudo-model selects echo. Nearby names and a declared alias
// resolving to a Meta model remain Meta; provider selection follows the model
// identity BuildPlan actually dispatches, not an inferred provider label.
func TestMuseBuildPlanEchoSelectionDoesNotCaptureMetaIdentities(t *testing.T) {
	for _, model := range []agentic.Model{
		{ID: "echo-model"},
		{ID: "Echo"},
		{ID: "echo", AliasOf: parityModel},
	} {
		t.Run(model.ID+"/"+model.AliasOf, func(t *testing.T) {
			req := launchRequest(t, "")
			req.Model = model
			req.Effort = "high"
			plan := buildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
			want := []string{"--model", model.LaunchIdentity(), "--reasoning-effort", "high", "--workspace", req.WorkDir}
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("BuildPlan Meta argv = %#v, want %#v", plan.Argv, want)
			}
		})
	}
}
