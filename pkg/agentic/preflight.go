package agentic

import "context"

// PreflightEvidence is what a Preflight check observed, carried for
// observability only. It never gates admission by itself: the admit/refuse
// decision is entirely the Preflightable implementation's own, expressed by
// returning nil (admit) or a non-nil error (refuse).
type PreflightEvidence struct {
	// Detail is a short, human-readable summary of what Preflight observed —
	// the broker state and source a status read reported, or the read
	// failure it hit. It is recorded for observability and is never parsed
	// back into a decision by any caller.
	Detail string
}

// Preflightable is the second, optional generic extension point a System
// plugin may implement: a fail-fast, advisory readiness check run once per
// launch, after resolution and before BuildPlan.
//
// It is optional and generic on purpose. The dispatcher that calls it
// (vendorplugin.BuildLaunch) type-asserts a resolved runtime's System
// against this interface and, only when the assertion succeeds, calls
// Preflight under a bounded ctx; a system that does not implement it is
// launched exactly as it was before this interface existed, with no
// per-system-identifier branch anywhere in the dispatcher.
//
// A non-nil error REFUSES the launch before BuildPlan is ever called. An
// implementation must be free of side effects other than reading whatever
// ctx and req let it reach: Preflight never starts, stops or signals the
// process it is asking about, and it performs no reclaim or cleanup of
// stale state — it only decides whether THIS launch attempt should proceed
// right now.
type Preflightable interface {
	Preflight(ctx context.Context, req LaunchRequest) (PreflightEvidence, error)
}
