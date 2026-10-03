package agentic

// ExecPlanSealer is optional. A plugin binds launch-owned artifacts to the
// plan; hosted plans with no artifacts retain a nil verifier.
type ExecPlanSealer interface {
	SealExecPlan(Plan) (ExecPlanVerifier, error)
}
type ExecPlanVerifier interface{ VerifyBeforeExec(Plan) error }

// VerifyBeforeExec verifies the plan identity and the plugin's sealed artifacts.
// Consumers MUST call this immediately before starting their own process using
// this plan's binary and argv. This module neither owns nor starts processes.
// Unsealed hosted plans retain their existing behavior.
func (p Plan) VerifyBeforeExec() error {
	if p.execVerifier != nil {
		if err := p.execVerifier.VerifyBeforeExec(p); err != nil {
			return err
		}
	}
	return nil
}
