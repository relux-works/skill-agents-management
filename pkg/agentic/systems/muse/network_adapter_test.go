package muse

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func museNetworkRequest(t *testing.T, build string) agentic.LaunchRequest {
	t.Helper()
	req := launchRequest(t, "network prompt")
	req.Env = append(req.Env, "HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(), "XDG_DATA_HOME="+t.TempDir(), "HTTPS_PROXY=hostile", "Http_Proxy=hostile", "MUSE_NO_AUTO_UPDATE=0")
	req.Network = agentic.Network{Patch: envpatch.Patch{Unset: envpatch.UnsetNames()}, Record: binding.Record{Schema: binding.SchemaRecord, ProfileRef: "test", ProfileDigest: "sha256:test", Assurance: "cooperative", Origin: "explicit", AdapterIdentity: binding.AdapterIdentity{Adapter: "muse-env-v1", Harness: "muse", Build: build, Entrypoint: "exec"}}}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
		req.Network.Patch.Set = append(req.Network.Patch.Set, envpatch.Pair{Name: name, Value: "http://127.0.0.1:18081"})
	}
	return req
}
func museNetworkPlan(t *testing.T, req agentic.LaunchRequest) (agentic.Plan, error) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	return agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
}
func writeNetworkSettings(t *testing.T, req agentic.LaunchRequest, data string) string {
	t.Helper()
	dir := filepath.Join(envValue(dedupeEnv(req.Env), "XDG_CONFIG_HOME"), "muse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
func readNetworkSettings(t *testing.T, plan agentic.Plan) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(envValue(plan.Env, "XDG_CONFIG_HOME"), "muse", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestMuseNetworkExactBuildGate(t *testing.T) {
	for _, build := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1", "1.4.1-R4503.2", "1.4.2-R4684.2", "1.4.1", "1.4.2", "1.5.0", ""} {
		t.Run(build, func(t *testing.T) {
			req := museNetworkRequest(t, build)
			plan, err := museNetworkPlan(t, req)
			supported := build == "1.4.1-R4503.1" || build == "1.4.2-R4684.1"
			if !supported {
				if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
					t.Fatalf("want unsupported build refusal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			record, ok := plan.NetworkProvenanceSnapshot()
			if !ok || record != req.Network.Record {
				t.Fatal("adapter Record was not preserved")
			}
		})
	}
	for _, dimension := range []string{"adapter", "harness", "entrypoint", "tool-release"} {
		t.Run(dimension, func(t *testing.T) {
			req := museNetworkRequest(t, "1.4.1-R4503.1")
			switch dimension {
			case "adapter":
				req.Network.Record.AdapterIdentity.Adapter = "generic-env-v1"
			case "harness":
				req.Network.Record.AdapterIdentity.Harness = "codex-cli"
			case "entrypoint":
				req.Network.Record.AdapterIdentity.Entrypoint = "interactive"
			case "tool-release":
				req.ToolRelease = "1.5.0"
			}
			_, err := museNetworkPlan(t, req)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("want unsupported build refusal, got %v", err)
			}
		})
	}
}

// R0 unmanaged control; R1/R3 patch representations; R2/R5/R6 are transport
// observations from the pinned contract, not invented network probe passes.
func TestMuseNetworkR0R6PlanRegressions(t *testing.T) {
	for _, build := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1"} {
		for _, shape := range []string{"proxy", "direct", "lowercase", "uppercase", "all-proxy"} {
			t.Run(build+"/"+shape, func(t *testing.T) {
				req := museNetworkRequest(t, build)
				switch shape {
				case "direct":
					req.Network.Patch.Set = nil
				case "lowercase":
					req.Network.Patch.Set = []envpatch.Pair{{Name: "https_proxy", Value: "http://127.0.0.1:18081"}}
				case "uppercase":
					req.Network.Patch.Set = []envpatch.Pair{{Name: "HTTPS_PROXY", Value: "http://127.0.0.1:18081"}}
				case "all-proxy":
					req.Network.Patch.Set = []envpatch.Pair{{Name: "ALL_PROXY", Value: "http://127.0.0.1:18081"}}
				}
				plan, err := museNetworkPlan(t, req)
				if err != nil {
					t.Fatal(err)
				}
				for _, pair := range req.Network.Patch.Set {
					if envValue(plan.Env, pair.Name) != pair.Value {
						t.Fatalf("process patch missing %s", pair.Name)
					}
				}
				if envValue(plan.Env, "Http_Proxy") != "" || envValue(plan.Env, museNoAutoUpdateEnv) != "1" || envValue(plan.Env, "HOME") != envValue(req.Env, "HOME") {
					t.Fatal("Muse env invariant changed")
				}
				if shape == "direct" && envValue(plan.Env, "HTTPS_PROXY") != "" {
					t.Fatal("direct retains proxy")
				}
				if err := plan.VerifyBeforeExec(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
func TestMuseNetworkInjectsEveryServerR4b(t *testing.T) {
	for _, build := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1"} {
		t.Run(build, func(t *testing.T) {
			req := museNetworkRequest(t, build)
			source := writeNetworkSettings(t, req, `{"schema_version":1,"model":"kept","mcpServers":{"alpha":{"command":"alpha","env":{"KEEP":"yes","Http_Proxy":"hostile","ALL_PROXY":"hostile"}},"beta":{"type":"stdio","command":"beta","args":["--stdio"]},"remote":{"type":"http","url":"https://example.invalid/mcp"}}}`)
			original, _ := os.ReadFile(source)
			plan, err := museNetworkPlan(t, req)
			if err != nil {
				t.Fatal(err)
			}
			settings := readNetworkSettings(t, plan)
			servers := settings["mcpServers"].(map[string]any)
			for _, name := range []string{"alpha", "beta"} {
				server := servers[name].(map[string]any)
				env, ok := server["env"].(map[string]any)
				if !ok {
					t.Fatalf("MCP injection missing for %s", name)
				}
				for _, pair := range req.Network.Patch.Set {
					if env[pair.Name] != pair.Value {
						t.Fatalf("MCP injection missing for %s/%s", name, pair.Name)
					}
				}
				if env["Http_Proxy"] != nil || env["ALL_PROXY"] != nil {
					t.Fatal("stale proxy retained")
				}
			}
			if servers["alpha"].(map[string]any)["env"].(map[string]any)["KEEP"] != "yes" || settings["model"] != "kept" {
				t.Fatal("unrelated settings dropped")
			}
			if servers["remote"].(map[string]any)["env"] != nil {
				t.Fatal("HTTP server received stdio env")
			}
			current, _ := os.ReadFile(source)
			if string(current) != string(original) {
				t.Fatal("shared config rewritten")
			}
		})
	}
}
func TestMuseNetworkWrapsEveryHookCommand(t *testing.T) {
	req := museNetworkRequest(t, "1.4.1-R4503.1")
	writeNetworkSettings(t, req, `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"printf '%s' \"$HTTPS_PROXY\"; printf ':%s' \"$NO_PROXY\" | cat"},{"type":"command","command":"printf '%s' \"$http_proxy\""}]}]}}`)
	plan, err := museNetworkPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	settings := readNetworkSettings(t, plan)
	handlers := settings["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)
	for i, value := range handlers {
		command := value.(map[string]any)["command"].(string)
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		out, err := cmd.CombinedOutput()
		if err != nil || (i == 0 && string(out) != req.Network.Patch.Set[0].Value+":"+req.Network.Patch.Set[0].Value || i == 1 && string(out) != req.Network.Patch.Set[0].Value) {
			t.Fatalf("hook wrapping missing: out=%q err=%v", out, err)
		}
	}
}
func TestMuseNetworkRefusesUnknownShapes(t *testing.T) {
	for name, data := range map[string]string{
		"duplicate-root":     `{"mcpServers":{},"mcpServers":{"x":{"command":"x"}}}`,
		"duplicate-entry":    `{"mcpServers":{"x":{"command":"x","command":"y"}}}`,
		"trailing":           `{} {}`,
		"null":               `null`,
		"mcp-root":           `{"mcpServers":[]}`,
		"mcp-entry":          `{"mcpServers":{"x":null}}`,
		"mcp-transport":      `{"mcpServers":{"x":{"type":"future","command":"x"}}}`,
		"mcp-member":         `{"mcpServers":{"x":{"command":"x","transport":"stdio"}}}`,
		"mcp-ambiguous":      `{"mcpServers":{"x":{"command":"x","url":"https://example.invalid"}}}`,
		"mcp-env":            `{"mcpServers":{"x":{"command":"x","env":{"N":1}}}}`,
		"mcp-args":           `{"mcpServers":{"x":{"command":"x","args":[1]}}}`,
		"hook-root":          `{"hooks":[]}`,
		"hook-event":         `{"hooks":{"SessionStart":{}}}`,
		"hook-member":        `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo ok","env":{}}]}]}}`,
		"hook-command":       `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":["echo","ok"]}]}]}}`,
		"hook-type":          `{"hooks":{"SessionStart":[{"hooks":[{"type":"prompt","command":"ok"}]}]}}`,
		"alternate-mcp":      `{"mcp_servers":{}}`,
		"hook-unknown-event": `{"hooks":{"FutureEvent":[{"hooks":[{"type":"command","command":"echo ok"}]}]}}`,
		"hook-timeout":       `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo ok","timeout":"ten"}]}]}}`,
		"managed-missing":    `{"managed_hooks_path":"missing.json"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := museNetworkRequest(t, "1.4.1-R4503.1")
			writeNetworkSettings(t, req, data)
			_, err := museNetworkPlan(t, req)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("want unknown shape refusal, got %v", err)
			}
		})
	}
}
func TestMuseNetworkInventoriesEffectiveChildEnv(t *testing.T) {
	req := museNetworkRequest(t, "1.4.2-R4684.1")
	original := envValue(req.Env, "XDG_CONFIG_HOME")
	writeNetworkSettings(t, req, `{"mcpServers":{"wrong":{"command":"wrong"}}}`)
	req.Env = append(req.Env, "XDG_CONFIG_HOME="+t.TempDir())
	writeNetworkSettings(t, req, `{"mcpServers":{"right":{"command":"right"}}}`)
	plan, err := museNetworkPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	servers := readNetworkSettings(t, plan)["mcpServers"].(map[string]any)
	if servers["right"] == nil || servers["wrong"] != nil || envValue(plan.Env, "XDG_CONFIG_HOME") == original {
		t.Fatal("inventory diverges from child's last-wins config")
	}
	count := 0
	for _, entry := range plan.Env {
		if strings.HasPrefix(entry, "XDG_CONFIG_HOME=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("config selector duplicated %d times", count)
	}
	owned, err := New().ChildEnv(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if envValue(owned, "XDG_CONFIG_HOME") != envValue(plan.Env, "XDG_CONFIG_HOME") {
		t.Fatal("owned config selection differs")
	}
}
func TestMuseNetworkLinksAuthAndManagedHooks(t *testing.T) {
	req := museNetworkRequest(t, "1.4.2-R4684.1")
	source := writeNetworkSettings(t, req, `{"managed_hooks_path":"managed.json"}`)
	dir := filepath.Dir(source)
	for name, data := range map[string]string{"auth.json": "opaque-test-auth", "trust.json": "{}", "managed.json": `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"printf '%s' \"$HTTPS_PROXY\""}]}]}}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := museNetworkPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(envValue(plan.Env, "XDG_CONFIG_HOME"), "muse")
	for _, name := range []string{"auth.json", "trust.json"} {
		target, err := os.Readlink(filepath.Join(private, name))
		if err != nil || target != filepath.Join(dir, name) {
			t.Fatalf("%s not passed by link", name)
		}
	}
	path := readNetworkSettings(t, plan)["managed_hooks_path"].(string)
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "env ") || path == filepath.Join(dir, "managed.json") {
		t.Fatal("managed hook not privately wrapped")
	}
	var managed map[string]any
	if err := json.Unmarshal(data, &managed); err != nil {
		t.Fatal(err)
	}
	command := managed["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	for _, pair := range req.Network.Patch.Set {
		if !strings.Contains(command, pair.Name+"="+pair.Value) {
			t.Fatalf("managed hook patch missing %s", pair.Name)
		}
	}

}
func TestMuseNetworkRefusesUncoveredSources(t *testing.T) {
	for _, shape := range []string{"project-mcp", "project-hook", "plugin", "read-failed", "dangling", "no-home", "relative-home"} {
		t.Run(shape, func(t *testing.T) {
			req := museNetworkRequest(t, "1.4.1-R4503.1")
			switch shape {
			case "project-mcp":
				os.WriteFile(filepath.Join(req.WorkDir, ".mcp.json"), []byte(`{}`), 0o600)
			case "project-hook":
				os.Mkdir(filepath.Join(req.WorkDir, ".muse"), 0o700)
				os.WriteFile(filepath.Join(req.WorkDir, ".muse", "hooks.json"), []byte(`{}`), 0o600)
			case "plugin":
				os.MkdirAll(filepath.Join(envValue(req.Env, "XDG_DATA_HOME"), "muse", "plugins"), 0o700)
			case "read-failed":
				os.MkdirAll(filepath.Join(envValue(req.Env, "XDG_CONFIG_HOME"), "muse", "settings.json"), 0o700)
			case "dangling":
				dir := filepath.Join(envValue(dedupeEnv(req.Env), "XDG_CONFIG_HOME"), "muse")
				os.MkdirAll(dir, 0o700)
				os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "settings.json"))
			case "no-home":
				req.Env = []string{"PATH=" + envValue(req.Env, "PATH")}
			case "relative-home":
				req.Env = agentic.SetEnvValue(req.Env, "XDG_CONFIG_HOME", "relative")
			}
			_, err := museNetworkPlan(t, req)
			if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("want uncovered source refusal, got %v", err)
			}
		})
	}
}
func TestMuseNetworkSealRefusesTampering(t *testing.T) {
	for _, shape := range []string{"bytes", "selector", "home", "project-added"} {
		t.Run(shape, func(t *testing.T) {
			req := museNetworkRequest(t, "1.4.1-R4503.1")
			plan, err := museNetworkPlan(t, req)
			if err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "bytes":
				path := filepath.Join(envValue(plan.Env, "XDG_CONFIG_HOME"), "muse", "settings.json")
				os.Chmod(path, 0o600)
				os.WriteFile(path, []byte(`{"model":"tampered"}`), 0o400)
				os.Chmod(path, 0o400)
			case "selector":
				plan.Env = agentic.SetEnvValue(plan.Env, "XDG_CONFIG_HOME", t.TempDir())
			case "home":
				plan.Env = agentic.SetEnvValue(plan.Env, "HOME", t.TempDir())
			case "project-added":
				os.WriteFile(filepath.Join(req.WorkDir, ".mcp.json"), []byte(`{}`), 0o600)
			}
			if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("want seal refusal, got %v", err)
			}
		})
	}
}

func TestMuseNetworkEnvOwnedParityAndFinalOverlay(t *testing.T) {
	req := museNetworkRequest(t, "1.4.2-R4684.1")
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	result, err := agentic.BuildPlanWithEnvironment(registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range req.Network.Patch.Set {
		if envValue(result.Plan.Env, pair.Name) != pair.Value || envValue(result.OwnedEnv, pair.Name) != pair.Value {
			t.Fatal("Env/OwnedEnv network parity broken")
		}
	}
	if envValue(result.Plan.Env, "XDG_CONFIG_HOME") != envValue(result.OwnedEnv, "XDG_CONFIG_HOME") {
		t.Fatal("private selector ownership lost")
	}
	overlaid := agentic.SetEnvValue(result.Plan.Env, "HTTPS_PROXY", "hostile")
	result.Plan.Env = req.Network.Patch.Apply(overlaid)
	if envValue(result.Plan.Env, "HTTPS_PROXY") != req.Network.Patch.Set[1].Value {
		t.Fatal("owner final patch lost")
	}
	if err := result.Plan.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
}
func TestMuseNetworkRefusesNonProxyPatch(t *testing.T) {
	for _, half := range []string{"set", "unset"} {
		t.Run(half, func(t *testing.T) {
			req := museNetworkRequest(t, "1.4.1-R4503.1")
			if half == "set" {
				req.Network.Patch.Set = append(req.Network.Patch.Set, envpatch.Pair{Name: "HOME", Value: t.TempDir()})
			} else {
				req.Network.Patch.Unset = append(req.Network.Patch.Unset, "HOME")
			}
			_, err := museNetworkPlan(t, req)
			if !errors.Is(err, agentic.ErrNetworkConfigurationConflict) {
				t.Fatalf("want non-proxy patch conflict, got %v", err)
			}
		})
	}
}
func TestMuseNetworkRefusesUnknownConfigSibling(t *testing.T) {
	req := museNetworkRequest(t, "1.4.1-R4503.1")
	source := writeNetworkSettings(t, req, `{}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(source), "extra.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := museNetworkPlan(t, req)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("want unknown sibling refusal, got %v", err)
	}
}
func TestMuseNetworkRefusesInteractiveTupleReplay(t *testing.T) {
	req := museNetworkRequest(t, "1.4.1-R4503.1")
	req.PromptPath = ""
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeInteractive)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("want interactive tuple refusal, got %v", err)
	}
}

func TestMuseNetworkOwnedSnapshotDoesNotRereadSource(t *testing.T) {
	req := museNetworkRequest(t, "1.4.1-R4503.1")
	writeNetworkSettings(t, req, `{"mcpServers":{"alpha":{"command":"alpha"}}}`)
	plan, err := museNetworkPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	writeNetworkSettings(t, req, `{"mcpServers":{"beta":{"command":"beta"}}}`)
	owned, err := New().ChildEnv(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if envValue(owned, "XDG_CONFIG_HOME") != envValue(plan.Env, "XDG_CONFIG_HOME") {
		t.Fatal("owned snapshot reread mutable settings")
	}
	_, err = museNetworkPlan(t, req)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("want changed source refusal, got %v", err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatal(err)
	}
}

func TestMuseNetworkRefusesAncestorProjectSources(t *testing.T) {
	req := museNetworkRequest(t, "1.4.1-R4503.1")
	root := req.WorkDir
	req.WorkDir = filepath.Join(root, "nested")
	if err := os.Mkdir(req.WorkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := museNetworkPlan(t, req)
	if !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("want ancestor source refusal, got %v", err)
	}
}

func TestMuseNetworkPermissionTripleKeepsExactBuildGate(t *testing.T) {
	req := museNetworkRequest(t, "1.4.1-R4503.1")
	req.ToolRelease = "1.4.1"
	if _, err := museNetworkPlan(t, req); err != nil {
		t.Fatal(err)
	}
	req.Network.Record.AdapterIdentity.Build = "1.4.1-R4503.2"
	if _, err := museNetworkPlan(t, req); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
		t.Fatalf("want exact build refusal despite matching triple, got %v", err)
	}
}

func TestMuseNetworkEnvValueReadsEffectiveChildView(t *testing.T) {
	env := []string{"XDG_CONFIG_HOME=/first", "HOME=/home", "XDG_CONFIG_HOME=/last", "BARE", "MUSE_NO_AUTO_UPDATE=1"}
	if got := envValue(env, "XDG_CONFIG_HOME"); got != "/last" {
		t.Fatalf("envValue reads %q, want last-wins %q", got, "/last")
	}
	if got := envValue(env, "HOME"); got != "/home" {
		t.Fatalf("envValue reads %q, want %q", got, "/home")
	}
	if got := envValue(env, "BARE"); got != "" {
		t.Fatalf("bare entry satisfies lookup with %q", got)
	}
	if got := envValue(env, "MISSING"); got != "" {
		t.Fatalf("missing key reads %q, want empty", got)
	}
	if hasDuplicateSensitiveSelector([]string{"HOME=/a", "HOME=/a"}) != true {
		t.Fatal("same-value HOME duplicate not detected")
	}
	if hasDuplicateSensitiveSelector([]string{"HOME=/a", "PATH=/b", "PATH=/c"}) != true {
		t.Fatal("sensitive PATH duplicate not detected")
	}
	if hasDuplicateSensitiveSelector([]string{"HOME=/a", "BARE", "BARE"}) != false {
		t.Fatal("bare entries counted as duplicates")
	}
}

func TestMuseNetworkRefusesDuplicateFinalSelectors(t *testing.T) {
	for _, shape := range []struct {
		name  string
		key   string
		value string
		same  bool
	}{
		{name: "XDG_CONFIG_HOME", key: "XDG_CONFIG_HOME"},
		{name: "HOME", key: "HOME"},
		{name: "XDG_DATA_HOME", key: "XDG_DATA_HOME"},
		{name: "MUSE_NO_AUTO_UPDATE", key: "MUSE_NO_AUTO_UPDATE", value: "0"},
		{name: "SHELL", key: "SHELL"},
		{name: "XDG_CACHE_HOME", key: "XDG_CACHE_HOME"},
		{name: "same-value-XDG_CONFIG_HOME", key: "XDG_CONFIG_HOME", same: true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			req := museNetworkRequest(t, "1.4.1-R4503.1")
			if shape.key == "SHELL" {
				req.Env = append(req.Env, "SHELL=/bin/sh")
			}
			if shape.key == "XDG_CACHE_HOME" {
				req.Env = append(req.Env, "XDG_CACHE_HOME="+t.TempDir())
			}
			writeNetworkSettings(t, req, `{"mcpServers":{"alpha":{"command":"alpha"}}}`)
			plan, err := museNetworkPlan(t, req)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.VerifyBeforeExec(); err != nil {
				t.Fatalf("sealed plan refuses before late duplicate: %v", err)
			}
			value := shape.value
			if shape.same {
				value = envValue(plan.Env, shape.key)
			} else if value == "" {
				value = t.TempDir()
				if value == envValue(plan.Env, shape.key) {
					t.Fatal("late value equals sealed value; want divergence")
				}
			}
			plan.Env = append(plan.Env, shape.key+"="+value)
			cmd := exec.Command("/usr/bin/env")
			cmd.Env = plan.Env
			output, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), shape.key+"="+value+"\n") {
				t.Fatal("exec did not deliver the late duplicate last-wins")
			}
			if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				t.Fatalf("late duplicate %s reaches the child but the seal admits it: %v", shape.key, err)
			}
		})
	}
}

func TestPanelDuplicatePathResolution(t *testing.T) {
	for _, build := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1"} {
		t.Run(build, func(t *testing.T) {
			req := museNetworkRequest(t, build)
			first := envValue(req.Env, "PATH")
			last := t.TempDir()
			if err := os.WriteFile(filepath.Join(last, "muse"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			req.Env = append(req.Env, "PATH="+last)
			plan, err := museNetworkPlan(t, req)
			if errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.VerifyBeforeExec(); err != nil {
				t.Fatal(err)
			}
			if got := envValue(plan.Env, "PATH"); got != last {
				t.Fatal("child PATH mismatch")
			}
			cmd := exec.Command("/bin/sh", "-c", "command -v muse")
			cmd.Env = plan.Env
			output, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(output)) != filepath.Join(last, "muse") {
				t.Fatal("child lookup did not select last PATH")
			}
			if plan.Binary == filepath.Join(first, "muse") {
				t.Fatal("first-match binary selected while final child PATH is last-match; VerifyBeforeExec admits the split")
			}
			if plan.Binary != filepath.Join(last, "muse") {
				t.Fatal("unexpected binary")
			}
		})
	}
}

func TestMuseNetworkDuplicatePATHRefusesBeforeResolution(t *testing.T) {
	for _, build := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1"} {
		for _, shape := range []string{"same-value", "missing-first", "missing-last"} {
			t.Run(build+"/"+shape, func(t *testing.T) {
				req := museNetworkRequest(t, build)
				first, last := envValue(req.Env, "PATH"), envValue(req.Env, "PATH")
				if shape == "missing-first" {
					first = t.TempDir()
				}
				if shape == "missing-last" {
					last = t.TempDir()
				}
				req.Env = agentic.SetEnvValue(req.Env, "PATH", first)
				req.Env = append(req.Env, "PATH="+last)
				if _, err := museNetworkPlan(t, req); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
					t.Fatalf("want duplicate PATH refusal before binary resolution, got %v", err)
				}
				if _, err := New().ResolveBinary(req); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
					t.Fatalf("direct resolution admitted duplicate PATH: %v", err)
				}
			})
		}
	}
}

func TestMuseResolveBinaryUsesEffectivePATH(t *testing.T) {
	req := launchRequest(t, "effective PATH")
	last := t.TempDir()
	want := filepath.Join(last, "muse")
	if err := os.WriteFile(want, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	req.Env = append(req.Env, "PATH="+last)
	got, err := New().ResolveBinary(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("binary resolution reads first PATH: got %q, want effective last PATH %q", got, want)
	}
	cmd := exec.Command("/bin/sh", "-c", "command -v muse")
	cmd.Env = req.Env
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != got {
		t.Fatalf("resolved binary differs from effective child PATH: output=%q err=%v", output, err)
	}
}

func TestMuseNetworkRefusesDuplicateFinalPATH(t *testing.T) {
	for _, build := range []string{"1.4.1-R4503.1", "1.4.2-R4684.1"} {
		for _, same := range []bool{false, true} {
			name := "different-value"
			if same {
				name = "same-value"
			}
			t.Run(build+"/"+name, func(t *testing.T) {
				req := museNetworkRequest(t, build)
				plan, err := museNetworkPlan(t, req)
				if err != nil {
					t.Fatal(err)
				}
				if err := plan.VerifyBeforeExec(); err != nil {
					t.Fatal(err)
				}
				value := t.TempDir()
				if same {
					value = envValue(plan.Env, "PATH")
				}
				plan.Env = append(plan.Env, "PATH="+value)
				plan.Env = req.Network.Patch.Apply(plan.Env)
				if err := plan.VerifyBeforeExec(); !errors.Is(err, agentic.ErrNetworkScopeUnsupported) {
					t.Fatalf("late duplicate PATH reaches the child but the seal admits it: %v", err)
				}
			})
		}
	}
}
