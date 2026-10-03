package agentic

import (
	"errors"
	"testing"
)

type rejectingExecVerifier struct{ err error }

func (v rejectingExecVerifier) VerifyBeforeExec(Plan) error { return v.err }

func TestVerifyBeforeExecPropagatesSealedArtifactRefusal(t *testing.T) {
	refusal := &LocalProviderRefusal{Kind: LocalProviderConflicting, File: "catalog.json"}
	plan := Plan{execVerifier: rejectingExecVerifier{err: refusal}}
	if err := plan.VerifyBeforeExec(); !errors.Is(err, ErrLocalProviderConflicting) {
		t.Fatalf("seal refusal lost: %v", err)
	}
}

func TestVerifyBeforeExecUnsealedHostedPlanUnchanged(t *testing.T) {
	if err := (Plan{Binary: "codex"}).VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
}
