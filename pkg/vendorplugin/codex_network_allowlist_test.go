package vendorplugin_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// This file drives the ONE verified codex network tuple {adapter:
// codex-env-v1, harness: codex-cli, build: 0.159.0, entrypoint: exec}
// through vendorplugin.BuildLaunch, the entry point a spawn goes through.
// The exact tuple is admitted onto a real codex launch — the patch in env,
// the Record as provenance, and the set half injected into each stdio MCP
// entry's env block — while every neighbouring tuple still refuses with
// typed network_scope_unsupported from the shared gate, before any vendor
// dispatch.

// codexNetworkCarrier is a well-formed codex-env-v1 scope bearing the exact
// verified tuple: the D1 egress-a patch (appendix §2.5 E1) plus its R1-style
// Record.
func codexNetworkCarrier() agentic.Network {
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
				Adapter: "codex-env-v1", Harness: "codex-cli", Build: "0.159.0", Entrypoint: "exec",
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

// managedCodexRequest is a real codex launch — the astraRequest shape, pinned
// to the non-alias gpt-6-astra — carrying a network scope and one stdio MCP
// entry for the injection to visit.
func managedCodexRequest(t *testing.T, network agentic.Network) vendorplugin.SpawnRequest {
	t.Helper()
	req := astraRequest(t, "gpt-6-astra", "high")
	// An isolated empty home: a managed codex launch establishes its MCP
	// file inventory from the effective home, and PATH-only env leaves no
	// home to establish it from.
	req.Env = append(req.Env, "HOME="+t.TempDir())
	req.Network = network
	req.Composition = agentic.Composition{
		Prefix: []string{
			"-c", `mcp_servers.local.command="/usr/local/bin/mcp-local"`,
			"-c", `mcp_servers.local.args=["--stdio"]`,
		},
		Servers: []agentic.CompositionServer{{Name: "local", Transport: "stdio"}},
	}
	return req
}

func codexMCPEnvPairs(argv []string) []string {
	var pairs []string
	for i := 0; i+1 < len(argv); i += 2 {
		if argv[i] != "-c" {
			continue
		}
		if strings.HasPrefix(argv[i+1], "mcp_servers.") && strings.Contains(argv[i+1], ".env=") {
			pairs = append(pairs, argv[i+1])
		}
	}
	return pairs
}

// TestBuildLaunchAdmitsTheVerifiedCodexNetworkTuple drives the exact tuple
// through the vendor entry point: the scope survives both layers onto a real
// codex plan, the patch in env, the Record as provenance, and the set half
// injected into the stdio entry's env block.
func TestBuildLaunchAdmitsTheVerifiedCodexNetworkTuple(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	network := codexNetworkCarrier()
	plan, err := vendorplugin.BuildLaunch(context.Background(), registry, managedCodexRequest(t, network), agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(codex, verified tuple): %v", err)
	}
	if plan.System != "codex" {
		t.Fatalf("plan.System = %q, want the codex runtime's declared harness", plan.System)
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
	wantPairs := []string{
		`mcp_servers.local.env={HTTP_PROXY="http://127.0.0.1:18081",HTTPS_PROXY="http://127.0.0.1:18081",http_proxy="http://127.0.0.1:18081",https_proxy="http://127.0.0.1:18081",NO_PROXY="127.0.0.1,::1,localhost",no_proxy="127.0.0.1,::1,localhost"}`,
	}
	if pairs := codexMCPEnvPairs(plan.Argv); !reflect.DeepEqual(pairs, wantPairs) {
		t.Fatalf("injected env pairs = %v, want %v", pairs, wantPairs)
	}
}

// TestBuildLaunchRefusesNeighbouringCodexNetworkTuples pins the exact-tuple
// rule at the vendor entry point: each row changes exactly one of the four
// members, so a declaration weakened to admit any one of them must fail that
// subtest by name.
func TestBuildLaunchRefusesNeighbouringCodexNetworkTuples(t *testing.T) {
	registry := isolatedRegistry(t, nil)
	cases := map[string]func(*binding.AdapterIdentity){
		"another build":      func(identity *binding.AdapterIdentity) { identity.Build = "0.159.1" },
		"another entrypoint": func(identity *binding.AdapterIdentity) { identity.Entrypoint = "dry-run" },
		"another adapter":    func(identity *binding.AdapterIdentity) { identity.Adapter = "generic-env-v1" },
		"another harness":    func(identity *binding.AdapterIdentity) { identity.Harness = "codex" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			network := codexNetworkCarrier()
			mutate(&network.Record.AdapterIdentity)

			_, err := vendorplugin.BuildLaunch(context.Background(), registry, managedCodexRequest(t, network), agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildLaunch err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildLaunch err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
}
