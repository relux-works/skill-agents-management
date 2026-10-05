package codex

import (
	"errors"
	"slices"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// importedNilImporter is Codex with a silent sealed importer: it proves
// NewImportedProcess refuses a (nil, nil) import rather than binding a
// process to no verifier. Only a fixture answers this way.
type importedNilImporter struct{ *System }

func (importedNilImporter) ImportSealedData(agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	return nil, nil
}

func importedCodexPlan(t *testing.T, newlines int) (agentic.Plan, agentic.Seal) {
	t.Helper()
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	if newlines > 0 {
		sealUniqueCatalog(t, home, newlines)
	}
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	return plan, seal
}

func TestNewImportedProcessRefusesNilVerifier(t *testing.T) {
	plan, seal := importedCodexPlan(t, 0)
	if _, err := agentic.NewImportedProcess(importedNilImporter{New()}, seal, plan); !errors.Is(err, agentic.ErrImportedVerifierMissing) {
		t.Fatalf("nil import err = %v, want ErrImportedVerifierMissing", err)
	}
}

func TestImportedProcessDelegatesToSealedVerifier(t *testing.T) {
	plan, seal := importedCodexPlan(t, 13)
	witness := plan
	witness.Env = append(slices.Clone(plan.Env), "IMPORTED_WITNESS=skip-delegation")
	imported, err := agentic.NewImportedProcess(New(), seal, witness)
	if err != nil {
		t.Fatalf("NewImportedProcess witness: %v", err)
	}
	control, err := agentic.NewImportedProcess(New(), seal, plan)
	if err != nil {
		t.Fatalf("NewImportedProcess control: %v", err)
	}
	if err := imported.VerifyBeforeExec(witness); err != nil {
		t.Fatalf("VerifyBeforeExec witness untouched: %v", err)
	}
	if err := control.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec control untouched: %v", err)
	}
	artifact := effectiveCatalogPath(t, plan)
	sealSwappedArtifact(t, artifact, sealCatalogBytes(t, artifact))
	// The exact process still matches on every bound fact; only the
	// delegate re-reading the catalog can refuse. The narrowing mutant
	// skips the delegate call for exactly the witness env entry, so this
	// assertion fails while the control below keeps refusing.
	if err := imported.VerifyBeforeExec(witness); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("delegation skipped for witness env: %v", err)
	}
	if err := control.VerifyBeforeExec(plan); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("swapped catalog admitted without witness: %v", err)
	}
}

func TestNewImportedProcessBindsSealedCodex(t *testing.T) {
	plan, seal := importedCodexPlan(t, 0)
	imported, err := agentic.NewImportedProcess(New(), seal, plan)
	if err != nil {
		t.Fatalf("NewImportedProcess: %v", err)
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec untouched: %v", err)
	}
	drifted := plan
	drifted.Env = append(slices.Clone(plan.Env), "IMPORTED_DRIFT=1")
	if err := imported.VerifyBeforeExec(drifted); !errors.Is(err, agentic.ErrImportedProcessChanged) {
		t.Fatalf("env drift err = %v, want ErrImportedProcessChanged", err)
	}
}
