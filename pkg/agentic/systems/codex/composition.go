package codex

import (
	"fmt"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// GrammarTOMLConfigPairs names codex's launch-composition argv shape: a flat
// sequence of `-c mcp_servers.<server>.<field>=<toml-value>` pairs.
//
// It is an opaque identifier the core never interprets, which is the reshape
// the core documents on GrammarID: in the source this was a CompositionGrammar
// enum in core, so a harness with a new composition shape was a core edit.
const GrammarTOMLConfigPairs agentic.GrammarID = "codex-toml-config-pairs"

// compositionFields is the closed set of MCP fields a composition may assign.
// Anything else is refused rather than passed through: the prefix is spliced
// straight into a launch, so an unknown key here is an unreviewed config
// override reaching the harness under the name of an MCP server.
var compositionFields = map[string]bool{
	"url":                  true,
	"bearer_token_env_var": true,
	"command":              true,
	"args":                 true,
}

// validateComposition refuses a composition whose prefix does not match
// codex's grammar. It is the source's validateCodexLaunchCompositionPrefix
// (launch_composition.go), ported rule for rule.
//
// Everything it checks is a REFUSAL surface, so each rule is worth reading as
// the thing it stops rather than as a shape it requires:
//
//   - Pairs, and every even element exactly "-c": a prefix that can smuggle a
//     top-level codex flag between the pairs is a prefix that can change the
//     sandbox, the model or the approval policy of a launch that was reviewed
//     as an MCP composition.
//   - Keys inside mcp_servers. only: the same escape one level down, into
//     arbitrary codex config.
//   - Server names that were actually declared: a composition naming a server
//     the caller did not carry is a config override wearing a server's shape.
//   - Field names from the closed set above, no duplicates per server.
//   - Bearer references that MATCH the server's declared metadata, in both
//     directions — a prefix naming a different variable than the server
//     declared is refused, and an http server whose metadata and prefix
//     disagree about whether a bearer exists at all is refused too.
//   - Per-transport shape: an http server carries a url and no command/args, a
//     stdio server carries a command and no url/bearer.
func validateComposition(c agentic.Composition) error {
	servers := make(map[string]agentic.CompositionServer, len(c.Servers))
	for _, server := range c.Servers {
		name := server.Name
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("codex composition carries an unnamed MCP server")
		}
		if _, duplicate := servers[name]; duplicate {
			return fmt.Errorf("codex composition declares the MCP server %q twice", name)
		}
		servers[name] = server
	}

	prefix := c.Prefix
	if len(prefix)%2 != 0 {
		return fmt.Errorf("codex composition must contain -c pairs")
	}
	seen := make(map[string]map[string]string, len(servers))
	for i := 0; i < len(prefix); i += 2 {
		if prefix[i] != "-c" {
			return fmt.Errorf("codex composition contains a disallowed top-level argument")
		}
		key, value, ok := strings.Cut(prefix[i+1], "=")
		if !ok || strings.ContainsAny(key, "\r\n\t ") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("invalid codex composition assignment")
		}
		const keyPrefix = "mcp_servers."
		if !strings.HasPrefix(key, keyPrefix) {
			return fmt.Errorf("codex composition assignment is outside mcp_servers")
		}
		remainder := strings.TrimPrefix(key, keyPrefix)
		dot := strings.LastIndex(remainder, ".")
		if dot <= 0 || dot == len(remainder)-1 {
			return fmt.Errorf("invalid codex MCP assignment key")
		}
		serverName, leaf := remainder[:dot], remainder[dot+1:]
		server, exists := servers[serverName]
		if !exists {
			return fmt.Errorf("codex composition references unknown MCP server")
		}
		if !compositionFields[leaf] {
			return fmt.Errorf("codex composition contains a disallowed MCP field")
		}
		if seen[serverName] == nil {
			seen[serverName] = map[string]string{}
		}
		if _, duplicate := seen[serverName][leaf]; duplicate {
			return fmt.Errorf("duplicate codex MCP field")
		}
		if leaf == "args" {
			if err := validateTOMLStringArray(value); err != nil {
				return fmt.Errorf("invalid codex MCP args value")
			}
		} else {
			decoded, err := strconv.Unquote(value)
			if err != nil || strings.TrimSpace(decoded) == "" {
				return fmt.Errorf("invalid quoted codex MCP value")
			}
			if leaf == "bearer_token_env_var" && decoded != server.BearerTokenEnvVar {
				return fmt.Errorf("codex bearer environment reference mismatch")
			}
		}
		seen[serverName][leaf] = value
	}
	for name, server := range servers {
		fields := seen[name]
		if server.Transport == "http" {
			if fields["url"] == "" || fields["command"] != "" || fields["args"] != "" {
				return fmt.Errorf("invalid codex HTTP MCP shape")
			}
			if (server.BearerTokenEnvVar == "") != (fields["bearer_token_env_var"] == "") {
				return fmt.Errorf("codex bearer metadata mismatch")
			}
			continue
		}
		if fields["command"] == "" || fields["url"] != "" || fields["bearer_token_env_var"] != "" {
			return fmt.Errorf("invalid codex stdio MCP shape")
		}
	}
	return nil
}

// validateTOMLStringArray reports whether value is a TOML array of strings.
//
// It parses with the same library and version the source used
// (github.com/pelletier/go-toml/v2 v2.4.3) rather than with a hand-written
// check, because the prefix is written by an external composer in TOML and a
// hand-rolled reader would disagree with the real parser at the edges — a
// multi-line array, a literal single-quoted string, a trailing comma. A
// validating gate that disagrees with the thing it is validating for is worse
// than no gate: it refuses valid launches and admits shapes it never modelled.
func validateTOMLStringArray(value string) error {
	var document struct {
		Args []string `toml:"args"`
	}
	return toml.Unmarshal([]byte("args = "+value), &document)
}
