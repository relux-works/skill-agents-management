package claude

import (
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const resumeFlag = "--resume"

// ElevateResumeIntent uses the same pinned Commander ownership parser as the
// native policy gate. Values owned by other options never become selectors.
func (*System) ElevateResumeIntent(wrapper, native []string) (agentic.ResumeElevation, error) {
	removed := make(map[int]bool)
	var selections []agentic.ResumeIntent
	for _, option := range claudeOptions(native) {
		arity, known := claudeOptionArities[option.name]
		if !known {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "unknown option arity"}
		}
		// claudeOptions splits a long attached option at the first '='. The
		// name must be known and its pinned arity must own an attached value.
		if arity == optionNone && strings.HasPrefix(native[option.index], "--") && strings.Contains(native[option.index], "=") {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "attached value on boolean option"}
		}
		if option.name != resumeFlag && option.name != "-r" && option.name != "--continue" && option.name != "-c" && option.name != "--session-id" && option.name != "--fork-session" {
			continue
		}
		if option.name == "--session-id" || option.name == "--fork-session" {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "ambiguous native identity"}
		}
		if !strings.HasPrefix(native[option.index], "--") && len(native[option.index]) > 2 && !strings.HasPrefix(native[option.index], "-r") {
			return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "combined short selector"}
		}
		intent := agentic.ResumeIntent{Kind: agentic.ResumeLatest}
		if option.name == resumeFlag || option.name == "-r" {
			if len(option.values) != 1 {
				return agentic.ResumeElevation{}, &agentic.ResumeInvalidError{Reason: "picker selector"}
			}
			intent = agentic.ResumeIntent{Kind: agentic.ResumeClaudeUUID, Identity: &option.values[0]}
		}
		selections = append(selections, intent)
		removed[option.index] = true
		if option.placement == agentic.NativePolicyPlacementSeparateToken {
			removed[option.index+1] = true
		}
	}
	tail := make([]string, 0, len(native))
	for i, token := range native {
		if !removed[i] {
			tail = append(tail, token)
		}
	}
	return agentic.ResolveResumeSelectors(wrapper, selections, tail, agentic.ResumeClaudeUUID)
}
