package agentic_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
)

func sealTestRegistry(t *testing.T) *agentic.Registry {
	t.Helper()
	registry := agentic.NewRegistry()
	for _, sys := range []agentic.System{claude.New(), codex.New(), muse.New()} {
		if err := registry.Register(sys); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	return registry
}

func sealTestStubBin(t *testing.T, name string) string {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write stub executable: %v", err)
	}
	return binDir
}

func sealTestClaudePlan(t *testing.T) agentic.Plan {
	t.Helper()
	binDir := sealTestStubBin(t, "claude")
	plan, err := agentic.BuildPlan(sealTestRegistry(t), agentic.LaunchRequest{
		System:  claude.New().ID(),
		Model:   agentic.Model{ID: "seal-test"},
		WorkDir: t.TempDir(),
		Env:     []string{"PATH=" + binDir},
	}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan Claude: %v", err)
	}
	return plan
}

func sealTestMusePlan(t *testing.T) agentic.Plan {
	t.Helper()
	binDir := sealTestStubBin(t, "muse")
	plan, err := agentic.BuildPlan(sealTestRegistry(t), agentic.LaunchRequest{
		System:  muse.New().ID(),
		Model:   agentic.Model{ID: "seal-test"},
		WorkDir: t.TempDir(),
		Env:     []string{"PATH=" + binDir},
		Run:     agentic.RunContext{RunID: "RUN-seal-test", TaskID: "TASK-seal-test"},
	}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan Muse: %v", err)
	}
	return plan
}

func TestClaudePlanExportsUnsealedGuard(t *testing.T) {
	plan := sealTestClaudePlan(t)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Schema != agentic.ExecGuardSchema || seal.SchemaVersion != agentic.ExecGuardVersion {
		t.Fatalf("envelope = %q %q, want the exec-guard 1.0.0 schema", seal.Schema, seal.SchemaVersion)
	}
	if seal.Data.Kind != agentic.SealKindUnsealed || seal.Data.Sealed != nil {
		t.Fatalf("data = %+v, want exactly kind unsealed", seal.Data)
	}
	if !seal.HostedAdmissible() {
		t.Fatal("Claude unsealed guard is not hosted-admissible")
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatalf("marshal seal: %v", err)
	}
	want := `{"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0","data":{"kind":"unsealed"}}`
	if string(wire) != want {
		t.Fatalf("wire seal = %s, want %s", wire, want)
	}
}

func TestMusePlanRefusesSealExport(t *testing.T) {
	plan := sealTestMusePlan(t)
	if _, err := plan.ExportSeal(); !errors.Is(err, agentic.ErrSealNotExportable) {
		t.Fatalf("ExportSeal err = %v, want ErrSealNotExportable", err)
	}
}

func TestImportSealAdmitsUnsealedForClaudeOnly(t *testing.T) {
	claudePlan := sealTestClaudePlan(t)
	seal, err := claudePlan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	t.Run("claude", func(t *testing.T) {
		verifier, err := agentic.ImportSeal(claude.New(), seal)
		if err != nil {
			t.Fatalf("ImportSeal Claude: %v", err)
		}
		if verifier == nil {
			t.Fatal("ImportSeal Claude yielded no verifier")
		}
		if err := verifier.VerifyBeforeExec(claudePlan); err != nil {
			t.Fatalf("imported unsealed verifier refused the untouched plan: %v", err)
		}
	})
	t.Run("muse", func(t *testing.T) {
		sealTestMusePlan(t)
		// Muse reaches the sealer no-downgrade gate first: it declares a
		// sealer, so the refusal must name that gate. The narrowing mutant
		// admits exactly muse past this gate; the opt-in gate still refuses,
		// so the reason changes and this assertion fails.
		_, err := agentic.ImportSeal(muse.New(), seal)
		if !errors.Is(err, agentic.ErrSealUnsealedRefused) || !strings.Contains(err.Error(), "declares a sealer") {
			t.Fatalf("muse sealer refusal lost: %v", err)
		}
	})
	t.Run("codex", func(t *testing.T) {
		// Control: the narrowing admits only muse past the sealer gate, so
		// sealed-system codex keeps refusing at the same gate.
		_, err := agentic.ImportSeal(codex.New(), seal)
		if !errors.Is(err, agentic.ErrSealUnsealedRefused) || !strings.Contains(err.Error(), "declares a sealer") {
			t.Fatalf("sealed-system codex sealer refusal lost: %v", err)
		}
	})
}

func TestImportSealRefusesUnknownSchema(t *testing.T) {
	plan := sealTestClaudePlan(t)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	seal.Schema = "urn:relux:agents-management:exec-guard-v2"
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealUnknownSchema) {
		t.Fatalf("ImportSeal err = %v, want ErrSealUnknownSchema", err)
	}
}

func TestImportSealRefusesUnknownVersion(t *testing.T) {
	plan := sealTestClaudePlan(t)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	seal.SchemaVersion = "2.0.0"
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealUnknownVersion) {
		t.Fatalf("ImportSeal err = %v, want ErrSealUnknownVersion", err)
	}
}

func TestImportSealRefusesUnknownKind(t *testing.T) {
	plan := sealTestClaudePlan(t)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	seal.Data.Kind = "sealed-v2"
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealMalformed) {
		t.Fatalf("ImportSeal err = %v, want ErrSealMalformed", err)
	}
}

func TestImportSealRefusesUnsealedCarryingSealedPayload(t *testing.T) {
	plan := sealTestClaudePlan(t)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	seal.Data.Sealed = &agentic.SealedData{Binary: "smuggled"}
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealMalformed) {
		t.Fatalf("ImportSeal err = %v, want ErrSealMalformed", err)
	}
}

func TestImportSealRefusesTruncatedSealedPayload(t *testing.T) {
	plan := sealTestClaudePlan(t)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	seal.Data.Kind = agentic.SealKindSealed
	seal.Data.Sealed = nil
	if _, err := agentic.ImportSeal(codex.New(), seal); !errors.Is(err, agentic.ErrSealMalformed) {
		t.Fatalf("ImportSeal err = %v, want ErrSealMalformed for a missing payload", err)
	}
	seal.Data.Sealed = &agentic.SealedData{}
	if _, err := agentic.ImportSeal(codex.New(), seal); !errors.Is(err, agentic.ErrSealMalformed) {
		t.Fatalf("ImportSeal err = %v, want ErrSealMalformed for an empty payload", err)
	}
}

func TestImportSealRefusesTamperedSealedPayload(t *testing.T) {
	sealTestClaudePlan(t)
	seal := agentic.Seal{
		Schema:        agentic.ExecGuardSchema,
		SchemaVersion: agentic.ExecGuardVersion,
		Data: agentic.SealData{Kind: agentic.SealKindSealed, Sealed: &agentic.SealedData{
			System:    "codex",
			Binary:    "/bin/codex",
			Argv:      []string{"exec"},
			Artifacts: []agentic.SealedArtifact{},
			Digest:    "sha256:" + strings.Repeat("0", 64),
		}},
	}
	if _, err := agentic.ImportSeal(codex.New(), seal); !errors.Is(err, agentic.ErrSealTampered) {
		t.Fatalf("ImportSeal err = %v, want ErrSealTampered", err)
	}
}
