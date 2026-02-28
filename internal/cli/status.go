package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current Memvra state for the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}

			dbPath := config.ProjectDBPath(root)
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				return fmt.Errorf("memvra not initialized in this project. Run `memvra init` first")
			}

			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = database.Close() }()

			store := memory.NewStore(database)

			proj, err := store.GetProject()
			if err != nil {
				return err
			}

			ts, _ := scanner.TechStackFromJSON(proj.TechStack)

			memCounts, err := store.CountMemoriesByType()
			if err != nil {
				return err
			}

			sessions, _ := store.CountSessions()

			gcfg, _ := config.LoadGlobal()
			pcfg, _ := config.LoadProject(root)

			modelName := gcfg.DefaultModel
			if pcfg.DefaultModel != "" {
				modelName = pcfg.DefaultModel
			}

			// DB file size.
			var dbSize int64
			if fi, err := os.Stat(dbPath); err == nil {
				dbSize = fi.Size()
			}

			// Last updated.
			lastUpdated := proj.UpdatedAt.Format("2006-01-02 15:04")

			fmt.Printf("\nProject:  %s\n", proj.Name)
			fmt.Printf("Stack:    %s\n", describeStackFull(ts))
			fmt.Printf("Indexed:  %d files, %d chunks\n", proj.FileCount, proj.ChunkCount)

			totalMem := 0
			for _, n := range memCounts {
				totalMem += n
			}
			fmt.Printf("Memories: %d total", totalMem)
			if totalMem > 0 {
				fmt.Printf(" (")
				first := true
				types := []memory.MemoryType{
					memory.TypeDecision, memory.TypeConvention,
					memory.TypeConstraint, memory.TypeNote, memory.TypeTodo,
				}
				for _, t := range types {
					if n, ok := memCounts[t]; ok && n > 0 {
						if !first {
							fmt.Printf(", ")
						}
						fmt.Printf("%d %s", n, t)
						first = false
					}
				}
				fmt.Printf(")")
			}
			fmt.Println()

			fmt.Printf("Sessions: %d\n", sessions)
			fmt.Printf("Updated:  %s\n", lastUpdated)
			fmt.Printf("Model:    %s (default)\n", modelName)
			fmt.Printf("DB size:  %s\n", formatBytes(dbSize))
			fmt.Println()

			return nil
		},
	}
}

func describeStackFull(ts scanner.TechStack) string {
	s := ts.Language
	if ts.Framework != "" {
		s += " / " + ts.Framework
	}
	if ts.Database != "" {
		s += " + " + ts.Database
	}
	return s
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func findRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	// First check if .memvra/ already exists in cwd or any parent.
	dir, _ := filepath.Abs(cwd)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".memvra")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to project root detection.
	return scanner.FindProjectRoot(cwd)
}
