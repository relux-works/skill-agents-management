package agentic_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"github.com/relux-works/skill-agents-management/internal/mcpjson"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Frozen launcher reference for parity. This is a transcription of the
// curator-agent-launcher boundary the adapter ports, kept in launcher shape
// (separate intent, launcher channel vocabulary) so the parity test executes
// both spellings and compares verdicts:
//
//   - internal/execution/execution.go:161-173 (Run order) and :202-233
//     (executable), plus the :166 exec_provider_missing wrap.
//   - internal/composition/composition.go:94-97 (codex-only layer gating)
//     and internal/composition/probe.go:9-64 (codes, LayerError,
//     CheckLaunchBoundary).
//   - internal/systemprompt/systemprompt.go:15-27 (codes, Refusal),
//     :44-76 (Select), :87-106 (ProbeFiles), :108-138 (readable),
//     :151-166 (PrepareLaunch), :172-198 (FormatWarnings).
//   - cmd/curator-run/main.go:300-309 (Boundary binds PrepareLaunch).
//
// Launcher @ fbcdbaf07efe6fc7a8b4929e1c709028345021da; the ported files are
// byte-identical to the contract-cited 2517d2753945d0a8b0c40a291e4aef883a7c16ee.
// Fixtures below are copied from the launcher's boundary tests, not imported:
// composition_test.go (TestLaunchBoundaryFilesystem,
// TestLaunchBoundaryLateReplacement, TestLaunchBoundaryUnengaged),
// execution_test.go (TestLateChecksBothModes, TestPrelaunchCheckOrder,
// TestEmptyEnvironmentAndLaunchPATH), systemprompt_test.go
// (TestSelectRefusalsAndNoOptIn, TestPrepareLaunchLateFilesAndWarnings,
// TestPrepareLaunchFileFailures, TestProbeParentFailures,
// TestPrepareLaunchSelectedPathChangesLate).
type refLBLayerError struct {
	Code string
	Path string
	Err  error
}

func (e *refLBLayerError) Error() string { return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.Err) }
func (e *refLBLayerError) Unwrap() error { return e.Err }

type refLBRefusal struct {
	Code, Path string
	Cause      error
}

func (e *refLBRefusal) Error() string { return fmt.Sprintf("%s: %q: %v", e.Code, e.Path, e.Cause) }
func (e *refLBRefusal) Unwrap() error { return e.Cause }

type refLBChannel struct {
	Kind      string
	Semantics string
	Flag      string
	Argument  string
	With      []string
}

type refLBFragment struct {
	Environment  string
	ProfileName  string
	Home         string
	MCPLayer     string // engaged layer path; "" = unengaged (codex only in the caller)
	SystemPrompt *refLBSystemPrompt
}

type refLBSystemPrompt struct {
	Path     string
	Channels []refLBChannel
}

type refLBSelection struct {
	channel refLBChannel
	has     bool
	path    string
}

func refLBExecutable(binary, workdir string, env []string) (string, error) {
	check := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(workdir, path)
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", os.ErrPermission
		}
		return path, nil
	}
	if strings.ContainsRune(binary, '/') {
		return check(binary)
	}
	for _, entry := range env {
		if path, ok := strings.CutPrefix(entry, "PATH="); ok {
			for _, dir := range filepath.SplitList(path) {
				if found, err := check(filepath.Join(dir, binary)); err == nil {
					return found, nil
				}
			}
		}
	}
	return "", exec.ErrNotFound
}

func refLBCheckMCPLayer(environment, layer string) error {
	if environment != "codex_cli" || layer == "" {
		return nil
	}
	path := layer
	fail := func(code string, err error) error { return &refLBLayerError{Code: code, Path: path, Err: err} }
	_, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fail("mcp_layer_missing", err)
		}
		return fail("mcp_layer_unreadable", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail("mcp_layer_unreadable", err)
	}
	if !info.Mode().IsRegular() {
		return fail("mcp_layer_unreadable", errors.New("not a regular file"))
	}
	f, err := os.Open(path)
	if err != nil {
		return fail("mcp_layer_unreadable", err)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return fail("mcp_layer_unreadable", err)
	}
	if !info.Mode().IsRegular() {
		return fail("mcp_layer_unreadable", errors.New("opened file is not regular"))
	}
	return nil
}

func refLBSelect(f *refLBFragment, opt string) (refLBSelection, error) {
	if opt == "" {
		return refLBSelection{}, nil
	}
	if f != nil && f.SystemPrompt != nil && (opt == "append" || opt == "replace") {
		for _, c := range f.SystemPrompt.Channels {
			if c.Semantics != opt || c.Kind == "file" {
				continue
			}
			s := refLBSelection{channel: c, has: true, path: f.SystemPrompt.Path}
			switch c.Kind {
			case "flag":
				if c.Argument != "path" {
					continue
				}
			case "config-key":
				if f.Environment != "codex_cli" {
					continue
				}
			default:
				continue
			}
			return s, nil
		}
	}
	return refLBSelection{}, &refLBRefusal{Code: "sysprompt_channel_unavailable", Cause: fmt.Errorf("no non-file system-prompt channel for %q", opt)}
}

type refLBObservation struct {
	Filename  string
	Semantics string
}

func refLBProbeFiles(f *refLBFragment) ([]refLBObservation, error) {
	if f.Environment != "pi" {
		return nil, nil
	}
	files := []refLBObservation{
		{Filename: "APPEND_SYSTEM.md", Semantics: "append"},
		{Filename: "SYSTEM.md", Semantics: "replace"},
	}
	var observed []refLBObservation
	for _, file := range files {
		present, err := refLBReadable(filepath.Join(f.Home, file.Filename), true)
		if err != nil {
			return nil, err
		}
		if present {
			observed = append(observed, file)
		}
	}
	return observed, nil
}

func refLBReadable(path string, allowAbsent bool) (bool, error) {
	fail := func(err error) (bool, error) {
		return false, &refLBRefusal{Code: "sysprompt_file_unreadable", Path: path, Cause: err}
	}
	_, err := os.Lstat(path)
	if err != nil {
		if allowAbsent && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return fail(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(fmt.Errorf("not a regular file"))
	}
	file, err := os.Open(path)
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(fmt.Errorf("opened object is not a regular file"))
	}
	return true, nil
}

const refLBCacheWarning = " A custom system prefix can change request caching and billing: the default may use shared prompt caching; a custom prefix forms its own cache prefix."

func refLBFormatWarnings(profile string, s refLBSelection, observed []refLBObservation) []string {
	var lines []string
	if s.has && s.channel.Kind != "" {
		line := fmt.Sprintf("warning: profile %q: applied %s/%s system-prompt channel", profile, s.channel.Kind, s.channel.Semantics)
		if s.channel.Kind == "flag" {
			line += fmt.Sprintf(" %q", s.channel.Flag)
		}
		line += "."
		if s.channel.Semantics == "replace" {
			line += " Replacement discards the tool's built-in system behavior entirely."
		}
		lines = append(lines, line+refLBCacheWarning)
	}
	for _, file := range observed {
		line := fmt.Sprintf("warning: profile %q: observed file/%s %q in managed home; ", profile, file.Semantics, file.Filename)
		if s.has && s.channel.Kind == "flag" && s.channel.Semantics == file.Semantics {
			line += fmt.Sprintf("discovery suppressed by applied launcher flag %q.", s.channel.Flag)
		} else {
			line += "conditional native-discovery candidate, subject to native flags and trusted-project precedence."
			if file.Semantics == "replace" {
				line += " If selected, replacement discards the tool's built-in system behavior entirely."
			}
		}
		lines = append(lines, line+refLBCacheWarning)
	}
	return lines
}

func refLBPrepareLaunch(f *refLBFragment, opt string) ([]string, error) {
	s, err := refLBSelect(f, opt)
	if err != nil {
		return nil, err
	}
	if f.Environment == "pi" && s.has && s.channel.Kind == "flag" {
		if _, err = refLBReadable(s.path, false); err != nil {
			return nil, err
		}
	}
	observed, err := refLBProbeFiles(f)
	if err != nil {
		return nil, err
	}
	return refLBFormatWarnings(f.ProfileName, s, observed), nil
}

// refLBRun executes the launcher's Run boundary order over the reference
// functions: binary, then MCP layer, then the prompt boundary.
func refLBRun(binary, workdir string, env []string, f *refLBFragment, opt string) ([]string, error) {
	if _, err := refLBExecutable(binary, workdir, env); err != nil {
		return nil, fmt.Errorf("exec_provider_missing: %s: install the provider executable and make it available on the launch PATH: %w", binary, err)
	}
	if err := refLBCheckMCPLayer(f.Environment, f.MCPLayer); err != nil {
		return nil, err
	}
	return refLBPrepareLaunch(f, opt)
}

// lbOracleMCPConfigPath is the admission oracle for the fragment mcp.path: a
// transcription of the launcher's registry gate (fragment.go:474-478) and
// absolutePath rule (fragment.go:316-326) with InvalidError rendering
// (fragment.go:222-227). It cannot import the launcher's internal package,
// so every row using it also pins the exact bytes literally (wantMessage);
// the oracle checks the branch, the literal anchors the bytes.
func lbOracleMCPConfigPath(environment, path string) error {
	if environment != "claude_code" && environment != "codex_cli" && environment != "opencode" {
		return fmt.Errorf("fragment: /mcp: %s has no MCP channel", environment)
	}
	if n := utf8.RuneCountInString(path); n < 2 || n > 4096 || path[0] != '/' || strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("fragment: /mcp/path: %q is not an absolute path", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return fmt.Errorf("fragment: /mcp/path: %q contains a .. segment", path)
		}
	}
	return nil
}

// lbOracleAdmission runs the MCP admission oracle in launcher order: the
// fragment mcp.path check first (fragment resolve precedes the spawn-plane
// build in cmd/curator-run/main.go), then the REAL mcpjson.Validate for the
// inline document. The inline oracle is the pinned behavior owner itself,
// not a second transcription, so any adapter slip fails against it.
func lbOracleAdmission(environment string, admission *agentic.LaunchBoundaryMCPAdmission) error {
	if admission == nil {
		return nil
	}
	if admission.ConfigPathSet {
		if err := lbOracleMCPConfigPath(environment, admission.ConfigPath); err != nil {
			return err
		}
	}
	return mcpjson.Validate(agentic.Composition{Prefix: admission.Inline.Prefix, Servers: admission.Inline.Servers})
}

func lbStdioServer() agentic.CompositionServer {
	return agentic.CompositionServer{Name: "a", Transport: "stdio"}
}

func lbHTTPServer() agentic.CompositionServer {
	return agentic.CompositionServer{Name: "h", Transport: "http", BearerTokenEnvVar: "TOKEN"}
}

func lbHTTPNoTokenServer() agentic.CompositionServer {
	return agentic.CompositionServer{Name: "h", Transport: "http"}
}

func writeLBFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func makeLBExec(t *testing.T, path string) {
	t.Helper()
	writeLBFile(t, path, []byte("#!/bin/sh\nexit 0\n"), 0o755)
}

// lbCode extracts the stable diagnostic code from either spelling's error.
func lbCode(err error) string {
	if err == nil {
		return ""
	}
	var binary *agentic.LaunchBoundaryBinaryError
	if errors.As(err, &binary) {
		return binary.Code()
	}
	var mcp *agentic.LaunchBoundaryMCPError
	if errors.As(err, &mcp) {
		return mcp.Code
	}
	var prompt *agentic.LaunchBoundaryPromptError
	if errors.As(err, &prompt) {
		return prompt.Code
	}
	var layer *refLBLayerError
	if errors.As(err, &layer) {
		return layer.Code
	}
	var refusal *refLBRefusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	if code, _, ok := strings.Cut(err.Error(), ": "); ok {
		return code
	}
	return err.Error()
}

// lbChannels converts one channel spelling to the other. Both vocabularies
// spell kinds, semantics, flags and arguments identically.
func lbChannels(channels []refLBChannel) []agentic.CuratorChannelDescriptor {
	out := make([]agentic.CuratorChannelDescriptor, 0, len(channels))
	for _, c := range channels {
		out = append(out, agentic.CuratorChannelDescriptor{
			Kind:      agentic.CuratorDescriptorKind(c.Kind),
			Semantics: agentic.CuratorSystemPromptIntent(c.Semantics),
			Flag:      c.Flag,
			Argument:  agentic.CuratorFlagArgument(c.Argument),
			With:      append([]string(nil), c.With...),
		})
	}
	return out
}

func lbClaudeChannels() []refLBChannel {
	return []refLBChannel{
		{Kind: "flag", Semantics: "append", Flag: "--append-system-prompt-file", Argument: "path"},
		{Kind: "flag", Semantics: "replace", Flag: "--system-prompt-file", Argument: "path"},
	}
}

func lbCodexChannels() []refLBChannel {
	return []refLBChannel{
		{Kind: "config-key", Semantics: "replace"},
	}
}

func lbPiChannels() []refLBChannel {
	return []refLBChannel{
		{Kind: "flag", Semantics: "append", Flag: "--append-system-prompt", Argument: "path"},
		{Kind: "file", Semantics: "append"},
		{Kind: "file", Semantics: "replace"},
	}
}

// lbParityCase drives one boundary situation through both the module adapter
// and the frozen launcher reference and requires the same verdict.
type lbParityCase struct {
	name         string
	binary       string // resolved per-case; "" selects the default executable
	env          []string
	environment  string
	mcpPath      string // "" = unengaged; "fixture" = per-case file below
	promptOpt    string
	promptPath   string // "" = per-case default under home
	section      bool
	channels     []refLBChannel
	prepare      func(t *testing.T, root, home string) (binary, mcp, prompt string)
	remapHome    func(t *testing.T, root, home string) string
	wantCode     string
	wantWarnings int
	// Admission inputs. admissionPathSet marks the mcp section present;
	// admissionPath starting with "$ROOT/" expands against the per-case
	// root. Any admission field set builds a non-nil MCPAdmission, which
	// routes the row through the byte-exact admission gate.
	admissionPathSet bool
	admissionPath    string
	inlinePrefix     []string
	inlineServers    []agentic.CompositionServer
	// wantMessage pins the exact Error() bytes. It is required for every
	// admission refusal (the launcher frames no code there) and set for
	// the late refusals whose bytes carry no OS error text.
	wantMessage string
}

func TestLaunchBoundaryParityWithLauncher(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)

	lbValidStdioDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "serve"}}}`
	lbValidHTTPDoc := `{"mcpServers": {"h": {"type": "http", "url": "https://m.example/s", "headers": {"Authorization": "Bearer ${TOKEN}"}}}}`
	lbDupKeysDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "x"}, "a": {"type": "stdio", "command": "y"}}}`
	lbDupFieldDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "x", "command": "y"}}}`
	lbOversizeDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "` + strings.Repeat("y", 1<<20) + `"}}}`
	lbUnknownFieldDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "x", "zzz": 1}}}`
	lbUnknownServerDoc := `{"mcpServers": {"b": {"type": "stdio", "command": "x"}}}`
	lbTypeMismatchDoc := `{"mcpServers": {"a": {"type": "http", "url": "https://x.example"}}}`
	lbHTTPNoURLDoc := `{"mcpServers": {"h": {"type": "http"}}}`
	lbHTTPHeadersNoTokenDoc := `{"mcpServers": {"h": {"type": "http", "url": "https://x.example", "headers": {"X": "y"}}}}`
	lbHTTPBadTokenDoc := `{"mcpServers": {"h": {"type": "http", "url": "https://x.example", "headers": {"Authorization": "Bearer ${WRONG}"}}}}`
	lbStdioURLDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "x", "url": "https://y.example"}}}`
	lbStdioHeadersDoc := `{"mcpServers": {"a": {"type": "stdio", "command": "x", "headers": {"A": "b"}}}}`
	lbOverlongPath := "/" + strings.Repeat("x", 4096) // 4097 runes: over the 4096 limit
	lbMaxPath := "/" + strings.Repeat("x", 4095)      // 4096 runes: exactly the limit
	lbOverlongMessage := fmt.Sprintf("fragment: /mcp/path: %q is not an absolute path", lbOverlongPath)

	cases := []lbParityCase{
		{
			name: "binary/absolute-ok", environment: "pi",
			wantCode: "", wantWarnings: 0,
		},
		{
			name: "binary/bare-name-on-PATH", environment: "pi",
			env: []string{"PATH=" + dir}, wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return "provider", "", ""
			},
		},
		{
			name: "binary/relative-ok", environment: "pi",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				link := filepath.Join(root, "local-provider")
				if err := os.Symlink(provider, link); err != nil {
					t.Fatal(err)
				}
				return "./local-provider", "", ""
			},
		},
		{
			name: "binary/empty-env-absolute-ok", environment: "pi",
			env: []string{}, wantCode: "", wantWarnings: 0,
		},
		{
			name: "binary/missing", environment: "pi",
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), "", ""
			},
		},
		{
			name: "binary/nonexec", environment: "pi",
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "plain-file")
				writeLBFile(t, path, []byte("not executable"), 0o600)
				return path, "", ""
			},
		},
		{
			name: "binary/bare-name-missing-from-PATH", environment: "pi",
			env: []string{"PATH=" + dir}, wantCode: "exec_provider_missing", wantWarnings: 0,
			wantMessage: "exec_provider_missing: fake-provider-not-on-PATH: install the provider executable and make it available on the launch PATH: executable file not found in $PATH",
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return "fake-provider-not-on-PATH", "", ""
			},
		},
		{
			name: "binary/directory", environment: "pi",
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "provider-dir")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return path, "", ""
			},
		},
		{
			name: "mcp/codex-regular", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "layer.toml")
				writeLBFile(t, path, []byte("mcp"), 0o600)
				return "", path, ""
			},
		},
		{
			name: "mcp/codex-symlink", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				target := filepath.Join(root, "target.toml")
				writeLBFile(t, target, []byte("mcp"), 0o600)
				path := filepath.Join(root, "layer.toml")
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				return "", path, ""
			},
		},
		{
			name: "mcp/codex-missing", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "mcp_layer_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return "", filepath.Join(root, "absent-layer.toml"), ""
			},
		},
		{
			name: "mcp/codex-directory", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "mcp_layer_unreadable", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "layer.toml")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return "", path, ""
			},
		},
		{
			name: "mcp/codex-dangling", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "mcp_layer_unreadable", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "layer.toml")
				if err := os.Symlink(filepath.Join(root, "absent-target"), path); err != nil {
					t.Fatal(err)
				}
				return "", path, ""
			},
		},
		{
			name: "mcp/codex-parent-file", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "mcp_layer_unreadable", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				parent := filepath.Join(root, "parent")
				writeLBFile(t, parent, []byte("mcp"), 0o600)
				return "", filepath.Join(parent, "child"), ""
			},
		},
		{
			name: "mcp/codex-unengaged-empty", environment: "codex_cli",
			wantCode: "", wantWarnings: 0,
		},
		{
			name: "mcp/claude-path-unprobed", environment: "claude_code", mcpPath: "fixture",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				// Only codex engages a layer; a claude MCP path, even
				// absent, is never probed at the boundary.
				return "", filepath.Join(root, "absent-claude-mcp.json"), ""
			},
		},
		{
			name: "prompt/pi-no-optin-no-files", environment: "pi",
			wantCode: "", wantWarnings: 0,
		},
		{
			name: "prompt/pi-no-optin-both-files", environment: "pi",
			wantCode: "", wantWarnings: 2,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				writeLBFile(t, filepath.Join(home, "APPEND_SYSTEM.md"), []byte("a"), 0o600)
				writeLBFile(t, filepath.Join(home, "SYSTEM.md"), []byte("s"), 0o600)
				return "", "", ""
			},
		},
		{
			name: "prompt/pi-append-selected", environment: "pi",
			promptOpt: "append", section: true, channels: lbPiChannels(),
			wantCode: "", wantWarnings: 1,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(home, "prompt.md")
				writeLBFile(t, path, []byte("prompt"), 0o600)
				return "", "", path
			},
		},
		{
			name: "prompt/pi-append-selected-with-files", environment: "pi",
			promptOpt: "append", section: true, channels: lbPiChannels(),
			wantCode: "", wantWarnings: 3,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(home, "prompt.md")
				writeLBFile(t, path, []byte("prompt"), 0o600)
				writeLBFile(t, filepath.Join(home, "APPEND_SYSTEM.md"), []byte("a"), 0o600)
				writeLBFile(t, filepath.Join(home, "SYSTEM.md"), []byte("s"), 0o600)
				return "", "", path
			},
		},
		{
			name: "prompt/pi-append-selected-missing", environment: "pi",
			promptOpt: "append", section: true, channels: lbPiChannels(),
			wantCode: "sysprompt_file_unreadable", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return "", "", filepath.Join(home, "absent-prompt.md")
			},
		},
		{
			name: "prompt/pi-append-selected-directory", environment: "pi",
			promptOpt: "append", section: true, channels: lbPiChannels(),
			wantCode: "sysprompt_file_unreadable", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(home, "prompt.md")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return "", "", path
			},
		},
		{
			name: "prompt/pi-append-no-section", environment: "pi",
			promptOpt: "append", section: false,
			wantCode: "sysprompt_channel_unavailable", wantWarnings: 0,
			wantMessage: `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "append"`,
		},
		{
			name: "prompt/pi-replace-no-flag-channel", environment: "pi",
			promptOpt: "replace", section: true, channels: lbPiChannels(),
			wantCode: "sysprompt_channel_unavailable", wantWarnings: 0,
			wantMessage: `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "replace"`,
		},
		{
			name: "prompt/pi-bogus-intent", environment: "pi",
			promptOpt: "bogus", section: true, channels: lbPiChannels(),
			wantCode: "sysprompt_channel_unavailable", wantWarnings: 0,
			wantMessage: `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "bogus"`,
		},
		{
			name: "prompt/claude-append", environment: "claude_code",
			promptOpt: "append", section: true, channels: lbClaudeChannels(),
			wantCode: "", wantWarnings: 1,
		},
		{
			name: "prompt/claude-replace", environment: "claude_code",
			promptOpt: "replace", section: true, channels: lbClaudeChannels(),
			wantCode: "", wantWarnings: 1,
		},
		{
			name: "prompt/claude-append-no-section", environment: "claude_code",
			promptOpt: "append", section: false,
			wantCode: "sysprompt_channel_unavailable", wantWarnings: 0,
			wantMessage: `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "append"`,
		},
		{
			name: "prompt/codex-replace", environment: "codex_cli",
			promptOpt: "replace", section: true, channels: lbCodexChannels(),
			wantCode: "", wantWarnings: 1,
		},
		{
			name: "prompt/codex-append-no-channel", environment: "codex_cli",
			promptOpt: "append", section: true, channels: lbCodexChannels(),
			wantCode: "sysprompt_channel_unavailable", wantWarnings: 0,
			wantMessage: `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "append"`,
		},
		{
			name: "prompt/opencode-no-probe", environment: "opencode",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				// Non-Pi environments never probe managed-home files.
				writeLBFile(t, filepath.Join(home, "APPEND_SYSTEM.md"), []byte("a"), 0o600)
				return "", "", ""
			},
		},
		{
			// Mirrors the launcher's TestProbeParentFailures/non-directory:
			// a managed home that is not a directory refuses the probe.
			name: "prompt/pi-home-is-file", environment: "pi",
			wantCode: "sysprompt_file_unreadable", wantWarnings: 0,
			remapHome: func(t *testing.T, root, home string) string {
				fileHome := filepath.Join(root, "home-file")
				writeLBFile(t, fileHome, []byte("not a directory"), 0o600)
				return fileHome
			},
		},
		{
			name: "order/all-three-fail", environment: "codex_cli", mcpPath: "fixture",
			promptOpt: "append", section: false,
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), filepath.Join(root, "absent-layer.toml"), ""
			},
		},
		{
			name: "order/mcp-before-prompt", environment: "codex_cli", mcpPath: "fixture",
			promptOpt: "append", section: false,
			wantCode: "mcp_layer_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return "", filepath.Join(root, "absent-layer.toml"), ""
			},
		},
		{
			name: "order/binary-before-prompt", environment: "pi",
			promptOpt: "append", section: false,
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), "", ""
			},
		},
		{
			name: "order/binary-before-mcp", environment: "codex_cli", mcpPath: "fixture",
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), filepath.Join(root, "absent-layer.toml"), ""
			},
		},
		{
			name: "order/prompt-last", environment: "pi",
			promptOpt: "append", section: false,
			wantCode: "sysprompt_channel_unavailable", wantWarnings: 0,
			wantMessage: `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "append"`,
		},
		{
			name: "admission/inline-stdio-ok", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbValidStdioDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantCode:      "", wantWarnings: 0,
		},
		{
			name: "admission/inline-http-ok", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbValidHTTPDoc},
			inlineServers: []agentic.CompositionServer{lbHTTPServer()},
			wantCode:      "", wantWarnings: 0,
		},
		{
			name: "admission/inline-malformed", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", `{"mcpServers": {oops`},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP config root",
		},
		{
			// Duplicate object keys: encoding/json last-wins, so the
			// grammar accepts. Pinned accept/accept with a follow-up.
			name: "admission/inline-duplicate-keys", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbDupKeysDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantCode:      "", wantWarnings: 0,
		},
		{
			// Duplicate fields inside one entry: last wins as well.
			name: "admission/inline-duplicate-entry-field", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbDupFieldDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantCode:      "", wantWarnings: 0,
		},
		{
			// The grammar enforces no document size limit. Pinned
			// accept/accept with a follow-up.
			name: "admission/inline-oversize", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbOversizeDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantCode:      "", wantWarnings: 0,
		},
		{
			name: "admission/inline-empty-doc", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", ""},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP config root",
		},
		{
			name: "admission/inline-empty-object", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", `{}`},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP config root",
		},
		{
			name: "admission/inline-null-servers", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", `{"mcpServers": null}`},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP config root",
		},
		{
			name: "admission/inline-empty-map-no-servers", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", `{"mcpServers": {}}`},
			inlineServers: []agentic.CompositionServer{},
			wantCode:      "", wantWarnings: 0,
		},
		{
			name: "admission/inline-unknown-field", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbUnknownFieldDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP entry",
		},
		{
			name: "admission/inline-trailing", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbValidStdioDoc + ` {"smuggled": true}`},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP config root",
		},
		{
			name: "admission/inline-unknown-server", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbUnknownServerDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "mcp-config-json composition references unknown MCP server",
		},
		{
			name: "admission/inline-type-mismatch", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbTypeMismatchDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP entry",
		},
		{
			name: "admission/inline-http-no-url", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbHTTPNoURLDoc},
			inlineServers: []agentic.CompositionServer{lbHTTPNoTokenServer()},
			wantMessage:   "invalid mcp-config-json HTTP MCP shape",
		},
		{
			name: "admission/inline-http-unexpected-headers", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbHTTPHeadersNoTokenDoc},
			inlineServers: []agentic.CompositionServer{lbHTTPNoTokenServer()},
			wantMessage:   "unexpected mcp-config-json HTTP headers",
		},
		{
			name: "admission/inline-http-credential-reference", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbHTTPBadTokenDoc},
			inlineServers: []agentic.CompositionServer{lbHTTPServer()},
			wantMessage:   "invalid mcp-config-json bearer environment reference",
		},
		{
			name: "admission/inline-stdio-with-url", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbStdioURLDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json stdio MCP shape",
		},
		{
			name: "admission/inline-stdio-with-headers", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbStdioHeadersDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json stdio MCP shape",
		},
		{
			name: "admission/inline-blank-server-name", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbValidStdioDoc},
			inlineServers: []agentic.CompositionServer{{Name: "  ", Transport: "stdio"}},
			wantMessage:   "mcp-config-json composition carries an unnamed MCP server",
		},
		{
			name: "admission/inline-duplicate-server-names", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbValidStdioDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer(), lbStdioServer()},
			wantMessage:   `mcp-config-json composition declares the MCP server "a" twice`,
		},
		{
			name: "admission/inline-servers-no-prefix", environment: "claude_code",
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "mcp-config-json MCP metadata has no config argument",
		},
		{
			name: "admission/inline-short-prefix", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config"},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "mcp-config-json composition must be exactly one --mcp-config pair",
		},
		{
			name: "admission/inline-wrong-flag", environment: "claude_code",
			inlinePrefix:  []string{"--other", lbValidStdioDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "mcp-config-json composition must be exactly one --mcp-config pair",
		},
		{
			name: "admission/inline-long-prefix", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", lbValidStdioDoc, "--extra"},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "mcp-config-json composition must be exactly one --mcp-config pair",
		},
		{
			name: "admission/path-relative", environment: "claude_code",
			admissionPathSet: true, admissionPath: "rel/mcp.json",
			wantMessage: `fragment: /mcp/path: "rel/mcp.json" is not an absolute path`,
		},
		{
			name: "admission/path-empty", environment: "claude_code",
			admissionPathSet: true, admissionPath: "",
			wantMessage: `fragment: /mcp/path: "" is not an absolute path`,
		},
		{
			name: "admission/path-nul", environment: "claude_code",
			admissionPathSet: true, admissionPath: "/a\x00b",
			wantMessage: "fragment: /mcp/path: \"/a\\x00b\" is not an absolute path",
		},
		{
			name: "admission/path-dotdot", environment: "claude_code",
			admissionPathSet: true, admissionPath: "/a/../b",
			wantMessage: `fragment: /mcp/path: "/a/../b" contains a .. segment`,
		},
		{
			name: "admission/path-overlong", environment: "claude_code",
			admissionPathSet: true, admissionPath: lbOverlongPath,
			wantMessage: lbOverlongMessage,
		},
		{
			name: "admission/path-max-edge", environment: "claude_code",
			admissionPathSet: true, admissionPath: lbMaxPath,
			wantCode: "", wantWarnings: 0,
		},
		{
			name: "admission/path-min-edge", environment: "claude_code",
			admissionPathSet: true, admissionPath: "/a",
			wantCode: "", wantWarnings: 0,
		},
		{
			name: "admission/path-root-only", environment: "claude_code",
			admissionPathSet: true, admissionPath: "/",
			wantMessage: `fragment: /mcp/path: "/" is not an absolute path`,
		},
		{
			name: "admission/path-pi-no-channel", environment: "pi",
			admissionPathSet: true, admissionPath: "/mcp.json",
			wantMessage: "fragment: /mcp: pi has no MCP channel",
		},
		{
			name: "admission/path-muse-no-channel", environment: "muse",
			admissionPathSet: true, admissionPath: "/mcp.json",
			wantMessage: "fragment: /mcp: muse has no MCP channel",
		},
		{
			name: "admission/path-unknown-env", environment: "bogus_env",
			admissionPathSet: true, admissionPath: "/mcp.json",
			wantMessage: "fragment: /mcp: bogus_env has no MCP channel",
		},
		{
			// Admission never stats the file: an absent path is
			// accepted here and fails only at the codex late probe.
			name: "admission/path-opencode-missing-accepted", environment: "opencode",
			admissionPathSet: true, admissionPath: "$ROOT/absent-mcp.json",
			wantCode: "", wantWarnings: 0,
		},
		{
			name: "admission/path-claude-missing-accepted", environment: "claude_code",
			admissionPathSet: true, admissionPath: "$ROOT/absent-mcp.json",
			wantCode: "", wantWarnings: 0,
		},
		{
			// Unsafe-but-readable permissions are not a refusal
			// anywhere in the launcher's MCP handling: accept/accept.
			name: "admission/path-claude-0666-accepted", environment: "claude_code",
			admissionPathSet: true, admissionPath: "$ROOT/mcp-0666.json",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "mcp-0666.json")
				writeLBFile(t, path, []byte("{}"), 0o600)
				if err := os.Chmod(path, 0o666); err != nil {
					t.Fatal(err)
				}
				return "", "", ""
			},
		},
		{
			name: "admission/path-claude-symlink-accepted", environment: "claude_code",
			admissionPathSet: true, admissionPath: "$ROOT/mcp-link.json",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				target := filepath.Join(root, "mcp-target.json")
				writeLBFile(t, target, []byte("{}"), 0o600)
				if err := os.Symlink(target, filepath.Join(root, "mcp-link.json")); err != nil {
					t.Fatal(err)
				}
				return "", "", ""
			},
		},
		{
			// The codex late probe opens the layer without any mode
			// check: a world-writable but readable layer is accepted.
			name: "admission/path-codex-0666-accepted", environment: "codex_cli", mcpPath: "fixture",
			admissionPathSet: true, admissionPath: "$ROOT/layer-0666.toml",
			wantCode: "", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				path := filepath.Join(root, "layer-0666.toml")
				writeLBFile(t, path, []byte("mcp"), 0o600)
				if err := os.Chmod(path, 0o666); err != nil {
					t.Fatal(err)
				}
				return "", path, ""
			},
		},
		{
			// A valid admission path that is absent on disk: admission
			// accepts (no stat), the codex late probe refuses missing.
			name: "admission/path-codex-missing-late-refusal", environment: "codex_cli", mcpPath: "fixture",
			admissionPathSet: true, admissionPath: "$ROOT/absent-layer.toml",
			wantCode: "mcp_layer_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return "", filepath.Join(root, "absent-layer.toml"), ""
			},
		},
		{
			name: "admission/valid-admission-bad-binary", environment: "claude_code",
			admissionPathSet: true, admissionPath: "/mcp.json",
			wantCode: "exec_provider_missing", wantWarnings: 0,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), "", ""
			},
		},
		{
			name: "admission/both-legs-accept", environment: "claude_code",
			admissionPathSet: true, admissionPath: "/mcp.json",
			inlinePrefix:  []string{"--mcp-config", lbValidStdioDoc},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantCode:      "", wantWarnings: 0,
		},
		{
			name: "admission/order-path-before-binary", environment: "claude_code",
			admissionPathSet: true, admissionPath: "rel/mcp.json",
			wantMessage: `fragment: /mcp/path: "rel/mcp.json" is not an absolute path`,
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), "", ""
			},
		},
		{
			name: "admission/order-inline-before-binary", environment: "claude_code",
			inlinePrefix:  []string{"--mcp-config", `{"mcpServers": {oops`},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   "invalid mcp-config-json MCP config root",
			prepare: func(t *testing.T, root, home string) (string, string, string) {
				return filepath.Join(root, "absent-provider"), "", ""
			},
		},
		{
			name: "admission/order-path-before-inline", environment: "claude_code",
			admissionPathSet: true, admissionPath: "rel/mcp.json",
			inlinePrefix:  []string{"--mcp-config", `{"mcpServers": {oops`},
			inlineServers: []agentic.CompositionServer{lbStdioServer()},
			wantMessage:   `fragment: /mcp/path: "rel/mcp.json" is not an absolute path`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home := t.TempDir()
			binary, mcp, prompt := provider, "", ""
			if tc.prepare != nil {
				if b, m, p := tc.prepare(t, root, home); b != "" || m != "" || p != "" || tc.binary != "" {
					if b != "" {
						binary = b
					}
					mcp, prompt = m, p
				}
			}
			if tc.binary != "" {
				binary = tc.binary
			}
			if tc.mcpPath != "" && tc.mcpPath != "fixture" {
				mcp = tc.mcpPath
			}
			if tc.promptPath != "" {
				prompt = tc.promptPath
			}
			if tc.remapHome != nil {
				home = tc.remapHome(t, root, home)
			}
			workdir := root
			if strings.HasPrefix(binary, "./") {
				workdir = root
			}
			env := tc.env
			if env == nil && !strings.Contains(tc.name, "empty-env") {
				env = []string{"PATH=/nonexistent"}
			}

			fragment := &refLBFragment{
				Environment: tc.environment, ProfileName: "test-profile",
				Home: home, MCPLayer: mcp,
			}
			if tc.section {
				fragment.SystemPrompt = &refLBSystemPrompt{Path: prompt, Channels: tc.channels}
			}
			refWarnings, refErr := refLBRun(binary, workdir, env, fragment, tc.promptOpt)

			var policyPrompt *agentic.LaunchBoundarySystemPrompt
			if tc.section {
				policyPrompt = &agentic.LaunchBoundarySystemPrompt{
					Path: prompt, Channels: lbChannels(tc.channels),
					Intent: agentic.CuratorSystemPromptIntent(tc.promptOpt),
				}
			} else if tc.promptOpt != "" {
				// Absent section with an explicit opt-in: intent with no
				// channels, which must refuse channel-unavailable.
				policyPrompt = &agentic.LaunchBoundarySystemPrompt{
					Intent: agentic.CuratorSystemPromptIntent(tc.promptOpt),
				}
			}
			var admission *agentic.LaunchBoundaryMCPAdmission
			if tc.admissionPathSet || tc.inlinePrefix != nil || tc.inlineServers != nil {
				path := tc.admissionPath
				if rest, ok := strings.CutPrefix(path, "$ROOT/"); ok {
					path = filepath.Join(root, rest)
				}
				admission = &agentic.LaunchBoundaryMCPAdmission{
					ConfigPath: path, ConfigPathSet: tc.admissionPathSet,
					Inline: agentic.Composition{Prefix: tc.inlinePrefix, Servers: tc.inlineServers},
				}
			}
			policy := agentic.LaunchBoundaryPolicy{
				Environment: tc.environment, MCPLayerPath: mcp,
				MCPAdmission: admission,
				Home:         home, ProfileName: "test-profile", SystemPrompt: policyPrompt,
			}
			process := agentic.LaunchBoundaryProcess{Binary: binary, WorkDir: workdir, Env: env}
			report, err := agentic.CheckLaunchBoundary(process, policy)

			// The combined oracle mirrors launcher order: an admission
			// fault is reported before any boundary fault.
			admissionErr := lbOracleAdmission(tc.environment, admission)
			oracleErr, oracleWarnings := admissionErr, []string(nil)
			if oracleErr == nil {
				oracleErr, oracleWarnings = refErr, refWarnings
			}

			if admission != nil {
				// Admission rows: the launcher frames no code for these
				// refusals (provider evidence under plan_refused), so the
				// gate is exact message bytes against the oracle plus the
				// pinned literal. A late refusal under a valid admission
				// additionally asserts its code.
				if (err == nil) != (oracleErr == nil) {
					t.Fatalf("adapter/oracle nil mismatch: adapter=%v oracle=%v", err, oracleErr)
				}
				if err != nil {
					if err.Error() != oracleErr.Error() {
						t.Fatalf("adapter bytes = %q, oracle = %q", err.Error(), oracleErr.Error())
					}
					if admissionErr != nil {
						if tc.wantMessage == "" {
							t.Fatal("admission refusal without pinned message bytes")
						}
						if err.Error() != tc.wantMessage {
							t.Fatalf("adapter bytes = %q, want %q", err.Error(), tc.wantMessage)
						}
						if oracleErr.Error() != tc.wantMessage {
							t.Fatalf("oracle bytes = %q, want %q (pinned contract drifted)", oracleErr.Error(), tc.wantMessage)
						}
					} else if got, want := lbCode(err), tc.wantCode; got != want {
						t.Fatalf("adapter code = %q, want %q (err=%v)", got, want, err)
					} else if got := lbCode(oracleErr); got != tc.wantCode {
						t.Fatalf("oracle code = %q, want %q (err=%v)", got, tc.wantCode, oracleErr)
					}
				} else {
					if !equalLBStrings(report.Warnings, oracleWarnings) {
						t.Fatalf("adapter warnings = %q, oracle = %q", report.Warnings, oracleWarnings)
					}
					if len(report.Warnings) != tc.wantWarnings {
						t.Fatalf("warnings = %d, want %d: %q", len(report.Warnings), tc.wantWarnings, report.Warnings)
					}
					if report.ResolvedBinary == "" {
						t.Fatal("accepted launch carries no resolved binary")
					}
					if !filepath.IsAbs(report.ResolvedBinary) {
						t.Fatalf("resolved binary is not absolute: %q", report.ResolvedBinary)
					}
				}
				return
			}

			if got, want := lbCode(err), tc.wantCode; got != want {
				t.Fatalf("adapter code = %q, want %q (err=%v)", got, want, err)
			}
			if got := lbCode(refErr); got != tc.wantCode {
				t.Fatalf("reference code = %q, want %q (err=%v)", got, tc.wantCode, refErr)
			}
			if lbCode(err) != lbCode(refErr) {
				t.Fatalf("adapter/ref divergence: %q vs %q", lbCode(err), lbCode(refErr))
			}
			if err != nil {
				// Byte-identical refusal contract: the code match above
				// cannot see a changed message.
				if refErr == nil {
					t.Fatalf("adapter refused %v while the reference accepted", err)
				}
				if err.Error() != refErr.Error() {
					t.Fatalf("adapter bytes = %q, reference = %q", err.Error(), refErr.Error())
				}
			}
			if tc.wantMessage != "" {
				if err == nil || err.Error() != tc.wantMessage {
					t.Fatalf("adapter bytes = %v, want %q", err, tc.wantMessage)
				}
				if refErr == nil || refErr.Error() != tc.wantMessage {
					t.Fatalf("reference bytes = %v, want %q", refErr, tc.wantMessage)
				}
			}
			if err == nil && !equalLBStrings(report.Warnings, refWarnings) {
				t.Fatalf("adapter warnings = %q, reference = %q", report.Warnings, refWarnings)
			}
			if err == nil && len(report.Warnings) != tc.wantWarnings {
				t.Fatalf("warnings = %d, want %d: %q", len(report.Warnings), tc.wantWarnings, report.Warnings)
			}
			if err == nil && report.ResolvedBinary == "" {
				t.Fatal("accepted launch carries no resolved binary")
			}
			if err == nil && !filepath.IsAbs(report.ResolvedBinary) {
				t.Fatalf("resolved binary is not absolute: %q", report.ResolvedBinary)
			}
		})
	}
}

// TestLaunchBoundarySelectMatrix mirrors the launcher's
// TestSelectRefusalsAndNoOptIn: every environment, section presence, and
// opt-in spelling, asserting adapter and reference agree with the launcher's
// support matrix.
func TestLaunchBoundarySelectMatrix(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	channelsFor := map[string][]refLBChannel{
		"claude_code": lbClaudeChannels(),
		"codex_cli":   lbCodexChannels(),
		"pi":          lbPiChannels(),
		"opencode":    {},
	}
	for _, env := range []string{"claude_code", "codex_cli", "pi", "opencode"} {
		for _, section := range []bool{false, true} {
			for _, opt := range []string{"", "append", "replace", "bogus"} {
				t.Run(env+"/"+map[bool]string{true: "section", false: "absent"}[section]+"/"+opt, func(t *testing.T) {
					home := t.TempDir()
					root := t.TempDir()
					prompt := filepath.Join(home, "prompt.md")
					writeLBFile(t, prompt, []byte("prompt"), 0o600)
					e := []string{"PATH=/nonexistent"}
					fragment := &refLBFragment{Environment: env, ProfileName: "test-profile", Home: home}
					if section {
						fragment.SystemPrompt = &refLBSystemPrompt{Path: prompt, Channels: channelsFor[env]}
					}
					refWarnings, refErr := refLBRun(provider, root, e, fragment, opt)
					var policyPrompt *agentic.LaunchBoundarySystemPrompt
					if section {
						policyPrompt = &agentic.LaunchBoundarySystemPrompt{
							Path: prompt, Channels: lbChannels(channelsFor[env]),
							Intent: agentic.CuratorSystemPromptIntent(opt),
						}
					} else if opt != "" {
						policyPrompt = &agentic.LaunchBoundarySystemPrompt{
							Intent: agentic.CuratorSystemPromptIntent(opt),
						}
					}
					policy := agentic.LaunchBoundaryPolicy{
						Environment: env, Home: home, ProfileName: "test-profile",
						SystemPrompt: policyPrompt,
					}
					report, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: e}, policy)
					supported := section && (env == "claude_code" || env == "codex_cli" && opt == "replace" || env == "pi" && opt == "append") && opt != "bogus"
					want := ""
					if opt != "" && !supported {
						want = "sysprompt_channel_unavailable"
					}
					if got := lbCode(err); got != want {
						t.Fatalf("adapter code = %q, want %q (err=%v)", got, want, err)
					}
					if got := lbCode(refErr); got != want {
						t.Fatalf("reference code = %q, want %q (err=%v)", got, want, refErr)
					}
					if want != "" {
						if err == nil || refErr == nil {
							t.Fatalf("missing channel-unavailable witness: adapter=%v reference=%v", err, refErr)
						}
						if err.Error() != refErr.Error() {
							t.Fatalf("adapter bytes = %q, reference = %q", err.Error(), refErr.Error())
						}
						wantMessage := fmt.Sprintf("sysprompt_channel_unavailable: \"\": no non-file system-prompt channel for %q", opt)
						if err.Error() != wantMessage {
							t.Fatalf("adapter bytes = %q, want %q", err.Error(), wantMessage)
						}
						if refErr.Error() != wantMessage {
							t.Fatalf("reference bytes = %q, want %q", refErr.Error(), wantMessage)
						}
						return
					}
					if err != nil || refErr != nil {
						t.Fatalf("supported selection refused: adapter=%v reference=%v", err, refErr)
					}
					if !equalLBStrings(report.Warnings, refWarnings) {
						t.Fatalf("adapter warnings = %q, reference = %q", report.Warnings, refWarnings)
					}
				})
			}
		}
	}
}

func equalLBStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLaunchBoundaryPromptFileMatrix mirrors the launcher's
// TestPrepareLaunchFileFailures: every managed-home file and the selected
// prompt path, each under every filesystem shape.
func TestLaunchBoundaryPromptFileMatrix(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	for _, which := range []string{"APPEND_SYSTEM.md", "SYSTEM.md", "selected"} {
		for _, kind := range []string{"missing", "directory", "dangling", "symlink", "permissions", "fifo", "loop"} {
			t.Run(which+"/"+kind, func(t *testing.T) {
				home := t.TempDir()
				root := t.TempDir()
				prompt := filepath.Join(home, "prompt.md")
				writeLBFile(t, prompt, []byte("prompt"), 0o600)
				path := filepath.Join(home, which)
				if which == "selected" {
					path = prompt
					if err := os.Remove(prompt); err != nil {
						t.Fatal(err)
					}
				}
				switch kind {
				case "directory":
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				case "dangling":
					if err := os.Symlink(filepath.Join(home, "missing-target"), path); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					target := filepath.Join(home, "target")
					writeLBFile(t, target, []byte("prompt"), 0o600)
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				case "permissions":
					writeLBFile(t, path, []byte("prompt"), 0o600)
					if err := os.Chmod(path, 0o000); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
					if os.Geteuid() == 0 {
						t.Skip("root bypasses mode permissions; permission denial not proven")
					}
				case "fifo":
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Fatal(err)
					}
				case "loop":
					if err := os.Symlink(path, path); err != nil {
						t.Fatal(err)
					}
				}
				env := []string{"PATH=/nonexistent"}
				fragment := &refLBFragment{
					Environment: "pi", ProfileName: "test-profile", Home: home,
					SystemPrompt: &refLBSystemPrompt{Path: prompt, Channels: lbPiChannels()},
				}
				_, refErr := refLBRun(provider, root, env, fragment, "append")
				policy := agentic.LaunchBoundaryPolicy{
					Environment: "pi", Home: home, ProfileName: "test-profile",
					SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
						Path: prompt, Channels: lbChannels(lbPiChannels()),
						Intent: agentic.CuratorSystemPromptAppend,
					},
				}
				_, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}, policy)
				good := kind == "symlink" || kind == "missing" && which != "selected"
				if good {
					if err != nil {
						t.Fatalf("adapter refused %s/%s: %v", which, kind, err)
					}
					if refErr != nil {
						t.Fatalf("reference refused %s/%s: %v", which, kind, refErr)
					}
					return
				}
				if got := lbCode(err); got != "sysprompt_file_unreadable" {
					t.Fatalf("adapter code = %q, want sysprompt_file_unreadable (err=%v)", got, err)
				}
				if got := lbCode(refErr); got != "sysprompt_file_unreadable" {
					t.Fatalf("reference code = %q, want sysprompt_file_unreadable (err=%v)", got, refErr)
				}
				var refusal *agentic.LaunchBoundaryPromptError
				if !errors.As(err, &refusal) || refusal.Path != path {
					t.Fatalf("adapter refusal path = %v, want %q", err, path)
				}
				if err.Error() != refErr.Error() {
					t.Fatalf("adapter bytes = %q, reference = %q", err.Error(), refErr.Error())
				}
			})
		}
	}
}

// TestLaunchBoundaryMCPUnreadable mirrors the launcher's unreadable MCP layer
// row: a present-but-unreadable layer refuses mcp_layer_unreadable rather
// than the missing code.
func TestLaunchBoundaryMCPUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses Unix file permissions")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	root := t.TempDir()
	layer := filepath.Join(root, "layer.toml")
	writeLBFile(t, layer, []byte("mcp"), 0o600)
	if err := os.Chmod(layer, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(layer, 0o600) })
	env := []string{"PATH=/nonexistent"}
	_, refErr := refLBRun(provider, root, env, &refLBFragment{Environment: "codex_cli", MCPLayer: layer}, "")
	policy := agentic.LaunchBoundaryPolicy{Environment: "codex_cli", MCPLayerPath: layer}
	_, err = agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}, policy)
	if err == nil || refErr == nil {
		t.Fatalf("missing permission witness: adapter=%v reference=%v", err, refErr)
	}
	if got := lbCode(err); got != "mcp_layer_unreadable" {
		t.Fatalf("adapter code = %q, want mcp_layer_unreadable (err=%v)", got, err)
	}
	if got := lbCode(refErr); got != "mcp_layer_unreadable" {
		t.Fatalf("reference code = %q, want mcp_layer_unreadable (err=%v)", got, refErr)
	}
	var mcp *agentic.LaunchBoundaryMCPError
	if !errors.As(err, &mcp) || mcp.Path != layer {
		t.Fatalf("adapter refusal path = %v, want %q", err, layer)
	}
	if err.Error() != refErr.Error() {
		t.Fatalf("adapter bytes = %q, reference = %q", err.Error(), refErr.Error())
	}
}

// TestLaunchBoundaryPermissionMessagesMatchLauncher is the named kill test
// for the launch-boundary-mcp-permission-message-altered mutant and the
// regression test for the round-2 F1 repeat: every permission-denied refusal
// protects the exact code AND the full Error() bytes against the frozen
// launcher reference. A renderer that drifts only the permission detail while
// keeping the code, path, cause identity, and verdict must fail here.
func TestLaunchBoundaryPermissionMessagesMatchLauncher(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses Unix file permissions; permission denial not proven")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	env := []string{"PATH=/nonexistent"}

	// MCP leg: a present-but-unreadable codex layer refuses with the
	// launcher's open detail, not a fixed sentence.
	root := t.TempDir()
	layer := filepath.Join(root, "layer.toml")
	writeLBFile(t, layer, []byte("mcp"), 0o600)
	if err := os.Chmod(layer, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(layer, 0o600) })
	_, refErr := refLBRun(provider, root, env, &refLBFragment{Environment: "codex_cli", MCPLayer: layer}, "")
	_, err = agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}, agentic.LaunchBoundaryPolicy{Environment: "codex_cli", MCPLayerPath: layer})
	if err == nil || refErr == nil {
		t.Fatalf("missing MCP permission witness: adapter=%v reference=%v", err, refErr)
	}
	if got := lbCode(err); got != "mcp_layer_unreadable" {
		t.Fatalf("adapter code = %q, want mcp_layer_unreadable (err=%v)", got, err)
	}
	if got := lbCode(refErr); got != "mcp_layer_unreadable" {
		t.Fatalf("reference code = %q, want mcp_layer_unreadable (err=%v)", got, refErr)
	}
	if err.Error() != refErr.Error() {
		t.Fatalf("mcp permission message bytes changed: adapter=%q reference=%q", err.Error(), refErr.Error())
	}

	// Prompt leg, selected path: an unreadable selected prompt refuses with
	// the launcher's open detail.
	home := t.TempDir()
	prompt := filepath.Join(home, "prompt.md")
	writeLBFile(t, prompt, []byte("prompt"), 0o600)
	if err := os.Chmod(prompt, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(prompt, 0o600) })
	selectedFragment := &refLBFragment{
		Environment: "pi", ProfileName: "test-profile", Home: home,
		SystemPrompt: &refLBSystemPrompt{Path: prompt, Channels: lbPiChannels()},
	}
	_, selectedRefErr := refLBRun(provider, root, env, selectedFragment, "append")
	_, selectedErr := agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{
			Environment: "pi", Home: home, ProfileName: "test-profile",
			SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
				Path: prompt, Channels: lbChannels(lbPiChannels()),
				Intent: agentic.CuratorSystemPromptAppend,
			},
		})
	if selectedErr == nil || selectedRefErr == nil {
		t.Fatalf("missing selected-prompt permission witness: adapter=%v reference=%v", selectedErr, selectedRefErr)
	}
	if got := lbCode(selectedErr); got != "sysprompt_file_unreadable" {
		t.Fatalf("adapter code = %q, want sysprompt_file_unreadable (err=%v)", got, selectedErr)
	}
	if selectedErr.Error() != selectedRefErr.Error() {
		t.Fatalf("selected permission message bytes changed: adapter=%q reference=%q", selectedErr.Error(), selectedRefErr.Error())
	}

	// Prompt leg, managed-home observation: an unreadable APPEND_SYSTEM.md
	// refuses while the selected prompt stays readable.
	home2 := t.TempDir()
	prompt2 := filepath.Join(home2, "prompt.md")
	writeLBFile(t, prompt2, []byte("prompt"), 0o600)
	observed := filepath.Join(home2, "APPEND_SYSTEM.md")
	writeLBFile(t, observed, []byte("append"), 0o600)
	if err := os.Chmod(observed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(observed, 0o600) })
	observedFragment := &refLBFragment{
		Environment: "pi", ProfileName: "test-profile", Home: home2,
		SystemPrompt: &refLBSystemPrompt{Path: prompt2, Channels: lbPiChannels()},
	}
	_, observedRefErr := refLBRun(provider, root, env, observedFragment, "append")
	_, observedErr := agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{
			Environment: "pi", Home: home2, ProfileName: "test-profile",
			SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
				Path: prompt2, Channels: lbChannels(lbPiChannels()),
				Intent: agentic.CuratorSystemPromptAppend,
			},
		})
	if observedErr == nil || observedRefErr == nil {
		t.Fatalf("missing managed-home permission witness: adapter=%v reference=%v", observedErr, observedRefErr)
	}
	if got := lbCode(observedErr); got != "sysprompt_file_unreadable" {
		t.Fatalf("adapter code = %q, want sysprompt_file_unreadable (err=%v)", got, observedErr)
	}
	if observedErr.Error() != observedRefErr.Error() {
		t.Fatalf("managed-home permission message bytes changed: adapter=%q reference=%q", observedErr.Error(), observedRefErr.Error())
	}
}

// TestLaunchBoundaryRefusesNonexecutableBinary is the named kill test for the
// launch-boundary-binary-identity-skipped mutant: the executable bit is part
// of binary identity, not just regularity and presence.
func TestLaunchBoundaryRefusesNonexecutableBinary(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	plain := filepath.Join(dir, "plain-file")
	writeLBFile(t, plain, []byte("not executable"), 0o600)
	env := []string{"PATH=/nonexistent"}
	policy := agentic.LaunchBoundaryPolicy{Environment: "pi", Home: t.TempDir()}
	if _, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env}, policy); err != nil {
		t.Fatalf("executable binary refused: %v", err)
	}
	_, err = agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: plain, WorkDir: dir, Env: env}, policy)
	if got := lbCode(err); got != "exec_provider_missing" {
		t.Fatalf("nonexecutable binary admitted: code = %q, want exec_provider_missing (err=%v)", got, err)
	}
}

// TestLaunchBoundaryRefusesDirectoryMCPLayer is the named kill test for the
// launch-boundary-mcp-check-skipped mutant: a directory at the layer path is
// not a regular file and refuses mcp_layer_unreadable.
func TestLaunchBoundaryRefusesDirectoryMCPLayer(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	root := t.TempDir()
	layer := filepath.Join(root, "layer.toml")
	if err := os.Mkdir(layer, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=/nonexistent"}
	policy := agentic.LaunchBoundaryPolicy{Environment: "codex_cli", MCPLayerPath: layer}
	_, err = agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}, policy)
	if got := lbCode(err); got != "mcp_layer_unreadable" {
		t.Fatalf("directory MCP layer admitted: code = %q, want mcp_layer_unreadable (err=%v)", got, err)
	}
}

// TestLaunchBoundaryRefusesMissingSelectedPrompt is the named kill test for
// the launch-boundary-prompt-check-skipped mutant: a selected prompt path
// that is absent refuses sysprompt_file_unreadable; absence is tolerated
// only for unselected managed-home observations.
func TestLaunchBoundaryRefusesMissingSelectedPrompt(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	home := t.TempDir()
	env := []string{"PATH=/nonexistent"}
	policy := agentic.LaunchBoundaryPolicy{
		Environment: "pi", Home: home, ProfileName: "test-profile",
		SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
			Path: filepath.Join(home, "absent-prompt.md"), Channels: lbChannels(lbPiChannels()),
			Intent: agentic.CuratorSystemPromptAppend,
		},
	}
	_, err = agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env}, policy)
	if got := lbCode(err); got != "sysprompt_file_unreadable" {
		t.Fatalf("missing selected prompt admitted: code = %q, want sysprompt_file_unreadable (err=%v)", got, err)
	}
}

// TestLaunchBoundaryIgnoresLowercasePathEnv pins the exact-case PATH match:
// the resolver honors only the exact "PATH=" prefix, never a case variant.
// It is the named kill test for the launch-boundary-path-case-admitted
// mutant, which preserves the "PATH=" token and changes behavior.
func TestLaunchBoundaryIgnoresLowercasePathEnv(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	makeLBExec(t, filepath.Join(dir, "provider"))
	policy := agentic.LaunchBoundaryPolicy{Environment: "pi", Home: t.TempDir()}
	process := agentic.LaunchBoundaryProcess{Binary: "provider", WorkDir: dir, Env: []string{"Path=" + dir}}
	_, err = agentic.CheckLaunchBoundary(process, policy)
	if got := lbCode(err); got != "exec_provider_missing" {
		t.Fatalf("lowercase Path= entry resolved the binary: code = %q, want exec_provider_missing (err=%v)", got, err)
	}
}

// TestLaunchBoundaryWarningsMatchLauncher pins the exact warning bytes the
// launcher's FormatWarnings emits: the applied-channel line, the suppression
// line, and the conditional-discovery line.
func TestLaunchBoundaryWarningsMatchLauncher(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	home := t.TempDir()
	prompt := filepath.Join(home, "prompt.md")
	writeLBFile(t, prompt, []byte("prompt"), 0o600)
	writeLBFile(t, filepath.Join(home, "APPEND_SYSTEM.md"), []byte("a"), 0o600)
	writeLBFile(t, filepath.Join(home, "SYSTEM.md"), []byte("s"), 0o600)
	env := []string{"PATH=/nonexistent"}
	policy := agentic.LaunchBoundaryPolicy{
		Environment: "pi", Home: home, ProfileName: "test-profile",
		SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
			Path: prompt, Channels: lbChannels(lbPiChannels()),
			Intent: agentic.CuratorSystemPromptAppend,
		},
	}
	report, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env}, policy)
	if err != nil {
		t.Fatal(err)
	}
	const cache = " A custom system prefix can change request caching and billing: the default may use shared prompt caching; a custom prefix forms its own cache prefix."
	want := []string{
		`warning: profile "test-profile": applied flag/append system-prompt channel "--append-system-prompt".` + cache,
		`warning: profile "test-profile": observed file/append "APPEND_SYSTEM.md" in managed home; discovery suppressed by applied launcher flag "--append-system-prompt".` + cache,
		`warning: profile "test-profile": observed file/replace "SYSTEM.md" in managed home; conditional native-discovery candidate, subject to native flags and trusted-project precedence. If selected, replacement discards the tool's built-in system behavior entirely.` + cache,
	}
	if !equalLBStrings(report.Warnings, want) {
		t.Fatalf("warnings = %q, want %q", report.Warnings, want)
	}

	codexPolicy := agentic.LaunchBoundaryPolicy{
		Environment: "codex_cli", Home: home, ProfileName: "test-profile",
		SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
			Path: "/prompt", Channels: lbChannels(lbCodexChannels()),
			Intent: agentic.CuratorSystemPromptReplace,
		},
	}
	codexReport, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env}, codexPolicy)
	if err != nil {
		t.Fatal(err)
	}
	wantCodex := `warning: profile "test-profile": applied config-key/replace system-prompt channel. Replacement discards the tool's built-in system behavior entirely.` + cache
	if len(codexReport.Warnings) != 1 || codexReport.Warnings[0] != wantCodex {
		t.Fatalf("codex warnings = %q, want %q", codexReport.Warnings, wantCodex)
	}

	claudePolicy := agentic.LaunchBoundaryPolicy{
		Environment: "claude_code", Home: home, ProfileName: "test-profile",
		SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
			Path: "/prompt", Channels: lbChannels(lbClaudeChannels()),
			Intent: agentic.CuratorSystemPromptReplace,
		},
	}
	claudeReport, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env}, claudePolicy)
	if err != nil {
		t.Fatal(err)
	}
	wantClaude := `warning: profile "test-profile": applied flag/replace system-prompt channel "--system-prompt-file". Replacement discards the tool's built-in system behavior entirely.` + cache
	if len(claudeReport.Warnings) != 1 || claudeReport.Warnings[0] != wantClaude {
		t.Fatalf("claude warnings = %q, want %q", claudeReport.Warnings, wantClaude)
	}
}

// TestLaunchBoundaryReprobesEveryCall mirrors the launcher's late-replacement
// rows: a successful check is stale evidence, and every call probes afresh.
func TestLaunchBoundaryReprobesEveryCall(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	root := t.TempDir()
	layer := filepath.Join(root, "layer.toml")
	writeLBFile(t, layer, []byte("mcp"), 0o600)
	env := []string{"PATH=/nonexistent"}
	policy := agentic.LaunchBoundaryPolicy{Environment: "codex_cli", MCPLayerPath: layer}
	process := agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}
	if _, err := agentic.CheckLaunchBoundary(process, policy); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(layer); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.CheckLaunchBoundary(process, policy); lbCode(err) != "mcp_layer_missing" {
		t.Fatalf("late MCP deletion admitted: %v", err)
	}
	if err := os.Mkdir(layer, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.CheckLaunchBoundary(process, policy); lbCode(err) != "mcp_layer_unreadable" {
		t.Fatalf("late MCP replacement admitted: %v", err)
	}

	home := t.TempDir()
	prompt := filepath.Join(home, "prompt.md")
	writeLBFile(t, prompt, []byte("prompt"), 0o600)
	promptPolicy := agentic.LaunchBoundaryPolicy{
		Environment: "pi", Home: home, ProfileName: "test-profile",
		SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
			Path: prompt, Channels: lbChannels(lbPiChannels()),
			Intent: agentic.CuratorSystemPromptAppend,
		},
	}
	promptProcess := agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}
	if _, err := agentic.CheckLaunchBoundary(promptProcess, promptPolicy); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if _, err := agentic.CheckLaunchBoundary(promptProcess, promptPolicy); lbCode(err) != "sysprompt_file_unreadable" {
		t.Fatalf("late prompt deletion admitted: %v", err)
	}
}

// TestLaunchBoundaryPerformsNoWrites pins the read-only contract: every
// probed file keeps its exact bytes, and the boundary creates nothing.
func TestLaunchBoundaryPerformsNoWrites(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	root := t.TempDir()
	home := t.TempDir()
	layer := filepath.Join(root, "layer.toml")
	writeLBFile(t, layer, []byte("mcp-bytes"), 0o600)
	prompt := filepath.Join(home, "prompt.md")
	writeLBFile(t, prompt, []byte("prompt-bytes"), 0o600)
	writeLBFile(t, filepath.Join(home, "APPEND_SYSTEM.md"), []byte("append-bytes"), 0o600)
	env := []string{"PATH=/nonexistent"}
	before := func() map[string]string {
		out := map[string]string{}
		for _, path := range []string{provider, layer, prompt, filepath.Join(home, "APPEND_SYSTEM.md")} {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out[path] = string(data)
		}
		return out
	}
	entriesBefore := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	wantBytes, wantRoot, wantHome := before(), entriesBefore(root), entriesBefore(home)
	policies := []agentic.LaunchBoundaryPolicy{
		{Environment: "codex_cli", MCPLayerPath: layer},
		{
			Environment: "pi", Home: home, ProfileName: "test-profile",
			SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
				Path: prompt, Channels: lbChannels(lbPiChannels()),
				Intent: agentic.CuratorSystemPromptAppend,
			},
		},
		{Environment: "pi", Home: home, ProfileName: "test-profile"},
	}
	for _, policy := range policies {
		if _, err := agentic.CheckLaunchBoundary(agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env}, policy); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range wantBytes {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("boundary rewrote %s", path)
		}
	}
	if !equalLBStrings(entriesBefore(root), wantRoot) || !equalLBStrings(entriesBefore(home), wantHome) {
		t.Fatal("boundary created directory entries")
	}
}

// TestLaunchBoundaryRefusalShapes pins the typed surface: every refusal is
// reachable via errors.As, unwraps to its filesystem cause, and frames the
// launcher code before the first ": ".
func TestLaunchBoundaryRefusalShapes(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	root := t.TempDir()
	env := []string{"PATH=/nonexistent"}

	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: filepath.Join(root, "absent"), WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{Environment: "pi", Home: t.TempDir()})
	var binaryErr *agentic.LaunchBoundaryBinaryError
	if !errors.As(err, &binaryErr) || binaryErr.Code() != "exec_provider_missing" || binaryErr.Unwrap() == nil {
		t.Fatalf("binary refusal shape = %#v", err)
	}
	if code, _, _ := strings.Cut(err.Error(), ": "); code != "exec_provider_missing" {
		t.Fatalf("binary refusal frames %q", err.Error())
	}

	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{Environment: "codex_cli", MCPLayerPath: filepath.Join(root, "absent-layer")})
	var mcpErr *agentic.LaunchBoundaryMCPError
	if !errors.As(err, &mcpErr) || mcpErr.Code != "mcp_layer_missing" || mcpErr.Unwrap() == nil {
		t.Fatalf("mcp refusal shape = %#v", err)
	}
	if code, _, _ := strings.Cut(err.Error(), ": "); code != "mcp_layer_missing" {
		t.Fatalf("mcp refusal frames %q", err.Error())
	}

	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{
			Environment: "pi", Home: t.TempDir(),
			SystemPrompt: &agentic.LaunchBoundarySystemPrompt{Intent: agentic.CuratorSystemPromptAppend},
		})
	var promptErr *agentic.LaunchBoundaryPromptError
	if !errors.As(err, &promptErr) || promptErr.Code != "sysprompt_channel_unavailable" || promptErr.Unwrap() == nil {
		t.Fatalf("prompt refusal shape = %#v", err)
	}
	if code, _, _ := strings.Cut(err.Error(), ": "); code != "sysprompt_channel_unavailable" {
		t.Fatalf("prompt refusal frames %q", err.Error())
	}
	// Byte-identical unavailable rendering, including the empty quoted path.
	if want := `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "append"`; err.Error() != want {
		t.Fatalf("unavailable refusal = %q, want %q", err.Error(), want)
	}

	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{
			Environment: "claude_code",
			MCPAdmission: &agentic.LaunchBoundaryMCPAdmission{
				Inline: agentic.Composition{
					Prefix:  []string{"--mcp-config", `{"mcpServers": {oops`},
					Servers: []agentic.CompositionServer{{Name: "a", Transport: "stdio"}},
				},
			},
		})
	var admissionErr *agentic.LaunchBoundaryMCPAdmissionError
	if !errors.As(err, &admissionErr) || admissionErr.Message != "invalid mcp-config-json MCP config root" {
		t.Fatalf("admission refusal shape = %#v", err)
	}
	if err.Error() != admissionErr.Message {
		t.Fatalf("admission refusal renders %q, want the sentence bytes", err.Error())
	}
}

// TestLaunchBoundaryRefusalMessagesMatchLauncher pins the exact refusal
// message bytes for every leg whose rendering carries no OS error text. It
// is the named kill test for the launch-boundary-refusal-message-altered
// mutant: any rendering change fails these pins.
func TestLaunchBoundaryRefusalMessagesMatchLauncher(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	root := t.TempDir()
	env := []string{"PATH=" + dir}

	// Binary leg: the full PATH-miss sentence, including the launcher's
	// help text and the transcribed ErrNotFound message.
	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: "fake-provider-not-on-PATH", WorkDir: root, Env: env},
		agentic.LaunchBoundaryPolicy{Environment: "pi", Home: t.TempDir()})
	wantBinary := "exec_provider_missing: fake-provider-not-on-PATH: install the provider executable and make it available on the launch PATH: executable file not found in $PATH"
	if err == nil || err.Error() != wantBinary {
		t.Fatalf("binary refusal message bytes changed: got %v, want %q", err, wantBinary)
	}

	// MCP leg: a directory layer refuses with a fixed sentence (no OS text).
	layer := filepath.Join(root, "layer.toml")
	if err := os.Mkdir(layer, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: []string{"PATH=/nonexistent"}},
		agentic.LaunchBoundaryPolicy{Environment: "codex_cli", MCPLayerPath: layer})
	wantMCP := fmt.Sprintf("mcp_layer_unreadable: %s: not a regular file", layer)
	if err == nil || err.Error() != wantMCP {
		t.Fatalf("mcp refusal message bytes changed: got %v, want %q", err, wantMCP)
	}

	// Prompt leg: a directory selection refuses with a fixed sentence.
	home := t.TempDir()
	prompt := filepath.Join(home, "prompt.md")
	if err := os.Mkdir(prompt, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: []string{"PATH=/nonexistent"}},
		agentic.LaunchBoundaryPolicy{
			Environment: "pi", Home: home, ProfileName: "test-profile",
			SystemPrompt: &agentic.LaunchBoundarySystemPrompt{
				Path: prompt, Channels: lbChannels(lbPiChannels()),
				Intent: agentic.CuratorSystemPromptAppend,
			},
		})
	wantPrompt := fmt.Sprintf("sysprompt_file_unreadable: %q: not a regular file", prompt)
	if err == nil || err.Error() != wantPrompt {
		t.Fatalf("prompt refusal message bytes changed: got %v, want %q", err, wantPrompt)
	}

	// Unavailable leg: empty quoted path included.
	_, err = agentic.CheckLaunchBoundary(
		agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: root, Env: []string{"PATH=/nonexistent"}},
		agentic.LaunchBoundaryPolicy{
			Environment: "pi", Home: t.TempDir(),
			SystemPrompt: &agentic.LaunchBoundarySystemPrompt{Intent: agentic.CuratorSystemPromptAppend},
		})
	wantUnavailable := `sysprompt_channel_unavailable: "": no non-file system-prompt channel for "append"`
	if err == nil || err.Error() != wantUnavailable {
		t.Fatalf("prompt refusal message bytes changed: got %v, want %q", err, wantUnavailable)
	}
}

// TestLaunchBoundaryRefusesTrailingInlineMCPContent is the named kill test
// for the launch-boundary-mcp-inline-admission-skipped mutant: a document
// with trailing content after a valid root is refused, while the other
// malformed shapes still refuse around it.
func TestLaunchBoundaryRefusesTrailingInlineMCPContent(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	env := []string{"PATH=/nonexistent"}
	check := func(doc string) error {
		_, err := agentic.CheckLaunchBoundary(
			agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env},
			agentic.LaunchBoundaryPolicy{
				Environment: "claude_code",
				MCPAdmission: &agentic.LaunchBoundaryMCPAdmission{
					Inline: agentic.Composition{
						Prefix:  []string{"--mcp-config", doc},
						Servers: []agentic.CompositionServer{{Name: "a", Transport: "stdio"}},
					},
				},
			})
		return err
	}
	// The mutant weakens only the trailing-content check: these refusals
	// must hold with and without it.
	for _, doc := range []string{`{"mcpServers": {oops`, "", `{"mcpServers": {"a": {"type": "stdio", "command": "x", "zzz": 1}}}`} {
		if err := check(doc); err == nil {
			t.Fatalf("malformed inline MCP document admitted: %q", doc)
		}
	}
	trailing := `{"mcpServers": {"a": {"type": "stdio", "command": "x"}}}` + ` {"smuggled": true}`
	if err := check(trailing); err == nil {
		t.Fatal("trailing inline MCP content admitted")
	} else if want := "invalid mcp-config-json MCP config root"; err.Error() != want {
		t.Fatalf("trailing inline MCP refusal = %q, want %q", err.Error(), want)
	}
}

// TestLaunchBoundaryRefusesRelativeMCPConfigPath is the named kill test for
// the launch-boundary-mcp-path-admission-skipped mutant: a relative config
// path is refused, while the other path refusals still refuse around it.
func TestLaunchBoundaryRefusesRelativeMCPConfigPath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(dir, "provider")
	makeLBExec(t, provider)
	env := []string{"PATH=/nonexistent"}
	check := func(path string) error {
		_, err := agentic.CheckLaunchBoundary(
			agentic.LaunchBoundaryProcess{Binary: provider, WorkDir: dir, Env: env},
			agentic.LaunchBoundaryPolicy{
				Environment: "claude_code",
				MCPAdmission: &agentic.LaunchBoundaryMCPAdmission{
					ConfigPath: path, ConfigPathSet: true,
				},
			})
		return err
	}
	// The mutant weakens only the leading-slash conjunct: these refusals
	// must hold with and without it.
	for _, path := range []string{"", "/a/../b", "/a\x00b", "/" + strings.Repeat("x", 4096)} {
		if err := check(path); err == nil {
			t.Fatalf("invalid MCP config path admitted: %q", path)
		}
	}
	if err := check("rel/mcp.json"); err == nil {
		t.Fatal("relative MCP config path admitted")
	} else if want := `fragment: /mcp/path: "rel/mcp.json" is not an absolute path`; err.Error() != want {
		t.Fatalf("relative MCP config path refusal = %q, want %q", err.Error(), want)
	}
	if err := check("/mcp.json"); err != nil {
		t.Fatalf("absolute MCP config path refused: %v", err)
	}
}
