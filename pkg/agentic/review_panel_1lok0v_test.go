package agentic_test

import (
	"encoding/json"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"strings"
	"testing"
)

func TestPanelClaudeRoundTripParity(t *testing.T) {
	base := sealTestClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{PromptEnv: []string{"PANEL_SELECTOR=one"}, NativeTail: []string{"--tail"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(seal)
	var decoded agentic.Seal
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	imported, err := agentic.ImportSeal(claude.New(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]agentic.Plan{}
	p := final
	p.Binary += "-changed"
	cases["binary"] = p
	p = final
	p.Argv = append(append([]string(nil), p.Argv...), "extra")
	cases["argv"] = p
	p = final
	p.Env = append(append([]string(nil), p.Env...), "PANEL_SELECTOR=two")
	cases["selector"] = p
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			local := p.VerifyBeforeExec()
			remote := imported.VerifyBeforeExec(p)
			t.Logf("in-process refused=%v imported refused=%v", local != nil, remote != nil)
			if (local != nil) != (remote != nil) {
				t.Error("verifier parity lost in transit")
			}
		})
	}
}
func TestPanelWireClosedAndBounded(t *testing.T) {
	prefix := `{"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0",`
	cases := map[string]string{
		"unknown-field":    prefix + `"data":{"kind":"unsealed","extra":1}}`,
		"duplicate-key":    prefix + `"data":{"kind":"sealed","kind":"unsealed"}}`,
		"null-kind-shadow": prefix + `"data":{"kind":"unsealed","kind":null}}`,
		"oversize":         prefix + `"data":{"kind":"unsealed","extra":"` + strings.Repeat("x", 65537) + `"}}`,
		"invalid-utf8":     prefix + `"data":{"kind":"unsealed"},"extra":"` + string([]byte{255}) + `"}`,
		"lone-surrogate":   prefix + `"data":{"kind":"unsealed"},"extra":"\ud800"}`,
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) {
			var seal agentic.Seal
			err := json.Unmarshal([]byte(wire), &seal)
			if err == nil {
				_, err = agentic.ImportSeal(claude.New(), seal)
			}
			t.Logf("refused=%v", err != nil)
			if err == nil {
				t.Error("invalid wire admitted")
			}
		})
	}
}
func TestPanelWireWrongTypeEmptyTruncated(t *testing.T) {
	for name, wire := range map[string]string{"wrong-type": `{"schema":42}`, "empty": "", "truncated": `{"schema":`, "null": "null", "empty-object": "{}"} {
		t.Run(name, func(t *testing.T) {
			var s agentic.Seal
			err := json.Unmarshal([]byte(wire), &s)
			if err != nil {
				t.Logf("decode refusal type=%T; ImportSeal not reached", err)
				return
			}
			_, err = agentic.ImportSeal(claude.New(), s)
			if err == nil {
				t.Error("invalid input admitted")
			} else {
				t.Logf("ImportSeal refusal=%v", err)
			}
		})
	}
}
