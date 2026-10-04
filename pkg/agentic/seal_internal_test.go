package agentic

import (
	"errors"
	"strings"
	"testing"
)

// The paths below are unreachable through BuildPlan: every verifier the
// registry path installs is nil, unsealed, plugin-sealed, or finalized, and
// no production importer returns a verifier without an artifact basis.
// These tests drive the production entry points — ExportSeal, FinalizePlan,
// ImportSeal — over fixture verifiers instead, and the refusal guard maps
// those sites out of contract with these test names.

type sealUnknownVerifier struct{}

func (sealUnknownVerifier) VerifyBeforeExec(Plan) error { return nil }

func TestExportSealRefusesUnknownVerifierType(t *testing.T) {
	plan := Plan{System: "claude-code", Binary: "claude", execVerifier: sealUnknownVerifier{}}
	if _, err := plan.ExportSeal(); !errors.Is(err, ErrSealNotExportable) {
		t.Fatalf("ExportSeal err = %v, want ErrSealNotExportable", err)
	}
}

func TestFinalizePlanRefusesUnknownVerifierType(t *testing.T) {
	plan := Plan{System: "claude-code", Binary: "claude", execVerifier: sealUnknownVerifier{}}
	if _, err := FinalizePlan(plan, FinalizeOverlays{}, nil); !errors.Is(err, ErrFinalizeNoSealBasis) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeNoSealBasis", err)
	}
}

// sealImportingSystem is a plugin-shaped importer whose sealed verifier
// binds no artifacts: it proves ImportSeal refuses selector-carrying sealed
// data rather than silently dropping the artifact half of the binding. No
// production plugin answers this way; only a fixture can.
type sealImportingSystem struct{}

func (sealImportingSystem) ID() SystemID { return "seal-fixture" }
func (sealImportingSystem) Capabilities() Capabilities {
	return Capabilities{LaunchModes: []LaunchMode{LaunchModeExec}, EffortTransport: EffortTransportNone}
}
func (sealImportingSystem) ResolveBinary(LaunchRequest) (string, error) { return "seal-fixture", nil }
func (sealImportingSystem) Argv(LaunchRequest, LaunchMode) ([]string, error) {
	return nil, nil
}
func (sealImportingSystem) ChildEnv(parent []string, _ LaunchRequest) ([]string, error) {
	return parent, nil
}
func (sealImportingSystem) Stdin(LaunchRequest) (StdinPayload, error) {
	return StdinPayload{}, nil
}
func (sealImportingSystem) ValidateComposition(Composition) error { return nil }
func (sealImportingSystem) ImportSealedData(SealedData) (ExecPlanVerifier, error) {
	return unsealedVerifier{}, nil
}

// sealBareSystem is a plugin-shaped system with no sealed importer: it
// proves ImportSeal refuses sealed data naming a system that implements no
// importer. Only a fixture can hold a valid integrity digest for a
// hand-made payload, so this path is unreachable through BuildPlan. It
// deliberately embeds nothing: embedding the importing fixture would
// promote its ImportSealedData and defeat the test.
type sealBareSystem struct{}

func (sealBareSystem) ID() SystemID { return "seal-fixture" }
func (sealBareSystem) Capabilities() Capabilities {
	return Capabilities{LaunchModes: []LaunchMode{LaunchModeExec}, EffortTransport: EffortTransportNone}
}
func (sealBareSystem) ResolveBinary(LaunchRequest) (string, error) { return "seal-fixture", nil }
func (sealBareSystem) Argv(LaunchRequest, LaunchMode) ([]string, error) {
	return nil, nil
}
func (sealBareSystem) ChildEnv(parent []string, _ LaunchRequest) ([]string, error) {
	return parent, nil
}
func (sealBareSystem) Stdin(LaunchRequest) (StdinPayload, error) {
	return StdinPayload{}, nil
}
func (sealBareSystem) ValidateComposition(Composition) error { return nil }

func TestImportSealRefusesSystemWithoutImporter(t *testing.T) {
	data := SealedData{System: "seal-fixture", Binary: "seal-fixture", Argv: []string{}, Artifacts: []SealedArtifact{}}
	data.Digest = sealIntegrityDigest(data)
	seal := Seal{Schema: ExecGuardSchema, SchemaVersion: ExecGuardVersion, Data: SealData{Kind: SealKindSealed, Sealed: &data}}
	if _, err := ImportSeal(sealBareSystem{}, seal); !errors.Is(err, ErrSealImportRefused) {
		t.Fatalf("ImportSeal err = %v, want ErrSealImportRefused", err)
	}
}

func TestImportSealRefusesSelectorsWithoutArtifactBasis(t *testing.T) {
	data := SealedData{
		System:    "seal-fixture",
		Binary:    "seal-fixture",
		Argv:      []string{"--fixture"},
		Artifacts: []SealedArtifact{},
		Selectors: map[string]string{"SEAL_FIXTURE": "fixture"},
	}
	data.Digest = sealIntegrityDigest(data)
	seal := Seal{Schema: ExecGuardSchema, SchemaVersion: ExecGuardVersion, Data: SealData{Kind: SealKindSealed, Sealed: &data}}
	if _, err := ImportSeal(sealImportingSystem{}, seal); !errors.Is(err, ErrSealImportRefused) {
		t.Fatalf("ImportSeal err = %v, want ErrSealImportRefused", err)
	}
}

// sealUnrepresentableExporter is a fixture sealed verifier whose artifact
// path cannot survive guard JSON. Only a fixture reaches the export-time
// representability gate: BuildPlan refuses invalid UTF-8 binary/argv/env
// first, and production artifact paths are temp-dir plus hex digest.
type sealUnrepresentableExporter struct{ data SealedData }

func (sealUnrepresentableExporter) VerifyBeforeExec(Plan) error { return nil }
func (s sealUnrepresentableExporter) ExportSealedData() SealedData {
	return s.data
}

func TestExportSealRefusesUnrepresentableSealedData(t *testing.T) {
	bad := string([]byte{0xff})
	for _, tc := range []struct {
		name string
		data SealedData
	}{
		{name: "artifact-path", data: SealedData{Binary: "seal-fixture", Argv: []string{}, Artifacts: []SealedArtifact{{Name: "catalog.json", Path: "/tmp/" + bad, Digest: "sha256:" + strings.Repeat("0", 64)}}}},
		{name: "binary", data: SealedData{Binary: "seal-" + bad, Argv: []string{}, Artifacts: []SealedArtifact{}}},
		{name: "argv", data: SealedData{Binary: "seal-fixture", Argv: []string{bad}, Artifacts: []SealedArtifact{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := Plan{System: "codex", Binary: "seal-fixture", execVerifier: sealUnrepresentableExporter{data: tc.data}}
			if _, err := plan.ExportSeal(); !errors.Is(err, ErrSealMalformed) {
				t.Fatalf("ExportSeal err = %v, want ErrSealMalformed", err)
			}
		})
	}
}

func TestExportSealRefusesUnrepresentableEnvelope(t *testing.T) {
	// A fixture finalized verifier reaches the common export boundary; normal
	// FinalizePlan already refuses this binary before binding it.
	p := Plan{Binary: "bad-\xff"}
	p.execVerifier = finalizedUnsealedVerifier{bindings: bindFinalProcess(p, processCommitmentKey)}
	if _, err := p.ExportSeal(); !errors.Is(err, ErrSealMalformed) {
		t.Fatalf("invalid finalized export admitted: %v", err)
	}
}
