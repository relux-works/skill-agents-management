package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// registeredPlugins names the plugins compiled into this binary. It is empty
// today and that is a valid answer, not a failure: the plugin contracts land
// in follow-up work, and a consumer asking an unpopulated binary what it
// carries deserves an empty list and exit 0, never an error.
var registeredPlugins []string

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

// pluginNames returns the registered plugin names in a stable order. The
// result is always non-nil so the JSON encoding of "nothing registered" is
// [] and never null — a consumer ranging over the answer must not have to
// special-case the empty case.
func pluginNames() []string {
	names := make([]string, 0, len(registeredPlugins))
	names = append(names, registeredPlugins...)
	sort.Strings(names)
	return names
}

func init() {
	pluginsCmd.Flags().BoolVar(&pluginsJSON, "json", false, "Emit the list as a JSON array")
	rootCmd.AddCommand(pluginsCmd)
}
