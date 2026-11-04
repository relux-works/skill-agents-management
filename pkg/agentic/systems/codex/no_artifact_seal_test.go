package codex

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Real provider builds finalize with TMPDIR absent or present, without inventing
// an environment entry or replacing the present variable's original guard.
// The fixture executable is discovered but no runtime is started.
func TestNoArtifactOptionalTempDirFinalization(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[present], func(t *testing.T) {
			var dir *string
			if present {
				value := t.TempDir()
				dir = &value
			}
			base := tempDirPlan(t, dir, nil)
			before := append([]string(nil), base.Env...)
			final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
			if err != nil {
				t.Fatalf("valid optional TMPDIR plan: %v", err)
			}
			if !reflect.DeepEqual(final.Env, before) {
				t.Fatal("finalization rewrote environment")
			}
			if err := final.VerifyBeforeExec(); err != nil {
				t.Fatal(err)
			}
			seal, err := final.ExportSeal()
			if err != nil {
				t.Fatal(err)
			}
			_, noArtifact := seal.Data.Sealed.Selectors[noArtifactSealSelector]
			if noArtifact == present {
				t.Fatal("no-artifact form replaced a TMPDIR obligation or was omitted")
			}
			verifier, err := agentic.ImportSeal(New(), seal)
			if err != nil {
				t.Fatalf("finalized round trip: %v", err)
			}
			if err := verifier.VerifyBeforeExec(final); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Each mutation reaches its intended generic process boundary. Rejected copies
// preserve the original basis and the first finalized result; no reseal occurs.
func TestNoArtifactProcessBoundaries(t *testing.T) {
	base := tempDirPlan(t, nil, nil)
	before, err := base.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"binary", "argv", "env"} {
		t.Run(field, func(t *testing.T) {
			change := func(p agentic.Plan) agentic.Plan {
				switch field {
				case "binary":
					p.Binary += "-changed"
				case "argv":
					p.Argv = append(append([]string(nil), p.Argv...), "--changed")
				case "env":
					p.Env = append(append([]string(nil), p.Env...), "BOUNDARY_CHANGED=yes")
				}
				return p
			}
			if _, err := agentic.FinalizePlan(change(base), agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("pre-final mutation: %v", err)
			}
			if err := change(base).VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("provider original-process mutation: %v", err)
			}
			final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{FragmentEnv: []string{"FINAL_CONTROL=valid"}}, nil)
			if err != nil {
				t.Fatalf("unchanged positive: %v", err)
			}
			if err := final.VerifyBeforeExec(); err != nil {
				t.Fatal(err)
			}
			if err := change(final).VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("post-final mutation: %v", err)
			}
			seal, err := final.ExportSeal()
			if err != nil {
				t.Fatal(err)
			}
			imported, err := agentic.ImportSeal(New(), seal)
			if err != nil {
				t.Fatal(err)
			}
			if err := imported.VerifyBeforeExec(change(final)); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
				t.Fatalf("imported mutation: %v", err)
			}
			if err := imported.VerifyBeforeExec(final); err != nil {
				t.Fatal(err)
			}
			if _, err := agentic.FinalizePlan(final, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizeNoSealBasis) {
				t.Fatalf("second finalize: %v", err)
			}
			if err := final.VerifyBeforeExec(); err != nil {
				t.Fatal("rejection changed original final result")
			}
		})
	}
	after, err := base.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejection changed original basis")
	}
	if err := base.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
}

// The new form is a closed provider projection, importable only with the generic
// final-process binding. Arbitrary selectors and required artifacts cannot be
// replaced by this marker. Duplicate JSON keys fail at the existing wire gate.
func TestNoArtifactClosedImport(t *testing.T) {
	base := tempDirPlan(t, nil, nil)
	baseSeal, err := base.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.ImportSeal(New(), baseSeal); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
		t.Fatalf("unfinalized import: %v", err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	data := *seal.Data.Sealed
	// ImportSeal strips numeric final-binding selectors before provider dispatch.
	data.Selectors = map[string]string{noArtifactSealSelector: "v1"}
	cases := []struct {
		name   string
		change func(*agentic.SealedData)
	}{
		{"unknown-version", func(d *agentic.SealedData) { d.Selectors[noArtifactSealSelector] = "v2" }},
		{"empty-version", func(d *agentic.SealedData) { d.Selectors[noArtifactSealSelector] = "" }},
		{"missing-selector", func(d *agentic.SealedData) { d.Selectors = nil }},
		{"missing-binding", func(d *agentic.SealedData) { d.Binding = nil }},
		{"artifact-required", func(d *agentic.SealedData) {
			d.Artifacts = []agentic.SealedArtifact{{Name: sealedCatalogName, Path: "/synthetic/catalog", Digest: "sha256:synthetic"}}
		}},
		{"tmpdir-required", func(d *agentic.SealedData) { d.Selectors[TempDirEnv] = "hmac-sha256:synthetic" }},
		{"managed-required", func(d *agentic.SealedData) { d.Selectors[managedSealSelector] = "unknown" }},
		{"unknown-report", func(d *agentic.SealedData) { d.Selectors[carriedSettingsSealSelector] = "unknown" }},
		{"unknown-selector", func(d *agentic.SealedData) { d.Selectors["unexpected"] = "v1" }},
		{"wrong-binary-binding", func(d *agentic.SealedData) { d.Binary += "-changed" }},
		{"wrong-argv-binding", func(d *agentic.SealedData) { d.Argv = append(append([]string(nil), d.Argv...), "--changed") }},
		{"tmpdir-binding", func(d *agentic.SealedData) {
			b := *d.Binding
			b.EnvNames = append(append([]string(nil), b.EnvNames...), TempDirEnv)
			d.Binding = &b
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			changed := data
			changed.Selectors = map[string]string{noArtifactSealSelector: "v1"}
			c.change(&changed)
			_, err := New().ImportSealedData(changed)
			if c.name == "managed-required" {
				if !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
					t.Fatalf("managed obligation: %v", err)
				}
			} else if !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("intended malformed refusal: %v", err)
			}
		})
	}
	if _, err := agentic.ImportSeal(New(), seal, agentic.SealCommitmentKey{0: 1}); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatalf("wrong binding key: %v", err)
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
	standalone, err := New().ImportSealedData(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := standalone.VerifyBeforeExec(final); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatalf("imported assertion bypassed generic wrapper: %v", err)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(string(wire), `"codex-no-artifact":"v1"`, `"codex-no-artifact":"v1","codex-no-artifact":"v1"`, 1)
	if duplicate == string(wire) {
		t.Fatal("duplicate control not applied")
	}
	var decoded agentic.Seal
	if err := json.Unmarshal([]byte(duplicate), &decoded); !errors.Is(err, agentic.ErrSealMalformed) {
		t.Fatalf("duplicate wire basis: %v", err)
	}
}

// An originally absent report cannot be injected on either side of finalization,
// including after import; adjacent unchanged plans still verify at each stage.
func TestNoArtifactReportPresenceGuard(t *testing.T) {
	base := tempDirPlan(t, nil, nil)
	if err := base.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.CarriedSettings = &agentic.LocalProviderSettingsStatus{Note: "injected"}
	if err := changed.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("provider report presence: %v", err)
	}
	if _, err := agentic.FinalizePlan(changed, agentic.FinalizeOverlays{}, nil); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("pre-final report presence: %v", err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	changed = final
	changed.CarriedSettings = &agentic.LocalProviderSettingsStatus{Note: "injected"}
	if err := changed.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("final report presence: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(changed); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("imported report presence: %v", err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
}

// An actual local-provider catalog keeps its original artifact verifier even
// without TMPDIR. The no-artifact branch cannot replace that required guard.
func TestNoArtifactPreservesCatalogBasis(t *testing.T) {
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	base, err := buildCodexPlan(t, catalogRequest(t, home, workDir, "low"))
	if err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	if len(seal.Data.Sealed.Artifacts) != 1 {
		t.Fatal("catalog obligation missing")
	}
	if _, present := seal.Data.Sealed.Selectors[noArtifactSealSelector]; present {
		t.Fatal("no-artifact marker replaced catalog basis")
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
}

// Existing managed admission remains the owning process basis. Its base and
// final guard forms continue to import without an ordinary no-artifact marker.
func TestNoArtifactManagedBasisCompatibility(t *testing.T) {
	req := managedPrimaryRequest(t)
	dir := t.TempDir()
	for _, name := range []string{"task-board", "tb-sessiond"} {
		writeStubExecutable(t, dir, name)
	}
	req.ManagerToolDir = &dir
	base := requireManagedPlan(t, req)
	seal, err := base.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	if _, present := seal.Data.Sealed.Selectors[noArtifactSealSelector]; present {
		t.Fatal("ordinary basis changed managed guard form")
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(base); err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err = final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err = agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
}

// The no-artifact finalized guard uses the caller's existing ephemeral key
// channel: the same key verifies and a different key refuses, without renewal.
func TestNoArtifactExplicitKeyRoundTrip(t *testing.T) {
	base := tempDirPlan(t, nil, nil)
	key := agentic.SealCommitmentKey{0: 42}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := agentic.ImportSeal(New(), seal, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyBeforeExec(final); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.ImportSeal(New(), seal, agentic.SealCommitmentKey{0: 43}); !errors.Is(err, agentic.ErrSealImportRefused) {
		t.Fatalf("wrong explicit key: %v", err)
	}
}
