package muse

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// importedMuseSealedPlan builds a sealed interactive Muse plan for the given
// verified build and exports its seal through the closed JSON wire, the
// production ImportSeal ingress. The request carries XDG overrides so the
// seal binds a non-trivial XDG identity, and an absolute managed home: the
// contract's final process never has the empty documentation default. It
// returns the plan, the decoded seal, and the release sidecar behind the
// sealed stub binary.
func importedMuseSealedPlan(t *testing.T, version, build string) (agentic.Plan, agentic.Seal, string) {
	t.Helper()
	dir := t.TempDir()
	writeMuseSidecarStub(t, dir, "Muse Code "+version+" ("+build+")\n")
	req := museSidecarRequest(t, dir)
	req.Home = t.TempDir()
	scratch := t.TempDir()
	req.Env = append(req.Env,
		"XDG_CONFIG_HOME="+filepath.Join(scratch, "config"),
		"XDG_DATA_HOME="+filepath.Join(scratch, "data"),
		"XDG_CACHE_HOME="+filepath.Join(scratch, "cache"),
	)
	plan := buildMuseSealedPlan(t, req)
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
	return plan, decoded, filepath.Join(dir, "release.txt")
}

// TestNewImportedProcessBindsSealedMuse walks the real sealed-Muse round
// trip on both verified builds: ExportSeal, JSON wire, ImportSeal,
// NewImportedProcess, VerifyBeforeExec. The untouched plan verifies; an
// argv drift refuses at the wrapper and a release swap behind the seal
// refuses at the Muse delegate.
func TestNewImportedProcessBindsSealedMuse(t *testing.T) {
	for _, tc := range []struct {
		version string
		build   string
	}{
		{version: "1.4.1", build: "1.4.1-R4503.1"},
		{version: "1.4.2", build: "1.4.2-R4684.1"},
	} {
		t.Run(tc.build, func(t *testing.T) {
			plan, seal, sidecar := importedMuseSealedPlan(t, tc.version, tc.build)
			imported, err := agentic.NewImportedProcess(New(), seal, plan)
			if err != nil {
				t.Fatalf("NewImportedProcess: %v", err)
			}
			if err := imported.VerifyBeforeExec(plan); err != nil {
				t.Fatalf("VerifyBeforeExec untouched: %v", err)
			}
			drifted := plan
			drifted.Argv = append(slices.Clone(plan.Argv), "--drift")
			if err := imported.VerifyBeforeExec(drifted); !errors.Is(err, agentic.ErrImportedProcessChanged) {
				t.Fatalf("argv drift err = %v, want ErrImportedProcessChanged", err)
			}
			// Swap the release behind the seal: the binary bytes still
			// match, so only the delegate's re-probe can refuse.
			if err := os.WriteFile(sidecar, []byte("Muse Code 1.5.0 (1.5.0-R9999.1)\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := imported.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealReleaseChanged) {
				t.Fatalf("swapped release err = %v, want ErrMuseSealReleaseChanged", err)
			}
		})
	}
}

// TestImportedProcessDelegatesToMuseVerifier pins wrapper-to-delegate
// delegation for sealed Muse: with the exact process still matching, only
// the delegate re-reading the binary can refuse the byte swap. The witness
// env entry is invisible to the Muse delegate (it seals XDG_*/pin only),
// so the narrowing mutant that skips the delegate call for exactly that
// entry fails here while the control below keeps refusing.
func TestImportedProcessDelegatesToMuseVerifier(t *testing.T) {
	plan, seal, _ := importedMuseSealedPlan(t, "1.4.1", "1.4.1-R4503.1")
	witness := plan
	witness.Env = append(slices.Clone(plan.Env), "MUSE_WITNESS=skip-delegation")
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
	// Same version answer, different bytes: the digest moves while the
	// release stays, so only the delegate's hash re-read refuses.
	if err := os.WriteFile(plan.Binary, []byte(museSealWitnessStub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := imported.VerifyBeforeExec(witness); !errors.Is(err, ErrMuseSealBinaryChanged) {
		t.Fatalf("delegation skipped for witness env: %v", err)
	}
	if err := control.VerifyBeforeExec(plan); !errors.Is(err, ErrMuseSealBinaryChanged) {
		t.Fatalf("swapped binary admitted without witness: %v", err)
	}
}
