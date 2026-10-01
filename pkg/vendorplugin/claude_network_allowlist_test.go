package vendorplugin_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file drives the ONE verified network tuple {adapter: generic-env-v1,
// harness: claude-code, build: 2.1.287, entrypoint: exec} through
// vendorplugin.BuildLaunch, the entry point a spawn goes through. The exact
// tuple is admitted onto a real claude launch; every neighbouring tuple still
// refuses with typed network_scope_unsupported from the shared gate, before
// any vendor dispatch.

// claudeNetworkCarrier is a well-formed generic-env-v1 scope bearing the
// exact verified tuple: the D1 egress-a patch (appendix §2.5 E1) plus its
// R1-style Record.
func claudeNetworkCarrier() agentic.Network {
	return agentic.Network{
		Patch: envpatch.Patch{
			Unset: envpatch.UnsetNames(),
			Set: []envpatch.Pair{
				{Name: "HTTP_PROXY", Value: "http://127.0.0.1:18081"},
				{Name: "HTTPS_PROXY", Value: "http://127.0.0.1:18081"},
				{Name: "http_proxy", Value: "http://127.0.0.1:18081"},
				{Name: "https_proxy", Value: "http://127.0.0.1:18081"},
				{Name: "NO_PROXY", Value: "127.0.0.1,::1,localhost"},
				{Name: "no_proxy", Value: "127.0.0.1,::1,localhost"},
			},
		},
		Record: binding.Record{
			Schema:        binding.SchemaRecord,
			ProfileRef:    "egress-a",
			ProfileDigest: "sha256:1e5912de9e3459a12b7365f338d385c1c4c7b4a6b622598e17ed4004d7658699",
			AdapterIdentity: binding.AdapterIdentity{
				Adapter: "generic-env-v1", Harness: "claude-code", Build: "2.1.287", Entrypoint: "exec",
			},
			Assurance: "cooperative",
			Origin:    "explicit",
			Probe: binding.ProbeRecord{
				TCP: "skipped", Connect: "skipped", TLS: "skipped",
				CheckedAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
			},
		},
	}
}

// managedClaudeRequest is a real claude launch — the opusRequest shape, pinned
// to the non-alias claude-opus-5 — carrying a network scope.
func managedClaudeRequest(t *testing.T, network agentic.Network) vendorplugin.SpawnRequest {
	t.Helper()
	req := opusRequest(t, "claude-opus-5", "high")
	req.Network = network
	return req
}

// TestBuildLaunchAdmitsTheVerifiedClaudeNetworkTuple drives the exact tuple
// through the vendor entry point: the scope survives both layers onto a real
// claude-code plan, the patch in env and the Record as provenance.
func TestBuildLaunchAdmitsTheVerifiedClaudeNetworkTuple(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	network := claudeNetworkCarrier()
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, managedClaudeRequest(t, network), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(claude, verified tuple): %v", err)
	}
	if plan.System != "claude-code" {
		t.Fatalf("plan.System = %q, want the claude runtime's declared harness", plan.System)
	}
	for _, want := range []string{
		"HTTP_PROXY=http://127.0.0.1:18081",
		"HTTPS_PROXY=http://127.0.0.1:18081",
		"http_proxy=http://127.0.0.1:18081",
		"https_proxy=http://127.0.0.1:18081",
		"NO_PROXY=127.0.0.1,::1,localhost",
		"no_proxy=127.0.0.1,::1,localhost",
	} {
		if !argvHasElement(plan.Env, want) {
			t.Errorf("plan.Env = %v, want %q", plan.Env, want)
		}
	}
	got, ok := plan.NetworkProvenanceSnapshot()
	if !ok {
		t.Fatal("the admitted plan carries no network provenance")
	}
	if !reflect.DeepEqual(got, network.Record) {
		t.Fatalf("provenance = %#v, want %#v", got, network.Record)
	}
}

// TestBuildLaunchRefusesNeighbouringClaudeNetworkTuples pins the exact-tuple
// rule at the vendor entry point: each row changes exactly one of the four
// members, so a declaration weakened to admit any one of them must fail that
// subtest by name.
func TestBuildLaunchRefusesNeighbouringClaudeNetworkTuples(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	cases := map[string]func(*binding.AdapterIdentity){
		"another build":      func(identity *binding.AdapterIdentity) { identity.Build = "2.1.286" },
		"another entrypoint": func(identity *binding.AdapterIdentity) { identity.Entrypoint = "dry-run" },
		"another adapter":    func(identity *binding.AdapterIdentity) { identity.Adapter = "muse-env-v1" },
		"another harness":    func(identity *binding.AdapterIdentity) { identity.Harness = "codex" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			network := claudeNetworkCarrier()
			mutate(&network.Record.AdapterIdentity)

			_, err := vendorplugin.BuildLaunch(context.Background(), registry, managedClaudeRequest(t, network), agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildLaunch err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildLaunch err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
}
