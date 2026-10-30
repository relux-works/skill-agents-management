package claude

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// v3Releases are the in-window Claude Code rows under permission-grammar-v3;
// every V3 refusal and its positive controls run against each of them.
var v3Releases = []string{"2.1.312", "2.1.314"}

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
// it verbatim, at each V3 release (v3Releases).
func TestClaude2312RefusesUnqualifiedSelectorsUnderYolo(t *testing.T) {
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3RefusesUnqualifiedSelectorsUnderYolo(t, release) })
	}
}

func v3RefusesUnqualifiedSelectorsUnderYolo(t *testing.T, release string) {
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
			req.ToolRelease = release
			req.NativeArgs = c.args
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("yolo %s: BuildPlan err = %v, want ErrNativePolicyUnknown", release, err)
			}
			for _, want := range []string{c.selector, string(agentic.PermissionGrammarV3)} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %q", err, want)
				}
			}
			if _, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("yolo %s: Argv err = %v, want ErrNativePolicyUnknown", release, err)
			}

			req.PermissionMode = agentic.PermissionModeNative
			if got, want := argvFor(t, req, agentic.LaunchModeInteractive), append(append([]string{}, base...), c.args...); !reflect.DeepEqual(got, want) {
				t.Fatalf("native %s: Argv = %#v, want %#v", release, got, want)
			}

		})
	}
}

// V3 retains every V2 refusal at each V3 release, and an unrelated flag beside the
// bypass still forwards, so the row does not refuse everything.
func TestClaude2312RetainsV2Refusals(t *testing.T) {
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3RetainsV2Refusals(t, release) })
	}
}

func v3RetainsV2Refusals(t *testing.T, release string) {
	t.Parallel()
	workDir := tempSlot(t)
	yolo := func(args ...string) agentic.LaunchRequest {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = release
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

// The hidden scalars 2.1.312 added (still in 2.1.314) own their next token, as the native parser
// does: a --permission-mode taken as their value is not a selector, while a
// real one after the value still conflicts.
func TestClaude2312HiddenScalarsOwnTheirValue(t *testing.T) {
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3HiddenScalarsOwnTheirValue(t, release) })
	}
}

func v3HiddenScalarsOwnTheirValue(t *testing.T, release string) {
	t.Parallel()
	workDir := tempSlot(t)
	base := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial, bypassPermissionsFlag}
	for _, args := range [][]string{
		{"--fast-mode", "--permission-mode", "plan"},
		{"--runner-declares", "--restricted"},
	} {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = release
		req.NativeArgs = args
		if got, want := argvFor(t, req, agentic.LaunchModeInteractive), append(append([]string{}, base...), args...); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: Argv = %#v, want %#v", args, got, want)
		}
	}
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = release
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
	if got, err := New().ClassifyNonInteractiveArgs("2.1.314", []string{"--fast-mode", "on", "-p", "hello"}); err != nil || got.Form != agentic.NonInteractiveFormPrint || got.Grammar != agentic.PermissionGrammarV3 {
		t.Fatalf("Classify(2.1.314) = %#v, %v; want print under %s", got, err, agentic.PermissionGrammarV3)
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
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3YoloRefusesUnqualifiedSettings(t, release) })
	}
}

func v3YoloRefusesUnqualifiedSettings(t *testing.T, release string) {
	t.Parallel()
	disable := `{"permissions":{"disableBypassPermissionsMode":"disable"}}`
	workDir := tempSlot(t)
	if err := os.WriteFile(filepath.Join(workDir, "perm.json"), []byte(disable), 0600); err != nil {
		t.Fatal(err)
	}
	request := func(mode agentic.PermissionMode, args ...string) agentic.LaunchRequest {
		req := interactiveRequest(workDir)
		req.PermissionMode = mode
		req.ToolRelease = release
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

// Documented limit, not a guarantee: the option tables describe in-window
// releases only, so the native-mode re-enable scan reads the token after
// --fast-mode as its value on every release. That matches 2.1.312. On an
// out-of-window release such as 2.1.261, which does not know --fast-mode, the
// binary would read that token as a flag, and the module does not see the
// re-enable. Yolo there still fails closed (TestClaudeRetiredReleaseRefusesYolo).
// The control shows the scan itself still refuses a bare re-enable on both.
func TestClaudeOutOfWindowNativeScansUseInWindowTables(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	base := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}
	for _, release := range []string{"2.1.312", "2.1.261"} {
		req := interactiveRequest(workDir)
		req.ToolRelease = release
		req.NativeArgs = []string{"--fast-mode", "--allowedTools=AskUserQuestion"}
		if got, want := argvFor(t, req, agentic.LaunchModeInteractive), append(append([]string{}, base...), req.NativeArgs...); !reflect.DeepEqual(got, want) {
			t.Fatalf("native %s: Argv = %#v, want %#v", release, got, want)
		}
		req.NativeArgs = []string{"--allowedTools=AskUserQuestion"}
		if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
			t.Fatalf("native %s bare re-enable: err = %v, want ErrDeniedToolReEnabled", release, err)
		}
	}
}

// Under V3 yolo a non-empty top-level `env` in the effective explicit settings
// refuses before any plan, through both permission request channels (the
// request's mode and the permission context descriptor). This covers the
// scrub variable that makes 2.1.312 fall back to mode "default" over the
// bypass, inline in separate and equals form, from a file, beside qualified
// permissions, as the last of repeated sources, and any other env member.
// Native forwards each form. Positives: an empty env, and a scrub source
// followed by a harmless last source, which native takes instead. A non-object
// env refuses as invalid settings.
func TestClaude2312YoloRefusesSettingsEnv(t *testing.T) {
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3YoloRefusesSettingsEnv(t, release) })
	}
}

func v3YoloRefusesSettingsEnv(t *testing.T, release string) {
	t.Parallel()
	scrub := `{"env":{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB":"1"}}`
	workDir := tempSlot(t)
	if err := os.WriteFile(filepath.Join(workDir, "scrub.json"), []byte(scrub), 0600); err != nil {
		t.Fatal(err)
	}
	channels := map[string]func(args ...string) agentic.LaunchRequest{
		"request-mode": func(args ...string) agentic.LaunchRequest {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			req.NativeArgs = args
			return req
		},
		"context-descriptor": func(args ...string) agentic.LaunchRequest {
			req := interactiveRequest(workDir)
			req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
			req.ToolRelease = release
			req.NativeArgs = args
			return req
		},
	}
	for name, yolo := range channels {
		for _, c := range []struct {
			member string
			args   []string
		}{
			{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", []string{"--settings", scrub}},
			{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", []string{"--settings=" + scrub}},
			{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", []string{"--settings", "scrub.json"}},
			{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", []string{"--settings", `{"model":"x"}`, "--settings", scrub}},
			{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", []string{"--settings", `{"permissions":{"deny":["Bash(rm *)"]},"env":{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB":"1"}}`}},
			{"CLAUDE_CONFIG_DIR", []string{"--settings", `{"env":{"CLAUDE_CONFIG_DIR":"/tmp/elsewhere"}}`}},
		} {
			t.Run(name+"/"+strings.Join(c.args, " "), func(t *testing.T) {
				err := planErrorFor(t, yolo(c.args...), agentic.LaunchModeInteractive)
				if !errors.Is(err, agentic.ErrNativePolicyUnknown) {
					t.Fatalf("yolo: err = %v, want ErrNativePolicyUnknown", err)
				}
				for _, want := range []string{c.member, string(agentic.PermissionGrammarV3)} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal %q does not name %q", err, want)
					}
				}
			})
		}
		err := planErrorFor(t, yolo("--settings", `{"env":"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1"}`), agentic.LaunchModeInteractive)
		var settingsErr *agentic.SettingsPolicyError
		if !errors.As(err, &settingsErr) || settingsErr.Kind != agentic.SettingsPolicyInvalid {
			t.Fatalf("%s non-object env: err = %v, want the invalid settings kind", name, err)
		}
		for _, args := range [][]string{
			{"--settings", `{"env":{}}`},
			{"--settings", scrub, "--settings", `{"model":"x"}`},
		} {
			if got := argvFor(t, yolo(args...), agentic.LaunchModeInteractive); !slices.Contains(got, bypassPermissionsFlag) || !slices.Equal(got[len(got)-len(args):], args) {
				t.Fatalf("%s %q: Argv = %#v, want the bypass and the caller settings unchanged", name, args, got)
			}
		}
	}
	native := interactiveRequest(workDir)
	native.ToolRelease = release
	for _, args := range [][]string{{"--settings", scrub}, {"--settings", "scrub.json"}, {"--settings", `{"env":{"CLAUDE_CONFIG_DIR":"/tmp/elsewhere"}}`}} {
		native.NativeArgs = args
		want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}, args...)
		if got := argvFor(t, native, agentic.LaunchModeInteractive); !reflect.DeepEqual(got, want) {
			t.Fatalf("native %q: Argv = %#v, want %#v", args, got, want)
		}
	}
}

// Under V3 yolo the explicit settings source is checked against a top-level
// allowlist: any member outside `$schema`, `model`, `permissions` and an
// empty `env` refuses before any plan, through both permission request
// channels. This covers a PreToolUse hook that answers "ask" (2.1.312 runs
// flag-settings hooks before mode evaluation), inline in separate and equals
// form, from a file, beside qualified permissions, and as the last of
// repeated sources, plus a plugin source and an unknown key. A qualified key
// of the wrong shape refuses as invalid settings. Native forwards each
// refused form. Positives: every qualified key together, and a hook source
// followed by a harmless last source, which native takes instead.
func TestClaude2312YoloSettingsTopLevelAllowlist(t *testing.T) {
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3YoloSettingsTopLevelAllowlist(t, release) })
	}
}

func v3YoloSettingsTopLevelAllowlist(t *testing.T, release string) {
	t.Parallel()
	hook := `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"printf '{\"hookSpecificOutput\":{\"permissionDecision\":\"ask\"}}'"}]}]}}`
	workDir := tempSlot(t)
	if err := os.WriteFile(filepath.Join(workDir, "hook.json"), []byte(hook), 0600); err != nil {
		t.Fatal(err)
	}
	channels := map[string]func(args ...string) agentic.LaunchRequest{
		"request-mode": func(args ...string) agentic.LaunchRequest {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			req.NativeArgs = args
			return req
		},
		"context-descriptor": func(args ...string) agentic.LaunchRequest {
			req := interactiveRequest(workDir)
			req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
			req.ToolRelease = release
			req.NativeArgs = args
			return req
		},
	}
	refused := []struct {
		member string
		args   []string
	}{
		{"hooks", []string{"--settings", hook}},
		{"hooks", []string{"--settings=" + hook}},
		{"hooks", []string{"--settings", "hook.json"}},
		{"hooks", []string{"--settings=hook.json"}},
		{"hooks", []string{"--settings", `{"model":"x"}`, "--settings", hook}},
		{"hooks", []string{"--settings", `{"permissions":{"deny":["Bash(rm *)"]},"hooks":{}}`}},
		{"enabledPlugins", []string{"--settings", `{"enabledPlugins":{"tool@market":true}}`}},
		{"futureSetting", []string{"--settings", `{"model":"x","futureSetting":1}`}},
	}
	for name, yolo := range channels {
		for _, c := range refused {
			t.Run(name+"/"+strings.Join(c.args, " "), func(t *testing.T) {
				err := planErrorFor(t, yolo(c.args...), agentic.LaunchModeInteractive)
				if !errors.Is(err, agentic.ErrNativePolicyUnknown) {
					t.Fatalf("yolo: err = %v, want ErrNativePolicyUnknown", err)
				}
				for _, want := range []string{c.member, string(agentic.PermissionGrammarV3)} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal %q does not name %q", err, want)
					}
				}
			})
		}
		for _, args := range [][]string{
			{"--settings", `{"model":1}`},
			{"--settings", `{"model":null}`},
			{"--settings", `{"$schema":["x"]}`},
		} {
			err := planErrorFor(t, yolo(args...), agentic.LaunchModeInteractive)
			var settingsErr *agentic.SettingsPolicyError
			if !errors.As(err, &settingsErr) || settingsErr.Kind != agentic.SettingsPolicyInvalid {
				t.Fatalf("%s %q: err = %v, want the invalid settings kind", name, args, err)
			}
		}
		for _, args := range [][]string{
			{"--settings", `{"$schema":"https://json.schemastore.org/claude-code-settings.json","model":"claude-x","permissions":{"allow":["Read"],"deny":["Bash(rm *)"],"ask":["Edit"],"additionalDirectories":["/tmp"]},"env":{}}`},
			{"--settings", hook, "--settings", `{"model":"x"}`},
		} {
			if got := argvFor(t, yolo(args...), agentic.LaunchModeInteractive); !slices.Contains(got, bypassPermissionsFlag) || !slices.Equal(got[len(got)-len(args):], args) {
				t.Fatalf("%s %q: Argv = %#v, want the bypass and the caller settings unchanged", name, args, got)
			}
		}
	}
	native := interactiveRequest(workDir)
	native.ToolRelease = release
	for _, c := range refused {
		native.NativeArgs = c.args
		want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}, c.args...)
		if got := argvFor(t, native, agentic.LaunchModeInteractive); !reflect.DeepEqual(got, want) {
			t.Fatalf("native %q: Argv = %#v, want %#v", c.args, got, want)
		}
	}
}

// Under V3 yolo every qualified permissions member must be a non-null array
// of non-null strings, the shape 2.1.312 declares; a scalar, an object, null
// or a mixed array in deny, ask, additionalDirectories or allow refuses as
// invalid settings before any plan, through both permission request
// channels. Empty and non-empty string arrays stay admitted.
func TestClaude2312YoloPermissionMemberShapes(t *testing.T) {
	for _, release := range v3Releases {
		t.Run(release, func(t *testing.T) { v3YoloPermissionMemberShapes(t, release) })
	}
}

func v3YoloPermissionMemberShapes(t *testing.T, release string) {
	t.Parallel()
	workDir := tempSlot(t)
	channels := map[string]func(args ...string) agentic.LaunchRequest{
		"request-mode": func(args ...string) agentic.LaunchRequest {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			req.NativeArgs = args
			return req
		},
		"context-descriptor": func(args ...string) agentic.LaunchRequest {
			req := interactiveRequest(workDir)
			req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
			req.ToolRelease = release
			req.NativeArgs = args
			return req
		},
	}
	for name, yolo := range channels {
		for _, member := range []string{"deny", "ask", "additionalDirectories", "allow"} {
			for _, value := range []string{`"Bash(rm *)"`, `{"defaultMode":"plan"}`, `null`, `[null]`, `["Read",7]`, `[{"defaultMode":"plan"}]`} {
				args := []string{"--settings", `{"permissions":{"` + member + `":` + value + `}}`}
				err := planErrorFor(t, yolo(args...), agentic.LaunchModeInteractive)
				var settingsErr *agentic.SettingsPolicyError
				if !errors.As(err, &settingsErr) || settingsErr.Kind != agentic.SettingsPolicyInvalid {
					t.Fatalf("%s %s=%s: err = %v, want the invalid settings kind", name, member, value, err)
				}
			}
			for _, value := range []string{`[]`, `["Read","Edit"]`} {
				args := []string{"--settings", `{"permissions":{"` + member + `":` + value + `}}`}
				if got := argvFor(t, yolo(args...), agentic.LaunchModeInteractive); !slices.Contains(got, bypassPermissionsFlag) {
					t.Fatalf("%s %s=%s: Argv = %#v, want a yolo plan", name, member, value, got)
				}
			}
		}
	}
	// The shape validation is a yolo guard only: native forwards the same
	// malformed deny, ask and additionalDirectories members verbatim and adds
	// no bypass. A malformed allow keeps the refusal every mode shares
	// (toolpolicy), so it is not a native control here.
	native := interactiveRequest(workDir)
	native.ToolRelease = release
	base := []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}
	for _, member := range []string{"deny", "ask", "additionalDirectories"} {
		for _, value := range []string{`"Bash(rm *)"`, `{"defaultMode":"plan"}`, `null`, `[null]`, `["Read",7]`, `[{"defaultMode":"plan"}]`} {
			native.NativeArgs = []string{"--settings", `{"permissions":{"` + member + `":` + value + `}}`}
			got := argvFor(t, native, agentic.LaunchModeInteractive)
			if want := append(append([]string{}, base...), native.NativeArgs...); !reflect.DeepEqual(got, want) || slices.Contains(got, bypassPermissionsFlag) {
				t.Fatalf("native %s=%s: Argv = %#v, want %#v and no bypass", member, value, got, want)
			}
		}
	}
}
