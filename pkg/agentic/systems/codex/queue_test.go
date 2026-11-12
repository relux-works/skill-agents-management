package codex

import (
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Codex 0.167.0 `queue --help` names --thread/--message and says queued
// messages use the thread's model. No live thread or message is used here.
func TestQueueInheritsThreadSelectors(t *testing.T) {
	for _, permission := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(permission), func(t *testing.T) {
			for _, native := range [][]string{
				{"queue", "--thread", "fixture-thread", "--message", "fixture message"},
				{"--strict-config", "-C", "/fixture", "queue", "--thread=fixture-thread", "--message=fixture message", "--resume"},
			} {
				req := interactiveRequest("/fixture")
				req.ToolRelease = "0.167.0"
				req.PermissionMode = permission
				req.NativeArgs = append([]string(nil), native...)
				got, err := Args(req, agentic.LaunchModeInteractive)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, native) {
					t.Fatalf("queue argv = %q; want unchanged native argv %q without launch selectors", got, native)
				}
				got[0] = "changed"
				if !reflect.DeepEqual(req.NativeArgs, native) {
					t.Fatal("queue argv aliases caller arguments")
				}
			}
		})
	}
}

// Only the root command is queue: option values, resumed-thread arguments and
// text after -- must retain ordinary launch model/effort/permission selection.
func TestQueueTextDoesNotSuppressLaunchSelectors(t *testing.T) {
	for _, native := range [][]string{
		nil,
		{"--", "queue"},
		{"resume", "queue"},
		{"-C", "queue"},
		{"--config", "queue", "resume"},
		{"--config=queue", "resume"},
		{"--unknown-option", "queue"},
	} {
		req := interactiveRequest("/fixture")
		req.ToolRelease = "0.167.0"
		req.PermissionMode = agentic.PermissionModeYolo
		req.NativeArgs = native
		// The unknown config value is not a valid yolo policy override;
		// native mode still must not mistake its value for the command.
		if len(native) > 0 && (native[0] == "--config" || native[0] == "--config=queue") {
			req.PermissionMode = agentic.PermissionModeNative
		}
		got, err := Args(req, agentic.LaunchModeInteractive)
		if err != nil {
			t.Fatalf("%q: %v", native, err)
		}
		want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}
		if req.PermissionMode == agentic.PermissionModeYolo {
			want = append(want, bypassApprovalsAndSandboxFlag)
		}
		want = append(want, native...)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: argv = %q; want %q", native, got, want)
		}
	}
}

// BuildPlan keeps the selected model as request metadata while the queue argv
// inherits the thread's settings. Native caller selectors are never removed.
func TestQueueThroughBuildPlan(t *testing.T) {
	req := interactiveRequest(tempSlot(t))
	req.ToolRelease = "0.167.0"
	req.PermissionMode = agentic.PermissionModeYolo
	req.NativeArgs = []string{"queue", "--thread", "fixture-thread", "--message", "fixture message"}
	plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
	if !reflect.DeepEqual(plan.Argv, req.NativeArgs) || plan.ModelIdentity.Requested != req.Model.ID {
		t.Fatalf("queue plan argv/model = %q/%q", plan.Argv, plan.ModelIdentity.Requested)
	}
	req.PermissionMode = agentic.PermissionModeNative
	req.NativeArgs = append([]string{"--model", "caller-model"}, req.NativeArgs...)
	got, err := Args(req, agentic.LaunchModeInteractive)
	if err != nil || !reflect.DeepEqual(got, req.NativeArgs) {
		t.Fatalf("caller suffix must remain verbatim for native Codex validation: %q, %v", got, err)
	}
}

// Queue does not bypass existing permission validation or release qualification;
// these refusals are checked alongside valid queue requests above.
func TestQueueRetainsPermissionRefusals(t *testing.T) {
	for _, tc := range []struct {
		name       string
		permission agentic.PermissionMode
		release    string
		want       error
	}{
		{"unknown-mode", agentic.PermissionMode("invalid"), "0.167.0", agentic.ErrPermissionModeUnknown},
		{"unqualified-yolo", agentic.PermissionModeYolo, "0.999.0", agentic.ErrPermissionModeUnverifiedRelease},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := interactiveRequest("/fixture")
			req.PermissionMode, req.ToolRelease = tc.permission, tc.release
			req.NativeArgs = []string{"queue", "--thread", "fixture-thread", "--message", "fixture message"}
			if _, err := Args(req, agentic.LaunchModeInteractive); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
