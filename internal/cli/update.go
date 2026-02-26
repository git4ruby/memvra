package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func newUpdateCmd() *cobra.Command {
	var force bool
	var quiet bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Re-scan the project and update the index incrementally",
		Long: `Detect changed files since the last scan and re-index only those files.
Re-generates embeddings for modified/added files and prunes deleted files.
Use --force to re-index everything regardless of content hash.
Use --quiet to suppress output (useful for git hooks).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRoot()
			if err != nil {
				return err
			}

			dbPath, err := ensureInitialized(root)
			if err != nil {
				return err
			}

			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer database.Close()

			store := memory.NewStore(database)
			vectors := memory.NewVectorStore(database)
			gcfg, _ := config.LoadGlobal()

			if !quiet {
				bar := progressbar.NewOptions(-1,
					progressbar.OptionSetDescription("  Scanning"),
					progressbar.OptionSpinnerType(14),
					progressbar.OptionSetWriter(os.Stderr),
					progressbar.OptionClearOnFinish(),
				)
				defer func() { _ = bar.Finish() }()
			}

			result := scanner.Scan(scanner.ScanOptions{
				Root:          root,
				MaxChunkLines: gcfg.Context.ChunkMaxLines,
			})

			var modified, added, skipped int
			changedFileIDs := make([]string, 0)

			for _, sf := range result.Files {
				fileID, status, err := upsertScannedFile(store, sf, force)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
					continue
				}
				switch status {
				case fileAdded:
					added++
					changedFileIDs = append(changedFileIDs, fileID)
				case fileModified:
					modified++
					changedFileIDs = append(changedFileIDs, fileID)
				default:
					skipped++
				}
			}

			// Prune files that are no longer on disk.
			var deleted int
			allDBFiles, err := store.ListFiles()
			if err == nil {
				scannedPaths := make(map[string]struct{}, len(result.Files))
				for _, sf := range result.Files {
					scannedPaths[sf.File.Path] = struct{}{}
				}
				for _, dbFile := range allDBFiles {
					if _, found := scannedPaths[dbFile.Path]; !found {
						pruneDeletedFile(store, vectors, dbFile.ID)
						deleted++
					}
				}
			}

			refreshProjectCounts(store)

			if !quiet {
				fileCount, _ := store.CountFiles()
				chunkCount, _ := store.CountChunks()
				fmt.Printf("Modified: %d files\n", modified)
				fmt.Printf("Added:    %d files\n", added)
				fmt.Printf("Deleted:  %d files\n", deleted)
				fmt.Printf("Skipped:  %d files (unchanged)\n", skipped)
				fmt.Printf("Total:    %d files, %d chunks\n", fileCount, chunkCount)
			}

			// Re-embed changed/added chunks.
			if len(changedFileIDs) == 0 {
				AutoExport(root, store)
				return nil
			}
			embedder := buildEmbedder(gcfg)
			if embedder == nil {
				return nil
			}

			if !quiet {
				embBar := progressbar.NewOptions(-1,
					progressbar.OptionSetDescription("  Generating embeddings"),
					progressbar.OptionSpinnerType(14),
					progressbar.OptionSetWriter(os.Stderr),
					progressbar.OptionClearOnFinish(),
				)
				defer func() { _ = embBar.Finish() }()
			}

			embeddedCount := embedFileChunks(context.Background(), store, vectors, embedder, changedFileIDs)

			if !quiet && embeddedCount > 0 {
				fmt.Printf("%d chunks re-embedded\n", embeddedCount)
			}

			AutoExport(root, store)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "re-index all files, ignoring content hashes")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress output (used by git hooks)")

	return cmd
}
