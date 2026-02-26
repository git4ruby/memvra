package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/memvra/memvra/internal/adapter"
	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b[^\[a-zA-Z]|\r`)

func newWrapCmd() *cobra.Command {
	var (
		model    string
		summarize bool
		extract   bool
		noInject  bool
	)

	cmd := &cobra.Command{
		Use:   "wrap <tool> [tool-args...]",
		Short: "Wrap an AI CLI tool and record the session",
		Long: `Launch an AI CLI tool (claude, gemini, etc.) as a child process,
proxy all I/O transparently, and on exit capture the conversation
as a Memvra session for future context injection.

Examples:
  memvra wrap gemini
  memvra wrap claude --model claude-3-5-sonnet
  memvra wrap aider --no-auto-commits
  memvra wrap ollama run llama3.2`,
		Args:                cobra.MinimumNArgs(1),
		DisableFlagParsing:  false,
		SilenceUsage:        true,
		TraverseChildren:    true,
		FParseErrWhitelist:  cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := args[0]
			toolArgs := args[1:]

			// 1. Load config and open DB (best-effort — wrap still works without init).
			root, rootErr := findRoot()
			var store *memory.Store
			var database *db.DB
			var gcfg config.GlobalConfig

			if rootErr == nil {
				gcfg, _ = config.LoadGlobal()
				dbPath := config.ProjectDBPath(root)
				if _, statErr := os.Stat(dbPath); statErr == nil {
					d, dbErr := db.Open(dbPath)
					if dbErr == nil {
						database = d
						defer database.Close()
						store = memory.NewStore(database)
					}
				}
			} else {
				gcfg = config.DefaultGlobal()
			}

			// 2. Build context preamble for injection into the wrapped tool.
			var contextPreamble string
			if !noInject && store != nil {
				contextPreamble = buildWrapContext(store)
				if contextPreamble != "" {
					fmt.Fprintf(os.Stderr, "[memvra] injecting project context into %s...\n", toolName)
				}
			}

			// 3. Capture buffer — filled by the TeeReader in runInPTY.
			var captureBuf bytes.Buffer

			// 4. Run the child in a PTY (or plain exec if not a terminal).
			var runErr error
			if term.IsTerminal(int(os.Stdin.Fd())) {
				runErr = runInPTY(toolName, toolArgs, &captureBuf, contextPreamble)
			} else {
				runErr = runWithoutPTY(toolName, toolArgs, &captureBuf, contextPreamble)
			}

			if runErr != nil {
				fmt.Fprintf(os.Stderr, "\n[memvra wrap] %s exited: %v\n", toolName, runErr)
			}

			// 5. Post-session processing (all best-effort).
			if store == nil || captureBuf.Len() == 0 {
				return nil
			}

			capturedClean := stripAnsi(captureBuf.String())
			if len(capturedClean) < 50 {
				return nil // too short to be meaningful
			}

			fmt.Fprintf(os.Stderr, "\n[memvra wrap] recording session...\n")

			// 6. Store session.
			sessID, _ := store.InsertSessionReturningID(memory.Session{
				Question:        "wrap: " + toolName + " session",
				ResponseSummary: truncateLabel(capturedClean, 300),
				ModelUsed:       toolName,
			})

			// 7. Determine LLM for summarization/extraction.
			providerName := gcfg.DefaultModel
			if model != "" {
				providerName = model
			}
			llm, llmErr := adapter.New(
				providerName,
				gcfg.Ollama.CompletionModel,
				apiKey(gcfg, providerName),
				gcfg.Ollama.Host,
			)

			// 8. Summarize.
			doSummarize := gcfg.Summarization.Enabled || summarize
			if doSummarize && sessID != "" && llmErr == nil {
				summary, err := memory.SummarizeSession(
					context.Background(), llm,
					"Session with "+toolName,
					capturedClean,
					gcfg.Summarization.MaxTokens,
				)
				if err == nil && summary != "" {
					_ = store.UpdateSessionSummary(sessID, summary)
					fmt.Fprintf(os.Stderr, "[memvra wrap] session summarized\n")
				}
			}

			// 9. Extract memories.
			doExtract := gcfg.Extraction.Enabled || extract
			if doExtract && llmErr == nil && database != nil {
				extracted, err := memory.ExtractMemories(
					context.Background(), llm,
					capturedClean,
					gcfg.Extraction.MaxExtracts,
				)
				if err == nil && len(extracted) > 0 {
					vectors := memory.NewVectorStore(database)
					ranker := memory.NewRanker()
					var embedder adapter.Embedder
					if emb := buildEmbedder(gcfg); emb != nil {
						embedder = emb
					}
					orchestrator := memory.NewOrchestrator(store, vectors, ranker, embedder)
					for _, m := range extracted {
						_, _ = orchestrator.Remember(context.Background(), m.Content, m.MemoryType, "extracted")
					}
					fmt.Fprintf(os.Stderr, "[memvra wrap] %d memor%s extracted\n",
						len(extracted), pluralY(len(extracted)))
				}
			}

			AutoExport(root, store)
			return nil
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "", "LLM provider for summarization (claude, openai, gemini, ollama)")
	cmd.Flags().BoolVarP(&summarize, "summarize", "s", false, "Force session summarization")
	cmd.Flags().BoolVarP(&extract, "extract", "e", false, "Force memory extraction from session")
	cmd.Flags().BoolVar(&noInject, "no-inject", false, "Skip injecting project context into the wrapped tool")

	return cmd
}

// runInPTY launches toolName in a pseudo-terminal, proxying all I/O.
// If contextPreamble is non-empty, it is written to the child's stdin
// as the first message after a brief startup delay.
// Output is tee'd into capture. Returns when the child exits.
func runInPTY(toolName string, toolArgs []string, capture *bytes.Buffer, contextPreamble string) error {
	cmd := exec.Command(toolName, toolArgs...)
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	// Forward terminal resize events to the child.
	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)
	go func() {
		for range winchCh {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	winchCh <- syscall.SIGWINCH // set initial size
	defer func() { signal.Stop(winchCh); close(winchCh) }()

	// Raw mode: every keystroke (including Ctrl+C) goes to the child.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()

	// stdin → child (with optional context injection)
	go func() {
		if contextPreamble != "" {
			// Wait for the tool to initialize before injecting context.
			time.Sleep(800 * time.Millisecond)
			_, _ = ptmx.Write([]byte(contextPreamble))
		}
		_, _ = io.Copy(ptmx, os.Stdin)
	}()

	// child → stdout + capture buffer
	_, _ = io.Copy(os.Stdout, io.TeeReader(ptmx, capture))

	return cmd.Wait()
}

// runWithoutPTY runs the tool without a PTY (for non-terminal contexts).
// If contextPreamble is non-empty, it is prepended to stdin.
func runWithoutPTY(toolName string, toolArgs []string, capture *bytes.Buffer, contextPreamble string) error {
	cmd := exec.Command(toolName, toolArgs...)
	cmd.Env = os.Environ()
	if contextPreamble != "" {
		cmd.Stdin = io.MultiReader(strings.NewReader(contextPreamble), os.Stdin)
	} else {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = io.MultiWriter(os.Stdout, capture)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildWrapContext builds a compact context summary from recent sessions and
// key memories. This is injected into the wrapped tool as its first message
// so the tool knows about previous work when the user types "continue".
func buildWrapContext(store *memory.Store) string {
	var b strings.Builder

	// Gather sessions and memories.
	sessions, _ := store.GetLastNSessions(3)
	decisions, _ := store.ListMemories(memory.TypeDecision)
	todos, _ := store.ListMemories(memory.TypeTodo)

	if len(sessions) == 0 && len(decisions) == 0 && len(todos) == 0 {
		return ""
	}

	b.WriteString("Here is project context from previous AI sessions (provided by Memvra):\n\n")

	if len(sessions) > 0 {
		b.WriteString("## Recent Sessions\n")
		for i := len(sessions) - 1; i >= 0; i-- {
			s := sessions[i]
			ts := s.CreatedAt.Format("2006-01-02 15:04")
			model := ""
			if s.ModelUsed != "" {
				model = " (" + s.ModelUsed + ")"
			}
			fmt.Fprintf(&b, "- [%s]%s %s", ts, model, s.Question)
			if s.ResponseSummary != "" {
				fmt.Fprintf(&b, ": %s", truncateLabel(s.ResponseSummary, 200))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(decisions) > 0 {
		b.WriteString("## Key Decisions\n")
		for _, d := range decisions {
			fmt.Fprintf(&b, "- %s\n", d.Content)
		}
		b.WriteString("\n")
	}

	if len(todos) > 0 {
		b.WriteString("## TODOs\n")
		for _, t := range todos {
			fmt.Fprintf(&b, "- %s\n", t.Content)
		}
		b.WriteString("\n")
	}

	b.WriteString("Please acknowledge this context and continue from where the previous session left off.\n")

	return b.String()
}

// stripAnsi removes ANSI escape codes, carriage returns, and collapses
// excessive blank lines from PTY-captured output.
func stripAnsi(s string) string {
	clean := ansiEscape.ReplaceAllString(s, "")
	clean = regexp.MustCompile(`\n{3,}`).ReplaceAllString(clean, "\n\n")
	return strings.TrimSpace(clean)
}
