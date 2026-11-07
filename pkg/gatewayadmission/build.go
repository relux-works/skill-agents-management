// Package gatewayadmission owns current policy admission for a launch-private
// gateway attempt. Expressibility and observational preflight alone issue no
// capability: one vendor build and serviceable actual limits-store verdicts are required.
package gatewayadmission

import (
	"context"
	"os"
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
	// Resolve before either planning or limits reads, retaining the requested
	// spelling for persisted records written under that spelling.
	requestedHome := req.Home
	canonical, homeIdentity, err := resolveHome(req.Home)
	if err != nil {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	req.Home = canonical
	runtime, err := r.ResolveRuntime(req.Runtime)
	if err != nil || runtime.ID != "codex" || runtime.SystemID != "codex" || runtime.VendorID != "openai" {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	homes := []string{req.Home, requestedHome}
	seenHome := false
	for _, entry := range req.Env {
		name, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "CODEX_HOME") {
			selector, identity, resolveErr := resolveHome(value)
			if seenHome || name != "CODEX_HOME" || resolveErr != nil || selector != req.Home || !os.SameFile(homeIdentity, identity) {
				return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
			}
			seenHome = true
			homes = append(homes, value)
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
	// Known aliases are declared by the vendor, never inferred from spelling.
	models := []string{built.Plan.ModelIdentity.Launched, built.Plan.ModelIdentity.Requested}
	for _, row := range runtime.Vendor.Models() {
		if string(row.AliasOf) == built.Plan.ModelIdentity.Launched && !slices.Contains(models, string(row.ID)) {
			models = append(models, string(row.ID))
		}
	}
	// Read the Cartesian product: every accepted spelling may hold writer state.
	for _, home := range homes {
		for _, model := range models {
			verdict, err := store.AvailabilityFor(providerlimits.VerdictQuery{Runtime: string(runtime.ID), Model: model, Home: home})
			if err != nil {
				return agentic.PlanWithEnvironment{}, err
			}
			if !verdict.Serviceable() {
				return agentic.PlanWithEnvironment{}, &LimitsRefusalError{Verdict: verdict}
			}
		}
	}
	candidate, err := agentic.SnapshotGatewayCandidate(built)
	if err != nil {
		return agentic.PlanWithEnvironment{}, err
	}
	// Bind the captured candidate to the directory resolved before the read.
	if current, err := os.Stat(req.Home); err != nil || !os.SameFile(homeIdentity, current) {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	basis, err := gatewaybasis.Issue(candidate)
	if err != nil {
		return agentic.PlanWithEnvironment{}, agentic.ErrCodexGatewayRefused
	}
	built.GatewayBasis = basis
	return built, nil
}
