package muse

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// writeFrozenMuseFixture installs an executable frozen-candidate binary
// named muse-bin-<build> answering --version with answer and --help with
// help, and returns its path and lowercase hex SHA-256.
func writeFrozenMuseFixture(t *testing.T, dir, build, answer, help string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, "muse-bin-"+build)
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		"printf '%s\\n' '" + answer + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		"printf '%s\\n' '" + help + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"cat >/dev/null\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the frozen fixture: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the frozen fixture: %v", err)
	}
	sum := sha256.Sum256(raw)
	return path, hex.EncodeToString(sum[:])
}

// TestFrozenOverrideSelectsSuppliedCopy pins the frozen override grant: a
// valid tuple selects the supplied copy even when PATH resolves a
// different attested build, and the seal binds the frozen build.
func TestFrozenOverrideSelectsSuppliedCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	frozen, digest := writeFrozenMuseFixture(t, dir, "1.4.2-R4684.1",
		"Muse Code 1.4.2 (1.4.2-R4684.1)", museYoloHelpFixture)
	req := launchRequest(t, "")
	req.PermissionMode = agentic.PermissionModeNative
	req.ToolRelease = ""
	req.FrozenToolBinary = frozen
	req.FrozenToolBuild = "1.4.2-R4684.1"
	req.FrozenToolSHA256 = digest
	plan := buildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
	if plan.Binary != frozen {
		t.Fatalf("plan binary = %q, want the frozen copy %q", plan.Binary, frozen)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Sealed.Selectors[sealedReleaseKey] != "1.4.2-R4684.1" {
		t.Fatalf("sealed release = %q, want the frozen attested build",
			seal.Data.Sealed.Selectors[sealedReleaseKey])
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec on the frozen plan: %v", err)
	}
}

// TestFrozenOverrideGatesPresentButInvalid pins the frozen override gate:
// any present-but-invalid tuple refuses typed without falling back to
// PATH. Every case below runs with a VALID muse on PATH that would plan
// fine, so a fallback would succeed and fail the test.
func TestFrozenOverrideGatesPresentButInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	valid, validDigest := writeFrozenMuseFixture(t, dir, "1.4.2-R4684.1",
		"Muse Code 1.4.2 (1.4.2-R4684.1)", museYoloHelpFixture)
	symlink := filepath.Join(dir, "muse-bin-1.4.1-R4503.1")
	if err := os.Symlink(valid, symlink); err != nil {
		t.Fatalf("linking the symlink fixture: %v", err)
	}
	subdir := filepath.Join(dir, "muse-bin-1.4.3-R1.1")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("making the directory fixture: %v", err)
	}
	plain := filepath.Join(dir, "muse")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the plain-name fixture: %v", err)
	}
	plainRaw, err := os.ReadFile(plain)
	if err != nil {
		t.Fatalf("reading the plain-name fixture: %v", err)
	}
	plainSum := sha256.Sum256(plainRaw)
	nonExec := filepath.Join(dir, "muse-bin-1.4.4-R1.1")
	if err := os.WriteFile(nonExec, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("writing the non-executable fixture: %v", err)
	}
	nonExecRaw, err := os.ReadFile(nonExec)
	if err != nil {
		t.Fatalf("reading the non-executable fixture: %v", err)
	}
	nonExecSum := sha256.Sum256(nonExecRaw)
	// Execute-only: stats, is regular, carries an exec bit and names a
	// valid build, but its bytes cannot be hashed — except by a root
	// runner, which reads it and refuses on the digest instead. Either
	// way the tuple refuses typed without falling back to PATH.
	unreadable := filepath.Join(dir, "muse-bin-1.4.5-R1.1")
	if err := os.WriteFile(unreadable, []byte("#!/bin/sh\nexit 0\n"), 0o111); err != nil {
		t.Fatalf("writing the unreadable fixture: %v", err)
	}
	upperDigest := strings.ToUpper(validDigest)
	for _, tc := range []struct {
		name   string
		binary string
		build  string
		sha    string
	}{
		{"missing-file", filepath.Join(dir, "muse-bin-9.9.9-R1.1"), "9.9.9-R1.1", strings.Repeat("0", 64)},
		{"symlink", symlink, "1.4.1-R4503.1", validDigest},
		{"relative", "muse-bin-1.4.2-R4684.1", "1.4.2-R4684.1", validDigest},
		{"unclean", dir + "/sub/../muse-bin-1.4.2-R4684.1", "1.4.2-R4684.1", validDigest},
		{"directory", subdir, "1.4.3-R1.1", strings.Repeat("0", 64)},
		{"non-executable", nonExec, "1.4.4-R1.1", hex.EncodeToString(nonExecSum[:])},
		{"plain-filename", plain, "1.4.2-R4684.1", hex.EncodeToString(plainSum[:])},
		{"name-build-mismatch", valid, "1.4.1-R4503.1", validDigest},
		{"digest-mismatch", valid, "1.4.2-R4684.1", strings.Repeat("0", 64)},
		{"unreadable", unreadable, "1.4.5-R1.1", strings.Repeat("0", 64)},
		{"bad-sha-shape", valid, "1.4.2-R4684.1", "zzz"},
		{"uppercase-sha", valid, "1.4.2-R4684.1", upperDigest},
		{"partial-binary-only", valid, "", ""},
		{"partial-no-sha", valid, "1.4.2-R4684.1", ""},
		{"partial-no-binary", "", "1.4.2-R4684.1", validDigest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := launchRequest(t, "")
			req.PermissionMode = agentic.PermissionModeNative
			req.ToolRelease = ""
			req.FrozenToolBinary = tc.binary
			req.FrozenToolBuild = tc.build
			req.FrozenToolSHA256 = tc.sha
			if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); !errors.Is(err, ErrMuseFrozenToolInvalid) {
				t.Fatalf("BuildPlan with %s frozen tuple err = %v, want ErrMuseFrozenToolInvalid", tc.name, err)
			}
		})
	}
}
