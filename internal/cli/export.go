package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/export"
	gitpkg "github.com/memvra/memvra/internal/git"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func newExportCmd() *cobra.Command {
	var (
		format  string
		section string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export context to CLAUDE.md, .cursorrules, or markdown",
		Long: `Render project memory in a format compatible with other AI tools.
Output is written to stdout — pipe it to a file.

Examples:
  memvra export --format claude > CLAUDE.md
  memvra export --format cursor > .cursorrules
  memvra export --format markdown > PROJECT_CONTEXT.md
  memvra export --format markdown --section decisions`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}

			dbPath := config.ProjectDBPath(root)
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				return fmt.Errorf("Memvra not initialized. Run `memvra init` first")
			}

			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = database.Close() }()

			store := memory.NewStore(database)

			proj, err := store.GetProject()
			if err != nil {
				return fmt.Errorf("get project: %w", err)
			}

			ts, _ := scanner.TechStackFromJSON(proj.TechStack)

			// Filter memories by section if requested.
			var filterType memory.MemoryType
			if section != "" {
				filterType = memory.MemoryType(strings.ToLower(section))
				if !memory.ValidMemoryType(filterType) {
					return fmt.Errorf("unknown section %q; valid: decision, convention, constraint, note, todo", section)
				}
			}

			memories, err := store.ListMemories(filterType)
			if err != nil {
				return fmt.Errorf("list memories: %w", err)
			}

			exporter, ok := export.Get(strings.ToLower(format))
			if !ok {
				return fmt.Errorf("unknown format %q; valid formats: %s",
					format, strings.Join(export.ValidFormats(), ", "))
			}

			sessions, _ := store.GetLastNSessions(5)
			gitState := gitpkg.CaptureWorkingState(root)

			output, err := exporter.Export(export.ExportData{
				Project:  proj,
				Stack:    ts,
				Memories: memories,
				Sessions: sessions,
				GitState: gitState,
			})
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}

			_, err = os.Stdout.WriteString(output)
			return err
		},
	}

	cmd.Flags().StringVar(&format, "format", "markdown",
		"output format: claude, cursor, markdown")
	cmd.Flags().StringVarP(&section, "section", "s", "",
		"export only memories of this type: decision, convention, constraint, note, todo")

	return cmd
}
