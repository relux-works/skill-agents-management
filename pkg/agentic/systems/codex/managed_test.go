package codex

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const managedThread = "01a14c36-5f05-76d3-9452-11ce06e095ca"

func managedPrimaryRequest(t *testing.T) agentic.LaunchRequest {
	t.Helper()
	bin := t.TempDir()
	writeStubExecutable(t, bin, "codex")
	return agentic.LaunchRequest{System: New().ID(), Model: agentic.Model{ID: "test-model"}, WorkDir: t.TempDir(), Home: t.TempDir(), Env: []string{"PATH=" + bin, "HOME=" + t.TempDir()}}
}
func managedBuild(req agentic.LaunchRequest) (agentic.Plan, error) {
	r := agentic.NewRegistry()
	if err := r.Register(New()); err != nil {
		return agentic.Plan{}, err
	}
	return agentic.BuildPlan(r, req, agentic.LaunchModeInteractive)
}
func requireManagedPlan(t *testing.T, req agentic.LaunchRequest) agentic.Plan {
	t.Helper()
	p, err := managedBuild(req)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Exact selection and its private home travel through BuildPlan; a second known
// identity must never replace the requested one. No native RPC/history is claimed.
func TestManagedRemoteClientExactThread(t *testing.T) {
	req := managedPrimaryRequest(t)
	req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
	req.NativeArgs = []string{"--no-alt-screen"}
	p := requireManagedPlan(t, req)
	want := []string{"--remote", "unix:///tmp/manager.sock", "resume", managedThread, "--no-alt-screen"}
	if !reflect.DeepEqual(p.Argv, want) {
		t.Fatalf("exact client argv = %q, want %q", p.Argv, want)
	}
	if got := codexPolicyEnvValue(p.Env, "CODEX_HOME"); got != req.Home {
		t.Fatalf("private home = %q", got)
	}
	req.CodexRemoteClient.ThreadID = "01a14c36-65d0-7d80-9218-d374ede83a8a"
	if strings.Contains(strings.Join(p.Argv, " "), req.CodexRemoteClient.ThreadID) {
		t.Fatal("plan aliases request")
	}
}

// Present empty/malformed identities, endpoint credentials, permission overrides
// and competing selectors refuse typed, without yielding a partial plan.
func TestManagedRemoteClientRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(*agentic.LaunchRequest)
	}{
		{"empty-id", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.ThreadID = "" }},
		{"picker-id", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.ThreadID = "--last" }},
		{"malformed-id", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.ThreadID = "not-a-thread" }},
		{"padded-id", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.ThreadID = " " + managedThread }},
		{"empty-endpoint", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.Endpoint = "" }},
		{"relative-endpoint", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.Endpoint = "unix://relative" }},
		{"credential-endpoint", func(r *agentic.LaunchRequest) { r.CodexRemoteClient.Endpoint = "ws://user:secret@localhost:1234" }},
		{"missing-home", func(r *agentic.LaunchRequest) { r.Home = "" }},
		{"latest-tail", func(r *agentic.LaunchRequest) { r.NativeArgs = []string{"resume", "--last"} }},
		{"thread-tail", func(r *agentic.LaunchRequest) { r.NativeArgs = []string{"--resume", managedThread} }},
		{"permission-tail", func(r *agentic.LaunchRequest) { r.NativeArgs = []string{"--sandbox", "danger-full-access"} }},
		{"config-tail", func(r *agentic.LaunchRequest) { r.NativeArgs = []string{"-c", "sandbox_mode=\"danger-full-access\""} }},
		{"yolo", func(r *agentic.LaunchRequest) { r.PermissionMode = agentic.PermissionModeYolo }},
		{"profile", func(r *agentic.LaunchRequest) { r.Profile = "other" }},
		{"service-tier", func(r *agentic.LaunchRequest) { r.ServiceTier = "priority" }},
		{"roots-on-client", func(r *agentic.LaunchRequest) { r.AdditionalWritableRoots = []string{"/tmp/runtime"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := managedPrimaryRequest(t)
			req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
			c.change(&req)
			p, err := managedBuild(req)
			if !errors.Is(err, agentic.ErrManagedLaunchInvalid) || !reflect.DeepEqual(p, agentic.Plan{}) {
				t.Fatalf("managed refusal = %v, plan=%+v", err, p)
			}
		})
	}
}

// The planned PATH resolves BOTH tools in the manager directory despite
// competing executables and duplicate inherited PATH entries. This executes
// fixture tools using the planned PATH, not a real installed session manager.
func TestManagedToolDirectoryWins(t *testing.T) {
	req := managedPrimaryRequest(t)
	manager, competitor := t.TempDir(), t.TempDir()
	for _, tool := range []string{"task-board", "tb-sessiond"} {
		for _, d := range []string{manager, competitor} {
			if err := os.WriteFile(filepath.Join(d, tool), []byte("#!/bin/sh\nprintf '%s' '"+d+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	req.Env = append(req.Env, "PATH="+competitor)
	req.ManagerToolDir = &manager
	p := requireManagedPlan(t, req)
	path := codexPolicyEnvValue(p.Env, "PATH")
	if strings.Split(path, string(os.PathListSeparator))[0] != manager {
		t.Fatalf("manager PATH is not first: %q", path)
	}
	count := 0
	for _, e := range p.Env {
		if strings.HasPrefix(e, "PATH=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("PATH count %d", count)
	}
	for _, tool := range []string{"task-board", "tb-sessiond"} {
		cmd := exec.Command("/bin/sh", "-c", tool)
		cmd.Env = p.Env
		out, err := cmd.Output()
		if err != nil || string(out) != manager {
			t.Fatalf("tool %s resolved %q: %v", tool, out, err)
		}
	}
}

// Regression for manager-path-duplicate: BuildPlan emits one manager directory
// first and preserves every other PATH component's bytes and order. Cleaning is
// lexical and absolute-only; symlink/filesystem equivalence is not claimed.
func TestManagedToolDirectoryDeduplicatesInheritedPath(t *testing.T) {
	manager := t.TempDir()
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"middle", "/usr/bin:" + manager + ":/bin", manager + ":/usr/bin:/bin"},
		{"end", "/usr/bin:/bin:" + manager, manager + ":/usr/bin:/bin"},
		{"trailing-slash", "/usr/bin:" + manager + "/:/bin", manager + ":/usr/bin:/bin"},
		{"dot-segments", "/usr/bin:" + manager + "/./:/bin", manager + ":/usr/bin:/bin"},
		{"multiple-spellings", manager + ":/usr/bin:" + manager + "/:/bin:" + manager + "/./", manager + ":/usr/bin:/bin"},
		{"empty-components", ":" + manager + ":/usr/bin::/bin:" + manager + "/:", manager + "::/usr/bin::/bin:"},
		{"only-empty-tail", manager + ":", manager + ":"},
		{"only-manager", manager, manager},
		{"empty-path", "", manager},
		{"no-manager", "/usr/bin::/bin:", manager + ":/usr/bin::/bin:"},
		{"other-spellings-preserved", "/usr/bin/:/usr/./bin:/bin:/bin", manager + ":/usr/bin/:/usr/./bin:/bin:/bin"},
		{"relative-and-prefix-preserved", strings.TrimPrefix(manager, "/") + ":" + manager + "-other", manager + ":" + strings.TrimPrefix(manager, "/") + ":" + manager + "-other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := managedPrimaryRequest(t)
			req.ManagerToolDir = &manager
			req.Env = append(req.Env, "PATH="+tc.in)
			p := requireManagedPlan(t, req)
			if got := codexPolicyEnvValue(p.Env, "PATH"); got != tc.want {
				t.Fatalf("manager PATH deduplication: got %q, want %q", got, tc.want)
			}
			if err := p.VerifyBeforeExec(); err != nil {
				t.Fatalf("deduplicated PATH seal: %v", err)
			}
		})
	}
}

// Preserve the reviewer's original public-entry regression and its test name.
func TestReviewManagerDuplicate(t *testing.T) {
	req := managedPrimaryRequest(t)
	dir := t.TempDir()
	req.ManagerToolDir = &dir
	req.Env = append(req.Env, "PATH=/usr/bin:"+dir+":/bin:"+dir)
	p := requireManagedPlan(t, req)
	if got, want := codexPolicyEnvValue(p.Env, "PATH"), dir+":/usr/bin:/bin"; got != want {
		t.Fatalf("manager must occur once preserving other PATH entries: got %q want %q", got, want)
	}
}

// Malformed directories and writable roots cannot become PATH components or
// silently cleaned/widened permissions. Valid controls appear in adjacent tests.
func TestManagedPathRefusals(t *testing.T) {
	for _, bad := range []string{"", "relative", "/tmp/../runtime", "/tmp/runtime/", "/tmp/runtime\x00", "/tmp/\xff"} {
		for _, field := range []string{"manager", "roots"} {
			t.Run(field+"/"+bad, func(t *testing.T) {
				req := managedPrimaryRequest(t)
				if field == "manager" {
					req.ManagerToolDir = &bad
				} else {
					req.AdditionalWritableRoots = []string{bad}
				}
				_, err := managedBuild(req)
				if !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
					t.Fatalf("invalid %s admitted: %v", field, err)
				}
			})
		}
	}
	req := managedPrimaryRequest(t)
	bad := "/tmp/one:/tmp/two"
	req.ManagerToolDir = &bad
	if _, err := managedBuild(req); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
		t.Fatalf("PATH injection admitted: %v", err)
	}
}

// TOML decoding asserts the exact ordered list, including quote/non-ASCII path
// bytes, and absence of parent grants. It does not execute Codex's sandbox.
func TestManagedWritableRootsExact(t *testing.T) {
	req := managedPrimaryRequest(t)
	req.AdditionalWritableRoots = []string{filepath.Join(req.WorkDir, "runtime"), "/tmp/quoted\"-é"}
	p := requireManagedPlan(t, req)
	const key = "sandbox_workspace_write.writable_roots="
	var got []string
	count := 0
	for i, a := range p.Argv {
		if strings.HasPrefix(a, key) {
			if i == 0 || p.Argv[i-1] != "-c" {
				t.Fatal("roots lack config flag")
			}
			var v struct {
				Sandbox struct {
					Roots []string `toml:"writable_roots"`
				} `toml:"sandbox_workspace_write"`
			}
			if err := toml.Unmarshal([]byte(a), &v); err != nil {
				t.Fatal(err)
			}
			got = v.Sandbox.Roots
			count++
		}
	}
	if count != 1 || !reflect.DeepEqual(got, req.AdditionalWritableRoots) {
		t.Fatalf("exact writable roots = %q, count=%d", got, count)
	}
	req.AdditionalWritableRoots[0] = "/tmp/changed"
	if strings.Contains(strings.Join(p.Argv, " "), "/tmp/changed") {
		t.Fatal("roots alias request")
	}
}

// Zero inputs preserve the old plan, including no PATH rewrite or new seal.
func TestManagedAbsentUnchanged(t *testing.T) {
	req := managedPrimaryRequest(t)
	p := requireManagedPlan(t, req)
	if want := []string{"-m", "test-model"}; !reflect.DeepEqual(p.Argv, want) {
		t.Fatalf("absent argv=%q", p.Argv)
	}
	req.AdditionalWritableRoots = []string{}
	q := requireManagedPlan(t, req)
	if !reflect.DeepEqual(p.Argv, q.Argv) || !reflect.DeepEqual(p.Env, q.Env) {
		t.Fatal("empty roots change plan")
	}
	if _, err := p.ExportSeal(); !errors.Is(err, agentic.ErrSealNotExportable) {
		t.Fatalf("absent seal changed: %v", err)
	}
}

// Base, finalized and wire-imported seals reject retargeting, PATH replacement
// and root broadening. Permitted unrelated overlays still finalize successfully.
func TestManagedPlanSealCoverage(t *testing.T) {
	for _, field := range []string{"remote", "manager", "roots"} {
		t.Run(field, func(t *testing.T) {
			req := managedPrimaryRequest(t)
			switch field {
			case "remote":
				req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
			case "manager":
				manager := t.TempDir()
				req.ManagerToolDir = &manager
			case "roots":
				req.AdditionalWritableRoots = []string{"/tmp/runtime"}
			}
			base := requireManagedPlan(t, req)
			final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{PromptEnv: []string{"PROMPT_METADATA=ok"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []agentic.Plan{base, final} {
				if err := p.VerifyBeforeExec(); err != nil {
					t.Fatal(err)
				}
				seal, err := p.ExportSeal()
				if err != nil {
					t.Fatal(err)
				}
				wire, err := json.Marshal(seal)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := agentic.DecodeSeal(wire)
				if err != nil {
					t.Fatal(err)
				}
				verifier, err := agentic.ImportSeal(New(), decoded)
				if err != nil {
					t.Fatal(err)
				}
				if err := verifier.VerifyBeforeExec(p); err != nil {
					t.Fatal(err)
				}
				changed := p
				changed.Argv = append([]string(nil), p.Argv...)
				changed.Env = append([]string(nil), p.Env...)
				switch field {
				case "remote":
					for i, a := range changed.Argv {
						if a == managedThread {
							changed.Argv[i] = "01a14c36-65d0-7d80-9218-d374ede83a8a"
						}
					}
				case "manager":
					changed.Env = agentic.SetEnvValue(changed.Env, "PATH", "/tmp/competitor")
				case "roots":
					for i, a := range changed.Argv {
						if strings.HasPrefix(a, "sandbox_workspace_write.writable_roots=") {
							changed.Argv[i] = `sandbox_workspace_write.writable_roots=["/tmp"]`
						}
					}
				}
				if err := changed.VerifyBeforeExec(); err == nil {
					t.Fatal("base/final seal admits changed managed field")
				}
				if err := verifier.VerifyBeforeExec(changed); err == nil {
					t.Fatal("imported seal admits changed managed field")
				}
			}
			// Native tails cannot bypass the semantic inputs, nor can PATH/home overlays.
			overlays := agentic.FinalizeOverlays{NativeTail: []string{"resume", "--last"}}
			if _, err := agentic.FinalizePlan(base, overlays, nil); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
				t.Fatalf("tail overlay admitted: %v", err)
			}
			if field == "manager" || field == "remote" {
				name := "PATH"
				if field == "remote" {
					name = "CODEX_HOME"
				}
				if _, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{FragmentEnv: []string{name + "=/tmp/other"}}, nil); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
					t.Fatalf("owned overlay admitted: %v", err)
				}
			}
		})
	}
}

// The closed client suffix admits only well-formed display/sleep config. A
// searched-for allowed key cannot hide a second policy key in the same token.
func TestManagedClientDisplayControls(t *testing.T) {
	for _, tail := range [][]string{
		{"-c", `model="visible-model"`},
		{"--config", `model_reasoning_effort="high"`},
		{`--config=features.sleep_tool.mode="always_on"`},
	} {
		req := managedPrimaryRequest(t)
		req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "ws://127.0.0.1:1234/", ThreadID: managedThread}
		req.NativeArgs = tail
		p := requireManagedPlan(t, req)
		if !reflect.DeepEqual(p.Argv[len(p.Argv)-len(tail):], tail) {
			t.Fatal("display tail changed")
		}
	}
	for _, tail := range [][]string{
		{"-c"}, {"-c", `model=1`}, {"-c", "model=\"ok\"\nsandbox_mode=\"danger-full-access\""},
		{"-c", "features.sleep_tool.mode=\"always_on\"\nfeatures.sleep_tool.other=\"bad\""},
		{"--last"}, {"--remote", "ws://127.0.0.1:1234"},
	} {
		req := managedPrimaryRequest(t)
		req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
		req.NativeArgs = tail
		if _, err := managedBuild(req); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
			t.Fatalf("display/config injection admitted: %q %v", tail, err)
		}
	}
}

// Primaries may select app-server, and reject root overrides/bypass in every
// other native spelling. Other modes cannot silently discard managed inputs.
func TestManagedPrimaryScopeControls(t *testing.T) {
	req := managedPrimaryRequest(t)
	req.AdditionalWritableRoots = []string{"/tmp/runtime"}
	req.NativeArgs = []string{"app-server"}
	if p := requireManagedPlan(t, req); p.Argv[len(p.Argv)-1] != "app-server" {
		t.Fatal("app-server dropped")
	}
	for _, tail := range [][]string{{"-c", `sandbox_workspace_write.writable_roots=["/tmp"]`}, {"--yolo"}, {"--sandbox", "danger-full-access"}} {
		req.NativeArgs = tail
		if _, err := managedBuild(req); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
			t.Fatalf("root conflict admitted: %v", err)
		}
	}
	req.NativeArgs = nil
	req.PermissionMode = agentic.PermissionModeYolo
	if _, err := managedBuild(req); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
		t.Fatalf("root bypass admitted: %v", err)
	}
	req.PermissionMode = "typo"
	if _, err := managedBuild(req); !errors.Is(err, agentic.ErrPermissionModeUnknown) {
		t.Fatalf("context validation swallowed: %v", err)
	}
	req.PermissionMode = ""
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeManagedSession} {
		r := agentic.NewRegistry()
		_ = r.Register(New())
		if _, err := agentic.BuildPlan(r, req, mode); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
			t.Fatalf("roots in %s admitted: %v", mode, err)
		}
		remote := managedPrimaryRequest(t)
		remote.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
		if _, err := agentic.BuildPlan(r, remote, mode); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
			t.Fatalf("remote in %s admitted: %v", mode, err)
		}
	}
	// Validation precedes binary resolution: no executable can mask malformed ID.
	req = managedPrimaryRequest(t)
	req.Env = nil
	req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: "invalid"}
	if _, err := managedBuild(req); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
		t.Fatalf("pre-I/O refusal lost: %v", err)
	}
}

// Imported guards reject malformed selectors. Finalized selectors verify with
// an explicit portable key, and a different key is never sufficient evidence.
func TestManagedSealKeyAndSelectorControls(t *testing.T) {
	req := managedPrimaryRequest(t)
	manager := t.TempDir()
	req.ManagerToolDir = &manager
	base := requireManagedPlan(t, req)
	key := agentic.SealCommitmentKey{1, 2, 3}
	p, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := p.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	v, err := agentic.ImportSeal(New(), seal, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.VerifyBeforeExec(p); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.ImportSeal(New(), seal, agentic.SealCommitmentKey{9}); err == nil {
		t.Fatal("wrong key admitted")
	}
	for _, change := range []func(*agentic.SealedData){
		func(d *agentic.SealedData) { d.Selectors[managedSealSelector] = "v2" },
		func(d *agentic.SealedData) { d.Selectors["PATH"] = "/literal/path" },
		func(d *agentic.SealedData) { d.Selectors["UNKNOWN"] = "value" },
	} {
		data := baseSealData(t, base)
		change(&data)
		if _, err := New().ImportSealedData(data); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
			t.Fatalf("malformed managed seal admitted: %v", err)
		}
	}
}
func baseSealData(t *testing.T, p agentic.Plan) agentic.SealedData {
	t.Helper()
	s, err := p.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	return *s.Data.Sealed
}

// Duplicate, removed and changed owned selectors refuse both in-memory and
// after wire import, including when an injected duplicate repeats the value.
func TestManagedSealOwnedEnvControls(t *testing.T) {
	for _, field := range []string{"PATH", "CODEX_HOME"} {
		req := managedPrimaryRequest(t)
		if field == "PATH" {
			manager := t.TempDir()
			req.ManagerToolDir = &manager
		} else {
			req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
		}
		p := requireManagedPlan(t, req)
		seal, err := p.ExportSeal()
		if err != nil {
			t.Fatal(err)
		}
		v, err := agentic.ImportSeal(New(), seal)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"duplicate", "remove", "change"} {
			q := p
			q.Env = append([]string(nil), p.Env...)
			switch kind {
			case "duplicate":
				q.Env = append(q.Env, field+"="+codexPolicyEnvValue(q.Env, field))
			case "remove":
				q.Env = agentic.SetEnvValue(q.Env, field, "")
			case "change":
				q.Env = agentic.SetEnvValue(q.Env, field, "/tmp/other")
			}
			if err := q.VerifyBeforeExec(); err == nil {
				t.Fatalf("%s %s admitted", field, kind)
			}
			if err := v.VerifyBeforeExec(q); err == nil {
				t.Fatalf("imported %s %s admitted", field, kind)
			}
		}
	}
}

// Managed guards retain both base catalog and TMPDIR protections. Artifact
// swaps after planning fail before execution and at import without re-minting.
func TestManagedWrappedCatalogAndTempDir(t *testing.T) {
	home, wd := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	sealUniqueCatalog(t, home, 103)
	req := catalogRequest(t, home, wd, "low")
	manager := t.TempDir()
	temp := t.TempDir()
	req.ManagerToolDir = &manager
	req.TempDir = &temp
	base, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := base.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	imported, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatal(err)
	}
	if err := imported.VerifyBeforeExec(base); err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Env = agentic.SetEnvValue(base.Env, TempDirEnv, "/tmp/changed")
	if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
		t.Fatalf("wrapped TMPDIR drift admitted: %v", err)
	}
	artifact := effectiveCatalogPath(t, base)
	original := sealCatalogBytes(t, artifact)
	sealSwappedArtifact(t, artifact, original)
	for _, p := range []agentic.Plan{base, final} {
		if err := p.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
			t.Fatalf("wrapped catalog drift admitted: %v", err)
		}
	}
	if _, err := agentic.ImportSeal(New(), seal); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("wrapped import resealed catalog: %v", err)
	}
}

// Hand-held plugin callers meet the same manager-path rejection as BuildPlan.
func TestManagedDirectChildEnvRefusal(t *testing.T) {
	path := "relative"
	req := agentic.LaunchRequest{ManagerToolDir: &path}
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrManagedLaunchInvalid) {
		t.Fatalf("direct manager path admitted: %v", err)
	}
}
