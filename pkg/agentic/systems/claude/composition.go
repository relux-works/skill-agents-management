package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// GrammarMCPConfigJSON names the launch-composition argv shape claude accepts:
// exactly one `--mcp-config <json>` pair, whose JSON document declares the same
// MCP servers the composition carries as metadata.
//
// The identifier names the GRAMMAR, not this system, and that is deliberate:
// the source's CompositionGrammarMCPConfigJSON is shared by claude, qwen and
// agy, validated by one function for all three. When those plugins are ported
// they declare this same grammar id, and the validator they reach has to be
// this one rather than a second copy — which is why the id and the validator
// are named for the shape rather than for the first system that used it.
//
// It is an opaque identifier the core never interprets, which is the reshape
// the core documents on GrammarID: in the source this was an enum in core, so a
// harness with a new composition shape was a core edit.
const GrammarMCPConfigJSON agentic.GrammarID = "mcp-config-json"

// mcpConfigFlag is the one top-level argument a claude composition prefix may
// carry.
const mcpConfigFlag = "--mcp-config"

// compositionRoot is the shape of the JSON document the prefix carries.
type compositionRoot struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// compositionEntry is one server inside that document.
type compositionEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
}

// validateComposition refuses a composition whose prefix does not match
// claude's grammar. It is the source's validateClaudeLaunchCompositionPrefix
// (launch_composition.go), ported rule for rule.
//
// Everything it checks is a REFUSAL surface, so each rule is worth reading as
// the thing it stops rather than as a shape it requires:
//
//   - EXACTLY one --mcp-config pair: a prefix that can carry a second top-level
//     argument is a prefix that can change the model, the permission mode or
//     the budget of a launch that was reviewed as an MCP composition. Claude's
//     grammar is a single JSON blob, so unlike codex's `-c` pairs there is no
//     legitimate second argument at all.
//   - Strict JSON decoding: unknown fields and trailing content are refused.
//     A permissive decoder would let an unreviewed key ride into the child's
//     MCP configuration under a shape the validator believes it has checked.
//   - One entry per declared server, and no entry for a server the caller did
//     not carry: a composition naming a server the metadata does not is a
//     config override wearing a server's shape.
//   - Per-transport shape: an http server carries a url and no command/args, a
//     stdio server carries a command and no url/headers.
//   - Bearer references that MATCH the server's declared metadata in BOTH
//     directions — a server declaring no bearer must carry no headers, and one
//     declaring a bearer must carry exactly the single Authorization header
//     naming that variable. Half of this check is what the core's
//     CompositionServer.BearerTokenEnvVar field exists to make expressible; the
//     other half is why the field is checked for absence too.
func validateComposition(c agentic.Composition) error {
	servers := make(map[string]agentic.CompositionServer, len(c.Servers))
	for _, server := range c.Servers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("claude composition carries an unnamed MCP server")
		}
		if _, duplicate := servers[server.Name]; duplicate {
			return fmt.Errorf("claude composition declares the MCP server %q twice", server.Name)
		}
		servers[server.Name] = server
	}

	prefix := c.Prefix
	if len(prefix) == 0 {
		if len(servers) == 0 {
			return nil
		}
		return fmt.Errorf("claude MCP metadata has no config argument")
	}
	if len(prefix) != 2 || prefix[0] != mcpConfigFlag {
		return fmt.Errorf("claude composition must be exactly one %s pair", mcpConfigFlag)
	}

	var root compositionRoot
	if err := decodeStrictJSON(prefix[1], &root); err != nil || root.MCPServers == nil || len(root.MCPServers) != len(servers) {
		return fmt.Errorf("invalid claude MCP config root")
	}
	for name, raw := range root.MCPServers {
		server, exists := servers[name]
		if !exists {
			return fmt.Errorf("claude composition references unknown MCP server")
		}
		var entry compositionEntry
		if err := decodeStrictJSON(string(raw), &entry); err != nil || entry.Type != server.Transport {
			return fmt.Errorf("invalid claude MCP entry")
		}
		if server.Transport == "http" {
			if strings.TrimSpace(entry.URL) == "" || entry.Command != "" || len(entry.Args) != 0 {
				return fmt.Errorf("invalid claude HTTP MCP shape")
			}
			if server.BearerTokenEnvVar == "" {
				if len(entry.Headers) != 0 {
					return fmt.Errorf("unexpected claude HTTP headers")
				}
				continue
			}
			expected := "Bearer ${" + server.BearerTokenEnvVar + "}"
			if len(entry.Headers) != 1 || entry.Headers["Authorization"] != expected {
				return fmt.Errorf("invalid claude bearer environment reference")
			}
			continue
		}
		if strings.TrimSpace(entry.Command) == "" || entry.URL != "" || len(entry.Headers) != 0 {
			return fmt.Errorf("invalid claude stdio MCP shape")
		}
	}
	return nil
}

// decodeStrictJSON decodes value into target, refusing unknown fields and any
// trailing content after the first document.
//
// It is the source's decodeStrictJSON verbatim. The trailing-content check is
// the half that is easy to drop and expensive to lose: encoding/json's Decoder
// happily reads one document and ignores whatever follows, so a prefix carrying
// `{...valid...} {...smuggled...}` would validate on the first and hand both to
// the child.
func decodeStrictJSON(value string, target any) error {
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
