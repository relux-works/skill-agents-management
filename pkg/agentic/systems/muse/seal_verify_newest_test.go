package muse

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// writeMuseMutableHelpStub installs a binary answering --version with the
// fixed answer and --help with the CURRENT content of its help file, so
// drift tests can mutate help between sealing and verification. It
// returns the binary path, the help file path and a PATH-only env.
func writeMuseMutableHelpStub(t *testing.T, dir, answer, help string) (string, string, []string) {
	t.Helper()
	helpFile := filepath.Join(dir, "help.txt")
	if err := os.WriteFile(helpFile, []byte(help), 0o644); err != nil {
		t.Fatalf("writing the help file: %v", err)
	}
	binary := filepath.Join(dir, "muse")
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		"printf '%s\\n' '" + answer + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		"cat '" + helpFile + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"exit 8\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the mutable stub: %v", err)
	}
	// The stub needs cat; the seal under test binds XDG_*/pin only, so
	// the extra PATH entries are invisible to it.
	return binary, helpFile, []string{"PATH=" + dir + ":/bin:/usr/bin"}
}

// writeMuseHelpCounterStub installs a binary answering --version with the
// fixed answer and --help with firstHelp on its first call and laterBody
// afterwards, so establishment tests can change help between argv mapping
// and seal creation.
func writeMuseHelpCounterStub(t *testing.T, dir, answer, firstHelp, laterBody string) []string {
	t.Helper()
	counter := filepath.Join(dir, "help-count")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatalf("writing the counter file: %v", err)
	}
	binary := filepath.Join(dir, "muse")
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		"printf '%s\\n' '" + answer + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		"n=$(cat '" + counter + "')\n" +
		"n=$((n + 1))\n" +
		"printf '%s' \"$n\" > '" + counter + "'\n" +
		"if [ \"$n\" -eq 1 ]; then\n" +
		"printf '%s\\n' '" + firstHelp + "'\n" +
		"else\n" + laterBody + "\n" +
		"fi\n" +
		"exit 0\n" +
		"fi\n" +
		"exit 8\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the counter stub: %v", err)
	}
	// The stub needs cat; the seal under test binds XDG_*/pin only, so
	// the extra PATH entries are invisible to it.
	return []string{"PATH=" + dir + ":/bin:/usr/bin"}
}

// TestHelpEvidenceBoundAndReverified pins the yolo evidence binding: the
// seal carries the help digests, export and import preserve them, and
// every verification re-probes — a changed help text or a lost
// declaration refuses typed on the direct and the imported verifier
// alike. Dropping the check on imported seals is what the M43 mutant
// does.
func TestHelpEvidenceBoundAndReverified(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, helpFile, env := writeMuseMutableHelpStub(t, dir,
		"Muse Code 1.4.1 (1.4.1-R4503.1)", museYoloHelpFixture)
	plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
	})
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Sealed.Selectors[sealedHelpStdoutKey] == "" || seal.Data.Sealed.Selectors[sealedHelpLinesKey] == "" {
		t.Fatal("yolo seal binds no help evidence digests")
	}
	imported, err := New().ImportSealedData(*seal.Data.Sealed)
	if err != nil {
		t.Fatalf("ImportSealedData: %v", err)
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("imported verify of unchanged help: %v", err)
	}
	rewriteHelp := func(t *testing.T, help string) {
		t.Helper()
		if err := os.WriteFile(helpFile, []byte(help), 0o644); err != nil {
			t.Fatalf("rewriting help: %v", err)
		}
	}
	t.Run("drift", func(t *testing.T) {
		rewriteHelp(t, museYoloHelpFixture+"\n  --yolo-extra documented\n")
		if err := plan.VerifyBeforeExec(); !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("direct verify of drifted help err = %v, want ErrMuseSealHelpChanged", err)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("imported verify of drifted help err = %v, want ErrMuseSealHelpChanged", err)
		}
	})
	t.Run("declaration-lost", func(t *testing.T) {
		rewriteHelp(t, museNoYoloHelpFixture)
		if err := plan.VerifyBeforeExec(); !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("direct verify of declaration-less help err = %v, want ErrMuseSealHelpChanged", err)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("imported verify of declaration-less help err = %v, want ErrMuseSealHelpChanged", err)
		}
	})
	t.Run("help-unreadable", func(t *testing.T) {
		if err := os.Remove(helpFile); err != nil {
			t.Fatalf("removing help: %v", err)
		}
		var attempt *agentic.ProbeExecutionError
		if err := plan.VerifyBeforeExec(); !errors.As(err, &attempt) {
			t.Fatalf("direct verify of unreadable help err = %v, want a *ProbeExecutionError", err)
		} else if attempt.Stage != agentic.ProbeStageHelp {
			t.Fatalf("direct verify of unreadable help stage = %q, want help", attempt.Stage)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.As(err, &attempt) {
			t.Fatalf("imported verify of unreadable help err = %v, want a *ProbeExecutionError", err)
		}
	})
}

// TestSealDriftRefusedOnEveryForm pins help drift refusal on all three
// verifier forms: the direct pre-exec check, the imported pre-exec check
// and the finalized plan check. One form bypassing the comparison admits
// a yolo plan for a binary whose help no longer declares the flag.
func TestSealDriftRefusedOnEveryForm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, helpFile, env := writeMuseMutableHelpStub(t, dir,
		"Muse Code 1.4.1 (1.4.1-R4503.1)", museYoloHelpFixture)
	plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
	})
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	imported, err := New().ImportSealedData(*seal.Data.Sealed)
	if err != nil {
		t.Fatalf("ImportSealedData: %v", err)
	}
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized verify of unchanged help: %v", err)
	}
	// Drift keeps the declaration: the refusal must come from the digest
	// comparison on every form, which is the branch the M43 mutant drops
	// for imported seals.
	if err := os.WriteFile(helpFile, []byte(museYoloHelpFixture+"\n  --model <other>\n"), 0o644); err != nil {
		t.Fatalf("rewriting help: %v", err)
	}
	if err := plan.VerifyBeforeExec(); !errors.Is(err, ErrMuseSealHelpChanged) {
		t.Fatalf("direct form err = %v, want ErrMuseSealHelpChanged", err)
	}
	if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealHelpChanged) {
		t.Fatalf("imported form err = %v, want ErrMuseSealHelpChanged", err)
	}
	if err := final.VerifyBeforeExec(); !errors.Is(err, ErrMuseSealHelpChanged) {
		t.Fatalf("finalized form err = %v, want ErrMuseSealHelpChanged", err)
	}
}

// TestSealedImportRefusesSelfMintedEvidence pins import authenticity: a
// release outside the grammar refuses on both importers, and a
// well-formed but forged release imports but never verifies — the
// re-probed attestation disagrees. The keyed importer enforces the same
// shape the direct one does.
func TestSealedImportRefusesSelfMintedEvidence(t *testing.T) {
	t.Parallel()
	plan := buildMuseSealedPlan(t, museSealedInteractiveRequest(t))
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	clone := func(t *testing.T) agentic.SealedData {
		t.Helper()
		data := *seal.Data.Sealed
		data.Selectors = make(map[string]string, len(seal.Data.Sealed.Selectors))
		for name, value := range seal.Data.Sealed.Selectors {
			data.Selectors[name] = value
		}
		return data
	}
	key := museSealTestKey(7)
	verifier, err := New().SealExecPlan(plan)
	if err != nil {
		t.Fatalf("SealExecPlan: %v", err)
	}
	keyed, ok := verifier.(agentic.ExecSealKeyedExporter)
	if !ok {
		t.Fatal("muse seal is not a keyed exporter")
	}
	keyedData := keyed.ExportSealedDataWithKey(key)
	cloneKeyed := func(t *testing.T) agentic.SealedData {
		t.Helper()
		data := keyedData
		data.Selectors = make(map[string]string, len(keyedData.Selectors))
		for name, value := range keyedData.Selectors {
			data.Selectors[name] = value
		}
		return data
	}
	t.Run("malformed-direct", func(t *testing.T) {
		data := clone(t)
		data.Selectors[sealedReleaseKey] = "1.5.0"
		if _, err := New().ImportSealedData(data); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("direct import of malformed release err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("malformed-keyed", func(t *testing.T) {
		data := cloneKeyed(t)
		data.Selectors[sealedReleaseKey] = "1.5.0"
		if _, err := New().ImportSealedDataWithKey(data, key); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("keyed import of malformed release err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("forged-direct", func(t *testing.T) {
		data := clone(t)
		data.Selectors[sealedReleaseKey] = "9.9.9-R1.1"
		imported, err := New().ImportSealedData(data)
		if err != nil {
			t.Fatalf("direct import of well-formed forged release: %v", err)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealReleaseChanged) {
			t.Fatalf("verify of forged release err = %v, want ErrMuseSealReleaseChanged", err)
		}
	})
	t.Run("forged-keyed", func(t *testing.T) {
		data := cloneKeyed(t)
		data.Selectors[sealedReleaseKey] = "9.9.9-R1.1"
		imported, err := New().ImportSealedDataWithKey(data, key)
		if err != nil {
			t.Fatalf("keyed import of well-formed forged release: %v", err)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealReleaseChanged) {
			t.Fatalf("keyed verify of forged release err = %v, want ErrMuseSealReleaseChanged", err)
		}
	})
}

// TestSealedImportStaleHelpDigestRefuses pins the yolo import shape and
// its reverification: a yolo seal without help selectors refuses on both
// importers — dropping required evidence is malformed, not native — and
// well-formed but stale digests import but never verify.
func TestSealedImportStaleHelpDigestRefuses(t *testing.T) {
	t.Parallel()
	yoloReq := museInteractiveRequest(t)
	yoloReq.PermissionMode = agentic.PermissionModeYolo
	yoloReq.ToolRelease = verifiedMuseRelease
	plan := buildMuseInteractivePlan(t, New(), yoloReq)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	clone := func(t *testing.T) agentic.SealedData {
		t.Helper()
		data := *seal.Data.Sealed
		data.Selectors = make(map[string]string, len(seal.Data.Sealed.Selectors))
		for name, value := range seal.Data.Sealed.Selectors {
			data.Selectors[name] = value
		}
		return data
	}
	key := museSealTestKey(11)
	verifier, err := New().SealExecPlan(plan)
	if err != nil {
		t.Fatalf("SealExecPlan: %v", err)
	}
	keyed, ok := verifier.(agentic.ExecSealKeyedExporter)
	if !ok {
		t.Fatal("muse seal is not a keyed exporter")
	}
	keyedData := keyed.ExportSealedDataWithKey(key)
	cloneKeyed := func(t *testing.T) agentic.SealedData {
		t.Helper()
		data := keyedData
		data.Selectors = make(map[string]string, len(keyedData.Selectors))
		for name, value := range keyedData.Selectors {
			data.Selectors[name] = value
		}
		return data
	}
	t.Run("missing-direct", func(t *testing.T) {
		data := clone(t)
		delete(data.Selectors, sealedHelpStdoutKey)
		if _, err := New().ImportSealedData(data); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("direct import without help digests err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("missing-keyed", func(t *testing.T) {
		data := cloneKeyed(t)
		delete(data.Selectors, sealedHelpStdoutKey)
		if _, err := New().ImportSealedDataWithKey(data, key); !errors.Is(err, ErrMuseSealMalformed) {
			t.Fatalf("keyed import without help digests err = %v, want ErrMuseSealMalformed", err)
		}
	})
	t.Run("stale-direct", func(t *testing.T) {
		data := clone(t)
		data.Selectors[sealedHelpStdoutKey] = strings.Repeat("1", 64)
		imported, err := New().ImportSealedData(data)
		if err != nil {
			t.Fatalf("direct import of stale help digests: %v", err)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("verify of stale help digests err = %v, want ErrMuseSealHelpChanged", err)
		}
	})
	t.Run("stale-keyed", func(t *testing.T) {
		data := cloneKeyed(t)
		data.Selectors[sealedHelpLinesKey] = strings.Repeat("2", 64)
		imported, err := New().ImportSealedDataWithKey(data, key)
		if err != nil {
			t.Fatalf("keyed import of stale help digests: %v", err)
		}
		if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("keyed verify of stale help digests err = %v, want ErrMuseSealHelpChanged", err)
		}
	})
}

// TestSealNeverExecsUnfrozenOnOverridePath pins frozen-tuple enforcement
// across every probe: with the tuple present, argv mapping probes nothing
// and sealing executes the frozen copy only. The PATH binary documents no
// bypass flag and records any execution, so resolving PATH either refuses
// or leaves the marker — both fail the test.
func TestSealNeverExecsUnfrozenOnOverridePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	frozen, digest := writeFrozenMuseFixture(t, dir, "9.9.9-R1.1",
		"Muse Code 9.9.9 (9.9.9-R1.1)", museYoloHelpFixture)
	pathDir := t.TempDir()
	marker := filepath.Join(pathDir, "path-muse-ran")
	pathStub := filepath.Join(pathDir, "muse")
	pathScript := "#!/bin/sh\n" +
		"touch '" + marker + "'\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		"printf '%s\\n' 'Muse Code 1.4.1 (1.4.1-R4503.1)'\n" +
		"exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		"printf '%s\\n' '" + museNoYoloHelpFixture + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"exit 8\n"
	if err := os.WriteFile(pathStub, []byte(pathScript), 0o755); err != nil {
		t.Fatalf("writing the PATH stub: %v", err)
	}
	req := launchRequest(t, "")
	// The PATH stub needs touch; /bin and /usr/bin carry no muse, so the
	// frozen copy is still the only selectable one.
	req.Env = []string{"PATH=" + pathDir + ":/bin:/usr/bin"}
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "9.9.9"
	req.FrozenToolBinary = frozen
	req.FrozenToolBuild = "9.9.9-R1.1"
	req.FrozenToolSHA256 = digest
	plan := buildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
	if plan.Binary != frozen {
		t.Fatalf("plan binary = %q, want the frozen copy %q", plan.Binary, frozen)
	}
	if err := assertMuseYoloExactlyOnce(plan); err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec on the frozen yolo plan: %v", err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Sealed.Selectors[sealedReleaseKey] != "9.9.9-R1.1" {
		t.Fatalf("sealed release = %q, want the frozen attested build",
			seal.Data.Sealed.Selectors[sealedReleaseKey])
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("PATH muse executed during the frozen plan (marker err = %v)", err)
	}
}

// TestSealReprobeTimeoutSurfacesAttemptError pins the seal re-probe error
// surface: a version probe that times out and one that exits nonzero both
// surface the typed attempt record — never a bare static refusal — so the
// host can route the recorded-failure path. Only an error the invoked
// module probe returned may become a host attempt event.
func TestSealReprobeTimeoutSurfacesAttemptError(t *testing.T) {
	t.Parallel()
	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		_, env := writeMuseVersionStub(t, "sleep 60\n")
		env[0] += ":/bin:/usr/bin"
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			PermissionMode: agentic.PermissionModeNative,
		}, agentic.LaunchModeInteractive)
		var attempt *agentic.ProbeExecutionError
		if !errors.As(err, &attempt) {
			t.Fatalf("hanging version probe err = %v, want a *ProbeExecutionError", err)
		}
		if !attempt.Timeout || !attempt.ExecAttempted || !attempt.ChildStarted {
			t.Fatalf("timeout attempt = %+v, want timeout with exec attempted and child started", attempt)
		}
		if attempt.Stage != agentic.ProbeStageVersion {
			t.Fatalf("timeout attempt stage = %q, want version", attempt.Stage)
		}
		if !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Fatalf("hanging version probe err = %v, want ErrToolReleaseUndetected too", err)
		}
	})
	t.Run("nonzero", func(t *testing.T) {
		t.Parallel()
		_, env := writeMuseVersionStub(t, "exit 3\n")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			PermissionMode: agentic.PermissionModeNative,
		}, agentic.LaunchModeInteractive)
		var attempt *agentic.ProbeExecutionError
		if !errors.As(err, &attempt) {
			t.Fatalf("failing version probe err = %v, want a *ProbeExecutionError", err)
		}
		if attempt.Timeout || !attempt.ExecAttempted || !attempt.ChildStarted {
			t.Fatalf("exit attempt = %+v, want exec attempted and child started without timeout", attempt)
		}
		if !errors.Is(err, agentic.ErrToolReleaseUndetected) {
			t.Fatalf("failing version probe err = %v, want ErrToolReleaseUndetected too", err)
		}
	})
}

// TestMuseSealEstablishmentRefusesHelpChangeMidSeal pins seal-time help
// enforcement: the seal binds the first help answer, and the
// establishment re-verification refuses any degradation before the plan
// exists — a lost declaration as a gone declaration, a failing probe as
// attempt evidence. The bound answer never authorizes a changed one.
func TestMuseSealEstablishmentRefusesHelpChangeMidSeal(t *testing.T) {
	t.Parallel()
	t.Run("declaration-lost", func(t *testing.T) {
		t.Parallel()
		env := writeMuseHelpCounterStub(t, t.TempDir(),
			"Muse Code 1.4.1 (1.4.1-R4503.1)", museYoloHelpFixture,
			"printf '%s\\n' '"+museNoYoloHelpFixture+"'")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
		}, agentic.LaunchModeInteractive)
		if !errors.Is(err, ErrMuseSealHelpChanged) {
			t.Fatalf("mid-seal declaration loss err = %v, want ErrMuseSealHelpChanged", err)
		}
	})
	t.Run("probe-fails", func(t *testing.T) {
		t.Parallel()
		env := writeMuseHelpCounterStub(t, t.TempDir(),
			"Muse Code 1.4.1 (1.4.1-R4503.1)", museYoloHelpFixture, "exit 1")
		_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
			System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
			ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
		}, agentic.LaunchModeInteractive)
		var attempt *agentic.ProbeExecutionError
		if !errors.As(err, &attempt) {
			t.Fatalf("mid-seal help failure err = %v, want a *ProbeExecutionError", err)
		}
		if attempt.Stage != agentic.ProbeStageHelp {
			t.Fatalf("mid-seal help failure stage = %q, want help", attempt.Stage)
		}
	})
}

// TestMuseSealEstablishmentRefusesRetainedDeclarationDrift pins the
// establishment digest comparison: a help text that keeps its --yolo
// declaration but changes bytes between binding and the establishment
// re-verification refuses — the seal binds one answer, not a drifting
// stream. Skipping the establishment re-verification for help drift is
// what the drift mutant does.
func TestMuseSealEstablishmentRefusesRetainedDeclarationDrift(t *testing.T) {
	t.Parallel()
	laterHelp := museYoloHelpFixture + "\n  --yolo-extra documented\n"
	env := writeMuseHelpCounterStub(t, t.TempDir(),
		"Muse Code 1.4.1 (1.4.1-R4503.1)", museYoloHelpFixture,
		"printf '%s\\n' '"+laterHelp+"'")
	_, err := tryBuildMusePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
	}, agentic.LaunchModeInteractive)
	if !errors.Is(err, ErrMuseSealHelpChanged) {
		t.Fatalf("establishment admitted retained-declaration help drift: %v", err)
	}
}

// writeMuseProbeOrderStub installs a binary answering --version with the
// fixed answer and --help with the fixed text, appending "version" or
// "help" to its order file on every probe, so establishment tests can
// assert probe order. It returns the order file path and a PATH-only env.
func writeMuseProbeOrderStub(t *testing.T, dir, answer, help string) (string, []string) {
	t.Helper()
	order := filepath.Join(dir, "probe-order")
	binary := filepath.Join(dir, "muse")
	script := "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then\n" +
		"printf 'version\\n' >> '" + order + "'\n" +
		"printf '%s\\n' '" + answer + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then\n" +
		"[ \"$MUSE_NO_AUTO_UPDATE\" = '1' ] || exit 13\n" +
		"printf 'help\\n' >> '" + order + "'\n" +
		"printf '%s\\n' '" + help + "'\n" +
		"exit 0\n" +
		"fi\n" +
		"exit 8\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the order stub: %v", err)
	}
	return order, []string{"PATH=" + dir + ":/bin:/usr/bin"}
}

// TestMuseSealEstablishmentProbesVersionBeforeHelp pins the establishment
// probe order through the public entry: version attestation runs before
// the help probe, and the immediate re-verification repeats the pair, so
// a yolo plan observes exactly version, help, version, help. Probing
// help before version attestation is what the ordering mutant does.
func TestMuseSealEstablishmentProbesVersionBeforeHelp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	order, env := writeMuseProbeOrderStub(t, dir,
		"Muse Code 1.4.1 (1.4.1-R4503.1)", museYoloHelpFixture)
	plan := buildMuseInteractivePlan(t, New(), agentic.LaunchRequest{
		System: systemID, Model: agentic.Model{ID: "echo"}, Env: env,
		ToolRelease: "1.4.1", PermissionMode: agentic.PermissionModeYolo,
	})
	if err := assertMuseYoloExactlyOnce(plan); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(order)
	if err != nil {
		t.Fatalf("reading the probe order: %v", err)
	}
	const want = "version\nhelp\nversion\nhelp"
	if got := strings.TrimSpace(string(raw)); got != want {
		t.Fatalf("establishment probe order = %q, want %q",
			strings.Split(got, "\n"), strings.Split(want, "\n"))
	}
}
