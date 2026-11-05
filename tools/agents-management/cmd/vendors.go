package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	"github.com/spf13/cobra"
)

// vendorRegistry is where this binary reads its Layer-2 plugins and runtime
// declarations from. As with systemRegistry, it points at the package-level
// default the plugin packages register into, so these commands hold no list of
// their own — a private one would be the shadow table the single-source guard
// fails the build over. The variable exists only so a test can swap in an
// isolated registry.
var vendorRegistry = vendorplugin.Default

var (
	vendorsJSON  bool
	runtimesJSON bool
)

var vendorsCmd = &cobra.Command{
	Use:   "vendors",
	Short: "List the vendor plugins registered in this binary",
	Long: "List the vendor plugins registered in this binary, one id per line.\n\n" +
		"An empty list is a valid answer and exits 0: it means no vendor plugin\n" +
		"has been compiled in yet, which is different from a failure to look.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		names := vendorNames()
		out := cmd.OutOrStdout()
		if vendorsJSON {
			encoded, err := json.Marshal(names)
			if err != nil {
				return fmt.Errorf("encoding vendor list: %w", err)
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

var runtimesCmd = &cobra.Command{
	Use:   "runtimes",
	Short: "List the declared (agentic system x vendor) runtimes",
	Long: "List the runtimes declared in this binary as `id\tsystem\tvendor`.\n\n" +
		"A runtime is a DECLARATION, so this lists what is declared rather than\n" +
		"what can currently launch: a runtime whose plugins are not compiled in\n" +
		"still appears, and a runtime whose vendor was never established reads\n" +
		"as \"vendor unresolved\" rather than as a blank column.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		rows := runtimeRows()
		out := cmd.OutOrStdout()
		if runtimesJSON {
			encoded, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("encoding runtime list: %w", err)
			}
			fmt.Fprintf(out, "%s\n", encoded)
			return nil
		}
		for _, row := range rows {
			fmt.Fprintf(out, "%s\t%s\t%s\n", row.ID, row.System, row.VendorLabel)
		}
		return nil
	},
}

// vendorNames returns the registered vendor ids in a stable order. The result
// is always non-nil so the JSON encoding of "nothing registered" is [] and
// never null.
func vendorNames() []string {
	ids := vendorRegistry.VendorIDs()
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, id.String())
	}
	return names
}

// runtimeRow is one declared pair as this command reports it.
//
// The broker fields are carried into the output rather than summarized away,
// because "unresolved" is only honest when the search behind it is visible:
// muse's row says which source was checked and that nothing established a
// vendor, which is a different statement from a blank field.
type runtimeRow struct {
	ID             string   `json:"id"`
	System         string   `json:"system"`
	Vendor         string   `json:"vendor,omitempty"`
	VendorResolved bool     `json:"vendor_resolved"`
	VendorLabel    string   `json:"vendor_label"`
	BrokerChecked  []string `json:"broker_checked"`
	BrokerFound    string   `json:"broker_found,omitempty"`
}

func runtimeRows() []runtimeRow {
	declarations := vendorRegistry.RuntimeDeclarations()
	rows := make([]runtimeRow, 0, len(declarations))
	for _, declaration := range declarations {
		checked := declaration.Broker.Checked
		if checked == nil {
			checked = []string{}
		}
		rows = append(rows, runtimeRow{
			ID:             declaration.ID.String(),
			System:         declaration.System.String(),
			Vendor:         declaration.Vendor.String(),
			VendorResolved: declaration.VendorResolved(),
			VendorLabel:    declaration.VendorLabel(),
			BrokerChecked:  checked,
			BrokerFound:    declaration.Broker.Found,
		})
	}
	return rows
}

func init() {
	vendorsCmd.Flags().BoolVar(&vendorsJSON, "json", false, "Emit the list as a JSON array")
	runtimesCmd.Flags().BoolVar(&runtimesJSON, "json", false, "Emit the list as a JSON array")
	rootCmd.AddCommand(vendorsCmd)
	rootCmd.AddCommand(runtimesCmd)
}
