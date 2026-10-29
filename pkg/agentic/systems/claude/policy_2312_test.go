package claude

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The 2.1.312 row maps yolo to the bypass flag under V3, with the exact argv
// order of the V2 row; neighbouring and near-miss releases stay unverified.
func TestClaude2312RowMapsYoloUnderV3(t *testing.T) {
	t.Parallel()
	got, err := New().PermissionMapping("2.1.312", agentic.PermissionModeYolo)
	if err != nil || got.Flag != bypassPermissionsFlag || got.Grammar != agentic.PermissionGrammarV3 {
		t.Fatalf("PermissionMapping(2.1.312, yolo) = %#v, %v; want the bypass flag under %s", got, err, agentic.PermissionGrammarV3)
	}
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "2.1.312"
	req.NativeArgs = []string{"--verbose", "deploy the thing"}
	want := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, bypassPermissionsFlag, "--verbose", "deploy the thing"}
	if argv := argvFor(t, req, agentic.LaunchModeInteractive); !reflect.DeepEqual(argv, want) {
		t.Fatalf("Argv = %#v, want %#v", argv, want)
	}
	for _, release := range []string{"2.1.311", "2.1.313", "2.1.312.1", "2.1.3120"} {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = release
		if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("release %s: BuildPlan err = %v, want ErrPermissionModeUnverifiedRelease", release, err)
		}
	}
}

// Under V3 each selector whose interaction with the bypass was not qualified
// refuses yolo, in flag and equals forms, before any plan. Native forwards it
// verbatim at 2.1.312, and the V2 row keeps forwarding it under yolo: the
// asymmetry is intentional, so the historical row does not change.
func TestClaude2312RefusesUnqualifiedSelectorsUnderYolo(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	base := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}
	for _, c := range []struct {
		selector string
		args     []string
	}{
		{inheritPermissionModeFlag, []string{"--inherit-permission-mode", "bypassPermissions"}},
		{inheritPermissionModeFlag, []string{"--inherit-permission-mode=plan"}},
		{planModeRequiredFlag, []string{"--plan-mode-required"}},
		{permissionPromptsFlag, []string{"--permission-prompts", "none"}},
		{permissionPromptsFlag, []string{"--permission-prompts=host"}},
		{enableAutoModeFlag, []string{"--enable-auto-mode"}},
		{planModeRequiredFlag, []string{"--verbose", "--plan-mode-required", "deploy"}},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = "2.1.312"
			req.NativeArgs = c.args
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("yolo 2.1.312: BuildPlan err = %v, want ErrNativePolicyUnknown", err)
			}
			for _, want := range []string{c.selector, string(agentic.PermissionGrammarV3)} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %q", err, want)
				}
			}
			if _, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("yolo 2.1.312: Argv err = %v, want ErrNativePolicyUnknown", err)
			}

			req.PermissionMode = agentic.PermissionModeNative
			if got, want := argvFor(t, req, agentic.LaunchModeInteractive), append(append([]string{}, base...), c.args...); !reflect.DeepEqual(got, want) {
				t.Fatalf("native 2.1.312: Argv = %#v, want %#v", got, want)
			}

			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = "2.1.261"
			want := append(append(append([]string{}, base...), bypassPermissionsFlag), c.args...)
			if got := argvFor(t, req, agentic.LaunchModeInteractive); !reflect.DeepEqual(got, want) {
				t.Fatalf("yolo 2.1.261 (V2): Argv = %#v, want %#v", got, want)
			}
		})
	}
}

// V3 retains every V2 refusal at 2.1.312, and an unrelated flag beside the
// bypass still forwards, so the row does not refuse everything.
func TestClaude2312RetainsV2Refusals(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	yolo := func(args ...string) agentic.LaunchRequest {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "2.1.312"
		req.NativeArgs = args
		return req
	}
	assertNativePolicyConflict(t, planErrorFor(t, yolo("--permission-mode", "plan"), agentic.LaunchModeInteractive), permissionModeFlag, agentic.NativePolicyPlacementSeparateToken)
	assertNativePolicyConflict(t, planErrorFor(t, yolo("--restricted"), agentic.LaunchModeInteractive), restrictedFlag, agentic.NativePolicyPlacementFlag)
	assertNativePolicyConflict(t, planErrorFor(t, yolo("--allow-dangerously-skip-permissions"), agentic.LaunchModeInteractive), allowDangerouslySkipPermissionsFlag, agentic.NativePolicyPlacementFlag)
	if err := planErrorFor(t, yolo(bypassPermissionsFlag), agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
		t.Fatalf("duplicate bypass: err = %v, want ErrPermissionModeDuplicate", err)
	}
	err := planErrorFor(t, yolo("--permission-mode", "ultrastrict"), agentic.LaunchModeInteractive)
	if !errors.Is(err, agentic.ErrNativePolicyUnknown) || !strings.Contains(err.Error(), string(agentic.PermissionGrammarV3)) {
		t.Fatalf("unknown mode: err = %v, want ErrNativePolicyUnknown naming %s", err, agentic.PermissionGrammarV3)
	}
	args := []string{"--safe-mode", "--permission-prompt-tool", "mcp__host__approve", "--verbose"}
	want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, bypassPermissionsFlag}, args...)
	if got := argvFor(t, yolo(args...), agentic.LaunchModeInteractive); !reflect.DeepEqual(got, want) {
		t.Fatalf("Argv = %#v, want %#v", got, want)
	}
}

// The hidden scalars 2.1.312 adds own their next token, as the native parser
// does: a --permission-mode taken as their value is not a selector, while a
// real one after the value still conflicts.
func TestClaude2312HiddenScalarsOwnTheirValue(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	base := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, bypassPermissionsFlag}
	for _, args := range [][]string{
		{"--fast-mode", "--permission-mode", "plan"},
		{"--runner-declares", "--restricted"},
	} {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "2.1.312"
		req.NativeArgs = args
		if got, want := argvFor(t, req, agentic.LaunchModeInteractive), append(append([]string{}, base...), args...); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: Argv = %#v, want %#v", args, got, want)
		}
	}
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "2.1.312"
	req.NativeArgs = []string{"--fast-mode", "on", "--permission-mode", "plan"}
	assertNativePolicyConflict(t, planErrorFor(t, req, agentic.LaunchModeInteractive), permissionModeFlag, agentic.NativePolicyPlacementSeparateToken)
}

// The print classifier admits 2.1.312 under its V3 row, and a near-miss
// release still refuses as unverified.
func TestClaude2312ClassifiesPrint(t *testing.T) {
	t.Parallel()
	got, err := New().ClassifyNonInteractiveArgs("2.1.312", []string{"--fast-mode", "on", "-p", "hello"})
	if err != nil || got.Form != agentic.NonInteractiveFormPrint || got.Grammar != agentic.PermissionGrammarV3 {
		t.Fatalf("Classify(2.1.312) = %#v, %v; want print under %s", got, err, agentic.PermissionGrammarV3)
	}
	got, err = New().ClassifyNonInteractiveArgs("2.1.312", []string{"--verbose", "hello"})
	if err != nil || got.Form != agentic.NonInteractiveFormNone {
		t.Fatalf("Classify(2.1.312, interactive) = %#v, %v; want no print form", got, err)
	}
	if _, err := New().ClassifyNonInteractiveArgs("2.1.313", []string{"-p", "hello"}); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
		t.Fatalf("Classify(2.1.313) err = %v, want ErrPermissionModeUnverifiedRelease", err)
	}
}
