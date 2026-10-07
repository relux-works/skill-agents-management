//go:build providerkeysmutant

package codex

import (
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"os"
	"strings"
)

func init() {
	switch os.Getenv("CODEX_PROVIDER_KEYS_MUTANT") {
	case "":
	case "drop-requires-openai-auth":
		// Keep the declaration present, but omit exactly one represented key.
		narrowed := make([]string, 0, len(representedProviderKeys)-1)
		for _, key := range representedProviderKeys {
			if key != "requires_openai_auth" {
				narrowed = append(narrowed, key)
			}
		}
		representedProviderKeys = narrowed
	default:
		panic("unknown provider-key mutant")
	}
}

// These seams introduce common-mode drift through the production BuildPlan
// call, on both ID and snapshot paths. They never edit source literals.
type acceptedPlanMutantSystem struct {
	*System
	mutant string
}

func (s *acceptedPlanMutantSystem) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	argv, err := s.System.Argv(req, mode)
	if err == nil && s.mutant == "wire-api-argv" {
		for i := range argv {
			if strings.HasSuffix(argv[i], `.wire_api="responses"`) {
				argv[i] = strings.TrimSuffix(argv[i], `"responses"`) + `"chat"`
			}
		}
	}
	return argv, err
}

func (s *acceptedPlanMutantSystem) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := s.System.ChildEnv(parent, req)
	if err == nil && s.mutant == "extra-child-env" {
		env = append(env, "CODEX_REVIEW_PARITY_DRIFT=1")
	}
	return env, err
}

func init() {
	mutant := os.Getenv("CODEX_ACCEPTED_PLAN_MUTANT")
	switch mutant {
	case "":
	case "wire-api-argv", "extra-child-env":
		acceptedPlanSystem = func() agentic.System {
			return &acceptedPlanMutantSystem{System: New(), mutant: mutant}
		}
	default:
		panic("unknown accepted-plan mutant")
	}
}
