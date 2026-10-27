package codex

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The real-binary qualification is .out/codex-0164-qualification.md. These
// tests prove planner/scanner behavior, not execution by a model or sandbox.
func TestCodex0164PermissionMappingAndArgv(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.PermissionMode{"", agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(mode), func(t *testing.T) {
			mapping, err := New().PermissionMapping("0.164.0", mode)
			if err != nil {
				t.Fatal(err)
			}
			wantFlag := ""
			if mode == agentic.PermissionModeYolo {
				wantFlag = bypassApprovalsAndSandboxFlag
			}
			if mapping.Grammar != agentic.PermissionGrammarV3 || mapping.Flag != wantFlag {
				t.Fatalf("mapping = %#v", mapping)
			}
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.PermissionMode = "0.164.0", mode
			req.NativeArgs = []string{"--search", "qualification prompt"}
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}
			if wantFlag != "" {
				want = append(want, wantFlag)
			}
			want = append(want, req.NativeArgs...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("argv = %q, want %q", plan.Argv, want)
			}
		})
	}
}

// Every existing V2 conflict/placement has a native forwarding control at
// 0.164.0; yolo refuses without returning partial argv. Old tests stay intact.
func TestCodex0164PolicyConflictMatrix(t *testing.T) {
	t.Parallel()
	for _, row := range knownCodexPolicyConflicts() {
		for _, prefix := range [][]string{nil, {"exec"}} {
			t.Run(row.name+"/"+stringJoinForTest(prefix), func(t *testing.T) {
				req := interactiveRequest(tempSlot(t))
				req.ToolRelease = "0.164.0"
				req.NativeArgs = append(append([]string{}, prefix...), row.args...)
				req.PermissionMode = agentic.PermissionModeNative
				plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
				want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}, req.NativeArgs...)
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("native argv = %q, want %q", plan.Argv, want)
				}
				req.PermissionMode = agentic.PermissionModeYolo
				err := interactivePlanError(t, req)
				if row.duplicate {
					if !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
						t.Fatalf("err = %v", err)
					}
				} else {
					assertCodexNativePolicyConflict(t, err, row.selector, row.placement)
				}
				argv, err := New().Argv(req, agentic.LaunchModeInteractive)
				if err == nil || len(argv) != 0 {
					t.Fatalf("refusal returned argv=%q, err=%v", argv, err)
				}
			})
		}
	}
}

func stringJoinForTest(prefix []string) string {
	if len(prefix) == 0 {
		return "root"
	}
	return prefix[0]
}

// Adjacent positive controls prevent an implementation rejecting every
// config override from satisfying unknown/malformed/ambiguous refusal cases.
func TestCodex0164ClosedOverrideGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args []string
		want error
	}{
		{[]string{"-c", `service_tier="priority"`}, nil},
		{[]string{"--config", `model_reasoning_effort="high"`}, nil},
		{[]string{"-cmcp_servers.docs.url=https://example.invalid"}, nil},
		{[]string{"-a", "on-request"}, agentic.ErrNativePolicyConflict},
		{[]string{"--ask-for-approval=never"}, agentic.ErrNativePolicyConflict},
		{[]string{"-s", "read-only"}, agentic.ErrNativePolicyConflict},
		{[]string{"--sandbox=workspace-write"}, agentic.ErrNativePolicyConflict},
		{[]string{"--yolo"}, agentic.ErrNativePolicyUnknown},
		{[]string{"--not-so-yolo"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-anever"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-awhatever"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-sdanger-full-access"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-s=workspace-write"}, agentic.ErrNativePolicyConflict},
		{[]string{"-a", "untrusted"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-a", "on-failure"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-a", "sometimes"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-s", "unrestricted"}, agentic.ErrNativePolicyUnknown},
		{[]string{"--sandbox="}, agentic.ErrNativePolicyUnknown},
		{[]string{"--sandbox", "--search"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-a"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-c"}, agentic.ErrNativePolicyUnknown},
		{[]string{"--config", "future_key=x"}, agentic.ErrNativePolicyUnknown},
		{[]string{"--config=future_key=x"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-cfuture_key=x"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-c", "noequals"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-c", "bad key=x"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-c", "approval_policy.granular.rules=true"}, agentic.ErrNativePolicyUnknown},
		{[]string{"-c", `approvals_reviewer="guardian_subagent"`}, agentic.ErrNativePolicyUnknown},
	}
	for _, c := range cases {
		t.Run(stringJoinForTest(c.args), func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.PermissionMode, req.NativeArgs = "0.164.0", agentic.PermissionModeYolo, c.args
			if c.want == nil {
				plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
				want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag}, c.args...)
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("argv = %q, want %q", plan.Argv, want)
				}
			} else if err := interactivePlanError(t, req); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// The 0.164.0 classifier recognizes commands only at proven positions. New,
// removed and unmodeled root options fail closed; prompt text is never moved.
func TestCodex0164Classification(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args          []string
		form          agentic.NonInteractiveForm
		indeterminate bool
	}{
		{[]string{"exec", "prompt"}, agentic.NonInteractiveFormExec, false},
		{[]string{"e", "prompt"}, agentic.NonInteractiveFormExec, false},
		{[]string{"--model=example", "exec"}, agentic.NonInteractiveFormExec, false},
		{[]string{"-a", "on-request", "exec"}, agentic.NonInteractiveFormExec, false},
		{[]string{"--model", "exec"}, agentic.NonInteractiveFormNone, false},
		{[]string{"--", "exec"}, agentic.NonInteractiveFormNone, false},
		{[]string{"--remote-auth-token", "exec"}, "", true},
		{[]string{"--remote-auth-token-env", "exec"}, "", true},
		{[]string{"--worktree", "exec"}, "", true},
		{[]string{"--no-daemon", "exec"}, "", true},
		{[]string{"--image", "one.png", "exec"}, "", true},
		{[]string{"--future-option", "exec"}, "", true},
	} {
		t.Run(stringJoinForTest(c.args), func(t *testing.T) {
			got, err := New().ClassifyNonInteractiveArgs("0.164.0", c.args)
			if c.indeterminate {
				if !errors.Is(err, agentic.ErrNativeArgsClassificationIndeterminate) || got.IsNonInteractive() {
					t.Fatalf("classification=%#v err=%v", got, err)
				}
				return
			}
			if err != nil || got.Form != c.form || got.Grammar != agentic.PermissionGrammarV3 {
				t.Fatalf("classification=%#v err=%v", got, err)
			}
		})
	}
}

// Qualification is exact, never a semver range: neighbouring versions receive
// no permission mapping or classification and cannot build a yolo plan.
func TestCodex0164NeighboursRemainUnverified(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"0.163.0", "0.164.1"} {
		for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
			if got, err := New().PermissionMapping(release, mode); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || got != (agentic.PermissionMapping{}) {
				t.Fatalf("%s mapping=%#v err=%v", release, got, err)
			}
		}
		if got, err := New().ClassifyNonInteractiveArgs(release, []string{"exec"}); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || got.IsNonInteractive() {
			t.Fatalf("%s classification=%#v err=%v", release, got, err)
		}
		req := interactiveRequest(tempSlot(t))
		req.ToolRelease, req.PermissionMode = release, agentic.PermissionModeYolo
		if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("%s err=%v", release, err)
		}
	}
}

// Inspection of native 0.164.0 plans reports explicit relaxations, keeps
// restrictive controls, and never treats malformed data as inspected. This does not claim native precedence or managed-policy coverage.
func TestCodex0164StoredPolicyInspection(t *testing.T) {
	for _, c := range []struct {
		name, config, selector string
		malformed              bool
	}{
		{"restrictive", "approval_policy = \"on-request\"\nsandbox_mode = \"read-only\"\n", "", false},
		{"no approvals", "approval_policy = \"never\"\n", "approval_policy", false},
		{"workspace writes", "sandbox_mode = \"workspace-write\"\n", "sandbox_mode", false},
		{"full access", "sandbox_mode = \"danger-full-access\"\n", "sandbox_mode", false},
		{"malformed", "approval_policy = [\n", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease = "0.164.0"
			if err := os.MkdirAll(req.Home, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(req.Home, "config.toml")
			if err := os.WriteFile(path, []byte(c.config), 0o600); err != nil {
				t.Fatal(err)
			}
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			report := agentic.InspectStoredPolicy(New(), plan)
			if c.malformed {
				if codexStoredPolicyContains(report.SourcesInspected, path) || !codexHasSourceIssue(report, path, agentic.StoredPolicySourceUnparseable) {
					t.Fatalf("report=%#v", report)
				}
				return
			}
			if !codexStoredPolicyContains(report.SourcesInspected, path) {
				t.Fatalf("report=%#v", report)
			}
			want := 0
			if c.selector != "" {
				want = 1
			}
			if len(report.Relaxations) != want {
				t.Fatalf("report=%#v", report)
			}
			if want == 1 && (report.Relaxations[0].Selector != c.selector || report.Relaxations[0].SourcePath != path) {
				t.Fatalf("report=%#v", report)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != c.config {
				t.Fatalf("inspection changed config: %q, %v", after, err)
			}
		})
	}
}

// A context-owned yolo selection uses the same V3 guard: its valid control
// emits one bypass, while unqualified policy forms refuse through BuildPlan.
func TestCodex0164ContextPolicyFormsReachBuildPlan(t *testing.T) {
	t.Parallel()
	for _, suffix := range [][]string{nil, {"--yolo"}, {"--not-so-yolo"}, {"-anever"}, {"-sdanger-full-access"}} {
		t.Run(stringJoinForTest(suffix), func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.NativeArgs = "0.164.0", suffix
			req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
			if len(suffix) == 0 {
				plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
				want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag}
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("argv=%q, want %q", plan.Argv, want)
				}
			} else if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
				t.Fatalf("err=%v, want ErrNativePolicyUnknown", err)
			}
		})
	}
}
