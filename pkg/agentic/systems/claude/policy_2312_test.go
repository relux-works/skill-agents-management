package claude

import (
	"errors"
	"os"
	"path/filepath"
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
// refuses yolo, in flag and equals forms, before any plan, and native forwards
// it verbatim at 2.1.312.
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

// The 2.1.261 row is retired under the owner's support window: yolo there
// fails closed as unverified before any plan, in every form including the
// arguments a shared option table would misread on that release, while native
// still forwards the same arguments verbatim.
func TestClaudeRetiredReleaseRefusesYolo(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	base := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}
	for _, args := range [][]string{
		nil,
		{"--fast-mode", "--permission-mode", "bypassPermissions"},
		{"--runner-declares", bypassPermissionsFlag},
	} {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "2.1.261"
		req.NativeArgs = args
		if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("yolo 2.1.261 %q: err = %v, want ErrPermissionModeUnverifiedRelease", args, err)
		}
		if _, err := New().PermissionMapping("2.1.261", agentic.PermissionModeYolo); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("PermissionMapping(2.1.261, yolo) err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		req.PermissionMode = agentic.PermissionModeNative
		if got, want := argvFor(t, req, agentic.LaunchModeInteractive), append(append([]string{}, base...), args...); !reflect.DeepEqual(got, want) {
			t.Fatalf("native 2.1.261 %q: Argv = %#v, want %#v", args, got, want)
		}
	}
}

// Under V3 yolo an explicit settings source whose permissions object carries
// any member other than the rule lists and extra directories refuses before
// any plan: disableBypassPermissionsMode, which 2.1.312 reads to skip the
// bypass mode, defaultMode, and an unknown member, inline in separate and
// equals form, from a file, and as the last of repeated sources. The same
// arguments forward at native. The positives keep harmless settings, the
// qualified members and a refused source followed by a harmless last one
// (native takes the last), so the guard does not refuse every settings use.
// A source it cannot read, decode or select refuses with its settings kind.
func TestClaude2312YoloRefusesUnqualifiedSettings(t *testing.T) {
	t.Parallel()
	disable := `{"permissions":{"disableBypassPermissionsMode":"disable"}}`
	workDir := tempSlot(t)
	if err := os.WriteFile(filepath.Join(workDir, "perm.json"), []byte(disable), 0600); err != nil {
		t.Fatal(err)
	}
	request := func(mode agentic.PermissionMode, args ...string) agentic.LaunchRequest {
		req := interactiveRequest(workDir)
		req.PermissionMode = mode
		req.ToolRelease = "2.1.312"
		req.NativeArgs = args
		return req
	}
	for _, c := range []struct {
		member string
		args   []string
	}{
		{"disableBypassPermissionsMode", []string{"--settings", disable}},
		{"disableBypassPermissionsMode", []string{"--settings=" + disable}},
		{"disableBypassPermissionsMode", []string{"--settings", "perm.json"}},
		{"disableBypassPermissionsMode", []string{"--settings", `{"model":"x"}`, "--settings", disable}},
		{"defaultMode", []string{"--settings", `{"permissions":{"defaultMode":"plan"}}`}},
		{"futureKey", []string{"--settings", `{"permissions":{"allow":[],"futureKey":1}}`}},
		{managedSettingsFlag, []string{"--managed-settings", disable}},
		{managedSettingsFlag, []string{"--managed-settings=" + disable}},
		{projectConfigRootFlag, []string{"--project-config-root", workDir}},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			err := planErrorFor(t, request(agentic.PermissionModeYolo, c.args...), agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("yolo: err = %v, want ErrNativePolicyUnknown", err)
			}
			for _, want := range []string{c.member, string(agentic.PermissionGrammarV3)} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %q", err, want)
				}
			}
			want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}, c.args...)
			if got := argvFor(t, request(agentic.PermissionModeNative, c.args...), agentic.LaunchModeInteractive); !reflect.DeepEqual(got, want) {
				t.Fatalf("native: Argv = %#v, want %#v", got, want)
			}
		})
	}
	for _, c := range []struct {
		kind agentic.SettingsPolicyFailureKind
		args []string
	}{
		{agentic.SettingsPolicyInvalid, []string{"--settings", `{"permissions":[]}`}},
		{agentic.SettingsPolicyUnreadable, []string{"--settings", "missing.json"}},
		{agentic.SettingsPolicyAmbiguous, []string{"--settings", disable, "--settings"}},
	} {
		err := planErrorFor(t, request(agentic.PermissionModeYolo, c.args...), agentic.LaunchModeInteractive)
		var settingsErr *agentic.SettingsPolicyError
		if !errors.As(err, &settingsErr) || settingsErr.Kind != c.kind {
			t.Fatalf("yolo %q: err = %v, want settings kind %s", c.args, err, c.kind)
		}
	}
	for _, args := range [][]string{
		nil,
		{"--settings", `{"model":"claude-x"}`},
		{"--settings", `{"permissions":{"allow":["Read"],"deny":["Bash(rm *)"],"ask":["Edit"],"additionalDirectories":["/tmp"]}}`},
		{"--settings", disable, "--settings", `{"model":"x"}`},
	} {
		want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, bypassPermissionsFlag}, args...)
		if got := argvFor(t, request(agentic.PermissionModeYolo, args...), agentic.LaunchModeInteractive); !reflect.DeepEqual(got, want) {
			t.Fatalf("yolo %q: Argv = %#v, want %#v", args, got, want)
		}
	}
}
