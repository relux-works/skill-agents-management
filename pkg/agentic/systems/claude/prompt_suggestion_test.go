package claude

import (
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestPromptSuggestionDisabledForExec(t *testing.T) {
	t.Parallel()
	assertPromptSuggestionDisabled(t, agentic.LaunchModeExec)
}

func TestPromptSuggestionDisabledForDryRun(t *testing.T) {
	t.Parallel()
	assertPromptSuggestionDisabled(t, agentic.LaunchModeDryRun)
}

func TestPromptSuggestionDisabledForInteractive(t *testing.T) {
	t.Parallel()
	assertPromptSuggestionDisabled(t, agentic.LaunchModeInteractive)
}

// Exercise BuildPlan -> System.ChildEnv -> childEnv for every declared mode.
// Inspect every entry: a map alone could hide a surviving inherited true.
func assertPromptSuggestionDisabled(t *testing.T, mode agentic.LaunchMode) {
	t.Helper()
	const key = "CLAUDE_CODE_ENABLE_PROMPT_SUGGESTION"
	for _, tc := range []struct {
		name   string
		parent []string
	}{
		{name: "absent"},
		{name: "inherited-true", parent: []string{key + "=true"}},
		{name: "inherited-false", parent: []string{key + "=false"}},
		{name: "inherited-empty", parent: []string{key + "="}},
		{name: "duplicate-inherited-values", parent: []string{key + "=true", key + "=false", key + "=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binDir, workDir := tempSlot(t), tempSlot(t)
			writeStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
			req.Env = append(slices.Clone(tc.parent), "PATH="+binDir, key+"_OTHER=keep-me")
			parent := slices.Clone(req.Env)
			plan := buildParityPlan(t, New(), req, mode)
			count := 0
			for _, entry := range plan.Env {
				name, value, _ := strings.Cut(entry, "=")
				if name == key {
					count++
					if value != "false" {
						t.Errorf("BuildPlan(%s) child env contains %s=%q, want false", mode, key, value)
					}
				}
			}
			if count != 1 {
				t.Errorf("BuildPlan(%s) child env contains %d entries for %s, want exactly one", mode, count, key)
			}
			if !slices.Contains(plan.Env, key+"_OTHER=keep-me") {
				t.Error("the prompt suggestion override changed an unrelated env key")
			}
			if !slices.Equal(req.Env, parent) {
				t.Error("the prompt suggestion override mutated the caller environment")
			}
		})
	}
}
