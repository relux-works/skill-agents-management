package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/relux-works/skill-agents-management/pkg/localruntime"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
	"github.com/spf13/cobra"
)

// localRuntimeStatusReader is where this command reads live status from. It
// is a var, not a call inline in RunE, only so a test can substitute a fake
// — production wiring is the real CLI-subprocess reader, constructed once.
var localRuntimeStatusReader localruntime.StatusReader = localruntime.NewCLIStatusReader()

var localRuntimeJSON bool

var localRuntimeCmd = &cobra.Command{
	Use:   "local-runtime",
	Short: "Inspect the local-models resource plane",
}

var localRuntimeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report local-models.toml's registration state and, when valid, each declared pair's live status",
	Long: "Report whether local-models.toml was found and parsed, distinctly for\n" +
		"absence versus a malformed file — {\"registered\": false, \"reason\": \"absent\"}\n" +
		"or {\"registered\": false, \"reason\": \"malformed\", \"error\": \"<msg>\"}. When the\n" +
		"file is valid, report each declared (runtime, model) pair's live status,\n" +
		"read the same way task-board's own Preflight check would.\n\n" +
		"This command reads localmodels.Peek()'s memoized-forever result directly —\n" +
		"the SAME loader a launch's conditional registration decision reads — and\n" +
		"never registers, declares or launches anything itself.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		report := buildLocalRuntimeReport(cmd.Context(), localmodels.Peek(), localRuntimeStatusReader)
		out := cmd.OutOrStdout()
		if localRuntimeJSON {
			encoded, err := json.Marshal(report)
			if err != nil {
				return fmt.Errorf("encoding local-runtime status: %w", err)
			}
			fmt.Fprintf(out, "%s\n", encoded)
			return nil
		}
		printLocalRuntimeReport(out, report)
		return nil
	},
}

// localRuntimeReport is this command's whole answer, in the shape both the
// JSON and the human-readable rendering share.
type localRuntimeReport struct {
	Registered bool                    `json:"registered"`
	Reason     string                  `json:"reason,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Runtimes   []localRuntimeStatusRow `json:"runtimes,omitempty"`
}

// localRuntimeStatusRow is one declared (runtime, model) pair's live status,
// or the read error that stood in for one.
type localRuntimeStatusRow struct {
	Runtime      string `json:"runtime"`
	Model        string `json:"model"`
	BrokerState  string `json:"broker_state,omitempty"`
	BrokerSource string `json:"broker_source,omitempty"`
	PID          int    `json:"pid,omitempty"`
	ActiveLeases int    `json:"active_leases"`
	MaxLeases    int    `json:"max_leases"`
	Error        string `json:"error,omitempty"`
}

// buildLocalRuntimeReport is the whole of this command's logic, kept a pure
// function of its inputs so a test can drive every one of Peek's three
// shapes with a fake StatusReader, without touching the real home directory
// or the real memoized-forever singleton.
func buildLocalRuntimeReport(ctx context.Context, result localmodels.ConfigResult, reader localruntime.StatusReader) localRuntimeReport {
	switch {
	case result.Absent:
		return localRuntimeReport{Registered: false, Reason: "absent"}
	case result.Err != nil:
		return localRuntimeReport{Registered: false, Reason: "malformed", Error: result.Err.Error()}
	default:
		rows := make([]localRuntimeStatusRow, 0)
		for _, runtime := range result.Config.Runtimes {
			for modelID, model := range runtime.Models {
				row := localRuntimeStatusRow{Runtime: string(runtime.ID), Model: string(modelID)}
				status, err := reader.Status(ctx, localruntime.StatusQuery{
					Runtime:            localruntime.RuntimeID(runtime.ID),
					Model:              localruntime.ModelID(modelID),
					AgentsInfraProject: model.Pointer.AgentsInfraProject,
					AgentsInfraProfile: model.Pointer.AgentsInfraProfile,
				})
				if err != nil {
					row.Error = err.Error()
				} else {
					row.BrokerState = status.BrokerState
					row.BrokerSource = string(status.BrokerSource)
					row.PID = status.PID
					row.ActiveLeases = status.ActiveLeases
					row.MaxLeases = status.MaxLeases
				}
				rows = append(rows, row)
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Runtime != rows[j].Runtime {
				return rows[i].Runtime < rows[j].Runtime
			}
			return rows[i].Model < rows[j].Model
		})
		return localRuntimeReport{Registered: true, Runtimes: rows}
	}
}

func printLocalRuntimeReport(out io.Writer, report localRuntimeReport) {
	if !report.Registered {
		if report.Error != "" {
			fmt.Fprintf(out, "registered: false (%s: %s)\n", report.Reason, report.Error)
			return
		}
		fmt.Fprintf(out, "registered: false (%s)\n", report.Reason)
		return
	}
	if len(report.Runtimes) == 0 {
		fmt.Fprintln(out, "registered: true (no runtimes declared)")
		return
	}
	for _, row := range report.Runtimes {
		if row.Error != "" {
			fmt.Fprintf(out, "%s\t%s\terror: %s\n", row.Runtime, row.Model, row.Error)
			continue
		}
		fmt.Fprintf(out, "%s\t%s\tstate=%s source=%s pid=%d leases=%d/%d\n",
			row.Runtime, row.Model, row.BrokerState, row.BrokerSource, row.PID, row.ActiveLeases, row.MaxLeases)
	}
}

func init() {
	localRuntimeStatusCmd.Flags().BoolVar(&localRuntimeJSON, "json", false, "Emit the report as JSON")
	localRuntimeCmd.AddCommand(localRuntimeStatusCmd)
	rootCmd.AddCommand(localRuntimeCmd)
}
