package codex

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The real-binary record is curator/docs/qualification/codex-0.167.0.md.
// These planner controls reuse the qualified V3 contract; they do not execute
// a model, establish sandbox enforcement or qualify the gateway transport.
func TestCodex0167ExactPermissionMapping(t *testing.T) {
	checkCodexExactPermissionMapping(t, "0.167.0")
}

// Known selectors refuse with typed errors and no partial argv beside native
// forwarding controls, at both root and exec placements.
func TestCodex0167PolicyConflictMatrix(t *testing.T) {
	checkCodexExactPolicyConflictMatrix(t, "0.167.0")
}

// Unqualified aliases, malformed overrides and unknown values refuse beside
// valid controls. Prompt bytes after -- retain their existing meaning.
func TestCodex0167ClosedPolicyForms(t *testing.T) {
	checkCodexExactClosedPolicyForms(t, "0.167.0")
}

// Only modeled command positions classify exec; removed/unmodeled options
// remain indeterminate and option values or prompt text stay data.
func TestCodex0167Classification(t *testing.T) {
	checkCodexExactClassification(t, "0.167.0")
}

// Context-owned yolo selects one bypass; unqualified policy aliases refuse
// through BuildPlan and return no partial argv.
func TestCodex0167ContextPermissionSelection(t *testing.T) {
	checkCodexExactContextPermissionSelection(t, "0.167.0")
}

// Explicit relaxations, restrictive and malformed stored controls stay
// distinct, without modifying the source or claiming native precedence.
func TestCodex0167StoredPolicyInspection(t *testing.T) {
	checkCodexStoredPolicyInspection(t, "0.167.0")
}

// The qualified control beside unlisted neighbors pins exact lookup: neither
// mapping, classification nor yolo argv may escape an unverified refusal.
func TestCodex0167NeighboursRemainUnverified(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"0.167.0", "0.166.0", "0.167.1", "0.167.0-dev", "0.167.0+different"} {
		t.Run(release, func(t *testing.T) {
			req := interactiveRequest(tempSlot(t))
			req.ToolRelease, req.PermissionMode = release, agentic.PermissionModeYolo
			mapping, err := New().PermissionMapping(release, req.PermissionMode)
			if release == "0.167.0" {
				if err != nil || mapping != (agentic.PermissionMapping{Grammar: agentic.PermissionGrammarV3, Flag: bypassApprovalsAndSandboxFlag}) {
					t.Fatalf("mapping=%#v err=%v", mapping, err)
				}
				interactivePlan(t, req, agentic.LaunchModeInteractive)
				return
			}
			if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || mapping != (agentic.PermissionMapping{}) {
				t.Fatalf("mapping=%#v err=%v", mapping, err)
			}
			if got, err := New().ClassifyNonInteractiveArgs(release, []string{"exec"}); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || got.IsNonInteractive() {
				t.Fatalf("classification=%#v err=%v", got, err)
			}
			if err := interactivePlanError(t, req); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
				t.Fatalf("plan err=%v", err)
			}
			if argv, err := New().Argv(req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) || len(argv) != 0 {
				t.Fatalf("argv=%q err=%v", argv, err)
			}
		})
	}
}
