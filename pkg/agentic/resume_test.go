package agentic_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"
)

const resumeTestUUID = "11111111-2222-3333-4444-555555555555"

func resumeRegistry(t *testing.T) *agentic.Registry {
	t.Helper()
	r := agentic.NewRegistry()
	for _, system := range []agentic.System{claude.New(), codex.New(), muse.New(), qwen.New()} {
		if err := r.Register(system); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestResumeElevationNativeAndWrapper(t *testing.T) {
	r := resumeRegistry(t)
	for _, tc := range []struct {
		name                  string
		system                agentic.SystemID
		wrapper, native, tail []string
		kind                  agentic.ResumeKind
		identity              string
	}{
		{"claude-long", "claude-code", nil, []string{"--resume", resumeTestUUID, "--name", "hello world"}, []string{"--name", "hello world"}, agentic.ResumeClaudeUUID, resumeTestUUID},
		{"claude-short", "claude-code", nil, []string{"-r", resumeTestUUID}, []string{}, agentic.ResumeClaudeUUID, resumeTestUUID},
		{"claude-attached", "claude-code", nil, []string{"-r" + resumeTestUUID}, []string{}, agentic.ResumeClaudeUUID, resumeTestUUID},
		{"claude-equals", "claude-code", nil, []string{"--resume=" + resumeTestUUID}, []string{}, agentic.ResumeClaudeUUID, resumeTestUUID},
		{"codex-command", "codex", nil, []string{"--config", "key=resume", "resume", resumeTestUUID, "--search"}, []string{"--config", "key=resume", "--search"}, agentic.ResumeCodexThread, resumeTestUUID},
		{"codex-flag", "codex", nil, []string{"--resume", resumeTestUUID}, []string{}, agentic.ResumeCodexThread, resumeTestUUID},
		{"muse-command", "muse", nil, []string{"--model", "resume", "resume", "session_123", "--yolo"}, []string{"--model", "resume", "--yolo"}, agentic.ResumeMuseSession, "session_123"},
		{"muse-arity-after-command", "muse", nil, []string{"resume", "--provider", "echo", "session_123"}, []string{"--provider", "echo"}, agentic.ResumeMuseSession, "session_123"},
		{"codex-arity-after-command", "codex", nil, []string{"resume", "--config", "key=--resume", resumeTestUUID}, []string{"--config", "key=--resume"}, agentic.ResumeCodexThread, resumeTestUUID},
		{"muse-latest", "muse", nil, []string{"resume", "--last"}, []string{}, agentic.ResumeLatest, ""},
		{"codex-latest", "codex", nil, []string{"resume", "--last"}, []string{}, agentic.ResumeLatest, ""},
		{"wrapper-latest", "claude-code", []string{"resume"}, nil, []string{}, agentic.ResumeLatest, ""},
		{"wrapper-handle", "claude-code", []string{"resume", "SES-example"}, nil, []string{}, agentic.ResumeHandle, "SES-example"},
		{"wrapper-native", "claude-code", []string{"--resume", resumeTestUUID}, nil, []string{}, agentic.ResumeClaudeUUID, resumeTestUUID},
		{"new", "claude-code", nil, []string{"--append-system-prompt", "--resume", "--", "-r", "literal"}, []string{"--append-system-prompt", "--resume", "--", "-r", "literal"}, agentic.ResumeNew, ""},
		{"claude-latest", "claude-code", nil, []string{"--continue"}, []string{}, agentic.ResumeLatest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agentic.ElevateResumeIntent(r, tc.system, tc.wrapper, tc.native)
			if err != nil {
				t.Fatal(err)
			}
			if got.Intent.Kind != tc.kind || !reflect.DeepEqual(got.NativeArgs, tc.tail) {
				t.Fatalf("elevation changed intent or tail: %#v", got)
			}
			if tc.identity == "" {
				if got.Intent.Identity != nil {
					t.Fatal("unexpected identity")
				}
			} else if got.Intent.Identity == nil || *got.Intent.Identity != tc.identity {
				t.Fatal("wrong identity")
			}
			if len(tc.native) > 0 {
				tc.native[0] = "changed"
				if len(got.NativeArgs) > 0 && got.NativeArgs[0] == "changed" {
					t.Fatal("aliased tail")
				}
			}
		})
	}
}

func TestResumeElevationRefusals(t *testing.T) {
	r := resumeRegistry(t)
	for _, tc := range []struct {
		name            string
		system          agentic.SystemID
		wrapper, native []string
	}{
		{"picker", "claude-code", nil, []string{"--resume"}},
		{"picker-identity", "muse", nil, []string{"resume", "latest"}},
		{"bad-uuid", "claude-code", nil, []string{"-r", "not-a-uuid"}},
		{"duplicate", "claude-code", nil, []string{"-r", resumeTestUUID, "--resume", resumeTestUUID}},
		{"wrapper-conflict", "claude-code", []string{"resume"}, []string{"-r", resumeTestUUID}},
		{"bad-handle", "claude-code", []string{"resume", "bad"}, nil},
		{"bad-wrapper", "claude-code", []string{"resume", "SES-example", "extra"}, nil},
		{"combined", "claude-code", nil, []string{"-cr", resumeTestUUID}},
		{"fork", "claude-code", nil, []string{"--fork-session"}},
		{"new-id", "claude-code", nil, []string{"--session-id", resumeTestUUID}},
		{"codex-latest-conflict", "codex", nil, []string{"resume", "--last", resumeTestUUID}},
		{"muse-latest-conflict", "muse", nil, []string{"resume", "--last", "session_123"}},
		{"codex-picker", "codex", nil, []string{"resume"}},
		{"muse-picker", "muse", nil, []string{"resume"}},
		{"claude-unknown", "claude-code", nil, []string{"--unknown", "--resume", resumeTestUUID}},
		{"codex-unknown", "codex", nil, []string{"--unknown", "resume", resumeTestUUID}},
		{"muse-unknown", "muse", nil, []string{"--unknown", "resume", "session_123"}},
		{"provider", "absent", nil, nil},
		{"unsupported", "qwen-code", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentic.ElevateResumeIntent(r, tc.system, tc.wrapper, tc.native)
			if !errors.Is(err, agentic.ErrSessionResumeInvalid) {
				t.Fatalf("want session_resume_invalid, got %v", err)
			}
		})
	}
}

func TestResumeIntentValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		intent agentic.ResumeIntent
		kind   agentic.ResumeKind
	}{
		{"identity-new", agentic.ResumeIntent{Kind: agentic.ResumeNew, Identity: ptr(resumeTestUUID)}, agentic.ResumeClaudeUUID},
		{"identity-latest", agentic.ResumeIntent{Kind: agentic.ResumeLatest, Identity: ptr(resumeTestUUID)}, agentic.ResumeClaudeUUID},
		{"picker", agentic.ResumeIntent{Kind: agentic.ResumeMuseSession, Identity: ptr("latest")}, agentic.ResumeMuseSession},
		{"bad-native", agentic.ResumeIntent{Kind: agentic.ResumeClaudeUUID, Identity: ptr("not-a-uuid")}, agentic.ResumeClaudeUUID},
		{"missing", agentic.ResumeIntent{Kind: agentic.ResumeClaudeUUID}, agentic.ResumeClaudeUUID},
		{"wrong-provider", agentic.ResumeIntent{Kind: agentic.ResumeClaudeUUID, Identity: ptr(resumeTestUUID)}, agentic.ResumeMuseSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := agentic.ValidateResumeIntent(tc.intent, tc.kind); !errors.Is(err, agentic.ErrSessionResumeInvalid) {
				t.Fatalf("want session_resume_invalid, got %v", err)
			}
		})
	}
}
func ptr(value string) *string { return &value }

// Every option spelling documented by pinned codex-cli 0.159.0
// `codex resume --help` keeps its exact bytes in the native tail while the
// SESSION_ID elevates. Root-only spellings still refuse inside resume.
func TestResumeCodexSubcommandInventory(t *testing.T) {
	r := resumeRegistry(t)
	scalars := []string{"-c", "--config", "--enable", "--disable", "--remote", "--remote-auth-token-env", "-i", "--image", "-m", "--model", "--local-provider", "-p", "--profile", "-s", "--sandbox", "-C", "--cd", "--add-dir", "-a", "--ask-for-approval"}
	for _, opt := range scalars {
		t.Run("scalar-"+strings.TrimPrefix(strings.TrimPrefix(opt, "--"), "-"), func(t *testing.T) {
			native := []string{"resume", opt, "v", resumeTestUUID}
			got, err := agentic.ElevateResumeIntent(r, "codex", nil, native)
			if err != nil {
				t.Fatalf("resume scalar %q refused: %v", opt, err)
			}
			if got.Intent.Kind != agentic.ResumeCodexThread || got.Intent.Identity == nil || *got.Intent.Identity != resumeTestUUID {
				t.Fatalf("resume scalar %q lost the session identity: %#v", opt, got.Intent)
			}
			if !reflect.DeepEqual(got.NativeArgs, []string{opt, "v"}) {
				t.Fatalf("resume scalar %q changed tail bytes: %#v", opt, got.NativeArgs)
			}
		})
		if strings.HasPrefix(opt, "--") {
			t.Run("equals-"+strings.TrimPrefix(opt, "--"), func(t *testing.T) {
				native := []string{"resume", opt + "=v", resumeTestUUID}
				got, err := agentic.ElevateResumeIntent(r, "codex", nil, native)
				if err != nil {
					t.Fatalf("resume scalar %q refused: %v", opt, err)
				}
				if !reflect.DeepEqual(got.NativeArgs, []string{opt + "=v"}) {
					t.Fatalf("resume scalar %q changed tail bytes: %#v", opt, got.NativeArgs)
				}
			})
		} else {
			t.Run("attached-"+strings.TrimPrefix(opt, "-"), func(t *testing.T) {
				native := []string{"resume", opt + "v", resumeTestUUID}
				got, err := agentic.ElevateResumeIntent(r, "codex", nil, native)
				if err != nil {
					t.Fatalf("resume scalar %q refused: %v", opt, err)
				}
				if !reflect.DeepEqual(got.NativeArgs, []string{opt + "v"}) {
					t.Fatalf("resume scalar %q changed tail bytes: %#v", opt, got.NativeArgs)
				}
			})
		}
	}
	booleans := []string{"--all", "--include-non-interactive", "--strict-config", "--oss", "--approve-for-me", "--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust", "--worktree", "--search", "--no-alt-screen", "--no-daemon", "-h", "--help", "-V", "--version"}
	for _, opt := range booleans {
		t.Run(strings.TrimPrefix(strings.TrimPrefix(opt, "--"), "-"), func(t *testing.T) {
			native := []string{"resume", opt, resumeTestUUID}
			got, err := agentic.ElevateResumeIntent(r, "codex", nil, native)
			if err != nil {
				t.Fatalf("resume option %q refused: %v", opt, err)
			}
			if got.Intent.Kind != agentic.ResumeCodexThread || got.Intent.Identity == nil || *got.Intent.Identity != resumeTestUUID {
				t.Fatalf("resume option %q lost the session identity: %#v", opt, got.Intent)
			}
			if !reflect.DeepEqual(got.NativeArgs, []string{opt}) {
				t.Fatalf("resume option %q changed tail bytes: %#v", opt, got.NativeArgs)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		native []string
		tail   []string
	}{
		{"root-scalar-before-command", []string{"--remote-auth-token", "tok", "resume", resumeTestUUID}, []string{"--remote-auth-token", "tok"}},
		{"root-boolean-before-command", []string{"--yolo", "resume", resumeTestUUID}, []string{"--yolo"}},
		{"mixed-resume-options", []string{"resume", "--all", "--config", "key=value", "-iimg.png", resumeTestUUID, "--search"}, []string{"--all", "--config", "key=value", "-iimg.png", "--search"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agentic.ElevateResumeIntent(r, "codex", nil, tc.native)
			if err != nil {
				t.Fatal(err)
			}
			if got.Intent.Kind != agentic.ResumeCodexThread || got.Intent.Identity == nil || *got.Intent.Identity != resumeTestUUID {
				t.Fatalf("lost the session identity: %#v", got.Intent)
			}
			if !reflect.DeepEqual(got.NativeArgs, tc.tail) {
				t.Fatalf("changed tail bytes: %#v", got.NativeArgs)
			}
		})
	}
}

func TestResumeCodexSubcommandInventoryRefusals(t *testing.T) {
	r := resumeRegistry(t)
	for _, tc := range []struct {
		name   string
		native []string
	}{
		{"root-scalar-in-resume", []string{"resume", "--remote-auth-token", "tok", resumeTestUUID}},
		{"root-boolean-in-resume", []string{"resume", "--not-so-yolo", resumeTestUUID}},
		{"root-alias-in-resume", []string{"resume", "--yolo", resumeTestUUID}},
		{"unknown-in-resume", []string{"resume", "--bogus", resumeTestUUID}},
		{"attached-unknown-in-resume", []string{"resume", "--bogus=x", resumeTestUUID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentic.ElevateResumeIntent(r, "codex", nil, tc.native)
			if !errors.Is(err, agentic.ErrSessionResumeInvalid) {
				t.Fatalf("want session_resume_invalid, got %v", err)
			}
		})
	}
}

// Every root option spelling documented by pinned Muse Code 1.4.1
// `muse --help` keeps its exact bytes on either side of resume while the
// session reference elevates.
func TestResumeMuseRootInventory(t *testing.T) {
	r := resumeRegistry(t)
	scalars := []string{"--agents", "--provider", "--preset", "--model", "--reasoning-effort", "--base-url", "--image", "--workspace", "--worktree-base", "--worktree-existing", "--approval-mode", "--permission-profile", "--approval-judge", "--echo-delay-ms", "--sandbox-network"}
	for _, opt := range scalars {
		t.Run("scalar-"+strings.TrimPrefix(opt, "--"), func(t *testing.T) {
			native := []string{"resume", opt, "v", "session_123"}
			got, err := agentic.ElevateResumeIntent(r, "muse", nil, native)
			if err != nil {
				t.Fatalf("root scalar %q refused: %v", opt, err)
			}
			if got.Intent.Kind != agentic.ResumeMuseSession || got.Intent.Identity == nil || *got.Intent.Identity != "session_123" {
				t.Fatalf("root scalar %q lost the session identity: %#v", opt, got.Intent)
			}
			if !reflect.DeepEqual(got.NativeArgs, []string{opt, "v"}) {
				t.Fatalf("root scalar %q changed tail bytes: %#v", opt, got.NativeArgs)
			}
		})
		t.Run("equals-"+strings.TrimPrefix(opt, "--"), func(t *testing.T) {
			native := []string{opt + "=v", "resume", "session_123"}
			got, err := agentic.ElevateResumeIntent(r, "muse", nil, native)
			if err != nil {
				t.Fatalf("root scalar %q refused: %v", opt, err)
			}
			if !reflect.DeepEqual(got.NativeArgs, []string{opt + "=v"}) {
				t.Fatalf("root scalar %q changed tail bytes: %#v", opt, got.NativeArgs)
			}
		})
	}
	booleans := []string{"--help", "-h", "--version", "-V", "--parallel-tool-calls", "--no-parallel-tool-calls", "--subagent-worktree-isolation", "--no-session-log", "--yolo", "--trust-workspace", "--disable-approval", "--disable-sandbox", "--disable-write", "--disable-shell", "--enable-shell-tool"}
	for _, opt := range booleans {
		t.Run(strings.TrimPrefix(strings.TrimPrefix(opt, "--"), "-"), func(t *testing.T) {
			native := []string{opt, "resume", "session_123"}
			got, err := agentic.ElevateResumeIntent(r, "muse", nil, native)
			if err != nil {
				t.Fatalf("root option %q refused: %v", opt, err)
			}
			if !reflect.DeepEqual(got.NativeArgs, []string{opt}) {
				t.Fatalf("root option %q changed tail bytes: %#v", opt, got.NativeArgs)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		native []string
		tail   []string
	}{
		{"worktree-bare", []string{"resume", "-w", "session_123"}, []string{"-w"}},
		{"worktree-separate-mode", []string{"resume", "--worktree", "create", "session_123"}, []string{"--worktree", "create"}},
		{"worktree-equals-mode", []string{"--worktree=existing", "resume", "session_123"}, []string{"--worktree=existing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agentic.ElevateResumeIntent(r, "muse", nil, tc.native)
			if err != nil {
				t.Fatal(err)
			}
			if got.Intent.Kind != agentic.ResumeMuseSession || got.Intent.Identity == nil || *got.Intent.Identity != "session_123" {
				t.Fatalf("lost the session identity: %#v", got.Intent)
			}
			if !reflect.DeepEqual(got.NativeArgs, tc.tail) {
				t.Fatalf("changed tail bytes: %#v", got.NativeArgs)
			}
		})
	}
	for _, native := range [][]string{{"resume", "--bogus", "session_123"}, {"resume", "--bogus=x", "session_123"}} {
		if _, err := agentic.ElevateResumeIntent(r, "muse", nil, native); !errors.Is(err, agentic.ErrSessionResumeInvalid) {
			t.Fatalf("unknown resume-side option admitted: %v", err)
		}
	}
}

// `--` ends option parsing only. The first positional after it still binds
// the session identity, and a `--resume` selector after it stays literal.
func TestResumeSeparatorPositionalBinding(t *testing.T) {
	r := resumeRegistry(t)
	for _, tc := range []struct {
		name        string
		system      agentic.SystemID
		native      []string
		kind        agentic.ResumeKind
		identity    string
		tail        []string
		wantRefusal bool
	}{
		{"codex-separator-identity", "codex", []string{"resume", "--", resumeTestUUID}, agentic.ResumeCodexThread, resumeTestUUID, []string{"--"}, false},
		{"codex-separator-prompt", "codex", []string{"resume", resumeTestUUID, "--", "p1"}, agentic.ResumeCodexThread, resumeTestUUID, []string{"--", "p1"}, false},
		{"codex-separator-session-prompt", "codex", []string{"resume", "--", resumeTestUUID, "p1"}, agentic.ResumeCodexThread, resumeTestUUID, []string{"--", "p1"}, false},
		{"codex-separator-hides-command", "codex", []string{"--", "resume", resumeTestUUID}, agentic.ResumeNew, "", []string{"--", "resume", resumeTestUUID}, false},
		{"codex-separator-flag-literal", "codex", []string{"resume", resumeTestUUID, "--", "--resume"}, agentic.ResumeCodexThread, resumeTestUUID, []string{"--", "--resume"}, false},
		{"codex-separator-last-literal", "codex", []string{"resume", "--", "--last"}, "", "", nil, true},
		{"muse-separator-identity", "muse", []string{"resume", "--", "session_123"}, agentic.ResumeMuseSession, "session_123", []string{"--"}, false},
		{"muse-separator-hides-command", "muse", []string{"--", "resume", "session_123"}, agentic.ResumeNew, "", []string{"--", "resume", "session_123"}, false},
		{"muse-separator-last-literal", "muse", []string{"resume", "--", "--last"}, "", "", nil, true},
		{"claude-separator-literal", "claude-code", []string{"--", "--resume", resumeTestUUID}, agentic.ResumeNew, "", []string{"--", "--resume", resumeTestUUID}, false},
		{"claude-separator-second-literal", "claude-code", []string{"--resume", resumeTestUUID, "--", "--resume", resumeTestUUID}, agentic.ResumeClaudeUUID, resumeTestUUID, []string{"--", "--resume", resumeTestUUID}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agentic.ElevateResumeIntent(r, tc.system, nil, tc.native)
			if tc.wantRefusal {
				if !errors.Is(err, agentic.ErrSessionResumeInvalid) {
					t.Fatalf("want session_resume_invalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("separator identity not bound: %v", err)
			}
			if got.Intent.Kind != tc.kind || !reflect.DeepEqual(got.NativeArgs, tc.tail) {
				t.Fatalf("separator changed intent or tail: %#v", got)
			}
			if tc.identity == "" {
				if got.Intent.Identity != nil {
					t.Fatal("unexpected identity")
				}
			} else if got.Intent.Identity == nil || *got.Intent.Identity != tc.identity {
				t.Fatal("wrong identity")
			}
		})
	}
}

// Positional cardinality per pinned grammar: Codex binds [SESSION_ID]
// [PROMPT], Muse binds exactly one session reference, and Claude selectors
// are flag-only with inert positional prompt text.
func TestResumePositionalCardinality(t *testing.T) {
	r := resumeRegistry(t)
	for _, tc := range []struct {
		name        string
		system      agentic.SystemID
		native      []string
		kind        agentic.ResumeKind
		identity    string
		tail        []string
		wantRefusal bool
	}{
		{"muse-surplus-other", "muse", []string{"resume", "id_123", "other"}, "", "", nil, true},
		{"muse-surplus-repeat", "muse", []string{"resume", resumeTestUUID, "resume", "session_2"}, "", "", nil, true},
		{"muse-separator-surplus", "muse", []string{"resume", "session_123", "--", "extra"}, "", "", nil, true},
		{"muse-last-separator-conflict", "muse", []string{"resume", "--last", "--", "extra"}, "", "", nil, true},
		{"codex-prompt", "codex", []string{"resume", resumeTestUUID, "p1"}, agentic.ResumeCodexThread, resumeTestUUID, []string{"p1"}, false},
		{"codex-prompt-literal-resume", "codex", []string{"resume", resumeTestUUID, "resume"}, agentic.ResumeCodexThread, resumeTestUUID, []string{"resume"}, false},
		{"codex-surplus", "codex", []string{"resume", resumeTestUUID, "p1", "p2"}, "", "", nil, true},
		{"codex-separator-surplus", "codex", []string{"resume", resumeTestUUID, "--", "p1", "p2"}, "", "", nil, true},
		{"codex-last-separator-conflict", "codex", []string{"resume", "--last", "--", resumeTestUUID}, "", "", nil, true},
		{"claude-positional-inert", "claude-code", []string{"--resume", resumeTestUUID, "somestring"}, agentic.ResumeClaudeUUID, resumeTestUUID, []string{"somestring"}, false},
		{"claude-bare-positional", "claude-code", []string{"hello"}, agentic.ResumeNew, "", []string{"hello"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agentic.ElevateResumeIntent(r, tc.system, nil, tc.native)
			if tc.wantRefusal {
				if !errors.Is(err, agentic.ErrSessionResumeInvalid) {
					t.Fatalf("want session_resume_invalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Intent.Kind != tc.kind || !reflect.DeepEqual(got.NativeArgs, tc.tail) {
				t.Fatalf("cardinality changed intent or tail: %#v", got)
			}
			if tc.identity == "" {
				if got.Intent.Identity != nil {
					t.Fatal("unexpected identity")
				}
			} else if got.Intent.Identity == nil || *got.Intent.Identity != tc.identity {
				t.Fatal("wrong identity")
			}
		})
	}
}

func TestClaudeRestartExactInsertion(t *testing.T) {
	plugin := claude.New()
	elevated, err := agentic.ElevateResumeIntent(resumeRegistry(t), "claude-code", nil, []string{"--resume", resumeTestUUID, "--name", "demo", "--", "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	template, err := plugin.ExportRestartTemplate(elevated.NativeArgs)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.ValidateRestartTransformation(template, template.Data.Argv, nil); err != nil {
		t.Fatal(err)
	}
	argv := append([]string{"--resume", resumeTestUUID}, template.Data.Argv...)
	if err := plugin.ValidateRestartTransformation(template, argv, ptr(resumeTestUUID)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"second-insertion", append(append([]string{}, argv...), "--resume", resumeTestUUID)},
		{"edit", []string{"--resume", resumeTestUUID, "--name", "changed", "--", "prompt"}},
		{"missing", template.Data.Argv},
		{"wrong-id", append([]string{"--resume", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, template.Data.Argv...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := plugin.ValidateRestartTransformation(template, tc.argv, ptr(resumeTestUUID)); err == nil {
				t.Fatal("want restart refusal")
			}
		})
	}
}

func TestClaudeRestartTemplateRefusals(t *testing.T) {
	plugin := claude.New()
	if _, err := plugin.ExportRestartTemplate([]string{"--resume", resumeTestUUID}); err == nil {
		t.Fatal("selector left in restart argv")
	}
	if _, err := plugin.ExportRestartTemplate([]string{"--resume"}); err == nil {
		t.Fatal("picker template accepted")
	}
	template, err := plugin.ExportRestartTemplate([]string{"--name", "demo", "--", "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []string{"schema", "version", "flag", "negative", "past-end", "nil-argv", "owned-value", "after-separator", "bad-selected-id"} {
		t.Run(tc, func(t *testing.T) {
			bad := template
			bad.Data.Argv = append([]string{}, template.Data.Argv...)
			switch tc {
			case "schema":
				bad.Schema = "unknown"
			case "version":
				bad.SchemaVersion = "2.0.0"
			case "flag":
				bad.Data.IdentitySlot.Flag = "-r"
			case "negative":
				bad.Data.IdentitySlot.Index = -1
			case "past-end":
				bad.Data.IdentitySlot.Index = 99
			case "nil-argv":
				bad.Data.Argv = nil
			case "owned-value":
				bad.Data.IdentitySlot.Index = 1
			case "after-separator":
				bad.Data.IdentitySlot.Index = 3
			}
			selected := ptr(resumeTestUUID)
			if tc == "bad-selected-id" {
				selected = ptr("bad")
			}
			argv := append([]string{bad.Data.IdentitySlot.Flag, *selected}, bad.Data.Argv...)
			if tc == "owned-value" || tc == "after-separator" {
				i := bad.Data.IdentitySlot.Index
				argv = append(append(append([]string{}, bad.Data.Argv[:i]...), "--resume", *selected), bad.Data.Argv[i:]...)
			}
			if err := plugin.ValidateRestartTransformation(bad, argv, selected); err == nil {
				t.Fatal("want restart refusal")
			}
		})
	}
}

func TestClaudeRestartSlotOnNew(t *testing.T) {
	plugin := claude.New()
	template, err := plugin.ExportRestartTemplate([]string{"--name", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	template.Data.IdentitySlot.Flag = "-r"
	if err := plugin.ValidateRestartTransformation(template, template.Data.Argv, nil); err == nil {
		t.Fatal("invalid slot on new admitted")
	}
}

// Exercise the production dispatcher with values owned by scalar options and
// arbitrary suffix bytes after --. No token normalization is permitted.
func TestResumeTailByteOrderProperty(t *testing.T) {
	registry := resumeRegistry(t)
	for _, provider := range []struct {
		system agentic.SystemID
		scalar string
	}{
		{"claude-code", "--append-system-prompt"}, {"codex", "--config"}, {"muse", "--provider"},
	} {
		t.Run(string(provider.system), func(t *testing.T) {
			for i := 0; i < 128; i++ {
				token := string([]byte{byte(i), byte(255 - i)}) + " --resume=unchanged "
				native := []string{provider.scalar, "--resume", "--", "resume", token, "-r", "latest"}
				got, err := agentic.ElevateResumeIntent(registry, provider.system, []string{"resume"}, native)
				if err != nil {
					t.Fatal(err)
				}
				if got.Intent.Kind != agentic.ResumeLatest || !reflect.DeepEqual(got.NativeArgs, native) {
					t.Fatal("scalar ownership or tail byte order changed")
				}
			}
		})
	}
}

func TestClaudeRestartInsertionProperty(t *testing.T) {
	plugin := claude.New()
	for length := 0; length < 32; length++ {
		base := make([]string, length)
		for i := range base {
			base[i] = "prompt"
		}
		template, err := plugin.ExportRestartTemplate(base)
		if err != nil {
			t.Fatal(err)
		}
		for index := 0; index <= length; index++ {
			candidate := template
			candidate.Data.IdentitySlot.Index = index
			argv := append(append(append([]string{}, base[:index]...), "--resume", resumeTestUUID), base[index:]...)
			if err := plugin.ValidateRestartTransformation(candidate, argv, ptr(resumeTestUUID)); err != nil {
				t.Fatal(err)
			}
			duplicate := append(append([]string{}, argv...), "--resume", resumeTestUUID)
			if err := plugin.ValidateRestartTransformation(candidate, duplicate, ptr(resumeTestUUID)); err == nil {
				t.Fatal("second insertion accepted by property suite")
			}
		}
	}
}

// Attached names obey the pinned inventories before --; after it, they are
// positional data. Drive the public dispatcher rather than a tokenizer helper.
func TestResumeAttachedOptionInventory(t *testing.T) {
	r := resumeRegistry(t)
	for _, tc := range []struct {
		name                  string
		system                agentic.SystemID
		wrapper, native, tail []string
		kind                  agentic.ResumeKind
		identity              string
		refuses               bool
	}{
		{"claude-unknown", "claude-code", nil, []string{"--bogus=x", "--resume", resumeTestUUID}, nil, "", "", true},
		{"claude-wrapper-unknown", "claude-code", []string{"resume"}, []string{"--panel-unknown=x"}, nil, "", "", true},
		{"claude-known-value", "claude-code", nil, []string{"--name=a=b", "--resume", resumeTestUUID}, []string{"--name=a=b"}, agentic.ResumeClaudeUUID, resumeTestUUID, false},
		{"claude-owned-selector-value", "claude-code", nil, []string{"--name=--bogus=x", "--resume", resumeTestUUID}, []string{"--name=--bogus=x"}, agentic.ResumeClaudeUUID, resumeTestUUID, false},
		{"claude-boolean", "claude-code", nil, []string{"--bare=x", "--resume", resumeTestUUID}, nil, "", "", true},
		{"claude-selector-boolean", "claude-code", nil, []string{"--continue=x"}, nil, "", "", true},
		{"claude-post-separator", "claude-code", nil, []string{"--resume", resumeTestUUID, "--", "--x=y"}, []string{"--", "--x=y"}, agentic.ResumeClaudeUUID, resumeTestUUID, false},
		{"codex-unknown", "codex", nil, []string{"resume", "--bogus=x", resumeTestUUID}, nil, "", "", true},
		{"codex-root-unknown", "codex", nil, []string{"--bogus=x", "resume", resumeTestUUID}, nil, "", "", true},
		{"codex-known-value", "codex", nil, []string{"resume", "--config=a=b", resumeTestUUID}, []string{"--config=a=b"}, agentic.ResumeCodexThread, resumeTestUUID, false},
		{"codex-boolean", "codex", nil, []string{"resume", "--all=x", resumeTestUUID}, nil, "", "", true},
		{"codex-root-boolean", "codex", nil, []string{"--search=x", "resume", resumeTestUUID}, nil, "", "", true},
		{"codex-selector-boolean", "codex", nil, []string{"resume", "--last=x"}, nil, "", "", true},
		{"codex-post-separator", "codex", nil, []string{"resume", "--", resumeTestUUID, "--x=y"}, []string{"--", "--x=y"}, agentic.ResumeCodexThread, resumeTestUUID, false},
		{"muse-unknown", "muse", nil, []string{"resume", "--bogus=x", "session_123"}, nil, "", "", true},
		{"muse-root-unknown", "muse", nil, []string{"--bogus=x", "resume", "session_123"}, nil, "", "", true},
		{"muse-known-value", "muse", nil, []string{"resume", "--model=a=b", "session_123"}, []string{"--model=a=b"}, agentic.ResumeMuseSession, "session_123", false},
		{"muse-boolean", "muse", nil, []string{"resume", "--parallel-tool-calls=x", "session_123"}, nil, "", "", true},
		{"muse-root-boolean", "muse", nil, []string{"--yolo=x", "resume", "session_123"}, nil, "", "", true},
		{"muse-selector-boolean", "muse", nil, []string{"resume", "--last=x"}, nil, "", "", true},
		{"muse-post-separator", "muse", nil, []string{"--", "--x=y"}, []string{"--", "--x=y"}, agentic.ResumeNew, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agentic.ElevateResumeIntent(r, tc.system, tc.wrapper, tc.native)
			if tc.refuses {
				if !errors.Is(err, agentic.ErrSessionResumeInvalid) {
					t.Fatalf("want session_resume_invalid for attached option, got %#v, %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Intent.Kind != tc.kind || !reflect.DeepEqual(got.NativeArgs, tc.tail) {
				t.Fatalf("attached option changed intent or bytes: %#v", got)
			}
			if tc.identity == "" {
				if got.Intent.Identity != nil {
					t.Fatal("unexpected identity")
				}
			} else if got.Intent.Identity == nil || *got.Intent.Identity != tc.identity {
				t.Fatal("wrong identity")
			}
		})
	}
}
