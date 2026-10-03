package codex

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestLocalDeclaredVocabularyMustFitNativeCatalog(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		for _, path := range []string{"id", "snapshot"} {
			t.Run(mode.String()+"/"+path, func(t *testing.T) {
				home, work := t.TempDir(), t.TempDir()
				writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				req := catalogRequest(t, home, work, "low")
				if path == "snapshot" {
					req = withSnapshot(req, mustSnapshot(t, req))
				}
				req.Model.EffortVocabulary = []string{"low"}
				if _, err := buildCodexPlanForMode(t, req, mode); err != nil {
					t.Fatalf("subset positive: %v", err)
				}
				req.Model.EffortVocabulary = []string{"low", "max"}
				if _, err := buildCodexPlanForMode(t, req, mode); !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
					t.Fatalf("BuildPlan(selected low, declared max) = %v, want ErrLocalProviderUnsupported", err)
				}
			})
		}
	}
}
