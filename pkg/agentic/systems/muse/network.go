package muse

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/relux-works/curator-network-profiles/pkg/binding"
	"github.com/relux-works/curator-network-profiles/pkg/envpatch"
	"github.com/relux-works/skill-agents-management/internal/launchenv"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Exact D8 tuples, integration-contract.md §2.1 at v0.2.0. The process
// owner establishes the build; a release triple alone is not a build identity.
func networkAdapters() []binding.AdapterIdentity {
	return []binding.AdapterIdentity{
		{Adapter: "muse-env-v1", Harness: "muse", Build: "1.4.1-R4503.1", Entrypoint: "exec"},
		{Adapter: "muse-env-v1", Harness: "muse", Build: "1.4.2-R4684.1", Entrypoint: "exec"},
	}
}

func admitNetwork(req agentic.LaunchRequest) error {
	if err := agentic.GateNetwork(req.Network, func() (agentic.Capabilities, error) { return New().Capabilities(), nil }); err != nil {
		return err
	}
	if !req.Network.IsZero() && req.ToolRelease != "" && req.ToolRelease != req.Network.Record.AdapterIdentity.Build && req.ToolRelease != strings.SplitN(req.Network.Record.AdapterIdentity.Build, "-R", 2)[0] {
		return agentic.ErrNetworkScopeUnsupported
	}
	if !req.Network.IsZero() {
		for _, name := range req.Network.Patch.Unset {
			if !networkProxyName(name) {
				return agentic.ErrNetworkConfigurationConflict
			}
		}
		for _, pair := range req.Network.Patch.Set {
			if !networkProxyName(pair.Name) {
				return agentic.ErrNetworkConfigurationConflict
			}
		}
	}
	return nil
}
func networkProxyName(name string) bool {
	for _, proxy := range envpatch.UnsetNames() {
		if strings.EqualFold(name, proxy) {
			return true
		}
	}
	return false
}

func networkChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	if err := admitNetwork(req); err != nil {
		return nil, err
	}
	env := dedupeEnv(childEnv(parent, req))
	keyBytes, _ := json.Marshal(struct {
		Env                 []string
		WorkDir, PromptPath string
		Patch               envpatch.Patch
		Run                 agentic.RunContext
	}{req.Env, req.WorkDir, req.PromptPath, req.Network.Patch, req.Run})
	key := fmt.Sprintf("%x", sha256.Sum256(keyBytes))
	// The owned snapshot reads the already-pinned projection, not mutable source
	// settings a second time. Reusing the same request with changed config refuses.
	networkConfigs.Lock()
	pinned := networkConfigs.requests[key]
	networkConfigs.Unlock()
	if parent == nil && pinned != "" {
		return agentic.SetEnvValue(env, "XDG_CONFIG_HOME", pinned), nil
	}

	// Inventory from the SAME ChildEnv construction and patch the process owner
	// uses, never from ambient os.Getenv or a first-wins mirror of parent env.
	inventoryEnv := req.Network.Patch.Apply(dedupeEnv(childEnv(req.Env, req)))
	var root string
	if value, err := networkConfig(inventoryEnv, req); err != nil {
		return nil, err
	} else {
		root = value
	}
	networkConfigs.Lock()
	defer networkConfigs.Unlock()
	if pinned := networkConfigs.requests[key]; pinned != "" && pinned != root {
		return nil, networkRefusal("config source changed for the same launch request")
	}
	networkConfigs.requests[key] = root
	return agentic.SetEnvValue(env, "XDG_CONFIG_HOME", root), nil
}

func dedupeEnv(env []string) []string {
	var out []string
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			out = agentic.SetEnvValue(out, key, value)
		}
	}
	return out
}

// envValue reads the LAST name=value entry, the value the exec'd child
// resolves. Go's launcher dedupes key=value pairs last-wins, so a
// first-wins read splits the planner from the child across duplicate keys.
// Every selector, updater-pin, home and config-location read in this adapter
// goes through this one helper.
func envValue(env []string, key string) string {
	value, _ := launchenv.LookupEffective(env, key)
	return value
}

// isNetworkSensitiveSelector reports whether name selects state this adapter
// relies on: PATH, HOME, SHELL, the updater pin, or any XDG_ directory. A late
// duplicate of one of these reaches the child last-wins, so the seal must
// refuse a final env carrying more than one entry for them, whoever appended
// it, even when the duplicate repeats the sealed value.
func isNetworkSensitiveSelector(name string) bool {
	if name == "PATH" || name == "HOME" || name == "SHELL" || name == museNoAutoUpdateEnv {
		return true
	}
	return strings.HasPrefix(name, "XDG_")
}

func hasDuplicateSensitiveSelector(env []string) bool {
	seen := map[string]bool{}
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !isNetworkSensitiveSelector(name) {
			continue
		}
		if seen[name] {
			return true
		}
		seen[name] = true
	}
	return false
}

// Other config sources cannot be overlaid by an exec flag on the pinned
// builds. Refuse them rather than mutating a workspace, HOME or shared files.
func networkConfig(env []string, req agentic.LaunchRequest) (string, error) {
	configHome := envValue(env, "XDG_CONFIG_HOME")
	if configHome == "" {
		if home := envValue(env, "HOME"); filepath.IsAbs(home) {
			configHome = filepath.Join(home, ".config")
		}
	}
	if !filepath.IsAbs(configHome) {
		return "", networkRefusal("cannot establish an absolute config home")
	}
	source := filepath.Join(configHome, "muse")
	dataHome := envValue(env, "XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(envValue(env, "HOME"), ".local", "share")
	}
	if !filepath.IsAbs(dataHome) || !filepath.IsAbs(req.WorkDir) {
		return "", networkRefusal("cannot inventory data home or workspace")
	}
	// Plugin entries have their own argv schema and no per-entry env. Project
	// files and enterprise hooks cannot be privately rewritten by this adapter.
	for _, path := range uncoveredConfigPaths(req.WorkDir, dataHome) {
		if err := requireAbsent(path); err != nil {
			return "", networkRefusal("uncovered project or plugin config source")
		}
	}
	data, err := readConfig(filepath.Join(source, "settings.json"))
	if err != nil {
		return "", networkRefusal("settings read failed")
	}
	settings := map[string]any{}
	if data != nil {
		if err := decodeUniqueJSON(data, &settings); err != nil || settings == nil {
			return "", networkRefusal("ambiguous or malformed settings")
		}
	}
	// The known pinned flat schema is preserved; alternate MCP/hook sources
	// and spellings cannot silently bypass inventory.
	for key := range settings {
		if strings.Contains(strings.ToLower(key), "mcp") && key != "mcpServers" && key != "mcp_oauth_dynamic_client_registration" || strings.Contains(strings.ToLower(key), "hook") && key != "hooks" && key != "managed_hooks_path" && key != "managed_hooks_env_vars" {
			return "", networkRefusal("unknown MCP or hook settings member")
		}
	}
	if err := injectServers(settings, req.Network.Patch); err != nil {
		return "", networkRefusal(err.Error())
	}
	if value, ok := settings["hooks"]; ok {
		if err := wrapHooks(value, req.Network.Patch, envValue(env, "SHELL")); err != nil {
			return "", networkRefusal(err.Error())
		}
	}
	managedSource := ""
	if value, ok := settings["managed_hooks_path"]; ok {
		path, ok := value.(string)
		if !ok || path == "" {
			return "", networkRefusal("unknown managed hook path shape")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(source, path)
		}
		managedSource = path
		data, err := readConfig(path)
		if err != nil || data == nil {
			return "", networkRefusal("managed hook read failed")
		}
		var managed map[string]any
		if err := decodeUniqueJSON(data, &managed); err != nil || len(managed) != 1 {
			return "", networkRefusal("unknown managed hook document")
		}
		if err := wrapHooks(managed["hooks"], req.Network.Patch, envValue(env, "SHELL")); err != nil {
			return "", networkRefusal(err.Error())
		}
		// materializeNetworkConfig replaces this internal value with a private path.
		settings["managed_hooks_path"] = managed
	}
	rendered, err := json.Marshal(settings)
	if err != nil {
		return "", networkRefusal("cannot encode private settings")
	}
	root, err := materializeNetworkConfig(source, rendered, settings, managedSource)
	if err != nil {
		return "", networkRefusal("cannot materialize private config")
	}
	return root, nil
}

func networkRefusal(detail string) error {
	return fmt.Errorf("muse-env-v1: %s: %w", detail, agentic.ErrNetworkScopeUnsupported)
}

// Muse discovers project sources above a nested workspace too. Inventory both
// lexical and resolved ancestry; links to uncovered sources refuse as present.
func uncoveredConfigPaths(workDir, dataHome string) []string {
	paths := []string{filepath.Join(dataHome, "muse", "plugins")}
	roots := []string{workDir}
	if real, err := filepath.EvalSymlinks(workDir); err == nil && real != workDir {
		roots = append(roots, real)
	}
	for _, root := range roots {
		for dir := filepath.Clean(root); ; dir = filepath.Dir(dir) {
			for _, name := range []string{".mcp.json", filepath.Join(".muse", "hooks.json"), filepath.Join(".muse", "settings.json"), filepath.Join(".muse", "plugins")} {
				paths = append(paths, filepath.Join(dir, name))
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return paths
}

func requireAbsent(path string) error {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("uncovered config source")
}
func readConfig(path string) ([]byte, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		if _, linkErr := os.Lstat(path); os.IsNotExist(linkErr) {
			return nil, nil
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular config")
	}
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("config too large")
	}
	return data, err
}

// JSON's ordinary last-wins decoding would inventory only part of an
// ambiguous document. Validate duplicate keys recursively before decoding.
func decodeUniqueJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for decoder.More() {
					token, err := decoder.Token()
					if err != nil {
						return err
					}
					key := token.(string)
					if seen[key] {
						return fmt.Errorf("duplicate JSON key")
					}
					seen[key] = true
					if err := visit(); err != nil {
						return err
					}
				}
			case '[':
				for decoder.More() {
					if err := visit(); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("unexpected JSON delimiter")
			}
			_, err = decoder.Token()
			return err
		}
		return nil
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return json.Unmarshal(data, target)
}

func injectServers(settings map[string]any, patch envpatch.Patch) error {
	value, present := settings["mcpServers"]
	if !present {
		return nil
	}
	servers, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("unknown MCP root shape")
	}
	var entries []agentic.MCPServerEnv
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		server, ok := servers[name].(map[string]any)
		if !ok {
			return fmt.Errorf("unknown MCP entry shape")
		}
		for key := range server {
			switch key {
			case "type", "command", "args", "env", "url", "headers", "cwd", "required", "disabled", "startup_timeout_sec", "tool_timeout_sec":
			default:
				return fmt.Errorf("unknown MCP entry member")
			}
		}
		transport, _ := server["type"].(string)
		if transport == "" {
			if _, exists := server["type"]; exists {
				return fmt.Errorf("unknown MCP transport")
			}
			transport = "stdio"
		}
		if transport != "stdio" && transport != "http" {
			return fmt.Errorf("unknown MCP transport")
		}
		command, commandOK := server["command"].(string)
		if transport == "stdio" {
			if !commandOK || strings.TrimSpace(command) == "" || server["url"] != nil || server["headers"] != nil {
				return fmt.Errorf("ambiguous MCP stdio shape")
			}
		} else {
			url, ok := server["url"].(string)
			if !ok || url == "" || server["command"] != nil || server["args"] != nil || server["env"] != nil {
				return fmt.Errorf("ambiguous MCP HTTP shape")
			}
			continue
		}
		if args, present := server["args"]; present {
			values, ok := args.([]any)
			if !ok {
				return fmt.Errorf("unknown MCP args shape")
			}
			for _, value := range values {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("unknown MCP argument")
				}
			}
		}
		var pairs []envpatch.Pair
		if value, present := server["env"]; present {
			env, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("unknown MCP env shape")
			}
			for key, value := range env {
				text, ok := value.(string)
				if !ok {
					return fmt.Errorf("unknown MCP env value")
				}
				pairs = append(pairs, envpatch.Pair{Name: key, Value: text})
			}
		}
		entries = append(entries, agentic.MCPServerEnv{Server: name, Env: pairs})
	}
	for _, entry := range agentic.ApplyNetworkPatchToServerEnvs(patch, entries) {
		env := map[string]any{}
		for _, pair := range entry.Env {
			env[pair.Name] = pair.Value
		}
		servers[entry.Server].(map[string]any)["env"] = env
	}
	return nil
}
func wrapHooks(value any, patch envpatch.Patch, shell string) error {
	if shell == "" {
		shell = "/bin/sh"
	}
	if !filepath.IsAbs(shell) {
		return fmt.Errorf("unknown hook shell")
	}
	events, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("unknown hook root shape")
	}
	for event, value := range events {
		switch event {
		case "SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "PreLLMCall", "PostLLMCall", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop", "Stop", "SessionEnd", "Notification", "PostToolUseFailure", "StopFailure", "PostToolBatch":
		default:
			return fmt.Errorf("unknown hook event")
		}

		groups, ok := value.([]any)
		if !ok {
			return fmt.Errorf("unknown hook event shape")
		}
		for _, value := range groups {
			group, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("unknown hook group")
			}
			for key := range group {
				if key != "matcher" && key != "hooks" {
					return fmt.Errorf("unknown hook group member")
				}
			}
			if matcher, present := group["matcher"]; present {
				if _, ok := matcher.(string); !ok {
					return fmt.Errorf("unknown hook matcher")
				}
			}
			handlers, ok := group["hooks"].([]any)
			if !ok {
				return fmt.Errorf("unknown hook handlers")
			}
			for _, value := range handlers {
				hook, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("unknown hook handler")
				}
				for key := range hook {
					switch key {
					case "type", "command", "timeout", "async", "statusMessage":
					default:
						return fmt.Errorf("unknown hook handler member")
					}
				}
				if value, present := hook["timeout"]; present {
					timeout, ok := value.(float64)
					if !ok || timeout < 0 || timeout != float64(int64(timeout)) {
						return fmt.Errorf("unknown hook timeout")
					}
				}
				if value, present := hook["async"]; present {
					if _, ok := value.(bool); !ok {
						return fmt.Errorf("unknown hook async shape")
					}
				}
				if value, present := hook["statusMessage"]; present {
					if _, ok := value.(string); !ok {
						return fmt.Errorf("unknown hook status message")
					}
				}
				command, ok := hook["command"].(string)
				if !ok || command == "" || hook["type"] != "command" {
					return fmt.Errorf("unknown hook command shape")
				}
				// A shell string may contain pipelines, lists, builtins, or redirects.
				// Wrapping a shell invocation carries the env to the ENTIRE command.
				prefix := "env"
				for _, pair := range patch.Set {
					prefix += " " + shellQuote(pair.Name+"="+pair.Value)
				}
				hook["command"] = prefix + " " + shellQuote(shell) + " -c " + shellQuote(command)
			}
		}
	}
	return nil
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// hasDuplicatePath refuses ambiguous binary selection before ResolveBinary can
// inspect the filesystem. Other initial selectors retain childEnv's last-wins
// projection; all sensitive duplicates are refused on the sealed final env.
func hasDuplicatePath(env []string) bool {
	seen := false
	for _, entry := range env {
		if !strings.HasPrefix(entry, "PATH=") {
			continue
		}
		if seen {
			return true
		}
		seen = true
	}
	return false
}
