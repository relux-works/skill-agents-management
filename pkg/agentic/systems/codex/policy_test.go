package codex

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the codex half of the versioned provider-capability table
// (curator-spec Decision 0018 choices 3 and 6): the verified release the
// yolo mapping holds for, the drift it fails closed on, the closed `-c`
// key grammar the yolo scan classifies, and the parsing rule that keeps
// prompt text out of the scan. Every row drives the real registry and
// BuildPlan — interactivePlan for the plans that must build,
// interactivePlanError/planErrorIn for the refusals — except where the
// plugin repeat needs the direct path too.

func yoloRequest(workDir string) agentic.LaunchRequest {
	req := interactiveRequest(workDir)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "0.153.2"
	return req
}

// The yolo positive with caller arguments: the bypass flag lands after
// model and effort and BEFORE the verbatim suffix (Decision 0018 item
// 1), and the suffix carries flags, positionals, and prose untouched.
func TestYoloForwardsNativeArgsVerbatimAfterTheBypassFlag(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	req := yoloRequest(workDir)
	req.NativeArgs = []string{"--search", "resume the session", "-a", "never"}

	plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
	want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "--search", "resume the session", "-a", "never"}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
	}
}

// Native performs no argv inspection at all: unknown `-c` keys, the
// bypass flag itself, and an unverified or missing release all pass
// through verbatim. The interface is UX, not a perimeter (Decision
// 0018 item 4), and the raw contract is unchanged.
func TestNativeForwardsEverythingVerbatimWithoutInspection(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	native := []string{"-c", "future_key=x", bypassApprovalsAndSandboxFlag}
	for _, c := range []struct {
		name    string
		mode    agentic.PermissionMode
		release string
	}{
		{"zero value at the pinned release", "", "0.153.2"},
		{"explicit native at the pinned release", agentic.PermissionModeNative, "0.153.2"},
		{"explicit native past the pin", agentic.PermissionModeNative, "0.153.4"},
		{"explicit native with no release established", agentic.PermissionModeNative, ""},
		{"zero value with no release established", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = c.mode
			req.ToolRelease = c.release
			req.NativeArgs = native
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}, native...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
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
	for _, release := range []string{"0.153.4", "9.9.9", ""} {
		t.Run("release "+quoteRelease(release), func(t *testing.T) {
			req := interactiveRequest(workDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = release
			if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
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
		req.ToolRelease = "0.153.4"
		err := interactivePlanError(t, req)
		if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		for _, want := range []string{string(agentic.PermissionGrammarV1), "0.153.4", "0.153.2"} {
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

// An unknown `-c` key under yolo is refused as usage (the caller maps
// it to exit 2): separate `-c` and `--config`, `=` form, attached
// short, and every malformed shape — a dangling flag, a value without
// `key=`, a key with whitespace. Never resolved into a claim about the
// session's posture.
func TestYoloRefusesUnknownConfigKeys(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"separate -c", []string{"-c", "future_key=x"}, "future_key"},
		{"separate --config", []string{"--config", "future_key=x"}, "future_key"},
		{"equals form", []string{"--config=future_key=x"}, "future_key"},
		{"attached short", []string{"-cfuture_key=x"}, "future_key"},
		{"attached short with equals", []string{"-c=future_key=x"}, "future_key"},
		{"a dangling -c", []string{"-c"}, "-c"},
		{"a dangling --config", []string{"--config"}, "--config"},
		{"a value without key=", []string{"-c", "noequals"}, "noequals"},
		{"an empty key", []string{"-c", "=x"}, ""},
		{"a key with whitespace", []string{"-c", "bad key=x"}, "bad key"},
		{"case is exact", []string{"-c", "Approval_Policy=never"}, "Approval_Policy"},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := yoloRequest(workDir)
			req.NativeArgs = c.args
			err := interactivePlanError(t, req)
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

// The narrowing the closed set buys: every verified key builds and
// forwards verbatim — the module's own transports, the three policy
// keys, and the composition-evidenced `mcp_servers.` shape. The refusal
// above is the UNKNOWN's, not a refusal of the flag.
func TestYoloAdmitsTheVerifiedConfigKeys(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, key := range []string{"model_reasoning_effort", "service_tier", "approval_policy", "sandbox_mode", "sandbox_permissions", "mcp_servers.docs.url"} {
		t.Run(key, func(t *testing.T) {
			req := yoloRequest(workDir)
			req.NativeArgs = []string{"-c", key + "=x"}
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "-c", key + "=x"}
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
			}
		})
	}
	t.Run("and in equals form", func(t *testing.T) {
		req := yoloRequest(workDir)
		req.NativeArgs = []string{"--config=sandbox_mode=read-only"}
		plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
		want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "--config=sandbox_mode=read-only"}
		if !reflect.DeepEqual(plan.Argv, want) {
			t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
		}
	})
}

// Long flags are exact and shorts are case-sensitive: `--configx` is
// an unknown long (not `--config` with an attached value) and `-C` is
// the workdir flag (not `-c`), so both forward verbatim instead of
// entering the override classifier.
func TestLongFlagsAreExactAndShortsAreCaseSensitive(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	for _, c := range []struct {
		name string
		args []string
	}{
		{"a longer long", []string{"--configx", "k=v"}},
		{"the capital short", []string{"-C", "/tmp/work"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := yoloRequest(workDir)
			req.NativeArgs = c.args
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag}, c.args...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
			}
		})
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
		req := yoloRequest(workDir)
		req.NativeArgs = args
		return req
	}
	t.Run("an unknown key after the separator is prompt, not policy", func(t *testing.T) {
		plan, _ := interactivePlan(t, yolo([]string{"--", "-c", "future_key=x"}), agentic.LaunchModeInteractive)
		want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "--", "-c", "future_key=x"}
		if !reflect.DeepEqual(plan.Argv, want) {
			t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
		}
		if err := interactivePlanError(t, yolo([]string{"-c", "future_key=x"})); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
			t.Fatalf("the flag-position twin was not refused: %v", err)
		}
	})
	t.Run("the bypass flag after the separator is prompt, not a duplicate", func(t *testing.T) {
		plan, _ := interactivePlan(t, yolo([]string{"--", bypassApprovalsAndSandboxFlag}), agentic.LaunchModeInteractive)
		want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "--", bypassApprovalsAndSandboxFlag}
		if !reflect.DeepEqual(plan.Argv, want) {
			t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
		}
		if err := interactivePlanError(t, yolo([]string{bypassApprovalsAndSandboxFlag})); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
			t.Fatalf("the flag-position twin was not refused as a duplicate: %v", err)
		}
	})
	t.Run("a positional that never led with a dash is prompt", func(t *testing.T) {
		plan, _ := interactivePlan(t, yolo([]string{"future_key=x"}), agentic.LaunchModeInteractive)
		want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "future_key=x"}
		if !reflect.DeepEqual(plan.Argv, want) {
			t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
		}
	})
	t.Run("and a lone dash is positional too", func(t *testing.T) {
		plan, _ := interactivePlan(t, yolo([]string{"-"}), agentic.LaunchModeInteractive)
		want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag, "-"}
		if !reflect.DeepEqual(plan.Argv, want) {
			t.Fatalf("Argv = %#v, want %#v", plan.Argv, want)
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
		{"at a non-zero index", []string{"--search", bypassApprovalsAndSandboxFlag}},
		{"in equals form", []string{bypassApprovalsAndSandboxFlag + "=true"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := yoloRequest(workDir)
			req.NativeArgs = c.args
			if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
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
	req := yoloRequest(workDir)
	req.NativeArgs = []string{"exec", "-c", "future_key=x"}
	if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
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
	req.NativeArgs = []string{"--search"}
	if err := planErrorIn(t, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("BuildPlan err = %v, want ErrNativeArgsNotInteractive", err)
	}
	if _, err := New().Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNativeArgsNotInteractive) {
		t.Fatalf("Argv err = %v, want ErrNativeArgsNotInteractive", err)
	}
}
