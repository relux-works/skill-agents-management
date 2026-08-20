package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// runRoot drives the real root command the way main() does, and restores the
// package-level command state afterwards so tests stay order-independent.
func runRoot(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)

	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		pluginsJSON = false
		_ = pluginsCmd.Flags().Set("json", "false")
		vendorsJSON = false
		_ = vendorsCmd.Flags().Set("json", "false")
		runtimesJSON = false
		_ = runtimesCmd.Flags().Set("json", "false")
	})

	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}

// withRegisteredPlugins swaps this binary's registry for an isolated one
// holding the named systems, for the duration of a test. It registers through
// the same public API a real plugin's init would use — there is no test-only
// path into the registry, so what these tests exercise is what ships.
func withRegisteredPlugins(t *testing.T, ids ...string) *agentic.Registry {
	t.Helper()
	registry := agentic.NewRegistry()
	for _, id := range ids {
		if err := registry.Register(stubSystem{id: agentic.SystemID(id)}); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
	}
	previous := systemRegistry
	systemRegistry = registry
	t.Cleanup(func() { systemRegistry = previous })
	return registry
}

// stubSystem is a minimal agentic.System. Its second job is to prove the
// contract is implementable from outside its own package: an interface with an
// unexported method could not be satisfied here, and a plugin living in
// another repository would discover that far later than this test does.
type stubSystem struct{ id agentic.SystemID }

func (s stubSystem) ID() agentic.SystemID { return s.id }

func (s stubSystem) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{LaunchModes: []agentic.LaunchMode{agentic.LaunchModeExec}}
}

func (s stubSystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return "/usr/bin/" + s.id.String(), nil
}

func (s stubSystem) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return nil, nil
}

func (s stubSystem) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	return parent, nil
}

func (s stubSystem) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}

func (s stubSystem) ValidateComposition(agentic.Composition) error {
	return errors.New("stub system declares no composition grammar")
}
