package agentic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// MCP admission validators.
//
// The fragment mcp.path validator ports the launcher's resolve-time admission
// (launcher internal/fragment/fragment.go): the fragment resolver admits the
// mcp section before the pre-exec boundary ever runs. The inline --mcp-config
// validator is the module's own mcp-config-json grammar
// (internal/mcpjson/mcpjson.go:96-153, byte-identical at v0.5.37), kept as an
// explicitly requested inline validator distinct from the launcher's native
// reachable boundary (launcher internal/plan/plan.go:74-77 at fbcdbaf0 keeps
// Composition unset, so the reachable curator run path carries no inline
// Composition to validate). CheckLaunchBoundary repeats both validators
// before its boundary legs so that a future daemon or consumer call re-checks
// admitted MCP facts immediately before every start with byte-identical
// accept/refuse behavior and message bytes. The daemon and the launcher
// do not call this API yet; launcher migration follows in a separate leaf
// and is not wired yet.
//
// The file validator ports the fragment mcp.path admission: the launcher
// checks the path STRING (absolute-path rule) and never stats or reads the
// file at admission. File STATE (missing, unreadable, permissions, symlinks)
// is observed only by the codex late layer probe, which accepts readable
// files regardless of mode bits and follows symlinks; the parity table pins
// those acceptances rather than inventing refusals.
//
// The inline validator transcribes internal/mcpjson.Validate rule for rule.
// It cannot call that function: internal/mcpjson imports pkg/agentic for the
// Composition types, so a call would be an import cycle. The parity test
// executes the real mcpjson.Validate as its oracle, so any transcription
// slip fails loudly against the pinned behavior owner.
//
// This file performs no I/O and owns no exec.
const (
	// launchBoundaryMCPGrammar transcribes internal/mcpjson.Grammar
	// (internal/mcpjson/mcpjson.go): the grammar identifier names the
	// --mcp-config JSON grammar, not a system.
	launchBoundaryMCPGrammar = "mcp-config-json"
	// launchBoundaryMCPConfigFlag transcribes internal/mcpjson.ConfigFlag:
	// the one top-level argument a composition prefix may carry.
	launchBoundaryMCPConfigFlag = "--mcp-config"
)

// LaunchBoundaryMCPAdmission carries the admitted MCP facts the boundary
// re-validates. A nil admission skips both validators. ConfigPathSet
// distinguishes an absent mcp section (skip the path check) from a present
// one (check it); an empty Inline composition is accepted exactly as the
// grammar accepts a zero composition.
type LaunchBoundaryMCPAdmission struct {
	// ConfigPath is the fragment mcp.path under admission. It is checked
	// only when ConfigPathSet is true.
	ConfigPath string
	// ConfigPathSet marks the mcp section present.
	ConfigPathSet bool
	// Inline is the composed --mcp-config prefix with the server metadata
	// the document must match.
	Inline Composition
}

// LaunchBoundaryMCPAdmissionError refuses MCP admission facts. It renders
// byte-identically to the sentence the launcher or the grammar reports: a
// fragment-shaped "fragment: ..." refusal for the config path, or the
// grammar's plain sentence for the inline document. Neither source frames a
// diagnostic code (the launcher preserves these bytes as provider evidence
// under plan_refused), so this type carries the sentence only.
type LaunchBoundaryMCPAdmissionError struct {
	Message string
}

func (e *LaunchBoundaryMCPAdmissionError) Error() string { return e.Message }

// checkLaunchBoundaryMCPConfigPath ports the fragment mcp.path admission
// (launcher internal/fragment/fragment.go:473-488): the environment must
// carry an MCP channel (mcpRegistry, fragment.go:200-204), and the path must
// satisfy the schema's absolutePath rule (checkAbsolutePath,
// fragment.go:316-326). The launcher never stats the file here; this check is
// pure string validation and performs no filesystem reads.
func checkLaunchBoundaryMCPConfigPath(environment string, admission *LaunchBoundaryMCPAdmission) error {
	if admission == nil || !admission.ConfigPathSet {
		return nil
	}
	// The registry has no entry for pi or muse (and none for an unknown
	// environment, which the launcher's fragment parser refuses earlier):
	// a fragment carrying an mcp section there is rejected.
	if environment != "claude_code" && environment != "codex_cli" && environment != "opencode" {
		return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("fragment: /mcp: %s has no MCP channel", environment)}
	}
	path := admission.ConfigPath
	// The condition order is the launcher's verbatim: the rune-count
	// short-circuit guards the path[0] index on empty input.
	if n := utf8.RuneCountInString(path); n < 2 || n > 4096 || path[0] != '/' || strings.IndexByte(path, 0) >= 0 {
		return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("fragment: /mcp/path: %q is not an absolute path", path)}
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("fragment: /mcp/path: %q contains a .. segment", path)}
		}
	}
	return nil
}

// launchBoundaryMCPDocument mirrors the mcpjson root shape: the JSON
// document the prefix carries.
type launchBoundaryMCPDocument struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// launchBoundaryMCPEntry mirrors one mcpjson server entry inside that
// document. Tags are identical to the source's.
type launchBoundaryMCPEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
}

// checkLaunchBoundaryMCPInline transcribes internal/mcpjson.Validate
// (internal/mcpjson/mcpjson.go:96-153, byte-identical at v0.5.37), the
// module's own mcp-config-json grammar kept as an explicitly requested inline
// validator. It is distinct from the launcher's native reachable boundary
// (launcher internal/plan/plan.go:74-77 at fbcdbaf0 keeps Composition unset).
// Rule order, accept/refuse behavior, and message bytes are identical to the
// source.
// The encoding/json decoder accepts duplicate object keys (last wins) and
// enforces no document size limit; both acceptances are pinned by parity
// rows and recorded as follow-ups, never re-decided here.
func checkLaunchBoundaryMCPInline(admission *LaunchBoundaryMCPAdmission) error {
	if admission == nil {
		return nil
	}
	c := admission.Inline
	servers := make(map[string]CompositionServer, len(c.Servers))
	for _, server := range c.Servers {
		if strings.TrimSpace(server.Name) == "" {
			return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("%s composition carries an unnamed MCP server", launchBoundaryMCPGrammar)}
		}
		if _, duplicate := servers[server.Name]; duplicate {
			return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("%s composition declares the MCP server %q twice", launchBoundaryMCPGrammar, server.Name)}
		}
		servers[server.Name] = server
	}
	prefix := c.Prefix
	if len(prefix) == 0 {
		if len(servers) == 0 {
			return nil
		}
		return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("%s MCP metadata has no config argument", launchBoundaryMCPGrammar)}
	}
	if len(prefix) != 2 || prefix[0] != launchBoundaryMCPConfigFlag {
		return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("%s composition must be exactly one %s pair", launchBoundaryMCPGrammar, launchBoundaryMCPConfigFlag)}
	}
	var document launchBoundaryMCPDocument
	if err := decodeLaunchBoundaryStrictJSON(prefix[1], &document); err != nil || document.MCPServers == nil || len(document.MCPServers) != len(servers) {
		return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("invalid %s MCP config root", launchBoundaryMCPGrammar)}
	}
	for name, raw := range document.MCPServers {
		server, exists := servers[name]
		if !exists {
			return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("%s composition references unknown MCP server", launchBoundaryMCPGrammar)}
		}
		var declared launchBoundaryMCPEntry
		if err := decodeLaunchBoundaryStrictJSON(string(raw), &declared); err != nil || declared.Type != server.Transport {
			return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("invalid %s MCP entry", launchBoundaryMCPGrammar)}
		}
		if server.Transport == "http" {
			if strings.TrimSpace(declared.URL) == "" || declared.Command != "" || len(declared.Args) != 0 {
				return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("invalid %s HTTP MCP shape", launchBoundaryMCPGrammar)}
			}
			if server.BearerTokenEnvVar == "" {
				if len(declared.Headers) != 0 {
					return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("unexpected %s HTTP headers", launchBoundaryMCPGrammar)}
				}
				continue
			}
			expected := "Bearer ${" + server.BearerTokenEnvVar + "}"
			if len(declared.Headers) != 1 || declared.Headers["Authorization"] != expected {
				return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("invalid %s bearer environment reference", launchBoundaryMCPGrammar)}
			}
			continue
		}
		if strings.TrimSpace(declared.Command) == "" || declared.URL != "" || len(declared.Headers) != 0 {
			return &LaunchBoundaryMCPAdmissionError{Message: fmt.Sprintf("invalid %s stdio MCP shape", launchBoundaryMCPGrammar)}
		}
	}
	return nil
}

// decodeLaunchBoundaryStrictJSON transcribes internal/mcpjson.DecodeStrictJSON
// (internal/mcpjson/mcpjson.go:163-174): unknown fields and any trailing
// content after the first document are refused.
func decodeLaunchBoundaryStrictJSON(value string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}
