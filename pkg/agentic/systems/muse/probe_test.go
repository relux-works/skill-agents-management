package muse

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func writeMuseVersionStub(t *testing.T, body string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, executableName)
	// A version probe must never start a session or carry another argument.
	script := "#!/bin/sh\n[ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ] || exit 8\n" + body
	if err := execfixture.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, []string{"PATH=" + dir}
}

// writeMuseVersionHelpStub is the version stub plus a --help answer: the
// version branch runs body, the help branch requires the curated updater
// pin and prints help. Yolo plans map from help evidence, so a yolo
// BuildPlan against a version-only stub refuses before it proves
// anything about the release under test.
func writeMuseVersionHelpStub(t *testing.T, versionBody, help string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "muse")
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		"[ \"$MUSE_NO_AUTO_UPDATE\" = '1' ] || exit 13\n" +
		"printf '%s\\n' '" + help + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"[ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ] || exit 8\n" + versionBody
	if err := execfixture.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, []string{"PATH=" + dir}
}

// Drive the entire host sequence through public entry points. The fake binary
// prints the pinned version output; no real provider or session is executed.
//
// M-AG3 retired the release table: the two historical releases below are
// fixture values, not admission rows. What this name still pins is the
// probe-to-plan flow — a probed binary reaches the plan it was probed
// for — with yolo mapping from help evidence and the no-evidence public
// query reporting evidence-required for yolo.
func TestMuseProbeVerifiedReleasesReachInteractivePlans(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		release string
		output  string
	}{
		{"1.4.1", "Muse Code 1.4.1 (1.4.1-R4503.1)"},
		{"1.4.2", "Muse Code 1.4.2 (1.4.2-R4684.1)"},
	} {
		t.Run(tc.release, func(t *testing.T) {
			t.Parallel()
			binary, env := writeMuseVersionHelpStub(t, "printf '%s\\n' '"+tc.output+"'\n", museYoloHelpFixture)
			system := New()
			release, err := agentic.ProbeToolRelease(context.Background(), system, env)
			if err != nil || release != tc.release {
				t.Fatalf("ProbeToolRelease = (%q, %v), want (%q, nil)", release, err, tc.release)
			}
			nativeMapping, err := system.PermissionMapping(release, agentic.PermissionModeNative)
			if err != nil || nativeMapping.Grammar != agentic.PermissionGrammarV1 || nativeMapping.Flag != "" {
				t.Fatalf("PermissionMapping native = (%#v, %v), want grammar v1 with no flag", nativeMapping, err)
			}
			if _, err := system.PermissionMapping(release, agentic.PermissionModeYolo); !errors.Is(err, ErrMuseHelpEvidenceRequired) {
				t.Fatalf("PermissionMapping yolo err = %v, want ErrMuseHelpEvidenceRequired", err)
			}
			for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
				t.Run(string(mode), func(t *testing.T) {
					req := agentic.LaunchRequest{
						System: system.ID(), Model: agentic.Model{ID: "echo"},
						Env: env, ToolRelease: release, PermissionMode: mode,
					}
					plan := buildMuseInteractivePlan(t, system, req)
					if plan.Binary != binary {
						t.Fatalf("plan binary = %q, want probed binary %q", plan.Binary, binary)
					}
					if mode == agentic.PermissionModeYolo {
						if err := assertMuseYoloExactlyOnce(plan); err != nil {
							t.Fatal(err)
						}
					} else if err := assertMuseNativePosture(plan); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestMuseProbeUsesLaunchChildEnvironment(t *testing.T) {
	t.Parallel()
	_, env := writeMuseVersionHelpStub(t, `
[ "$MUSE_NO_AUTO_UPDATE" = 1 ] || exit 9
[ "$LANG" = C ] || exit 10
[ -n "$HOME" ] || exit 11
[ "${UNLISTED_VARIABLE+x}" != x ] || exit 12
printf '%s\n' 'Muse Code 1.4.2 (1.4.2-R4684.1)'
`, museYoloHelpFixture)
	env = append(env, "MUSE_NO_AUTO_UPDATE=0", "MUSE_NO_AUTO_UPDATE=false", "LANG=C", "HOME="+t.TempDir(), "UNLISTED_VARIABLE=discard")
	before := append([]string(nil), env...)
	release, err := agentic.ProbeToolRelease(context.Background(), New(), env)
	if err != nil || release != "1.4.2" {
		t.Fatalf("ProbeToolRelease child environment = (%q, %v), want (1.4.2, nil)", release, err)
	}
	for i := range env {
		if env[i] != before[i] {
			t.Fatal("probe modified the caller's environment")
		}
	}
	plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: release, PermissionMode: agentic.PermissionModeYolo,
	})
	if value, ok := paritycase.Lookup(plan.Env, museNoAutoUpdateEnv); !ok || value != "1" {
		t.Fatalf("plan auto-update pin = (%q, %v), want (1, true)", value, ok)
	}
}

// Detection is distinct from qualification. M-AG3 retired the release
// table, so "unknown" no longer refuses everywhere: native maps an
// unknown release verbatim, and a yolo plan with help evidence maps it
// too. What this name still pins is the refusal that survives without
// evidence — the public no-evidence yolo query reports
// evidence-required — plus the new grants beside it.
func TestMuseProbeUnknownReleasesRefusePermissionMapping(t *testing.T) {
	t.Parallel()
	system := New()
	for _, unknown := range []string{"1.4.0", "1.5.0"} {
		t.Run(unknown, func(t *testing.T) {
			t.Parallel()
			_, env := writeMuseVersionHelpStub(t,
				"printf '%s\\n' 'Muse Code "+unknown+" ("+unknown+"-R1.1)'\n",
				museYoloHelpFixture)
			release, err := agentic.ProbeToolRelease(context.Background(), system, env)
			if err != nil || release != unknown {
				t.Fatalf("ProbeToolRelease = (%q, %v), want (%q, nil)", release, err, unknown)
			}
			if mapping, err := system.PermissionMapping(release, agentic.PermissionModeYolo); !errors.Is(err, ErrMuseHelpEvidenceRequired) {
				t.Fatalf("PermissionMapping yolo = (%#v, %v), want ErrMuseHelpEvidenceRequired", mapping, err)
			}
			mapping, err := system.PermissionMapping(release, agentic.PermissionModeNative)
			if err != nil || mapping.Flag != "" || mapping.Grammar != agentic.PermissionGrammarV1 {
				t.Fatalf("PermissionMapping native = (%#v, %v), want verbatim grammar v1 with no flag", mapping, err)
			}
			plan := buildMuseInteractivePlan(t, system, agentic.LaunchRequest{
				System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
				ToolRelease: release, PermissionMode: agentic.PermissionModeYolo,
			})
			if err := assertMuseYoloExactlyOnce(plan); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMuseProbeFailuresRemainUndetected(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"unparsable", "echo 'not a version'\n"},
		{"wrong_product", "echo 'Other Code 1.4.2 (1.4.2-R4684.1)'\n"},
		{"short_triple", "echo 'Muse Code 1.4 (1.4-R4684.1)'\n"},
		{"mismatched_build", "echo 'Muse Code 1.4.1 (1.4.2-R4684.1)'\n"},
		{"missing_build", "echo 'Muse Code 1.4.2'\n"},
		{"invalid_revision", "echo 'Muse Code 1.4.2 (1.4.2-Rinvalid)'\n"},
		{"stderr_only", "echo 'Muse Code 1.4.2 (1.4.2-R4684.1)' >&2\n"},
		{"nonzero_exit", "echo 'Muse Code 1.4.2 (1.4.2-R4684.1)'\nexit 3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, env := writeMuseVersionStub(t, tc.body)
			requireMuseProbeUndetected(t, context.Background(), env)
		})
	}
	t.Run("missing_binary", func(t *testing.T) {
		requireMuseProbeUndetected(t, context.Background(), []string{"PATH=" + t.TempDir()})
	})
	t.Run("missing_path", func(t *testing.T) {
		requireMuseProbeUndetected(t, context.Background(), nil)
	})
	t.Run("cancelled_context", func(t *testing.T) {
		_, env := writeMuseVersionStub(t, "echo 'Muse Code 1.4.2 (1.4.2-R4684.1)'\n")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		requireMuseProbeUndetected(t, ctx, env)
	})
	t.Run("fired_deadline", func(t *testing.T) {
		gate := execfixture.NewGate(t)
		_, env := writeMuseVersionStub(t,
			"printf '%s\\n' 'Muse Code 1.4.2 (1.4.2-R4684.1)'\n"+gate.Command()+"\n")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		type result struct {
			release string
			err     error
		}
		resultCh := make(chan result, 1)
		go func() {
			release, err := agentic.ProbeToolRelease(signaledProbeDeadline{ctx}, New(), env)
			resultCh <- result{release, err}
		}()
		gate.Wait(t)
		cancel()
		got := <-resultCh
		var attempt *agentic.ProbeExecutionError
		if got.release != "" || !errors.Is(got.err, agentic.ErrToolReleaseUndetected) || !errors.As(got.err, &attempt) || !attempt.Timeout || !attempt.ChildStarted {
			t.Fatalf("ready child with fired deadline = (%q, %v), want started timeout and ErrToolReleaseUndetected", got.release, got.err)
		}
		if !strings.Contains(string(attempt.Stdout), "Muse Code 1.4.2 (1.4.2-R4684.1)") {
			t.Fatalf("deadline attempt sample = %q, want the valid answer the child printed before blocking", attempt.Stdout)
		}
	})
}

// Fire the caller's deadline only after the fixture is observably running.
// This implements Context's terminal Err contract without a scheduling race.
type signaledProbeDeadline struct{ context.Context }

// Keep cancellation propagation on this context's Err method, rather than
// exposing the embedded cancel context through its private Value key.
func (signaledProbeDeadline) Value(any) any { return nil }

func (c signaledProbeDeadline) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func requireMuseProbeUndetected(t *testing.T, ctx context.Context, env []string) {
	t.Helper()
	release, err := agentic.ProbeToolRelease(ctx, New(), env)
	if release != "" || !errors.Is(err, agentic.ErrToolReleaseUndetected) {
		t.Fatalf("ProbeToolRelease = (%q, %v), want empty release and ErrToolReleaseUndetected", release, err)
	}
}
