package codex

import (
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Each independently qualified exact release emits one bypass only for yolo, before an unchanged
// caller suffix. These are planner assertions, not native execution evidence.
func TestCodex0163ExactPermissionMapping(t *testing.T) {
	checkCodexExactPermissionMapping(t, "0.163.0")
}

func TestCodex0165ExactPermissionMapping(t *testing.T) {
	checkCodexExactPermissionMapping(t, "0.165.0")
}

func checkCodexExactPermissionMapping(t *testing.T, release string) {
	t.Helper()
	t.Parallel()
	for _, mode := range []agentic.PermissionMode{"", agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(mode), func(t *testing.T) {
			mapping, err := New().PermissionMapping(release, mode)
			flag := ""
			if mode == agentic.PermissionModeYolo {
				flag = bypassApprovalsAndSandboxFlag
			}
			if err != nil || mapping != (agentic.PermissionMapping{Grammar: agentic.PermissionGrammarV3, Flag: flag}) {
				t.Fatalf("mapping=%#v err=%v", mapping, err)
			}
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.PermissionMode = release, mode
			req.NativeArgs = []string{"--search", "--", "-anever", "--yolo"}
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}
			if flag != "" {
				want = append(want, flag)
			}
			want = append(want, req.NativeArgs...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("argv=%q want=%q", plan.Argv, want)
			}
		})
	}
}

// Each selector/placement refuses with its typed conflict or duplicate and
// no partial argv. Its native twin forwards identical bytes, including after exec.
func TestCodex0163PolicyConflictMatrix(t *testing.T) {
	checkCodexExactPolicyConflictMatrix(t, "0.163.0")
}

func TestCodex0165PolicyConflictMatrix(t *testing.T) {
	checkCodexExactPolicyConflictMatrix(t, "0.165.0")
}

func checkCodexExactPolicyConflictMatrix(t *testing.T, release string) {
	t.Helper()
	t.Parallel()
	for _, row := range knownCodexPolicyConflicts() {
		for _, prefix := range [][]string{nil, {"exec"}} {
			t.Run(row.name+"/"+stringJoinForTest(prefix), func(t *testing.T) {
				req := interactiveRequest(tempSlot(t))
				req.ToolRelease = release
				req.NativeArgs = append(append([]string{}, prefix...), row.args...)
				original := append([]string{}, req.NativeArgs...)
				req.PermissionMode = agentic.PermissionModeNative
				plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
				want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}, original...)
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("native argv=%q want=%q", plan.Argv, want)
				}
				req.PermissionMode = agentic.PermissionModeYolo
				err := interactivePlanError(t, req)
				if row.duplicate {
					if !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
						t.Fatalf("err=%v want duplicate", err)
					}
				} else {
					assertCodexNativePolicyConflict(t, err, row.selector, row.placement)
				}
				argv, err := New().Argv(req, agentic.LaunchModeInteractive)
				if err == nil || len(argv) != 0 || !reflect.DeepEqual(req.NativeArgs, original) {
					t.Fatalf("refusal argv=%q err=%v suffix=%q", argv, err, req.NativeArgs)
				}
			})
		}
	}
}

// V3 refuses unknown enums, ambiguous aliases, malformed keys and missing
// values at root/exec. Known policy keys conflict even with invalid TOML values:
// the scanner does not validate native TOML types or claim native precedence.
// Nearby valid non-policy overrides prevent a reject-all implementation passing.
func TestCodex0163ClosedPolicyForms(t *testing.T) {
	checkCodexExactClosedPolicyForms(t, "0.163.0")
}

func TestCodex0165ClosedPolicyForms(t *testing.T) {
	checkCodexExactClosedPolicyForms(t, "0.165.0")
}

func checkCodexExactClosedPolicyForms(t *testing.T, release string) {
	t.Helper()
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want error
	}{
		{"valid", []string{"--search"}, nil},
		{"valid-config", []string{"-c", `service_tier="priority"`}, nil},
		{"valid-attached", []string{`-cmodel_reasoning_effort="high"`}, nil},
		{"valid-equals", []string{`--config=service_tier="priority"`}, nil},
		{"approval", []string{"-a", "never"}, agentic.ErrNativePolicyConflict},
		{"sandbox", []string{"--sandbox=workspace-write"}, agentic.ErrNativePolicyConflict},
		{"invalid-enum", []string{"-a", "sometimes"}, agentic.ErrNativePolicyUnknown},
		{"unsupported-enum", []string{"-a", "untrusted"}, agentic.ErrNativePolicyUnknown},
		{"invalid-sandbox", []string{"--sandbox", "unrestricted"}, agentic.ErrNativePolicyUnknown},
		{"empty-value", []string{"--sandbox="}, agentic.ErrNativePolicyUnknown},
		{"missing-value", []string{"-a"}, agentic.ErrNativePolicyUnknown},
		{"flag-as-value", []string{"-s", "--search"}, agentic.ErrNativePolicyUnknown},
		{"alias", []string{"--yolo"}, agentic.ErrNativePolicyUnknown},
		{"hidden", []string{"--not-so-yolo"}, agentic.ErrNativePolicyUnknown},
		{"compact-approval", []string{"-anever"}, agentic.ErrNativePolicyUnknown},
		{"compact-sandbox", []string{"-sdanger-full-access"}, agentic.ErrNativePolicyUnknown},
		{"compact-equals", []string{"-a=never"}, agentic.ErrNativePolicyConflict},
		{"malformed-config", []string{"-c", "noequals"}, agentic.ErrNativePolicyUnknown},
		{"missing-config", []string{"--config"}, agentic.ErrNativePolicyUnknown},
		{"unknown-config", []string{"--config=future_policy=true"}, agentic.ErrNativePolicyUnknown},
		{"whitespace-key", []string{"-c", "sandbox_mode =true"}, agentic.ErrNativePolicyUnknown},
		{"nested-policy", []string{"-c", "approval_policy.granular.rules=true"}, agentic.ErrNativePolicyUnknown},
		{"escaped-value", []string{"-c", `approval_policy="ne\u0076er"`}, agentic.ErrNativePolicyConflict},
		{"wrong-type", []string{"--config=sandbox_mode=true"}, agentic.ErrNativePolicyConflict},
		{"wrong-type-attached", []string{"-capproval_policy=3"}, agentic.ErrNativePolicyConflict},
		{"duplicate", []string{bypassApprovalsAndSandboxFlag}, agentic.ErrPermissionModeDuplicate},
	}
	for _, c := range cases {
		for _, prefix := range [][]string{nil, {"exec"}} {
			t.Run(c.name+"/"+stringJoinForTest(prefix), func(t *testing.T) {
				req := interactiveRequest(tempSlot(t))
				req.ToolRelease, req.PermissionMode = release, agentic.PermissionModeYolo
				req.NativeArgs = append(append([]string{}, prefix...), c.args...)
				if c.want == nil {
					plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
					want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag}, req.NativeArgs...)
					if !reflect.DeepEqual(plan.Argv, want) {
						t.Fatalf("argv=%q want=%q", plan.Argv, want)
					}
				} else {
					if err := interactivePlanError(t, req); !errors.Is(err, c.want) {
						t.Fatalf("err=%v want=%v", err, c.want)
					}
					if argv, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, c.want) || len(argv) != 0 {
						t.Fatalf("refusal argv=%q err=%v", argv, err)
					}
				}
				// The exact same invalid or ambiguous input is data in native mode.
				req.PermissionMode = agentic.PermissionModeNative
				plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
				want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}, req.NativeArgs...)
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("native argv=%q want=%q", plan.Argv, want)
				}
			})
		}
	}
}

// Removed/unmodeled options must not turn a value into a command. Known option
// values and prompt data stay data; the historical remote-token row stays V2.
func TestCodex0163Classification(t *testing.T) {
	checkCodexExactClassification(t, "0.163.0")
}

func TestCodex0165Classification(t *testing.T) {
	checkCodexExactClassification(t, "0.165.0")
}

func checkCodexExactClassification(t *testing.T, release string) {
	t.Helper()
	t.Parallel()
	for _, c := range []struct {
		name          string
		args          []string
		form          agentic.NonInteractiveForm
		indeterminate bool
	}{
		{"exec", []string{"exec", "prompt"}, agentic.NonInteractiveFormExec, false},
		{"alias", []string{"e", "prompt"}, agentic.NonInteractiveFormExec, false},
		{"after-model", []string{"--model=example", "exec"}, agentic.NonInteractiveFormExec, false},
		{"model-value", []string{"--model", "exec"}, agentic.NonInteractiveFormNone, false},
		{"prompt", []string{"--", "exec"}, agentic.NonInteractiveFormNone, false},
		{"old-remote", []string{"--remote-auth-token", "exec"}, "", true},
		{"remote-env", []string{"--remote-auth-token-env", "exec"}, "", true},
		{"image", []string{"--image", "one.png", "exec"}, "", true},
		{"worktree", []string{"--worktree", "exec"}, "", true},
		{"no-daemon", []string{"--no-daemon", "exec"}, "", true},
		{"unknown", []string{"--future-option", "exec"}, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := New().ClassifyNonInteractiveArgs(release, c.args)
			if c.indeterminate {
				if !errors.Is(err, agentic.ErrNativeArgsClassificationIndeterminate) || got.IsNonInteractive() {
					t.Fatalf("classification=%#v err=%v", got, err)
				}
			} else if err != nil || got.Form != c.form || got.Grammar != agentic.PermissionGrammarV3 {
				t.Fatalf("classification=%#v err=%v", got, err)
			}
		})
	}
	got, err := New().ClassifyNonInteractiveArgs("0.153.2", []string{"--remote-auth-token", "placeholder", "exec"})
	if err != nil || got.Form != agentic.NonInteractiveFormExec || got.Grammar != agentic.PermissionGrammarV2 {
		t.Fatalf("historical classification=%#v err=%v", got, err)
	}
}

// Unqualified patch, prerelease and build-metadata strings receive no
// mapping/classification and no partial yolo argv. Native
// forwarding remains available independently of qualification.
func TestCodexUnqualifiedAndDriftedReleases(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"0.162.0", "0.163.1", "0.164.1", "0.165.1", "0.163.0-dev", "0.163.0+different", "0.165.0-dev", "0.165.0+different", "0.166.0", ""} {
		t.Run(quoteRelease(release), func(t *testing.T) {
			for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
				got, err := New().PermissionMapping(release, mode)
				if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || got != (agentic.PermissionMapping{}) {
					t.Fatalf("mapping=%#v err=%v", got, err)
				}
			}
			if got, err := New().ClassifyNonInteractiveArgs(release, []string{"exec"}); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || got.IsNonInteractive() {
				t.Fatalf("classification=%#v err=%v", got, err)
			}
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.PermissionMode = release, agentic.PermissionModeYolo
			if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("err=%v", err)
			}
			if argv, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || len(argv) != 0 {
				t.Fatalf("argv=%q err=%v", argv, err)
			}
			req.PermissionMode = agentic.PermissionModeNative
			req.NativeArgs = []string{"--yolo", "-c", "future_key=true", "--", "exec"}
			plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
			want := append([]string{"-m", parityModel, "-c", `model_reasoning_effort="high"`}, req.NativeArgs...)
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("native argv=%q want=%q", plan.Argv, want)
			}
		})
	}
}

// A context-owned selection follows the same exact V3 row: a valid plan emits
// one bypass, while unqualified aliases refuse without partial argv.
func TestCodex0163ContextPermissionSelection(t *testing.T) {
	checkCodexExactContextPermissionSelection(t, "0.163.0")
}

func TestCodex0165ContextPermissionSelection(t *testing.T) {
	checkCodexExactContextPermissionSelection(t, "0.165.0")
}

func checkCodexExactContextPermissionSelection(t *testing.T, release string) {
	t.Helper()
	t.Parallel()
	for _, suffix := range [][]string{nil, {"--yolo"}, {"--not-so-yolo"}, {"-anever"}, {"-sdanger-full-access"}} {
		t.Run(stringJoinForTest(suffix), func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.NativeArgs = release, suffix
			req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextPermission, Permission: &agentic.PermissionContext{Mode: agentic.PermissionModeYolo}}}
			if len(suffix) == 0 {
				plan, _ := interactivePlan(t, req, agentic.LaunchModeInteractive)
				want := []string{"-m", parityModel, "-c", `model_reasoning_effort="high"`, bypassApprovalsAndSandboxFlag}
				if !reflect.DeepEqual(plan.Argv, want) {
					t.Fatalf("argv=%q want=%q", plan.Argv, want)
				}
			} else {
				if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrNativePolicyUnknown) {
					t.Fatalf("err=%v", err)
				}
				if argv, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyUnknown) || len(argv) != 0 {
					t.Fatalf("argv=%q err=%v", argv, err)
				}
			}
		})
	}
}
