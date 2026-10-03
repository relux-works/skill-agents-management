package claude

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/netprofile"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/curator-network-profiles/pkg/resolve"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Public library outputs, rather than handwritten patches, cross both launch
// entry points. Direct and inherited are still managed: the exact-tuple gate
// must refuse a neighbouring build even when the patch only removes values.
func TestBuildPlanPublicNetworkProfilesPreserveCarrierAndAdmission(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{netprofile.KindExternalHTTPProxy, netprofile.KindDirect} {
		for _, origin := range []resolve.Origin{resolve.OriginExplicit, resolve.OriginInherited} {
			for _, owned := range []bool{false, true} {
				entrypoint := "BuildPlan"
				if owned {
					entrypoint = "BuildPlanWithEnvironment"
				}
				t.Run(kind+"/"+string(origin)+"/"+entrypoint, func(t *testing.T) {
					input := netprofile.Input{Kind: kind}
					if kind == netprofile.KindExternalHTTPProxy {
						input.Endpoint = "http://127.0.0.1:18081"
						input.BypassHosts = netprofile.RequiredBypassHosts
					}
					profile, err := netprofile.Normalize("public-network", input)
					if err != nil {
						t.Fatal(err)
					}
					patch := (envpatch.Generic{}).Patch(profile)
					bound := binding.Binding{
						ProfileRef: profile.Name, ProfileDigest: netprofile.Digest(profile),
						AdapterIdentity: verifiedNetworkCarrier().Record.AdapterIdentity,
						Assurance:       binding.AssuranceCooperative, EnvPatch: patch,
					}
					network := agentic.Network{Patch: patch, Record: bound.Record(string(origin), nil)}
					registry := agentic.NewRegistry()
					if err := registry.Register(New()); err != nil {
						t.Fatal(err)
					}
					req := managedRequest(t, network)
					req.Env = append(req.Env, "Http_Proxy=http://stale.invalid:3128", "ALL_PROXY=http://stale.invalid:3128", "BYSTANDER=kept")
					build := func() (agentic.Plan, []string, error) {
						if owned {
							result, err := agentic.BuildPlanWithEnvironment(registry, req, agentic.LaunchModeExec)
							return result.Plan, result.OwnedEnv, err
						}
						plan, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
						return plan, nil, err
					}
					plan, ownedEnv, err := build()
					if err != nil {
						t.Fatal(err)
					}
					got, ok := plan.NetworkProvenanceSnapshot()
					if !ok || !reflect.DeepEqual(got, network.Record) {
						t.Fatalf("provenance = %#v, present %v; want %#v", got, ok, network.Record)
					}
					if !planEnvHas(plan.Env, "BYSTANDER=kept") {
						t.Fatal("network patch removed unrelated parent environment")
					}
					for _, env := range [][]string{plan.Env, ownedEnv} {
						for _, entry := range env {
							name, _, _ := strings.Cut(entry, "=")
							if strings.Contains(entry, "stale.invalid") || (kind == netprofile.KindDirect && strings.HasSuffix(strings.ToLower(name), "_proxy")) {
								t.Fatalf("%s retained proxy state: %q", kind, entry)
							}
						}
					}
					if kind == netprofile.KindExternalHTTPProxy {
						for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
							value := "http://127.0.0.1:18081"
							if strings.EqualFold(name, "NO_PROXY") {
								value = "127.0.0.1,::1,localhost"
							}
							if !planEnvHas(plan.Env, name+"="+value) || (owned && !planEnvHas(ownedEnv, name+"="+value)) {
								t.Fatalf("missing public patch literal %s", name)
							}
						}
					}
					req.Network.Record.AdapterIdentity.Build = "2.1.286"
					_, _, err = build()
					if code, ok := refusal.CodeOf(err); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) || !ok || code != refusal.CodeScopeUnsupported {
						t.Fatalf("neighbouring build err = %v, want typed network_scope_unsupported", err)
					}
				})
			}
		}
	}
}
