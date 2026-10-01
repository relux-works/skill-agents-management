package muse

import (
	"errors"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// TestMuseRefusesNetworkScopeUntilD8VerifiesIt drives the production entry
// point: BuildPlan's admission gate refuses every non-zero Network with typed
// network_scope_unsupported, because muse declares no verified adapter tuple.
// Muse's closed parent allowlist would be bypassed by a post-filter patch, so
// no tuple is honoured until D8 verifies one. It never launches without the
// scope: the refusal returns no plan.
func TestMuseRefusesNetworkScopeUntilD8VerifiesIt(t *testing.T) {
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register(muse): %v", err)
	}
	req := launchRequest(t, "do the thing")
	req.Network = agentic.Network{
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
				Adapter: "generic-env-v1", Harness: "muse", Build: "", Entrypoint: "exec",
			},
			Assurance: "cooperative",
			Origin:    "explicit",
			Probe: binding.ProbeRecord{
				TCP: "skipped", Connect: "skipped", TLS: "skipped",
				CheckedAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("BuildPlan(muse, managed) err = %v, want ErrNetworkScopeUnsupported", err)
	}
	if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
		t.Fatalf("BuildPlan(muse, managed) err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
	}
}

// TestMuseRefusesUnsetOnlyNetworkScope is the narrowing control for the gate
// above: an unset-only patch (no set half) is still a non-zero scope and must
// still be refused. (The ChildEnv-weakening mutant for this shape is killed by
// TestMuseChildEnvRefusesNetworkScopeDirectly instead: through BuildPlan the
// admission gate refuses before ChildEnv is reached.)
func TestMuseRefusesUnsetOnlyNetworkScope(t *testing.T) {
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register(muse): %v", err)
	}
	req := launchRequest(t, "do the thing")
	req.Network = agentic.Network{
		Patch: envpatch.Patch{Unset: envpatch.UnsetNames()},
		Record: binding.Record{
			Schema:        binding.SchemaRecord,
			ProfileRef:    "egress-a",
			ProfileDigest: "sha256:1e5912de9e3459a12b7365f338d385c1c4c7b4a6b622598e17ed4004d7658699",
			AdapterIdentity: binding.AdapterIdentity{
				Adapter: "generic-env-v1", Harness: "muse", Build: "", Entrypoint: "exec",
			},
			Assurance: "cooperative",
			Origin:    "explicit",
			Probe: binding.ProbeRecord{
				TCP: "skipped", Connect: "skipped", TLS: "skipped",
				CheckedAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("BuildPlan(muse, unset-only) err = %v, want ErrNetworkScopeUnsupported", err)
	}
}

// TestMuseChildEnvRefusesNetworkScopeDirectly holds the second line: a caller
// holding the plugin itself — not BuildPlan, whose admission gate refuses
// first — gets the same typed refusal from ChildEnv, for a full patch and for
// an unset-only scope alike. It is the narrowing control for the ChildEnv
// check: a mutant that weakens it to only refuse when the set half is present
// admits the unset-only shape and must fail that subtest.
func TestMuseChildEnvRefusesNetworkScopeDirectly(t *testing.T) {
	full := agentic.Network{
		Patch: envpatch.Patch{
			Unset: envpatch.UnsetNames(),
			Set: []envpatch.Pair{
				{Name: "HTTP_PROXY", Value: "http://127.0.0.1:18081"},
			},
		},
		Record: binding.Record{
			Schema:        binding.SchemaRecord,
			ProfileRef:    "egress-a",
			ProfileDigest: "sha256:1e5912de9e3459a12b7365f338d385c1c4c7b4a6b622598e17ed4004d7658699",
			AdapterIdentity: binding.AdapterIdentity{
				Adapter: "generic-env-v1", Harness: "muse", Build: "", Entrypoint: "exec",
			},
			Assurance: "cooperative",
			Origin:    "explicit",
		},
	}
	unsetOnly := full.Clone()
	unsetOnly.Patch.Set = nil
	for name, network := range map[string]agentic.Network{
		"full patch": full,
		"unset-only": unsetOnly,
	} {
		t.Run(name, func(t *testing.T) {
			req := launchRequest(t, "do the thing")
			req.Network = network
			_, err := New().ChildEnv(req.Env, req)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("ChildEnv err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("ChildEnv err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
}

// TestMuseChildEnvPassesZeroNetworkThrough is the control beside the direct
// refusal above: without a scope the surface answers the curated environment,
// auto-update forced off, exactly as before the carrier existed.
func TestMuseChildEnvPassesZeroNetworkThrough(t *testing.T) {
	req := launchRequest(t, "do the thing")
	env, err := New().ChildEnv(req.Env, req)
	if err != nil {
		t.Fatalf("ChildEnv(zero Network): %v", err)
	}
	found := false
	for _, entry := range env {
		if entry == "MUSE_NO_AUTO_UPDATE=1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ChildEnv(zero Network) = %v, want MUSE_NO_AUTO_UPDATE=1 forced", env)
	}
}

func TestMuseZeroNetworkStillPlans(t *testing.T) {
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register(muse): %v", err)
	}
	req := launchRequest(t, "do the thing")

	plan, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(muse, zero Network): %v", err)
	}
	if _, ok := plan.NetworkProvenanceSnapshot(); ok {
		t.Fatal("zero Network produced network provenance on muse")
	}
}
