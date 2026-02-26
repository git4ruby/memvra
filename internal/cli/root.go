// Package cli defines the Cobra command tree for the memvra CLI.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// version, commit, date are set via -ldflags at build time.
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// rootCmd is the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "memvra",
	Short: "Persistent, model-agnostic AI memory layer for software projects",
	Long: `Memvra gives AI coding assistants a persistent memory of your project.

It indexes your codebase, stores architectural decisions, and automatically
injects relevant context into any LLM call — so your AI finally remembers
your project across sessions.

Run 'memvra init' in any project directory to get started.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute(v, c, d string) {
	version, commit, date = v, c, d
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		newInitCmd(),
		newAskCmd(),
		newRememberCmd(),
		newForgetCmd(),
		newContextCmd(),
		newDiffCmd(),
		newStatusCmd(),
		newUpdateCmd(),
		newWatchCmd(),
		newExportCmd(),
		newHookCmd(),
		newSetupCmd(),
		newPruneCmd(),
		newMCPCmd(),
		newVersionCmd(),
	)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("memvra %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}
