package agentic_test

import (
	"errors"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"testing"
)

func TestDelta2vv8jzClosedHostedNullMembers(t *testing.T) {
	for _, extra := range []string{`,"binding":null`, `,"sealed":null`, `,"binding":null,"sealed":null`} {
		wire := `{"schema":"urn:relux:agents-management:exec-guard","schema_version":"1.0.0","data":{"kind":"unsealed"` + extra + `}}`
		seal, err := agentic.DecodeSeal([]byte(wire))
		if !errors.Is(err, agentic.ErrSealMalformed) {
			var imported error
			if err == nil {
				_, imported = agentic.ImportSeal(claude.New(), seal)
			}
			t.Errorf("non-closed marker admitted: decode=%v import=%v hosted=%v extra=%s", err, imported, seal.HostedAdmissible(), extra)
		}
	}
}
