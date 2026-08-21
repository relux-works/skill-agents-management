package cmd

import (
	"encoding/json"
	"strings"
	"testing"
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

// pluginNames hands out a copy: a caller that sorts or appends to the answer
// must not be able to reorder or grow the compiled-in set.
func TestPluginNamesDoesNotAliasRegistry(t *testing.T) {
	withRegisteredPlugins(t, "vendor-openai", "claude-code")

	names := pluginNames()
	names[0] = "mutated"

	if registeredPlugins[0] != "vendor-openai" {
		t.Errorf("registeredPlugins[0] = %q, want %q: pluginNames aliased the registry",
			registeredPlugins[0], "vendor-openai")
	}
}
