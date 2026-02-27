package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

func newForgetCmd() *cobra.Command {
	var memID string
	var memType string
	var all bool

	cmd := &cobra.Command{
		Use:   "forget",
		Short: "Remove specific memories or reset all",
		Long: `Remove stored memories from the project.

Examples:
  memvra forget --id mem_abc123
  memvra forget --type todo
  memvra forget --all`,
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

			switch {
			case all:
				if !confirmPrompt("This will delete ALL memories. Continue?") {
					fmt.Println("Aborted.")
					return nil
				}
				n, err := store.DeleteAllMemories()
				if err != nil {
					return fmt.Errorf("delete memories: %w", err)
				}
				fmt.Printf("Deleted %d memories.\n", n)

			case memType != "":
				mt := memory.MemoryType(strings.ToLower(memType))
				if !memory.ValidMemoryType(mt) {
					return fmt.Errorf("unknown memory type %q", memType)
				}
				n, err := store.DeleteMemoriesByType(mt)
				if err != nil {
					return fmt.Errorf("delete memories: %w", err)
				}
				fmt.Printf("Deleted %d %s memories.\n", n, mt)

			case memID != "":
				if err := store.DeleteMemory(memID); err != nil {
					return err
				}
				fmt.Printf("Deleted memory %s.\n", memID)

			default:
				// Interactive mode: list memories and let user choose.
				return forgetInteractive(store)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&memID, "id", "", "Delete a specific memory by ID")
	cmd.Flags().StringVarP(&memType, "type", "t", "", "Delete all memories of this type")
	cmd.Flags().BoolVar(&all, "all", false, "Delete all memories (requires confirmation)")

	return cmd
}

func forgetInteractive(store *memory.Store) error {
	memories, err := store.ListMemories("")
	if err != nil {
		return err
	}
	if len(memories) == 0 {
		fmt.Println("No memories stored.")
		return nil
	}

	fmt.Printf("Stored memories (%d):\n\n", len(memories))
	for i, m := range memories {
		preview := m.Content
		if len(preview) > 80 {
			preview = preview[:77] + "..."
		}
		fmt.Printf("  [%2d] %-12s %s\n", i+1, "["+string(m.MemoryType)+"]", preview)
		fmt.Printf("       id: %s\n", m.ID)
	}

	fmt.Print("\nEnter memory number to delete (or 'q' to quit): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)

	if line == "q" || line == "" {
		fmt.Println("Aborted.")
		return nil
	}

	var idx int
	if _, err := fmt.Sscan(line, &idx); err != nil || idx < 1 || idx > len(memories) {
		return fmt.Errorf("invalid selection: %s", line)
	}

	m := memories[idx-1]
	if err := store.DeleteMemory(m.ID); err != nil {
		return err
	}
	fmt.Printf("Deleted: %q\n", m.Content)
	return nil
}

func confirmPrompt(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
