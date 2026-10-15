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

// managedMuseContext returns a valid v4 muse fragment rooted at home, with
// or without the root-context fact.
func managedMuseContext(home string, fact bool) *agentic.CuratorContext {
	context := validMuseTestContext()
	for _, name := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		context.Env["XDG_"+name+"_HOME"] = filepath.Join(home, strings.ToLower(name))
	}
	if fact {
		context.RootContext = &agentic.CuratorRootContext{Exclusive: true}
	}
	return context
}

// writeMuseRootContext materializes a root-context target. The exclusion
// decision must ignore it: only the typed fact counts.
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

// TestMuseForeignPersonalContextFollowsTheFact: the fact maps to exactly
// --no-foreign-personal-context on exec and dry-run and to the -c pair on
// the interactive form; its absence adds neither, even when a well-formed
// Curator root-context file sits at the managed target (environments §10.2:
// absence never permits filesystem inference).
func TestMuseForeignPersonalContextFollowsTheFact(t *testing.T) {
	for _, fact := range []bool{false, true} {
		for _, permission := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
				home := t.TempDir()
				writeMuseRootContext(t, filepath.Join(home, "config"), "<!--\ncurator-root-context-v2\n-->\nmanaged rules")
				req := agentic.LaunchRequest{Model: agentic.Model{ID: "muse-spark-1.3-contributor"}, Effort: "high", WorkDir: t.TempDir(), PromptPath: "assignment",
					Home: home, Context: managedMuseContext(home, fact), ToolRelease: "1.4.7"}
				want := []string{"--model", req.Model.ID, "--reasoning-effort", "high", "--workspace", req.WorkDir}
				if mode == agentic.LaunchModeInteractive {
					// A permission mode is an interactive-only input.
					req.PermissionMode = permission
					req.NativeArgs = []string{"--trust-workspace", "prompt"}
					if fact {
						want = append(want, "-c", "context.foreign_personal_skills=false")
					}
					if permission == agentic.PermissionModeYolo {
						want = append(want, "--yolo")
					}
					want = append(want, req.NativeArgs...)
				} else {
					want = append([]string{"exec", "--json", "--yolo"}, want...)
					if fact {
						want = append(want, "--no-foreign-personal-context")
					}
					want = append(want, "--prompt-file", "assignment")
				}
				got, err := Args(req, mode)
				if err != nil {
					t.Fatalf("fact=%v %s %s: %v", fact, permission, mode, err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("fact=%v %s %s argv = %q, want %q", fact, permission, mode, got, want)
				}
			}
		}
	}
	// No Curator context at all: an ambient root-context file never adds
	// an exclusion either. Sequential because of Setenv.
	cfg := t.TempDir()
	writeMuseRootContext(t, cfg, "<!--\ncurator-root-context-v2\n-->\nrules")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	got, err := Args(agentic.LaunchRequest{Model: agentic.Model{ID: "echo"}, Env: []string{"XDG_CONFIG_HOME=" + cfg}, ToolRelease: "1.4.7"}, agentic.LaunchModeExec)
	if err != nil || !reflect.DeepEqual(got, []string{"exec", "--json", "--yolo", "--provider", "echo"}) {
		t.Fatalf("unmanaged argv = %q, err %v", got, err)
	}
}

// TestMuseForeignPersonalContextRefusesUnqualifiedBuild: with the fact, a
// release or frozen build outside the qualified set refuses at plan time,
// before any launch surface exists (environments §7.9). 1.4.8 stays
// unqualified until Curator's P1-P4 replay passes. Without the fact the
// same requests do not refuse as unqualified.
func TestMuseForeignPersonalContextRefusesUnqualifiedBuild(t *testing.T) {
	t.Parallel()
	_, stubEnv := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.8 (1.4.8-R6256.1)'\n", museYoloHelpFixture)
	cases := []struct {
		name, release, frozen string
	}{
		{"release-1.4.8", "1.4.8", ""},
		{"release-unknown", "", ""},
		{"release-1.4.6", "1.4.6", ""},
		{"frozen-build-other", "1.4.7", "1.4.7-R6102.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
				home := t.TempDir()
				req := agentic.LaunchRequest{System: systemID, Model: agentic.Model{ID: "echo"}, WorkDir: t.TempDir(), PromptPath: "assignment", Home: home,
					Env: stubEnv, Context: managedMuseContext(home, true), ToolRelease: tc.release, FrozenToolBuild: tc.frozen}
				if _, err := tryBuildMusePlan(t, New(), req, mode); !errors.Is(err, ErrForeignContextExclusionUnqualified) {
					t.Fatalf("%s with the fact: err = %v, want ErrForeignContextExclusionUnqualified", mode, err)
				}
				req.Context = managedMuseContext(home, false)
				if _, err := tryBuildMusePlan(t, New(), req, mode); errors.Is(err, ErrForeignContextExclusionUnqualified) {
					t.Fatalf("%s without the fact refused as unqualified: %v", mode, err)
				}
			}
		})
	}
}

// TestMuseForeignPersonalContextSealRefusesUnqualifiedAttestedBuild: the
// caller claims the qualified release, but the binary attests another 1.4.7
// build; the interactive seal refuses rather than dropping the exclusion.
func TestMuseForeignPersonalContextSealRefusesUnqualifiedAttestedBuild(t *testing.T) {
	t.Parallel()
	_, env := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.7 (1.4.7-R6102.1)'\n", museYoloHelpFixture)
	home := t.TempDir()
	req := agentic.LaunchRequest{System: systemID, Model: agentic.Model{ID: "echo"}, Home: home, WorkDir: t.TempDir(), Env: env, Context: managedMuseContext(home, true), ToolRelease: "1.4.7"}
	if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); !errors.Is(err, ErrForeignContextExclusionUnqualified) {
		t.Fatalf("attested unqualified build err = %v", err)
	}
	req.Context = managedMuseContext(home, false)
	if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); err != nil {
		t.Fatalf("without the fact the same build must plan: %v", err)
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
				for _, fact := range []bool{false, true} {
					home := t.TempDir()
					context := managedMuseContext(home, fact)
					if permission == agentic.PermissionModeYolo {
						context.Permissions.Mode = agentic.CuratorPermissionYolo
					}
					req := agentic.LaunchRequest{Model: agentic.Model{ID: "echo"}, Home: home, Context: context, ToolRelease: "1.4.7", PermissionMode: permission, NativeArgs: tc.args}
					got, err := Args(req, agentic.LaunchModeInteractive)
					if fact && tc.refused {
						if !errors.Is(err, agentic.ErrNativePolicyConflict) || got != nil {
							t.Fatalf("fact=%v posture=%s argv=%q err=%v", fact, permission, got, err)
						}
						req.System = systemID
						req.WorkDir = t.TempDir()
						req.Env = stubEnv
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

// TestMuseForeignPersonalContextCuratorOverlayAndSeal: the sealed
// interactive plan carries the exclusion pair, the managed XDG homes and the
// fragment's TBH_CREDENTIAL_BACKEND=file over an ambient value, and the seal
// refuses a later argv or backend change.
func TestMuseForeignPersonalContextCuratorOverlayAndSeal(t *testing.T) {
	t.Parallel()
	for _, permission := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(permission), func(t *testing.T) {
			home := t.TempDir()
			_, env := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.7 (1.4.7-R6101.1)'\n", museYoloHelpFixture)
			context := managedMuseContext(home, true)
			if permission == agentic.PermissionModeYolo {
				context.Permissions.Mode = agentic.CuratorPermissionYolo
			}
			req := agentic.LaunchRequest{System: systemID, Model: agentic.Model{ID: parityModel, Effort: agentic.EffortSupportRequired}, Effort: "high", Home: home, WorkDir: t.TempDir(),
				Env: append(env, "XDG_CONFIG_HOME=relative", "TBH_CREDENTIAL_BACKEND=keychain"), Context: context, ToolRelease: "1.4.7", PermissionMode: permission, NativeArgs: []string{"prompt"}}
			plan := buildMuseInteractivePlan(t, New(), req)
			want := []string{"--model", req.Model.ID, "--reasoning-effort", "high", "--workspace", req.WorkDir, "-c", "context.foreign_personal_skills=false"}
			if permission == agentic.PermissionModeYolo {
				want = append(want, "--yolo")
			}
			want = append(want, "prompt")
			if !reflect.DeepEqual(plan.Argv, want) {
				t.Fatalf("sealed argv = %q, want %q", plan.Argv, want)
			}
			if envValue(plan.Env, "XDG_CONFIG_HOME") != filepath.Join(home, "config") {
				t.Fatalf("child CFG differs: %q", plan.Env)
			}
			if got := envValue(plan.Env, agentic.CuratorMuseCredentialBackendVariable); got != agentic.CuratorMuseCredentialBackendFile {
				t.Fatalf("child backend = %q, want the fragment's file", got)
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
			tampered := plan
			tampered.Argv = append([]string(nil), plan.Argv...)
			tampered.Argv[7] = "context.foreign_personal_skills=true"
			if err := imported.VerifyBeforeExec(tampered); !errors.Is(err, ErrMuseSealArgvChanged) {
				t.Fatalf("argv override err=%v", err)
			}
			tampered = plan
			tampered.Env = agentic.SetEnvValue(append([]string(nil), plan.Env...), agentic.CuratorMuseCredentialBackendVariable, "keychain")
			if err := imported.VerifyBeforeExec(tampered); err == nil {
				t.Fatal("seal admitted a changed credential backend")
			}
		})
	}
}
