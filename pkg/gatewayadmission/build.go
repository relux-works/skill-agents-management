// Package gatewayadmission owns current policy admission for a launch-private
// gateway attempt. Expressibility and observational preflight alone issue no
// capability: one vendor build and one actual limits-store verdict are required.
package gatewayadmission

import (
	"context"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/gatewaybasis"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/providerlimits"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// LimitsRefusalError retains the actual nonserviceable verdict without issuing
// an admission basis. Consumers can preserve their existing diagnostic family.
type LimitsRefusalError struct{ Verdict vendorplugin.Availability }

func (*LimitsRefusalError) Error() string {
	return "agentic: Codex gateway provider limits do not admit this launch"
}
func (*LimitsRefusalError) Unwrap() error { return agentic.ErrCodexGatewayRefused }

func Build(ctx context.Context, r *vendorplugin.Registry, store *providerlimits.Store, req vendorplugin.SpawnRequest) (agentic.PlanWithEnvironment, error) {
	if store == nil || strings.TrimSpace(req.Home) == "" || strings.TrimSpace(req.WorkDir) == "" {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	built, err := vendorplugin.BuildLaunchWithEnvironment(ctx, r, req, agentic.LaunchModeInteractive)
	if err != nil {
		return agentic.PlanWithEnvironment{}, err
	}
	verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: string(req.Runtime), Model: string(req.Model), Home: req.Home})
	if err != nil {
		return agentic.PlanWithEnvironment{}, err
	}
	if !verdict.Serviceable() {
		return agentic.PlanWithEnvironment{}, &LimitsRefusalError{Verdict: verdict}
	}
	candidate, err := agentic.SnapshotGatewayCandidate(built)
	if err != nil {
		return agentic.PlanWithEnvironment{}, err
	}
	basis, err := gatewaybasis.Issue(candidate)
	if err != nil {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	built.GatewayBasis = basis
	return built, nil
}
