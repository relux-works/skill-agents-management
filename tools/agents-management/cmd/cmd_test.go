package cmd

import (
	"bytes"
	"testing"
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
	})

	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}

// withRegisteredPlugins swaps the compiled-in plugin set for the duration of
// a test. There is no registry type yet on purpose; the plugin contracts are
// follow-up work.
func withRegisteredPlugins(t *testing.T, names ...string) {
	t.Helper()
	previous := registeredPlugins
	registeredPlugins = names
	t.Cleanup(func() { registeredPlugins = previous })
}
