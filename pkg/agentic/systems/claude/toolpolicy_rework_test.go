package claude

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Every scalar help row is driven through production, rather than testing only
// prompt flags. Settings uses a real relative file whose name looks like a flag.
func TestAskUserQuestionScalarOwnership(t *testing.T) {
	var names []string
	for name, arity := range claudeOptionArities {
		if arity == optionRequired {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range []string{"--allowedTools=AskUserQuestion", "--disallowedTools=Read", "--settings=missing.json", "--"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				req := interactiveRequest(tempSlot(t))
				req.NativeArgs = []string{name, value, "--verbose"}
				if name == "--settings" {
					if err := os.WriteFile(filepath.Join(req.WorkDir, value), []byte(`{}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := planErrorFor(t, req, agentic.LaunchModeInteractive); err != nil {
					t.Fatalf("scalar value classified as policy: %v", err)
				}
				argv := argvFor(t, req, agentic.LaunchModeInteractive)
				want := append([]string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}, req.NativeArgs...)
				if !reflect.DeepEqual(argv, want) {
					t.Fatalf("scalar value classified as policy: got %q, want %q", argv, want)
				}
			})
		}
	}
}

func TestAskUserQuestionOptionalAndVariadicOwnership(t *testing.T) {
	for _, row := range []struct {
		name   string
		args   []string
		refuse bool
	}{
		{"optional_separate", []string{"--remote-control", "--allowedTools=AskUserQuestion"}, true},
		{"optional_attached", []string{"--remote-control=--allowedTools=AskUserQuestion"}, false},
		{"short_attached", []string{"-n--allowedTools=AskUserQuestion"}, false},
		{"combined_short", []string{"-cn--allowedTools=AskUserQuestion"}, false},
		{"variadic_first", []string{"--betas", "--allowedTools=AskUserQuestion"}, false},
		{"variadic_later", []string{"--betas", "test", "--allowedTools=AskUserQuestion"}, true},
		{"equals_stops", []string{"--betas=test", "--allowedTools=AskUserQuestion"}, true},
		{"separator_owned", []string{"--model", "--", "--allowedTools=AskUserQuestion"}, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.NativeArgs = row.args
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if row.refuse {
				if !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
					t.Fatalf("want ErrDeniedToolReEnabled, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("owned option value refused: %v", err)
			}
		})
	}
}

func TestAskUserQuestionSettingsPolicy(t *testing.T) {
	for _, posture := range []string{"native", "yolo"} {
		for _, form := range []string{"inline", "file", "equals_inline", "equals_file"} {
			for _, row := range []struct {
				name string
				body string
				deny bool
				kind agentic.SettingsPolicyFailureKind
			}{
				{name: "exact", body: `{"permissions":{"allow":["AskUserQuestion"]}}`, deny: true},
				{name: "qualified", body: `{"permissions":{"allow":["Read","AskUserQuestion(*)"]}}`, deny: true},
				{name: "empty_qualifier", body: `{"permissions":{"allow":["AskUserQuestion()"]}}`, deny: true},
				{name: "scoped_attempt", body: `{"permissions":{"allow":["AskUserQuestion(questions:*)"]}}`, deny: true},
				{name: "unrelated", body: `{"permissions":{"allow":["Read","askuserquestion","AskUserQuestionX","*","Ask*","mcp__server__*"]}}`},
				{name: "absent", body: `{}`},
				{name: "deny_only", body: `{"permissions":{"deny":["AskUserQuestion"]}}`},
				{name: "invalid_json", body: `{"permissions":}`, kind: agentic.SettingsPolicyInvalid},
				{name: "invalid_allow", body: `{"permissions":{"allow":"AskUserQuestion"}}`, kind: agentic.SettingsPolicyInvalid},
				{name: "invalid_member", body: `{"permissions":{"allow":[null]}}`, kind: agentic.SettingsPolicyInvalid},
				{name: "invalid_permissions", body: `{"permissions":null}`, kind: agentic.SettingsPolicyInvalid},
			} {
				t.Run(posture+"/"+form+"/"+row.name, func(t *testing.T) {
					req := interactiveRequest(tempSlot(t))
					if posture == "yolo" {
						req.PermissionMode = agentic.PermissionModeYolo
						req.ToolRelease = "2.1.261"
					}
					value := row.body
					if strings.HasSuffix(form, "file") {
						value = "settings.json"
						if err := os.WriteFile(filepath.Join(req.WorkDir, value), []byte(row.body), 0600); err != nil {
							t.Fatal(err)
						}
					}
					req.NativeArgs = []string{"--settings", value}
					if strings.HasPrefix(form, "equals") {
						req.NativeArgs = []string{"--settings=" + value}
					}
					err := planErrorFor(t, req, agentic.LaunchModeInteractive)
					if row.deny {
						var typed *agentic.DeniedToolReEnabledError
						if !errors.Is(err, agentic.ErrDeniedToolReEnabled) || !errors.As(err, &typed) || typed.Placement != agentic.NativePolicyPlacementSettings {
							t.Fatalf("want typed settings re-enable refusal, got %v", err)
						}
					} else if row.kind != "" {
						var typed *agentic.SettingsPolicyError
						if !errors.Is(err, agentic.ErrSettingsPolicy) || !errors.As(err, &typed) || typed.Kind != row.kind {
							t.Fatalf("want settings %s refusal, got %v", row.kind, err)
						}
					} else if err != nil {
						t.Fatalf("unrelated settings refused: %v", err)
					}
				})
			}
		}
	}
}

// TestAskUserQuestionSettingsECMAScriptRules pins ECMAScript trim on the
// settings surface: a BOM-prefixed allow rule refuses like the bare tool, a
// BOM-wrapped value still selects inline JSON (native Xss trims before the
// brace check), and U+0085 — which ECMAScript preserves — keeps its literal
// meaning on both paths. Every row drives BuildPlan in both postures.
func TestAskUserQuestionSettingsECMAScriptRules(t *testing.T) {
	denyBody := `{"permissions":{"allow":["AskUserQuestion"]}}`
	for _, posture := range []string{"native", "yolo"} {
		for _, row := range []struct {
			name  string
			value string
			want  string
		}{
			{name: "settings_BOM_rule_refused", value: "{\"permissions\":{\"allow\":[\"\ufeffAskUserQuestion\"]}}", want: "denied"},
			{name: "settings_NBSP_rule_refused", value: "{\"permissions\":{\"allow\":[\"\u00a0AskUserQuestion\"]}}", want: "denied"},
			{name: "settings_inline_BOM_wrapped_refused", value: "\ufeff" + denyBody + "\u00a0", want: "denied"},
			{name: "settings_NEL_rule_admitted", value: "{\"permissions\":{\"allow\":[\"\u0085AskUserQuestion\"]}}", want: "admitted"},
			{name: "settings_inline_NEL_wrapped_unreadable", value: "\u0085" + denyBody, want: "unreadable"},
		} {
			t.Run(posture+"/"+row.name, func(t *testing.T) {
				req := interactiveRequest(tempSlot(t))
				if posture == "yolo" {
					req.PermissionMode = agentic.PermissionModeYolo
					req.ToolRelease = "2.1.261"
				}
				req.NativeArgs = []string{"--settings", row.value}
				err := planErrorFor(t, req, agentic.LaunchModeInteractive)
				switch row.want {
				case "denied":
					var typed *agentic.DeniedToolReEnabledError
					if !errors.Is(err, agentic.ErrDeniedToolReEnabled) || !errors.As(err, &typed) || typed.Placement != agentic.NativePolicyPlacementSettings {
						t.Fatalf("want typed settings re-enable refusal for %q, got %v", row.value, err)
					}
					if typed.Tool != deniedToolAskUserQuestion {
						t.Fatalf("typed refusal tool = %q, want the trimmed %q", typed.Tool, deniedToolAskUserQuestion)
					}
				case "admitted":
					if err != nil {
						t.Fatalf("ECMAScript-literal settings rule refused: %v", err)
					}
				case "unreadable":
					var typed *agentic.SettingsPolicyError
					if !errors.Is(err, agentic.ErrSettingsPolicy) || !errors.As(err, &typed) || typed.Kind != agentic.SettingsPolicyUnreadable {
						t.Fatalf("want settings unreadable refusal for %q, got %v", row.value, err)
					}
				}
			})
		}
	}
}

func TestAskUserQuestionSettingsReadFailure(t *testing.T) {
	for _, name := range []string{"missing", "directory", "broken_symlink"} {
		t.Run(name, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			path := filepath.Join(req.WorkDir, "settings.json")
			if name == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if name == "broken_symlink" {
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			}
			req.NativeArgs = []string{"--settings", path}
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			var typed *agentic.SettingsPolicyError
			if !errors.As(err, &typed) || typed.Kind != agentic.SettingsPolicyUnreadable {
				t.Fatalf("want unreadable settings refusal, got %v", err)
			}
		})
	}
}

func TestAskUserQuestionSettingsPrecedence(t *testing.T) {
	denied := `{"permissions":{"allow":["AskUserQuestion"]}}`
	for _, row := range []struct {
		name   string
		args   []string
		denied bool
	}{
		{"last_safe", []string{"--settings", denied, "--settings={}"}, false},
		{"last_denied", []string{"--settings={}", "--settings", denied}, true},
		{"overwritten_missing", []string{"--settings=missing.json", "--settings={}"}, false},
		{"overwritten_invalid", []string{"--settings={not-json}", "--settings={}"}, false},
		{"prompt_text", []string{"--", "--settings", denied}, false},
		{"scalar_text", []string{"--append-system-prompt", "--settings=" + denied}, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.NativeArgs = row.args
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)
			if row.denied {
				if !errors.Is(err, agentic.ErrDeniedToolReEnabled) {
					t.Fatalf("want settings re-enable refusal: %v", err)
				}
			} else if err != nil {
				t.Fatalf("ineffective settings inspected: %v", err)
			}
		})
	}
}

// TestAskUserQuestionEagerSettingsAmbiguity is the T1 regression
// (repeat-of rev1/S1): the pinned native eager settings scan and the
// Commander-style option parse select the effective --settings source
// independently, and any disagreement refuses with the typed ambiguity kind
// before launch — regardless of the candidate content. Agreement controls
// pin the boundary: an agreed deny still refuses as a re-enable attempt, and
// agreed benign or absent sources pass. Every vector drives BuildPlan in both
// postures; the combined-short vector also drives the plugin directly,
// pinning the refusal as the plugin's.
func TestAskUserQuestionEagerSettingsAmbiguity(t *testing.T) {
	deny := `{"permissions":{"allow":["AskUserQuestion"]}}`
	benign := `{}`
	for _, posture := range []string{"native", "yolo"} {
		for _, row := range []struct {
			name string
			args []string
			want string
		}{
			{name: "eager_only_separate_cn", args: []string{"-cn", "--settings", deny}, want: "ambiguous"},
			{name: "eager_only_separate_pn", args: []string{"-pn", "--settings", deny}, want: "ambiguous"},
			{name: "eager_only_attached_cn", args: []string{"-cn", "--settings=" + deny}, want: "ambiguous"},
			{name: "eager_only_benign_cn", args: []string{"-cn", "--settings", benign}, want: "ambiguous"},
			{name: "last_differs_cn", args: []string{"--settings={}", "-cn", "--settings=" + deny}, want: "ambiguous"},
			{name: "last_differs_benign_wins_eager", args: []string{"--settings=" + deny, "-cn", "--settings=" + benign}, want: "ambiguous"},
			{name: "hidden_scalar_agent_id", args: []string{"--agent-id", "--settings", deny}, want: "ambiguous"},
			{name: "hidden_short_m", args: []string{"-m", "--settings", deny}, want: "ambiguous"},
			{name: "trailing_bare_settings", args: []string{"--settings"}, want: "ambiguous"},
			{name: "trailing_bare_after_valued", args: []string{"--settings={}", "--settings"}, want: "ambiguous"},
			{name: "agree_combined_none_short", args: []string{"-c", "--settings", deny}, want: "denied"},
			{name: "agree_benign", args: []string{"--settings", benign}, want: "admitted"},
			{name: "agree_absent_beside_combined", args: []string{"-cn", "value"}, want: "admitted"},
			{name: "agree_scalar_owned", args: []string{"--model", "--settings", deny}, want: "admitted"},
		} {
			t.Run(posture+"/"+row.name, func(t *testing.T) {
				req := interactiveRequest(tempSlot(t))
				if posture == "yolo" {
					req.PermissionMode = agentic.PermissionModeYolo
					req.ToolRelease = "2.1.261"
				}
				req.NativeArgs = row.args
				err := planErrorFor(t, req, agentic.LaunchModeInteractive)
				switch row.want {
				case "ambiguous":
					var typed *agentic.SettingsPolicyError
					if !errors.Is(err, agentic.ErrSettingsPolicy) || !errors.As(err, &typed) || typed.Kind != agentic.SettingsPolicyAmbiguous {
						t.Fatalf("want typed settings ambiguity refusal for %q, got %v", row.args, err)
					}
				case "denied":
					var typed *agentic.DeniedToolReEnabledError
					if !errors.Is(err, agentic.ErrDeniedToolReEnabled) || !errors.As(err, &typed) || typed.Placement != agentic.NativePolicyPlacementSettings {
						t.Fatalf("want typed settings re-enable refusal for %q, got %v", row.args, err)
					}
				case "admitted":
					if err != nil {
						t.Fatalf("agreed settings selection refused: %v", err)
					}
				}
				if row.name == "eager_only_separate_cn" && row.want == "ambiguous" {
					if _, direct := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(direct, agentic.ErrSettingsPolicy) {
						t.Fatalf("Argv err = %v, want ErrSettingsPolicy", direct)
					}
				}
			})
		}
	}
}

func TestAskUserQuestionGoldenAllPlanEntryPoints(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
		t.Run(mode.String(), func(t *testing.T) {
			req := parityRequest(tempSlot(t), parityPromptRunID, parityPromptTaskID)
			if mode == agentic.LaunchModeInteractive {
				req = interactiveRequest(req.WorkDir)
			} else {
				req.PromptPath = writePromptFile(t, req.WorkDir, parityPromptBody)
			}
			bin := tempSlot(t)
			writeStubExecutable(t, bin, executableName)
			req.Env = append(req.Env, "PATH="+bin)
			registry := agentic.NewRegistry()
			if err := registry.Register(New()); err != nil {
				t.Fatal(err)
			}
			want := []string{"-p", "--output-format", "json", "--model", parityModel, "--effort", parityEffort, bypassPermissionsFlag, disallowedToolsDenial}
			if mode == agentic.LaunchModeInteractive {
				want = []string{"--model", parityModel, "--effort", parityEffort, disallowedToolsDenial}
			}
			plain, err := agentic.BuildPlan(registry, req, mode)
			if err != nil {
				t.Fatal(err)
			}
			withEnv, err := agentic.BuildPlanWithEnvironment(registry, req, mode)
			if err != nil {
				t.Fatal(err)
			}
			for _, argv := range [][]string{plain.Argv, withEnv.Plan.Argv} {
				if !reflect.DeepEqual(argv, want) {
					t.Fatalf("golden argv: got %q, want %q", argv, want)
				}
			}
		})
	}
}

func TestAskUserQuestionRefusalsThroughEnvironment(t *testing.T) {
	for _, row := range []struct {
		name     string
		args     []string
		sentinel error
	}{
		{"allow_flag", []string{"--allowedTools=AskUserQuestion"}, agentic.ErrDeniedToolReEnabled},
		{"settings_allow", []string{"--settings", `{"permissions":{"allow":["AskUserQuestion"]}}`}, agentic.ErrDeniedToolReEnabled},
		{"settings_unreadable", []string{"--settings=missing.json"}, agentic.ErrSettingsPolicy},
		{"settings_invalid", []string{"--settings={invalid}"}, agentic.ErrSettingsPolicy},
	} {
		t.Run(row.name, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.NativeArgs = row.args
			bin := tempSlot(t)
			writeStubExecutable(t, bin, executableName)
			req.Env = append(req.Env, "PATH="+bin)
			registry := agentic.NewRegistry()
			if err := registry.Register(New()); err != nil {
				t.Fatal(err)
			}
			if _, err := agentic.BuildPlanWithEnvironment(registry, req, agentic.LaunchModeInteractive); !errors.Is(err, row.sentinel) {
				t.Fatalf("want environment-entry refusal %v, got %v", row.sentinel, err)
			}
		})
	}
}
