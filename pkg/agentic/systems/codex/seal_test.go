package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

// sealUniqueCatalog appends newlines to the test catalog so the materialized
// launch artifact is unique to this test: identical bytes would share the
// process-wide artifact with another test, and a swap would leak across
// tests. Counts 7/9/11 stay distinct from the existing 3- and 5-newline
// tamper tests.
func sealUniqueCatalog(t *testing.T, home string, newlines int) {
	t.Helper()
	path := filepath.Join(home, "catalog.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(strings.Repeat("\n", newlines))...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sealSwappedArtifact(t *testing.T, artifact string, data []byte) {
	t.Helper()
	if err := os.Chmod(artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifact, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(artifact, 0o600); err != nil {
			t.Errorf("restore catalog mode: %v", err)
			return
		}
		if err := os.WriteFile(artifact, data, 0o600); err != nil {
			t.Errorf("restore catalog bytes: %v", err)
			return
		}
		if err := os.Chmod(artifact, 0o400); err != nil {
			t.Errorf("restore catalog readonly mode: %v", err)
		}
	})
}

func sealCatalogBytes(t *testing.T, artifact string) []byte {
	t.Helper()
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExportSealBindsCatalogProcess(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
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
	if data.System != "codex" || data.Binary != plan.Binary || strings.Join(data.Argv, "\x00") != strings.Join(plan.Argv, "\x00") {
		t.Fatal("sealed payload does not bind the plan process")
	}
	if len(data.Artifacts) != 1 || data.Artifacts[0].Name != "catalog.json" {
		t.Fatalf("artifacts = %+v, want exactly the catalog", data.Artifacts)
	}
	if !strings.HasPrefix(data.Artifacts[0].Digest, "sha256:") || !strings.HasPrefix(data.Digest, "sha256:") {
		t.Fatalf("digests = %q / %q, want sha256-prefixed hex", data.Artifacts[0].Digest, data.Digest)
	}
	if seal.HostedAdmissible() {
		t.Fatal("sealed guard is hosted-admissible before closed sealed schemas land")
	}
}

func TestImportSealAcceptsUntouchedSealedProcess(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	if err := verifier.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("imported verifier refused the untouched process: %v", err)
	}
	moved := plan
	moved.Binary += "-changed"
	if err := verifier.VerifyBeforeExec(moved); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("changed binary admitted: %v", err)
	}
	extended := plan
	extended.Argv = append(append([]string(nil), plan.Argv...), "--extra")
	if err := verifier.VerifyBeforeExec(extended); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("changed argv admitted: %v", err)
	}
}

func TestImportSealRefusesSwappedCatalog(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	sealUniqueCatalog(t, home, 7)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	artifact := effectiveCatalogPath(t, plan)
	original := sealCatalogBytes(t, artifact)
	if _, err := agentic.ImportSeal(New(), seal); err != nil {
		t.Fatalf("ImportSeal refused the unswapped catalog: %v", err)
	}
	sealSwappedArtifact(t, artifact, original)
	if _, err := agentic.ImportSeal(New(), seal); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("swapped catalog admitted at import: %v", err)
	}
}

func TestImportedSealRefusesLaterCatalogSwap(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	sealUniqueCatalog(t, home, 9)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	artifact := effectiveCatalogPath(t, plan)
	sealSwappedArtifact(t, artifact, sealCatalogBytes(t, artifact))
	if err := verifier.VerifyBeforeExec(plan); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("post-import catalog swap admitted: %v", err)
	}
}

func TestImportSealRefusesTamperedSealedArgv(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	seal.Data.Sealed.Argv = append(append([]string(nil), seal.Data.Sealed.Argv...), "--tampered")
	if _, err := agentic.ImportSeal(New(), seal); !errors.Is(err, agentic.ErrSealTampered) {
		t.Fatalf("ImportSeal err = %v, want ErrSealTampered", err)
	}
}

// TestImportSealedDataRefusesMalformedArtifacts drives the exported plugin
// importer directly: any mutation of an exported payload invalidates the
// envelope integrity digest first, so ImportSeal would name tampering
// instead of the artifact defect. The ImportSeal-to-plugin wiring is proven
// separately by the round-trip and swapped-catalog tests above.
func TestImportSealedDataRefusesMalformedArtifacts(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*agentic.SealedData)
	}{
		{name: "no-artifacts", mutate: func(data *agentic.SealedData) { data.Artifacts = nil }},
		{name: "wrong-name", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Name = "other.json" }},
		{name: "relative-path", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Path = "catalog.json" }},
		{name: "bad-digest", mutate: func(data *agentic.SealedData) { data.Artifacts[0].Digest = "sha256:zzz" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := *seal.Data.Sealed
			data.Artifacts = append([]agentic.SealedArtifact(nil), seal.Data.Sealed.Artifacts...)
			tc.mutate(&data)
			if _, err := New().ImportSealedData(data); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("ImportSealedData err = %v, want ErrLocalProviderMalformed", err)
			}
		})
	}
}

func TestImportSealRefusesSealedForAnotherSystem(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if _, err := agentic.ImportSeal(claude.New(), seal); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatalf("ImportSeal err = %v, want ErrSealImportRefused", err)
	}
}

func TestFinalizeSealedPlanPreservesOriginalDigest(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	sealUniqueCatalog(t, home, 11)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	artifact := effectiveCatalogPath(t, plan)
	original := sealCatalogBytes(t, artifact)
	// The swap lands AFTER sealing and BEFORE finalization: finalization
	// must not look at the disk, so it succeeds and carries the ORIGINAL
	// digest forward instead of resealing the swapped bytes.
	sealSwappedArtifact(t, artifact, original)
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{
		NativeTail: []string{"--final-tail"},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan refused after an artifact swap: %v", err)
	}
	after, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if after.Data.Sealed.Artifacts[0].Digest != before.Data.Sealed.Artifacts[0].Digest {
		t.Fatal("finalized seal carries a re-minted artifact digest")
	}
	if err := final.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("swapped artifact admitted at verification: %v", err)
	}
	if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("base plan stopped refusing the swapped artifact: %v", err)
	}
}

func TestFinalizeSealedPlanRefusesMalformedOverlay(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{FragmentEnv: []string{"1BAD=oops"}}, nil); !errors.Is(err, agentic.ErrFinalizeOverlayMalformed) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeOverlayMalformed", err)
	}
}

func TestFinalizeSealedPlanRefusesBinding(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{}, []agentic.TypedBinding{{Kind: "future-kind"}}); !errors.Is(err, agentic.ErrFinalizeBindingUnknown) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeBindingUnknown", err)
	}
}

// TestFinalizeSealedPlanRefusesAReservation drives the sealed finalize path:
// Codex has no native session grammar for the managed-session slots, so a
// well-formed reservation with both slots still refuses typed.
func TestFinalizeSealedPlanRefusesAReservation(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	const ses, uuid = "SES-7f3a91c2", "0b9f8c2e-4d1a-4e6b-9c3d-2a5f7e8d1b04"
	_, err = agentic.FinalizePlan(plan, agentic.FinalizeOverlays{Reservation: &agentic.SessionReservation{SessionID: ses, NativeID: uuid}}, []agentic.TypedBinding{
		{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionEnvSlot, Value: ses},
		{Kind: agentic.BindingKindManagedSession, Name: agentic.ManagedSessionArgvSlot, Value: uuid},
	})
	if !errors.Is(err, agentic.ErrFinalizeBindingUnknown) {
		t.Fatalf("FinalizePlan err = %v, want ErrFinalizeBindingUnknown for a system with no native session grammar", err)
	}
}

func TestFinalizedSealedPlanRefusesChangedArgv(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{NativeTail: []string{"--final-tail"}}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	changed := final
	changed.Argv = append(append([]string(nil), final.Argv...), "--extra")
	if err := changed.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("changed finalized argv admitted: %v", err)
	}
}

func TestFinalizeSealedPlanExportBindsFinalProcess(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, wd, "low"))
	if err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{
		FragmentEnv: []string{"SEAL_CODEX_A=alpha"},
		NativeTail:  []string{"--final-tail"},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Kind != agentic.SealKindSealed {
		t.Fatalf("data kind = %q, want sealed", seal.Data.Kind)
	}
	if strings.Join(seal.Data.Sealed.Argv, "\x00") != strings.Join(final.Argv, "\x00") {
		t.Fatal("re-exported seal does not bind the final argv")
	}
	if seal.Data.Sealed.Binding == nil || len(seal.Data.Sealed.Selectors) != len(final.Env) {
		t.Fatal("missing keyed environment bindings")
	}
	if seal.HostedAdmissible() {
		t.Fatal("sealed guard is hosted-admissible before closed sealed schemas land")
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatalf("imported finalized verifier refused the final process: %v", err)
	}
	changed := final
	changed.Env = append([]string(nil), final.Env...)
	replaced := false
	for i, entry := range changed.Env {
		if name, _, ok := strings.Cut(entry, "="); ok && name == "SEAL_CODEX_A" {
			changed.Env[i] = "SEAL_CODEX_A=tampered"
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("selector missing from the final env")
	}
	if err := verifier.VerifyBeforeExec(changed); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("changed selector admitted after import: %v", err)
	}
}

func TestSealedExportCollectionsNeverAlias(t *testing.T) {
	p := panelPlan(t)
	f, err := agentic.FinalizePlan(p, agentic.FinalizeOverlays{PromptEnv: []string{"SELECTOR=synthetic-canary"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*agentic.SealedData){
		"artifacts":         func(d *agentic.SealedData) { d.Artifacts[0].Digest = "changed" },
		"argv":              func(d *agentic.SealedData) { d.Argv[0] = "changed" },
		"selectors":         func(d *agentic.SealedData) { d.Selectors["0"] = "changed" },
		"binding-argv":      func(d *agentic.SealedData) { d.Binding.Argv[0] = "changed" },
		"binding-env-names": func(d *agentic.SealedData) { d.Binding.EnvNames[0] = "changed" },
		"binding-selectors": func(d *agentic.SealedData) { d.Binding.Selectors["0"] = "changed" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			seal, _ := f.ExportSeal()
			before, _ := json.Marshal(seal)
			imported, err := agentic.ImportSeal(New(), seal)
			if err != nil {
				t.Fatal(err)
			}
			mutate(seal.Data.Sealed)
			next, _ := f.ExportSeal()
			after, _ := json.Marshal(next)
			if string(before) != string(after) {
				t.Fatal("sealed export collection aliases retained verifier")
			}
			if err := imported.VerifyBeforeExec(f); err != nil {
				t.Fatalf("sealed import aliases supplied collection: %v", err)
			}
		})
	}
}

func TestStrictSealedWireObjectClosure(t *testing.T) {
	p, err := agentic.FinalizePlan(panelPlan(t), agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, _ := p.ExportSeal()
	original, _ := json.Marshal(seal)
	for _, name := range []string{"envelope", "data", "sealed", "binding", "artifact"} {
		t.Run(name, func(t *testing.T) {
			var decoded map[string]any
			if err := json.Unmarshal(original, &decoded); err != nil {
				t.Fatal(err)
			}
			data := decoded["data"].(map[string]any)
			sealed := data["sealed"].(map[string]any)
			target := decoded
			switch name {
			case "data":
				target = data
			case "sealed":
				target = sealed
			case "binding":
				target = sealed["binding"].(map[string]any)
			case "artifact":
				target = sealed["artifacts"].([]any)[0].(map[string]any)
			}
			target["extra"] = nil
			wire, _ := json.Marshal(decoded)
			if _, err := agentic.DecodeSeal(wire); !errors.Is(err, agentic.ErrSealMalformed) {
				t.Fatalf("unknown %s member admitted: %v", name, err)
			}
		})
	}
}
