package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/spf13/cobra"
)

// systemRegistry is where this binary reads its plugins from. It points at the
// package-level default registry that plugin packages register into from their
// init functions, so the command has no list of its own.
//
// A private list here would be a second binding for the same fact — the exact
// shadow table docs/architecture.md's invariant 5 forbids and
// pkg/agentic/singlesource_guard_test.go fails the build over. The variable
// exists only so a test can swap in an isolated registry.
var systemRegistry = agentic.Default

var pluginsJSON bool

var pluginsCmd = &cobra.Command{
	Use:   "plugins",
	Short: "List the plugins registered in this binary",
	Long: "List the plugins registered in this binary, one name per line.\n\n" +
		"An empty list is a valid answer and exits 0: it means no plugin has\n" +
		"been compiled in yet, which is different from a failure to look.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		names := pluginNames()
		out := cmd.OutOrStdout()
		if pluginsJSON {
			encoded, err := json.Marshal(names)
			if err != nil {
				return fmt.Errorf("encoding plugin list: %w", err)
			}
			fmt.Fprintf(out, "%s\n", encoded)
			return nil
		}
		for _, name := range names {
			fmt.Fprintln(out, name)
		}
		return nil
	},
}

// pluginNames returns the registered agentic system ids in a stable order. The
// result is always non-nil so the JSON encoding of "nothing registered" is []
// and never null — a consumer ranging over the answer must not have to
// special-case the empty case.
func pluginNames() []string {
	ids := systemRegistry.IDs()
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, id.String())
	}
	return names
}

func init() {
	pluginsCmd.Flags().BoolVar(&pluginsJSON, "json", false, "Emit the list as a JSON array")
	rootCmd.AddCommand(pluginsCmd)
}
