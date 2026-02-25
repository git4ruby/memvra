package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/memvra/memvra/internal/adapter"
	ctxpkg "github.com/memvra/memvra/internal/context"
	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

func newAskCmd() *cobra.Command {
	var (
		model       string
		files       []string
		noMemory    bool
		contextOnly bool
		verbose     bool
		extract     bool
		summarize   bool
		maxTokens   int
		temperature float64
	)

	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a question with full project context injected",
		Long: `Send a question to your configured LLM with Memvra's project context automatically injected.

Examples:
  memvra ask "How should I implement the document upload endpoint?"
  memvra ask "Explain the auth flow" --model openai
  memvra ask "Refactor this" --files app/controllers/documents_controller.rb
  memvra ask "Generate a migration" --context-only`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")

			root, err := findRoot()
			if err != nil {
				return err
			}

			gcfg, err := config.LoadGlobal()
			if err != nil {
				gcfg = config.DefaultGlobal()
			}
			pcfg, _ := config.LoadProject(root)

			// Prepend always_include paths from project config.
			if len(pcfg.AlwaysInclude) > 0 {
				files = append(pcfg.AlwaysInclude, files...)
			}

			// Determine effective model.
			providerName := gcfg.DefaultModel
			if pcfg.DefaultModel != "" {
				providerName = pcfg.DefaultModel
			}
			if model != "" {
				providerName = model
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

			// Build context.
			tokenizer, err := ctxpkg.NewTokenizer()
			if err != nil {
				return fmt.Errorf("init tokenizer: %w", err)
			}
			formatter := ctxpkg.NewFormatter()

			// Use a no-op embedder unless memory is requested.
			var embedder adapter.LLMAdapter
			if !noMemory {
				embedderName := gcfg.DefaultEmbedder
				embedder, _ = adapter.New(embedderName, gcfg.Ollama.EmbedModel, apiKey(gcfg, embedderName), gcfg.Ollama.Host)
			}

			vectors := memory.NewVectorStore(database)
			ranker := memory.NewRanker()
			orchestrator := memory.NewOrchestrator(store, vectors, ranker, embedder)
			builder := ctxpkg.NewBuilder(store, orchestrator, formatter, tokenizer)

			builtCtx, err := builder.Build(context.Background(), ctxpkg.BuildOptions{
				Question:            question,
				ProjectRoot:         root,
				MaxTokens:           gcfg.Context.MaxTokens,
				TopKChunks:          gcfg.Context.TopKChunks,
				TopKMemories:        gcfg.Context.TopKMemories,
				TopKSessions:        gcfg.Context.TopKSessions,
				SessionTokenBudget:  gcfg.Context.SessionTokenBudget,
				SimilarityThreshold: gcfg.Context.SimilarityThreshold,
				ExtraFiles:          files,
			})
			if err != nil {
				return fmt.Errorf("build context: %w", err)
			}

			if verbose && len(builtCtx.Sources) > 0 {
				fmt.Fprintln(os.Stderr, "=== Sources included ===")
				for _, s := range builtCtx.Sources {
					fmt.Fprintf(os.Stderr, "  • %s\n", s)
				}
				fmt.Fprintln(os.Stderr)
			}

			if contextOnly {
				fmt.Println("=== System Prompt ===")
				fmt.Println(builtCtx.SystemPrompt)
				fmt.Println("=== Context ===")
				fmt.Println(builtCtx.ContextText)
				fmt.Printf("\n--- %d tokens ---\n", builtCtx.TokensUsed)
				return nil
			}

			// Call the LLM.
			llm, err := adapter.New(providerName, gcfg.Ollama.CompletionModel, apiKey(gcfg, providerName), gcfg.Ollama.Host)
			if err != nil {
				return fmt.Errorf("init LLM adapter: %w", err)
			}

			mt := maxTokens
			if mt == 0 {
				mt = 4096
			}
			temp := temperature
			if temp == 0 {
				temp = 0.7
			}

			stream, err := llm.Complete(context.Background(), adapter.CompletionRequest{
				SystemPrompt: builtCtx.SystemPrompt,
				Context:      builtCtx.ContextText,
				UserMessage:  question,
				MaxTokens:    mt,
				Temperature:  temp,
				Stream:       gcfg.Output.Stream,
			})
			if err != nil {
				return fmt.Errorf("LLM request: %w", err)
			}

			var responseBuf strings.Builder
			for chunk := range stream {
				if chunk.Error != nil {
					return fmt.Errorf("stream error: %w", chunk.Error)
				}
				fmt.Print(chunk.Text)
				responseBuf.WriteString(chunk.Text)
			}
			fmt.Println()

			// Record the session (best-effort — non-fatal on failure).
			var sessID string
			if sourcesJSON, err := json.Marshal(builtCtx.Sources); err == nil {
				sessID, _ = store.InsertSessionReturningID(memory.Session{
					Question:        question,
					ContextUsed:     string(sourcesJSON),
					ResponseSummary: truncateLabel(responseBuf.String(), 300),
					ModelUsed:       providerName,
					TokensUsed:      builtCtx.TokensUsed,
				})
			}

			// Auto-summarize session if enabled.
			doSummarize := gcfg.Summarization.Enabled || summarize
			if doSummarize && sessID != "" {
				summary, err := memory.SummarizeSession(
					context.Background(), llm,
					question, responseBuf.String(),
					gcfg.Summarization.MaxTokens,
				)
				if err != nil {
					if verbose {
						fmt.Fprintf(os.Stderr, "  warn: session summarization failed: %v\n", err)
					}
				} else if summary != "" {
					_ = store.UpdateSessionSummary(sessID, summary)
					if verbose {
						fmt.Fprintf(os.Stderr, "  session summary stored (%d chars)\n", len(summary))
					}
				}
			}

			// Auto-extract memories from the response if enabled.
			doExtract := gcfg.Extraction.Enabled || extract
			if doExtract {
				extracted, err := memory.ExtractMemories(context.Background(), llm, responseBuf.String(), gcfg.Extraction.MaxExtracts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warn: memory extraction failed: %v\n", err)
				} else if len(extracted) == 0 {
					if verbose {
						fmt.Fprintf(os.Stderr, "  (no memories extracted from response)\n")
					}
				} else {
					for _, m := range extracted {
						saved, saveErr := orchestrator.Remember(context.Background(), m.Content, m.MemoryType, "extracted")
						if saveErr != nil {
							continue
						}
						if verbose {
							fmt.Fprintf(os.Stderr, "  extracted (%s): %s\n", saved.MemoryType, truncateLabel(saved.Content, 60))
						}
					}
					if !verbose {
						fmt.Fprintf(os.Stderr, "  %d memor%s extracted and stored.\n", len(extracted), pluralY(len(extracted)))
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "", "LLM provider override: claude, openai, gemini, ollama")
	cmd.Flags().StringArrayVarP(&files, "files", "f", nil, "files to always include in context (comma-separated paths)")
	cmd.Flags().BoolVar(&noMemory, "no-memory", false, "skip memory retrieval, use raw question only")
	cmd.Flags().BoolVar(&contextOnly, "context-only", false, "print injected context without calling LLM")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show which memories and chunks were included in context")
	cmd.Flags().BoolVarP(&extract, "extract", "e", false, "auto-extract decisions and constraints from the response")
	cmd.Flags().BoolVarP(&summarize, "summarize", "s", false, "auto-summarize this session with an LLM call")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 4096, "maximum response tokens")
	cmd.Flags().Float64Var(&temperature, "temperature", 0.7, "sampling temperature")

	return cmd
}

// truncateLabel truncates s to max runes for display purposes.
func truncateLabel(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// pluralY returns "y" for n==1, "ies" otherwise (for "memory"/"memories").
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// apiKey returns the correct API key from the global config for the given provider.
func apiKey(cfg config.GlobalConfig, provider string) string {
	switch provider {
	case adapter.ProviderClaude:
		return cfg.Keys.Anthropic
	case adapter.ProviderOpenAI:
		return cfg.Keys.OpenAI
	case adapter.ProviderGemini:
		return cfg.Keys.Gemini
	default:
		return ""
	}
}
