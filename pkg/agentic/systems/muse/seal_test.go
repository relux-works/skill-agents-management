package muse

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// museSealWitnessStub is the fixed tampered-binary witness: it answers
// --version exactly like the fixture stub but its bytes differ, so a
// byte swap keeps the release while changing the digest. Its SHA-256 is
// pinned below and asserted in the test: the binary-hash narrowing mutant
// exempts exactly this digest, so the literal and the mutant must agree.
// sha256: 878df98063d8951e538cdf03ac1e5de0d4d600eb5ed50b5a33c8c5633ff62c41
const museSealWitnessStub = "#!/bin/sh\n" +
	"# witness: same version answer, different bytes\n" +
	"[ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ] || exit 8\n" +
	"printf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\n"

const museSealWitnessDigest = "878df98063d8951e538cdf03ac1e5de0d4d600eb5ed50b5a33c8c5633ff62c41"

// museSealedInteractiveRequest builds an interactive request whose parent
// environment carries XDG overrides alongside PATH, so the seal binds a
// non-trivial XDG identity.
func museSealedInteractiveRequest(t *testing.T) agentic.LaunchRequest {
	t.Helper()
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeNative
	req.ToolRelease = ""
	scratch := t.TempDir()
	req.Env = append(append([]string(nil), req.Env...),
		"XDG_CONFIG_HOME="+filepath.Join(scratch, "config"),
		"XDG_DATA_HOME="+filepath.Join(scratch, "data"),
		"XDG_CACHE_HOME="+filepath.Join(scratch, "cache"),
	)
	return req
}

func buildMuseSealedPlan(t *testing.T, req agentic.LaunchRequest) agentic.Plan {
	t.Helper()
	return buildMuseInteractivePlan(t, New(), req)
}

func requireMuseSealRefusal(t *testing.T, err error, sentinel error, what string) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("%s admitted: %v", what, err)
	}
}

// TestMuseInteractiveSealBindsBinaryArgvReleaseAndEnv drives the
// production seal entry point: BuildPlan seals, the untouched plan
// verifies, and the export binds the binary, exact argv, binary digest,
// verified full build and commitment-form XDG/pin selectors.
func TestMuseInteractiveSealBindsBinaryArgvReleaseAndEnv(t *testing.T) {
	req := museSealedInteractiveRequest(t)
	plan := buildMuseSealedPlan(t, req)
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("sealed plan refused itself: %v", err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Schema != agentic.ExecGuardSchema || seal.SchemaVersion != agentic.ExecGuardVersion {
		t.Fatalf("envelope = %q %q, want the exec-guard 1.0.0 schema", seal.Schema, seal.SchemaVersion)
	}
	if seal.Data.Kind != agentic.SealKindSealed || seal.Data.Sealed == nil {
		t.Fatalf("data kind = %q, want sealed with a payload", seal.Data.Kind)
	}
	data := seal.Data.Sealed
	if data.System != "muse" || data.Binary != plan.Binary || strings.Join(data.Argv, "\x00") != strings.Join(plan.Argv, "\x00") {
		t.Fatal("sealed payload does not bind the plan process")
	}
	if len(data.Artifacts) != 1 || data.Artifacts[0].Name != sealedMuseBinaryName || data.Artifacts[0].Path != plan.Binary {
		t.Fatalf("artifacts = %+v, want exactly the sealed binary", data.Artifacts)
	}
	raw, err := os.ReadFile(plan.Binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if want := sealedDigestPrefix + hex.EncodeToString(sum[:]); data.Artifacts[0].Digest != want {
		t.Fatalf("artifact digest = %q, want the binary SHA-256 %q", data.Artifacts[0].Digest, want)
	}
	if data.Selectors[sealedReleaseKey] != "1.4.1-R4503.1" {
		t.Fatalf("release selector = %q, want the probed full build 1.4.1-R4503.1", data.Selectors[sealedReleaseKey])
	}
	if data.Selectors[sealedKeyIDKey] != agentic.LocalCommitmentKeyID() {
		t.Fatalf("key id selector = %q, want the process commitment key id", data.Selectors[sealedKeyIDKey])
	}
	for _, entry := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", museNoAutoUpdateEnv} {
		want, _ := paritycase.Lookup(plan.Env, entry)
		if want == "" {
			t.Fatalf("plan env carries no %s to seal", entry)
		}
		if !isSealedCommitment(data.Selectors[entry]) {
			t.Fatalf("selector %q = %q, want a keyed commitment, never the literal value", entry, data.Selectors[entry])
		}
	}
	if len(data.Selectors) != 7 {
		t.Fatalf("selectors = %#v, want exactly the release, the mode, the key id and the four sealed env entries", data.Selectors)
	}
	if data.Selectors[sealedModeKey] != string(agentic.PermissionModeNative) {
		t.Fatalf("mode selector = %q, want the sealed native posture", data.Selectors[sealedModeKey])
	}
	if seal.HostedAdmissible() {
		t.Fatal("sealed Muse guard is hosted-admissible before closed sealed schemas land")
	}
}

// TestMuseInteractiveSealRefusesBinarySwap covers the three binary-swap
// shapes through plan.VerifyBeforeExec: a changed path, changed bytes
// under the same release, and a deleted binary.
func TestMuseInteractiveSealRefusesBinarySwap(t *testing.T) {
	sum := sha256.Sum256([]byte(museSealWitnessStub))
	if hex.EncodeToString(sum[:]) != museSealWitnessDigest {
		t.Fatalf("witness digest = %x, want the pinned %s the hash mutant exempts", sum, museSealWitnessDigest)
	}
	t.Run("path-changed", func(t *testing.T) {
		plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
		moved := plan
		moved.Binary += "-moved"
		requireMuseSealRefusal(t, moved.VerifyBeforeExec(), ErrMuseSealBinaryChanged, "moved binary path")
	})
	t.Run("bytes-swapped", func(t *testing.T) {
		plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
		if err := os.WriteFile(plan.Binary, []byte(museSealWitnessStub), 0o755); err != nil {
			t.Fatal(err)
		}
		requireMuseSealRefusal(t, plan.VerifyBeforeExec(), ErrMuseSealBinaryChanged, "swapped binary bytes")
	})
	t.Run("deleted", func(t *testing.T) {
		plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
		if err := os.Remove(plan.Binary); err != nil {
			t.Fatal(err)
		}
		requireMuseSealRefusal(t, plan.VerifyBeforeExec(), ErrMuseSealBinaryChanged, "deleted binary")
	})
	t.Run("unexecutable", func(t *testing.T) {
		plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
		// Readable but not executable: the byte hash still matches, so
		// only the release re-probe can refuse, and it refuses as
		// undetected rather than inventing a release.
		if err := os.Chmod(plan.Binary, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Fatalf("unexecutable binary admitted: %v", err)
		}
	})
}

// TestMuseInteractiveSealRefusesDuplicateSelectorsAtSeal sends duplicate
// XDG selectors through BuildPlan itself: establishment fails closed
// before any probe runs, even when the duplicates repeat one value.
func TestMuseInteractiveSealRefusesDuplicateSelectorsAtSeal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
	}{
		{name: "same-value", entry: "XDG_CONFIG_HOME"},
		{name: "other-selector", entry: "XDG_DATA_HOME"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := museSealedInteractiveRequest(t)
			for _, entry := range req.Env {
				if key, _, ok := strings.Cut(entry, "="); ok && key == tc.entry {
					req.Env = append(req.Env, entry)
				}
			}
			_, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("duplicate %s sealed: %v", tc.entry, err)
			}
		})
	}
}

// writeMuseSidecarStub installs a fixture binary whose version answer comes
// from a sidecar file while its own bytes stay fixed. It models the
// launcher-shim class the release re-probe exists for: same dispatch
// bytes, different effective release.
func writeMuseSidecarStub(t *testing.T, dir, answer string) (binary, sidecar string) {
	t.Helper()
	sidecar = filepath.Join(dir, "release.txt")
	if err := os.WriteFile(sidecar, []byte(answer), 0o644); err != nil {
		t.Fatal(err)
	}
	binary = filepath.Join(dir, executableName)
	script := "#!/bin/sh\n[ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ] || exit 8\ncat \"" + sidecar + "\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, sidecar
}

func museSidecarRequest(t *testing.T, dir string) agentic.LaunchRequest {
	t.Helper()
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeNative
	req.ToolRelease = ""
	// The sidecar script needs cat; the seal under test binds XDG_*/pin
	// only, so the extra PATH entries are invisible to it.
	req.Env = []string{"PATH=" + dir + ":/bin:/usr/bin"}
	return req
}

// TestMuseInteractiveSealRefusesReleaseMismatch swaps the effective
// release behind unchanged binary bytes and requires the release-typed
// refusal. The byte hash passes by construction, so only the re-probe
// stands between the swap and admission.
func TestMuseInteractiveSealRefusesReleaseMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
	}{
		{name: "to-1-4-2", answer: "Muse Code 1.4.2 (1.4.2-R4684.1)\n"},
		{name: "to-1-5-0", answer: "Muse Code 1.5.0 (1.5.0-R9999.1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, sidecar := writeMuseSidecarStub(t, dir, "Muse Code 1.4.1 (1.4.1-R4503.1)\n")
			plan := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			if err := os.WriteFile(sidecar, []byte(tc.answer), 0o644); err != nil {
				t.Fatal(err)
			}
			err := plan.VerifyBeforeExec()
			if !errors.Is(err, ErrMuseSealReleaseChanged) {
				t.Fatalf("same-bytes release swap admitted: %v", err)
			}
		})
	}
}

// TestMuseInteractiveSealRefusesUnverifiedRelease builds interactive
// native plans — no yolo mapping involved — against binaries that attest
// no well-formed build. Every one refuses typed at plan time: probe
// failures are undetected. No interactive plan ships unsealed.
//
// M-AG2 retired the verified-build list this name once pinned: novel
// well-formed builds now seal (see TestSealBindsAttestedNewest), and only
// undetected or malformed attestation refuses.
func TestMuseInteractiveSealRefusesUnverifiedRelease(t *testing.T) {
	refuseUnverified := func(t *testing.T, env []string, sentinel error, what string) {
		t.Helper()
		req := museInteractiveRequest(t)
		req.PermissionMode = agentic.PermissionModeNative
		req.ToolRelease = ""
		req.Env = env
		if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); !errors.Is(err, sentinel) {
			t.Fatalf("unverified %s admitted without a seal: %v", what, err)
		}
	}
	t.Run("native-silent", func(t *testing.T) {
		dir := t.TempDir()
		paritycase.WriteStubExecutable(t, dir, executableName)
		refuseUnverified(t, []string{"PATH=" + dir}, agentic.ErrToolReleaseUndetected, "silent")
	})
	t.Run("native-bad-output", func(t *testing.T) {
		_, env := writeMuseVersionStub(t, "printf '%s\\n' 'Other Code 1.4.1 (1.4.1-R4503.1)'\n")
		refuseUnverified(t, env, agentic.ErrToolReleaseUndetected, "misattributed")
	})
}

// TestMuseInteractiveSealRefusesBuildRevisionDrift swaps the effective
// build behind unchanged binary bytes to an unpinned revision of the
// SAME triple and requires the release-typed refusal. The byte hash
// passes by construction and the triple is unchanged, so only the full
// build binding stands between the swap and admission.
func TestMuseInteractiveSealRefusesBuildRevisionDrift(t *testing.T) {
	dir := t.TempDir()
	_, sidecar := writeMuseSidecarStub(t, dir, "Muse Code 1.4.1 (1.4.1-R4503.1)\n")
	plan := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
	if err := os.WriteFile(sidecar, []byte("Muse Code 1.4.1 (1.4.1-R9999.1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyBeforeExec(); !errors.Is(err, ErrMuseSealReleaseChanged) {
		t.Fatalf("same-triple build drift admitted: %v", err)
	}
}

// TestMuseInteractiveSealRefusesArgvChange seals a yolo plan carrying the
// posture flag plus a caller trust suffix, then requires argv-typed
// refusals for every argv mutation through plan.VerifyBeforeExec.
func TestMuseInteractiveSealRefusesArgvChange(t *testing.T) {
	sealed := func(t *testing.T) agentic.Plan {
		t.Helper()
		req := museSealedInteractiveRequest(t)
		req.PermissionMode = agentic.PermissionModeYolo
		req.ToolRelease = verifiedMuseRelease
		req.NativeArgs = []string{"--trust-workspace", "open the current project"}
		plan := buildMuseSealedPlan(t, req)
		if err := assertMuseYoloExactlyOnce(plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	t.Run("appended", func(t *testing.T) {
		plan := sealed(t)
		changed := plan
		changed.Argv = append(append([]string(nil), plan.Argv...), "--extra")
		requireMuseSealRefusal(t, changed.VerifyBeforeExec(), ErrMuseSealArgvChanged, "appended argv element")
	})
	t.Run("removed", func(t *testing.T) {
		plan := sealed(t)
		changed := plan
		changed.Argv = append([]string(nil), plan.Argv[:len(plan.Argv)-1]...)
		requireMuseSealRefusal(t, changed.VerifyBeforeExec(), ErrMuseSealArgvChanged, "truncated argv")
	})
	t.Run("swapped", func(t *testing.T) {
		plan := sealed(t)
		changed := plan
		changed.Argv = append([]string(nil), plan.Argv...)
		changed.Argv[len(changed.Argv)-1] = "a different prompt"
		requireMuseSealRefusal(t, changed.VerifyBeforeExec(), ErrMuseSealArgvChanged, "swapped argv tail")
	})
	t.Run("posture-removed", func(t *testing.T) {
		plan := sealed(t)
		changed := plan
		changed.Argv = nil
		for _, arg := range plan.Argv {
			if arg == museYoloFlag {
				continue
			}
			changed.Argv = append(changed.Argv, arg)
		}
		requireMuseSealRefusal(t, changed.VerifyBeforeExec(), ErrMuseSealArgvChanged, "removed posture flag")
	})
	t.Run("trust-removed", func(t *testing.T) {
		plan := sealed(t)
		changed := plan
		changed.Argv = nil
		for _, arg := range plan.Argv {
			if arg == "--trust-workspace" {
				continue
			}
			changed.Argv = append(changed.Argv, arg)
		}
		requireMuseSealRefusal(t, changed.VerifyBeforeExec(), ErrMuseSealArgvChanged, "removed trust suffix")
	})
}

// TestMuseInteractiveSealRefusesEnvChange mutates the sealed XDG/pin
// selectors through plan.VerifyBeforeExec: changed values, appeared and
// disappeared selectors, an unpinned updater, and duplicates that repeat
// the sealed value.
func TestMuseInteractiveSealRefusesEnvChange(t *testing.T) {
	sealed := func(t *testing.T) agentic.Plan {
		t.Helper()
		return buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	}
	setEnv := func(plan agentic.Plan, name, value string) agentic.Plan {
		changed := plan
		changed.Env = nil
		replaced := false
		for _, entry := range plan.Env {
			key, _, ok := strings.Cut(entry, "=")
			if ok && key == name {
				changed.Env = append(changed.Env, name+"="+value)
				replaced = true
				continue
			}
			changed.Env = append(changed.Env, entry)
		}
		if !replaced {
			changed.Env = append(changed.Env, name+"="+value)
		}
		return changed
	}
	unsetEnv := func(plan agentic.Plan, name string) agentic.Plan {
		changed := plan
		changed.Env = nil
		for _, entry := range plan.Env {
			if key, _, ok := strings.Cut(entry, "="); ok && key == name {
				continue
			}
			changed.Env = append(changed.Env, entry)
		}
		return changed
	}
	duplicateEnv := func(plan agentic.Plan, name string) agentic.Plan {
		changed := plan
		changed.Env = append([]string(nil), plan.Env...)
		for _, entry := range plan.Env {
			if key, _, ok := strings.Cut(entry, "="); ok && key == name {
				changed.Env = append(changed.Env, entry)
			}
		}
		return changed
	}
	t.Run("value-config-changed", func(t *testing.T) {
		plan := sealed(t)
		requireMuseSealRefusal(t, setEnv(plan, "XDG_CONFIG_HOME", "/elsewhere").VerifyBeforeExec(), ErrMuseSealEnvChanged, "changed XDG_CONFIG_HOME")
	})
	t.Run("value-cache-changed", func(t *testing.T) {
		plan := sealed(t)
		requireMuseSealRefusal(t, setEnv(plan, "XDG_CACHE_HOME", "/elsewhere").VerifyBeforeExec(), ErrMuseSealEnvChanged, "changed XDG_CACHE_HOME")
	})
	t.Run("pin-removed", func(t *testing.T) {
		plan := sealed(t)
		requireMuseSealRefusal(t, unsetEnv(plan, museNoAutoUpdateEnv).VerifyBeforeExec(), ErrMuseSealEnvChanged, "removed updater pin")
	})
	t.Run("pin-zeroed", func(t *testing.T) {
		plan := sealed(t)
		requireMuseSealRefusal(t, setEnv(plan, museNoAutoUpdateEnv, "0").VerifyBeforeExec(), ErrMuseSealEnvChanged, "zeroed updater pin")
	})
	t.Run("runtime-appeared", func(t *testing.T) {
		plan := sealed(t)
		requireMuseSealRefusal(t, setEnv(plan, "XDG_RUNTIME_DIR", "/run/muse").VerifyBeforeExec(), ErrMuseSealEnvChanged, "appeared XDG_RUNTIME_DIR")
	})
	t.Run("data-removed", func(t *testing.T) {
		plan := sealed(t)
		requireMuseSealRefusal(t, unsetEnv(plan, "XDG_DATA_HOME").VerifyBeforeExec(), ErrMuseSealEnvChanged, "removed XDG_DATA_HOME")
	})
	t.Run("dup-config-same", func(t *testing.T) {
		plan := sealed(t)
		err := duplicateEnv(plan, "XDG_CONFIG_HOME").VerifyBeforeExec()
		if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate XDG_CONFIG_HOME admitted: %v", err)
		}
	})
	t.Run("dup-data-same", func(t *testing.T) {
		plan := sealed(t)
		err := duplicateEnv(plan, "XDG_DATA_HOME").VerifyBeforeExec()
		if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate XDG_DATA_HOME admitted: %v", err)
		}
	})
	t.Run("dup-pin-same", func(t *testing.T) {
		plan := sealed(t)
		err := duplicateEnv(plan, museNoAutoUpdateEnv).VerifyBeforeExec()
		if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate updater pin admitted: %v", err)
		}
	})
	t.Run("dup-config-different", func(t *testing.T) {
		plan := sealed(t)
		changed := duplicateEnv(plan, "XDG_CONFIG_HOME")
		changed.Env = append(changed.Env, "XDG_CONFIG_HOME=/elsewhere")
		err := changed.VerifyBeforeExec()
		if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("conflicting XDG_CONFIG_HOME duplicate admitted: %v", err)
		}
	})
}

// TestMuseInteractiveSealRoundTripsThroughExportImport exports the seal,
// walks it through the closed JSON wire, reimports it, and requires the
// imported verifier to accept the untouched plan and to refuse every
// sealed-class mutation with the same typed errors.
func TestMuseInteractiveSealRoundTripsThroughExportImport(t *testing.T) {
	plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatalf("marshal seal: %v", err)
	}
	decoded, err := agentic.DecodeSeal(wire)
	if err != nil {
		t.Fatalf("DecodeSeal: %v", err)
	}
	verifier, err := agentic.ImportSeal(New(), decoded)
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	if err := verifier.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("imported verifier refused the untouched plan: %v", err)
	}
	moved := plan
	moved.Binary += "-moved"
	requireMuseSealRefusal(t, verifier.VerifyBeforeExec(moved), ErrMuseSealBinaryChanged, "imported moved binary path")
	extended := plan
	extended.Argv = append(append([]string(nil), plan.Argv...), "--extra")
	requireMuseSealRefusal(t, verifier.VerifyBeforeExec(extended), ErrMuseSealArgvChanged, "imported appended argv")
	recolored := plan
	recolored.Env = append(append([]string(nil), plan.Env...), "XDG_STATE_HOME=/elsewhere")
	requireMuseSealRefusal(t, verifier.VerifyBeforeExec(recolored), ErrMuseSealEnvChanged, "imported changed env")
	duplicated := plan
	duplicated.Env = append(append([]string(nil), plan.Env...), "XDG_DATA_HOME="+mustMuseEnvValue(t, plan.Env, "XDG_DATA_HOME"))
	requireMuseSealRefusal(t, verifier.VerifyBeforeExec(duplicated), ErrMuseSealEnvChanged, "imported duplicated env")
	// An imported verifier re-exports the same selectors: commitments
	// are already committed, so a second export must not wrap them again.
	exporter, ok := verifier.(agentic.ExecSealExporter)
	if !ok {
		t.Fatal("imported Muse verifier exports no sealed data")
	}
	again := exporter.ExportSealedData()
	if strings.Join(sortedMuseSelectorKeys(again.Selectors), "\x00") != strings.Join(sortedMuseSelectorKeys(decoded.Data.Sealed.Selectors), "\x00") {
		t.Fatalf("re-exported selectors = %#v, want the imported %#v", again.Selectors, decoded.Data.Sealed.Selectors)
	}
	for name, value := range decoded.Data.Sealed.Selectors {
		if again.Selectors[name] != value {
			t.Fatalf("re-exported selector %q = %q, want the imported %q", name, again.Selectors[name], value)
		}
	}
}

func sortedMuseSelectorKeys(selectors map[string]string) []string {
	keys := make([]string, 0, len(selectors))
	for name := range selectors {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}

func mustMuseEnvValue(t *testing.T, env []string, name string) string {
	t.Helper()
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && key == name {
			return value
		}
	}
	t.Fatalf("env carries no %s", name)
	return ""
}

// TestMuseInteractiveSealRoundTripsReleaseMismatch reimports a seal over
// a sidecar binary whose release changed after export: the imported
// verifier's re-probe must refuse release-typed, for a cross-triple swap
// and for a same-triple revision swap alike.
func TestMuseInteractiveSealRoundTripsReleaseMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
	}{
		{name: "to-1-4-2", answer: "Muse Code 1.4.2 (1.4.2-R4684.1)\n"},
		{name: "same-triple-R9999", answer: "Muse Code 1.4.1 (1.4.1-R9999.1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, sidecar := writeMuseSidecarStub(t, dir, "Muse Code 1.4.1 (1.4.1-R4503.1)\n")
			plan := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			seal, err := plan.ExportSeal()
			if err != nil {
				t.Fatalf("ExportSeal: %v", err)
			}
			verifier, err := agentic.ImportSeal(New(), seal)
			if err != nil {
				t.Fatalf("ImportSeal: %v", err)
			}
			if err := os.WriteFile(sidecar, []byte(tc.answer), 0o644); err != nil {
				t.Fatal(err)
			}
			requireMuseSealRefusal(t, verifier.VerifyBeforeExec(plan), ErrMuseSealReleaseChanged, "imported same-bytes release swap")
		})
	}
}

// TestMuseImportSealedDataRefusesMalformedShapes drives the exported plugin
// importer directly: any mutation of an exported payload invalidates the
// envelope integrity digest first, so ImportSeal would name tampering
// instead of the shape defect. The ImportSeal-to-plugin wiring is proven
// separately by the round-trip tests above. Malformed releases, a missing
// or unexpected mode selector, missing or unexpected help digests, a
// foreign commitment key and literal env values refuse here too.
func TestMuseImportSealedDataRefusesMalformedShapes(t *testing.T) {
	plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	cases := []struct {
		name     string
		mutate   func(*agentic.SealedData)
		sentinel error
	}{
		{name: "no-artifacts", mutate: func(data *agentic.SealedData) { data.Artifacts = nil }, sentinel: ErrMuseSealMalformed},
		{name: "wrong-name", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Name = "other" }, sentinel: ErrMuseSealMalformed},
		{name: "relative-binary", mutate: func(data *agentic.SealedData) { data.Binary = "muse" }, sentinel: ErrMuseSealMalformed},
		{name: "artifact-path-mismatch", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Path += "-moved" }, sentinel: ErrMuseSealMalformed},
		{name: "bad-digest", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Digest = "sha256:zzz" }, sentinel: ErrMuseSealMalformed},
		{name: "bare-digest", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Digest = strings.Repeat("0", 64) }, sentinel: ErrMuseSealMalformed},
		{name: "missing-release", mutate: func(data *agentic.SealedData) { delete(data.Selectors, sealedReleaseKey) }, sentinel: ErrMuseSealMalformed},
		{name: "malformed-release-triple", mutate: func(data *agentic.SealedData) { data.Selectors[sealedReleaseKey] = "1.5.0" }, sentinel: ErrMuseSealMalformed},
		{name: "malformed-release-garbage", mutate: func(data *agentic.SealedData) { data.Selectors[sealedReleaseKey] = "not-a-build" }, sentinel: ErrMuseSealMalformed},
		{name: "missing-mode", mutate: func(data *agentic.SealedData) { delete(data.Selectors, sealedModeKey) }, sentinel: ErrMuseSealMalformed},
		{name: "bad-mode", mutate: func(data *agentic.SealedData) { data.Selectors[sealedModeKey] = "turbo" }, sentinel: ErrMuseSealMalformed},
		{name: "native-with-help-stdout", mutate: func(data *agentic.SealedData) { data.Selectors[sealedHelpStdoutKey] = strings.Repeat("0", 64) }, sentinel: ErrMuseSealMalformed},
		{name: "native-with-help-lines", mutate: func(data *agentic.SealedData) { data.Selectors[sealedHelpLinesKey] = strings.Repeat("0", 64) }, sentinel: ErrMuseSealMalformed},
		{name: "missing-key-id", mutate: func(data *agentic.SealedData) { delete(data.Selectors, sealedKeyIDKey) }, sentinel: ErrMuseSealMalformed},
		{name: "foreign-selector", mutate: func(data *agentic.SealedData) { data.Selectors["PATH"] = "/elsewhere" }, sentinel: ErrMuseSealMalformed},
		{name: "literal-env-value", mutate: func(data *agentic.SealedData) { data.Selectors["XDG_DATA_HOME"] = "/elsewhere" }, sentinel: ErrMuseSealMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := *seal.Data.Sealed
			data.Artifacts = append([]agentic.SealedArtifact(nil), seal.Data.Sealed.Artifacts...)
			data.Selectors = make(map[string]string, len(seal.Data.Sealed.Selectors))
			for name, value := range seal.Data.Sealed.Selectors {
				data.Selectors[name] = value
			}
			tc.mutate(&data)
			if _, err := New().ImportSealedData(data); !errors.Is(err, tc.sentinel) {
				t.Fatalf("ImportSealedData %s err = %v, want %v", tc.name, err, tc.sentinel)
			}
		})
	}
	// M-AG2 retired the verified-build list: well-formed releases import
	// (see TestSealBindsAttestedNewest), and only malformed ones refuse
	// above. The yolo help selectors below are the seal's required
	// evidence shape: dropping one is malformed, not native mode.
	yoloReq := museInteractiveRequest(t)
	yoloReq.PermissionMode = agentic.PermissionModeYolo
	yoloReq.ToolRelease = verifiedMuseRelease
	yoloPlan := buildMuseInteractivePlan(t, New(), yoloReq)
	yoloSeal, err := yoloPlan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal yolo: %v", err)
	}
	cloneYolo := func(t *testing.T) agentic.SealedData {
		t.Helper()
		data := *yoloSeal.Data.Sealed
		data.Selectors = make(map[string]string, len(yoloSeal.Data.Sealed.Selectors))
		for name, value := range yoloSeal.Data.Sealed.Selectors {
			data.Selectors[name] = value
		}
		return data
	}
	t.Run("yolo-missing-help-stdout", func(t *testing.T) {
		data := cloneYolo(t)
		delete(data.Selectors, sealedHelpStdoutKey)
		if _, err := New().ImportSealedData(data); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("ImportSealedData yolo without help stdout digest err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("yolo-missing-help-lines", func(t *testing.T) {
		data := cloneYolo(t)
		delete(data.Selectors, sealedHelpLinesKey)
		if _, err := New().ImportSealedData(data); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("ImportSealedData yolo without help lines digest err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("yolo-bad-help-digest", func(t *testing.T) {
		data := cloneYolo(t)
		data.Selectors[sealedHelpStdoutKey] = "not-hex"
		if _, err := New().ImportSealedData(data); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("ImportSealedData yolo with malformed help digest err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("foreign-key-id", func(t *testing.T) {
		data := *seal.Data.Sealed
		data.Selectors = make(map[string]string, len(seal.Data.Sealed.Selectors))
		for name, value := range seal.Data.Sealed.Selectors {
			data.Selectors[name] = value
		}
		data.Selectors[sealedKeyIDKey] = strings.Repeat("0", 64)
		if _, err := New().ImportSealedData(data); !errors.Is(err, agentic.ErrSealImportRefused) {
			t.Fatalf("ImportSealedData foreign key id err = %v, want ErrSealImportRefused", err)
		}
	})
	t.Run("swapped-bytes", func(t *testing.T) {
		data := *seal.Data.Sealed
		if err := os.WriteFile(plan.Binary, []byte(museSealWitnessStub), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := New().ImportSealedData(data); !errors.Is(err, ErrMuseSealBinaryChanged) {
			t.Fatalf("swapped binary admitted at import: %v", err)
		}
	})
}

// TestMuseSealRefusesUnsealedImport pins the R-SEAL no-downgrade rule for
// Muse: the plugin declares a sealer, so the unsealed guard refuses at
// the sealer gate.
func TestMuseSealRefusesUnsealedImport(t *testing.T) {
	seal := agentic.Seal{Schema: agentic.ExecGuardSchema, SchemaVersion: agentic.ExecGuardVersion, Data: agentic.SealData{Kind: agentic.SealKindUnsealed}}
	_, err := agentic.ImportSeal(New(), seal)
	if !errors.Is(err, agentic.ErrSealUnsealedRefused) || !strings.Contains(err.Error(), "declares a sealer") {
		t.Fatalf("muse sealer refusal lost: %v", err)
	}
}

// TestMuseSealSurvivesFinalizePlan finalizes a sealed interactive plan:
// a benign overlay applies, the original binary digest travels forward
// untouched, and post-finalization verification still refuses a swapped
// or deleted binary, a changed final process, and an unprobed release.
// Release drift across finalization is pinned separately by
// TestMuseFinalizedSealRefusesReleaseDrift; XDG overlays refuse by
// TestMuseFinalizedXDGIdentityImmutable.
func TestMuseSealSurvivesFinalizePlan(t *testing.T) {
	plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	before, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{
		FragmentEnv: []string{"LC_ALL=C"},
		NativeTail:  []string{"--final-tail"},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	after, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if after.Data.Sealed.Artifacts[0].Digest != before.Data.Sealed.Artifacts[0].Digest {
		t.Fatal("finalized seal carries a re-minted artifact digest")
	}
	changed := final
	changed.Argv = append(append([]string(nil), final.Argv...), "--extra")
	requireMuseSealRefusal(t, changed.VerifyBeforeExec(), agentic.ErrFinalizedProcessChanged, "changed finalized argv")

	swapped := plan
	if err := os.WriteFile(swapped.Binary, []byte(museSealWitnessStub), 0o755); err != nil {
		t.Fatal(err)
	}
	requireMuseSealRefusal(t, final.VerifyBeforeExec(), ErrMuseSealBinaryChanged, "swapped binary after finalization")
	requireMuseSealRefusal(t, swapped.VerifyBeforeExec(), ErrMuseSealBinaryChanged, "swapped binary on the base plan")

	deleted := plan
	if err := os.Remove(deleted.Binary); err != nil {
		t.Fatal(err)
	}
	requireMuseSealRefusal(t, final.VerifyBeforeExec(), ErrMuseSealBinaryChanged, "deleted binary after finalization")

	// A finalized plan over a binary that is readable but not executable
	// reaches the finalized release re-probe, which refuses undetected
	// rather than inventing a release.
	unprobed := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	unprobedFinal, err := agentic.FinalizePlan(unprobed, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := os.Chmod(unprobed.Binary, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unprobedFinal.VerifyBeforeExec(); !errors.Is(err, agentic.ErrToolReleaseUndetected) {
		t.Fatalf("unexecutable finalized binary admitted: %v", err)
	}
}

// TestMuseFinalizedSealImports drives the finalized Muse path end to end
// for both verified builds: BuildPlan seals, FinalizePlan carries the
// plugin selectors beside the process binding, and ImportSeal rebuilds a
// verifier that accepts the untouched final plan and refuses
// final-process mutations with the same typed errors as the in-process
// wrapper.
func TestMuseFinalizedSealImports(t *testing.T) {
	for _, tc := range []struct {
		version string
		build   string
	}{
		{version: "1.4.1", build: "1.4.1-R4503.1"},
		{version: "1.4.2", build: "1.4.2-R4684.1"},
	} {
		t.Run(tc.build, func(t *testing.T) {
			dir := t.TempDir()
			writeMuseSidecarStub(t, dir, "Muse Code "+tc.version+" ("+tc.build+")\n")
			plan := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, nil)
			if err != nil {
				t.Fatalf("FinalizePlan: %v", err)
			}
			seal, err := final.ExportSeal()
			if err != nil {
				t.Fatalf("ExportSeal: %v", err)
			}
			if seal.Data.Kind != agentic.SealKindSealed || seal.Data.Sealed == nil || seal.Data.Sealed.Binding == nil {
				t.Fatalf("data = %+v, want sealed with a process binding", seal.Data)
			}
			if seal.Data.Sealed.Selectors[sealedReleaseKey] != tc.build {
				t.Fatalf("finalized release selector = %q, want the sealed %q", seal.Data.Sealed.Selectors[sealedReleaseKey], tc.build)
			}
			wire, err := json.Marshal(seal)
			if err != nil {
				t.Fatalf("marshal seal: %v", err)
			}
			decoded, err := agentic.DecodeSeal(wire)
			if err != nil {
				t.Fatalf("DecodeSeal: %v", err)
			}
			verifier, err := agentic.ImportSeal(New(), decoded)
			if err != nil {
				t.Fatalf("finalized %s seal refused import: %v", tc.build, err)
			}
			if err := verifier.VerifyBeforeExec(final); err != nil {
				t.Fatalf("imported finalized verifier refused the untouched plan: %v", err)
			}
			extended := final
			extended.Argv = append(append([]string(nil), final.Argv...), "--extra")
			requireMuseSealRefusal(t, extended.VerifyBeforeExec(), agentic.ErrFinalizedProcessChanged, "in-process finalized argv change")
			requireMuseSealRefusal(t, verifier.VerifyBeforeExec(extended), agentic.ErrFinalizedProcessChanged, "imported finalized argv change")
			recolored := final
			recolored.Env = append(append([]string(nil), final.Env...), "XDG_STATE_HOME=/elsewhere")
			requireMuseSealRefusal(t, recolored.VerifyBeforeExec(), agentic.ErrFinalizedProcessChanged, "in-process finalized env change")
			requireMuseSealRefusal(t, verifier.VerifyBeforeExec(recolored), agentic.ErrFinalizedProcessChanged, "imported finalized env change")
		})
	}
}

// TestMuseFinalizedSealRefusesReleaseDrift finalizes a sealed plan, then
// swaps the effective build behind unchanged bytes: the base verifier,
// the finalized verifier and the imported finalized verifier refuse
// release-typed alike. Finalization keeps the release check; it never
// drops it.
func TestMuseFinalizedSealRefusesReleaseDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
	}{
		{name: "to-1-5-0", answer: "Muse Code 1.5.0 (1.5.0-R9999.1)\n"},
		{name: "to-1-4-2", answer: "Muse Code 1.4.2 (1.4.2-R4684.1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, sidecar := writeMuseSidecarStub(t, dir, "Muse Code 1.4.1 (1.4.1-R4503.1)\n")
			base := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
			if err != nil {
				t.Fatalf("FinalizePlan: %v", err)
			}
			seal, err := final.ExportSeal()
			if err != nil {
				t.Fatalf("ExportSeal: %v", err)
			}
			imported, err := agentic.ImportSeal(New(), seal)
			if err != nil {
				t.Fatalf("ImportSeal: %v", err)
			}
			if err := os.WriteFile(sidecar, []byte(tc.answer), 0o644); err != nil {
				t.Fatal(err)
			}
			requireMuseSealRefusal(t, base.VerifyBeforeExec(), ErrMuseSealReleaseChanged, "base release drift")
			requireMuseSealRefusal(t, final.VerifyBeforeExec(), ErrMuseSealReleaseChanged, "finalized plan admits release drift refused by base")
			requireMuseSealRefusal(t, imported.VerifyBeforeExec(final), ErrMuseSealReleaseChanged, "imported finalized release drift")
		})
	}
}

// TestMuseSealExportCarriesNoEnvironmentValues pins the guard hygiene
// rule over the real export path: a synthetic XDG value reaches the
// sealed plan but must not appear anywhere in the exported guard JSON,
// and every env selector must carry a keyed commitment.
func TestMuseSealExportCarriesNoEnvironmentValues(t *testing.T) {
	const canary = "synthetic-env-canary-2hdguc"
	req := museSealedInteractiveRequest(t)
	replaced := false
	for i, entry := range req.Env {
		if strings.HasPrefix(entry, "XDG_CACHE_HOME=") {
			req.Env[i] = "XDG_CACHE_HOME=" + canary
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("request carries no XDG_CACHE_HOME to mark")
	}
	plan := buildMuseSealedPlan(t, req)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatalf("marshal seal: %v", err)
	}
	if strings.Contains(string(wire), canary) {
		t.Fatal("exported guard contains literal environment canary")
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", museNoAutoUpdateEnv} {
		if !isSealedCommitment(seal.Data.Sealed.Selectors[name]) {
			t.Fatalf("exported selector %q is not a keyed commitment", name)
		}
	}
}

// TestMuseSealEstablishmentRefusesUnreadableBinary stages a binary that
// deletes itself while answering the seal-time version probe:
// establishment reaches the seal-read refusal deterministically through
// BuildPlan, with no concurrent mutation.
func TestMuseSealEstablishmentRefusesUnreadableBinary(t *testing.T) {
	_, env := writeMuseVersionStub(t, "/bin/rm -- \"$0\"\nprintf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\n")
	req := museInteractiveRequest(t)
	req.Env = env
	req.PermissionMode = agentic.PermissionModeNative
	req.ToolRelease = ""
	_, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
	if !errors.Is(err, ErrMuseSealMalformed) {
		t.Fatalf("self-deleting binary sealed: %v", err)
	}
}

// TestMuseSealEstablishmentRefusesReleaseChangeMidSeal stages a binary
// whose first version answer is 1.4.1 and whose later answers are 1.4.2:
// the seal self-check reaches the release refusal deterministically
// through BuildPlan, with no concurrent mutation.
func TestMuseSealEstablishmentRefusesReleaseChangeMidSeal(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "counter")
	body := fmt.Sprintf("if [ -f '%s' ]; then printf '%%s\\n' 'Muse Code 1.4.2 (1.4.2-R4684.1)'; else /usr/bin/touch '%s'; printf '%%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'; fi\n", counter, counter)
	_, env := writeMuseVersionStub(t, body)
	req := museInteractiveRequest(t)
	req.Env = env
	req.PermissionMode = agentic.PermissionModeNative
	req.ToolRelease = ""
	_, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
	if !errors.Is(err, ErrMuseSealReleaseChanged) {
		t.Fatalf("mid-seal release change admitted: %v", err)
	}
}

// TestMuseNonInteractivePlansStayUnsealed pins the seal scope: plain exec
// and dry-run plans carry no verifier, so their export refuses without
// downgrading to unsealed.
func TestMuseNonInteractivePlansStayUnsealed(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			plan := buildMusePlan(t, New(), launchRequest(t, ""), mode)
			if _, err := plan.ExportSeal(); !errors.Is(err, agentic.ErrSealNotExportable) {
				t.Fatalf("ExportSeal err = %v, want ErrSealNotExportable", err)
			}
		})
	}
}

// TestMuseSealRefusesRelativeBinary drives SealExecPlan directly over a
// plan whose binary is not absolute: hashing has no base without one, so
// establishment refuses rather than resolving against ambient state.
func TestMuseSealRefusesRelativeBinary(t *testing.T) {
	plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	relative := plan
	relative.Binary = "bin/muse"
	_, err := New().SealExecPlan(relative)
	if !errors.Is(err, ErrMuseSealMalformed) {
		t.Fatalf("relative binary sealed: %v", err)
	}
}

// museFinalizedPinBuilds are the two module-verified builds every
// finalized-pin regression walks: the pin invariant holds per build.
func museFinalizedPinBuilds() []struct {
	version string
	build   string
} {
	return []struct {
		version string
		build   string
	}{
		{version: "1.4.1", build: "1.4.1-R4503.1"},
		{version: "1.4.2", build: "1.4.2-R4684.1"},
	}
}

// TestMuseFinalizedSealRefusesUpdaterPinOverlay pins the F1
// finalization gate through the production FinalizePlan entry point: any
// overlay naming the updater pin — either layer, any value, even the
// pinned value itself — refuses typed before the binding can attest it,
// on both verified builds. A sensitive selector named in both overlays
// refuses at the first layer's touch. Sealed XDG overlays refuse by the
// same rule (see TestMuseFinalizedXDGIdentityImmutable).
func TestMuseFinalizedSealRefusesUpdaterPinOverlay(t *testing.T) {
	for _, tc := range museFinalizedPinBuilds() {
		t.Run(tc.build, func(t *testing.T) {
			sealed := func(t *testing.T) agentic.Plan {
				t.Helper()
				dir := t.TempDir()
				writeMuseSidecarStub(t, dir, "Muse Code "+tc.version+" ("+tc.build+")\n")
				return buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			}
			t.Run("fragment-zeroed", func(t *testing.T) {
				_, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{
					FragmentEnv: []string{museNoAutoUpdateEnv + "=0"},
				}, nil)
				if !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("fragment pin=0 overlay admitted: %v", err)
				}
			})
			t.Run("prompt-zeroed", func(t *testing.T) {
				_, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{
					PromptEnv: []string{museNoAutoUpdateEnv + "=0"},
				}, nil)
				if !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("prompt pin=0 overlay admitted: %v", err)
				}
			})
			t.Run("fragment-same-value", func(t *testing.T) {
				_, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{
					FragmentEnv: []string{museNoAutoUpdateEnv + "=1"},
				}, nil)
				if !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("fragment pin=1 overlay admitted: %v", err)
				}
			})
			t.Run("prompt-other-value", func(t *testing.T) {
				_, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{
					PromptEnv: []string{museNoAutoUpdateEnv + "=2"},
				}, nil)
				if !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("prompt pin=2 overlay admitted: %v", err)
				}
			})
			t.Run("cross-overlay-duplicate", func(t *testing.T) {
				_, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{
					FragmentEnv: []string{"XDG_CONFIG_HOME=/fragment/config"},
					PromptEnv:   []string{"XDG_CONFIG_HOME=/prompt/config"},
				}, nil)
				if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "must not touch") {
					t.Fatalf("cross-overlay XDG duplicate admitted: %v", err)
				}
			})
		})
	}
}

// TestMuseFinalizedVerifierRefusesUnpinnedEnv pins the F1 verifier half:
// the finalized Muse check re-enforces the updater pin and the duplicate
// rule on the final env before any hash or probe, on both builds. It
// drives VerifyFinalizedPlan through the production
// ExecFinalizedPlanVerifier interface over a seal the production sealer
// built: a white-box call is required because FinalizePlan already
// refuses pin overlays, so no public-entry final plan can carry an
// unpinned env to the verifier. Each case names the pin value the
// narrowing mutant admits.
func TestMuseFinalizedVerifierRefusesUnpinnedEnv(t *testing.T) {
	for _, tc := range museFinalizedPinBuilds() {
		t.Run(tc.build, func(t *testing.T) {
			dir := t.TempDir()
			writeMuseSidecarStub(t, dir, "Muse Code "+tc.version+" ("+tc.build+")\n")
			base := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			verifier, err := sealInteractiveExecPlan(base)
			if err != nil {
				t.Fatalf("sealInteractiveExecPlan: %v", err)
			}
			final, ok := verifier.(agentic.ExecFinalizedPlanVerifier)
			if !ok {
				t.Fatal("muse seal implements no finalized verifier")
			}
			setPin := func(plan agentic.Plan, value string) agentic.Plan {
				changed := plan
				changed.Env = nil
				for _, entry := range plan.Env {
					if key, _, ok := strings.Cut(entry, "="); ok && key == museNoAutoUpdateEnv {
						changed.Env = append(changed.Env, museNoAutoUpdateEnv+"="+value)
						continue
					}
					changed.Env = append(changed.Env, entry)
				}
				return changed
			}
			t.Run("pin-zeroed", func(t *testing.T) {
				if err := final.VerifyFinalizedPlan(setPin(base, "0")); !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("unpinned finalized plan admitted: %v", err)
				}
			})
			t.Run("pin-removed", func(t *testing.T) {
				changed := base
				changed.Env = nil
				for _, entry := range base.Env {
					if key, _, ok := strings.Cut(entry, "="); ok && key == museNoAutoUpdateEnv {
						continue
					}
					changed.Env = append(changed.Env, entry)
				}
				if err := final.VerifyFinalizedPlan(changed); !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("pin-removed finalized plan admitted: %v", err)
				}
			})
			t.Run("pin-other-value", func(t *testing.T) {
				if err := final.VerifyFinalizedPlan(setPin(base, "2")); !errors.Is(err, ErrMuseSealEnvChanged) {
					t.Fatalf("pin=2 finalized plan admitted: %v", err)
				}
			})
			t.Run("duplicate-selector", func(t *testing.T) {
				changed := base
				changed.Env = append(append([]string(nil), base.Env...), museNoAutoUpdateEnv+"=1")
				err := final.VerifyFinalizedPlan(changed)
				if !errors.Is(err, ErrMuseSealEnvChanged) || !strings.Contains(err.Error(), "duplicate") {
					t.Fatalf("duplicated finalized pin admitted: %v", err)
				}
			})
			t.Run("pinned-accepts", func(t *testing.T) {
				if err := final.VerifyFinalizedPlan(base); err != nil {
					t.Fatalf("pinned finalized plan refused: %v", err)
				}
			})
		})
	}
}

// museSidecarXDGRequest extends the sidecar request with a non-trivial
// XDG identity, so finalized-XDG regressions run against both verified
// builds instead of only the 1.4.1 fixture binary.
func museSidecarXDGRequest(t *testing.T, dir string) agentic.LaunchRequest {
	t.Helper()
	req := museSidecarRequest(t, dir)
	scratch := t.TempDir()
	req.Env = append(req.Env,
		"XDG_CONFIG_HOME="+filepath.Join(scratch, "config"),
		"XDG_DATA_HOME="+filepath.Join(scratch, "data"),
		"XDG_CACHE_HOME="+filepath.Join(scratch, "cache"),
	)
	return req
}

// TestMuseFinalizedXDGIdentityImmutable is the generated class test for
// the finalization-drops-release-verification class: the sealed XDG
// override identity is immutable after sealing, exactly like the updater
// pin. Cases are generated over the catalog families — both verified
// builds x every sealed XDG variable x {fragment, prompt} x
// {change, remove, add} — not hand-enumerated examples, so a new bypass
// shape in the same class fails here rather than needing a new example.
// The overlay half drives the production FinalizePlan entry point; the
// verifier half drives VerifyFinalizedPlan through the production
// ExecFinalizedPlanVerifier interface over a production-built seal (a
// white-box call is required because FinalizePlan already refuses
// selector overlays, and the overlay grammar cannot express removal, so
// no public-entry final plan can carry a drifted identity to the
// verifier). The cache change value is the historically admitted
// /final/cache; the narrowing mutants exempt exactly one generated case
// each.
func TestMuseFinalizedXDGIdentityImmutable(t *testing.T) {
	sealedVars := []struct {
		name    string
		changed string
	}{
		{name: "XDG_CONFIG_HOME", changed: "/changed/config"},
		{name: "XDG_DATA_HOME", changed: "/changed/data"},
		{name: "XDG_CACHE_HOME", changed: "/final/cache"},
	}
	const addedVar = "XDG_STATE_HOME"
	for _, tc := range museFinalizedPinBuilds() {
		t.Run(tc.build, func(t *testing.T) {
			sealed := func(t *testing.T) agentic.Plan {
				t.Helper()
				dir := t.TempDir()
				writeMuseSidecarStub(t, dir, "Muse Code "+tc.version+" ("+tc.build+")\n")
				plan := buildMuseSealedPlan(t, museSidecarXDGRequest(t, dir))
				for _, v := range sealedVars {
					mustMuseEnvValue(t, plan.Env, v.name)
				}
				mustMuseEnvValue(t, plan.Env, museNoAutoUpdateEnv)
				return plan
			}
			finalizeRefuses := func(t *testing.T, overlays agentic.FinalizeOverlays, what string) {
				t.Helper()
				_, err := agentic.FinalizePlan(sealed(t), overlays, nil)
				requireMuseSealRefusal(t, err, ErrMuseSealEnvChanged, what)
			}
			for _, layer := range []string{"fragment", "prompt"} {
				t.Run(layer, func(t *testing.T) {
					place := func(entry string) agentic.FinalizeOverlays {
						if layer == "fragment" {
							return agentic.FinalizeOverlays{FragmentEnv: []string{entry}}
						}
						return agentic.FinalizeOverlays{PromptEnv: []string{entry}}
					}
					for _, v := range sealedVars {
						t.Run("change-"+v.name, func(t *testing.T) {
							finalizeRefuses(t, place(v.name+"="+v.changed), "XDG identity overlay")
						})
					}
					t.Run("add-"+addedVar, func(t *testing.T) {
						finalizeRefuses(t, place(addedVar+"=/added/state"), "XDG identity overlay")
					})
				})
			}
			t.Run("cross-overlay", func(t *testing.T) {
				finalizeRefuses(t, agentic.FinalizeOverlays{
					FragmentEnv: []string{"XDG_CONFIG_HOME=/fragment/config"},
					PromptEnv:   []string{"XDG_CONFIG_HOME=/prompt/config"},
				}, "cross-overlay XDG identity overlay")
			})
			t.Run("benign-accepts", func(t *testing.T) {
				empty, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{}, nil)
				if err != nil {
					t.Fatalf("empty finalize refused: %v", err)
				}
				if err := empty.VerifyBeforeExec(); err != nil {
					t.Fatalf("empty finalized plan refused itself: %v", err)
				}
				overlaid, err := agentic.FinalizePlan(sealed(t), agentic.FinalizeOverlays{
					FragmentEnv: []string{"LC_ALL=C"},
				}, nil)
				if err != nil {
					t.Fatalf("benign overlay finalize refused: %v", err)
				}
				if err := overlaid.VerifyBeforeExec(); err != nil {
					t.Fatalf("benign finalized plan refused itself: %v", err)
				}
			})
			t.Run("verify", func(t *testing.T) {
				base := sealed(t)
				verifier, err := sealInteractiveExecPlan(base)
				if err != nil {
					t.Fatalf("sealInteractiveExecPlan: %v", err)
				}
				final, ok := verifier.(agentic.ExecFinalizedPlanVerifier)
				if !ok {
					t.Fatal("muse seal implements no finalized verifier")
				}
				setEnv := func(plan agentic.Plan, name, value string) agentic.Plan {
					changed := plan
					changed.Env = nil
					replaced := false
					for _, entry := range plan.Env {
						if key, _, ok := strings.Cut(entry, "="); ok && key == name {
							changed.Env = append(changed.Env, name+"="+value)
							replaced = true
							continue
						}
						changed.Env = append(changed.Env, entry)
					}
					if !replaced {
						changed.Env = append(changed.Env, name+"="+value)
					}
					return changed
				}
				unsetEnv := func(plan agentic.Plan, name string) agentic.Plan {
					changed := plan
					changed.Env = nil
					for _, entry := range plan.Env {
						if key, _, ok := strings.Cut(entry, "="); ok && key == name {
							continue
						}
						changed.Env = append(changed.Env, entry)
					}
					return changed
				}
				for _, v := range sealedVars {
					t.Run("change-"+v.name, func(t *testing.T) {
						requireMuseSealRefusal(t, final.VerifyFinalizedPlan(setEnv(base, v.name, v.changed)), ErrMuseSealEnvChanged, "finalized XDG identity drift")
					})
					t.Run("remove-"+v.name, func(t *testing.T) {
						requireMuseSealRefusal(t, final.VerifyFinalizedPlan(unsetEnv(base, v.name)), ErrMuseSealEnvChanged, "finalized XDG identity drift")
					})
				}
				t.Run("add-"+addedVar, func(t *testing.T) {
					requireMuseSealRefusal(t, final.VerifyFinalizedPlan(setEnv(base, addedVar, "/added/state")), ErrMuseSealEnvChanged, "finalized XDG identity drift")
				})
				t.Run("sealed-accepts", func(t *testing.T) {
					if err := final.VerifyFinalizedPlan(base); err != nil {
						t.Fatalf("sealed finalized plan refused: %v", err)
					}
				})
			})
		})
	}
}

// museSealTestKey builds a fixed explicit commitment key whose first byte
// is marker and whose remaining bytes encode marker too, so the key hex
// is distinctive in guard-hygiene scans. Marker 42 is the panel's
// cross-process key; 99 is the wrong key the key-ignoring narrowing
// mutant admits; 100 is a second wrong key that mutant still refuses.
func museSealTestKey(marker byte) agentic.SealCommitmentKey {
	var key agentic.SealCommitmentKey
	for i := range key {
		key[i] = marker + byte(i%251)
	}
	key[0] = marker
	return key
}

// TestMuseFinalizedSealCrossProcessKeyRoundTrip pins the F2 shared-key
// contract through the production FinalizePlan/ExportSeal/ImportSeal path
// on both builds: finalizing under an explicit key and importing with
// that same key accepts (the two-key-context cross-process witness: the
// explicit key differs from either process-local key, so acceptance
// proves the supplied key threads through), while a different key or a
// missing key refuses typed. The key itself never enters the guard.
func TestMuseFinalizedSealCrossProcessKeyRoundTrip(t *testing.T) {
	for _, tc := range museFinalizedPinBuilds() {
		t.Run(tc.build, func(t *testing.T) {
			dir := t.TempDir()
			writeMuseSidecarStub(t, dir, "Muse Code "+tc.version+" ("+tc.build+")\n")
			plan := buildMuseSealedPlan(t, museSidecarRequest(t, dir))
			keySame := museSealTestKey(42)
			keyWrong := museSealTestKey(99)
			keyOtherWrong := museSealTestKey(100)
			final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, nil, keySame)
			if err != nil {
				t.Fatalf("FinalizePlan with explicit key: %v", err)
			}
			seal, err := final.ExportSeal()
			if err != nil {
				t.Fatalf("ExportSeal: %v", err)
			}
			wire, err := json.Marshal(seal)
			if err != nil {
				t.Fatalf("marshal seal: %v", err)
			}
			// The key never serializes: only its ID (a hash) travels.
			// The ID must be present and must equal the committing key's.
			if got := seal.Data.Sealed.Selectors[sealedKeyIDKey]; got != agentic.CommitmentKeyID(keySame) {
				t.Fatalf("finalized key id = %q, want the explicit key id %q", got, agentic.CommitmentKeyID(keySame))
			}
			keyHex := hex.EncodeToString(keySame[:])
			if strings.Contains(string(wire), keyHex) {
				t.Fatal("exported guard contains explicit commitment key bytes")
			}
			if strings.Contains(string(wire), string(keySame[:])) {
				t.Fatal("exported guard contains raw commitment key bytes")
			}
			decoded, err := agentic.DecodeSeal(wire)
			if err != nil {
				t.Fatalf("DecodeSeal: %v", err)
			}
			t.Run("equal-key-accepts", func(t *testing.T) {
				verifier, err := agentic.ImportSeal(New(), decoded, keySame)
				if err != nil {
					t.Fatalf("same-key import refused: %v", err)
				}
				if err := verifier.VerifyBeforeExec(final); err != nil {
					t.Fatalf("same-key verifier refused the untouched plan: %v", err)
				}
				// A keyed import re-exports under the same key id, never
				// the importer's process-local id.
				exporter, ok := verifier.(agentic.ExecSealExporter)
				if !ok {
					t.Fatal("keyed import yielded no exporter")
				}
				if got := exporter.ExportSealedData().Selectors[sealedKeyIDKey]; got != agentic.CommitmentKeyID(keySame) {
					t.Fatalf("re-exported key id = %q, want the explicit key id", got)
				}
			})
			t.Run("different-key-refuses", func(t *testing.T) {
				_, err := agentic.ImportSeal(New(), decoded, keyWrong)
				if !errors.Is(err, agentic.ErrSealImportRefused) {
					t.Fatalf("wrong commitment key admitted: %v", err)
				}
				if strings.Contains(err.Error(), keyHex) || strings.Contains(err.Error(), hex.EncodeToString(keyWrong[:])) {
					t.Fatalf("key refusal leaks key bytes: %v", err)
				}
			})
			t.Run("other-wrong-key-refuses", func(t *testing.T) {
				_, err := agentic.ImportSeal(New(), decoded, keyOtherWrong)
				if !errors.Is(err, agentic.ErrSealImportRefused) {
					t.Fatalf("second wrong commitment key admitted: %v", err)
				}
			})
			t.Run("missing-key-refuses", func(t *testing.T) {
				_, err := agentic.ImportSeal(New(), decoded)
				if !errors.Is(err, agentic.ErrSealImportRefused) {
					t.Fatalf("missing commitment key admitted: %v", err)
				}
			})
		})
	}
}
