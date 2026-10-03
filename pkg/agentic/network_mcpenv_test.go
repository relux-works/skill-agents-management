package agentic_test

import (
	"reflect"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// testMCPPatch is a two-name patch with both halves exercised: HTTP_PROXY is
// unset-then-set, ALL_PROXY is unset-only, and NO_PROXY is set-only so a
// mutant that only honours one half cannot hide behind the other.
func testMCPPatch() envpatch.Patch {
	return envpatch.Patch{
		Unset: []string{"HTTP_PROXY", "ALL_PROXY"},
		Set: []envpatch.Pair{
			{Name: "HTTP_PROXY", Value: "http://managed:18081"},
			{Name: "NO_PROXY", Value: "localhost"},
		},
	}
}

func TestApplyNetworkPatchToServerEnvsVisitsEveryEntry(t *testing.T) {
	t.Parallel()
	patch := testMCPPatch()
	servers := []agentic.MCPServerEnv{
		{Server: "first"},
		{Server: "second", Env: []envpatch.Pair{{Name: "KEEP", Value: "1"}}},
		{Server: "third"},
	}
	got := agentic.ApplyNetworkPatchToServerEnvs(patch, servers)
	if len(got) != len(servers) {
		t.Fatalf("visited %d entries, want %d", len(got), len(servers))
	}
	for i, entry := range got {
		if entry.Server != servers[i].Server {
			t.Fatalf("entry %d is %q, want %q; the merge reordered or dropped an entry", i, entry.Server, servers[i].Server)
		}
		if !reflect.DeepEqual(lastPairs(entry.Env, len(patch.Set)), patch.Set) {
			t.Errorf("entry %q ends with %v, want the set half %v appended", entry.Server, entry.Env, patch.Set)
		}
	}
	if !reflect.DeepEqual(got[0].Env, patch.Set) {
		t.Errorf("the entry with no env block got %v, want exactly the set half %v", got[0].Env, patch.Set)
	}
}

func lastPairs(pairs []envpatch.Pair, n int) []envpatch.Pair {
	if len(pairs) < n {
		return pairs
	}
	return pairs[len(pairs)-n:]
}

// TestApplyNetworkPatchToServerEnvsMergesOneEntry pins the per-entry rule: the
// unset half removes (including unset-only and mixed-case names), the set half
// overwrites exact names, and every untouched member survives in order.
func TestApplyNetworkPatchToServerEnvsMergesOneEntry(t *testing.T) {
	t.Parallel()
	patch := testMCPPatch()
	existing := []envpatch.Pair{
		{Name: "KEEP_FIRST", Value: "1"},
		{Name: "HTTP_PROXY", Value: "http://wrong:1"},
		{Name: "KEEP_MIDDLE", Value: "2"},
		{Name: "ALL_PROXY", Value: "socks5://stale:1080"},
		{Name: "Http_Proxy", Value: "http://odd:1"},
		{Name: "HTTP_PROXY_EXTRA", Value: "1"},
		{Name: "KEEP_LAST", Value: "3"},
	}
	got := agentic.ApplyNetworkPatchToServerEnvs(patch, []agentic.MCPServerEnv{{Server: "local", Env: existing}})
	want := []envpatch.Pair{
		{Name: "KEEP_FIRST", Value: "1"},
		{Name: "KEEP_MIDDLE", Value: "2"},
		{Name: "HTTP_PROXY_EXTRA", Value: "1"},
		{Name: "KEEP_LAST", Value: "3"},
		{Name: "HTTP_PROXY", Value: "http://managed:18081"},
		{Name: "NO_PROXY", Value: "localhost"},
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0].Env, want) {
		t.Fatalf("merged = %v, want %v", got[0].Env, want)
	}
}

// TestApplyNetworkPatchToServerEnvsWithNoEntries holds the empty input: no
// entry in, no entry out.
func TestApplyNetworkPatchToServerEnvsWithNoEntries(t *testing.T) {
	t.Parallel()
	for name, servers := range map[string][]agentic.MCPServerEnv{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			if got := agentic.ApplyNetworkPatchToServerEnvs(testMCPPatch(), servers); len(got) != 0 {
				t.Fatalf("got %v, want no entries", got)
			}
		})
	}
}

// TestApplyNetworkPatchToServerEnvsWithAnEmptyPatch holds that an unmanaged
// patch changes no member: entries pass through with their blocks intact.
func TestApplyNetworkPatchToServerEnvsWithAnEmptyPatch(t *testing.T) {
	t.Parallel()
	servers := []agentic.MCPServerEnv{
		{Server: "local", Env: []envpatch.Pair{{Name: "KEEP", Value: "1"}, {Name: "HTTP_PROXY", Value: "http://ambient:1"}}},
	}
	got := agentic.ApplyNetworkPatchToServerEnvs(envpatch.Patch{}, servers)
	if !reflect.DeepEqual(got, servers) {
		t.Fatalf("got %v, want the entries unchanged %v", got, servers)
	}
}

// TestApplyNetworkPatchToServerEnvsDoesNotAliasItsInput keeps a caller from
// writing through the merged blocks into its own entries or the patch.
func TestApplyNetworkPatchToServerEnvsDoesNotAliasItsInput(t *testing.T) {
	t.Parallel()
	patch := testMCPPatch()
	servers := []agentic.MCPServerEnv{
		{Server: "local", Env: []envpatch.Pair{{Name: "KEEP", Value: "1"}}},
	}
	got := agentic.ApplyNetworkPatchToServerEnvs(patch, servers)
	got[0].Env[0].Value = "MUTATED"
	got[0].Env[1].Value = "MUTATED"
	if servers[0].Env[0].Value != "1" {
		t.Error("the merged block aliases the caller's entry env")
	}
	if patch.Set[0].Value != "http://managed:18081" {
		t.Error("the merged block aliases the patch's set half")
	}
}
