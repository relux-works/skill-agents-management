package agentic_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/agy"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/gemini"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pi"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
	_ "github.com/relux-works/skill-agents-management/pkg/agentic/systems/qwen"
)

// TestBuildPlanRefusesNetworkScopeForEverySystemWithoutAVerifiedAdapter is the
// round-1 V1 gate over the REAL registry: every registered plugin refuses a
// well-formed non-zero Network with typed network_scope_unsupported, because
// no plugin declares a verified adapter tuple in this revision.
//
// The table is the registry, not a hand-picked list: it ranges over
// Default.IDs(), so a ninth plugin is covered the moment it registers — and
// refuses by default, because the zero Capabilities declaration admits
// nothing. The dry-run mode is the shared grammar every system declares, and
// the one request shape passes every contract gate before the admission gate
// on all eight systems (pi's pre-plan preparation needs the provider/model
// identity and profile; no other system reads them). A mutant that admits one
// undeclared plugin — exempting any single id from the gate — fails exactly
// that subtest.
func TestBuildPlanRefusesNetworkScopeForEverySystemWithoutAVerifiedAdapter(t *testing.T) {
	ids := agentic.Default.IDs()
	// The closed set this revision verifies: nothing. A new registration
	// fails here until its author states the declaration beside it — the
	// loop above already refuses it, and this is the acknowledgement that
	// the refusal is the intent, not an accident of an empty table.
	want := []agentic.SystemID{
		"antigravity", "claude-code", "codex", "gemini-cli",
		"muse", "pi", "pi-native", "qwen-code",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("Default.IDs() = %v, want %v; a registration changed without updating the admission table", ids, want)
	}
	for _, id := range ids {
		t.Run(string(id), func(t *testing.T) {
			req := agentic.LaunchRequest{
				System:              id,
				SystemModelIdentity: "test-provider/test-model",
				Model:               agentic.Model{ID: "test-model", Effort: agentic.EffortSupportNone},
				Profile:             "test-profile",
				Network:             registryNetworkCarrier(),
			}
			_, err := agentic.BuildPlan(agentic.Default, req, agentic.LaunchModeDryRun)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan(%s, managed) err = %v, want ErrNetworkScopeUnsupported", id, err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildPlan(%s, managed) err = %v, want typed %q", id, err, refusal.CodeScopeUnsupported)
			}
		})
	}
}

// TestBuildPlanRefusesNetworkBeforePiPreparation is the N1 field reproducer
// (round-2 merged finding, panels 65kwny + 1emymd + 205pku): in exec mode the
// pi preparer reads PromptPath (systems/pi/args.go turnPrompt), so on rev 2 a
// request with an unreadable prompt file died in the preparer and the network
// refusal never fired. On this candidate the typed network error wins for both
// refusal classes even though the preparer would fail first.
func TestBuildPlanRefusesNetworkBeforePiPreparation(t *testing.T) {
	malformed := registryNetworkCarrier()
	malformed.Patch.Unset = []string{""}
	cases := map[string]struct {
		network  agentic.Network
		wantErr  error
		wantCode string
	}{
		"malformed":  {network: malformed, wantErr: agentic.ErrNetworkProfileInvalid, wantCode: refusal.CodeProfileInvalid},
		"undeclared": {network: registryNetworkCarrier(), wantErr: agentic.ErrNetworkScopeUnsupported, wantCode: refusal.CodeScopeUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := agentic.LaunchRequest{
				System:              "pi",
				SystemModelIdentity: "test-provider/test-model",
				Model:               agentic.Model{ID: "test-model", Effort: agentic.EffortSupportNone},
				Profile:             "test-profile",
				PromptPath:          "/nonexistent/does-not-exist-143e84.md",
				Network:             tc.network,
			}
			_, err := agentic.BuildPlan(agentic.Default, req, agentic.LaunchModeExec)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("BuildPlan(pi, PromptPath unreadable) err = %v, want %v", err, tc.wantErr)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != tc.wantCode {
				t.Fatalf("BuildPlan(pi, PromptPath unreadable) err = %v, want typed %q", err, tc.wantCode)
			}
		})
	}
}

// registryNetworkCarrier is a well-formed scope naming a REAL harness tuple —
// generic-env-v1 over codex — so the refusal on every system, codex included,
// is the admission gate firing on an undeclared plugin, not the shape gate
// firing on a malformed carrier.
func registryNetworkCarrier() agentic.Network {
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
				Adapter: "generic-env-v1", Harness: "codex", Build: "0.153.2", Entrypoint: "exec",
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
