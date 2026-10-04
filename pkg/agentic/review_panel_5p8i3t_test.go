package agentic_test

import (
	"encoding/json"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"strings"
	"testing"
)

func TestPanelWireRefusals(t *testing.T) {
	prefix := `{"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0",`
	cases := map[string]string{
		"unknown-member":   prefix + `"data":{"kind":"unsealed","unexpected":true}}`,
		"duplicate-kind":   prefix + `"data":{"kind":"sealed","kind":"unsealed"}}`,
		"duplicate-schema": `{"schema":"bad",` + prefix[1:] + `"data":{"kind":"unsealed"}}`,
		"wrong-type":       prefix + `"data":false}`,
		"truncated":        prefix,
		"empty":            "",
		"oversize":         prefix + `"data":{"kind":"unsealed","padding":"` + strings.Repeat("a", 65537) + `"}}`,
	}
	for n, b := range cases {
		t.Run(n, func(t *testing.T) {
			var s agentic.Seal
			e := json.Unmarshal([]byte(b), &s)
			if e != nil {
				t.Logf("JSON refused (not module typed): %T", e)
				return
			}
			_, e = agentic.ImportSeal(claude.New(), s)
			if e == nil {
				t.Error("invalid wire admitted")
			}
		})
	}
}
