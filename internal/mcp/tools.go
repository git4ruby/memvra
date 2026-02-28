package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/memvra/memvra/internal/adapter"
	"github.com/memvra/memvra/internal/config"
	ctxpkg "github.com/memvra/memvra/internal/context"
	"github.com/memvra/memvra/internal/export"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func (s *Server) handleSaveProgress(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	task, err := req.RequireString("task")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: task"), nil
	}
	summary, err := req.RequireString("summary")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: summary"), nil
	}
	model, err := req.RequireString("model")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: model"), nil
	}

	// Include touched files in summary if provided.
	filesTouched := req.GetStringSlice("files_touched", nil)
	if len(filesTouched) > 0 {
		summary += "\n\nFiles touched: " + strings.Join(filesTouched, ", ")
	}

	sess := memory.Session{
		Question:        task,
		ResponseSummary: summary,
		ModelUsed:       model,
	}
	_, insertErr := s.store.InsertSessionReturningID(sess)
	if insertErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save progress: %v", insertErr)), nil
	}

	export.AutoExport(s.root, s.store)
	return mcp.NewToolResultText("Progress saved. Other AI tools will see this context in CLAUDE.md, .cursorrules, and PROJECT_CONTEXT.md."), nil
}

func (s *Server) handleRemember(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: content"), nil
	}

	typeStr := req.GetString("type", "")

	var mt memory.MemoryType
	if typeStr != "" {
		mt = memory.MemoryType(typeStr)
		if !memory.ValidMemoryType(mt) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid type %q (valid: decision, convention, constraint, note, todo)", typeStr)), nil
		}
	} else {
		mt = memory.ClassifyMemoryType(content)
	}

	m := memory.Memory{
		Content:    content,
		MemoryType: mt,
		Source:     "user",
		Importance: 0.6,
	}
	if mt == memory.TypeDecision || mt == memory.TypeConstraint {
		m.Importance = 0.8
	}

	id, insertErr := s.store.InsertMemory(m)
	if insertErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to store memory: %v", insertErr)), nil
	}

	// Best-effort embed.
	s.embedMemory(id, content)

	export.AutoExport(s.root, s.store)
	return mcp.NewToolResultText(fmt.Sprintf("Remembered as %s (id: %s)", mt, id)), nil
}

func (s *Server) handleGetContext(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	question := req.GetString("question", "")

	gcfg, _ := config.Load(s.root)

	// Build embedder for semantic search (best-effort).
	var embedder adapter.Embedder
	if emb := buildEmbedder(gcfg); emb != nil {
		embedder = emb
	}

	ranker := memory.NewRanker()
	orchestrator := memory.NewOrchestrator(s.store, s.vectors, ranker, embedder)
	formatter := ctxpkg.NewFormatter()
	tokenizer, _ := ctxpkg.NewTokenizer()
	builder := ctxpkg.NewBuilder(s.store, orchestrator, formatter, tokenizer)

	opts := ctxpkg.BuildOptions{
		Question:            question,
		ProjectRoot:         s.root,
		MaxTokens:           gcfg.Context.MaxTokens,
		TopKChunks:          gcfg.Context.TopKChunks,
		TopKMemories:        gcfg.Context.TopKMemories,
		TopKSessions:        gcfg.Context.TopKSessions,
		SessionTokenBudget:  gcfg.Context.SessionTokenBudget,
		SimilarityThreshold: gcfg.Context.SimilarityThreshold,
	}

	built, err := builder.Build(ctx, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build context: %v", err)), nil
	}

	var result strings.Builder
	if built.SystemPrompt != "" {
		result.WriteString(built.SystemPrompt)
		result.WriteString("\n\n")
	}
	result.WriteString(built.ContextText)

	return mcp.NewToolResultText(result.String()), nil
}

func (s *Server) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: query"), nil
	}
	topK := req.GetInt("top_k", 10)

	gcfg, _ := config.Load(s.root)

	var embedder adapter.Embedder
	if emb := buildEmbedder(gcfg); emb != nil {
		embedder = emb
	}

	ranker := memory.NewRanker()
	orchestrator := memory.NewOrchestrator(s.store, s.vectors, ranker, embedder)

	result, err := orchestrator.Retrieve(ctx, query, memory.RetrieveOptions{
		TopKChunks:          topK,
		TopKMemories:        topK,
		SimilarityThreshold: gcfg.Context.SimilarityThreshold,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	var sb strings.Builder
	if len(result.Memories) > 0 {
		sb.WriteString("## Matching Memories\n\n")
		for _, m := range result.Memories {
			fmt.Fprintf(&sb, "- [%s] %s (id: %s)\n", m.MemoryType, m.Content, m.ID)
		}
		sb.WriteString("\n")
	}
	if len(result.Chunks) > 0 {
		sb.WriteString("## Matching Code\n\n")
		for _, c := range result.Chunks {
			file, _ := s.store.GetFileByID(c.FileID)
			label := file.Path
			if label == "" {
				label = c.FileID
			}
			fmt.Fprintf(&sb, "### %s (lines %d-%d)\n```\n%s\n```\n\n", label, c.StartLine, c.EndLine, c.Content)
		}
	}

	if sb.Len() == 0 {
		return mcp.NewToolResultText("No results found."), nil
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) handleForget(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: id"), nil
	}

	if delErr := s.store.DeleteMemory(id); delErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete memory: %v", delErr)), nil
	}

	// Also remove vector embedding (best-effort).
	_ = s.vectors.DeleteMemoryEmbedding(id)

	export.AutoExport(s.root, s.store)
	return mcp.NewToolResultText(fmt.Sprintf("Memory %s deleted.", id)), nil
}

func (s *Server) handleProjectStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	proj, err := s.store.GetProject()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("no project found: %v", err)), nil
	}

	ts, _ := scanner.TechStackFromJSON(proj.TechStack)
	memCounts, _ := s.store.CountMemoriesByType()
	sessionCount, _ := s.store.CountSessions()

	var sb strings.Builder
	fmt.Fprintf(&sb, "Project: %s\n", proj.Name)

	stack := ts.Language
	if ts.Framework != "" {
		stack += " / " + ts.Framework
	}
	if ts.Database != "" {
		stack += " + " + ts.Database
	}
	fmt.Fprintf(&sb, "Stack:   %s\n", stack)
	fmt.Fprintf(&sb, "Files:   %d indexed, %d chunks\n", proj.FileCount, proj.ChunkCount)

	totalMem := 0
	for _, n := range memCounts {
		totalMem += n
	}
	fmt.Fprintf(&sb, "Memories: %d total", totalMem)
	if totalMem > 0 {
		parts := []string{}
		for _, t := range []memory.MemoryType{memory.TypeDecision, memory.TypeConvention, memory.TypeConstraint, memory.TypeNote, memory.TypeTodo} {
			if n, ok := memCounts[t]; ok && n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, t))
			}
		}
		fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
	}
	sb.WriteString("\n")

	fmt.Fprintf(&sb, "Sessions: %d\n", sessionCount)
	fmt.Fprintf(&sb, "Updated: %s\n", proj.UpdatedAt.Format("2006-01-02 15:04"))

	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) handleListMemories(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	typeStr := req.GetString("type", "")
	memories, err := s.store.ListMemories(memory.MemoryType(typeStr))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list memories: %v", err)), nil
	}

	if len(memories) == 0 {
		return mcp.NewToolResultText("No memories stored."), nil
	}

	var sb strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&sb, "[%s] %s\n  id: %s | source: %s | created: %s\n\n",
			m.MemoryType, m.Content, m.ID, m.Source, m.CreatedAt.Format("2006-01-02 15:04"))
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) handleListSessions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := req.GetInt("limit", 10)
	sessions, err := s.store.GetLastNSessions(limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list sessions: %v", err)), nil
	}

	if len(sessions) == 0 {
		return mcp.NewToolResultText("No sessions recorded."), nil
	}

	var sb strings.Builder
	// Reverse to chronological order (newest-first from DB → oldest-first for display).
	for i := len(sessions) - 1; i >= 0; i-- {
		sess := sessions[i]
		fmt.Fprintf(&sb, "[%s] (%s) %s\n",
			sess.CreatedAt.Format("2006-01-02 15:04"), sess.ModelUsed, sess.Question)
		if sess.ResponseSummary != "" {
			fmt.Fprintf(&sb, "  → %s\n", sess.ResponseSummary)
		}
		sb.WriteString("\n")
	}
	return mcp.NewToolResultText(sb.String()), nil
}

// embedMemory generates and stores a vector embedding for a memory (best-effort).
func (s *Server) embedMemory(id, content string) {
	gcfg, _ := config.LoadGlobal()
	embedder := buildEmbedder(gcfg)
	if embedder == nil {
		return
	}
	vecs, err := embedder.Embed(context.Background(), []string{content})
	if err != nil || len(vecs) == 0 {
		return
	}
	_ = s.vectors.UpsertMemoryEmbedding(id, vecs[0])
}

// buildEmbedder creates an embedder from config (returns nil on failure).
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
