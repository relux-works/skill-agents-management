package muse

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func writeMuseRootContext(t *testing.T, cfg, body string) string {
	t.Helper()
	path := filepath.Join(cfg, "muse", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMuseForeignPersonalContextGate(t *testing.T) {
	// Sequential because the ambient-only regression deliberately uses Setenv.
	cases := []struct {
		name  string
		setup func(*testing.T, string) []string
		want  bool
	}{
		{"valid", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"managed rules")
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, true},
		{"missing", func(t *testing.T, cfg string) []string { return []string{"XDG_CONFIG_HOME=" + cfg} }, false},
		{"empty", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, "")
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"prefix-only", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, curatorRootContextPrefix)
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"symlink", func(t *testing.T, cfg string) []string {
			target := writeMuseRootContext(t, t.TempDir(), curatorRootContextPrefix+"rules")
			if err := os.MkdirAll(filepath.Join(cfg, "muse"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(cfg, "muse", "AGENTS.md")); err != nil {
				t.Fatal(err)
			}
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"directory", func(t *testing.T, cfg string) []string {
			if err := os.MkdirAll(filepath.Join(cfg, "muse", "AGENTS.md"), 0o700); err != nil {
				t.Fatal(err)
			}
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"foreign-first-line", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, "# Personal rules\n"+curatorRootContextPrefix+"rules")
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"v3", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, "<!--\ncurator-root-context-v3\nrules")
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"crlf", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, "<!--\r\ncurator-root-context-v2\r\nrules")
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"unreadable", func(t *testing.T, cfg string) []string {
			path := writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			if file, err := os.Open(path); err == nil {
				_ = file.Close()
				t.Skip("this user can read mode-000 files")
			}
			return []string{"XDG_CONFIG_HOME=" + cfg}
		}, false},
		{"cfg-unset", func(t *testing.T, cfg string) []string { return nil }, false},
		{"cfg-empty", func(t *testing.T, cfg string) []string { return []string{"XDG_CONFIG_HOME="} }, false},
		{"cfg-relative", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			rel, err := filepath.Rel("/", cfg)
			if err != nil {
				t.Fatal(err)
			}
			return []string{"XDG_CONFIG_HOME=" + rel}
		}, false},
		{"ambient-only", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			t.Setenv("XDG_CONFIG_HOME", cfg)
			return nil
		}, false},
		{"effective-cfg-empty", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			return []string{"XDG_CONFIG_HOME=" + cfg, "XDG_CONFIG_HOME="}
		}, false},
		{"effective-cfg-valid", func(t *testing.T, cfg string) []string {
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			return []string{"XDG_CONFIG_HOME=relative", "XDG_CONFIG_HOME=" + cfg}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.setup(t, t.TempDir())
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
				t.Run(mode.String(), func(t *testing.T) {
					req := agentic.LaunchRequest{Model: agentic.Model{ID: "muse-spark-1.3-contributor"}, Effort: "high", WorkDir: t.TempDir(), PromptPath: "assignment", Env: env, ToolRelease: foreignPersonalContextRelease}
					want := []string{"--model", req.Model.ID, "--reasoning-effort", "high", "--workspace", req.WorkDir}
					if mode == agentic.LaunchModeInteractive {
						req.NativeArgs = []string{"--trust-workspace", "prompt"}
						if tc.want {
							want = append(want, "-c", "context.foreign_personal_skills=false")
						}
						want = append(want, req.NativeArgs...)
					} else {
						want = append([]string{"exec", "--json", "--yolo"}, want...)
						if tc.want {
							want = append(want, "--no-foreign-personal-context")
						}
						want = append(want, "--prompt-file", "assignment")
					}
					got, err := Args(req, mode)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("argv = %#v, want %#v", got, want)
					}
				})
			}
		})
	}
}

func TestMuseForeignPersonalContextReleaseGate(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"", "1.4.1", "1.4.2", "1.4.6", "1.4.8", "9.9.9", "1.4.7"} {
		t.Run(release, func(t *testing.T) {
			cfg := t.TempDir()
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeInteractive} {
				req := agentic.LaunchRequest{Model: agentic.Model{ID: "echo"}, Env: []string{"XDG_CONFIG_HOME=" + cfg}, ToolRelease: release}
				got, err := Args(req, mode)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"--provider", "echo"}
				if mode == agentic.LaunchModeExec {
					want = append([]string{"exec", "--json", "--yolo"}, want...)
				}
				if release == foreignPersonalContextRelease {
					if mode == agentic.LaunchModeExec {
						want = append(want, "--no-foreign-personal-context")
					} else {
						want = append(want, "-c", "context.foreign_personal_skills=false")
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s argv = %q, want %q", mode, got, want)
				}
			}
		})
	}
}

func TestMuseForeignPersonalContextNativePolicy(t *testing.T) {
	t.Parallel()
	_, stubEnv := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.7 (1.4.7-R6101.1)'\n", museYoloHelpFixture)
	cases := []struct {
		name    string
		args    []string
		refused bool
	}{
		{"exec-flag", []string{"--no-foreign-personal-context"}, true},
		{"exec-flag-equals", []string{"--no-foreign-personal-context=false"}, true},
		{"config-short", []string{"-c", "context.foreign_personal_skills=false"}, true},
		{"config-enable", []string{"-c", "context.foreign_personal_skills=true"}, true},
		{"config-short-equals", []string{"-c=context.foreign_personal_skills=false"}, true},
		{"config-attached", []string{"-ccontext.foreign_personal_skills=true"}, true},
		{"config-long", []string{"--config", "context.foreign_personal_skills=false"}, true},
		{"config-long-equals", []string{"--config=context.foreign_personal_skills=true"}, true},
		{"config-spaces", []string{"-c", " context.foreign_personal_skills = true"}, true},
		{"other-config", []string{"-c", "other=false"}, false},
		{"other-flag-value", []string{"--other=context.foreign_personal_skills=false"}, false},
		{"prompt-separator", []string{"--", "--no-foreign-personal-context", "-c", "context.foreign_personal_skills=true"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, permission := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
				for _, managed := range []bool{false, true} {
					cfg := t.TempDir()
					if managed {
						writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
					}
					req := agentic.LaunchRequest{Model: agentic.Model{ID: "echo"}, Env: []string{"XDG_CONFIG_HOME=" + cfg}, ToolRelease: foreignPersonalContextRelease, PermissionMode: permission, NativeArgs: tc.args}
					got, err := Args(req, agentic.LaunchModeInteractive)
					if managed && tc.refused {
						if !errors.Is(err, agentic.ErrNativePolicyConflict) || got != nil {
							t.Fatalf("managed=%v posture=%s argv=%q err=%v", managed, permission, got, err)
						}
						req.System = systemID
						req.Env = append(req.Env, stubEnv...)
						if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrNativePolicyConflict) {
							t.Fatalf("BuildPlan native conflict err=%v", err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got[len(got)-len(tc.args):], tc.args) {
							t.Fatalf("native suffix changed: %q", got)
						}
					}
				}
			}
		})
	}
}

func TestMuseForeignPersonalContextCuratorOverlayAndSeal(t *testing.T) {
	t.Parallel()
	for _, permission := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(permission), func(t *testing.T) {
			home := t.TempDir()
			cfg := filepath.Join(home, "config")
			writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
			_, env := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.7 (1.4.7-R6101.1)'\n", museYoloHelpFixture)
			context := validMuseTestContext()
			for _, name := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
				context.Env["XDG_"+name+"_HOME"] = filepath.Join(home, strings.ToLower(name))
			}
			req := agentic.LaunchRequest{System: systemID, Model: agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired}, Effort: "high", Home: home, WorkDir: t.TempDir(), Env: append(env, "XDG_CONFIG_HOME=relative"), Context: context, ToolRelease: foreignPersonalContextRelease, PermissionMode: permission, NativeArgs: []string{"prompt"}}
			if permission == agentic.PermissionModeYolo {
				context.Permissions.Mode = agentic.CuratorPermissionYolo
			}
			plan := buildMuseInteractivePlan(t, New(), req)
			want := []string{"--model", req.Model.ID, "--reasoning-effort", "high", "--workspace", req.WorkDir, "-c", "context.foreign_personal_skills=false"}
			if permission == agentic.PermissionModeYolo {
				want = append(want, "--yolo")
			}
			want = append(want, "prompt")
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("sealed argv = %q, want %q", plan.Argv, want)
			}
			if envValue(plan.Env, "XDG_CONFIG_HOME") != cfg {
				t.Fatalf("child CFG differs: %q", plan.Env)
			}
			sealed, err := plan.ExportSeal()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(sealed.Data.Sealed.Argv, want) {
				t.Fatalf("export omitted managed argv: %q", sealed.Data.Sealed.Argv)
			}
			imported, err := New().ImportSealedData(*sealed.Data.Sealed)
			if err != nil {
				t.Fatal(err)
			}
			if err := imported.VerifyBeforeExec(plan); err != nil {
				t.Fatal(err)
			}
			plan.Argv = append([]string(nil), plan.Argv...)
			plan.Argv[7] = "context.foreign_personal_skills=true"
			if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealArgvChanged) {
				t.Fatalf("argv override err=%v", err)
			}
		})
	}
}

func TestMuseForeignPersonalContextUnverifiedBuild(t *testing.T) {
	t.Parallel()
	cfg := t.TempDir()
	writeMuseRootContext(t, cfg, curatorRootContextPrefix+"rules")
	_, env := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.7 (1.4.7-R6102.1)'\n", museYoloHelpFixture)
	req := agentic.LaunchRequest{System: systemID, Model: agentic.Model{ID: "echo"}, Env: append(env, "XDG_CONFIG_HOME="+cfg), ToolRelease: foreignPersonalContextRelease}
	if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); !errors.Is(err, ErrMuseToolReleaseMismatch) {
		t.Fatalf("unverified build err=%v", err)
	}
	req.FrozenToolBuild = "1.4.7-R6102.1"
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeInteractive} {
		got, err := Args(req, mode)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"--provider", "echo"}
		if mode == agentic.LaunchModeExec {
			want = append([]string{"exec", "--json", "--yolo"}, want...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unverified frozen build argv=%q, want %q", got, want)
		}
	}
}
