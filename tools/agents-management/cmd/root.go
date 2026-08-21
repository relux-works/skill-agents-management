// Package cmd holds the agents-management command tree.
//
// The tool spawns and describes heterogeneous agentic systems and the model
// vendors behind them. Everything a concrete harness or vendor knows lives in
// a plugin; this package owns only the command surface.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version info — overridden via ldflags at build time by the Makefile's
// `build` target. The -X targets name this package, so moving or renaming
// these variables silently strips the version metadata unless the Makefile
// moves with them; TestMakeBuildInjectsVersionMetadata guards that binding.
var (
	Version   = "dev"
	Commit    = ""
	BuildDate = ""
)

var rootCmd = &cobra.Command{
	Use:   "agents-management",
	Short: "Spawn and manage heterogeneous agentic systems",
	Long: "Spawn and manage heterogeneous agentic systems and the model vendors\n" +
		"behind them. Runtimes are (agentic system x vendor) pairs contributed by\n" +
		"plugins; this binary owns registration, admission and observation only.",
	SilenceErrors: true,
	SilenceUsage:  true,
}

// Execute runs the root command and maps any failure onto exit status 1.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Version = formatVersion()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

// formatVersion renders the build metadata carried by the binary. Commit and
// build date are only present in a release build, so they are omitted rather
// than printed empty.
func formatVersion() string {
	v := fmt.Sprintf("agents-management version %s", Version)
	if Commit != "" {
		v += fmt.Sprintf(" (commit %s", Commit)
		if BuildDate != "" {
			v += fmt.Sprintf(", built %s", BuildDate)
		}
		v += ")"
	}
	return v
}
