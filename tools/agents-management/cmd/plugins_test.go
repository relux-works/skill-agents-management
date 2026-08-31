package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// An unpopulated binary must answer "nothing registered" and exit 0. Treating
// the empty set as a failure would make every consumer unable to tell "no
// plugins compiled in" from "the lookup broke".
func TestPluginsCommandEmptyListSucceeds(t *testing.T) {
	withRegisteredPlugins(t)

	stdout, stderr, err := runRoot(t, "plugins")
	if err != nil {
		t.Fatalf("plugins command errored on an empty registry: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// json.Marshal of a nil slice is `null`, of an empty slice `[]`. A consumer
// ranging over the answer must not have to special-case the empty registry,
// so the empty answer is pinned to `[]`.
func TestPluginsCommandEmptyListEncodesAsEmptyArray(t *testing.T) {
	withRegisteredPlugins(t)

	stdout, _, err := runRoot(t, "plugins", "--json")
	if err != nil {
		t.Fatalf("plugins --json errored on an empty registry: %v", err)
	}
	if stdout != "[]\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "[]\n")
	}

	var decoded []string
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if decoded == nil {
		t.Error("decoded to nil; the empty registry must encode as [] and never null")
	}
	if len(decoded) != 0 {
		t.Errorf("decoded = %v, want empty", decoded)
	}
}

// The empty answer must mean "the registry is empty", not "this command
// always prints nothing" — so a populated registry has to come back out.
func TestPluginsCommandListsRegisteredNamesSorted(t *testing.T) {
	withRegisteredPlugins(t, "vendor-openai", "claude-code", "vendor-anthropic")

	stdout, _, err := runRoot(t, "plugins")
	if err != nil {
		t.Fatalf("plugins command failed: %v", err)
	}
	want := "claude-code\nvendor-anthropic\nvendor-openai\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestPluginsCommandJSONListsRegisteredNamesSorted(t *testing.T) {
	withRegisteredPlugins(t, "vendor-openai", "claude-code")

	stdout, _, err := runRoot(t, "plugins", "--json")
	if err != nil {
		t.Fatalf("plugins --json failed: %v", err)
	}
	var decoded []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &decoded); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	want := []string{"claude-code", "vendor-openai"}
	if len(decoded) != len(want) {
		t.Fatalf("decoded = %v, want %v", decoded, want)
	}
	for i := range want {
		if decoded[i] != want[i] {
			t.Fatalf("decoded = %v, want %v", decoded, want)
		}
	}
}

// pluginNames hands out a copy: a caller that sorts, appends to or overwrites
// the answer must not be able to reorder or grow the compiled-in set.
func TestPluginNamesDoesNotAliasRegistry(t *testing.T) {
	registry := withRegisteredPlugins(t, "vendor-openai", "claude-code")

	names := pluginNames()
	names[0] = "mutated"

	if got := registry.IDs(); len(got) != 2 || got[0] != "claude-code" || got[1] != "vendor-openai" {
		t.Errorf("registry ids = %v after mutating pluginNames' result; the answer aliased the registry", got)
	}
	if again := pluginNames(); again[0] != "claude-code" {
		t.Errorf("pluginNames() = %v on the second call; the first caller's write survived", again)
	}
}

// The command reports what the registry holds, so a system registered through
// the public API — the only way a binding exists — comes back out under its
// id, and there is no second path that could print something else.
func TestPluginsCommandReportsWhatTheRegistryHolds(t *testing.T) {
	withRegisteredPlugins(t, "claude-code", "codex")

	stdout, _, err := runRoot(t, "plugins")
	if err != nil {
		t.Fatalf("plugins command failed: %v", err)
	}
	if stdout != "claude-code\ncodex\n" {
		t.Errorf("stdout = %q, want the registered ids in sorted order", stdout)
	}
}

// The CLI can never print a non-canonical spelling, because a plugin that
// answers one cannot be registered at all — agentic.ErrUnnormalizedSystemID,
// enforced at the registration boundary this command reads through.
//
// This is the same property an earlier version of the test above asserted by
// registering "Claude-Code" and watching "claude-code" come out. Under the
// stricter contract that registration is refused outright, so the property is
// asserted where it now lives: at the boundary, driven from outside the
// agentic package by the same stub a real out-of-repo plugin would be.
func TestCLICannotSurfaceAnUnnormalizedPluginID(t *testing.T) {
	registry := agentic.NewRegistry()
	err := registry.Register(stubSystem{id: "Claude-Code"})
	if !errors.Is(err, agentic.ErrUnnormalizedSystemID) {
		t.Fatalf("Register(\"Claude-Code\") err = %v, want ErrUnnormalizedSystemID", err)
	}

	previous := systemRegistry
	systemRegistry = registry
	t.Cleanup(func() { systemRegistry = previous })

	stdout, _, err := runRoot(t, "plugins")
	if err != nil {
		t.Fatalf("plugins command failed: %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing: the refused plugin must not reach the CLI under either spelling", stdout)
	}
}
