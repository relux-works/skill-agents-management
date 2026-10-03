package agentic

import (
	"strings"

	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
)

// MCPServerEnv is one MCP server entry's env block: the entry's name plus its
// existing env members in render order.
//
// It is the harness-neutral unit the env-injecting network adapters share.
// codex-env-v1 parses it out of `-c mcp_servers.<name>.env={...}` pairs and
// renders the merged block back into the same spelling; muse-env-v1 will parse
// and render its own JSON entries around this same type. Transport selection
// (which entries HAVE an env block) stays with the plugin — the core never
// learns one harness's transport words — and so does validation of the
// entry's own shape.
type MCPServerEnv struct {
	// Server names the entry the block belongs to. It is carried through
	// untouched and matched by the caller, never interpreted here.
	Server string
	// Env holds the entry's existing members in render order. A nil or empty
	// slice is an entry with no env block yet.
	Env []envpatch.Pair
}

// ApplyNetworkPatchToServerEnvs injects a managed network patch into every MCP
// server entry's env block.
//
// Per entry the rule mirrors envpatch.Patch.Apply, the generic adapter's own
// application: members whose name matches an unset name (case-insensitively,
// so a stale Http_Proxy goes with HTTP_PROXY) or a set name (exactly) are
// removed, every other member is preserved in order, and the set half is
// appended in patch order. Unset-only names (ALL_PROXY in the generic patch)
// remove without re-adding. The input — the slice and every entry's Env — is
// never mutated; the result is freshly allocated throughout.
//
// Every entry in the input is visited exactly once, in order. Skipping one
// would leave a stdio child on ambient routing while its siblings are
// managed, which is a launch that looks uniformly managed and is not.
func ApplyNetworkPatchToServerEnvs(patch envpatch.Patch, servers []MCPServerEnv) []MCPServerEnv {
	out := make([]MCPServerEnv, 0, len(servers))
	for _, server := range servers {
		merged := make([]envpatch.Pair, 0, len(server.Env)+len(patch.Set))
		for _, member := range server.Env {
			if patchUnsets(patch, member.Name) || patchSets(patch, member.Name) {
				continue
			}
			merged = append(merged, member)
		}
		merged = append(merged, patch.Set...)
		out = append(out, MCPServerEnv{Server: server.Server, Env: merged})
	}
	return out
}

// patchUnsets reports whether name matches an unset name case-insensitively,
// the same match envpatch.Patch.Apply removes by.
func patchUnsets(patch envpatch.Patch, name string) bool {
	for _, unset := range patch.Unset {
		if strings.EqualFold(name, unset) {
			return true
		}
	}
	return false
}

// patchSets reports whether name matches a set name exactly, the same match
// envpatch.Patch.Apply removes by before appending the set half.
func patchSets(patch envpatch.Patch, name string) bool {
	for _, pair := range patch.Set {
		if pair.Name == name {
			return true
		}
	}
	return false
}
