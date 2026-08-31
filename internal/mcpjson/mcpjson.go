// Package mcpjson is the `--mcp-config <json>` launch-composition grammar,
// shared by every system whose harness accepts one.
//
// # Why it is shared rather than copied
//
// The extraction source validates claude, qwen and agy compositions with ONE
// function — validateClaudeLaunchCompositionPrefix, reached through a single
// CompositionGrammarMCPConfigJSON table entry for all three
// (skill-project-management, tools/board-cli/internal/spawn/launch_composition.go).
// The claude plugin's port said the same thing before any second system
// existed: the grammar id "names the GRAMMAR, not this system... When those
// plugins are ported they declare this same grammar id, and the validator they
// reach has to be this one rather than a second copy".
//
// This package is that one validator, moved out of the claude plugin so the
// qwen and agy plugins reach it without importing another system's plugin. It
// is the same extraction internal/argvguard and internal/launchenv record: a
// second implementation of one rule, even one written by copying the first, is
// two definitions that drift the moment either is extended — and for a REFUSAL
// surface, the drift is a shape one system admits and another rejects, with
// nobody able to say which is the contract.
//
// # Every rule here is a refusal
//
// The prefix this validates is spliced straight into a launch, so each rule is
// worth reading as the thing it stops rather than as a shape it requires:
//
//   - EXACTLY one --mcp-config pair: a prefix that can carry a second top-level
//     argument is a prefix that can change the model, the permission mode or
//     the budget of a launch that was reviewed as an MCP composition. This
//     grammar is a single JSON blob, so unlike codex's `-c` pairs there is no
//     legitimate second argument at all.
//   - Strict JSON decoding: unknown fields and trailing content are refused. A
//     permissive decoder would let an unreviewed key ride into the child's MCP
//     configuration under a shape the validator believes it has checked.
//   - One entry per declared server, and no entry for a server the caller did
//     not carry: a composition naming a server the metadata does not is a
//     config override wearing a server's shape.
//   - Per-transport shape: an http server carries a url and no command/args, a
//     stdio server carries a command and no url/headers.
//   - Bearer references that MATCH the server's declared metadata in BOTH
//     directions — a server declaring no bearer must carry no headers, and one
//     declaring a bearer must carry exactly the single Authorization header
//     naming that variable. Half of this check is what
//     agentic.CompositionServer.BearerTokenEnvVar exists to make expressible;
//     the other half is why the field is checked for absence too.
//
// # The error text names the grammar, not a system
//
// The source's messages say "Claude" even when the composition being refused
// belongs to qwen or agy, because one function served all three. Reproducing
// that here would send an operator debugging an agy launch to the claude
// plugin. The messages name the grammar instead; the SHAPES they refuse are the
// source's, rule for rule, and that is what the callers' tests compare.
package mcpjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Grammar is the composition-grammar identifier a system declares to say it
// accepts this shape. It is an opaque identifier the core never interprets,
// which is the reshape pkg/agentic documents on GrammarID: in the source this
// was an enum in core, so a harness with a new composition shape was a core
// edit.
const Grammar agentic.GrammarID = "mcp-config-json"

// ConfigFlag is the one top-level argument a composition prefix may carry.
const ConfigFlag = "--mcp-config"

// root is the shape of the JSON document the prefix carries.
type root struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// entry is one server inside that document.
type entry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
}

// Validate refuses a composition whose prefix does not match this grammar.
//
// A ZERO composition is accepted here and by every caller: absence and an empty
// composition are the same fact, and pkg/agentic refuses a non-zero composition
// against a system declaring no grammar before it ever reaches a plugin.
func Validate(c agentic.Composition) error {
	servers := make(map[string]agentic.CompositionServer, len(c.Servers))
	for _, server := range c.Servers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("%s composition carries an unnamed MCP server", Grammar)
		}
		if _, duplicate := servers[server.Name]; duplicate {
			return fmt.Errorf("%s composition declares the MCP server %q twice", Grammar, server.Name)
		}
		servers[server.Name] = server
	}

	prefix := c.Prefix
	if len(prefix) == 0 {
		if len(servers) == 0 {
			return nil
		}
		return fmt.Errorf("%s MCP metadata has no config argument", Grammar)
	}
	if len(prefix) != 2 || prefix[0] != ConfigFlag {
		return fmt.Errorf("%s composition must be exactly one %s pair", Grammar, ConfigFlag)
	}

	var document root
	if err := DecodeStrictJSON(prefix[1], &document); err != nil || document.MCPServers == nil || len(document.MCPServers) != len(servers) {
		return fmt.Errorf("invalid %s MCP config root", Grammar)
	}
	for name, raw := range document.MCPServers {
		server, exists := servers[name]
		if !exists {
			return fmt.Errorf("%s composition references unknown MCP server", Grammar)
		}
		var declared entry
		if err := DecodeStrictJSON(string(raw), &declared); err != nil || declared.Type != server.Transport {
			return fmt.Errorf("invalid %s MCP entry", Grammar)
		}
		if server.Transport == "http" {
			if strings.TrimSpace(declared.URL) == "" || declared.Command != "" || len(declared.Args) != 0 {
				return fmt.Errorf("invalid %s HTTP MCP shape", Grammar)
			}
			if server.BearerTokenEnvVar == "" {
				if len(declared.Headers) != 0 {
					return fmt.Errorf("unexpected %s HTTP headers", Grammar)
				}
				continue
			}
			expected := "Bearer ${" + server.BearerTokenEnvVar + "}"
			if len(declared.Headers) != 1 || declared.Headers["Authorization"] != expected {
				return fmt.Errorf("invalid %s bearer environment reference", Grammar)
			}
			continue
		}
		if strings.TrimSpace(declared.Command) == "" || declared.URL != "" || len(declared.Headers) != 0 {
			return fmt.Errorf("invalid %s stdio MCP shape", Grammar)
		}
	}
	return nil
}

// DecodeStrictJSON decodes value into target, refusing unknown fields and any
// trailing content after the first document.
//
// It is the source's decodeStrictJSON verbatim. The trailing-content check is
// the half that is easy to drop and expensive to lose: encoding/json's Decoder
// happily reads one document and ignores whatever follows, so a prefix carrying
// `{...valid...} {...smuggled...}` would validate on the first and hand both to
// the child.
func DecodeStrictJSON(value string, target any) error {
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
