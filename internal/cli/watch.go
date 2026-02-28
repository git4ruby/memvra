package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func newWatchCmd() *cobra.Command {
	var debounceMs int

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch the project for file changes and auto-reindex",
		Long: `Start a long-running watcher that monitors the project directory for file
changes (create, modify, delete) and incrementally updates the index.

Changes are debounced so that rapid edits (e.g. saving multiple files at once)
are batched into a single re-index pass.

Press Ctrl-C to stop.`,
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
			defer func() { _ = database.Close() }()

			store := memory.NewStore(database)
			vectors := memory.NewVectorStore(database)
			gcfg, _ := config.LoadGlobal()

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return fmt.Errorf("create watcher: %w", err)
			}
			defer func() { _ = watcher.Close() }()

			ignore := scanner.NewIgnoreMatcher(root)

			// Add all non-ignored directories recursively.
			if err := addWatchDirs(watcher, root, ignore); err != nil {
				return fmt.Errorf("add watch directories: %w", err)
			}

			debounce := time.Duration(debounceMs) * time.Millisecond

			fmt.Printf("Watching %s for changes (debounce %s). Press Ctrl-C to stop.\n", root, debounce)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle Ctrl-C gracefully.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			// Collect changed relative paths, debounce, then process.
			pending := make(map[string]fsnotify.Op)
			timer := time.NewTimer(debounce)
			timer.Stop() // Don't fire immediately.

			for {
				select {
				case <-sigCh:
					fmt.Println("\nStopping watcher.")
					return nil

				case event, ok := <-watcher.Events:
					if !ok {
						return nil
					}

					rel, err := filepath.Rel(root, event.Name)
					if err != nil || rel == "." {
						continue
					}

					// Skip events inside hard-ignored or .memvra dirs.
					if shouldIgnoreEvent(rel, ignore) {
						continue
					}

					// If a new directory was created, start watching it.
					if event.Has(fsnotify.Create) {
						if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
							if !scanner.HardIgnore(filepath.Base(event.Name)) {
								_ = watcher.Add(event.Name)
							}
							continue
						}
					}

					// Only care about source files.
					if scanner.SkipFile(filepath.Base(rel)) {
						continue
					}
					if scanner.LanguageForFile(rel) == "" {
						continue
					}

					pending[rel] = event.Op
					timer.Reset(debounce)

				case err, ok := <-watcher.Errors:
					if !ok {
						return nil
					}
					fmt.Fprintf(os.Stderr, "  watch error: %v\n", err)

				case <-timer.C:
					if len(pending) == 0 {
						continue
					}
					batch := pending
					pending = make(map[string]fsnotify.Op)

					processChanges(ctx, root, batch, store, vectors, ignore, gcfg)

				case <-ctx.Done():
					return nil
				}
			}
		},
	}

	cmd.Flags().IntVar(&debounceMs, "debounce", 500, "debounce interval in milliseconds")

	return cmd
}

// addWatchDirs recursively adds directories to the watcher, skipping ignored ones.
func addWatchDirs(watcher *fsnotify.Watcher, root string, ignore *scanner.IgnoreMatcher) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if scanner.HardIgnore(name) {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, path)
		if rel != "." && ignore.Match(rel) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

// shouldIgnoreEvent checks whether a relative path should be ignored by the watcher.
func shouldIgnoreEvent(rel string, ignore *scanner.IgnoreMatcher) bool {
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		if scanner.HardIgnore(p) {
			return true
		}
	}
	return ignore.Match(rel)
}

// processChanges handles a batch of file change events.
func processChanges(
	ctx context.Context,
	root string,
	batch map[string]fsnotify.Op,
	store *memory.Store,
	vectors *memory.VectorStore,
	ignore *scanner.IgnoreMatcher,
	gcfg config.GlobalConfig,
) {
	var added, modified, deleted int
	changedFileIDs := make([]string, 0, len(batch))

	for rel, op := range batch {
		absPath := filepath.Join(root, rel)

		// If the file was removed (or renamed away), prune it.
		if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				existing, lookupErr := store.GetFileByPath(rel)
				if lookupErr == nil {
					pruneDeletedFile(store, vectors, existing.ID)
					deleted++
				}
				continue
			}
		}

		// File was created or modified — scan and upsert.
		sf, err := scanner.ScanFile(root, rel, gcfg.Context.ChunkMaxLines, ignore)
		if err != nil || sf == nil {
			continue
		}

		fileID, status, err := upsertScannedFile(store, *sf, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: %v\n", err)
			continue
		}
		switch status {
		case fileAdded:
			added++
			changedFileIDs = append(changedFileIDs, fileID)
		case fileModified:
			modified++
			changedFileIDs = append(changedFileIDs, fileID)
		}
	}

	if added+modified+deleted == 0 {
		return
	}

	refreshProjectCounts(store)

	ts := time.Now().Format("15:04:05")
	fmt.Printf("[%s] +%d ~%d -%d", ts, added, modified, deleted)

	// Re-embed if we have an embedder.
	if len(changedFileIDs) > 0 {
		if embedder := buildEmbedder(gcfg); embedder != nil {
			n := embedFileChunks(ctx, store, vectors, embedder, changedFileIDs)
			if n > 0 {
				fmt.Printf(" (%d chunks embedded)", n)
			}
		}
	}

	fmt.Println()
}
