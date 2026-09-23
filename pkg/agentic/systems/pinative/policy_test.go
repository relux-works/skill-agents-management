package pinative_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

// This file is the pi-native half of the versioned
// provider-capability table (curator-spec Decision 0018 choice 6):
// drift fails closed before support is even asked, the verified
// release stays unsupported, and native forwards verbatim. Pi-native
// has no native-policy scan — yolo never reaches classification — but
// unlike the wrapper it does probe: its binary is raw `pi`, which
// attests its own release (probe_test.go).

// The verified release stays unsupported: yolo at pi 0.84.2 is refused
// with the unchanged sentinel even carrying native arguments no
// grammar knows — the refusal precedes classification, so the scan is
// never reached here.
func TestYoloAtTheVerifiedReleaseStaysUnsupported(t *testing.T) {
	req, _ := request(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "0.84.2"
	req.NativeArgs = []string{"--permission-mode", "ultrastrict", "-c", "future_key=x"}
	if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnsupported", err)
	}
	if _, err := pinative.New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("Argv err = %v, want ErrPermissionModeUnsupported", err)
	}
}

// Drift fails closed first: yolo at any release but the verified one —
// a newer release, an invented future one, or none at all — is refused
// as unverified, through BuildPlan and through the plugin held
// directly.
func TestYoloRefusesDriftWithANamedDiagnostic(t *testing.T) {
	for _, release := range []string{"0.85.0", "9.9.9", ""} {
		name := release
		if name == "" {
			name = "unestablished"
		}
		t.Run("release "+name, func(t *testing.T) {
			req, _ := request(t)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			if _, err := buildPlan(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
			if _, err := pinative.New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("Argv err = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
		})
	}
	t.Run("the diagnostic names the grammar token the launcher cites", func(t *testing.T) {
		req, _ := request(t)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "0.85.0"
		_, err := buildPlan(t, req, agentic.LaunchModeInteractive)
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

// Native forwards the caller's arguments with no inspection at any
// release — pinned, newer, or never established — because the raw
// contract is unchanged and no claim is made (Decision 0018 item 4).
func TestNativeForwardsVerbatimAtAnyRelease(t *testing.T) {
	native := []string{"--no-approve", "resume the session"}
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
			req, _ := request(t)
			req.PermissionMode = c.mode
			req.ToolRelease = c.release
			req.NativeArgs = native
			plan, err := buildPlan(t, req, agentic.LaunchModeInteractive)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			want := append([]string{"--model", "anthropic/claude-opus-5", "--thinking", "high"}, native...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
			}
		})
	}
}

// Native arguments outside interactive mode are refused, through
// BuildPlan and through the plugin held directly. Pi-native declares
// no exec mode, so the dry-run mirror — byte-identical argv, but no
// verbatim suffix — carries the repeat.
func TestNativeArgsOutsideInteractiveAreRefused(t *testing.T) {
	req, _ := request(t)
	req.NativeArgs = []string{"--no-approve"}
	if _, err := buildPlan(t, req, agentic.LaunchModeDryRun); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("BuildPlan err = %v, want ErrNativeArgsNotInteractive", err)
	}
	if _, err := pinative.New().Argv(req, agentic.LaunchModeDryRun); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("Argv err = %v, want ErrNativeArgsNotInteractive", err)
	}
}
