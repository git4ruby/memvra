package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

func newRememberCmd() *cobra.Command {
	var memType string

	cmd := &cobra.Command{
		Use:   "remember <statement>",
		Short: "Store a decision, convention, or constraint",
		Long: `Manually save something Memvra should always remember about this project.

Examples:
  memvra remember "We switched from Devise to custom JWT auth"
  memvra remember "All background jobs must be idempotent" --type constraint
  memvra remember "TODO: Add rate limiting to document upload endpoint"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			statement := strings.Join(args, " ")

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
			defer database.Close()

			store := memory.NewStore(database)

			// Determine memory type.
			var mt memory.MemoryType
			if memType != "" {
				mt = memory.MemoryType(strings.ToLower(memType))
				if !memory.ValidMemoryType(mt) {
					return fmt.Errorf("unknown memory type %q (valid: decision, convention, constraint, note, todo)", memType)
				}
			} else {
				mt = memory.ClassifyMemoryType(statement)
			}

			m := memory.Memory{
				Content:    statement,
				MemoryType: mt,
				Source:     "user",
				Importance: 0.6,
			}

			// Decisions and constraints are slightly more important.
			if mt == memory.TypeDecision || mt == memory.TypeConstraint {
				m.Importance = 0.8
			}

			id, err := store.InsertMemory(m)
			if err != nil {
				return fmt.Errorf("store memory: %w", err)
			}

			// Embed the memory (best-effort — non-fatal on failure).
			gcfg, _ := config.LoadGlobal()
			if embedder := buildEmbedder(gcfg); embedder != nil {
				vectors := memory.NewVectorStore(database)
				if vecs, embErr := embedder.Embed(context.Background(), []string{statement}); embErr == nil && len(vecs) > 0 {
					_ = vectors.UpsertMemoryEmbedding(id, vecs[0])
				}
			}

			fmt.Printf("Stored as: %s\n", mt)
			fmt.Printf("  %q\n", statement)
			fmt.Printf("  id: %s\n", id)

			AutoExport(root, store)
			return nil
		},
	}

	cmd.Flags().StringVarP(&memType, "type", "t", "",
		"Memory type: decision, convention, constraint, note, todo (auto-detected if not set)")

	return cmd
}
