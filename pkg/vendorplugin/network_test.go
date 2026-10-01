package vendorplugin

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func testNetwork() agentic.Network {
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
				Adapter: "generic-env-v1", Harness: "pangolin", Build: "0.0.0-test", Entrypoint: "exec",
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

// TestBuildLaunchForwardsNetworkUnchanged drives the production entry point:
// SpawnRequest.Network reaches the agentic plan's environment and provenance
// without the vendor touching it. The vendor layer carries the scope; only
// BuildPlan interprets it.
func TestBuildLaunchForwardsNetworkUnchanged(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())
	req := narwhalRequest()
	want := testNetwork()
	req.Network = want.Clone()
	req.Env = []string{"PATH=/usr/bin", "HOME=/home/agent", "HTTPS_PROXY=http://wrong.invalid:3128"}

	plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(managed): %v", err)
	}
	for _, entry := range plan.Env {
		if entry == "HTTPS_PROXY=http://wrong.invalid:3128" {
			t.Fatalf("forwarded patch kept the conflicting value: %q", entry)
		}
	}
	for _, wantEnv := range []string{
		"HTTP_PROXY=http://127.0.0.1:18081",
		"HTTPS_PROXY=http://127.0.0.1:18081",
		"http_proxy=http://127.0.0.1:18081",
		"https_proxy=http://127.0.0.1:18081",
		"NO_PROXY=127.0.0.1,::1,localhost",
		"no_proxy=127.0.0.1,::1,localhost",
		narwhalAuthEnv,
	} {
		if !containsEnv(plan.Env, wantEnv) {
			t.Errorf("plan.Env = %v, want %q", plan.Env, wantEnv)
		}
	}
	got, ok := plan.NetworkProvenanceSnapshot()
	if !ok {
		t.Fatal("forwarded plan carries no network provenance")
	}
	if !reflect.DeepEqual(got, want.Record) {
		t.Fatalf("provenance = %#v, want %#v", got, want.Record)
	}
}

// TestPassthroughLaunchForwardsNetworkLosslessly pins the shared projection
// system-only bindings use: no vendor exists to call Spawn, so the lossless
// copy is the whole forwarding path.
func TestPassthroughLaunchForwardsNetworkLosslessly(t *testing.T) {
	req := narwhalRequest()
	want := testNetwork()
	req.Network = want.Clone()
	model := newNarwhal().models[0]

	got := passthroughLaunchRequest(pangolinID, model, req.Effort, req)
	if !reflect.DeepEqual(got.Network, want) {
		t.Fatalf("passthrough Network = %#v, want %#v", got.Network, want)
	}
	got.Network.Patch.Set[0].Value = "http://mutated.invalid:1"
	if req.Network.Patch.Set[0].Value == "http://mutated.invalid:1" {
		t.Fatal("passthrough aliases the caller's patch slices")
	}
}

// TestBuildLaunchRefusesAVendorThatChangesTheNetworkScope is the fidelity
// half: a vendor may ADD authentication and may not redirect the scope. Each
// knob below rewrites the carrier on the way out and must be refused before
// any plan is built.
func TestBuildLaunchRefusesAVendorThatChangesTheNetworkScope(t *testing.T) {
	managed := testNetwork()
	dropped := agentic.Network{}
	retargeted := managed.Clone()
	retargeted.Patch.Set[0].Value = "http://127.0.0.1:18082"
	for name, knob := range map[string]*agentic.Network{
		"the scope dropped":    &dropped,
		"the patch retargeted": &retargeted,
	} {
		t.Run(name, func(t *testing.T) {
			vendor := newNarwhal()
			vendor.spawnNetwork = knob
			registry := registerNarwhal(t, vendor)
			req := narwhalRequest()
			req.Network = managed.Clone()

			_, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeExec)
			if !errors.Is(err, ErrVendorContract) {
				t.Fatalf("BuildLaunch err = %v, want ErrVendorContract", err)
			}
		})
	}
}
