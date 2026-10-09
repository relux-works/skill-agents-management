package muse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// writeMuseHelpShellStub installs a binary answering --version with the
// fixed answer and --help by running helpBody shell, so help-bound tests
// can generate large or hanging answers without embedding megabytes in
// the fixture script.
func writeMuseHelpShellStub(t *testing.T, answer, helpBody string) []string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "muse")
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		"printf '%s\\n' '" + answer + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		helpBody + "\n" +
		"fi\n" +
		"exit 8\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the help-shell stub: %v", err)
	}
	return []string{"PATH=" + dir + ":/bin:/usr/bin"}
}

// writeMuseVersionShellStub installs a binary running versionBody shell
// for --version and exiting 8 otherwise.
func writeMuseVersionShellStub(t *testing.T, versionBody string) []string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "muse")
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		versionBody + "\n" +
		"fi\n" +
		"exit 8\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the version-shell stub: %v", err)
	}
	return []string{"PATH=" + dir + ":/bin:/usr/bin"}
}

func requireProbeAttempt(t *testing.T, err error, stage string, timeout, limited bool) *agentic.ProbeExecutionError {
	t.Helper()
	var attempt *agentic.ProbeExecutionError
	if !errors.As(err, &attempt) {
		t.Fatalf("probe err = %v, want a *ProbeExecutionError", err)
	}
	if attempt.Stage != stage || attempt.Timeout != timeout || attempt.OutputLimited != limited {
		t.Fatalf("attempt = %+v, want stage %q timeout %v limited %v", attempt, stage, timeout, limited)
	}
	if !attempt.ExecAttempted || !attempt.ChildStarted {
		t.Fatalf("attempt = %+v, want exec attempted and child started", attempt)
	}
	return attempt
}

// TestSealVersionProbeBoundsPipeDrain pins bounded drainage of the seal
// version re-probe: a descendant holding the stdout pipe past the parent
// exit still completes — fast when the holder is brief, through the
// deadline group kill when it is not — with the complete version bytes.
// Skipping the kill on version probes (the M36 mutant) turns the long
// holder into teardown-incomplete instead of a result.
func TestSealVersionProbeBoundsPipeDrain(t *testing.T) {
	t.Parallel()
	sealWithHolder := func(t *testing.T, hold string) {
		t.Helper()
		env := writeMuseVersionShellStub(t,
			"(sleep "+hold+" &)\nprintf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\nexit 0")
		plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			PermissionMode: agentic.PermissionModeNative,
		})
		seal, err := plan.ExportSeal()
		if err != nil {
			t.Fatalf("ExportSeal: %v", err)
		}
		if seal.Data.Sealed.Selectors[sealedReleaseKey] != "1.4.1-R4503.1" {
			t.Fatalf("sealed release = %q, want the complete drained answer",
				seal.Data.Sealed.Selectors[sealedReleaseKey])
		}
		if err := plan.VerifyBeforeExec(); err != nil {
			t.Fatalf("VerifyBeforeExec after drained probe: %v", err)
		}
	}
	t.Run("short-holder", func(t *testing.T) {
		t.Parallel()
		sealWithHolder(t, "2")
	})
	t.Run("long-holder", func(t *testing.T) {
		t.Parallel()
		sealWithHolder(t, "30")
	})
}

// TestSealVersionProbeCapsDuringRead pins the during-read stdout cap: a
// finite answer past 64 KiB refuses output-limited, and an answer that
// passes the cap and then holds the pipe open refuses output-limited
// too — the first byte past the cap terminates the probe; it never
// waits for EOF. Buffering first and checking after (the M37 mutant)
// still refuses the finite answer but waits out the execution deadline
// on the held pipe and reports timeout instead.
func TestSealVersionProbeCapsDuringRead(t *testing.T) {
	t.Parallel()
	t.Run("finite-over-cap", func(t *testing.T) {
		t.Parallel()
		env := writeMuseVersionShellStub(t, "printf '%70000s' ''\nexit 0")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			PermissionMode: agentic.PermissionModeNative,
		}, agentic.LaunchModeInteractive)
		requireProbeAttempt(t, err, agentic.ProbeStageVersion, false, true)
	})
	t.Run("over-cap-then-holds", func(t *testing.T) {
		t.Parallel()
		env := writeMuseVersionShellStub(t, "printf '%70000s' ''\nsleep 60")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			PermissionMode: agentic.PermissionModeNative,
		}, agentic.LaunchModeInteractive)
		requireProbeAttempt(t, err, agentic.ProbeStageVersion, false, true)
	})
}

// TestHelpProbeBounded pins the help probe bounds: a help text past the
// 1 MiB cap refuses output-limited, a hanging help refuses on the
// execution deadline, and both surface the typed attempt record. The
// cap-plus-one witness is the one member the M38 mutant admits — a
// help text it parses instead of refusing, which then fails as
// unsupported rather than output-limited — while the next byte up still
// refuses output-limited under it.
func TestHelpProbeBounded(t *testing.T) {
	t.Parallel()
	const answer = "Muse Code 1.4.1 (1.4.1-R4503.1)"
	t.Run("witness-over-cap", func(t *testing.T) {
		t.Parallel()
		env := writeMuseHelpShellStub(t, answer, "printf '%1048577s' ''\nexit 0")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
		}, agentic.LaunchModeInteractive)
		requireProbeAttempt(t, err, agentic.ProbeStageHelp, false, true)
	})
	t.Run("control-one-more-byte", func(t *testing.T) {
		t.Parallel()
		env := writeMuseHelpShellStub(t, answer, "printf '%1048578s' ''")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
		}, agentic.LaunchModeInteractive)
		requireProbeAttempt(t, err, agentic.ProbeStageHelp, false, true)
	})
	t.Run("hanging-help", func(t *testing.T) {
		t.Parallel()
		env := writeMuseHelpShellStub(t, answer, "sleep 60")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
		}, agentic.LaunchModeInteractive)
		requireProbeAttempt(t, err, agentic.ProbeStageHelp, true, false)
	})
}
