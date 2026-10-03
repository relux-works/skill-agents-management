package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/refusal"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This file is the revision-5 regression for class
// mcp-config-source-escapes-injection (E1/H1/J1/K1): under an admitted managed
// scope, every command-backed server codex will actually load must receive
// the managed set/unset patch, or admission must refuse before launch.
//
// The fix it pins is a design change, not a fifth point fix: the child's
// view is the only input. Under a managed scope the child env is deduped
// last-wins before both the inventory and the child see it, the inventory
// reads through the same ChildEnv computation the launch delivers (not a
// mirror of it), and lookups resolve last-wins the way the launcher
// delivers. There is no second reader left to disagree.
//
// Four tests hold the class closed:
//   - TestManagedLoaderInputAgreement: one table row per loader-selection
//     input (duplicate CODEX_HOME/HOME in both orders and every spelling,
//     duplicates beside a selected profile, ignored inputs, safe-direction
//     stillborns, refusal agreement), each asserting covered ==
//     kernel-visible == want through agentic.BuildPlan, plus agreement with
//     the installed 0.159.0 loader.
//   - TestManagedChildEnvCarriesOneValuePerKey: the normalization itself —
//     exactly one `key=value` per name under a managed scope, bare entries
//     preserved, unmanaged envs byte-identical.
//   - TestDedupeManagedEnvMatchesGoDelivery / TestLastEnvValueReadsTheChildView:
//     unit vectors pinning Go os/exec parity (last-wins, bare passthrough,
//     order of last occurrence) and the child lookup (last `key=value`
//     wins, bare ignored).
//   - TestManagedEnvHasASingleReader: a source guard failing if the
//     inventory stops routing through System.ChildEnv, if ChildEnv stops
//     deduping, or if a first-wins lookup reappears.

// ---- loader input agreement: one row per input ----

// agreementRow is one loader-selection input: setup builds the request and
// fixture, want is the exact cover set, and kind selects the assertion.
type agreementRow struct {
	name  string
	setup func(t *testing.T) (req agentic.LaunchRequest, want []string)
	// refuse is the typed refusal code BuildPlan must return; empty means
	// the plan admits and covers want.
	refuse string
	// stillborn means the plan admits and covers want while the installed
	// loader itself fails on the input — a safe-direction split (no launch
	// either way), asserted as the stillborn.
	stillborn bool
	// absent names servers the loader must NOT load (refusal rows).
	absent []string
}

// agreementReq is a managed exec request with an isolated HOME and no
// profile, for rows that build their own selecting env.
func agreementReq(t *testing.T, network agentic.Network) agentic.LaunchRequest {
	t.Helper()
	req := managedRequest(t, network)
	req.Profile = ""
	return req
}

// stripHomeKeys removes the selecting keys so the row controls every entry.
func stripHomeKeys(env []string) []string { return filterEnvKeys(env, "HOME", "CODEX_HOME") }

func TestManagedLoaderInputAgreement(t *testing.T) {
	t.Parallel()
	rows := []agreementRow{
		{
			name: "codexhome empty then loaded",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME=", "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "codexhome loaded then empty falls back to HOME",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				fallback := t.TempDir()
				writeHomeConfig(t, fallback, "[mcp_servers.fromhome]\ncommand = \"/bin/home\"\n")
				req.Env = append(stripHomeKeys(req.Env), "HOME="+fallback, "CODEX_HOME="+loaded, "CODEX_HOME=")
				return req, []string{"fromhome"}
			},
		},
		{
			name: "codexhome decoy then loaded",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				decoy, loaded := t.TempDir(), t.TempDir()
				writeCodexHomeConfig(t, decoy, siblingConfig())
				writeCodexHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+decoy, "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "codexhome loaded then decoy",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded, decoy := t.TempDir(), t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				writeCodexHomeConfig(t, decoy, siblingConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded, "CODEX_HOME="+decoy)
				return req, []string{pathSibling}
			},
		},
		{
			name: "codexhome bare then loaded",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME", "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "codexhome loaded then bare",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded, "CODEX_HOME")
				return req, []string{pathFiler}
			},
		},
		{
			name: "codexhome triple",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded, decoy := t.TempDir(), t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				writeCodexHomeConfig(t, decoy, siblingConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded, "CODEX_HOME="+decoy, "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "codexhome same twice",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded, "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "home empty then loaded",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME=", "HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name:   "home loaded then empty refuses",
			refuse: string(refusal.CodeFileUnreadable),
			absent: []string{"tbsentinel261002"},
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeHomeConfig(t, loaded, "[mcp_servers.tbsentinel261002]\ncommand = \"/bin/echo\"\n")
				req.Env = append(stripHomeKeys(req.Env), "HOME="+loaded, "HOME=")
				return req, nil
			},
		},
		{
			name: "home decoy then loaded",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				decoy, loaded := t.TempDir(), t.TempDir()
				writeHomeConfig(t, decoy, siblingConfig())
				writeHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME="+decoy, "HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "home loaded then decoy",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded, decoy := t.TempDir(), t.TempDir()
				writeHomeConfig(t, loaded, filerConfig())
				writeHomeConfig(t, decoy, siblingConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME="+loaded, "HOME="+decoy)
				return req, []string{pathSibling}
			},
		},
		{
			name: "home bare then loaded",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME", "HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "home loaded then bare",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeHomeConfig(t, loaded, filerConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME="+loaded, "HOME")
				return req, []string{pathFiler}
			},
		},
		{
			name: "home triple",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded, decoy := t.TempDir(), t.TempDir()
				writeHomeConfig(t, loaded, filerConfig())
				writeHomeConfig(t, decoy, siblingConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME="+loaded, "HOME="+decoy, "HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "dup PATH ignored",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				path, _ := lastRequestEnv(req.Env, "PATH")
				req.Env = append(stripHomeKeys(req.Env), "PATH="+path, "PATH="+path, "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "dup HOME ignored when CODEX_HOME selects",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				selected := t.TempDir()
				writeCodexHomeConfig(t, selected, filerConfig())
				decoy, other := t.TempDir(), t.TempDir()
				writeHomeConfig(t, decoy, siblingConfig())
				writeHomeConfig(t, other, siblingConfig())
				req.Env = append(stripHomeKeys(req.Env), "HOME="+decoy, "HOME="+other, "CODEX_HOME="+selected)
				return req, []string{pathFiler}
			},
		},
		{
			name: "dup home with selected profile",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				decoy, loaded := t.TempDir(), t.TempDir()
				writeCodexHomeConfig(t, decoy, siblingConfig())
				writeCodexHomeConfig(t, loaded, filerConfig())
				writeProfileConfig(t, loaded, pathProfile, "[mcp_servers.layered]\ncommand = \"/bin/true\"\n")
				req.Profile = pathProfile
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+decoy, "CODEX_HOME="+loaded)
				return req, []string{pathFiler, pathLayered}
			},
		},
		{
			name: "project config decoy ignored",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				projectDot := filepath.Join(req.WorkDir, ".codex")
				if err := os.MkdirAll(projectDot, 0o755); err != nil {
					t.Fatalf("creating the project .codex: %v", err)
				}
				if err := os.WriteFile(filepath.Join(projectDot, "config.toml"), []byte(siblingConfig()), 0o644); err != nil {
					t.Fatalf("writing the project config: %v", err)
				}
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "include key ignored",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, "include = [\"extra.toml\"]\n"+filerConfig())
				if err := os.WriteFile(filepath.Join(loaded, "extra.toml"), []byte(siblingConfig()), 0o644); err != nil {
					t.Fatalf("writing the include target: %v", err)
				}
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name: "XDG decoy ignored",
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, filerConfig())
				xdg := t.TempDir()
				if err := os.WriteFile(filepath.Join(xdg, "config.toml"), []byte(siblingConfig()), 0o644); err != nil {
					t.Fatalf("writing the XDG config: %v", err)
				}
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded, "XDG_CONFIG_HOME="+xdg)
				return req, []string{pathFiler}
			},
		},
		{
			name:      "legacy profile key stillborns the loader",
			stillborn: true,
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				loaded := t.TempDir()
				writeCodexHomeConfig(t, loaded, "profile = \"work\"\n"+filerConfig())
				writeProfileConfig(t, loaded, pathProfile, "[mcp_servers.layered]\ncommand = \"/bin/true\"\n")
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+loaded)
				return req, []string{pathFiler}
			},
		},
		{
			name:      "whitespace-only CODEX_HOME stillborns the loader",
			stillborn: true,
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME=   ")
				return req, []string{}
			},
		},
		{
			name:   "CODEX_HOME is a file",
			refuse: string(refusal.CodeFileUnreadable),
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				file := filepath.Join(t.TempDir(), "notadir")
				if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
					t.Fatalf("writing the file home: %v", err)
				}
				req.Env = append(stripHomeKeys(req.Env), "CODEX_HOME="+file)
				return req, nil
			},
		},
		{
			name:   "no home refuses and loads nothing of ours",
			refuse: string(refusal.CodeFileUnreadable),
			absent: []string{"tbsentinel261002"},
			setup: func(t *testing.T) (agentic.LaunchRequest, []string) {
				req := agreementReq(t, verifiedNetworkCarrier())
				nowhere := t.TempDir()
				writeCodexHomeConfig(t, nowhere, "[mcp_servers.tbsentinel261002]\ncommand = \"/bin/echo\"\n")
				req.Env = stripHomeKeys(req.Env)
				return req, nil
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			req, want := row.setup(t)
			switch {
			case row.refuse != "":
				err := planErrorFor(t, req, agentic.LaunchModeExec)
				if err == nil {
					t.Fatalf("BuildPlan admitted a %s case; want typed %s", row.name, row.refuse)
				}
				code, ok := refusal.CodeOf(err)
				if !ok || string(code) != row.refuse {
					t.Fatalf("BuildPlan err = %v, want typed %s", err, row.refuse)
				}
				requireLoaderLoadsNoneOf(t, req, row.absent)
			case row.stillborn:
				plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)
				requireStillbornRow(t, req, plan, want)
			default:
				requireInventoryMatchesKernelAndLoader(t, req, want)
			}
		})
	}
}

// requireStillbornRow asserts the safe-direction split: the plan admits and
// covers want from the literal input, while the installed loader fails on
// that same input — so no launch follows either way.
func requireStillbornRow(t *testing.T, req agentic.LaunchRequest, plan agentic.Plan, want []string) {
	t.Helper()
	home, ok := effectiveRequestHomeOK(req)
	if !ok {
		t.Fatalf("stillborn row carries no establishable home")
	}
	files := []string{loaderLiteralPath(req.WorkDir, home, "config.toml")}
	oracle := kernelInventoryAt(t, files...)
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
	probe := probeCodexLoader()
	if probe.bin == "" {
		t.Logf("LOADER-SKIP: %s; kernel-oracle assertions stand", probe.err)
		return
	}
	if names, err := loaderListNames(t, probe.bin, req.WorkDir, plan.Env); err == nil {
		t.Fatalf("loader loaded %v, want the stillborn this row pins", names)
	} else {
		t.Logf("loader stillborn as pinned: %v", err)
	}
}

// requireLoaderLoadsNoneOf asserts the loader half of a refusal row: with
// the request's own env, the installed loader loads none of the named
// fixture servers. A loader failure counts as loading none — a stillborn
// loads nothing.
func requireLoaderLoadsNoneOf(t *testing.T, req agentic.LaunchRequest, absent []string) {
	t.Helper()
	probe := probeCodexLoader()
	if probe.bin == "" {
		t.Logf("LOADER-SKIP: %s; refusal assertion stands", probe.err)
		return
	}
	env := append([]string{}, req.Env...)
	names, err := loaderListNames(t, probe.bin, req.WorkDir, env)
	if err != nil {
		t.Logf("loader refuses as well: %v", err)
		return
	}
	for _, name := range absent {
		for _, loaded := range names {
			if loaded == name {
				t.Fatalf("loader loads %q despite the planner's refusal: %v", name, names)
			}
		}
	}
}

// ---- normalization: one value per key under a managed scope ----

// TestManagedChildEnvCarriesOneValuePerKey pins the K1 normalization: under
// a managed scope Plan.Env carries exactly one `key=value` entry per name —
// the last one the request carried — so the inventory and the child resolve
// every key from the same unambiguous value. Bare entries pass through
// (the launcher passes them through too; the lookup ignores them), and an
// unmanaged plan keeps its duplicate bytes verbatim.
func TestManagedChildEnvCarriesOneValuePerKey(t *testing.T) {
	t.Parallel()
	t.Run("duplicates collapse last-wins", func(t *testing.T) {
		t.Parallel()
		req := managedRequest(t, verifiedNetworkCarrier())
		req.Env = append(stripHomeKeys(req.Env),
			"CODEX_HOME=/first", "HOME=/home-first",
			"CODEX_HOME=/second", "HOME=/home-second",
			"CODEX_HOME", "PATH=/a", "PATH=/b",
		)
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		seen := map[string]int{}
		var bare []string
		for _, entry := range plan.Env {
			key, _, found := strings.Cut(entry, "=")
			if !found {
				bare = append(bare, entry)
				continue
			}
			seen[key]++
		}
		for key, count := range seen {
			if count != 1 {
				t.Fatalf("plan env carries %d %q entries, want exactly one: %v", count, key, plan.Env)
			}
		}
		if got, _ := lastRequestEnv(plan.Env, "CODEX_HOME"); got != "/second" {
			t.Fatalf("plan CODEX_HOME = %q, want the last entry %q", got, "/second")
		}
		if got, _ := lastRequestEnv(plan.Env, "HOME"); got != "/home-second" {
			t.Fatalf("plan HOME = %q, want the last entry %q", got, "/home-second")
		}
		if got, _ := lastRequestEnv(plan.Env, "PATH"); got != "/b" {
			t.Fatalf("plan PATH = %q, want the last entry %q", got, "/b")
		}
		found := false
		for _, entry := range bare {
			if entry == "CODEX_HOME" {
				found = true
			}
		}
		if !found {
			t.Fatalf("plan env dropped the bare entry; the launcher preserves it: %v", plan.Env)
		}
	})
	t.Run("unmanaged duplicates pass through", func(t *testing.T) {
		t.Parallel()
		workDir := tempSlot(t)
		binDir := tempSlot(t)
		writeStubExecutable(t, binDir, executableName)
		req := parityRequest(workDir)
		req.PromptPath = writePromptFile(t, workDir, "plain prompt")
		req.Env = []string{"PATH=" + binDir, "CODEX_HOME=/first", "HOME=/h", "CODEX_HOME=/second"}
		plan := buildParityPlan(t, New(), req, agentic.LaunchModeExec)

		var got []string
		for _, entry := range plan.Env {
			if entry == "CODEX_HOME=/first" || entry == "CODEX_HOME=/second" {
				got = append(got, entry)
			}
		}
		if !reflect.DeepEqual(got, []string{"CODEX_HOME=/first", "CODEX_HOME=/second"}) {
			t.Fatalf("unmanaged plan env = %v, want both duplicates in order", plan.Env)
		}
	})
}

// ---- unit vectors: Go delivery parity and the child lookup ----

// TestDedupeManagedEnvMatchesGoDelivery pins dedupeManagedEnv against Go
// os/exec's unix delivery entry by entry: later `key=value` wins, order of
// last occurrence kept, bare entries preserved, empty entries dropped, the
// input never mutated.
func TestDedupeManagedEnvMatchesGoDelivery(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in   []string
		want []string
	}{
		"dup-free is identity": {
			in:   []string{"PATH=/b", "HOME=/h", "CODEX_HOME=/c"},
			want: []string{"PATH=/b", "HOME=/h", "CODEX_HOME=/c"},
		},
		"last wins, order of last occurrence": {
			in:   []string{"B=1", "A=1", "B=2"},
			want: []string{"A=1", "B=2"},
		},
		"conflicting empty and non-empty": {
			in:   []string{"CODEX_HOME=/x", "CODEX_HOME="},
			want: []string{"CODEX_HOME="},
		},
		"bare then kv keeps both": {
			in:   []string{"CODEX_HOME", "CODEX_HOME=/x"},
			want: []string{"CODEX_HOME", "CODEX_HOME=/x"},
		},
		"kv then bare keeps both": {
			in:   []string{"CODEX_HOME=/x", "CODEX_HOME"},
			want: []string{"CODEX_HOME=/x", "CODEX_HOME"},
		},
		"bare duplicates are never deduped": {
			in:   []string{"CODEX_HOME", "CODEX_HOME"},
			want: []string{"CODEX_HOME", "CODEX_HOME"},
		},
		"empty entry dropped": {
			in:   []string{"A=1", "", "B=2"},
			want: []string{"A=1", "B=2"},
		},
		"keys are case-sensitive": {
			in:   []string{"A=1", "a=2"},
			want: []string{"A=1", "a=2"},
		},
		"no-prefix match": {
			in:   []string{"CODEX_HOME_EXTRA=1", "CODEX_HOME=/x"},
			want: []string{"CODEX_HOME_EXTRA=1", "CODEX_HOME=/x"},
		},
		"NUL preserved": {
			in:   []string{"A=1\x00", "B=2"},
			want: []string{"A=1\x00", "B=2"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := append([]string(nil), tc.in...)
			if got := dedupeManagedEnv(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("dedupeManagedEnv(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !reflect.DeepEqual(tc.in, in) {
				t.Fatalf("dedupeManagedEnv mutated its input: %q", tc.in)
			}
		})
	}
}

// TestLastEnvValueReadsTheChildView pins the inventory lookup: the last
// `name=value` entry wins and bare entries never satisfy it — the value the
// exec'd child resolves through the launcher's dedup plus getenv.
func TestLastEnvValueReadsTheChildView(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		env   []string
		value string
		found bool
	}{
		"single":            {env: []string{"CODEX_HOME=/x"}, value: "/x", found: true},
		"last wins":         {env: []string{"CODEX_HOME=/a", "CODEX_HOME=/b"}, value: "/b", found: true},
		"last empty wins":   {env: []string{"CODEX_HOME=/a", "CODEX_HOME="}, value: "", found: true},
		"bare ignored":      {env: []string{"CODEX_HOME", "CODEX_HOME=/x"}, value: "/x", found: true},
		"trailing bare old": {env: []string{"CODEX_HOME=/x", "CODEX_HOME"}, value: "/x", found: true},
		"bare only absent":  {env: []string{"CODEX_HOME"}, value: "", found: false},
		"absent":            {env: []string{"HOME=/h"}, value: "", found: false},
		"no-prefix match":   {env: []string{"CODEX_HOME_EXTRA=/x"}, value: "", found: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, found := lastEnvValue(tc.env, "CODEX_HOME")
			if value != tc.value || found != tc.found {
				t.Fatalf("lastEnvValue = %q,%v, want %q,%v", value, found, tc.value, tc.found)
			}
		})
	}
}

// ---- source guard: the single reader ----

// TestManagedEnvHasASingleReader pins the K1 design against drift: the
// inventory resolves through System.ChildEnv (not a second computation of
// filter+pin+patch), ChildEnv dedupes under a managed scope, and no
// first-wins lookup reaches the managed path again. Each guarded function
// must be found exactly once, so a rename silently dropping it from the
// guard fails instead of passing vacuously.
func TestManagedEnvHasASingleReader(t *testing.T) {
	t.Parallel()
	networkRaw, err := os.ReadFile("network.go")
	if err != nil {
		t.Fatalf("reading network.go: %v", err)
	}
	networkCode := stripLineComments(string(networkRaw))
	if strings.Contains(networkCode, "firstEnvValue") {
		t.Fatalf("network.go resurrects firstEnvValue; the managed lookup is last-wins")
	}
	managed := extractFuncBody(t, networkCode, "managedChildEnv")
	for _, token := range []string{"ChildEnv(", "dedupeManagedEnv"} {
		if !strings.Contains(managed, token) {
			t.Fatalf("managedChildEnv no longer routes through %s", token)
		}
	}
	codexRaw, err := os.ReadFile("codex.go")
	if err != nil {
		t.Fatalf("reading codex.go: %v", err)
	}
	codexCode := stripLineComments(string(codexRaw))
	child := extractFuncBody(t, codexCode, "(*System) ChildEnv")
	if !strings.Contains(child, "dedupeManagedEnv") {
		t.Fatalf("ChildEnv no longer dedupes the managed env")
	}
}
