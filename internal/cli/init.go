package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/adapter"
	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func newInitCmd() *cobra.Command {
	var projectRoot string
	var skipPrompt bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Memvra in the current project",
		Long: `Scan the project directory, detect the tech stack, chunk source files,
and set up the .memvra/ directory with a SQLite database and config.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine project root.
			root := projectRoot
			if root == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
				root, err = scanner.FindProjectRoot(cwd)
				if err != nil {
					return err
				}
			}
			root, _ = filepath.Abs(root)

			fmt.Println("Scanning project...")

			// Load any existing global config for scan options.
			gcfg, _ := config.LoadGlobal()

			// Run the scanner.
			scanOpts := scanner.ScanOptions{
				Root:          root,
				MaxChunkLines: gcfg.Context.ChunkMaxLines,
			}

			bar := progressbar.NewOptions(-1,
				progressbar.OptionSetDescription("  Indexing files"),
				progressbar.OptionSpinnerType(14),
				progressbar.OptionSetWriter(os.Stderr),
				progressbar.OptionClearOnFinish(),
			)

			result := scanner.Scan(scanOpts)
			_ = bar.Finish()

			fmt.Printf("Detected: %s", describeStack(result.Stack))
			if result.Stack.Database != "" {
				fmt.Printf(" + %s", result.Stack.Database)
			}
			fmt.Println()

			if len(result.Errors) > 0 {
				fmt.Fprintf(os.Stderr, "  Warning: %d file(s) could not be read\n", len(result.Errors))
			}

			// Open (or create) the database.
			dbPath := config.ProjectDBPath(root)
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer database.Close()

			store := memory.NewStore(database)

			// Persist all files and chunks.
			for _, sf := range result.Files {
				fileID, err := store.UpsertFile(sf.File)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: could not index %s: %v\n", sf.File.Path, err)
					continue
				}
				// Re-index: remove old chunks then insert new ones.
				_ = store.DeleteChunksByFileID(fileID)
				for _, chunk := range sf.Chunks {
					chunk.FileID = fileID
					if err := store.InsertChunk(chunk); err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: chunk error for %s: %v\n", sf.File.Path, err)
					}
				}
			}

			fileCount, _ := store.CountFiles()
			chunkCount, _ := store.CountChunks()

			// Persist the project profile.
			proj := memory.Project{
				Name:       result.Stack.ProjectName,
				RootPath:   root,
				TechStack:  result.Stack.ToJSON(),
				FileCount:  fileCount,
				ChunkCount: chunkCount,
			}
			if err := store.UpsertProject(proj); err != nil {
				return fmt.Errorf("save project profile: %w", err)
			}

			fmt.Printf("%d files indexed, %d chunks stored\n", fileCount, chunkCount)

			// --- Embedding phase ---
			// Build embedder from config; skip silently if unavailable or unconfigured.
			embedder := buildEmbedder(gcfg)
			vectors := memory.NewVectorStore(database)
			if embedder != nil {
				embBar := progressbar.NewOptions(-1,
					progressbar.OptionSetDescription("  Generating embeddings"),
					progressbar.OptionSpinnerType(14),
					progressbar.OptionSetWriter(os.Stderr),
					progressbar.OptionClearOnFinish(),
				)
				embeddedCount, embErr := embedAllChunks(context.Background(), store, vectors, embedder)
				_ = embBar.Finish()
				if embErr != nil {
					// Connection-refused means the embedder (e.g. Ollama) isn't running.
					fmt.Fprintf(os.Stderr, "  Embedder not available — skipping semantic indexing.\n")
					fmt.Fprintf(os.Stderr, "  To enable: start Ollama or run `memvra setup` to configure OpenAI.\n")
				} else if embeddedCount > 0 {
					fmt.Printf("%d chunks embedded for semantic search\n", embeddedCount)
				}
			} else {
				fmt.Println("Tip: run `memvra setup` to configure an embedder for semantic search.")
			}

			// Optional user notes.
			if !skipPrompt {
				fmt.Println()
				fmt.Println("Optional: Describe anything else about this project?")
				fmt.Println("  (coding conventions, constraints, team preferences — or press Enter to skip)")
				fmt.Print("> ")

				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(line)

				if line != "" {
					mt := memory.ClassifyMemoryType(line)
					m := memory.Memory{
						Content:    line,
						MemoryType: mt,
						Source:     "user",
						Importance: 0.7,
					}
					id, err := store.InsertMemory(m)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: could not save note: %v\n", err)
					} else {
						fmt.Printf("Stored as: %s (id: %s)\n", mt, id[:8])
						// Embed the memory too (best-effort).
						if embedder != nil {
							if vecs, err := embedder.Embed(context.Background(), []string{line}); err == nil && len(vecs) > 0 {
								_ = vectors.UpsertMemoryEmbedding(id, vecs[0])
							}
						}
					}
				}
			}

			// Write project config.
			pcfg := config.ProjectConfig{
				Project: config.ProjectMeta{Name: result.Stack.ProjectName},
			}
			if err := config.SaveProject(root, pcfg); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not write project config: %v\n", err)
			}

			// Ensure .memvra/ and auto-export files are in .gitignore.
			ensureGitignore(root)

			// Auto-export context files for all AI tools.
			AutoExport(root, store)

			fmt.Println()
			fmt.Println("Memvra initialized. Project context saved to .memvra/")
			fmt.Println(`Tip: Run "memvra status" to see your project profile.`)
			return nil
		},
	}

	cmd.Flags().StringVarP(&projectRoot, "root", "r", "", "Project root directory (default: auto-detect from cwd)")
	cmd.Flags().BoolVar(&skipPrompt, "no-prompt", false, "Skip the interactive notes prompt")

	return cmd
}

func describeStack(ts scanner.TechStack) string {
	if ts.Framework != "" && ts.Language != "" {
		return ts.Framework + " (" + ts.Language + ")"
	}
	if ts.Language != "" {
		return ts.Language
	}
	return ts.ProjectName
}

// buildEmbedder constructs an Embedder from the global config.
// Returns nil if no embedder is configured or available.
func buildEmbedder(gcfg config.GlobalConfig) adapter.Embedder {
	name := gcfg.DefaultEmbedder
	if name == "" {
		name = "ollama"
	}
	var apiKey string
	if name == adapter.ProviderOpenAI {
		apiKey = gcfg.Keys.OpenAI
	}
	emb, err := adapter.New(name, gcfg.Ollama.EmbedModel, apiKey, gcfg.Ollama.Host)
	if err != nil {
		return nil
	}
	return emb
}

// embedAllChunks fetches every chunk from the store and batch-embeds them,
// writing the resulting vectors into vec_chunks. Returns the number embedded.
func embedAllChunks(ctx context.Context, store *memory.Store, vectors *memory.VectorStore, embedder adapter.Embedder) (int, error) {
	chunks, err := store.ListAllChunks()
	if err != nil {
		return 0, fmt.Errorf("list chunks: %w", err)
	}
	if len(chunks) == 0 {
		return 0, nil
	}

	const batchSize = 32
	embedded := 0
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		vecs, err := embedder.Embed(ctx, texts)
		if err != nil {
			return embedded, fmt.Errorf("embed batch at offset %d: %w", i, err)
		}

		for j, vec := range vecs {
			if j >= len(batch) {
				break
			}
			if err := vectors.UpsertChunkEmbedding(batch[j].ID, vec); err != nil {
				// Non-fatal: log and continue.
				continue
			}
			embedded++
		}
	}

	return embedded, nil
}

// ensureGitignore appends .memvra/ and auto-export filenames to .gitignore
// if not already present.
func ensureGitignore(root string) {
	path := filepath.Join(root, ".gitignore")
	content, _ := os.ReadFile(path)
	existing := string(content)

	// Collect lines that need to be added.
	entries := []string{".memvra/"}

	gcfg, _ := config.Load(root)
	if gcfg.AutoExport.Enabled {
		entries = append(entries, autoExportFilenames(gcfg.AutoExport)...)
	}

	var toAdd []string
	for _, entry := range entries {
		if !strings.Contains(existing, entry) {
			toAdd = append(toAdd, entry)
		}
	}
	if len(toAdd) == 0 {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	if len(content) > 0 && !strings.HasSuffix(existing, "\n") {
		_, _ = f.WriteString("\n")
	}

	_, _ = f.WriteString("\n# Memvra auto-generated context files\n")
	for _, entry := range toAdd {
		_, _ = f.WriteString(entry + "\n")
	}
}
