package agentic_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
)

func TestSealWireUnsealedKindClosure(t *testing.T) {
	base := sealTestClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	local, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := json.Marshal(local.Data.Binding)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"sealed-null":            `{"kind":"unsealed","sealed":null}`,
		"binding-null":           `{"kind":"unsealed","binding":null}`,
		"both-null":              `{"kind":"unsealed","sealed":null,"binding":null}`,
		"sealed-object":          `{"kind":"unsealed","sealed":{}}`,
		"binding-object":         `{"kind":"unsealed","binding":` + string(binding) + `}`,
		"binding-before-kind":    `{"binding":` + string(binding) + `,"kind":"unsealed"}`,
		"bound-missing-binding":  `{"kind":"unsealed-bound"}`,
		"bound-null-binding":     `{"kind":"unsealed-bound","binding":null}`,
		"bound-sealed":           `{"kind":"unsealed-bound","binding":` + string(binding) + `,"sealed":{}}`,
		"sealed-missing-payload": `{"kind":"sealed"}`,
		"sealed-null-payload":    `{"kind":"sealed","sealed":null}`,
		"sealed-cross-binding":   `{"kind":"sealed","binding":` + string(binding) + `}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			wire := []byte(`{"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0","data":` + data + `}`)
			seal, decodeErr := agentic.DecodeSeal(wire)
			// Exercise both consumers even when the byte boundary has refused.
			_, importErr := agentic.ImportSeal(claude.New(), seal)
			if !errors.Is(decodeErr, agentic.ErrSealMalformed) || importErr == nil || seal.HostedAdmissible() {
				t.Fatalf("kind-crossing %s admitted: decode=%v import=%v hosted=%v", name, decodeErr, importErr, seal.HostedAdmissible())
			}
		})
	}
	for _, plan := range []agentic.Plan{base, final} {
		seal, err := plan.ExportSeal()
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(seal)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := agentic.DecodeSeal(wire)
		if err != nil {
			t.Fatal(err)
		}
		imported, err := agentic.ImportSeal(claude.New(), decoded)
		if err != nil {
			t.Fatal(err)
		}
		if err := imported.VerifyBeforeExec(plan); err != nil {
			t.Fatal(err)
		}
		if decoded.HostedAdmissible() != (decoded.Data.Kind == agentic.SealKindUnsealed) {
			t.Fatal("local projection admitted by hosted schema")
		}
	}
	t.Logf("kind/null combinations attacked: %d of %d; base and local controls verify", len(cases), len(cases))
}
