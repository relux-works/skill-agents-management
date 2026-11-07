// Package gatewayadmission owns current policy admission for a launch-private
// gateway attempt. Expressibility and observational preflight alone issue no
// capability: one vendor build and one actual limits-store verdict are required.
package gatewayadmission

import (
	"context"
	"path/filepath"
	"slices"
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
	// Pin the configuration selector before building or reading limits. Ordinary
	// Codex ChildEnv does not translate Home into CODEX_HOME on its own.
	req.Home = filepath.Clean(strings.TrimSpace(req.Home))
	if !filepath.IsAbs(req.Home) || strings.ContainsAny(req.Home, "\x00\r\n") {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	seenHome := false
	for _, entry := range req.Env {
		name, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "CODEX_HOME") {
			if seenHome || name != "CODEX_HOME" || value != req.Home {
				return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
			}
			seenHome = true
		}
	}
	req.Env = agentic.SetEnvValue(slices.Clone(req.Env), "CODEX_HOME", req.Home)
	built, err := vendorplugin.BuildLaunchWithEnvironment(ctx, r, req, agentic.LaunchModeInteractive)
	if err != nil {
		return agentic.PlanWithEnvironment{}, err
	}
	// Carry the selector as owned data in the issued candidate: admission
	// overlays and the actual child environment must preserve this exact value.
	pin := "CODEX_HOME=" + req.Home
	if built.Plan.Home != req.Home || !slices.Contains(built.Plan.Env, pin) {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	built.OwnedEnv = agentic.SetEnvValue(slices.Clone(built.OwnedEnv), "CODEX_HOME", req.Home)
	slices.Sort(built.OwnedEnv)
	// Runtime is a declared identity, independent of the agentic system ID.
	// Model aliases are admission inputs; limits apply to what will launch.
	runtime, err := vendorplugin.NormalizeRuntimeID(string(req.Runtime))
	if err != nil {
		return agentic.PlanWithEnvironment{}, err
	}
	verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: string(runtime), Model: built.Plan.ModelIdentity.Launched, Home: req.Home})
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
