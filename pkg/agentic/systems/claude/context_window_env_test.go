package claude

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file holds the claude-code half of the operating context window: a row
// that declares one (agentic.Model.ContextWindowTokens, stated by the vendor
// layer) must reach the child as CLAUDE_CODE_AUTO_COMPACT_WINDOW, and a row
// that declares none must leave the variable exactly as the parent had it.
//
// Everything is driven through BuildPlan, the production call site that turns
// the request into the child environment, never through childEnv directly.

const compactWindowKey = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// compactWindowEntries returns every environment entry for the variable, in
// order. A count rather than a map lookup: a duplicate that survives is a
// launch whose effective value depends on which entry the harness reads.
func compactWindowEntries(env []string) []string {
	var out []string
	for _, entry := range env {
		if name, _, _ := strings.Cut(entry, "="); name == compactWindowKey {
			out = append(out, entry)
		}
	}
	return out
}

// windowPlanEnv builds a real exec plan for a row and returns its environment.
func windowPlanEnv(t *testing.T, sys agentic.System, mode agentic.LaunchMode, model agentic.Model, parent []string) []string {
	t.Helper()
	binDir, workDir := tempSlot(t), tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	if mode != agentic.LaunchModeInteractive {
		// An interactive launch has no channel for an assignment prompt.
		req.PromptPath = writePromptFile(t, workDir, "context window contract prompt")
	}
	req.Model = model
	req.Model.Effort = agentic.EffortSupportRequired
	req.Env = append(slices.Clone(parent), "PATH="+binDir)
	return buildParityPlan(t, sys, req, mode).Env
}

// windowProblems is the whole contract as a list of violations, so the real
// plugin must return none and every narrowing of it must return some.
func windowProblems(t *testing.T, sys agentic.System) []string {
	t.Helper()
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeInteractive} {
		// A declared window is exported verbatim, two values on purpose: a
		// plugin that hard-codes the one number the registry uses today
		// satisfies a single-value test and ignores the row.
		for _, window := range []int{100_000, 250_000} {
			want := fmt.Sprintf("%s=%d", compactWindowKey, window)
			for _, tc := range []struct {
				name   string
				model  agentic.Model
				parent []string
			}{
				{"identity", agentic.Model{ID: "claude-haiku-5-5", ContextWindowTokens: window}, nil},
				{"alias", agentic.Model{ID: "haiku", AliasOf: "claude-haiku-5-5", ContextWindowTokens: window}, nil},
				// The parent's own value must lose to the row's, once.
				{"inherited-wider", agentic.Model{ID: "claude-haiku-5-5", ContextWindowTokens: window}, []string{compactWindowKey + "=1000000"}},
				{"inherited-duplicates", agentic.Model{ID: "claude-haiku-5-5", ContextWindowTokens: window}, []string{compactWindowKey + "=1", compactWindowKey + "=2"}},
			} {
				got := compactWindowEntries(windowPlanEnv(t, sys, mode, tc.model, tc.parent))
				if len(got) != 1 || got[0] != want {
					report("%s/%s/%d: child env carries %v, want exactly [%s]", mode, tc.name, window, got, want)
				}
			}
		}

		// A row with no window adds nothing and removes nothing.
		if got := compactWindowEntries(windowPlanEnv(t, sys, mode, agentic.Model{ID: "claude-sonnet-5-5"}, nil)); len(got) != 0 {
			report("%s/no-window: a row that declares none got %v; absence must stay absence", mode, got)
		}
		inherited := compactWindowKey + "=300000"
		if got := compactWindowEntries(windowPlanEnv(t, sys, mode, agentic.Model{ID: "claude-sonnet-5-5"}, []string{inherited})); !slices.Equal(got, []string{inherited}) {
			report("%s/no-window-inherited: child env carries %v, want the parent's [%s] untouched", mode, got, inherited)
		}
	}
	return problems
}

func TestAClaudeRowThatDeclaresAWindowExportsItAndOneThatDoesNotLeavesTheVariableAlone(t *testing.T) {
	t.Parallel()
	for _, problem := range windowProblems(t, New()) {
		t.Error(problem)
	}
}

// windowMutant is the real plugin with its child environment reshaped after
// the fact, which is the smallest possible weakening of the export.
type windowMutant struct {
	*System
	reshape func(env []string) []string
}

func (m windowMutant) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env, err := m.System.ChildEnv(parent, req)
	if err != nil {
		return nil, err
	}
	return m.reshape(env), nil
}

// TestTheWindowContractRefusesEveryNarrowingOfTheExport narrows the export one
// way at a time. A contract that stayed green on any of these would be equally
// consistent with one that checks nothing.
func TestTheWindowContractRefusesEveryNarrowingOfTheExport(t *testing.T) {
	t.Parallel()
	for name, reshape := range map[string]func([]string) []string{
		// The export is dropped: the harness compacts at its own default.
		"drop-the-export": func(env []string) []string {
			return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
				key, _, _ := strings.Cut(entry, "=")
				return key == compactWindowKey
			})
		},
		// The export keeps its name and loses the row: one number for everyone.
		"hard-code-100000": func(env []string) []string {
			if len(compactWindowEntries(env)) == 0 {
				return env
			}
			return agentic.SetEnvValue(slices.Clone(env), compactWindowKey, "100000")
		},
		// The export fires for a row that declared nothing.
		"export-for-zero": func(env []string) []string {
			if len(compactWindowEntries(env)) != 0 {
				return env
			}
			return agentic.SetEnvValue(slices.Clone(env), compactWindowKey, "100000")
		},
		// The export appends instead of replacing: the parent's entry survives.
		"append-beside-inherited": func(env []string) []string {
			return append(slices.Clone(env), compactWindowKey+"=1000000")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if problems := windowProblems(t, windowMutant{System: New(), reshape: reshape}); len(problems) == 0 {
				t.Fatalf("the %s mutant passes the window contract, so nothing in it distinguishes that weakening from the real export", name)
			}
		})
	}
}
