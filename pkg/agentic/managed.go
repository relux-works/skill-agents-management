package agentic

import "errors"

// CodexRemoteClientTarget selects the manager-owned native conversation on one
// app-server endpoint. It never means latest, a picker, or a new conversation.
// Home on the enclosing request must identify the manager's private Codex home.
type CodexRemoteClientTarget struct {
	Endpoint string
	ThreadID string
}

// ErrManagedLaunchInvalid identifies malformed, conflicting or unsupported
// managed plan inputs. Diagnostics never include endpoint credentials or IDs.
var ErrManagedLaunchInvalid = errors.New("agentic: managed launch input invalid")

// ManagedLaunchPlanner is an optional plugin contract. BuildPlan calls validation
// before preparation/binary resolution, then wraps the plugin's base verifier
// with the managed process guard. A plugin without this contract refuses all
// present managed inputs rather than silently dropping them.
type ManagedLaunchPlanner interface {
	ValidateManagedLaunch(LaunchRequest, LaunchMode) error
	SealManagedLaunch(LaunchRequest, Plan, ExecPlanVerifier) (ExecPlanVerifier, error)
}

func hasManagedLaunchInputs(req LaunchRequest) bool {
	return req.CodexRemoteClient != nil || req.ManagerToolDir != nil || len(req.AdditionalWritableRoots) != 0
}
