package pi

import (
	"context"
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/localruntime"
)

// ErrPreflightRefused classifies a successful status read whose broker state
// is not safe to launch. Read failures retain their original wrapped cause so
// callers can distinguish an operational read error from a policy refusal.
var ErrPreflightRefused = errors.New("pi: preflight refused")

// curatorEnginesProjectEnv mirrors local-models's environment contract — the
// explicit project directory whose engine profile the status read queries.
const curatorEnginesProjectEnv = "CURATOR_ENGINES_PROJECT_DIR"

// preflight constructs a StatusQuery from req's OWN fields — never from
// ambient CWD, an environment variable read directly by this process, or
// any other fallback — reads it through status, and applies the admit/
// refuse table below.
func preflight(ctx context.Context, status localruntime.StatusReader, req agentic.LaunchRequest) (agentic.PreflightEvidence, error) {
	if _, err := nativeModelIdentity(req); err != nil {
		return agentic.PreflightEvidence{}, err
	}
	query := localruntime.StatusQuery{
		Runtime:               localruntime.RuntimeID(req.Runtime),
		Model:                 localruntime.ModelID(req.Model.ID),
		CuratorEnginesProject: launchenv.Value(req.Env, curatorEnginesProjectEnv),
		CuratorEnginesProfile: req.Profile,
	}

	result, err := status.Status(ctx, query)
	if err != nil {
		return agentic.PreflightEvidence{}, fmt.Errorf("pi: preflight status read failed: %w", err)
	}
	if err := admitOrRefuse(result); err != nil {
		return agentic.PreflightEvidence{}, err
	}
	return agentic.PreflightEvidence{
		Detail: fmt.Sprintf("broker_state=%s broker_source=%s", result.BrokerState, result.BrokerSource),
	}, nil
}

// admitOrRefuse is Preflight's own admit/refuse table (architecture decision
// §5.2.1), answering a narrower operational question than Availability's
// UX table (pkg/vendorplugin/vendors/local-models/availability.go):
// "should BuildLaunch even attempt the native Pi launch."
//
// Only a LIVE, attested connection may admit: absence is not permission to
// start a local model, because this process has no ensure/lease owner.
// Contention (candidate-only) and every record-derived-unverified state,
// including unverified-stale, REFUSE because they prove nothing about now.
// Even an attested "draining" state refuses, because the broker's own
// listener is confirmed already closed.
//
// This function performs ZERO filesystem or process-table operations of its
// own: it is advisory-only, and any reclaim or cleanup of a stale record
// belongs entirely to curator-engines' lifecycle owner; this Pi plugin only
// builds a native launch plan after an ADMIT here.
func admitOrRefuse(status localruntime.Status) error {
	if status.BrokerSource == localruntime.SourceDetermined && status.BrokerState == "absent" {
		return fmt.Errorf("%w: broker is absent; native Pi Exec has no owner that can start or lease this local model", ErrPreflightRefused)
	}
	if status.BrokerSource == localruntime.SourceAttested {
		switch status.BrokerState {
		case "starting", "serving", "lingering":
			return nil
		case "draining":
			return fmt.Errorf("%w: broker is draining (attested); its listener is already closed", ErrPreflightRefused)
		default:
			return fmt.Errorf("%w: attested but unrecognized broker state %q", ErrPreflightRefused, status.BrokerState)
		}
	}
	return fmt.Errorf("%w: unattested/indeterminate broker read (state=%q source=%q); only a live, attested broker outside draining may admit", ErrPreflightRefused,
		status.BrokerState, status.BrokerSource)
}
