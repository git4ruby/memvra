package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	mcppkg "github.com/memvra/memvra/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP (Model Context Protocol) server",
		Long: `Start Memvra as an MCP server over stdio.

AI coding assistants like Claude Code, Cursor, and Windsurf can connect
to this server to automatically save progress, store decisions, and
retrieve project context — without you running any commands.

To register Memvra with your AI tools, run:
  memvra mcp install`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return fmt.Errorf("no Memvra project found: %w", err)
			}

			srv, err := mcppkg.NewServer(root)
			if err != nil {
				return fmt.Errorf("start MCP server: %w", err)
			}
			defer srv.Close()

			return srv.Run()
		},
	}

	cmd.AddCommand(newMCPInstallCmd())
	return cmd
}

func newMCPInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Register Memvra MCP server with Claude Code and Cursor",
		Long: `Registers Memvra as an MCP server so AI tools can discover and use it.

This writes configuration to:
  Claude Code: ~/.claude/mcp.json
  Cursor:      .cursor/mcp.json (project-level)

After installing, restart your AI tool to pick up the new server.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Install for Claude Code (global).
			if err := mcppkg.InstallClaudeCode(); err != nil {
				fmt.Printf("  Claude Code: failed (%v)\n", err)
			} else {
				fmt.Println("  Claude Code: installed (~/.claude/mcp.json)")
			}

			// Install for Cursor (project-level).
			root, err := findRoot()
			if err == nil {
				if err := mcppkg.InstallCursor(root); err != nil {
					fmt.Printf("  Cursor: failed (%v)\n", err)
				} else {
					fmt.Println("  Cursor: installed (.cursor/mcp.json)")
				}
			}

			fmt.Println("\nRestart your AI tool to activate Memvra MCP tools.")
			return nil
		},
	}
}
