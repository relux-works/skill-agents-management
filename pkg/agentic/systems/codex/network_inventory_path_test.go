package codex

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the revision-4/5 regression for class
// mcp-config-source-escapes-injection (E1/H1/J1/K1): under an admitted
// managed scope, every command-backed server codex will actually load must
// receive the managed set/unset patch, or admission must refuse before
// launch.
//
// The fix it pins is a design change, not another spelling patch: the
// inventory builds the file path codex 0.159.0 opens by pure string
// concatenation — literal home bytes plus the leaf — and lets the kernel
// resolve symlinks and `..` the same way for both readers. There is no
// normalization step left to diverge from the loader. Revision 5 adds the
// duplicate-entry family (K1): the child's last-wins view is the only
// input, and the oracle models the launcher's lookup, not the request's
// first entry.
//
// Three tests hold the class closed:
//   - TestManagedInventoryCoversEveryPathSpelling: one table row per
//     spelling family, each asserting covered == kernel-visible through
//     agentic.BuildPlan, plus agreement with the installed 0.159.0 loader
//     (profile rows run the loader with the plan's own top-level `-p`).
//   - TestManagedInventoryCoverageProperty: a seeded generated test over
//     the same invariant, parametrized over home/profile/content/env/
//     workdir/dup families, with an exhaustive killing core.
//   - TestManagedInventoryPathBuilderUsesNoNormalization: a source guard
//     failing if any normalization primitive reaches the path builder.
//     (The duplicate-input agreement table and the single-reader guard live
//     in network_loader_inputs_test.go.)

// ---- kernel oracle: what the loader string resolves to ----

// loaderLiteralPath reproduces the string codex 0.159.0 opens: the literal
// home bytes plus the leaf, resolved against the child workdir when
// relative. It is deliberately NOT the production builder — it is the
// test's own three-line concatenation, and the property is that production
// agrees with the kernel at this string. Probed against the installed
// 0.159.0: literal CODEX_HOME plus config.toml relative to the process
// cwd, for trailing-slash, `//`, `./` and symlink/`..` homes alike, and
// `$CODEX_HOME/<name>.config.toml` for `-p <name>` through a symlink/`..`
// root (exec names the kernel-resolved profile file on a parse error).
func loaderLiteralPath(workDir, home, leaf string) string {
	base := home
	if !strings.HasPrefix(base, "/") {
		base = workDir + "/" + base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + leaf
}

// kernelInventory is what the kernel resolves at the loader strings: the
// command-backed names across the layered files, or malformed when a
// present file is unparseable or misshapen (which fails the launch closed,
// never an empty inventory).
type kernelInventory struct {
	servers   map[string]bool
	malformed bool
}

// kernelInventoryAt reads files in layer order (base, then profile) through
// the kernel. A missing file loads nothing, exactly as for the harness.
func kernelInventoryAt(t *testing.T, files ...string) kernelInventory {
	t.Helper()
	out := kernelInventory{servers: map[string]bool{}}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("reading fixture %s: %v", file, err)
		}
		var config map[string]any
		if err := toml.Unmarshal(data, &config); err != nil {
			out.malformed = true
			return out
		}
		section, found := config["mcp_servers"]
		if !found {
			continue
		}
		tables, ok := section.(map[string]any)
		if !ok {
			out.malformed = true
			return out
		}
		for name, raw := range tables {
			table, ok := raw.(map[string]any)
			if !ok {
				out.malformed = true
				return out
			}
			if _, hasCommand := table["command"]; hasCommand {
				out.servers[name] = true
			}
			if rawEnv, found := table["env"]; found {
				envTable, ok := rawEnv.(map[string]any)
				if !ok {
					out.malformed = true
					return out
				}
				for _, value := range envTable {
					if _, ok := value.(string); !ok {
						out.malformed = true
						return out
					}
				}
			}
		}
	}
	return out
}

// lastRequestEnv reads the LAST `name=value` entry, ignoring bare `name`
// entries — the lookup the exec'd child performs. Go's launcher dedups
// `key=value` pairs last-wins and passes bare entries through, and the
// child's getenv scans for the `name=` prefix (probed end to end: duplicate
// CODEX_HOME resolves to the last entry through os/exec, bare entries are
// ignored). An oracle reading first-wins would model a reader the child
// never was (round-4 K1).
func lastRequestEnv(env []string, name string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if key, value, found := strings.Cut(env[i], "="); found && key == name {
			return value, true
		}
	}
	return "", false
}

// effectiveRequestHome is the home bytes the request resolves: the LAST
// CODEX_HOME unless exactly empty-or-absent, else the LAST HOME plus
// `.codex` by the same literal concatenation the loader performs.
func effectiveRequestHome(t *testing.T, req agentic.LaunchRequest) string {
	t.Helper()
	home, ok := effectiveRequestHomeOK(req)
	if !ok {
		t.Fatalf("fixture carries no establishable home")
	}
	return home
}

// effectiveRequestHomeOK is effectiveRequestHome reporting establishability
// instead of failing: both keys absent-or-empty means no home, the case the
// planner refuses with network_file_unreadable.
func effectiveRequestHomeOK(req agentic.LaunchRequest) (string, bool) {
	if value, ok := lastRequestEnv(req.Env, "CODEX_HOME"); ok && value != "" {
		return value, true
	}
	home, ok := lastRequestEnv(req.Env, "HOME")
	if !ok || home == "" {
		return "", false
	}
	if strings.HasSuffix(home, "/") {
		return home + ".codex", true
	}
	return home + "/.codex", true
}

// ---- plan coverage assertions ----

// requireExactLeafCoverage requires the plan to cover exactly the wanted
// file servers: one per-key pair per set var per server, in deterministic
// order, and no whole-table pair (these fixtures carry no composition or
// descriptors, so any `.env={` pair is a writer the inventory never meant).
func requireExactLeafCoverage(t *testing.T, plan agentic.Plan, want []string) {
	t.Helper()
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	var expected []string
	for _, name := range sorted {
		expected = append(expected, wantLeafPairs(name)...)
	}
	if got := mcpEnvLeafPairs(plan.Argv); !reflect.DeepEqual(got, expected) {
		t.Fatalf("per-key pairs = %v, want %v", got, expected)
	}
	for i := 0; i+1 < len(plan.Argv); i++ {
		if plan.Argv[i] == "-c" && strings.Contains(plan.Argv[i+1], ".env={") {
			t.Fatalf("file-only launch emits a whole-table pair: %q", plan.Argv[i+1])
		}
	}
}

// requireChildEnvPremise pins the premise the inventory resolves under:
// the child env carries the request's home bytes back (last match — the
// value the launcher delivers), so resolving from the request env reads
// what the child will load. The egress-a patch touches proxy vars only, so
// any divergence here is a fixture that smuggles a redirect past the
// oracle.
func requireChildEnvPremise(t *testing.T, req agentic.LaunchRequest, plan agentic.Plan) {
	t.Helper()
	for _, name := range []string{"CODEX_HOME", "HOME"} {
		want, wantOK := lastRequestEnv(req.Env, name)
		got, gotOK := lastRequestEnv(plan.Env, name)
		if wantOK != gotOK || want != got {
			t.Fatalf("child %s = %q,%v, want request's %q,%v", name, got, gotOK, want, wantOK)
		}
	}
}

// requireInventoryReadsPlanEnv pins the single-reader design (round-4 K1):
// the env the inventory resolved from is byte-for-byte the env the launch
// delivers. A recomputation that disagreed on any entry would be two
// readers of one input again.
func requireInventoryReadsPlanEnv(t *testing.T, req agentic.LaunchRequest, plan agentic.Plan) {
	t.Helper()
	env, err := managedChildEnv(req)
	if err != nil {
		t.Fatalf("managedChildEnv refused a request the plan admitted: %v", err)
	}
	if !reflect.DeepEqual(env, plan.Env) {
		t.Fatalf("inventory env differs from plan env:\ninv=%v\nplan=%v", env, plan.Env)
	}
}

// ---- installed-loader cross-check ----

type loaderProbe struct {
	bin string // "" when no verified loader is available
	err string // skip reason when bin == ""
}

var probeCodexLoader = sync.OnceValue(func() loaderProbe {
	// An explicit override names the binary under test; otherwise PATH
	// decides. There is no machine-specific fallback: a hardcoded author
	// path has no place in a public repository, and a missing binary is a
	// recorded skip, not a failure.
	if override := strings.TrimSpace(os.Getenv("CODEX_TEST_BINARY")); override != "" {
		if st, err := os.Stat(override); err != nil || st.IsDir() {
			return loaderProbe{err: fmt.Sprintf("CODEX_TEST_BINARY=%q names no binary: %v", override, err)}
		}
		return pinLoaderVersion(override)
	}
	bin, err := exec.LookPath("codex")
	if err != nil {
		return loaderProbe{err: "no codex binary on PATH (set CODEX_TEST_BINARY to override)"}
	}
	return pinLoaderVersion(bin)
})

func pinLoaderVersion(bin string) loaderProbe {
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "0.159.0") {
		return loaderProbe{err: fmt.Sprintf("codex binary %s is not the verified 0.159.0 (%q)", bin, strings.TrimSpace(string(out)))}
	}
	return loaderProbe{bin: bin}
}

// planConfigPairs collects every `-c` value in argv, the override stream the
// loader cross-check replays.
func planConfigPairs(argv []string) []string {
	var pairs []string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-c" {
			pairs = append(pairs, argv[i+1])
		}
	}
	return pairs
}

// loaderServerEnv runs the installed 0.159.0 loader over one server with the
// plan's own pairs and returns the env the loader reports. Profile is the
// launch's selected profile (already trimmed): the loader takes it as a
// top-level `-p` ahead of the subcommand — `codex -p work mcp get`, the
// same position the plan's own `-p` occupies — and layers
// `$CODEX_HOME/<name>.config.toml` over the base config exactly as the
// managed child loads it.
func loaderServerEnv(t *testing.T, bin, workDir string, env, pairs []string, profile, server string) map[string]any {
	t.Helper()
	var args []string
	if strings.TrimSpace(profile) != "" {
		args = append(args, "-p", strings.TrimSpace(profile))
	}
	args = append(args, "mcp", "get", server, "--json")
	for _, pair := range pairs {
		args = append(args, "-c", pair)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = workDir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex mcp get %s failed: %v: %s", server, err, out)
	}
	// The loader may print a WARNING line ahead of the JSON (observed when
	// the home derives from HOME under a temp dir); the `--json` payload
	// itself starts at the first '{'.
	if index := strings.IndexByte(string(out), '{'); index >= 0 {
		out = out[index:]
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("codex mcp get %s returned unparseable JSON: %v: %s", server, err, out)
	}
	transport, _ := result["transport"].(map[string]any)
	reported, _ := transport["env"].(map[string]any)
	return reported
}

// loaderListNames runs the installed 0.159.0 `mcp list --json` under env and
// returns the loaded server names. Unlike loaderServerEnv it reports the
// loader's failure instead of failing: safe-direction rows assert the
// stillborn itself.
func loaderListNames(t *testing.T, bin, workDir string, env []string) ([]string, error) {
	t.Helper()
	cmd := exec.Command(bin, "mcp", "list", "--json")
	cmd.Dir = workDir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("codex mcp list: %v: %s", err, out)
	}
	if index := strings.IndexByte(string(out), '['); index >= 0 {
		out = out[index:]
	}
	var result []map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("codex mcp list returned unparseable JSON: %v: %s", err, out)
	}
	var names []string
	for _, entry := range result {
		if name, ok := entry["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names, nil
}

// requireLoaderAgreement runs the installed 0.159.0 loader with the plan's
// own env, workdir, selected profile and `-c` pairs and requires every
// wanted server to resolve with the full managed set at exact values. It
// returns false with a logged reason when no verified loader is available;
// the kernel-oracle assertions the caller already made still stand.
func requireLoaderAgreement(t *testing.T, workDir string, env, pairs []string, profile string, want []string) bool {
	t.Helper()
	probe := probeCodexLoader()
	if probe.bin == "" {
		t.Logf("LOADER-SKIP: %s; kernel-oracle assertions stand", probe.err)
		return false
	}
	set := verifiedNetworkCarrier().Patch.Set
	for _, server := range want {
		reported := loaderServerEnv(t, probe.bin, workDir, env, pairs, profile, server)
		for _, pair := range set {
			value, ok := reported[pair.Name].(string)
			if !ok || value != pair.Value {
				t.Fatalf("loader resolves %s env %q = %v, want %q", server, pair.Name, reported[pair.Name], pair.Value)
			}
		}
	}
	return true
}

// ---- table regression: one row per spelling family ----

const (
	pathFiler   = "filer"
	pathSibling = "sibling"
	pathLayered = "layered"
	pathProfile = "work"
)

func filerConfig() string   { return "[mcp_servers.filer]\ncommand = \"/bin/echo\"\n" }
func siblingConfig() string { return "[mcp_servers.sibling]\ncommand = \"/bin/false\"\n" }

// layoutLinkDotDot builds base/{real/cfg,lexical/cfg,real/child} with
// lexical/link pointing at real/child, and returns the kernel-resolved
// config dir first: opening base/lexical/link/../cfg lands in real/cfg,
// while a lexical clean lands in lexical/cfg.
func layoutLinkDotDot(t *testing.T, base string) (kernel, sibling string) {
	t.Helper()
	kernel = filepath.Join(base, "real", "cfg")
	sibling = filepath.Join(base, "lexical", "cfg")
	for _, dir := range []string{kernel, sibling, filepath.Join(base, "real", "child")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	if err := os.Symlink(filepath.Join(base, "real", "child"), filepath.Join(base, "lexical", "link")); err != nil {
		t.Fatalf("creating the link: %v", err)
	}
	return kernel, sibling
}

// requireInventoryMatchesKernelAndLoader drives one spelling through the
// production entry point and requires covered == kernel-visible == want,
// then agreement with the installed loader. The oracle comparison is the
// anti-circularity anchor: want is the test's own declaration of which
// bytes the literal string must reach, the kernel confirms the string
// reaches them, and the plan must cover exactly that set. A decoy server
// in the cleaned sibling makes a wrong-file read fail loudly instead of
// passing vacuously.
func requireInventoryMatchesKernelAndLoader(t *testing.T, req agentic.LaunchRequest, want []string) {
	t.Helper()
	plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
	home := effectiveRequestHome(t, req)
	files := []string{loaderLiteralPath(req.WorkDir, home, "config.toml")}
	profile := strings.TrimSpace(req.Profile)
	if profile != "" {
		files = append(files, loaderLiteralPath(req.WorkDir, home, profile+".config.toml"))
	}
	oracle := kernelInventoryAt(t, files...)
	if oracle.malformed {
		t.Fatalf("kernel oracle finds a malformed fixture at %v", files)
	}
	wantSet := map[string]bool{}
	for _, name := range want {
		wantSet[name] = true
	}
	if !reflect.DeepEqual(oracle.servers, wantSet) {
		t.Fatalf("kernel-visible servers = %v, want %v (files %v)", oracle.servers, wantSet, files)
	}
	requireExactLeafCoverage(t, plan, want)
	requireChildEnvPremise(t, req, plan)
	requireInventoryReadsPlanEnv(t, req, plan)
	// Profile rows run the loader with the same top-level `-p` the plan
	// carries, so the layered entry is loader-verified directly — no
	// synthetic replay. (`mcp get` takes no `-p` after the subcommand;
	// ahead of it, it layers exactly as the managed child loads.)
	requireLoaderAgreement(t, req.WorkDir, plan.Env, planConfigPairs(plan.Argv), profile, want)
}

func TestManagedInventoryCoversEveryPathSpelling(t *testing.T) {
	t.Parallel()
	t.Run("absolute clean", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		home := filepath.Join(base, "cfg")
		writeCodexHomeConfig(t, home, filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+home)
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("relative clean", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.WorkDir = base
		req.Env = append(req.Env, "CODEX_HOME=cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("trailing space", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		actual := filepath.Join(base, "cfg ")
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), siblingConfig())
		writeCodexHomeConfig(t, actual, filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+actual)
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("leading space", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		actual := filepath.Join(base, " cfg")
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), siblingConfig())
		writeCodexHomeConfig(t, actual, filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+actual)
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("symlink parent absolute", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		kernel, sibling := layoutLinkDotDot(t, base)
		writeCodexHomeConfig(t, sibling, siblingConfig())
		writeCodexHomeConfig(t, kernel, filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+base+"/lexical/link/../cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("symlink parent relative", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		kernel, sibling := layoutLinkDotDot(t, base)
		writeCodexHomeConfig(t, sibling, siblingConfig())
		writeCodexHomeConfig(t, kernel, filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.WorkDir = base
		req.Env = append(req.Env, "CODEX_HOME=lexical/link/../cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("dot segment", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+base+"/./cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("double slash", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+base+"//cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("trailing slash", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = append(req.Env, "CODEX_HOME="+filepath.Join(base, "cfg")+"/")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("HOME-derived default", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeHomeConfig(t, filepath.Join(base, "h"), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = agentic.SetEnvValue(req.Env, "HOME", filepath.Join(base, "h"))
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("HOME-derived trailing space", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeHomeConfig(t, filepath.Join(base, "h"), siblingConfig())
		writeHomeConfig(t, filepath.Join(base, "h "), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.Env = agentic.SetEnvValue(req.Env, "HOME", filepath.Join(base, "h "))
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("selected profile", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		home := filepath.Join(base, "cfg")
		writeCodexHomeConfig(t, home, filerConfig())
		writeProfileConfig(t, home, pathProfile, "[mcp_servers.layered]\ncommand = \"/bin/true\"\n")
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = pathProfile
		req.Env = append(req.Env, "CODEX_HOME="+home)
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler, pathLayered})
	})
	t.Run("selected profile missing", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		home := filepath.Join(base, "cfg")
		writeCodexHomeConfig(t, home, filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = pathProfile
		req.Env = append(req.Env, "CODEX_HOME="+home)
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
	t.Run("profile through symlink parent", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		kernel, sibling := layoutLinkDotDot(t, base)
		writeCodexHomeConfig(t, sibling, siblingConfig())
		writeCodexHomeConfig(t, kernel, filerConfig())
		writeProfileConfig(t, kernel, pathProfile, "[mcp_servers.layered]\ncommand = \"/bin/true\"\n")
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = pathProfile
		req.Env = append(req.Env, "CODEX_HOME="+base+"/lexical/link/../cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler, pathLayered})
	})
	t.Run("relative home under trailing-slash workdir", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeCodexHomeConfig(t, filepath.Join(base, "cfg"), filerConfig())
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Profile = ""
		req.WorkDir = base + "/"
		req.Env = append(req.Env, "CODEX_HOME=cfg")
		requireInventoryMatchesKernelAndLoader(t, req, []string{pathFiler})
	})
}

// ---- generated property: covered == loaded, or a typed refusal ----

// Families the property draws from. The invariant is the class invariant:
// every command-backed server loaded under managed admission receives the
// managed patch, or admission refuses before launch.
type inventoryHomeKind int

const (
	homeAbsClean inventoryHomeKind = iota
	homeAbsTrailingSlash
	homeAbsTrailingSpace
	homeAbsLeadingSpace
	homeAbsDotSeg
	homeAbsDoubleSlash
	homeAbsLinkDotDot
	homeRelClean
	homeRelLinkDotDot
	homeRelDotSlash
	homeDerived
	homeDerivedTrailingSpace
	numHomeKinds
)

func (k inventoryHomeKind) String() string {
	return []string{"abs-clean", "abs-trailslash", "abs-trailspace", "abs-leadspace", "abs-dotseg", "abs-dblslash", "abs-linkdotdot", "rel-clean", "rel-linkdotdot", "rel-dotslash", "home-derived", "home-trailspace"}[k]
}

func (k inventoryHomeKind) isRelative() bool {
	return k == homeRelClean || k == homeRelLinkDotDot || k == homeRelDotSlash
}

func (k inventoryHomeKind) isDerived() bool {
	return k == homeDerived || k == homeDerivedTrailingSpace
}

// inventoryDupKind is the duplicate-entry family over the home-selecting key
// (round-4 K1): the launcher delivers the LAST `key=value` entry and ignores
// bare entries, so every order and every empty/non-empty conflict must agree
// with the inventory. The decoy value names a sibling dir carrying a
// distinguishing server, so a read of the wrong entry covers the wrong
// server instead of passing vacuously.
type inventoryDupKind int

const (
	dupNone inventoryDupKind = iota
	dupSame
	dupEmptyFirst
	dupEmptyLast
	dupDecoyFirst
	dupDecoyLast
	dupBareFirst
	dupBareLast
	dupTriple
	numDupKinds
)

func (k inventoryDupKind) String() string {
	return []string{"dup-none", "dup-same", "dup-empty-first", "dup-empty-last", "dup-decoy-first", "dup-decoy-last", "dup-bare-first", "dup-bare-last", "dup-triple"}[k]
}

// readsDecoy reports whether the selecting key's last entry names the decoy.
func (k inventoryDupKind) readsDecoy() bool { return k == dupDecoyLast }

// readsFallback reports whether the selecting key's last entry is empty, so
// a CODEX_HOME home falls back to HOME/.codex (a derived home has nowhere
// to fall back to and refuses).
func (k inventoryDupKind) readsFallback() bool { return k == dupEmptyLast }

type inventorySpec struct {
	home    inventoryHomeKind
	profile int // 0 none, 1 present, 2 absent-but-selected
	padded  bool
	content int // 0 fileServer, 1 httpOnly, 2 empty, 3 absent, 4 malformedBase, 5 malformedProfile
	envv    int // 0 none, 1 unrelated, 2 staleProxy, 3 unremovable (content 0 only)
	workdir int // 0 clean, 1 trailingSlash, 2 relative
	dup     inventoryDupKind
}

func (s inventorySpec) describe() string {
	profile := []string{"prof-none", "prof-present", "prof-absent"}[s.profile]
	if s.profile != 0 && s.padded {
		profile += "-padded"
	}
	content := []string{"file", "http", "empty", "absent", "badbase", "badprof"}[s.content]
	envv := []string{"env-none", "env-unrelated", "env-stale", "env-unremovable"}[s.envv]
	workdir := []string{"wd-clean", "wd-trailslash", "wd-relative"}[s.workdir]
	return fmt.Sprintf("home-%s/%s/%s/%s/%s/%s", s.home, profile, content, envv, workdir, s.dup)
}

func drawInventorySpec(rng *rand.Rand) inventorySpec {
	spec := inventorySpec{
		home:    inventoryHomeKind(rng.Intn(int(numHomeKinds))),
		profile: rng.Intn(3),
		content: rng.Intn(6),
		envv:    rng.Intn(4),
		workdir: rng.Intn(3),
		dup:     inventoryDupKind(rng.Intn(int(numDupKinds))),
	}
	return normalizeInventorySpec(spec, rng)
}

func normalizeInventorySpec(spec inventorySpec, rng *rand.Rand) inventorySpec {
	// A padded name only varies the profile lookup.
	spec.padded = spec.profile != 0 && rng.Intn(2) == 0
	if spec.content == 5 && spec.profile != 1 {
		spec.content = 0
	}
	if spec.content != 0 {
		spec.envv = 0
	}
	return spec
}

func exhaustiveInventorySpecs() []inventorySpec {
	var specs []inventorySpec
	homes := []inventoryHomeKind{homeAbsLinkDotDot, homeRelLinkDotDot, homeAbsTrailingSpace}
	for _, home := range homes {
		for _, profile := range []int{0, 1} {
			for _, content := range []int{0, 4} {
				specs = append(specs, inventorySpec{home: home, profile: profile, content: content, envv: 2})
			}
			specs = append(specs, inventorySpec{home: home, profile: profile, content: 0, envv: 3})
		}
	}
	specs = append(specs,
		inventorySpec{home: homeRelLinkDotDot, profile: 0, content: 0, workdir: 1},
		inventorySpec{home: homeRelLinkDotDot, profile: 1, content: 0, workdir: 1},
		inventorySpec{home: homeRelClean, profile: 0, content: 0, workdir: 2},
		inventorySpec{home: homeAbsClean, profile: 0, content: 0, workdir: 2},
	)
	// The duplicate killing core (round-4 K1): every dup spelling over a
	// spelling-sensitive home, a relative home and a derived home, with
	// and without a selected profile. Both empty/non-empty orders and both
	// decoy orders are present deterministically — a first-wins lookup or
	// a skipped dedupe fails these before any random case runs.
	dupHomes := []inventoryHomeKind{homeAbsLinkDotDot, homeRelLinkDotDot, homeDerived}
	dups := []inventoryDupKind{dupSame, dupEmptyFirst, dupEmptyLast, dupDecoyFirst, dupDecoyLast, dupBareFirst, dupBareLast, dupTriple}
	for _, home := range dupHomes {
		for _, dup := range dups {
			for _, profile := range []int{0, 1} {
				specs = append(specs, inventorySpec{home: home, profile: profile, content: 0, envv: 2, dup: dup})
			}
		}
	}
	return specs
}

// specBaseBody renders the base config body for a spec: the filer with the
// drawn env variant, an http-only entry, empty, or garbage.
func specBaseBody(spec inventorySpec) (body string, absent bool) {
	switch spec.content {
	case 1:
		return "[mcp_servers.web]\nurl = \"https://mcp.invalid/rpc\"\n", false
	case 2:
		return "", false
	case 3:
		return "", true
	case 4:
		return "NOT VALID TOML [[[\n", false
	default:
		return specFilerBody(spec.envv), false
	}
}

func specFilerBody(envv int) string {
	switch envv {
	case 1:
		return "[mcp_servers.filer]\ncommand = \"/bin/echo\"\n[mcp_servers.filer.env]\nFOO = \"bar\"\n"
	case 2:
		return "[mcp_servers.filer]\ncommand = \"/bin/echo\"\n[mcp_servers.filer.env]\nHTTP_PROXY = \"http://stale.invalid\"\n"
	case 3:
		return "[mcp_servers.filer]\ncommand = \"/bin/echo\"\n[mcp_servers.filer.env]\nALL_PROXY = \"socks5://stale.invalid\"\n"
	default:
		return filerConfig()
	}
}

// runInventorySpec builds one generated case, drives it through the
// production entry point, and requires the class invariant: covered ==
// kernel-visible, or the typed refusal the poison demands.
func runInventorySpec(t *testing.T, spec inventorySpec) {
	t.Helper()
	base := t.TempDir()
	req := managedRequest(t, verifiedNetworkCarrier())
	req.Profile = ""

	// The workdir family. A relative workdir is only establishable when
	// nothing needs it; the expectation switch below refuses the cases
	// that do.
	workBase := t.TempDir()
	switch spec.workdir {
	case 1:
		req.WorkDir = workBase + "/"
	case 2:
		req.WorkDir = "relative-workdir"
	default:
		req.WorkDir = workBase
	}

	// The home family: the primary value X plus a decoy value D. xDir is the
	// config dir X resolves to through the kernel; decoyDir the dir D
	// resolves to. For derived homes the values are HOME dirs and the dirs
	// their `.codex` children.
	var xValue, decoyValue, xDir, decoyDir string
	switch spec.home {
	case homeAbsClean, homeAbsTrailingSlash, homeAbsDotSeg, homeAbsDoubleSlash:
		xDir = filepath.Join(base, "cfg")
		decoyDir = filepath.Join(base, "decoy")
		xValue = xDir
		switch spec.home {
		case homeAbsTrailingSlash:
			xValue += "/"
		case homeAbsDotSeg:
			xValue = base + "/./cfg"
		case homeAbsDoubleSlash:
			xValue = base + "//cfg"
		}
		decoyValue = decoyDir
	case homeAbsTrailingSpace, homeAbsLeadingSpace:
		sibling := filepath.Join(base, "cfg")
		xDir = sibling
		if spec.home == homeAbsTrailingSpace {
			xDir = filepath.Join(base, "cfg ")
		} else {
			xDir = filepath.Join(base, " cfg")
		}
		xValue = xDir
		// The trimmed sibling doubles as the dup decoy.
		decoyDir, decoyValue = sibling, sibling
	case homeAbsLinkDotDot:
		kernel, sibling := layoutLinkDotDot(t, base)
		xDir = kernel
		xValue = base + "/lexical/link/../cfg"
		decoyDir, decoyValue = sibling, sibling
	case homeRelClean, homeRelDotSlash:
		req.WorkDir = base
		if spec.workdir == 1 {
			req.WorkDir = base + "/"
		} else if spec.workdir == 2 {
			req.WorkDir = "relative-workdir"
		}
		xDir = filepath.Join(base, "cfg")
		decoyDir = filepath.Join(base, "decoy")
		xValue = "cfg"
		if spec.home == homeRelDotSlash {
			xValue = "./cfg"
		}
		decoyValue = "decoy"
	case homeRelLinkDotDot:
		req.WorkDir = base
		if spec.workdir == 1 {
			req.WorkDir = base + "/"
		} else if spec.workdir == 2 {
			req.WorkDir = "relative-workdir"
		}
		kernel, _ := layoutLinkDotDot(t, base)
		xDir = kernel
		xValue = "lexical/link/../cfg"
		decoyDir = filepath.Join(base, "lexical", "cfg")
		decoyValue = "lexical/cfg"
	case homeDerived, homeDerivedTrailingSpace:
		xHome := filepath.Join(base, "h")
		decoyHome := filepath.Join(base, "decoyhome")
		if spec.home == homeDerivedTrailingSpace {
			decoyHome = xHome
			xHome = filepath.Join(base, "h ")
		}
		xValue, decoyValue = xHome, decoyHome
		xDir = filepath.Join(xHome, ".codex")
		decoyDir = filepath.Join(decoyHome, ".codex")
	}

	// The dup spelling assigns the read dir: the spec content goes where
	// the loader reads, the decoy sibling where it must not, so a read of
	// the wrong entry covers the wrong server instead of passing
	// vacuously. An empty last CODEX_HOME falls back to HOME/.codex; an
	// empty last HOME on a derived home refuses (nowhere to fall back to).
	readDir, unreadDir := xDir, decoyDir
	if spec.dup.readsDecoy() {
		readDir, unreadDir = decoyDir, xDir
	}
	if spec.dup.readsFallback() && !spec.home.isDerived() {
		home, _ := lastRequestEnv(req.Env, "HOME")
		readDir, unreadDir = filepath.Join(home, ".codex"), xDir
	}
	homeRefused := spec.dup.readsFallback() && spec.home.isDerived()

	body, absent := specBaseBody(spec)
	if homeRefused {
		// Nothing is read; X still carries the spec body so a reader that
		// guessed past the empty HOME would cover it.
		if !absent {
			writeCodexHomeConfig(t, xDir, body)
		}
		writeCodexHomeConfig(t, decoyDir, siblingConfig())
	} else {
		if !absent {
			writeCodexHomeConfig(t, readDir, body)
		} else if err := os.MkdirAll(readDir, 0o755); err != nil {
			t.Fatalf("creating the kernel dir: %v", err)
		}
		writeCodexHomeConfig(t, unreadDir, siblingConfig())
	}

	profile := ""
	if spec.profile != 0 {
		profile = pathProfile
		if spec.padded {
			profile = "  " + pathProfile + "  "
		}
		req.Profile = profile
		if spec.profile == 1 {
			profileBody := "[mcp_servers.layered]\ncommand = \"/bin/true\"\n"
			if spec.content == 5 {
				profileBody = "NOT VALID TOML [[[\n"
			}
			target := readDir
			if homeRefused {
				target = xDir
			}
			writeProfileConfig(t, target, pathProfile, profileBody)
		}
	}

	// The dup entries over the selecting key. The last `key=value` entry
	// is what the launcher delivers; a bare key is passed through and
	// ignored by the child's getenv.
	if spec.home.isDerived() {
		req.Env = filterEnvKeys(req.Env, "HOME")
		req.Env = append(req.Env, dupEntries("HOME", xValue, decoyValue, spec.dup)...)
	} else {
		req.Env = append(req.Env, dupEntries("CODEX_HOME", xValue, decoyValue, spec.dup)...)
	}

	// The expectation: a refusal code, or the exact cover set. Refusal is
	// derived from what the loader opens, not from the spec draw: a poison
	// in an unread dir refuses nothing.
	_, establishable := effectiveRequestHomeOK(req)
	// A relative workdir refuses only when the SELECTED home is relative:
	// an empty last CODEX_HOME falls back to the absolute HOME/.codex and
	// never touches the workdir.
	needsWorkdir := spec.home.isRelative() && !(spec.dup.readsFallback() && !spec.home.isDerived())
	// Production order: the home resolves (establishable, workdir) before
	// any file is read, and files read before the merge gate runs — so a
	// relative home under a relative workdir refuses file_unreadable even
	// when the unread content would refuse scope_unsupported.
	refuseCode := ""
	switch {
	case !establishable:
		refuseCode = string(refusal.CodeFileUnreadable)
	case needsWorkdir && spec.workdir == 2:
		refuseCode = string(refusal.CodeFileUnreadable)
	case spec.content == 4 || spec.content == 5:
		refuseCode = string(refusal.CodeFileUnreadable)
	case spec.content == 0 && spec.envv == 3:
		refuseCode = string(refusal.CodeScopeUnsupported)
	}
	if spec.content == 5 && spec.profile != 1 {
		t.Fatalf("spec draws a malformed profile without a present profile: %s", spec.describe())
	}

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
	if refuseCode != "" {
		if err == nil {
			t.Fatalf("BuildPlan admitted a %s case; want a typed %s refusal", spec.describe(), refuseCode)
		}
		code, ok := refusal.CodeOf(err)
		if !ok || string(code) != refuseCode {
			t.Fatalf("BuildPlan err = %v, want typed %s", err, refuseCode)
		}
		// The poison must sit in the file the loader opens, not merely
		// somewhere nearby: a refusal for the wrong file is a refusal
		// that still launches the poisoned one bare. A relative home
		// under a relative workdir, and an unestablishable home, refuse
		// before any read, so there is no loader file to check the
		// poison in — the refusal code above is the whole assertion
		// there.
		if (spec.content == 4 || spec.content == 5) && establishable && !(needsWorkdir && spec.workdir == 2) {
			home := effectiveRequestHome(t, req)
			files := []string{loaderLiteralPath(req.WorkDir, home, "config.toml")}
			if spec.profile != 0 {
				files = append(files, loaderLiteralPath(req.WorkDir, home, strings.TrimSpace(profile)+".config.toml"))
			}
			if oracle := kernelInventoryAt(t, files...); !oracle.malformed {
				t.Fatalf("refused %s but the kernel oracle parses the loader files %v", spec.describe(), files)
			}
		}
		return
	}
	if err != nil {
		t.Fatalf("BuildPlan refused a %s case: %v", spec.describe(), err)
	}

	want := []string{}
	if spec.content == 0 {
		want = append(want, pathFiler)
	}
	if spec.profile == 1 {
		want = append(want, pathLayered)
	}
	home := effectiveRequestHome(t, req)
	files := []string{loaderLiteralPath(req.WorkDir, home, "config.toml")}
	if spec.profile != 0 {
		files = append(files, loaderLiteralPath(req.WorkDir, home, strings.TrimSpace(profile)+".config.toml"))
	}
	oracle := kernelInventoryAt(t, files...)
	if oracle.malformed {
		t.Fatalf("kernel oracle finds a malformed fixture at %v (%s)", files, spec.describe())
	}
	wantSet := map[string]bool{}
	for _, name := range want {
		wantSet[name] = true
	}
	if !reflect.DeepEqual(oracle.servers, wantSet) {
		t.Fatalf("kernel-visible servers = %v, want %v (%s; files %v)", oracle.servers, wantSet, spec.describe(), files)
	}
	requireExactLeafCoverage(t, plan, want)
	requireChildEnvPremise(t, req, plan)
	requireInventoryReadsPlanEnv(t, req, plan)
}

// dupEntries renders the selecting key's entries for a dup spelling: X is
// the primary value, D the decoy. The last `key=value` entry is what the
// launcher delivers; a bare key is passed through and ignored.
func dupEntries(key, x, d string, dup inventoryDupKind) []string {
	kv := func(v string) string { return key + "=" + v }
	switch dup {
	case dupSame:
		return []string{kv(x), kv(x)}
	case dupEmptyFirst:
		return []string{kv(""), kv(x)}
	case dupEmptyLast:
		return []string{kv(x), kv("")}
	case dupDecoyFirst:
		return []string{kv(d), kv(x)}
	case dupDecoyLast:
		return []string{kv(x), kv(d)}
	case dupBareFirst:
		return []string{key, kv(x)}
	case dupBareLast:
		return []string{kv(x), key}
	case dupTriple:
		return []string{kv(x), kv(d), kv(x)}
	default:
		return []string{kv(x)}
	}
}

func TestManagedInventoryCoverageProperty(t *testing.T) {
	t.Parallel()
	const seed int64 = 261002
	const randomCases = 120
	rng := rand.New(rand.NewSource(seed))
	exhaustive := exhaustiveInventorySpecs()
	t.Logf("inventory coverage property: seed=%d random=%d exhaustive=%d", seed, randomCases, len(exhaustive))
	for i := 0; i < randomCases; i++ {
		spec := drawInventorySpec(rng)
		t.Run(fmt.Sprintf("case-%03d-%s", i, spec.describe()), func(t *testing.T) {
			t.Parallel()
			runInventorySpec(t, spec)
		})
	}
	for _, spec := range exhaustive {
		t.Run("must-"+spec.describe(), func(t *testing.T) {
			t.Parallel()
			runInventorySpec(t, spec)
		})
	}
}

// ---- source guard: no normalization reaches the path builder ----

// TestManagedInventoryPathBuilderUsesNoNormalization fails if any
// normalization primitive can reach the managed inventory path: the whole
// file must not reference path/filepath, the normalizing shared helpers,
// or cleaning calls at all, and the builder functions must additionally be
// free of trimming, joining, cleaning, absolutizing, symlink-resolving and
// expanding identifiers. Each guarded function must be found exactly once,
// so a rename silently dropping it from the guard fails instead of passing
// vacuously.
func TestManagedInventoryPathBuilderUsesNoNormalization(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("network.go")
	if err != nil {
		t.Fatalf("reading network.go: %v", err)
	}
	code := stripLineComments(string(raw))
	for _, token := range []string{"path/filepath", "filepath.", "codexProfilePath", "resolveCodexConfigRoot"} {
		if strings.Contains(code, token) {
			t.Fatalf("network.go code references %q; the inventory path builder must not", token)
		}
	}
	trimTokens := []string{"TrimSpace", "Trim(", "TrimLeft", "TrimRight", "TrimPrefix", "TrimSuffix"}
	pathTokens := []string{"Clean", "Join", "Abs(", "EvalSymlinks", "Expand", "ToSlash", "FromSlash", "Base(", "Dir(", "Split(", "Glob("}
	// The path-string builders take no trimming at all: the home bytes
	// reach the kernel exactly as the child receives them.
	for _, name := range []string{"joinInventoryPath", "isAbsInventoryPath", "resolveManagedPath", "resolveManagedConfigRoot", "managedInventoryProfilePath"} {
		body := extractFuncBody(t, code, name)
		for _, token := range append(append([]string{}, trimTokens...), pathTokens...) {
			if strings.Contains(body, token) {
				t.Fatalf("%s body reaches normalization primitive %q", name, token)
			}
		}
	}
	// The inventory reader additionally selects the profile NAME with the
	// same trim Args applies before exec — argv matching, not path
	// normalization — so it is pinned against path primitives only.
	inventoryBody := extractFuncBody(t, code, "readCodexMCPInventory")
	for _, token := range pathTokens {
		if strings.Contains(inventoryBody, token) {
			t.Fatalf("readCodexMCPInventory body reaches normalization primitive %q", token)
		}
	}
	// The inventory reads through the one builder: the call graph must
	// show it, or the guard above pins functions nobody calls.
	inventory := extractFuncBody(t, code, "readCodexMCPInventory")
	for _, token := range []string{"joinInventoryPath", "managedInventoryProfilePath"} {
		if !strings.Contains(inventory, token) {
			t.Fatalf("readCodexMCPInventory no longer routes through %s", token)
		}
	}
}

func stripLineComments(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func extractFuncBody(t *testing.T, code, name string) string {
	t.Helper()
	marker := "func " + name + "("
	start := strings.Index(code, marker)
	if start < 0 {
		t.Fatalf("guard cannot find %s; a rename must update the guard", name)
	}
	if strings.Index(code[start+len(marker):], marker) >= 0 {
		t.Fatalf("guard finds %s twice; the extraction is ambiguous", name)
	}
	rest := code[start:]
	next := strings.Index(rest[len(marker):], "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:len(marker)+next]
}
