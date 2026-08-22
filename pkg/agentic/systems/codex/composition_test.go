package codex

import (
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The composition validator is a REFUSAL surface: whatever it admits is
// spliced verbatim into a launch's argv. So the tests that matter here are the
// negative ones, and the positive case exists mainly to prove the negatives are
// not passing because the validator refuses everything.
//
// No golden covers composition — the source's parity capture built no
// composed launch — so this file is the only evidence this grammar has.

func httpComposition() agentic.Composition {
	return agentic.Composition{
		Prefix: []string{
			"-c", `mcp_servers.board.url="http://127.0.0.1:9124/mcp"`,
			"-c", `mcp_servers.board.bearer_token_env_var="BOARD_TOKEN"`,
		},
		Servers: []agentic.CompositionServer{
			{Name: "board", Transport: "http", BearerTokenEnvVar: "BOARD_TOKEN"},
		},
	}
}

func stdioComposition() agentic.Composition {
	return agentic.Composition{
		Prefix: []string{
			"-c", `mcp_servers.local.command="/usr/local/bin/mcp-local"`,
			"-c", `mcp_servers.local.args=["--stdio", "--quiet"]`,
		},
		Servers: []agentic.CompositionServer{{Name: "local", Transport: "stdio"}},
	}
}

// TestAValidCompositionIsAdmitted is the reachability half. Without it, every
// refusal below would be equally consistent with a validator that says no to
// everything.
func TestAValidCompositionIsAdmitted(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]agentic.Composition{
		"http with a bearer": httpComposition(),
		"stdio with args":    stdioComposition(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := New().ValidateComposition(c); err != nil {
				t.Errorf("a valid composition was refused: %v", err)
			}
		})
	}
}

// TestAnAbsentCompositionIsNotAnInvalidOne holds the difference between "no
// composition" and "an empty one that failed validation".
func TestAnAbsentCompositionIsNotAnInvalidOne(t *testing.T) {
	t.Parallel()
	if err := New().ValidateComposition(agentic.Composition{}); err != nil {
		t.Errorf("the zero composition was refused: %v", err)
	}
}

// TestCompositionRefusals is the negative set. Each case is a prefix a composer
// could plausibly emit, and each one names what admitting it would let through.
func TestCompositionRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		lets    string
		mutate  func(*agentic.Composition)
		compose func() agentic.Composition
	}{
		{
			name:   "an odd number of prefix elements",
			lets:   "a dangling argument whose meaning depends on what codex does with an unpaired -c",
			mutate: func(c *agentic.Composition) { c.Prefix = c.Prefix[:len(c.Prefix)-1] },
		},
		{
			name:   "a top-level flag smuggled between the pairs",
			lets:   "an arbitrary codex flag — a different sandbox, a different model, a different approval policy — into a launch reviewed as an MCP composition",
			mutate: func(c *agentic.Composition) { c.Prefix[0] = "--sandbox" },
		},
		{
			name:   "an assignment outside mcp_servers",
			lets:   "any codex config key at all to be set by something that only claimed to add an MCP server",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = `sandbox_workspace_write.network_access=true` },
		},
		{
			name:   "an assignment with no value",
			lets:   "a malformed override whose effect on codex's config parser nobody has established",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = "mcp_servers.board.url" },
		},
		{
			name:   "a newline inside the value",
			lets:   "a second config line to be injected through a value that reads as one",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = "mcp_servers.board.url=\"http://x\"\nother=1" },
		},
		{
			name:   "a key with no field after the server name",
			lets:   "an assignment to a server TABLE rather than to one of its fields",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = `mcp_servers.board="whatever"` },
		},
		{
			name:   "a server nobody declared",
			lets:   "config for a server the caller never carried, which is an override wearing a server's shape",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = `mcp_servers.ghost.url="http://127.0.0.1:9124/mcp"` },
		},
		{
			name: "a field outside the closed set, added to an otherwise valid composition",
			lets: "an unreviewed per-server key reaching the harness",
			// ADDED rather than substituted. Replacing the url with the
			// unknown field is refused by the per-transport shape rule
			// instead — "an http server needs a url" — so that spelling
			// leaves the closed-set check itself unproven. A mutation run
			// caught exactly that: disabling the field check left the suite
			// green until this case existed.
			mutate: func(c *agentic.Composition) {
				c.Prefix = append(c.Prefix, "-c", `mcp_servers.board.env="LEAK=1"`)
			},
		},
		{
			name:   "an unknown field standing IN for a required one",
			lets:   "an http server composed with no url at all, under a key nobody reviewed",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = `mcp_servers.board.env="LEAK=1"` },
		},
		{
			name: "the same field assigned twice",
			lets: "two values for one field, where which one wins is codex's parser's business rather than the reviewer's",
			mutate: func(c *agentic.Composition) {
				c.Prefix = append(c.Prefix, "-c", `mcp_servers.board.url="http://evil.invalid/mcp"`)
			},
		},
		{
			name:   "an unquoted value",
			lets:   "a bare token where TOML expects a string, which parses as something else or not at all",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = `mcp_servers.board.url=http://127.0.0.1:9124/mcp` },
		},
		{
			name:   "a quoted but blank value",
			lets:   "a server configured with an empty url, which fails at launch rather than at validation",
			mutate: func(c *agentic.Composition) { c.Prefix[1] = `mcp_servers.board.url="   "` },
		},
		{
			name: "a bearer reference the server did not declare",
			lets: "the composition to point codex at a DIFFERENT environment variable than the one the server's metadata authorized",
			mutate: func(c *agentic.Composition) {
				c.Prefix[3] = `mcp_servers.board.bearer_token_env_var="SOME_OTHER_TOKEN"`
			},
		},
		{
			name: "a bearer in the prefix that the metadata does not have",
			lets: "a bearer to be attached to a server the caller declared as needing none",
			mutate: func(c *agentic.Composition) {
				c.Servers[0].BearerTokenEnvVar = ""
				c.Prefix[3] = `mcp_servers.board.bearer_token_env_var=""`
			},
		},
		{
			name: "metadata declaring a bearer the prefix omits",
			lets: "a server that needs authentication to be composed without it, which fails at request time",
			mutate: func(c *agentic.Composition) {
				c.Prefix = c.Prefix[:2]
			},
		},
		{
			name: "an http server carrying a command",
			lets: "a process to be spawned for a server declared as a remote endpoint",
			mutate: func(c *agentic.Composition) {
				c.Prefix = append(c.Prefix, "-c", `mcp_servers.board.command="/bin/sh"`)
			},
		},
		{
			name:    "a stdio server carrying a url",
			lets:    "a stdio server to be pointed at a network endpoint instead",
			compose: stdioComposition,
			mutate: func(c *agentic.Composition) {
				c.Prefix = append(c.Prefix, "-c", `mcp_servers.local.url="http://127.0.0.1:9124/mcp"`)
			},
		},
		{
			name:    "a stdio server with no command",
			lets:    "a stdio server with nothing to run",
			compose: stdioComposition,
			mutate:  func(c *agentic.Composition) { c.Prefix = c.Prefix[2:] },
		},
		{
			name:    "args that are not a TOML string array",
			lets:    "a value codex's own parser rejects, or reads as something other than an argument list",
			compose: stdioComposition,
			mutate:  func(c *agentic.Composition) { c.Prefix[3] = `mcp_servers.local.args={"not":"an array"}` },
		},
		{
			name:    "args holding a number rather than a string",
			lets:    "an argument list whose elements are not arguments",
			compose: stdioComposition,
			mutate:  func(c *agentic.Composition) { c.Prefix[3] = `mcp_servers.local.args=[1, 2]` },
		},
		{
			name: "two servers declared under one name",
			lets: "one composition entry to silently shadow another, so the reviewed set and the launched set differ",
			mutate: func(c *agentic.Composition) {
				c.Servers = append(c.Servers, agentic.CompositionServer{Name: "board", Transport: "stdio"})
			},
		},
		{
			name:   "an unnamed server",
			lets:   "a server that no assignment can refer to and that no per-transport check can be attributed to",
			mutate: func(c *agentic.Composition) { c.Servers[0].Name = "  " },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compose := tc.compose
			if compose == nil {
				compose = httpComposition
			}
			// The unmutated composition must be admitted first, or a "refused"
			// mutant could be refused for a reason unrelated to its defect.
			clean := compose()
			if err := New().ValidateComposition(clean); err != nil {
				t.Fatalf("the unmutated composition is already refused (%v), so this case proves nothing", err)
			}

			c := compose()
			tc.mutate(&c)
			if err := New().ValidateComposition(c); err == nil {
				t.Errorf("the validator admitted %s, which lets %s", tc.name, tc.lets)
			}
		})
	}
}

// TestTheGrammarIsDeclaredAndEnforcedThroughBuildPlan proves the validator is
// reached from production rather than only from this file.
//
// Both directions are asserted: a valid composition survives BuildPlan, and an
// invalid one is refused by it. A validator that is unit-tested but never
// called from the dispatch promises nothing about a launch.
func TestTheGrammarIsDeclaredAndEnforcedThroughBuildPlan(t *testing.T) {
	t.Parallel()
	if got := New().Capabilities().CompositionGrammar; got != GrammarTOMLConfigPairs {
		t.Fatalf("CompositionGrammar = %q, want %q", got, GrammarTOMLConfigPairs)
	}
	if GrammarTOMLConfigPairs == agentic.GrammarNone {
		t.Fatal("the declared grammar is GrammarNone, which would make BuildPlan refuse every composition before this plugin sees one")
	}

	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	base := parityRequest(workDir)
	base.PromptPath = writePromptFile(t, workDir, "composition prompt")
	base.Env = []string{"PATH=" + binDir}

	valid := base
	valid.Composition = httpComposition()
	plan := buildParityPlan(t, New(), valid, agentic.LaunchModeExec)
	if len(plan.Argv) == 0 || plan.Argv[0] != "-c" {
		t.Errorf("the validated composition did not reach the argv: %#v", plan.Argv)
	}

	invalid := base
	invalid.Composition = httpComposition()
	invalid.Composition.Prefix[0] = "--sandbox"

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := agentic.BuildPlan(registry, invalid, agentic.LaunchModeExec); err == nil {
		t.Fatal("BuildPlan admitted a composition smuggling a top-level codex flag; the grammar is declared but not enforced on the dispatch path")
	} else if !strings.Contains(err.Error(), "composition") {
		t.Errorf("BuildPlan refused for something other than the composition: %v", err)
	}
}

// TestValidateTOMLStringArrayUsesARealParser narrows the args check onto the
// reason it takes a dependency: the shapes a hand-rolled reader would disagree
// with the real codex config parser about.
func TestValidateTOMLStringArrayUsesARealParser(t *testing.T) {
	t.Parallel()
	valid := []string{
		`["--stdio"]`,
		`[]`,
		`["a", "b",]`,
		"[\n  \"a\",\n  \"b\",\n]",
		`['literal string']`,
	}
	for _, value := range valid {
		if err := validateTOMLStringArray(value); err != nil {
			t.Errorf("validateTOMLStringArray(%q) refused a valid TOML string array: %v", value, err)
		}
	}
	invalid := []string{`[1]`, `["unterminated`, `{"a": "b"}`, `not-an-array`, `[true]`}
	for _, value := range invalid {
		if err := validateTOMLStringArray(value); err == nil {
			t.Errorf("validateTOMLStringArray(%q) admitted a value that is not a TOML string array", value)
		}
	}
}

// TestCompositionErrorsAreNotWrappedSentinels records what these refusals are
// and are not: they carry human-readable reasons, not a sentinel a caller
// branches on. BuildPlan wraps them with the system id and the grammar, which
// is the layer that gives an operator somewhere to look.
func TestCompositionErrorsAreNotWrappedSentinels(t *testing.T) {
	t.Parallel()
	c := httpComposition()
	c.Prefix[0] = "--sandbox"
	err := New().ValidateComposition(c)
	if err == nil {
		t.Fatal("the smuggled flag was admitted")
	}
	if errors.Unwrap(err) != nil {
		t.Errorf("composition refusals are expected to be leaf errors; %v wraps something, which suggests a sentinel a caller could branch on without one being declared", err)
	}
}
