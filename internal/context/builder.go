package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

// BuildOptions controls how context is assembled.
type BuildOptions struct {
	Question            string
	ProjectRoot         string   // used to resolve ExtraFiles relative paths
	MaxTokens           int
	TopKChunks          int
	TopKMemories        int
	TopKSessions        int      // how many recent session summaries to inject (0 = skip)
	SessionTokenBudget  int      // max tokens for session history block
	SimilarityThreshold float64
	ExtraFiles          []string // paths to always include
}

// BuiltContext is the result of a context build operation.
type BuiltContext struct {
	SystemPrompt string
	ContextText  string
	TokensUsed   int
	ChunksUsed   int
	MemoriesUsed int
	SessionsUsed int
	// Sources lists what was included, for --verbose output.
	// Each entry is a short human-readable label.
	Sources []string
}

// Builder assembles token-budget-aware prompts from project memory.
type Builder struct {
	store        *memory.Store
	orchestrator interface {
		Retrieve(ctx context.Context, query string, opts memory.RetrieveOptions) (*memory.RetrievalResult, error)
	}
	formatter *Formatter
	tokenizer *Tokenizer
}

// NewBuilder creates a Builder.
func NewBuilder(
	store *memory.Store,
	orchestrator interface {
		Retrieve(ctx context.Context, query string, opts memory.RetrieveOptions) (*memory.RetrievalResult, error)
	},
	formatter *Formatter,
	tokenizer *Tokenizer,
) *Builder {
	return &Builder{
		store:        store,
		orchestrator: orchestrator,
		formatter:    formatter,
		tokenizer:    tokenizer,
	}
}

// Build constructs the context for the given question within the token budget.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*BuiltContext, error) {
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 8000
	}
	if opts.TopKChunks == 0 {
		opts.TopKChunks = 10
	}
	if opts.TopKMemories == 0 {
		opts.TopKMemories = 5
	}
	if opts.SimilarityThreshold == 0 {
		opts.SimilarityThreshold = 0.3
	}
	if opts.SessionTokenBudget == 0 {
		opts.SessionTokenBudget = 500
	}

	remaining := opts.MaxTokens
	var contextSections []string
	var sources []string

	// --- Step 1: Project profile (always included) ---
	proj, err := b.store.GetProject()
	if err != nil {
		proj = memory.Project{Name: "unknown"}
	}
	ts, _ := scanner.TechStackFromJSON(proj.TechStack)

	// --- Step 2: Conventions + constraints (always included) ---
	conventions, _ := b.store.ListMemories(memory.TypeConvention)
	constraints, _ := b.store.ListMemories(memory.TypeConstraint)
	decisions, _ := b.store.ListMemories(memory.TypeDecision)

	systemPrompt := b.formatter.FormatSystemPrompt(proj, ts, conventions, constraints)

	// --- Step 3: Explicitly requested files (highest priority, always included) ---
	for _, relPath := range opts.ExtraFiles {
		absPath := relPath
		if opts.ProjectRoot != "" && !filepath.IsAbs(relPath) {
			absPath = filepath.Join(opts.ProjectRoot, relPath)
		}
		content, err := os.ReadFile(absPath)
		if err != nil {
			// File not found or unreadable — skip gracefully.
			continue
		}
		c := memory.Chunk{
			Content:   string(content),
			StartLine: 1,
			EndLine:   strings.Count(string(content), "\n") + 1,
			ChunkType: "code",
		}
		block := b.formatter.FormatChunk(c, relPath)
		tokens := b.tokenizer.Count(block)
		if tokens <= remaining {
			contextSections = append(contextSections, block)
			remaining -= tokens
			sources = append(sources, fmt.Sprintf("file (explicit): %s", relPath))
		}
	}

	// --- Step 3b: Recent session summaries (budget-gated) ---
	sessionsUsed := 0
	if opts.TopKSessions > 0 && remaining > 200 {
		sessions, _ := b.store.GetLastNSessions(opts.TopKSessions)
		if len(sessions) > 0 {
			block := b.formatter.FormatSessionHistory(sessions)
			tokens := b.tokenizer.Count(block)
			allowed := opts.SessionTokenBudget
			if allowed > remaining {
				allowed = remaining
			}
			if tokens <= allowed {
				contextSections = append(contextSections, block)
				remaining -= tokens
				sessionsUsed = len(sessions)
				sources = append(sources, fmt.Sprintf("recent sessions: %d", len(sessions)))
			}
		}
	}

	// --- Step 4: Retrieve semantically relevant content ---
	retrieval, _ := b.orchestrator.Retrieve(ctx, opts.Question, memory.RetrieveOptions{
		TopKChunks:          opts.TopKChunks,
		TopKMemories:        opts.TopKMemories,
		SimilarityThreshold: opts.SimilarityThreshold,
	})

	// --- Step 5: Decision block ---
	if len(decisions) > 0 {
		block := b.formatter.FormatMemories(memory.TypeDecision, decisions)
		tokens := b.tokenizer.Count(block)
		if tokens <= remaining {
			contextSections = append(contextSections, block)
			remaining -= tokens
			for _, d := range decisions {
				sources = append(sources, fmt.Sprintf("decision: %s", truncateStr(d.Content, 60)))
			}
		}
	}

	// --- Step 6: Fill remaining budget with retrieved chunks and memories ---
	chunksUsed := 0
	memoriesUsed := 0

	if retrieval != nil {
		// Add relevant memories first.
		for _, m := range retrieval.Memories {
			if m.MemoryType == memory.TypeConvention || m.MemoryType == memory.TypeConstraint || m.MemoryType == memory.TypeDecision {
				continue // Already included via system prompt or decisions block.
			}
			block := "- " + m.Content + "\n"
			tokens := b.tokenizer.Count(block)
			if tokens <= remaining {
				contextSections = append(contextSections, block)
				remaining -= tokens
				memoriesUsed++
				sources = append(sources, fmt.Sprintf("memory (%s): %s", m.MemoryType, truncateStr(m.Content, 60)))
			}
		}

		// Add relevant chunks.
		for _, c := range retrieval.Chunks {
			// Resolve file path from the file record.
			filePath := ""
			if file, err := b.store.GetFileByID(c.FileID); err == nil {
				filePath = file.Path
			}
			block := b.formatter.FormatChunk(c, filePath)
			tokens := b.tokenizer.Count(block)
			if tokens <= remaining {
				contextSections = append(contextSections, block)
				remaining -= tokens
				chunksUsed++
				sources = append(sources, fmt.Sprintf("chunk: %s:%d-%d", filePath, c.StartLine, c.EndLine))
			} else if remaining > 100 {
				// Truncate the chunk to fit.
				truncated := b.tokenizer.Truncate(c.Content, remaining-50)
				c.Content = truncated
				block = b.formatter.FormatChunk(c, filePath)
				contextSections = append(contextSections, block)
				remaining = 0
				chunksUsed++
				sources = append(sources, fmt.Sprintf("chunk (truncated): %s:%d-%d", filePath, c.StartLine, c.EndLine))
				break
			} else {
				break
			}
		}
	}

	contextText := strings.Join(contextSections, "\n")
	tokensUsed := opts.MaxTokens - remaining

	return &BuiltContext{
		SystemPrompt: systemPrompt,
		ContextText:  contextText,
		TokensUsed:   tokensUsed,
		ChunksUsed:   chunksUsed,
		MemoriesUsed: memoriesUsed,
		SessionsUsed: sessionsUsed,
		Sources:      sources,
	}, nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
