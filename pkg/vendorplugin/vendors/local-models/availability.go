package localmodels

import (
	"context"
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// statusSourceLabel names the evidence source recorded on every Availability
// verdict this vendor produces.
const statusSourceLabel = "local-runtime status"

// Availability reads query.Runtime to disambiguate which of this vendor's
// declared runtimes' pointers to check — the SAME per-(runtime, model)
// lookup Spawn performs — then composes a live status read into a verdict
// per the source-aware mapping table (mapAvailability below).
//
// An empty query.Model asks about the vendor as a whole, which this vendor
// has no single answer for — it reports per model, since whether a model is
// loaded and whether inference is running on it are per-model facts — so it
// answers Unchecked rather than guessing which of its models the caller
// meant.
func (v *Vendor) Availability(query vendorplugin.AvailabilityQuery) (vendorplugin.Availability, error) {
	if query.Model == "" {
		return vendorplugin.Unchecked(), nil
	}
	pointer, ok := pointerFor(v.cfg, query.Runtime, query.Model)
	if !ok {
		return vendorplugin.Availability{}, fmt.Errorf("%w: runtime %s, model %s", ErrNoLocalPointer, query.Runtime, query.Model)
	}

	status, err := v.status.Status(context.Background(), localruntime.StatusQuery{
		Runtime:            localruntime.RuntimeID(query.Runtime),
		Model:              localruntime.ModelID(query.Model),
		AgentsInfraProject: pointer.AgentsInfraProject,
		AgentsInfraProfile: pointer.AgentsInfraProfile,
	})
	if err != nil {
		// A read failure is a COMPLETE, honest Unknown verdict — never
		// Healthy, never a guessed Unreachable, and never propagated as a Go
		// error: the verdict itself (we could not tell) was produced just
		// fine.
		return vendorplugin.Availability{
			State:   vendorplugin.AvailabilityUnknown,
			Checked: []string{statusSourceLabel},
			Failures: []vendorplugin.ReadFailure{{
				Source: statusSourceLabel,
				Reason: err.Error(),
			}},
		}, nil
	}
	return mapAvailability(status), nil
}

// mapAvailability is the source-aware Availability mapping, switching on the
// full (BrokerState, BrokerSource) pair rather than BrokerState alone.
//
// Healthy is reachable ONLY through BrokerSource == attested: no unverified
// read, however optimistic its last-known state, can ever produce Healthy.
// This is deliberately DIFFERENT from Preflight's own admit/refuse question
// (pkg/agentic/systems/pi/preflight.go) — the two tables answer different
// questions about the same underlying fact, and a caller must not read one
// off the other.
func mapAvailability(status localruntime.Status) vendorplugin.Availability {
	checked := []string{statusSourceLabel}
	observed := []vendorplugin.Observation{{
		Source: statusSourceLabel,
		Detail: fmt.Sprintf("broker_state=%s broker_source=%s", status.BrokerState, status.BrokerSource),
	}}

	if status.BrokerSource == localruntime.SourceAttested {
		switch status.BrokerState {
		case "serving":
			if status.ActiveLeases < status.MaxLeases {
				return vendorplugin.Availability{State: vendorplugin.AvailabilityHealthy, Checked: checked, Observed: observed}
			}
			// At capacity: no evidence-derived clear time exists in v1, so
			// this is Unknown, never a fabricated Limited.
			return vendorplugin.UnknownAfterCheck(checked...)
		case "lingering":
			// A lingering broker, live-confirmed, still admits a new lease
			// before its linger timer drains it.
			return vendorplugin.Availability{State: vendorplugin.AvailabilityHealthy, Checked: checked, Observed: observed}
		case "starting":
			// No evidence-derived Until — StartupTimeoutSeconds is never
			// surfaced to this adapter.
			return vendorplugin.UnknownAfterCheck(checked...)
		case "draining":
			// Positively confirmed: the broker's listener is already closed.
			return vendorplugin.Availability{State: vendorplugin.AvailabilityUnreachable, Checked: checked, Observed: observed}
		}
		return vendorplugin.UnknownAfterCheck(checked...)
	}

	if status.BrokerSource == localruntime.SourceDetermined && status.BrokerState == "absent" {
		// Positively absent is a definite, live-determined fact for THIS
		// table's UX question — see the divergence note above for why
		// Preflight answers this same pair differently.
		return vendorplugin.Availability{State: vendorplugin.AvailabilityUnreachable, Checked: checked, Observed: observed}
	}

	// Every remaining reachable pair — candidate-only contention, and every
	// record-derived-unverified state including unverified-stale — is
	// Unknown: never claim Healthy or Unreachable from a last-known record
	// or an in-progress election with no live confirmation THIS read made.
	// A pair outside the frozen eleven (this adapter's own decode already
	// refuses those as a read failure, but this mapping stays total and
	// panic-free regardless) falls through to the same Unknown answer.
	return vendorplugin.UnknownAfterCheck(checked...)
}
