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
// release or frozen build outside the qualified set of the form a mode emits
// refuses at plan time, before any launch surface exists (environments §7.9).
// 1.4.10, 1.4.11 and 1.4.13 are qualified for the exec form only (Curator
// records 49d69b1c, 142bbda9 and 44bb3326): their exec and dry-run plans
// carry the exec flag, and their interactive plans refuse. 1.4.8 and the
// skipped 1.4.12 qualify neither form. Without
// the fact none of these requests refuses as unqualified.
func TestMuseForeignPersonalContextRefusesUnqualifiedBuild(t *testing.T) {
	t.Parallel()
	_, stubEnv := writeMuseVersionHelpStub(t, "printf '%s\\n' 'Muse Code 1.4.8 (1.4.8-R6256.1)'\n", museYoloHelpFixture)
	cases := []struct {
		name, release, frozen string
		execQualified         bool
	}{
		{"release-1.4.8", "1.4.8", "", false},
		{"release-unknown", "", "", false},
		{"release-1.4.6", "1.4.6", "", false},
		{"frozen-build-other", "1.4.7", "1.4.7-R6102.1", false},
		{"release-1.4.10-exec-only", "1.4.10", "", true},
		{"release-1.4.11-exec-only", "1.4.11", "", true},
		{"frozen-1.4.10-other-build", "1.4.10", "1.4.10-R6428.1", false},
		{"frozen-build-of-another-row", "1.4.11", "1.4.10-R6427.1", false},
		{"release-1.4.13-exec-only", "1.4.13", "", true},
		{"frozen-1.4.13-other-build", "1.4.13", "1.4.13-R6997.1", false},
		{"release-1.4.12-skipped", "1.4.12", "", false},
		{"frozen-1.4.12-build", "1.4.12", "1.4.12-R6909.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
				home := t.TempDir()
				req := agentic.LaunchRequest{System: systemID, Model: agentic.Model{ID: "echo"}, WorkDir: t.TempDir(), PromptPath: "assignment", Home: home,
					Env: stubEnv, Context: managedMuseContext(home, true), ToolRelease: tc.release, FrozenToolBuild: tc.frozen}
				plan, err := tryBuildMusePlan(t, New(), req, mode)
				if tc.execQualified && mode != agentic.LaunchModeInteractive {
					if err != nil {
						t.Fatalf("%s with the fact on an exec-qualified build: %v", mode, err)
					}
					if activeMuseFlagCount(plan.Argv, museNoForeignContextFlag) != 1 {
						t.Fatalf("%s argv lacks the exec exclusion: %q", mode, plan.Argv)
					}
				} else if !errors.Is(err, ErrForeignContextExclusionUnqualified) {
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

// TestMuseForeignPersonalContextDirectCallersFollowTheForm: a direct plugin
// caller that skips BuildPlan gets the same per-form answer. Args refuses
// the interactive argv on an exec-only build and builds the exec argv;
// ChildEnv, which has no mode, admits a build some form qualifies and refuses
// one no form qualifies, including a frozen build no row names.
func TestMuseForeignPersonalContextDirectCallersFollowTheForm(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"1.4.10", "1.4.11", "1.4.13"} {
		home := t.TempDir()
		req := agentic.LaunchRequest{Model: agentic.Model{ID: "echo"}, Home: home, Context: managedMuseContext(home, true), ToolRelease: release}
		if got, err := Args(req, agentic.LaunchModeInteractive); !errors.Is(err, ErrForeignContextExclusionUnqualified) || got != nil {
			t.Fatalf("%s interactive Args = %q, err %v", release, got, err)
		}
		got, err := Args(req, agentic.LaunchModeExec)
		if err != nil || activeMuseFlagCount(got, museNoForeignContextFlag) != 1 {
			t.Fatalf("%s exec Args = %q, err %v", release, got, err)
		}
		if _, err := New().ChildEnv(nil, req); err != nil {
			t.Fatalf("%s ChildEnv refused an exec-qualified build: %v", release, err)
		}
	}
	for _, tc := range []struct{ release, frozen string }{{"1.4.8", ""}, {"1.4.12", ""}, {"1.4.10", "1.4.10-R6428.1"}, {"1.4.13", "1.4.13-R6909.1"}, {"1.4.7", "1.4.10-R6427.1"}} {
		home := t.TempDir()
		req := agentic.LaunchRequest{Model: agentic.Model{ID: "echo"}, Home: home, Context: managedMuseContext(home, true), ToolRelease: tc.release, FrozenToolBuild: tc.frozen}
		if _, err := New().ChildEnv(nil, req); !errors.Is(err, ErrForeignContextExclusionUnqualified) {
			t.Fatalf("ChildEnv %s/%s err = %v, want ErrForeignContextExclusionUnqualified", tc.release, tc.frozen, err)
		}
		req.Context = managedMuseContext(home, false)
		if _, err := New().ChildEnv(nil, req); err != nil {
			t.Fatalf("ChildEnv %s/%s without the fact: %v", tc.release, tc.frozen, err)
		}
	}
}

// TestMuseForeignPersonalContextFormTables: the seal asks the interactive
// table, where an exec-only build is absent, and an undeclared mode must be
// qualified in every form. BuildPlan's gate refuses an exec-only release
// before the seal runs, so this direct check is the witness for the seal's
// form choice.
func TestMuseForeignPersonalContextFormTables(t *testing.T) {
	t.Parallel()
	for _, row := range []struct{ release, build string }{{"1.4.10", "1.4.10-R6427.1"}, {"1.4.11", "1.4.11-R6667.1"}, {"1.4.13", "1.4.13-R6996.1"}} {
		if !foreignContextExclusionBuildQualified(exclusionFormExec, row.release, row.build) {
			t.Fatalf("%s missing from the exec table", row.build)
		}
		if foreignContextExclusionBuildQualified(exclusionFormInteractive, row.release, row.build) {
			t.Fatalf("%s admitted by the interactive table", row.build)
		}
	}
	for _, form := range []exclusionForm{exclusionFormExec, exclusionFormInteractive} {
		if !foreignContextExclusionBuildQualified(form, "1.4.7", "1.4.7-R6101.1") {
			t.Fatalf("1.4.7-R6101.1 missing from the %s table", form)
		}
		if foreignContextExclusionBuildQualified(form, "1.4.8", "1.4.8-R6256.1") {
			t.Fatalf("1.4.8 admitted by the %s table", form)
		}
		if foreignContextExclusionBuildQualified(form, "1.4.12", "1.4.12-R6909.1") {
			t.Fatalf("skipped 1.4.12 admitted by the %s table", form)
		}
	}
	home := t.TempDir()
	req := agentic.LaunchRequest{Home: home, Context: managedMuseContext(home, true), ToolRelease: "1.4.10"}
	if err := validateForeignContextExclusion(req, agentic.LaunchModeManagedSession); !errors.Is(err, ErrForeignContextExclusionUnqualified) {
		t.Fatalf("undeclared mode on an exec-only build err = %v", err)
	}
	req.ToolRelease = "1.4.7"
	if err := validateForeignContextExclusion(req, agentic.LaunchModeManagedSession); err != nil {
		t.Fatalf("undeclared mode on a both-form build: %v", err)
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
