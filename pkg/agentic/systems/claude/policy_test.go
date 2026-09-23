package claude

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the claude half of the versioned provider-capability
// table (curator-spec Decision 0018 choices 3 and 6): the verified
// release the yolo mapping holds for, the drift it fails closed on, the
// closed `--permission-mode` grammar the yolo scan classifies, and the
// parsing rule that keeps prompt text out of the scan. Every row drives
// the real registry and BuildPlan — argvFor for the plans that must
// build, planErrorFor for the refusals — except where the plugin repeat
// needs the direct path too.

// The yolo positive with caller arguments: the bypass flag lands after
// model and effort and BEFORE the verbatim suffix (Decision 0018 item
// 1), and the suffix carries positionals, shorts, and prose untouched.
func TestYoloForwardsNativeArgsVerbatimAfterTheBypassFlag(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "2.1.261"
	req.NativeArgs = []string{"--verbose", "deploy the thing", "-d"}

	got := argvFor(t, req, agentic.LaunchModeInteractive)
	want := []string{"--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, "--verbose", "deploy the thing", "-d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Argv = %#v, want %#v", got, want)
	}
}

// Native performs no argv inspection at all: unknown policy forms, the
// bypass flag itself, and an unverified or missing release all pass
// through verbatim. The interface is UX, not a perimeter (Decision
// 0018 item 4), and the raw contract is unchanged.
func TestNativeForwardsEverythingVerbatimWithoutInspection(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	native := []string{"--permission-mode", "ultrastrict", "--verbose", bypassPermissionsFlag}
	for _, c := range []struct {
		name    string
		mode    agentic.PermissionMode
		release string
	}{
		{"zero value at the pinned release", "", "2.1.261"},
		{"explicit native at the pinned release", agentic.PermissionModeNative, "2.1.261"},
		{"explicit native past the pin", agentic.PermissionModeNative, "2.1.274"},
		{"explicit native with no release established", agentic.PermissionModeNative, ""},
		{"zero value with no release established", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = c.mode
			req.ToolRelease = c.release
			req.NativeArgs = native
			got := argvFor(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"--model", parityModel, "--effort", parityEffort}, native...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Argv = %#v, want %#v", got, want)
			}
		})
	}
}

// Drift fails closed first: yolo at any release but the pinned one —
// the installed newer release, an invented future one, or none at all —
// is refused with the named diagnostic, through BuildPlan and through
// the plugin held directly.
func TestYoloRefusesDriftWithANamedDiagnostic(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, release := range []string{"2.1.274", "9.9.9", ""} {
		t.Run("release "+quoteRelease(release), func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("BuildPlan err = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
			if _, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("Argv err = %v, want ErrPermissionModeUnverifiedRelease", err)
			}
		})
	}
	t.Run("the diagnostic names the grammar token the launcher cites", func(t *testing.T) {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "2.1.274"
		err := planErrorFor(t, req, agentic.LaunchModeInteractive)
		if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		for _, want := range []string{string(agentic.PermissionGrammarV2), "2.1.274", "2.1.261"} {
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

// An unknown `--permission-mode` value under yolo is refused as usage
// (the caller maps it to exit 2), in separate form, `=` form, and when
// the flag dangles with no value at all. Never resolved into a claim
// about the session's posture.
func TestYoloRefusesUnknownPermissionModes(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"separate form", []string{"--permission-mode", "ultrastrict"}, "ultrastrict"},
		{"equals form", []string{"--permission-mode=ultrastrict"}, "ultrastrict"},
		{"empty equals value", []string{"--permission-mode="}, ""},
		{"a dangling flag", []string{"--permission-mode"}, "--permission-mode"},
		{"case is exact", []string{"--permission-mode", "Auto"}, "Auto"},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = "2.1.261"
			req.NativeArgs = c.args
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("BuildPlan err = %v, want ErrNativePolicyUnknown", err)
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("refusal %q does not name %q", err, c.want)
			}
			if _, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("Argv err = %v, want ErrNativePolicyUnknown", err)
			}
		})
	}
}

// Every known permission mode conflicts with yolo in both argv forms.
// Each row drives BuildPlan, and its native twin proves inspection is
// mode-gated rather than shared with the forwarding path.
func TestPermissionModeConflictMatrix(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, value := range []string{"acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk", "plan"} {
		for _, form := range []struct {
			name      string
			args      []string
			placement agentic.NativePolicyPlacement
		}{
			{"separate-token", []string{"--permission-mode", value}, agentic.NativePolicyPlacementSeparateToken},
			{"equals", []string{"--permission-mode=" + value}, agentic.NativePolicyPlacementEquals},
		} {
			t.Run(value+"/"+form.name, func(t *testing.T) {
				req := interactiveRequest(workDir)
				req.PermissionMode = agentic.PermissionModeYolo
				req.ToolRelease = "2.1.261"
				req.NativeArgs = form.args
				err := planErrorFor(t, req, agentic.LaunchModeInteractive)
				assertNativePolicyConflict(t, err, "--permission-mode", form.placement)
			})
			t.Run("native/"+value+"/"+form.name, func(t *testing.T) {
				req := interactiveRequest(workDir)
				req.PermissionMode = agentic.PermissionModeNative
				req.ToolRelease = "2.1.261"
				req.NativeArgs = form.args
				got := argvFor(t, req, agentic.LaunchModeInteractive)
				want := append([]string{"--model", parityModel, "--effort", parityEffort}, form.args...)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Argv = %#v, want %#v", got, want)
				}
			})
		}
	}
}

func TestOtherKnownClaudePolicyConflicts(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	rows := []struct {
		name      string
		selector  string
		placement agentic.NativePolicyPlacement
		args      []string
		duplicate bool
	}{
		{"allow-bypass flag", allowDangerouslySkipPermissionsFlag, agentic.NativePolicyPlacementFlag, []string{allowDangerouslySkipPermissionsFlag}, false},
		{"allow-bypass equals", allowDangerouslySkipPermissionsFlag, agentic.NativePolicyPlacementEquals, []string{allowDangerouslySkipPermissionsFlag + "=true"}, false},
		{"restricted flag", restrictedFlag, agentic.NativePolicyPlacementFlag, []string{restrictedFlag}, false},
		{"restricted equals", restrictedFlag, agentic.NativePolicyPlacementEquals, []string{restrictedFlag + "=true"}, false},
		{"mapped bypass flag", bypassPermissionsFlag, agentic.NativePolicyPlacementFlag, []string{bypassPermissionsFlag}, true},
		{"mapped bypass equals", bypassPermissionsFlag, agentic.NativePolicyPlacementEquals, []string{bypassPermissionsFlag + "=true"}, true},
	}
	for _, row := range rows {
		t.Run("yolo/"+row.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = "2.1.261"
			req.NativeArgs = row.args
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if row.duplicate {
				if !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
					t.Fatalf("BuildPlan err = %v, want ErrPermissionModeDuplicate", err)
				}
				return
			}
			assertNativePolicyConflict(t, err, row.selector, row.placement)
		})
		t.Run("native/"+row.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeNative
			req.ToolRelease = "2.1.261"
			req.NativeArgs = row.args
			got := argvFor(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"--model", parityModel, "--effort", parityEffort}, row.args...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Argv = %#v, want %#v", got, want)
			}
		})
	}
}

func TestYoloForwardsKnownNonConflictingClaudeSelectors(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "2.1.261"
	req.NativeArgs = []string{"--debug", "--verbose", "-d"}
	got := argvFor(t, req, agentic.LaunchModeInteractive)
	want := []string{"--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, "--debug", "--verbose", "-d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Argv = %#v, want %#v", got, want)
	}
}

func assertNativePolicyConflict(t *testing.T, err error, selector string, placement agentic.NativePolicyPlacement) {
	t.Helper()
	if !errors.Is(err, agentic.ErrNativePolicyConflict) {
		t.Fatalf("BuildPlan err = %v, want ErrNativePolicyConflict", err)
	}
	var conflict *agentic.NativePolicyConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("BuildPlan err %T does not contain NativePolicyConflictError", err)
	}
	if conflict.Selector != selector || conflict.Placement != placement {
		t.Fatalf("conflict = %+v, want selector %q placement %q", conflict, selector, placement)
	}
}

// The parsing rule, both directions: the same flag-looking text is
// refused in flag position and forwarded as prompt text after `--` —
// and a positional that never led with a dash is prompt wherever it
// sits.
func TestPromptTextIsNeverParsedAsAFlag(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	yolo := func(args []string) agentic.LaunchRequest {
		req := interactiveRequest(workDir)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = "2.1.261"
		req.NativeArgs = args
		return req
	}
	t.Run("an unknown mode after the separator is prompt, not policy", func(t *testing.T) {
		got := argvFor(t, yolo([]string{"--", "--permission-mode", "ultrastrict"}), agentic.LaunchModeInteractive)
		want := []string{"--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, "--", "--permission-mode", "ultrastrict"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Argv = %#v, want %#v", got, want)
		}
		if err := planErrorFor(t, yolo([]string{"--permission-mode", "ultrastrict"}), agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
			t.Fatalf("the flag-position twin was not refused: %v", err)
		}
	})
	t.Run("the bypass flag after the separator is prompt, not a duplicate", func(t *testing.T) {
		got := argvFor(t, yolo([]string{"--", bypassPermissionsFlag}), agentic.LaunchModeInteractive)
		want := []string{"--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, "--", bypassPermissionsFlag}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Argv = %#v, want %#v", got, want)
		}
		if err := planErrorFor(t, yolo([]string{bypassPermissionsFlag}), agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
			t.Fatalf("the flag-position twin was not refused as a duplicate: %v", err)
		}
	})
	t.Run("a positional that never led with a dash is prompt", func(t *testing.T) {
		got := argvFor(t, yolo([]string{"ultrastrict", "deploy"}), agentic.LaunchModeInteractive)
		want := []string{"--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, "ultrastrict", "deploy"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Argv = %#v, want %#v", got, want)
		}
	})
	t.Run("and a lone dash is positional too", func(t *testing.T) {
		got := argvFor(t, yolo([]string{"-"}), agentic.LaunchModeInteractive)
		want := []string{"--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, "-"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Argv = %#v, want %#v", got, want)
		}
	})
}

// The mapped bypass flag in the caller's flag positions is refused,
// not emitted twice: at a non-zero index (so a scan narrowed to the
// first element admits it), and in `=` form (so an exact-match scan
// admits that).
func TestYoloRefusesTheBypassFlagInNativeArgs(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, c := range []struct {
		name string
		args []string
	}{
		{"at a non-zero index", []string{"--verbose", bypassPermissionsFlag}},
		{"in equals form", []string{bypassPermissionsFlag + "=true"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = "2.1.261"
			req.NativeArgs = c.args
			if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
				t.Fatalf("BuildPlan err = %v, want ErrPermissionModeDuplicate", err)
			}
			if _, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
				t.Fatalf("Argv err = %v, want ErrPermissionModeDuplicate", err)
			}
		})
	}
}

// The scan is placement-insensitive: a positional `exec` ahead of the
// flag does not hide it. Only `--` ends flag parsing.
func TestTheScanSeesExecPlacement(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "2.1.261"
	req.NativeArgs = []string{"exec", "--permission-mode", "ultrastrict"}
	if err := planErrorFor(t, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
		t.Fatalf("err = %v, want ErrNativePolicyUnknown", err)
	}
}

// Native arguments outside interactive mode are refused, through
// BuildPlan and through the plugin held directly: no other grammar
// forwards them.
func TestNativeArgsOutsideInteractiveAreRefused(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := interactiveRequest(workDir)
	req.NativeArgs = []string{"--verbose"}
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	if err := planErrorFor(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("BuildPlan err = %v, want ErrNativeArgsNotInteractive", err)
	}
	if _, err := New().Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("Argv err = %v, want ErrNativeArgsNotInteractive", err)
	}
}
