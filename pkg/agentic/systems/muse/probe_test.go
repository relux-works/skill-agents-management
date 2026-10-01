package muse

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func writeMuseVersionStub(t *testing.T, body string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, executableName)
	// A version probe must never start a session or carry another argument.
	script := "#!/bin/sh\n[ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ] || exit 8\n" + body
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, []string{"PATH=" + dir}
}

// Drive the entire host sequence through public entry points. The fake binary
// prints the pinned version output; no real provider or session is executed.
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
			binary, env := writeMuseVersionStub(t, "printf '%s\\n' '"+tc.output+"'\n")
			system := New()
			release, err := agentic.ProbeToolRelease(context.Background(), system, env)
			if err != nil || release != tc.release {
				t.Fatalf("ProbeToolRelease = (%q, %v), want (%q, nil)", release, err, tc.release)
			}
			row, err := agentic.LookupReleaseCapability(verifiedReleases, release)
			if err != nil || row.Release != tc.release || row.Grammar != agentic.PermissionGrammarV1 || !row.YoloSupported {
				t.Fatalf("LookupReleaseCapability = (%#v, %v), want the verified release row", row, err)
			}
			for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
				t.Run(string(mode), func(t *testing.T) {
					mapping, err := system.PermissionMapping(release, mode)
					if err != nil || mapping.Grammar != agentic.PermissionGrammarV1 {
						t.Fatalf("PermissionMapping = (%#v, %v), want grammar v1", mapping, err)
					}
					wantFlag := ""
					if mode == agentic.PermissionModeYolo {
						wantFlag = museYoloFlag
					}
					if mapping.Flag != wantFlag {
						t.Fatalf("permission flag = %q, want %q", mapping.Flag, wantFlag)
					}
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
	_, env := writeMuseVersionStub(t, `
[ "$MUSE_NO_AUTO_UPDATE" = 1 ] || exit 9
[ "$LANG" = C ] || exit 10
[ -n "$HOME" ] || exit 11
[ "${UNLISTED_VARIABLE+x}" != x ] || exit 12
printf '%s\n' 'Muse Code 1.4.2 (1.4.2-R4684.1)'
`)
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

// Detection is distinct from qualification: a well-formed unknown release can
// be read, but cannot claim either permission mapping or a yolo launch plan.
func TestMuseProbeUnknownReleasesRefusePermissionMapping(t *testing.T) {
	t.Parallel()
	for _, unknown := range []string{"1.4.0", "1.5.0"} {
		t.Run(unknown, func(t *testing.T) {
			t.Parallel()
			_, env := writeMuseVersionStub(t, "printf '%s\\n' 'Muse Code "+unknown+" ("+unknown+"-R1.1)'\n")
			release, err := agentic.ProbeToolRelease(context.Background(), New(), env)
			if err != nil || release != unknown {
				t.Fatalf("ProbeToolRelease = (%q, %v), want (%q, nil)", release, err, unknown)
			}
			for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
				t.Run(string(mode), func(t *testing.T) {
					mapping, err := New().PermissionMapping(release, mode)
					if !errors.Is(err, agentic.ErrPermissionModeUnsupported) || !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
						t.Fatalf("PermissionMapping = (%#v, %v), want unsupported and unverified", mapping, err)
					}
				})
			}
			_, err = tryBuildMusePlan(t, New(), agentic.LaunchRequest{
				System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
				ToolRelease: release, PermissionMode: agentic.PermissionModeYolo,
			}, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrPermissionModeUnsupported) || !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("BuildPlan unknown release = %v, want unsupported and unverified", err)
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
		_, env := writeMuseVersionStub(t, "echo 'Muse Code 1.4.2 (1.4.2-R4684.1)'\nexec /bin/sleep 1\n")
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		requireMuseProbeUndetected(t, ctx, env)
	})
}

func requireMuseProbeUndetected(t *testing.T, ctx context.Context, env []string) {
	t.Helper()
	release, err := agentic.ProbeToolRelease(ctx, New(), env)
	if release != "" || !errors.Is(err, agentic.ErrToolReleaseUndetected) {
		t.Fatalf("ProbeToolRelease = (%q, %v), want empty release and ErrToolReleaseUndetected", release, err)
	}
}
