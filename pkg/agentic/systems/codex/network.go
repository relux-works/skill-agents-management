package codex

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the codex-env-v1 network adapter: the generic environment
// patch BuildPlan applies, PLUS the set half injected into each MCP server
// entry's env block.
//
// codex-cli 0.159.0 sends model and startup traffic through the proxy, but
// its MCP stdio children lose the proxy variables (R4); an explicit env
// block in each server entry works (R4b). The adapter is therefore the
// generic patch plus, for every command-backed entry the harness will
// launch, the set half in that entry's env — merged with any
// caller-supplied block by the shared agentic.ApplyNetworkPatchToServerEnvs,
// the same MCP mechanism muse-env-v1 will reuse. Verified against the
// installed codex-cli 0.159.0: argv-defined servers merge command, args and
// env pairs into the one entry, and the resolved entry carries the block to
// the stdio child.
//
// Coverage is defined by what codex will actually launch (round-1 E1): the
// request-declared entries (composition, descriptors) AND the entries codex
// loads from its own files — the effective CODEX_HOME config plus the one
// profile the launch selects. Request-declared entries are rewritten in the
// pair streams below; file entries, which the plan cannot rewrite, gain one
// per-key `mcp_servers.<name>.env.<VAR>=...` pair per set var, and a
// command-backed file entry the adapter cannot cover reliably fails the
// whole managed launch closed with a typed refusal naming the server. A
// launch never goes out half-covered.
//
// Only command-backed entries are visited. An http entry has no child
// process and codex accepts env only for stdio servers, so the grammar
// refuses a caller-supplied env block on one and injection emits none — an
// injected http env pair would be a shape this plugin's own validator
// refuses.

// refuseUnadmittedNetwork is the adapter's admission second line: BuildPlan's
// gate refuses first, before any plugin surface runs, and this holds the same
// refusal for a caller holding the plugin directly. A non-zero scope no
// declared tuple names is refused rather than half-applied — argv injection
// without the process patch (or the reverse) would be a launch that looks
// managed and is not.
func refuseUnadmittedNetwork(network agentic.Network) error {
	if network.IsZero() {
		return nil
	}
	if New().Capabilities().AdmitsNetwork(network.Record.AdapterIdentity) {
		return nil
	}
	return fmt.Errorf("codex: %w: no verified codex network adapter names this scope", agentic.ErrNetworkScopeUnsupported)
}

// injectNetworkEnvIntoPrefix rewrites a legacy composition prefix for a
// managed scope: every command-backed entry's env block carries the patch
// merged in, and nothing else in the stream moves.
//
// A prefix that is not a `-c` pair stream is refused rather than guessed
// at. It is unreachable via BuildPlan — the grammar validator refuses it
// first — so this fires only for a direct Args caller that bypassed
// validation, where emitting a half-managed stream would be worse.
func injectNetworkEnvIntoPrefix(prefix []string, servers []agentic.CompositionServer, patch envpatch.Patch) ([]string, error) {
	if len(prefix)%2 != 0 {
		return nil, fmt.Errorf("codex: cannot inject the network env into an MCP pair stream with an unpaired element")
	}
	pairs := make([]string, 0, len(prefix)/2)
	for i := 0; i < len(prefix); i += 2 {
		if prefix[i] != "-c" {
			return nil, fmt.Errorf("codex: cannot inject the network env into an MCP pair stream carrying %q", prefix[i])
		}
		pairs = append(pairs, prefix[i+1])
	}
	var merged []string

	if mergedValue, err := applyNetworkEnvToMCPPairs(pairs, servers, patch); err != nil {
		return nil, err
	} else {
		merged = mergedValue
	}
	out := make([]string, 0, len(merged)*2)
	for _, pair := range merged {
		out = append(out, "-c", pair)
	}
	return out, nil
}

// injectNetworkEnvIntoOverrides rewrites context-derived MCP overrides for a
// managed scope. The overrides carry no caller env blocks — encodeCodexMCP
// emits none — so every command-backed entry gains exactly the set half;
// routing them through the same pair transform as the legacy prefix keeps
// one merge site rather than a special case that could drift from it.
func injectNetworkEnvIntoOverrides(overrides []configOverride, servers []agentic.CompositionServer, patch envpatch.Patch) ([]configOverride, error) {
	pairs := make([]string, 0, len(overrides))
	for _, override := range overrides {
		pairs = append(pairs, override.key+"="+override.value)
	}
	var merged []string

	if mergedValue, err := applyNetworkEnvToMCPPairs(pairs, servers, patch); err != nil {
		return nil, err
	} else {
		merged = mergedValue
	}
	out := make([]configOverride, 0, len(merged))
	for _, pair := range merged {
		key, value, _ := strings.Cut(pair, "=")
		out = append(out, configOverride{key: key, value: value})
	}
	return out, nil
}

// applyNetworkEnvToMCPPairs is the one pair transform both MCP streams share:
// for every command-backed server in servers, in order, the stream gains
// exactly one `mcp_servers.<name>.env={...}` pair carrying the patch merged
// with that entry's existing block. Caller env pairs for those entries are
// consumed (re-emitted merged); every other pair — other fields, http
// entries, unknown servers — passes through byte for byte.
//
// "Command-backed" is isCommandBackedTransport, the SAME predicate the
// grammar validator uses — not the exact word "stdio". The validator admits
// every non-http spelling as stdio-shaped, so keying injection on the exact
// word would admit managed launches it never injects into (round-1 E2).
//
// An env pair that fails to parse is passed through untouched AND the managed
// block is still appended for that entry. It is unreachable via BuildPlan
// (the grammar validator refuses it first); a direct caller keeps its bytes
// rather than having them silently dropped, and the harness refuses the
// malformed value loudly at launch. A second env pair for one entry resolves
// last-wins, as a later `-c` wins in codex.
func applyNetworkEnvToMCPPairs(pairs []string, servers []agentic.CompositionServer, patch envpatch.Patch) ([]string, error) {
	var commandBacked []string
	isCommandBacked := make(map[string]bool, len(servers))
	for _, server := range servers {
		if !isCommandBackedTransport(server.Transport) || isCommandBacked[server.Name] {
			continue
		}
		isCommandBacked[server.Name] = true
		commandBacked = append(commandBacked, server.Name)
	}
	if err := refuseUnaddressableServerNames(commandBacked); err != nil {
		return nil, err
	}
	existing := make(map[string][]envpatch.Pair, len(commandBacked))
	out := make([]string, 0, len(pairs)+len(commandBacked))
	for _, pair := range pairs {
		server, table, ok := matchEnvPair(pair, isCommandBacked)
		if !ok {
			out = append(out, pair)
			continue
		}
		members, err := parseTOMLStringTable(table)
		if err != nil {
			out = append(out, pair)
			continue
		}
		existing[server] = sortedEnvPairs(members)
	}
	entries := make([]agentic.MCPServerEnv, 0, len(commandBacked))
	for _, name := range commandBacked {
		entries = append(entries, agentic.MCPServerEnv{Server: name, Env: existing[name]})
	}
	for _, entry := range agentic.ApplyNetworkPatchToServerEnvs(patch, entries) {
		table, err := encodeTOMLTable(entry.Env)
		if err != nil {
			return nil, err
		}
		out = append(out, "mcp_servers."+entry.Server+".env="+table)
	}
	return out, nil
}

// matchEnvPair reports whether pair assigns the env block of a known
// command-backed server, and if so which server and what table value. The
// match is exact — `<name>.env` against a declared name — so a dotted server
// name cannot misroute: `mcp_servers.a.b.command` never matches server `a`,
// and an env pair for an unknown or http server is not ours to rewrite.
func matchEnvPair(pair string, isCommandBacked map[string]bool) (server, table string, ok bool) {
	key, value, found := strings.Cut(pair, "=")
	if !found || !strings.HasPrefix(key, mcpServersKeyPrefix) {
		return "", "", false
	}
	candidate, found := strings.CutSuffix(strings.TrimPrefix(key, mcpServersKeyPrefix), ".env")
	if !found || candidate == "" || !isCommandBacked[candidate] {
		return "", "", false
	}
	return candidate, value, true
}

// sortedEnvPairs renders a parsed block into pairs in a deterministic order.
// TOML mappings carry no order and the parse lands in a map, so the preserved
// members are sorted by name; the set half the shared merge appends keeps
// patch order behind them. Member SET is what the adapter preserves —
// order within the mapping is not significant to codex.
func sortedEnvPairs(members map[string]string) []envpatch.Pair {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]envpatch.Pair, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, envpatch.Pair{Name: name, Value: members[name]})
	}
	return pairs
}

// encodeTOMLTable renders one merged env block as the inline table the
// grammar admits. Values reuse encodeTOMLValue's JSON-is-TOML spelling, so
// what this emits is what the validator parses.
func encodeTOMLTable(pairs []envpatch.Pair) (string, error) {
	var rendered strings.Builder
	rendered.WriteByte('{')
	for i, pair := range pairs {
		if i > 0 {
			rendered.WriteByte(',')
		}
		key, err := encodeTOMLKey(pair.Name)
		if err != nil {
			return "", err
		}
		value, err := encodeTOMLValue(pair.Value)
		if err != nil {
			return "", err
		}
		rendered.WriteString(key)
		rendered.WriteByte('=')
		rendered.WriteString(value)
	}
	rendered.WriteByte('}')
	return rendered.String(), nil
}

// encodeTOMLKey renders one member name: bare when the TOML bare-key alphabet
// covers it, quoted otherwise. A quoted caller key round-trips through the
// validator's real parser rather than through a second reading of the spec.
func encodeTOMLKey(key string) (string, error) {
	if isTOMLBareKey(key) {
		return key, nil
	}
	return encodeTOMLValue(key)
}

// isTOMLBareKey reports whether key spells as a TOML bare key: ASCII
// letters, digits, underscore and dash, non-empty.
func isTOMLBareKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// ---- managed file inventory: the entries codex loads itself ----
//
// A managed launch covers every command-backed MCP server codex will launch,
// not just the ones the request declares (round-1 E1: a stdio server in the
// selected CODEX_HOME/config.toml launched with env null under an admitted
// scope). Codex 0.159.0 loads MCP entries from three channels, established
// against the installed binary with disposable homes:
//
//   - the `-c mcp_servers.*` pairs on argv (composition, descriptors),
//     covered by the pair transform above;
//   - the effective home's config.toml `[mcp_servers.*]` tables, where the
//     home is CODEX_HOME when set and non-empty, else HOME/.codex (an absent
//     file, and an empty one, mean no servers; an unreadable or unparseable
//     one fails the launch in the harness, natively);
//   - the one profile the launch selects (`-p <name>`, this plugin's Profile
//     or the Curator `-p curator-mcp`), layered from
//     $CODEX_HOME/<name>.config.toml on top of the base config with a
//     deep per-leaf merge. A missing profile file loads nothing; an
//     unreadable or unparseable SELECTED one fails the launch natively.
//     Unselected profile files are never read, by codex or here.
//
// A project `.codex/config.toml` carries no MCP entries (probed: only the
// home's servers list), and the legacy `profile = "..."` key is refused by
// 0.159.0, so neither is inventoried.
//
// File entries cannot be rewritten — the contract forbids touching shared
// MCP configuration — so each one gains per-key
// `mcp_servers.<name>.env.<VAR>=...` pairs carrying the set half (verified:
// the merged entry `codex mcp get` reports is identical to the whole-table
// spelling, and override leaves win over file leaves). `-c` overrides merge
// deeply and can never REMOVE a file leaf, so a file entry whose effective
// env keeps a member the patch removes, or whose name a `-c` key cannot
// address (only TOML-bare names route; quoted segments are not supported),
// fails the whole managed launch closed with a typed refusal naming the
// server. So does any inventory the adapter cannot establish: an
// unresolvable home, an unreadable or unparseable file, or an entry whose
// shape is not a string table. A read failure is never headroom for a
// launch, and unmanaged launches never read at all.

// fileMCPServer is one command-backed MCP server from codex's own files: its
// name plus its effective env members (base config merged with the selected
// profile, profile winning per leaf) in deterministic order.
type fileMCPServer struct {
	name string
	env  []envpatch.Pair
}

// managedFileServerEnvPairs covers the file half of a managed launch: it
// reads the effective inventory and renders the per-key `-c` pairs, flat
// (`-c`, pair, …) for splicing after the request streams. Servers the
// request streams already cover — command-backed composition servers in the
// modes that render the prefix, descriptor servers in every mode — are
// checked but emit nothing: their whole-table pair already carries the set
// half over any file leaf, so a second spelling would only be a second
// writer to the same leaves.
func managedFileServerEnvPairs(req agentic.LaunchRequest, context contextValues, mode agentic.LaunchMode) ([]string, error) {
	var root string

	if rootValue, err := resolveManagedConfigRoot(req); err != nil {
		return nil, err
	} else {
		root = rootValue
	}
	// The profile name is trimmed because Args trims it before exec: the
	// child receives `-p <trimmed>` (or `--profile <trimmed>`), so the
	// inventory matches the child's bytes, not the request's. This is argv
	// matching, not path normalization — the home bytes the path is built
	// from are never trimmed.
	profile := strings.TrimSpace(req.Profile)
	if profile == "" && context.hasCuratorMCP {
		profile = "curator-mcp"
	}
	var servers []fileMCPServer

	if serversValue, err := readCodexMCPInventory(root, profile); err != nil {
		return nil, err
	} else {
		servers = serversValue
	}
	covered := make(map[string]bool, len(servers))
	if mode == agentic.LaunchModeExec || mode == agentic.LaunchModeDryRun {
		for _, server := range req.Composition.Servers {
			if isCommandBackedTransport(server.Transport) {
				covered[server.Name] = true
			}
		}
	}
	for _, server := range context.mcpServers {
		if isCommandBackedTransport(server.Transport) {
			covered[server.Name] = true
		}
	}
	return fileServerEnvOverridePairs(servers, covered, req.Network.Patch)
}

// managedChildEnv is the inventory's config-selecting input: the SAME child
// env the launch delivers — System.ChildEnv plus the single network
// application point — deduped so exactly one value per name exists. It CALLS
// ChildEnv rather than reimplementing it (round-4 K1: a mirrored
// filter+pin+patch computation is a second reader that can disagree with
// the first; the plugin filter, the local-provider CODEX_HOME pin when
// bound, and every run-context write reach the inventory through the same
// code that reaches the child). Reading req.Env instead would miss the pin
// on local-provider launches and a patch that redirects CODEX_HOME or HOME.
func managedChildEnv(req agentic.LaunchRequest) ([]string, error) {
	var env []string

	if envValue, err := New().ChildEnv(req.Env, req); err != nil {
		return nil, err
	} else {
		env = envValue
	}
	return dedupeManagedEnv(req.Network.Patch.Apply(env)), nil
}

// lastEnvValue reads the LAST `name=value` entry, ignoring bare `name`
// entries — the value the exec'd child resolves. Go's launcher dedups
// `key=value` pairs last-wins and passes bare entries through, and the
// child's getenv scans for the `name=` prefix, so a bare entry never
// satisfies the lookup. (The FIRST-wins reading used through rev 4 split
// the planner from the child across duplicate keys: round-4 K1.)
func lastEnvValue(env []string, name string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if key, value, found := strings.Cut(env[i], "="); found && key == name {
			return value, true
		}
	}
	return "", false
}

// dedupeManagedEnv normalizes a managed child environment so exactly one
// `key=value` entry exists per name: the LAST one wins, reproducing Go
// os/exec's unix delivery (dedupEnv: later values win, order of last
// occurrence kept). Entries without '=' are preserved verbatim — Go passes
// them through undeduped too — and an empty entry is dropped, as Go drops
// it before delivery. The input is never mutated.
//
// It runs on the managed child env BEFORE both the inventory and the child
// see it (System.ChildEnv when the scope is managed, and managedChildEnv
// after the patch), so the planner and codex resolve every config-selecting
// key from the same unambiguous value. Unmanaged envs never pass through
// it, so their bytes are untouched.
//
// NUL-containing entries are preserved, NOT dropped the way os/exec drops
// them: a Plan.Env carrying NUL fails at launch exactly as before, while
// dropping one here would turn a hard launcher error into a launch under a
// different env. The inventory reads such a home literally, fails to open
// it, and admits empty or refuses — and no child follows, so the split is
// safe by construction.
func dedupeManagedEnv(env []string) []string {
	seen := make(map[string]bool, len(env))
	rev := make([]string, 0, len(env))
	for i := len(env) - 1; i >= 0; i-- {
		entry := env[i]
		if entry == "" {
			continue
		}
		key, found := dedupeKeyOf(entry)
		if !found {
			rev = append(rev, entry)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		rev = append(rev, entry)
	}
	out := make([]string, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}

// dedupeKeyOf extracts the dedupe key exactly the way os/exec does,
// including its leading-'=' quirk: an entry starting with '=' keys on the
// span through the SECOND '=', and an entry with no '=' at all has no key
// and is never deduped.
func dedupeKeyOf(entry string) (string, bool) {
	i := strings.Index(entry, "=")
	if i == 0 {
		i = strings.Index(entry[1:], "=") + 1
	}
	if i < 0 {
		return "", false
	}
	return entry[:i], true
}

// resolveManagedConfigRoot resolves the configuration home the managed child
// will load: CODEX_HOME when set and non-empty, else HOME/.codex. Only the
// exact empty string counts as unset — codex falls back past it, and past
// it alone: a whitespace-only or space-padded value is a literal path the
// harness loads byte for byte (round-2 H1: trimming CODEX_HOME for the
// inventory while the child kept the literal bytes read a sibling home and
// launched its servers bare). Duplicate entries resolve last-wins — the
// value the launcher delivers (round-4 K1). A home that cannot be established from the
// launch fails closed: with neither variable the harness falls back to
// ambient user state no request carries, which this planner refuses to
// guess at. Relative homes resolve against the launch workdir, the child's
// own working directory; a relative home with no absolute workdir fails
// closed rather than guessing the launcher's cwd.
//
// The returned root is a LITERAL string — home bytes plus, for a relative
// home, the literal workdir prefix — never cleaned. Cleaning is what let a
// symlink/`..` home escape (round-3 J1: filepath.Join collapsed `link/..`
// lexically while the kernel followed the symlink for the child). The
// inventory opens exactly this string below, so both readers resolve it.
func resolveManagedConfigRoot(req agentic.LaunchRequest) (string, error) {
	var env []string

	if envValue, err := managedChildEnv(req); err != nil {
		return "", err
	} else {
		env = envValue
	}
	codexHome, codexPresent := lastEnvValue(env, "CODEX_HOME")
	if codexPresent && codexHome != "" {
		if root, ok := resolveManagedPath(codexHome, req.WorkDir); ok {
			return root, nil
		}
		return "", inventoryUnreadable("CODEX_HOME", "effective config home cannot be established")
	}
	home, homePresent := lastEnvValue(env, "HOME")
	if !homePresent || home == "" {
		return "", inventoryUnreadable("CODEX_HOME", "effective config home cannot be established")
	}
	if root, ok := resolveManagedPath(joinInventoryPath(home, ".codex"), req.WorkDir); ok {
		return root, nil
	}
	return "", inventoryUnreadable("CODEX_HOME", "effective config home cannot be established")
}

// resolveManagedPath resolves one managed inventory home exactly as the
// child will load it: byte for byte, with no trimming, no `~` expansion,
// no cleaning, and no other normalization the child env does not also get.
// An absolute home is used literally; a relative one gains the literal
// launch-workdir prefix by concatenation, and fails closed when the workdir
// is not absolute — the launcher's cwd is ambient state no request carries.
// `~` stays literal: codex 0.159.0 loads it as a relative name and fails
// stillborn when it is absent, it never expands.
//
// It deliberately does NOT call resolveCodexConfigRoot: that helper trims,
// expands and cleans for the stored-policy and provider surfaces, and
// sharing it here is what let a trailing-space home escape (round-2 H1).
// It deliberately does NOT call filepath.Join either: Join cleans
// lexically (`.`, `..`, redundant separators) while the child's literal
// load resolves through the kernel, and the two disagree across a symlink
// (round-3 J1). There is no normalization step left to diverge — the path
// the inventory opens below is built only by joinInventoryPath.
func resolveManagedPath(raw, workDir string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if isAbsInventoryPath(raw) {
		return raw, true
	}
	if workDir == "" || !isAbsInventoryPath(workDir) {
		return "", false
	}
	return joinInventoryPath(workDir, raw), true
}

// joinInventoryPath is the ONE function that builds a managed inventory
// file path: plain string concatenation of the directory, one `/`
// separator, and each element. The separator is skipped only when the
// directory already ends with one, so the home bytes themselves are never
// altered — no trimming, no cleaning of `.`, `..` or redundant slashes,
// no expansion. The returned string is exactly the string codex 0.159.0
// opens (probed: literal CODEX_HOME plus `config.toml`, relative to the
// process cwd; trailing-slash, `//`, `./` and symlink/`..` homes all load
// through kernel resolution), so opening it with os.ReadFile resolves the
// same file for both readers: the kernel follows symlinks and `..` the
// same way twice.
//
// This file imports no path package at all, so no Join, Clean, Abs or
// EvalSymlinks can reach this builder again; the guard test pins that.
func joinInventoryPath(dir string, elems ...string) string {
	out := dir
	for _, elem := range elems {
		if !strings.HasSuffix(out, "/") {
			out += "/"
		}
		out += elem
	}
	return out
}

// isAbsInventoryPath reports whether p is an absolute inventory path: a
// leading `/`, and nothing else. It is strings-only by construction —
// filepath.IsAbs would read the same on unix, but this file must not
// import path/filepath at all (see joinInventoryPath), so the check is
// spelled out rather than imported.
func isAbsInventoryPath(p string) bool {
	return strings.HasPrefix(p, "/")
}

// managedInventoryProfilePath builds the selected profile's file path with
// the same traversal guard codexProfilePath applies — an escaping name is
// never read — but joined by concatenation. It deliberately does NOT call
// codexProfilePath: that helper serves the stored-policy surface and joins
// with filepath.Join, whose lexical clean collapses a root carrying
// symlink/`..` and would read a sibling profile (round-3 J1). The guard is
// restated rather than shared because the join is the part that had to
// change; the name rule is identical (a name with no separator is its own
// base, so the Base check it replaces holds vacuously).
func managedInventoryProfilePath(root, name string) (string, bool) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	return joinInventoryPath(root, name+".config.toml"), true
}

// readCodexMCPInventory reads the effective file inventory: the base config
// plus the selected profile layered on top with a per-leaf merge. The result
// holds every command-backed entry — a `command` key present in either
// layer — sorted by name. An entry with no command key is not visited: a
// url-only entry has no child process, and a malformed neither-nor entry is
// the harness's own refusal at launch.
func readCodexMCPInventory(root, profile string) ([]fileMCPServer, error) {
	var base map[string]configMCPEntry

	if baseValue, err := readCodexMCPServersFile(joinInventoryPath(root, "config.toml"), "config.toml", ""); err != nil {
		return nil, err
	} else {
		base = baseValue
	}
	merged := base
	if strings.TrimSpace(profile) != "" {
		profilePath, ok := managedInventoryProfilePath(root, strings.TrimSpace(profile))
		if ok {
			var layered map[string]configMCPEntry
			subject := strings.TrimSpace(profile) + ".config.toml"
			if layeredValue, err := readCodexMCPServersFile(profilePath, subject, strings.TrimSpace(profile)); err != nil {
				return nil, err
			} else {
				layered = layeredValue
			}
			merged = layerConfigMCPEntries(base, layered)
		}
		// A name that escapes the root is never read: codex refuses such a
		// -p value at CLI parse, so the launch is stillborn and the file
		// would only be a read outside the request's home.
	}
	names := make([]string, 0, len(merged))
	for name, entry := range merged {
		if entry.command {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	servers := make([]fileMCPServer, 0, len(names))
	for _, name := range names {
		servers = append(servers, fileMCPServer{name: name, env: sortedEnvPairs(merged[name].env)})
	}
	return servers, nil
}

// configMCPEntry is one file entry's coverage shape: whether either layer
// carries a `command` key, and the effective string env table.
type configMCPEntry struct {
	command bool
	env     map[string]string
}

// layerConfigMCPEntries layers profile entries over base entries the way the
// harness does: per leaf, the profile winning. Absent profile leaves
// inherit the base — an entry the profile never names keeps its base
// command and env untouched.
func layerConfigMCPEntries(base, layered map[string]configMCPEntry) map[string]configMCPEntry {
	merged := make(map[string]configMCPEntry, len(base)+len(layered))
	for name, entry := range base {
		env := make(map[string]string, len(entry.env))
		for key, value := range entry.env {
			env[key] = value
		}
		merged[name] = configMCPEntry{command: entry.command, env: env}
	}
	for name, entry := range layered {
		under, found := merged[name]
		if !found {
			env := make(map[string]string, len(entry.env))
			for key, value := range entry.env {
				env[key] = value
			}
			merged[name] = configMCPEntry{command: entry.command, env: env}
			continue
		}
		for key, value := range entry.env {
			under.env[key] = value
		}
		under.command = under.command || entry.command
		merged[name] = under
	}
	return merged
}

// readCodexMCPServersFile reads one inventory file into coverage entries. A
// missing file loads nothing, exactly as the harness treats it; anything
// else that stops the read — an I/O error, unparseable TOML, or a section
// that is not a table of string-table entries — fails closed, because a
// partial inventory is headroom for an unlisted server. Profile is the
// selected profile's name for diagnostics, empty for the base config.
func readCodexMCPServersFile(path, subject, profile string) (map[string]configMCPEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]configMCPEntry{}, nil
		}
		return nil, inventoryFileUnreadable(subject, profile, "config file cannot be read")
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, inventoryFileUnreadable(subject, profile, "config file cannot be parsed")
	}
	section, found := config["mcp_servers"]
	if !found {
		return map[string]configMCPEntry{}, nil
	}
	tables, ok := section.(map[string]any)
	if !ok {
		return nil, inventoryFileUnreadable(subject, profile, "MCP inventory has an unsupported shape")
	}
	entries := make(map[string]configMCPEntry, len(tables))
	for name, raw := range tables {
		entry, ok := configMCPEntryShape(raw)
		if !ok {
			return nil, inventoryEntryUnreadable(name, subject, profile)
		}
		entries[name] = entry
	}
	return entries, nil
}

// configMCPEntryShape reduces one file entry to its coverage shape: command
// presence plus the string env table. Every other key — url, args,
// bearer_token_env_var, enabled, timeouts — is the harness's business, and a
// non-table entry or a non-string env member is not a shape this planner
// covers.
func configMCPEntryShape(raw any) (configMCPEntry, bool) {
	table, ok := raw.(map[string]any)
	if !ok {
		return configMCPEntry{}, false
	}
	_, command := table["command"]
	env := map[string]string{}
	if rawEnv, found := table["env"]; found {
		envTable, ok := rawEnv.(map[string]any)
		if !ok {
			return configMCPEntry{}, false
		}
		for key, value := range envTable {
			text, ok := value.(string)
			if !ok {
				return configMCPEntry{}, false
			}
			env[key] = text
		}
	}
	return configMCPEntry{command: command, env: env}, true
}

// fileServerEnvOverridePairs renders the per-key pairs for file servers the
// request streams do not already cover. Every server is still gated — an
// unaddressable name or an env member the patch removes but no `-c` pair can
// express fails the launch — including covered ones, whose file half the
// whole-table pair cannot prune. The removal check is derived from the
// shared merge itself: a file member the merge drops and the set half does
// not re-add under its exact name is a removal `-c` overrides cannot
// express, because every `-c` spelling merges deeply over the file table.
func fileServerEnvOverridePairs(servers []fileMCPServer, covered map[string]bool, patch envpatch.Patch) ([]string, error) {
	var out []string
	for _, server := range servers {
		if !isTOMLBareKey(server.name) {
			return nil, uncoverableServerError(server.name, "server name is not a plain config key")
		}
		merged := agentic.ApplyNetworkPatchToServerEnvs(patch, []agentic.MCPServerEnv{{Server: server.name, Env: server.env}})
		kept := make(map[string]bool, len(merged[0].Env))
		for _, pair := range merged[0].Env {
			kept[pair.Name] = true
		}
		var lost []string
		for _, member := range server.env {
			if !kept[member.Name] {
				lost = append(lost, member.Name)
			}
		}
		if len(lost) > 0 {
			sort.Strings(lost)
			return nil, uncoverableServerError(server.name, "its configured env keeps "+strings.Join(lost, ", ")+" that the managed patch removes")
		}
		if covered[server.name] {
			continue
		}
		for _, pair := range patch.Set {
			if !isTOMLBareKey(pair.Name) {
				return nil, fmt.Errorf("codex: patch set carries a name that is not an environment name: %w", agentic.ErrNetworkProfileInvalid)
			}
			value, err := encodeTOMLValue(pair.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, "-c", "mcp_servers."+server.name+".env."+pair.Name+"="+value)
		}
	}
	return out, nil
}

// refuseUnaddressableServerNames fails a managed launch whose injection would
// not reach its entry: only TOML-bare names route through a `-c` dotted
// key, and quoted segments are not supported, so an env pair for any other
// name lands on a bogus entry while the real one launches bare.
func refuseUnaddressableServerNames(names []string) error {
	for _, name := range names {
		if !isTOMLBareKey(name) {
			return uncoverableServerError(name, "server name is not a plain config key")
		}
	}
	return nil
}

// uncoverableServerError refuses a managed scope over a command-backed
// server the adapter cannot cover reliably. It names the server: the
// operator's fix is per-entry — rename it, or drop the stale member from
// its configured env — and a refusal without the name would send them
// hunting through every entry.
func uncoverableServerError(name, reason string) error {
	return fmt.Errorf("codex: codex-env-v1 cannot cover MCP server %q: %s: %w", name, reason, agentic.ErrNetworkScopeUnsupported)
}

// inventoryUnreadable refuses a managed scope whose file inventory cannot be
// established at all: the effective home resolves from neither CODEX_HOME
// nor HOME, and the harness fallback past both is ambient state no request
// carries. The code is network_file_unreadable — a read failure is never
// headroom — not scope_unsupported: nothing about the scope was judged,
// only the inventory read failed.
func inventoryUnreadable(subject, detail string) error {
	return fmt.Errorf("codex: %w", refusal.New(refusal.CodeFileUnreadable, subject, detail))
}

// inventoryFileUnreadable refuses a managed scope over one inventory file
// that cannot be read, parsed, or walked as a table. Profile names the
// selected profile for diagnostics, empty for the base config.
func inventoryFileUnreadable(subject, profile, detail string) error {
	if profile == "" {
		return fmt.Errorf("codex: %w", refusal.New(refusal.CodeFileUnreadable, subject, detail))
	}
	return fmt.Errorf("codex: cannot read the MCP inventory from profile %q: %w", profile, refusal.New(refusal.CodeFileUnreadable, subject, detail))
}

// inventoryEntryUnreadable refuses a managed scope over one entry whose
// shape is not a table with a string env block. It names the server the
// walk was reading: without the name a many-entry config sends the operator
// hunting.
func inventoryEntryUnreadable(server, subject, profile string) error {
	if profile == "" {
		return fmt.Errorf("codex: cannot read the MCP inventory entry for server %q: %w", server, refusal.New(refusal.CodeFileUnreadable, subject, "MCP entry has an unsupported shape"))
	}
	return fmt.Errorf("codex: cannot read the MCP inventory entry for server %q from profile %q: %w", server, profile, refusal.New(refusal.CodeFileUnreadable, subject, "MCP entry has an unsupported shape"))
}
