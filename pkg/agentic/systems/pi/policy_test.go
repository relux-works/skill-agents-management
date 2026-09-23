package pi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the pi half of the versioned provider-capability table
// (curator-spec Decision 0018 choice 6): drift fails closed before
// support is even asked, the verified release stays unsupported, and
// native forwards verbatim. Pi has no native-policy scan — yolo never
// reaches classification — and no release probe: its binary is the
// agents-infra wrapper, and the wrapper's version is not pi's release.

// The verified release stays unsupported: yolo at pi 0.84.2 is refused
// with the unchanged sentinel even carrying native arguments no
// grammar knows — the refusal precedes classification, so the scan is
// never reached here.
func TestYoloAtTheVerifiedReleaseStaysUnsupported(t *testing.T) {
	req, _ := interactiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "0.84.2"
	req.NativeArgs = []string{"--permission-mode", "ultrastrict", "-c", "future_key=x"}
	if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnsupported", err)
	}
	if _, err := New(&fakeStatusReader{}).Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("Argv err = %v, want ErrPermissionModeUnsupported", err)
	}
}

// Drift fails closed first: yolo at any release but the verified one —
// a newer release, an invented future one, or none at all — is refused
// as unverified, through BuildPlan and through the plugin held
// directly.
func TestYoloRefusesDriftWithANamedDiagnostic(t *testing.T) {
	for _, release := range []string{"0.85.0", "9.9.9", ""} {
		t.Run("release "+quoteRelease(release), func(t *testing.T) {
			req, _ := interactiveRequest(t)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			err := mustPlanError(t, req)
			if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
			if _, err := New(&fakeStatusReader{}).Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("Argv err = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
		})
	}
	t.Run("the diagnostic names the grammar token the launcher cites", func(t *testing.T) {
		req, _ := interactiveRequest(t)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "0.85.0"
		err := mustPlanError(t, req)
		if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		for _, want := range []string{string(agentic.PermissionGrammarV1), "0.85.0", "0.84.2"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q does not name %q", err, want)
			}
		}
	})
}

func quoteRelease(release string) string {
	if release == "" {
		return "unestablished"
	}
	return release
}

func mustPlanError(t *testing.T, req agentic.LaunchRequest) error {
	t.Helper()
	_, err := buildPlan(t, req, agentic.LaunchModeInteractive)
	if err == nil {
		t.Fatal("BuildPlan built a plan that must be refused")
	}
	return err
}

// Native forwards the caller's arguments with no inspection at any
// release — pinned, newer, or never established — because the raw
// contract is unchanged and no claim is made (Decision 0018 item 4).
func TestNativeForwardsVerbatimAtAnyRelease(t *testing.T) {
	native := []string{"--approve", "resume the session"}
	for _, c := range []struct {
		name    string
		mode    agentic.PermissionMode
		release string
	}{
		{"zero value at the pinned release", "", "0.84.2"},
		{"explicit native at the pinned release", agentic.PermissionModeNative, "0.84.2"},
		{"explicit native past the pin", agentic.PermissionModeNative, "0.85.0"},
		{"explicit native with no release established", agentic.PermissionModeNative, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			req, _ := interactiveRequest(t)
			req.PermissionMode = c.mode
			req.ToolRelease = c.release
			req.NativeArgs = native
			plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			want := append([]string{"pi", "--model", "qwen-3.8-27b-mlx-8bit"}, native...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
			}
		})
	}
}

// The wrapper has no probe: resolving `agents-infra` and asking IT for
// a version would attest the wrapper, not pi's release, so the system
// establishes none and yolo without an explicitly passed release fails
// closed as unverified.
func TestTheWrapperEstablishesNoToolRelease(t *testing.T) {
	_, binDir := interactiveRequest(t)
	if _, err := agentic.ProbeToolRelease(context.Background(), New(&fakeStatusReader{}), []string{"PATH=" + binDir}); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
		t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
	}
}

// Native arguments outside interactive mode are refused, through
// BuildPlan and through the plugin held directly: no other grammar
// forwards them.
func TestNativeArgsOutsideInteractiveAreRefused(t *testing.T) {
	req, _ := interactiveRequest(t)
	req.NativeArgs = []string{"--approve"}
	if _, err := buildPlan(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("BuildPlan err = %v, want ErrNativeArgsNotInteractive", err)
	}
	if _, err := New(&fakeStatusReader{}).Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("Argv err = %v, want ErrNativeArgsNotInteractive", err)
	}
}
