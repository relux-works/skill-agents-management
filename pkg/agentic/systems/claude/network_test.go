package claude

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file holds the network allowlist this system declares: exactly one
// verified tuple {adapter: generic-env-v1, harness: claude-code, build:
// 2.1.287, entrypoint: exec}. A Record bearing that tuple is admitted through
// the production entry point; every neighbouring tuple still refuses with
// typed network_scope_unsupported.

// verifiedNetworkCarrier is a well-formed generic-env-v1 scope bearing the
// exact verified tuple: the D1 egress-a patch (appendix §2.5 E1) plus its
// R1-style Record.
func verifiedNetworkCarrier() agentic.Network {
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

// managedRequest is a prompt-mode launch carrying a network scope: the parity
// request plus a stub `claude` on PATH and a prompt file on disk, so the only
// thing standing between the request and a plan is the admission gate.
func managedRequest(t *testing.T, network agentic.Network) agentic.LaunchRequest {
	t.Helper()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := parityRequest(workDir, parityPromptRunID, parityPromptTaskID)
	req.PromptPath = writePromptFile(t, workDir, parityPromptBody)
	req.Env = []string{"PATH=" + binDir}
	req.Network = network
	return req
}

// TestNetworkAdaptersDeclareExactlyTheVerifiedTuple pins the declaration
// itself: one entry, the verified tuple, nothing else. A second entry — or a
// weakened member — admits a scope no verification covers.
func TestNetworkAdaptersDeclareExactlyTheVerifiedTuple(t *testing.T) {
	t.Parallel()
	want := []binding.AdapterIdentity{
		{Adapter: "generic-env-v1", Harness: "claude-code", Build: "2.1.287", Entrypoint: "exec"},
	}
	if got := New().Capabilities().NetworkAdapters; !reflect.DeepEqual(got, want) {
		t.Fatalf("NetworkAdapters = %#v, want %#v", got, want)
	}
}

// TestBuildPlanAdmitsTheVerifiedNetworkTuple drives the exact tuple through
// the production entry point: the patch reaches the child env and the Record
// travels as provenance.
func TestBuildPlanAdmitsTheVerifiedNetworkTuple(t *testing.T) {
	t.Parallel()
	network := verifiedNetworkCarrier()
	plan := buildParityPlan(t, New(), managedRequest(t, network), agentic.LaunchModeExec)

	for _, want := range []string{
		"HTTP_PROXY=http://127.0.0.1:18081",
		"HTTPS_PROXY=http://127.0.0.1:18081",
		"http_proxy=http://127.0.0.1:18081",
		"https_proxy=http://127.0.0.1:18081",
		"NO_PROXY=127.0.0.1,::1,localhost",
		"no_proxy=127.0.0.1,::1,localhost",
	} {
		if !planEnvHas(plan.Env, want) {
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

// TestBuildPlanRefusesNeighbouringNetworkTuples pins the exact-tuple rule at
// the production entry point: each row changes exactly one of the four
// members, so a declaration weakened to admit any one of them — the previous
// build, another entrypoint, another adapter, another harness — must fail that
// subtest by name.
func TestBuildPlanRefusesNeighbouringNetworkTuples(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*binding.AdapterIdentity){
		"another build":      func(identity *binding.AdapterIdentity) { identity.Build = "2.1.286" },
		"another entrypoint": func(identity *binding.AdapterIdentity) { identity.Entrypoint = "dry-run" },
		"another adapter":    func(identity *binding.AdapterIdentity) { identity.Adapter = "muse-env-v1" },
		"another harness":    func(identity *binding.AdapterIdentity) { identity.Harness = "codex" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := verifiedNetworkCarrier()
			mutate(&network.Record.AdapterIdentity)

			// planErrorFor supplies the stub PATH itself; the helper's
			// entry would leave two PATHs in one environment.
			req := managedRequest(t, network)
			req.Env = nil
			err := planErrorFor(t, req, agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
}

func planEnvHas(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
