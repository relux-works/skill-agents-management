package claude

import "github.com/relux-works/skill-agents-management/pkg/agentic"

// AuxiliaryArgv owns the closed preflight commands. No caller value is an
// argument: even the goal probe is the bare capability query, never the goal.
func (*System) AuxiliaryArgv(role agentic.AuxRole) ([]string, error) {
	switch role {
	case agentic.ClaudeVersionProbe:
		return []string{"--version"}, nil
	case agentic.ClaudeGoalProbe:
		return []string{"-p", "--output-format", "json", "/goal"}, nil
	default:
		return nil, agentic.ErrAuxRoleUnknown
	}
}
