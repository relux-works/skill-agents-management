package pi

import (
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestStoredPolicyInspectorReportsUnsupported(t *testing.T) {
	got := agentic.InspectStoredPolicy(New(&fakeStatusReader{}), agentic.Plan{})
	if got.Support != agentic.StoredPolicyUnsupported {
		t.Fatalf("Support = %q, want unsupported", got.Support)
	}
	if len(got.Relaxations) != 0 || len(got.SourcesInspected) != 0 || len(got.SourcesNotInspected) != 0 {
		t.Fatalf("unsupported inspection made claims about settings: %#v", got)
	}
}
