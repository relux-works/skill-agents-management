package agentic

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
)

// testNetwork is a valid generic-env-v1 carrier for the pangolin double: the
// D1 egress-a patch (appendix §2.5 E1) plus its R1-style Record. It is the
// request a test starts from when it wants to exercise one network refusal.
func testNetwork() Network {
	return Network{
		Patch: envpatch.Patch{
			Unset: []string{
				"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY",
				"http_proxy", "https_proxy", "all_proxy", "ftp_proxy", "no_proxy",
			},
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

func TestNetworkZeroLeavesPlanByteIdentical(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)
	req := pangolinRequest()

	plan, err := BuildPlan(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(zero Network): %v", err)
	}
	if _, ok := plan.NetworkProvenanceSnapshot(); ok {
		t.Fatal("zero Network produced network provenance; absence of a scope must not claim one")
	}
	for _, entry := range plan.Env {
		if strings.Contains(entry, "127.0.0.1:18081") || strings.HasPrefix(entry, "HTTP_PROXY=") {
			t.Fatalf("zero Network changed the child env: %q", entry)
		}
	}
	withEnv, err := BuildPlanWithEnvironment(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlanWithEnvironment(zero Network): %v", err)
	}
	for _, entry := range withEnv.OwnedEnv {
		if strings.HasPrefix(entry, "HTTP_PROXY=") || strings.HasPrefix(entry, "http_proxy=") {
			t.Fatalf("zero Network changed OwnedEnv: %q", entry)
		}
	}
	if _, ok := withEnv.Plan.NetworkProvenanceSnapshot(); ok {
		t.Fatal("zero Network produced provenance through the owned-env path")
	}
}

func TestBuildPlanAppliesNetworkPatchAfterChildEnv(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)
	req := pangolinRequest()
	req.Env = []string{
		"PATH=/usr/bin",
		"http_proxy=http://wrong.invalid:3128",
		"HTTPS_PROXY=http://wrong.invalid:3128",
		"Http_Proxy=http://odd.invalid:3128",
		"NO_PROXY=*",
		"PANGOLIN_SESSION=parent-123",
		"HOME=/home/agent",
	}
	req.Network = testNetwork()

	plan, err := BuildPlan(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(managed): %v", err)
	}
	// Conflicting inherited proxy state never reaches the child, in any case.
	for _, entry := range plan.Env {
		if strings.Contains(entry, "wrong.invalid") || strings.Contains(entry, "odd.invalid") {
			t.Fatalf("managed plan kept a conflicting proxy value: %q", entry)
		}
		if entry == "NO_PROXY=*" || entry == "no_proxy=*" {
			t.Fatalf("managed plan kept ambient %q", entry)
		}
		if strings.HasPrefix(entry, "PANGOLIN_SESSION=") {
			t.Fatalf("network patch reintroduced filtered harness state: %q", entry)
		}
	}
	for _, want := range []string{
		"HTTP_PROXY=http://127.0.0.1:18081",
		"HTTPS_PROXY=http://127.0.0.1:18081",
		"http_proxy=http://127.0.0.1:18081",
		"https_proxy=http://127.0.0.1:18081",
		"NO_PROXY=127.0.0.1,::1,localhost",
		"no_proxy=127.0.0.1,::1,localhost",
		"PATH=/usr/bin",
		"HOME=/home/agent",
		"PANGOLIN_HOME=~/.pangolin",
	} {
		if !containsEnv(plan.Env, want) {
			t.Errorf("plan.Env = %v, want %q", plan.Env, want)
		}
	}
	// The set half is appended in patch order, after every surviving entry.
	setOrder := []string{"HTTP_PROXY=", "HTTPS_PROXY=", "http_proxy=", "https_proxy=", "NO_PROXY=", "no_proxy="}
	last := -1
	for _, prefix := range setOrder {
		found := -1
		for i, entry := range plan.Env {
			if strings.HasPrefix(entry, prefix) {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("plan.Env = %v, missing %q", plan.Env, prefix)
		}
		if found < last {
			t.Fatalf("plan.Env = %v, set half out of patch order at %q", plan.Env, prefix)
		}
		last = found
	}
	// The Record travels as provenance, never mixed into env.
	got, ok := plan.NetworkProvenanceSnapshot()
	if !ok {
		t.Fatal("managed plan carries no network provenance")
	}
	if !reflect.DeepEqual(got, req.Network.Record) {
		t.Fatalf("provenance = %#v, want %#v", got, req.Network.Record)
	}
	for _, entry := range plan.Env {
		if strings.Contains(entry, "egress-a") || strings.Contains(entry, "1e5912de") {
			t.Fatalf("Record leaked into the child env: %q", entry)
		}
	}
	// The snapshot is detached: mutating the returned value cannot move the plan.
	got.ProfileRef = "mutated"
	again, ok := plan.NetworkProvenanceSnapshot()
	if !ok || again.ProfileRef != "egress-a" {
		t.Fatal("network provenance aliases the returned value")
	}
}

func TestBuildPlanJoinsNetworkSetHalfIntoOwnedEnv(t *testing.T) {
	sys := newPangolin()
	registry := registerPangolin(t, sys)
	req := pangolinRequest()
	req.Network = testNetwork()

	got, err := BuildPlanWithEnvironment(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlanWithEnvironment(managed): %v", err)
	}
	for _, want := range []string{
		"HTTP_PROXY=http://127.0.0.1:18081",
		"HTTPS_PROXY=http://127.0.0.1:18081",
		"http_proxy=http://127.0.0.1:18081",
		"https_proxy=http://127.0.0.1:18081",
		"NO_PROXY=127.0.0.1,::1,localhost",
		"no_proxy=127.0.0.1,::1,localhost",
		"PANGOLIN_HOME=~/.pangolin",
	} {
		if !containsEnv(got.OwnedEnv, want) {
			t.Errorf("OwnedEnv = %v, want %q", got.OwnedEnv, want)
		}
	}
	for i := 1; i < len(got.OwnedEnv); i++ {
		if got.OwnedEnv[i] < got.OwnedEnv[i-1] {
			t.Fatalf("OwnedEnv = %v, not sorted; later layers diff it", got.OwnedEnv)
		}
	}
	if _, ok := got.Plan.NetworkProvenanceSnapshot(); !ok {
		t.Fatal("owned-env path dropped the network provenance")
	}
}

func TestBuildPlanRefusesMalformedNetworkPatch(t *testing.T) {
	valid := testNetwork()
	cases := map[string]func(*Network){
		"unset empty name": func(n *Network) { n.Patch.Unset = []string{""} },
		"unset invalid name": func(n *Network) {
			n.Patch.Unset = []string{"HTTP-PROXY"}
		},
		"set empty name": func(n *Network) {
			n.Patch.Set = []envpatch.Pair{{Name: "", Value: "http://127.0.0.1:18081"}}
		},
		"set invalid name": func(n *Network) {
			n.Patch.Set = []envpatch.Pair{{Name: "HTTP PROXY", Value: "http://127.0.0.1:18081"}}
		},
		"set duplicate names": func(n *Network) {
			n.Patch.Set = []envpatch.Pair{
				{Name: "HTTP_PROXY", Value: "http://127.0.0.1:18081"},
				{Name: "HTTP_PROXY", Value: "http://127.0.0.1:18082"},
			}
		},
		"set NUL value": func(n *Network) {
			n.Patch.Set = []envpatch.Pair{{Name: "HTTP_PROXY", Value: "http://x\x00y"}}
		},
		"set without unset": func(n *Network) {
			n.Patch.Unset = nil
		},
		"patch without Record": func(n *Network) { n.Record = binding.Record{} },
		"Record without patch": func(n *Network) {
			n.Patch = envpatch.Patch{}
		},
		"Record bad schema": func(n *Network) { n.Record.Schema = "relux-network-binding-record-v9" },
		"Record empty ref":  func(n *Network) { n.Record.ProfileRef = "  " },
		"Record empty digest": func(n *Network) {
			n.Record.ProfileDigest = ""
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			sys := newPangolin()
			registry := registerPangolin(t, sys)
			req := pangolinRequest()
			network := valid.Clone()
			mutate(&network)
			req.Network = network

			_, err := BuildPlan(registry, req, LaunchModeExec)
			if !errors.Is(err, ErrNetworkProfileInvalid) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkProfileInvalid", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeProfileInvalid {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeProfileInvalid)
			}
			for _, surface := range []string{"ResolveBinary", "Argv", "ChildEnv", "Stdin"} {
				if sys.calls[surface] != 0 {
					t.Errorf("%s was dispatched %d time(s) for a malformed carrier; the refusal must come before every plugin surface", surface, sys.calls[surface])
				}
			}
		})
	}
}

func TestBuildPlanRefusesNetworkPatchTouchingReservedOrOwned(t *testing.T) {
	valid := testNetwork()
	cases := map[string]func(*Network){
		"set reserved run id": func(n *Network) {
			n.Patch.Set = append(n.Patch.Set, envpatch.Pair{Name: EnvRunID, Value: "RUN-EVIL"})
		},
		"unset reserved run id": func(n *Network) {
			n.Patch.Unset = append(n.Patch.Unset, EnvTaskID)
		},
		"unset reserved folded case": func(n *Network) {
			n.Patch.Unset = append(n.Patch.Unset, strings.ToLower(EnvBoardDir))
		},
		"set owned home": func(n *Network) {
			n.Patch.Set = append(n.Patch.Set, envpatch.Pair{Name: "PANGOLIN_HOME", Value: "/evil"})
		},
		"unset owned home": func(n *Network) {
			n.Patch.Unset = append(n.Patch.Unset, "PANGOLIN_HOME")
		},
		"unset owned folded case": func(n *Network) {
			n.Patch.Unset = append(n.Patch.Unset, "pangolin_home")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			sys := newPangolin()
			registry := registerPangolin(t, sys)
			req := pangolinRequest()
			network := valid.Clone()
			mutate(&network)
			req.Network = network

			_, err := BuildPlan(registry, req, LaunchModeExec)
			if !errors.Is(err, ErrNetworkConfigurationConflict) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkConfigurationConflict", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeConfigurationConflict {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeConfigurationConflict)
			}
			if sys.calls["Stdin"] != 0 {
				t.Error("Stdin was built for a patch that rewrites reserved or owned state; the refusal must come before it")
			}
		})
	}
}

func TestBuildPlanAdmitsNetworkSetNamesThatMerelyResembleOwned(t *testing.T) {
	// Set matching is exact: pangolin_home is a different variable from
	// PANGOLIN_HOME on a case-sensitive environment, so setting it does not
	// rewrite owned state. Unset matching stays case-insensitive because
	// Patch.Apply removes that way. Without this control the reserved/owned
	// gate above would be indistinguishable from a gate that refuses
	// everything case-insensitively.
	sys := newPangolin()
	registry := registerPangolin(t, sys)
	req := pangolinRequest()
	network := testNetwork()
	network.Patch.Set = append(network.Patch.Set,
		envpatch.Pair{Name: "pangolin_home", Value: "/not-owned"},
		envpatch.Pair{Name: strings.ToLower(EnvRunID), Value: "not-the-run"},
	)
	req.Network = network

	plan, err := BuildPlan(registry, req, LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan(case-adjacent set names): %v", err)
	}
	if !containsEnv(plan.Env, "pangolin_home=/not-owned") {
		t.Errorf("plan.Env = %v, want the case-adjacent set entry applied", plan.Env)
	}
}

// TestBuildPlanRefusesNetworkScopeWithoutAVerifiedAdapter is the V1
// regression test (round-1 merged finding): a well-formed scope for a plugin
// that declares no verified network adapter is refused with typed
// network_scope_unsupported before any plugin surface is dispatched — never
// admitted by default, never launched without the scope.
func TestBuildPlanRefusesNetworkScopeWithoutAVerifiedAdapter(t *testing.T) {
	sys := newPangolin()
	sys.caps.NetworkAdapters = nil
	registry := registerPangolin(t, sys)
	req := pangolinRequest()
	req.Network = testNetwork()

	_, err := BuildPlan(registry, req, LaunchModeExec)
	if !errors.Is(err, ErrNetworkScopeUnsupported) {
		t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
	}
	if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
		t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
	}
	for _, surface := range []string{"ResolveBinary", "Argv", "ChildEnv", "Stdin"} {
		if sys.calls[surface] != 0 {
			t.Errorf("%s was dispatched %d time(s) for an undeclared scope; the refusal must come before every plugin surface", surface, sys.calls[surface])
		}
	}
}

// TestBuildPlanRefusesNetworkScopeForUndeclaredAdapterIdentity pins the
// exact-tuple rule: a declared adapter admits only the Record identities that
// equal it member for member. Each row changes exactly one of the four
// members, so a mutant that weakens the comparison — matching the adapter
// alone, or any three members — admits the neighbouring row and must fail
// that subtest by name.
func TestBuildPlanRefusesNetworkScopeForUndeclaredAdapterIdentity(t *testing.T) {
	cases := map[string]func(*binding.AdapterIdentity){
		"different adapter":    func(identity *binding.AdapterIdentity) { identity.Adapter = "muse-env-v1" },
		"different harness":    func(identity *binding.AdapterIdentity) { identity.Harness = "codex" },
		"different build":      func(identity *binding.AdapterIdentity) { identity.Build = "9.9.9" },
		"different entrypoint": func(identity *binding.AdapterIdentity) { identity.Entrypoint = "dry-run" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			sys := newPangolin()
			registry := registerPangolin(t, sys)
			req := pangolinRequest()
			network := testNetwork()
			mutate(&network.Record.AdapterIdentity)
			req.Network = network

			_, err := BuildPlan(registry, req, LaunchModeExec)
			if !errors.Is(err, ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
}

func TestNetworkCloneDetachesPatchSlices(t *testing.T) {
	original := testNetwork()
	cloned := original.Clone()
	if !reflect.DeepEqual(cloned, original) {
		t.Fatal("Clone changed the value")
	}
	cloned.Patch.Unset[0] = "MUTATED"
	cloned.Patch.Set[0].Value = "http://mutated.invalid:1"
	cloned.Record.ProfileRef = "mutated"
	if original.Patch.Unset[0] == "MUTATED" || original.Patch.Set[0].Value == "http://mutated.invalid:1" {
		t.Fatal("Clone aliases the patch slices")
	}
	if original.Record.ProfileRef == "mutated" {
		t.Fatal("Clone aliases the Record")
	}
}
