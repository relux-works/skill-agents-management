package codex

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file holds the network allowlist this system declares — exactly one
// verified tuple {adapter: codex-env-v1, harness: codex-cli, build: 0.159.0,
// entrypoint: exec} — and the codex-env-v1 argv half: the set half injected
// into each stdio MCP entry's env block. The generic process patch is
// BuildPlan's application point; what is asserted here through the production
// entry point is the admission (exact tuple, neighbours refused) and the
// injection (none, one, several, existing blocks, http skipped).

// verifiedNetworkCarrier is a well-formed codex-env-v1 scope bearing the
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

// managedEnvTable is the exact TOML table the egress-a set half renders as
// inside one injected `mcp_servers.<name>.env` pair: patch order, no spaces.
const managedEnvTable = `{HTTP_PROXY="http://127.0.0.1:18081",HTTPS_PROXY="http://127.0.0.1:18081",http_proxy="http://127.0.0.1:18081",https_proxy="http://127.0.0.1:18081",NO_PROXY="127.0.0.1,::1,localhost",no_proxy="127.0.0.1,::1,localhost"}`

// managedRequest is an exec launch carrying a network scope: the parity
// request plus a stub `codex` on PATH, a prompt file on disk, and an
// isolated HOME whose .codex holds no servers — so the only thing standing
// between the request and a plan is the admission gate, and the file half
// of the coverage contributes nothing unless the test writes it. A managed
// launch without any establishable home is refused (the harness would fall
// back to ambient user state no request carries), so PATH-only env cannot
// serve here.
func managedRequest(t *testing.T, network agentic.Network) agentic.LaunchRequest {
	t.Helper()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "managed prompt")
	req.Env = []string{"PATH=" + binDir, "HOME=" + tempSlot(t)}
	req.Network = network
	return req
}

func planErrorFor(t *testing.T, req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := agentic.BuildPlan(registry, req, mode)
	return err
}

// mcpEnvPairs collects the `-c` values in argv that assign an MCP env block.
func mcpEnvPairs(argv []string) []string {
	var pairs []string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] != "-c" {
			continue
		}
		if strings.HasPrefix(argv[i+1], "mcp_servers.") && strings.Contains(argv[i+1], ".env=") {
			pairs = append(pairs, argv[i+1])
		}
	}
	return pairs
}

// mcpEnvLeafPairs collects the `-c` values in argv that assign one MCP env
// leaf (`mcp_servers.<name>.env.<VAR>=...`), the per-key spelling the file
// half emits. Whole-table pairs never match: `.env={` contains no `.env.`
// segment.
func mcpEnvLeafPairs(argv []string) []string {
	var pairs []string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] != "-c" {
			continue
		}
		if strings.HasPrefix(argv[i+1], "mcp_servers.") && strings.Contains(argv[i+1], ".env.") {
			pairs = append(pairs, argv[i+1])
		}
	}
	return pairs
}

// managedLeafSuffixes is the per-key value half the egress-a set renders
// as: one `VAR="value"` suffix per set var, in patch order.
var managedLeafSuffixes = []string{
	`HTTP_PROXY="http://127.0.0.1:18081"`,
	`HTTPS_PROXY="http://127.0.0.1:18081"`,
	`http_proxy="http://127.0.0.1:18081"`,
	`https_proxy="http://127.0.0.1:18081"`,
	`NO_PROXY="127.0.0.1,::1,localhost"`,
	`no_proxy="127.0.0.1,::1,localhost"`,
}

// wantLeafPairs renders the exact per-key pairs one file server gains: one
// pair per set var, in patch order.
func wantLeafPairs(server string) []string {
	pairs := make([]string, 0, len(managedLeafSuffixes))
	for _, suffix := range managedLeafSuffixes {
		pairs = append(pairs, "mcp_servers."+server+".env."+suffix)
	}
	return pairs
}

// requestEnvValue reads one var back out of a request's env, for fixtures
// that must write into the home the request resolves. It reads the LAST
// `name=value` entry — the value the launcher delivers — so fixtures over
// duplicate envs write where the child will load.
func requestEnvValue(t *testing.T, req agentic.LaunchRequest, name string) string {
	t.Helper()
	for i := len(req.Env) - 1; i >= 0; i-- {
		if key, value, ok := strings.Cut(req.Env[i], "="); ok && key == name {
			return value
		}
	}
	t.Fatalf("request env carries no %s", name)
	return ""
}

// writeHomeConfig writes the config.toml HOME/.codex resolves to.
func writeHomeConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating .codex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing config.toml: %v", err)
	}
}

// writeCodexHomeConfig writes the config.toml a CODEX_HOME dir resolves to.
func writeCodexHomeConfig(t *testing.T, codexHome, body string) {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("creating CODEX_HOME: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing config.toml: %v", err)
	}
}

// writeProfileConfig writes one profile file into a config root.
func writeProfileConfig(t *testing.T, root, profile, body string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("creating the config root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, profile+".config.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the profile file: %v", err)
	}
}

func twoStdioComposition() agentic.Composition {
	return agentic.Composition{
		Prefix: []string{
			"-c", `mcp_servers.first.command="/bin/first"`,
			"-c", `mcp_servers.second.command="/bin/second"`,
			"-c", `mcp_servers.second.args=["--stdio"]`,
		},
		Servers: []agentic.CompositionServer{
			{Name: "first", Transport: "stdio"},
			{Name: "second", Transport: "stdio"},
		},
	}
}

func mixedComposition() agentic.Composition {
	http := httpComposition()
	stdio := stdioComposition()
	return agentic.Composition{
		Prefix:  append(append([]string{}, http.Prefix...), stdio.Prefix...),
		Servers: append(append([]agentic.CompositionServer{}, http.Servers...), stdio.Servers...),
	}
}

// TestNetworkAdaptersDeclareExactlyTheVerifiedTuple pins the declaration
// itself: one entry, the verified tuple, nothing else. A second entry — or a
// weakened member — admits a scope no verification covers.
func TestNetworkAdaptersDeclareExactlyTheVerifiedTuple(t *testing.T) {
	t.Parallel()
	want := []binding.AdapterIdentity{
		{Adapter: "codex-env-v1", Harness: "codex-cli", Build: "0.159.0", Entrypoint: "exec"},
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
// members, so a declaration weakened to admit any one of them — the next
// build, another entrypoint, the sibling generic adapter, the
// confusingly-close harness spelling — must fail that subtest by name.
func TestBuildPlanRefusesNeighbouringNetworkTuples(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*binding.AdapterIdentity){
		"another build":      func(identity *binding.AdapterIdentity) { identity.Build = "0.159.1" },
		"another entrypoint": func(identity *binding.AdapterIdentity) { identity.Entrypoint = "dry-run" },
		"another adapter":    func(identity *binding.AdapterIdentity) { identity.Adapter = "generic-env-v1" },
		"another harness":    func(identity *binding.AdapterIdentity) { identity.Harness = "codex" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := verifiedNetworkCarrier()
			mutate(&network.Record.AdapterIdentity)

			err := planErrorFor(t, managedRequest(t, network), agentic.LaunchModeExec)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
}

// TestBuildPlanInjectsTheSetHalfIntoEveryStdioEntry drives injection through
// the production entry point: no entries, one entry, several entries. Each
// stdio entry gains exactly one env pair carrying the set half; a mutant
// that skips an entry fails the row that names it.
func TestBuildPlanInjectsTheSetHalfIntoEveryStdioEntry(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		composition agentic.Composition
		wantPairs   []string
	}{
		"no entries": {
			composition: agentic.Composition{},
			wantPairs:   nil,
		},
		"one entry": {
			composition: stdioComposition(),
			wantPairs:   []string{"mcp_servers.local.env=" + managedEnvTable},
		},
		"several entries": {
			composition: twoStdioComposition(),
			wantPairs: []string{
				"mcp_servers.first.env=" + managedEnvTable,
				"mcp_servers.second.env=" + managedEnvTable,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := managedRequest(t, verifiedNetworkCarrier())
			req.Composition = tc.composition
			plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

			if got := mcpEnvPairs(plan.Argv); !reflect.DeepEqual(got, tc.wantPairs) {
				t.Fatalf("injected env pairs = %v, want %v", got, tc.wantPairs)
			}
		})
	}
}

// TestBuildPlanMergesThePatchWithAnExistingEnvBlock pins the merge through
// the production entry point: the preserved member survives, the overwritten
// name carries the managed value, the unset-only name is gone, and the rest
// of the set half is appended.
func TestBuildPlanMergesThePatchWithAnExistingEnvBlock(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.Composition = stdioCompositionWithEnv()
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	want := []string{`mcp_servers.local.env={KEEP="1",` + managedEnvTable[1:]}
	if got := mcpEnvPairs(plan.Argv); !reflect.DeepEqual(got, want) {
		t.Fatalf("injected env pairs = %v, want %v", got, want)
	}
	for _, entry := range plan.Argv {
		if strings.Contains(entry, "http://wrong:1") || strings.Contains(entry, "socks5://stale:1080") {
			t.Fatalf("a stale caller value survived injection: %q", entry)
		}
	}
}

// TestBuildPlanSkipsHttpEntries holds that injection visits only the entries
// that have an env block: an http entry gains no pair, and in a mixed launch
// the stdio sibling still gains exactly its own.
func TestBuildPlanSkipsHttpEntries(t *testing.T) {
	t.Parallel()
	httpOnly := managedRequest(t, verifiedNetworkCarrier())
	httpOnly.Composition = httpComposition()
	if got := mcpEnvPairs(buildParityPlan(t, New(), httpOnly, agentic.LaunchModeExec).Argv); len(got) != 0 {
		t.Fatalf("an http-only launch gained env pairs %v; an http entry has no child process to inject into", got)
	}

	mixed := managedRequest(t, verifiedNetworkCarrier())
	mixed.Composition = mixedComposition()
	want := []string{"mcp_servers.local.env=" + managedEnvTable}
	if got := mcpEnvPairs(buildParityPlan(t, New(), mixed, agentic.LaunchModeExec).Argv); !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed launch env pairs = %v, want %v", got, want)
	}
}

// TestBuildPlanInjectsContextDescriptorServers covers the second MCP channel:
// descriptor-derived servers are entries the harness will launch too, so the
// stdio one gains the set half and the http one gains nothing.
func TestBuildPlanInjectsContextDescriptorServers(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.ContextDescriptors = []agentic.ContextDescriptor{{
		Kind: agentic.ContextMCPServers,
		MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
			{Name: "ctxstdio", Transport: agentic.MCPTransportStdio, Command: "/bin/ctx", Args: []string{"--stdio"}},
			{Name: "ctxhttp", Transport: agentic.MCPTransportHTTP, URL: "https://ctx.invalid/mcp"},
		}},
	}}
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	want := []string{"mcp_servers.ctxstdio.env=" + managedEnvTable}
	if got := mcpEnvPairs(plan.Argv); !reflect.DeepEqual(got, want) {
		t.Fatalf("descriptor launch env pairs = %v, want %v", got, want)
	}
}

// TestManagedInteractiveLaunchesInjectDescriptorServers pins the uniform
// rule: the admission gate is mode-independent, so an admitted interactive
// launch injects into its descriptor servers rather than delivering the
// process patch without the entry halves.
func TestManagedInteractiveLaunchesInjectDescriptorServers(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.PromptPath = ""
	req.ServiceTier = ""
	req.Profile = ""
	req.ContextDescriptors = []agentic.ContextDescriptor{{
		Kind: agentic.ContextMCPServers,
		MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
			{Name: "ctxstdio", Transport: agentic.MCPTransportStdio, Command: "/bin/ctx"},
		}},
	}}
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeInteractive)

	want := []string{"mcp_servers.ctxstdio.env=" + managedEnvTable}
	if got := mcpEnvPairs(plan.Argv); !reflect.DeepEqual(got, want) {
		t.Fatalf("interactive launch env pairs = %v, want %v", got, want)
	}
}

// TestManagedDryRunMirrorsTheExecArgv holds the dry-run contract for a
// managed launch: the preview carries the same injected argv as the launch
// it mirrors, or it previews a different launch.
func TestManagedDryRunMirrorsTheExecArgv(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	homeDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	promptPath := writePromptFile(t, workDir, "managed prompt")
	// A file entry both modes must cover identically.
	writeHomeConfig(t, homeDir, "[mcp_servers.ambient]\ncommand = \"/bin/echo\"\n")

	managed := func() agentic.LaunchRequest {
		req := parityRequest(workDir)
		req.Env = []string{"PATH=" + binDir, "HOME=" + homeDir}
		req.Network = verifiedNetworkCarrier()
		req.Composition = stdioCompositionWithEnv()
		return req
	}
	execReq := managed()
	execReq.PromptPath = promptPath
	execArgv := buildParityPlan(t, New(), execReq, agentic.LaunchModeExec).Argv

	dryReq := managed()
	dryArgv := buildParityPlan(t, New(), dryReq, agentic.LaunchModeDryRun).Argv

	if !reflect.DeepEqual(dryArgv, execArgv) {
		t.Fatalf("dry-run argv = %v, want the exec argv %v", dryArgv, execArgv)
	}
}

// TestUnmanagedComposedPlansStayByteIdentical pins AC3 for the one surface
// this change touches: without a scope, a composed launch — including a
// caller env block — renders exactly the argv it always rendered, pair for
// pair. A mutant that injects without a scope fails here.
func TestUnmanagedComposedPlansStayByteIdentical(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := parityRequest(workDir)
	req.PromptPath = writePromptFile(t, workDir, "byte-identity prompt")
	req.Env = []string{"PATH=" + binDir}
	req.Composition = stdioCompositionWithEnv()

	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	boardDir := workDir + "/.task-board"
	want := []string{
		"-c", `mcp_servers.local.command="/usr/local/bin/mcp-local"`,
		"-c", `mcp_servers.local.args=["--stdio", "--quiet"]`,
		"-c", `mcp_servers.local.env={KEEP="1",HTTP_PROXY="http://wrong:1",ALL_PROXY="socks5://stale:1080"}`,
		"--search", "-a", "never",
		"-p", parityProfile,
		"exec", "-m", parityModel,
		"-c", `model_reasoning_effort="high"`,
		"-c", `service_tier="priority"`,
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		"-C", workDir,
		"--add-dir", boardDir,
		"-",
	}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("unmanaged argv = %v, want %v", plan.Argv, want)
	}
	if _, ok := plan.NetworkProvenanceSnapshot(); ok {
		t.Fatal("an unmanaged plan carries network provenance")
	}
}

// TestTheInjectedPrefixStillSatisfiesTheGrammar is the self-consistency
// half for the request streams: what the whole-table transform emits for a
// managed launch is itself a valid composition, so the emitted pairs never
// carry a shape the validator would refuse. The file half's per-key pairs
// are adapter-emitted, not composer-supplied — the composer grammar refuses
// them, and their own evidence is the exact-pair tests plus the harness
// probes that established the spelling — so they sit outside this slice.
func TestTheInjectedPrefixStillSatisfiesTheGrammar(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.Composition = twoStdioComposition()
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	// The MCP block leads the argv: the caller pairs in order, then one
	// injected env pair per stdio entry in Servers order.
	mcpLen := len(req.Composition.Prefix) + 2*len(req.Composition.Servers)
	emitted := agentic.Composition{Prefix: plan.Argv[:mcpLen], Servers: req.Composition.Servers}
	if err := New().ValidateComposition(emitted); err != nil {
		t.Fatalf("the injected prefix is not a valid composition: %v\nargv=%v", err, plan.Argv)
	}
}

// TestArgsRefusesAnUnadmittedNetworkScope holds the admission second line
// for a caller holding the plugin directly: Args and Argv refuse a scope
// no declared tuple names, with the typed refusal.
func TestArgsRefusesAnUnadmittedNetworkScope(t *testing.T) {
	t.Parallel()
	network := verifiedNetworkCarrier()
	network.Record.AdapterIdentity.Build = "0.159.1"
	req := parityRequest("/tmp/project")
	req.Network = network

	if _, err := Args(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("Args err = %v, want ErrNetworkScopeUnsupported", err)
	} else if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
		t.Fatalf("Args err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
	}
	if _, err := New().Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("System.Argv err = %v, want ErrNetworkScopeUnsupported", err)
	}
}

// TestArgsRefusesAMalformedPairStreamWhenManaged holds that a managed
// launch never silently half-injects: a prefix that is not a pair stream
// is refused. Unmanaged, the same bytes pass through as they always have —
// Args is not a validator, and the refusal exists only to protect the
// managed launch.
func TestArgsRefusesAMalformedPairStreamWhenManaged(t *testing.T) {
	t.Parallel()
	for name, prefix := range map[string][]string{
		"unpaired element": {"-c", `mcp_servers.local.command="/bin/x"`, "-c"},
		"non-pair flag":    {"--sandbox", `mcp_servers.local.command="/bin/x"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			managed := parityRequest("/tmp/project")
			managed.Composition = agentic.Composition{
				Prefix:  prefix,
				Servers: []agentic.CompositionServer{{Name: "local", Transport: "stdio"}},
			}
			managed.Network = verifiedNetworkCarrier()
			// An isolated empty home: without one the file inventory
			// refuses first and this case would pass for the wrong reason.
			managed.Env = []string{"HOME=" + tempSlot(t)}
			if _, err := Args(managed, agentic.LaunchModeExec); err == nil {
				t.Fatal("Args injected into a malformed pair stream without an error")
			} else if !strings.Contains(err.Error(), "pair stream") {
				t.Fatalf("Args refused the malformed prefix for the wrong reason: %v", err)
			}

			unmanaged := parityRequest("/tmp/project")
			unmanaged.Composition = agentic.Composition{Prefix: prefix}
			if _, err := Args(unmanaged, agentic.LaunchModeExec); err != nil {
				t.Fatalf("Args refused an unmanaged malformed prefix: %v; the refusal protects managed launches only", err)
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

// ---- round-2 coverage: every source, every spelling, fail-closed ----

// TestBuildPlanInjectsConfigFileServers is the E1 regression: a managed
// launch covers the stdio entries codex loads from its own files, not just
// the ones the request declares. Each file entry gains one per-key pair per
// set var; http entries gain nothing; a missing or empty config loads
// nothing. The panel's ambient reproducer is the one-entry row.
func TestBuildPlanInjectsConfigFileServers(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		config    string
		wantPairs []string
	}{
		"one entry without env": {
			config:    "[mcp_servers.ambient]\ncommand = \"/bin/echo\"\n",
			wantPairs: wantLeafPairs("ambient"),
		},
		"one entry with env": {
			config: "[mcp_servers.withenv]\ncommand = \"/bin/echo\"\n" +
				"[mcp_servers.withenv.env]\nKEEP = \"1\"\nHTTP_PROXY = \"http://wrong:1\"\n",
			wantPairs: wantLeafPairs("withenv"),
		},
		"several entries in name order": {
			config: "[mcp_servers.zeta]\ncommand = \"/bin/zeta\"\n" +
				"[mcp_servers.alpha]\ncommand = \"/bin/alpha\"\n",
			wantPairs: append(append([]string{}, wantLeafPairs("alpha")...), wantLeafPairs("zeta")...),
		},
		"http entry skipped": {
			config:    "[mcp_servers.rem]\nurl = \"https://mcp.invalid/mcp\"\n",
			wantPairs: nil,
		},
		"mixed entries": {
			config: "[mcp_servers.rem]\nurl = \"https://mcp.invalid/mcp\"\n" +
				"[mcp_servers.job]\ncommand = \"/bin/job\"\n",
			wantPairs: wantLeafPairs("job"),
		},
		"empty config loads nothing": {
			config:    "",
			wantPairs: nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := managedRequest(t, verifiedNetworkCarrier())
			writeHomeConfig(t, requestEnvValue(t, req, "HOME"), tc.config)
			plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

			if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, tc.wantPairs) {
				t.Fatalf("file env pairs = %v, want %v", got, tc.wantPairs)
			}
			if len(mcpEnvPairs(plan.Argv)) != 0 {
				t.Fatalf("a file-only launch gained whole-table pairs %v; file entries use the per-key spelling", mcpEnvPairs(plan.Argv))
			}
		})
	}
}

// TestBuildPlanWithoutAConfigFileLoadsNothing pins the absent-file half of
// the inventory: no config.toml means no file servers, not a refusal.
func TestBuildPlanWithoutAConfigFileLoadsNothing(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
		t.Fatalf("a launch with no config file gained file env pairs %v", got)
	}
}

// TestBuildPlanPrefersCodexHomeOverHome pins the resolution order the
// harness uses: a set CODEX_HOME wins over HOME, and the inventory comes
// from the winner alone.
func TestBuildPlanPrefersCodexHomeOverHome(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "[mcp_servers.fromhome]\ncommand = \"/bin/home\"\n")
	codexHome := tempSlot(t)
	writeCodexHomeConfig(t, codexHome, "[mcp_servers.fromcodexhome]\ncommand = \"/bin/pinned\"\n")
	req.Env = append(req.Env, "CODEX_HOME="+codexHome)
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	want := wantLeafPairs("fromcodexhome")
	if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, want) {
		t.Fatalf("file env pairs = %v, want %v (CODEX_HOME wins)", got, want)
	}
}

// TestBuildPlanInjectsSelectedProfileServers covers the profile channel: the
// one profile the launch selects layers its entries over the base, and an
// unselected profile file — even an unparseable one — is never read.
func TestBuildPlanInjectsSelectedProfileServers(t *testing.T) {
	t.Parallel()
	t.Run("profile entry layered over base", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = "p1"
		home := requestEnvValue(t, req, "HOME")
		writeHomeConfig(t, home, "[mcp_servers.base]\ncommand = \"/bin/base\"\n")
		writeProfileConfig(t, filepath.Join(home, ".codex"), "p1", "[mcp_servers.prof]\ncommand = \"/bin/prof\"\n")
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		want := append(append([]string{}, wantLeafPairs("base")...), wantLeafPairs("prof")...)
		if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, want) {
			t.Fatalf("file env pairs = %v, want %v", got, want)
		}
	})
	t.Run("missing profile file loads nothing", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = "absent"
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
			t.Fatalf("a launch with a missing profile file gained file env pairs %v", got)
		}
	})
	t.Run("unselected profile file never read", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		home := requestEnvValue(t, req, "HOME")
		writeHomeConfig(t, home, "[mcp_servers.base]\ncommand = \"/bin/base\"\n")
		writeProfileConfig(t, filepath.Join(home, ".codex"), "other", "[[[not toml")
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		want := wantLeafPairs("base")
		if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, want) {
			t.Fatalf("file env pairs = %v, want %v; the unselected profile must never be read", got, want)
		}
	})
}

// TestManagedFilePairsCoverCuratorProfile pins the Curator branch of the
// profile selection: a Curator MCP launch selects curator-mcp, and its file
// entries are covered. It calls the inventory pair builder directly —
// spelling a whole Curator fragment only to set one boolean would test the
// fragment, not the branch.
func TestManagedFilePairsCoverCuratorProfile(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.Profile = ""
	home := requestEnvValue(t, req, "HOME")
	writeHomeConfig(t, home, "")
	writeProfileConfig(t, filepath.Join(home, ".codex"), "curator-mcp", "[mcp_servers.cur]\ncommand = \"/bin/cur\"\n")

	pairs, err := managedFileServerEnvPairs(req, contextValues{hasCuratorMCP: true}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("managedFileServerEnvPairs: %v", err)
	}
	var values []string
	for i := 0; i+1 < len(pairs); i += 2 {
		values = append(values, pairs[i+1])
	}
	if want := wantLeafPairs("cur"); !reflect.DeepEqual(values, want) {
		t.Fatalf("curator profile pairs = %v, want %v", values, want)
	}
}

// TestBuildPlanCollidesCompositionWithFile pins the overlap: a server the
// request declares AND the files define is checked against the file half
// but emits only its whole-table pair — no second per-key writer — while a
// file-only sibling still gains its per-key pairs.
func TestBuildPlanCollidesCompositionWithFile(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.Composition = stdioComposition()
	home := requestEnvValue(t, req, "HOME")
	writeHomeConfig(t, home,
		"[mcp_servers.local]\ncommand = \"/bin/file-local\"\n"+
			"[mcp_servers.local.env]\nKEEP = \"file\"\n"+
			"[mcp_servers.lonely]\ncommand = \"/bin/lonely\"\n")
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	wantTable := []string{"mcp_servers.local.env=" + managedEnvTable}
	if got := mcpEnvPairs(plan.Argv); !reflect.DeepEqual(got, wantTable) {
		t.Fatalf("whole-table pairs = %v, want %v", got, wantTable)
	}
	wantLeaf := wantLeafPairs("lonely")
	if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, wantLeaf) {
		t.Fatalf("per-key pairs = %v, want %v; the collided entry must not gain a second writer", got, wantLeaf)
	}
}

// TestDirectInteractiveArgsCoverCollidedFileServers pins the mode-aware half
// of the overlap: interactive launches render no composition prefix, so a
// file entry colliding with a dropped composition name still gains its
// per-key pairs. (Through BuildPlan this composition is refused before Args;
// the direct caller meets Args first.)
func TestDirectInteractiveArgsCoverCollidedFileServers(t *testing.T) {
	t.Parallel()
	req := parityRequest("/tmp/project")
	req.Profile = ""
	req.ServiceTier = ""
	req.Env = []string{"HOME=" + tempSlot(t)}
	writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "[mcp_servers.local]\ncommand = \"/bin/file-local\"\n")
	req.Composition = stdioComposition()
	req.Network = verifiedNetworkCarrier()

	argv, err := Args(req, agentic.LaunchModeInteractive)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	if want := wantLeafPairs("local"); !reflect.DeepEqual(mcpEnvLeafPairs(argv), want) {
		t.Fatalf("per-key pairs = %v, want %v; the composition prefix is not rendered interactively", mcpEnvLeafPairs(argv), want)
	}
}

// TestBuildPlanCoversEveryTransportSpellingPerSource is the E2 regression
// table: validation and injection share one command-backed class, so every
// non-http spelling a composition admits is injected, http is skipped, and
// descriptors refuse every spelling but the exact two. The panel's
// transport reproducer is the three non-exact composition rows.
func TestBuildPlanCoversEveryTransportSpellingPerSource(t *testing.T) {
	t.Parallel()
	t.Run("composition", func(t *testing.T) {
		t.Parallel()
		for _, spelling := range []string{"", "stdio", "STDIO", "unknown"} {
			t.Run("transport_"+spelling, func(t *testing.T) {
				t.Parallel()
				req := managedRequest(t, verifiedNetworkCarrier())
				req.Composition = stdioCompositionWithEnv()
				req.Composition.Servers[0].Transport = spelling
				plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

				if got := mcpEnvPairs(plan.Argv); len(got) != 1 || !strings.Contains(got[0], "NO_PROXY=") {
					t.Fatalf("transport %q: env pairs = %v, want the managed block injected", spelling, got)
				}
			})
		}
		t.Run("transport_http skipped", func(t *testing.T) {
			t.Parallel()
			req := managedRequest(t, verifiedNetworkCarrier())
			req.Composition = httpComposition()
			plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

			if got := mcpEnvPairs(plan.Argv); len(got) != 0 {
				t.Fatalf("transport http: env pairs = %v, want none", got)
			}
		})
	})
	t.Run("composition shape mismatches refused", func(t *testing.T) {
		t.Parallel()
		httpWordWithCommand := stdioComposition()
		httpWordWithCommand.Servers[0].Transport = "http"
		nonHTTPWordWithURL := httpComposition()
		nonHTTPWordWithURL.Servers[0].Transport = "unknown"
		for name, composition := range map[string]agentic.Composition{
			"http word with command": httpWordWithCommand,
			"unknown word with url":  nonHTTPWordWithURL,
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				req := managedRequest(t, verifiedNetworkCarrier())
				req.Composition = composition
				if err := planErrorFor(t, req, agentic.LaunchModeExec); err == nil {
					t.Fatal("BuildPlan admitted a composition whose shape contradicts its transport word")
				}
			})
		}
	})
	t.Run("descriptor", func(t *testing.T) {
		t.Parallel()
		mcpRequest := func(transport string) agentic.LaunchRequest {
			req := managedRequest(t, verifiedNetworkCarrier())
			server := agentic.MCPServerDescriptor{Name: "ctx", Transport: transport}
			if transport == agentic.MCPTransportHTTP {
				server.URL = "https://ctx.invalid/mcp"
			} else {
				server.Command = "/bin/ctx"
			}
			req.ContextDescriptors = []agentic.ContextDescriptor{{
				Kind: agentic.ContextMCPServers,
				MCP:  &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{server}},
			}}
			return req
		}
		t.Run("transport_stdio injected", func(t *testing.T) {
			t.Parallel()
			plan := buildParityPlan(t, New(), mcpRequest(agentic.MCPTransportStdio), agentic.LaunchModeExec)
			if got := mcpEnvPairs(plan.Argv); len(got) != 1 {
				t.Fatalf("descriptor stdio: env pairs = %v, want one injected block", got)
			}
		})
		t.Run("transport_http skipped", func(t *testing.T) {
			t.Parallel()
			plan := buildParityPlan(t, New(), mcpRequest(agentic.MCPTransportHTTP), agentic.LaunchModeExec)
			if got := mcpEnvPairs(plan.Argv); len(got) != 0 {
				t.Fatalf("descriptor http: env pairs = %v, want none", got)
			}
		})
		for _, spelling := range []string{"", "STDIO", "unknown"} {
			t.Run("transport_"+spelling+" refused", func(t *testing.T) {
				t.Parallel()
				err := planErrorFor(t, mcpRequest(spelling), agentic.LaunchModeExec)
				if err == nil {
					t.Fatalf("BuildPlan admitted a descriptor with transport %q", spelling)
				}
				var invalid *agentic.InvalidContextDescriptorError
				if !errors.As(err, &invalid) {
					t.Fatalf("BuildPlan refused transport %q with %v, want an invalid-descriptor refusal", spelling, err)
				}
			})
		}
	})
	t.Run("config file", func(t *testing.T) {
		t.Parallel()
		fileRequest := func(body string) agentic.LaunchRequest {
			req := managedRequest(t, verifiedNetworkCarrier())
			writeHomeConfig(t, requestEnvValue(t, req, "HOME"), body)
			return req
		}
		t.Run("command entry injected", func(t *testing.T) {
			t.Parallel()
			plan := buildParityPlan(t, New(), fileRequest("[mcp_servers.s]\ncommand = \"/bin/s\"\n"), agentic.LaunchModeExec)
			if want := wantLeafPairs("s"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
				t.Fatalf("per-key pairs = %v, want %v", mcpEnvLeafPairs(plan.Argv), want)
			}
		})
		t.Run("url entry skipped", func(t *testing.T) {
			t.Parallel()
			plan := buildParityPlan(t, New(), fileRequest("[mcp_servers.s]\nurl = \"https://s.invalid/mcp\"\n"), agentic.LaunchModeExec)
			if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
				t.Fatalf("per-key pairs = %v, want none for a url entry", got)
			}
		})
		t.Run("neither entry admitted without pairs", func(t *testing.T) {
			t.Parallel()
			// Not command-backed, so vacuously covered: the plan is
			// admitted and the harness refuses the malformed entry
			// natively at launch.
			plan := buildParityPlan(t, New(), fileRequest("[mcp_servers.s]\n"), agentic.LaunchModeExec)
			if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
				t.Fatalf("per-key pairs = %v, want none for a command-less entry", got)
			}
		})
	})
}

// TestBuildPlanRefusesUncoverableServers pins the fail-closed half: a
// command-backed server the per-key spelling cannot express — an
// unaddressable name, or a configured member the patch removes — refuses
// the whole managed launch with typed network_scope_unsupported naming the
// server. A stale member the patch OVERWRITES is covered, not refused.
func TestBuildPlanRefusesUncoverableServers(t *testing.T) {
	t.Parallel()
	refused := map[string]struct {
		config string
		server string
	}{
		"dotted file name":            {config: "[mcp_servers.\"a.b\"]\ncommand = \"/bin/x\"\n", server: "a.b"},
		"empty file name":             {config: "[mcp_servers.\"\"]\ncommand = \"/bin/x\"\n", server: ""},
		"unset-only member":           {config: "[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nALL_PROXY = \"socks5://stale:1080\"\n", server: "s"},
		"mixed-case unset member":     {config: "[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nHttp_Proxy = \"http://odd:1\"\n", server: "s"},
		"lowercase unset-only member": {config: "[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nall_proxy = \"socks5://stale:1080\"\n", server: "s"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := managedRequest(t, verifiedNetworkCarrier())
			writeHomeConfig(t, requestEnvValue(t, req, "HOME"), tc.config)
			err := planErrorFor(t, req, agentic.LaunchModeExec)

			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
			if tc.server != "" && !strings.Contains(err.Error(), tc.server) {
				t.Fatalf("BuildPlan err = %v, want it to name server %q", err, tc.server)
			}
		})
	}
	t.Run("stale set member overwritten", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		writeHomeConfig(t, requestEnvValue(t, req, "HOME"),
			"[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nHTTP_PROXY = \"http://wrong:1\"\n")
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if want := wantLeafPairs("s"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v", mcpEnvLeafPairs(plan.Argv), want)
		}
		for _, entry := range plan.Argv {
			if strings.Contains(entry, "http://wrong:1") {
				t.Fatalf("a stale file value survived injection: %q", entry)
			}
		}
	})
	t.Run("dotted composition name refused when managed", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		// Spelled the way the grammar reads it: server from field on the
		// last dot, so this validates and reaches the injection gate.
		req.Composition = agentic.Composition{
			Prefix:  []string{"-c", `mcp_servers.a.b.command="/bin/x"`},
			Servers: []agentic.CompositionServer{{Name: "a.b", Transport: "stdio"}},
		}
		err := planErrorFor(t, req, agentic.LaunchModeExec)

		if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
			t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
		}
		if !strings.Contains(err.Error(), "a.b") {
			t.Fatalf("BuildPlan err = %v, want it to name server %q", err, "a.b")
		}
	})
	t.Run("collided unset-only member refused", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Composition = stdioComposition()
		writeHomeConfig(t, requestEnvValue(t, req, "HOME"),
			"[mcp_servers.local]\ncommand = \"/bin/file-local\"\n[mcp_servers.local.env]\nALL_PROXY = \"socks5://stale:1080\"\n")
		err := planErrorFor(t, req, agentic.LaunchModeExec)

		if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
			t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
		}
		if !strings.Contains(err.Error(), "local") {
			t.Fatalf("BuildPlan err = %v, want it to name server %q", err, "local")
		}
	})
	t.Run("profile layering keeps base unset member", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = "p1"
		home := requestEnvValue(t, req, "HOME")
		writeHomeConfig(t, home,
			"[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nALL_PROXY = \"socks5://stale:1080\"\n")
		writeProfileConfig(t, filepath.Join(home, ".codex"), "p1", "[mcp_servers.s.env]\nHTTP_PROXY = \"http://prof:2\"\n")
		err := planErrorFor(t, req, agentic.LaunchModeExec)

		if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
			t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported; the profile cannot prune the base leaf", err)
		}
	})
}

// TestBuildPlanRefusesUnreadableInventory pins the inventory half of
// fail-closed: anything that stops the adapter establishing WHAT codex will
// load — no home, an unreadable or unparseable file, an entry that is not a
// string table — refuses with typed network_file_unreadable. A read failure
// is never headroom for a launch.
func TestBuildPlanRefusesUnreadableInventory(t *testing.T) {
	t.Parallel()
	fileUnreadable := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("BuildPlan admitted a launch whose inventory cannot be established")
		}
		if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeFileUnreadable {
			t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeFileUnreadable)
		}
	}
	t.Run("no home at all", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = filterEnvKeys(req.Env, "HOME", "CODEX_HOME")
		fileUnreadable(t, planErrorFor(t, req, agentic.LaunchModeExec))
	})
	t.Run("empty homes count as unset", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = append(filterEnvKeys(req.Env, "HOME", "CODEX_HOME"), "HOME=", "CODEX_HOME=")
		fileUnreadable(t, planErrorFor(t, req, agentic.LaunchModeExec))
	})
	t.Run("config is a directory", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		home := requestEnvValue(t, req, "HOME")
		if err := os.MkdirAll(filepath.Join(home, ".codex", "config.toml"), 0o755); err != nil {
			t.Fatalf("laying a directory over config.toml: %v", err)
		}
		fileUnreadable(t, planErrorFor(t, req, agentic.LaunchModeExec))
	})
	t.Run("unparseable config", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "[[[not toml")
		fileUnreadable(t, planErrorFor(t, req, agentic.LaunchModeExec))
	})
	t.Run("mcp_servers not a table", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "mcp_servers = \"nope\"\n")
		fileUnreadable(t, planErrorFor(t, req, agentic.LaunchModeExec))
	})
	entryShapes := map[string]string{
		"entry not a table":       "[mcp_servers]\ns = \"nope\"\n",
		"env not a table":         "[mcp_servers.s]\ncommand = \"/bin/s\"\nenv = \"nope\"\n",
		"env member not a string": "[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nPORT = 8080\n",
	}
	for name, config := range entryShapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := managedRequest(t, verifiedNetworkCarrier())
			writeHomeConfig(t, requestEnvValue(t, req, "HOME"), config)
			err := planErrorFor(t, req, agentic.LaunchModeExec)
			fileUnreadable(t, err)
			if !strings.Contains(err.Error(), "s") {
				t.Fatalf("BuildPlan err = %v, want it to name server %q", err, "s")
			}
		})
	}
	t.Run("unparseable selected profile", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = "p1"
		home := requestEnvValue(t, req, "HOME")
		writeHomeConfig(t, home, "")
		writeProfileConfig(t, filepath.Join(home, ".codex"), "p1", "[[[not toml")
		err := planErrorFor(t, req, agentic.LaunchModeExec)
		fileUnreadable(t, err)
		if !strings.Contains(err.Error(), "p1") {
			t.Fatalf("BuildPlan err = %v, want it to name profile %q", err, "p1")
		}
	})
	t.Run("profile name escaping the root is never read", func(t *testing.T) {
		t.Parallel()
		// Codex refuses such a -p value at CLI parse, so the launch is
		// stillborn either way; the adapter must not turn it into a read
		// outside the request's home.
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = "../sib"
		writeProfileConfig(t, tempSlot(t), "sib", "[mcp_servers.evil]\ncommand = \"/bin/evil\"\n")
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
		if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
			t.Fatalf("per-key pairs = %v, want none; an escaping profile name is never read", got)
		}
	})
	t.Run("relative CODEX_HOME resolves against the workdir", func(t *testing.T) {
		t.Parallel()
		workDir := tempSlot(t)
		binDir := tempSlot(t)
		writeStubExecutable(t, binDir, executableName)
		writeCodexHomeConfig(t, filepath.Join(workDir, "cfg"), "[mcp_servers.rel]\ncommand = \"/bin/rel\"\n")
		req := parityRequest(workDir)
		req.PromptPath = writePromptFile(t, workDir, "managed prompt")
		req.Env = []string{"PATH=" + binDir, "CODEX_HOME=cfg"}
		req.Network = verifiedNetworkCarrier()
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
		if want := wantLeafPairs("rel"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v", mcpEnvLeafPairs(plan.Argv), want)
		}
	})
}

// TestManagedInteractiveRefusesNativeMCPChannel pins the raw channel: native
// arguments are forwarded verbatim, so a managed launch refuses native
// arguments that select or configure MCP entries. Unmanaged launches — and
// managed launches whose native arguments stay clear of MCP — pass through.
func TestManagedInteractiveRefusesNativeMCPChannel(t *testing.T) {
	t.Parallel()
	interactiveManaged := func() agentic.LaunchRequest {
		req := managedRequest(t, verifiedNetworkCarrier())
		req.PromptPath = ""
		req.ServiceTier = ""
		req.Profile = ""
		return req
	}
	refused := map[string][]string{
		"native -c mcp entry":     {"-c", `mcp_servers.evil.command="/bin/evil"`},
		"native --config mcp key": {"--config", `mcp_servers.evil.command="/bin/evil"`},
		"native -p selector":      {"-p", "work"},
		"native --profile":        {"--profile", "work"},
	}
	for name, native := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := interactiveManaged()
			req.NativeArgs = native
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)

			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeScopeUnsupported {
				t.Fatalf("BuildPlan err = %v, want typed %q", err, refusal.CodeScopeUnsupported)
			}
		})
	}
	t.Run("native mcp beside descriptors refused", func(t *testing.T) {
		t.Parallel()
		req := interactiveManaged()
		req.ContextDescriptors = []agentic.ContextDescriptor{{
			Kind: agentic.ContextMCPServers,
			MCP: &agentic.MCPServersContext{Servers: []agentic.MCPServerDescriptor{
				{Name: "ctx", Transport: agentic.MCPTransportStdio, Command: "/bin/ctx"},
			}},
		}}
		req.NativeArgs = []string{"-c", `mcp_servers.evil.command="/bin/evil"`}
		err := planErrorFor(t, req, agentic.LaunchModeInteractive)

		if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
			t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
		}
	})
	t.Run("native non-mcp passes through", func(t *testing.T) {
		t.Parallel()
		req := interactiveManaged()
		req.NativeArgs = []string{"-c", `model="gpt-5.6-terra"`}
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeInteractive)
		if len(plan.Argv) == 0 {
			t.Fatal("a managed interactive launch with non-MCP native arguments was not planned")
		}
	})
	t.Run("unmanaged native mcp passes through", func(t *testing.T) {
		t.Parallel()
		req := interactiveManaged()
		req.Network = agentic.Network{}
		req.NativeArgs = []string{"-c", `mcp_servers.evil.command="/bin/evil"`}
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeInteractive)
		found := false
		for _, entry := range plan.Argv {
			if entry == `mcp_servers.evil.command="/bin/evil"` {
				found = true
			}
		}
		if !found {
			t.Fatalf("unmanaged native arguments were not forwarded verbatim: %v", plan.Argv)
		}
	})
}

// TestManagedInventoryReadsTheChildEnv pins that the file half is read from
// the home the CHILD will load, not the home the request arrived with: the
// local-provider pin and a patch that redirects CODEX_HOME both move the
// inventory.
func TestManagedInventoryReadsTheChildEnv(t *testing.T) {
	t.Parallel()
	t.Run("patch redirect moves the inventory", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		home := requestEnvValue(t, req, "HOME")
		writeHomeConfig(t, home, "[mcp_servers.fromhome]\ncommand = \"/bin/home\"\n")
		target := tempSlot(t)
		writeCodexHomeConfig(t, target, "[mcp_servers.frompatch]\ncommand = \"/bin/patched\"\n")
		network := verifiedNetworkCarrier()
		network.Patch.Unset = append([]string{"CODEX_HOME"}, network.Patch.Unset...)
		network.Patch.Set = append([]envpatch.Pair{{Name: "CODEX_HOME", Value: target}}, network.Patch.Set...)
		req.Network = network
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		var got []string
		for _, pair := range mcpEnvLeafPairs(plan.Argv) {
			if strings.HasPrefix(pair, "mcp_servers.frompatch.env.HTTP_PROXY=") {
				got = append(got, pair)
			}
		}
		if len(got) != 1 {
			t.Fatalf("no per-key pairs for the patch-redirected home's server; pairs = %v", mcpEnvLeafPairs(plan.Argv))
		}
		for _, pair := range mcpEnvLeafPairs(plan.Argv) {
			if strings.HasPrefix(pair, "mcp_servers.fromhome.") {
				t.Fatalf("the inventory came from the request home, not the patch-redirected child home: %v", mcpEnvLeafPairs(plan.Argv))
			}
		}
	})
	t.Run("patch unset of CODEX_HOME falls back to HOME", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		home := requestEnvValue(t, req, "HOME")
		writeHomeConfig(t, home, "[mcp_servers.fromhome]\ncommand = \"/bin/home\"\n")
		codexHome := tempSlot(t)
		writeCodexHomeConfig(t, codexHome, "[mcp_servers.fromcodexhome]\ncommand = \"/bin/pinned\"\n")
		req.Env = append(req.Env, "CODEX_HOME="+codexHome)
		network := verifiedNetworkCarrier()
		network.Patch.Unset = append([]string{"CODEX_HOME"}, network.Patch.Unset...)
		req.Network = network
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		want := wantLeafPairs("fromhome")
		if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, want) {
			t.Fatalf("per-key pairs = %v, want %v; the patch removed CODEX_HOME so HOME rules", got, want)
		}
	})
	t.Run("local provider pin moves the inventory", func(t *testing.T) {
		t.Parallel()
		home, operatorHome, workDir := tempSlot(t), tempSlot(t), tempSlot(t)
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		config, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil {
			t.Fatalf("reading the provider config: %v", err)
		}
		writeProviderConfigBody(t, home, string(config)+"[mcp_servers.pinned]\ncommand = \"/bin/pinned\"\n")
		writeHomeConfig(t, operatorHome, "[mcp_servers.decoy]\ncommand = \"/bin/decoy\"\n")
		req := providerRequest(t, home, workDir, "local-story")
		req.Env = filterEnvKeys(req.Env, "CODEX_HOME")
		req.Env = agentic.SetEnvValue(req.Env, "HOME", operatorHome)
		req.Network = verifiedNetworkCarrier()
		plan, err := buildCodexPlan(t, req)
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}

		want := wantLeafPairs("pinned")
		if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, want) {
			t.Fatalf("per-key pairs = %v, want %v; the pin, not HOME, rules", got, want)
		}
	})
	t.Run("conflicting CODEX_HOME keeps provider precedence", func(t *testing.T) {
		t.Parallel()
		home, workDir := tempSlot(t), tempSlot(t)
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		req := providerRequest(t, home, workDir, "local-story")
		req.Env = append(req.Env, "CODEX_HOME="+tempSlot(t))
		req.Network = verifiedNetworkCarrier()
		_, err := buildCodexPlan(t, req)

		var providerRefusal *agentic.LocalProviderRefusal
		if !errors.As(err, &providerRefusal) {
			t.Fatalf("BuildPlan err = %v, want the local-provider conflict refusal first", err)
		}
	})
}

// TestDirectArgsRefuseMalformedPatchSetNames pins the defense in depth:
// BuildPlan's gate refuses a patch whose set names are not environment
// names before Args runs; a caller holding the plugin directly meets this
// refusal instead, because a per-key pair cannot spell such a name.
func TestDirectArgsRefuseMalformedPatchSetNames(t *testing.T) {
	t.Parallel()
	req := parityRequest("/tmp/project")
	req.Env = []string{"HOME=" + tempSlot(t)}
	writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "[mcp_servers.s]\ncommand = \"/bin/s\"\n")
	network := verifiedNetworkCarrier()
	network.Patch.Set = []envpatch.Pair{{Name: "A B", Value: "x"}}
	network.Patch.Unset = []string{"A B"}
	req.Network = network

	_, err := Args(req, agentic.LaunchModeExec)
	if !errors.Is(err, agentic.ErrNetworkProfileInvalid) {
		t.Fatalf("Args err = %v, want ErrNetworkProfileInvalid", err)
	}
	if code, ok := refusal.CodeOf(err); !ok || code != refusal.CodeProfileInvalid {
		t.Fatalf("Args err = %v, want typed %q", err, refusal.CodeProfileInvalid)
	}
}

// TestManagedFilePairsQuoteValues pins the per-key value spelling: values
// reuse the JSON-is-TOML quoting, so a value carrying quotes or a
// backslash round-trips through the TOML string parser.
func TestManagedFilePairsQuoteValues(t *testing.T) {
	t.Parallel()
	req := managedRequest(t, verifiedNetworkCarrier())
	writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "[mcp_servers.s]\ncommand = \"/bin/s\"\n")
	network := verifiedNetworkCarrier()
	network.Patch.Set = []envpatch.Pair{{Name: "HTTP_PROXY", Value: "http://x:1/\"q\"\\tail"}}
	network.Patch.Unset = []string{"HTTP_PROXY"}
	req.Network = network
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

	want := []string{`mcp_servers.s.env.HTTP_PROXY="http://x:1/\"q\"\\tail"`}
	if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, want) {
		t.Fatalf("per-key pairs = %v, want %v", got, want)
	}
	key, value, _ := strings.Cut(mcpEnvLeafPairs(plan.Argv)[0], "=")
	if key != "mcp_servers.s.env.HTTP_PROXY" {
		t.Fatalf("per-key key = %q", key)
	}
	if decoded, err := strconv.Unquote(value); err != nil || decoded != "http://x:1/\"q\"\\tail" {
		t.Fatalf("per-key value %q decodes to %q, %v", value, decoded, err)
	}
}

// TestBuildPlanCoversLiteralWhitespaceHomes is the H1 regression
// (repeat-of E1): the inventory reads exactly the home bytes the child
// will receive — no trimming, no normalization the child does not get.
// A trailing-space or leading-space CODEX_HOME (or HOME) loads its own
// literal directory, not the trimmed sibling; a whitespace-only CODEX_HOME
// is a literal relative path, not a fallback to HOME; and a
// whitespace-only HOME is a literal empty inventory, not a refusal.
func TestBuildPlanCoversLiteralWhitespaceHomes(t *testing.T) {
	t.Parallel()
	t.Run("trailing-space CODEX_HOME", func(t *testing.T) {
		t.Parallel()
		base := filepath.Join(t.TempDir(), "home")
		actual := base + " "
		writeCodexHomeConfig(t, base, "")
		writeCodexHomeConfig(t, actual, "[mcp_servers.escaped]\ncommand = \"/bin/echo\"\n")
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = append(req.Env, "CODEX_HOME="+actual)
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if want := wantLeafPairs("escaped"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v; the inventory must read the literal trailing-space home", mcpEnvLeafPairs(plan.Argv), want)
		}
	})
	t.Run("leading-space CODEX_HOME", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		base := filepath.Join(dir, "lead")
		actual := filepath.Join(dir, " lead")
		writeCodexHomeConfig(t, base, "")
		writeCodexHomeConfig(t, actual, "[mcp_servers.escaped]\ncommand = \"/bin/echo\"\n")
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = append(req.Env, "CODEX_HOME="+actual)
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if want := wantLeafPairs("escaped"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v; the inventory must read the literal leading-space home", mcpEnvLeafPairs(plan.Argv), want)
		}
	})
	t.Run("trailing-space HOME", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		trimmed := filepath.Join(dir, "hsp")
		actual := filepath.Join(dir, "hsp ")
		writeHomeConfig(t, trimmed, "")
		writeHomeConfig(t, actual, "[mcp_servers.escaped]\ncommand = \"/bin/echo\"\n")
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = agentic.SetEnvValue(req.Env, "HOME", actual)
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if want := wantLeafPairs("escaped"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v; the HOME-derived inventory must read the literal home", mcpEnvLeafPairs(plan.Argv), want)
		}
	})
	t.Run("relative CODEX_HOME with trailing space", func(t *testing.T) {
		t.Parallel()
		workDir := tempSlot(t)
		binDir := tempSlot(t)
		writeStubExecutable(t, binDir, executableName)
		writeCodexHomeConfig(t, filepath.Join(workDir, "cfg"), "")
		writeCodexHomeConfig(t, filepath.Join(workDir, "cfg "), "[mcp_servers.escaped]\ncommand = \"/bin/echo\"\n")
		req := parityRequest(workDir)
		req.PromptPath = writePromptFile(t, workDir, "managed prompt")
		req.Env = []string{"PATH=" + binDir, "HOME=" + tempSlot(t), "CODEX_HOME=cfg "}
		req.Network = verifiedNetworkCarrier()
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if want := wantLeafPairs("escaped"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v; a relative home resolves literally against the workdir", mcpEnvLeafPairs(plan.Argv), want)
		}
	})
	t.Run("relative CODEX_HOME with leading space", func(t *testing.T) {
		t.Parallel()
		workDir := tempSlot(t)
		binDir := tempSlot(t)
		writeStubExecutable(t, binDir, executableName)
		writeCodexHomeConfig(t, filepath.Join(workDir, "cfg"), "")
		writeCodexHomeConfig(t, filepath.Join(workDir, " cfg"), "[mcp_servers.escaped]\ncommand = \"/bin/echo\"\n")
		req := parityRequest(workDir)
		req.PromptPath = writePromptFile(t, workDir, "managed prompt")
		req.Env = []string{"PATH=" + binDir, "HOME=" + tempSlot(t), "CODEX_HOME= cfg"}
		req.Network = verifiedNetworkCarrier()
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if want := wantLeafPairs("escaped"); !reflect.DeepEqual(mcpEnvLeafPairs(plan.Argv), want) {
			t.Fatalf("per-key pairs = %v, want %v; a leading-space relative home resolves literally", mcpEnvLeafPairs(plan.Argv), want)
		}
	})
	t.Run("whitespace-only CODEX_HOME does not fall back to HOME", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		writeHomeConfig(t, requestEnvValue(t, req, "HOME"), "[mcp_servers.fromhome]\ncommand = \"/bin/home\"\n")
		req.Env = append(req.Env, "CODEX_HOME=   ")
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
			t.Fatalf("per-key pairs = %v, want none; a whitespace-only CODEX_HOME is a literal (missing) path, not a HOME fallback", got)
		}
	})
	t.Run("whitespace-only HOME admits empty", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = agentic.SetEnvValue(req.Env, "HOME", "   ")
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		if got := mcpEnvLeafPairs(plan.Argv); len(got) != 0 {
			t.Fatalf("per-key pairs = %v, want none; a whitespace-only HOME is a literal (missing) home", got)
		}
	})
}

// TestManagedNativeRefusalsNeverDiscloseValues is the H2 regression:
// every managed native refusal names the flag and config key only, never
// the value. Each native form carries a synthetic marker as its value;
// the refusal must name the flag (and key) and must not contain the
// marker.
func TestManagedNativeRefusalsNeverDiscloseValues(t *testing.T) {
	t.Parallel()
	const marker = "SYNTHETIC_ENDPOINT_MARKER"
	const mcpKey = "mcp_servers.evil.env.HTTP_PROXY"
	mcpValue := mcpKey + "=\"http://" + marker + ".invalid\""
	cases := map[string]struct {
		native []string
		flag   string
		key    string
	}{
		"attached -c":        {native: []string{"-c" + mcpValue}, flag: "-c", key: mcpKey},
		"separate -c":        {native: []string{"-c", mcpValue}, flag: "-c", key: mcpKey},
		"equals --config":    {native: []string{"--config=" + mcpValue}, flag: "--config", key: mcpKey},
		"separate --config":  {native: []string{"--config", mcpValue}, flag: "--config", key: mcpKey},
		"attached -p":        {native: []string{"-p" + marker}, flag: "-p"},
		"separate -p":        {native: []string{"-p", marker}, flag: "-p"},
		"equals --profile":   {native: []string{"--profile=" + marker}, flag: "--profile"},
		"separate --profile": {native: []string{"--profile", marker}, flag: "--profile"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := managedRequest(t, verifiedNetworkCarrier())
			req.PromptPath = ""
			req.ServiceTier = ""
			req.Profile = ""
			req.NativeArgs = tc.native
			err := planErrorFor(t, req, agentic.LaunchModeInteractive)

			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("BuildPlan err = %v, want ErrNetworkScopeUnsupported", err)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("refusal discloses the native value: %v", err)
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Fatalf("refusal %v does not name flag %q", err, tc.flag)
			}
			if tc.key != "" && !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("refusal %v does not name config key %q", err, tc.key)
			}
		})
	}
}

// TestUnmanagedFileStatesStayByteIdentical pins AC3 for the file half:
// without a scope, no home state — however hostile — changes the argv or
// refuses the launch, because unmanaged launches never read at all.
func TestUnmanagedFileStatesStayByteIdentical(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	base := parityRequest(workDir)
	base.PromptPath = writePromptFile(t, workDir, "byte-identity prompt")
	base.Env = []string{"PATH=" + binDir}
	base.Composition = stdioCompositionWithEnv()
	plain := buildParityPlan(t, New(), base, agentic.LaunchModeExec)

	nasty := tempSlot(t)
	writeHomeConfig(t, nasty,
		"[mcp_servers.\"a.b\"]\ncommand = \"/bin/x\"\n"+
			"[mcp_servers.s]\ncommand = \"/bin/s\"\n[mcp_servers.s.env]\nALL_PROXY = \"socks5://stale:1080\"\n")
	broken := tempSlot(t)
	writeHomeConfig(t, broken, "[[[not toml")
	for name, env := range map[string][]string{
		"hostile HOME":       {"PATH=" + binDir, "HOME=" + nasty},
		"hostile CODEX_HOME": {"PATH=" + binDir, "CODEX_HOME=" + nasty},
		"unparseable HOME":   {"PATH=" + binDir, "HOME=" + broken},
		"no home at all":     {"PATH=" + binDir},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := base
			req.Env = env
			plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

			if !reflect.DeepEqual(plan.Argv, plain.Argv) {
				t.Fatalf("unmanaged argv = %v, want the home-less argv %v", plan.Argv, plain.Argv)
			}
			if _, ok := plan.NetworkProvenanceSnapshot(); ok {
				t.Fatal("an unmanaged plan carries network provenance")
			}
		})
	}
}
