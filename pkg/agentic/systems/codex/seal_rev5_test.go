package codex

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// sealRev5EnvSystem is the real Codex plugin with only the child environment
// overridden, so the sealed round-trip still drives Registry -> BuildPlan ->
// FinalizePlan -> ExportSeal -> DecodeSeal / ImportSeal.
type sealRev5EnvSystem struct {
	*System
	env []string
}

func (s *sealRev5EnvSystem) ChildEnv([]string, agentic.LaunchRequest) ([]string, error) {
	return append([]string(nil), s.env...), nil
}

// TestSealedEmptyEnvRoundTrips is the sealed-family zero-environment
// control: an untouched finalized sealed plan with an empty process
// environment round-trips, and the imported verifier still binds the exact
// final process and the original catalog digest.
func TestSealedEmptyEnvRoundTrips(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	sys := &sealRev5EnvSystem{System: New(), env: []string{}}
	registry := agentic.NewRegistry()
	if err := registry.Register(sys); err != nil {
		t.Fatalf("Register: %v", err)
	}
	base, err := agentic.BuildPlan(registry, catalogRequest(t, home, wd, "low"), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Kind != agentic.SealKindSealed || seal.Data.Sealed == nil || seal.Data.Sealed.Binding == nil {
		t.Fatalf("data = %+v, want sealed with a process binding", seal.Data)
	}
	if seal.Data.Sealed.Binding.Selectors == nil {
		t.Fatal("empty selectors collapsed: sealed export lost the required empty map")
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatalf("marshal seal: %v", err)
	}
	for _, member := range []string{`"selectors":null`, `"env_names":null`, `"argv":null`, `"artifacts":null`} {
		if strings.Contains(string(wire), member) {
			t.Fatalf("required collection serialized as null: %s", member)
		}
	}
	decoded, err := agentic.DecodeSeal(wire)
	if err != nil {
		t.Fatalf("DecodeSeal refused the untouched sealed export: %v", err)
	}
	imported, err := agentic.ImportSeal(New(), decoded)
	if err != nil {
		t.Fatalf("ImportSeal refused the untouched sealed export: %v", err)
	}
	if err := imported.VerifyBeforeExec(final); err != nil {
		t.Fatalf("imported verifier refused the untouched sealed process: %v", err)
	}
	changed := final
	changed.Argv = append(append([]string(nil), final.Argv...), "--extra")
	if err := imported.VerifyBeforeExec(changed); err == nil {
		t.Fatalf("imported verifier admitted an argv change: %v", err)
	}
	if !errors.Is(imported.VerifyBeforeExec(changed), agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("imported verifier refusal is untyped: %v", imported.VerifyBeforeExec(changed))
	}
}
