package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build version, commit and build date",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// OutOrStdout, not cobra's Print helpers: those fall back to stderr.
		fmt.Fprintln(cmd.OutOrStdout(), formatVersion())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
