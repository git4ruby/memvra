package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

// stubOrchestrator returns a fixed RetrievalResult for testing.
type stubOrchestrator struct {
	result *memory.RetrievalResult
	err    error
}

func (s *stubOrchestrator) Retrieve(_ context.Context, _ string, _ memory.RetrieveOptions) (*memory.RetrievalResult, error) {
	return s.result, s.err
}

func setupBuilderTestDB(t *testing.T, orch *stubOrchestrator) (*db.DB, *memory.Store, *Builder) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "builder_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	store := memory.NewStore(database)
	tokenizer, err := NewTokenizer()
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}
	formatter := NewFormatter()
	builder := NewBuilder(store, orch, formatter, tokenizer)
	return database, store, builder
}

func seedProject(t *testing.T, store *memory.Store) {
	t.Helper()
	store.UpsertProject(memory.Project{
		Name:      "testproject",
		RootPath:  "/tmp/testproject",
		TechStack: `{"language":"Go","framework":"net/http","database":"PostgreSQL"}`,
	})
}

func TestBuilder_Build_Defaults(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "how does auth work?",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// System prompt is always populated (project profile + instructions).
	if result.SystemPrompt == "" {
		t.Error("expected non-empty SystemPrompt")
	}
	// With no memories/chunks/sessions, context budget should report 0 tokens used.
	// (System prompt is separate from the context budget.)
	if result.TokensUsed < 0 {
		t.Errorf("expected non-negative TokensUsed, got %d", result.TokensUsed)
	}
}

func TestBuilder_Build_ProjectProfile(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "what is this project?",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(result.SystemPrompt, "testproject") {
		t.Error("system prompt should contain project name")
	}
	if !strings.Contains(result.SystemPrompt, "Go") {
		t.Error("system prompt should contain language")
	}
	if !strings.Contains(result.SystemPrompt, "PostgreSQL") {
		t.Error("system prompt should contain database")
	}
}

func TestBuilder_Build_ConventionsAndConstraints(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	store.InsertMemory(memory.Memory{Content: "use camelCase", MemoryType: memory.TypeConvention, Importance: 0.7})
	store.InsertMemory(memory.Memory{Content: "never expose API keys", MemoryType: memory.TypeConstraint, Importance: 0.8})

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "code review",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(result.SystemPrompt, "use camelCase") {
		t.Error("system prompt should contain convention")
	}
	if !strings.Contains(result.SystemPrompt, "never expose API keys") {
		t.Error("system prompt should contain constraint")
	}
}

func TestBuilder_Build_Decisions(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	store.InsertMemory(memory.Memory{Content: "decided to use PostgreSQL", MemoryType: memory.TypeDecision, Importance: 0.8})
	store.InsertMemory(memory.Memory{Content: "switched to React 18", MemoryType: memory.TypeDecision, Importance: 0.8})

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "what database do we use?",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(result.ContextText, "decided to use PostgreSQL") {
		t.Error("context should contain decision")
	}
	if !strings.Contains(result.ContextText, "switched to React 18") {
		t.Error("context should contain second decision")
	}
}

func TestBuilder_Build_RetrievedChunks(t *testing.T) {
	// Orchestrator returns a chunk.
	fileID := "file-123"
	orch := &stubOrchestrator{
		result: &memory.RetrievalResult{
			Chunks: []memory.Chunk{
				{ID: "chunk-1", FileID: fileID, Content: "func handler() {}", StartLine: 10, EndLine: 20, ChunkType: "code"},
			},
		},
	}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	// Insert the file so GetFileByID can resolve the path.
	store.UpsertFile(memory.File{Path: "internal/api/handler.go", Language: "go", LastModified: time.Now(), ContentHash: "h"})

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "how does the API work?",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.ChunksUsed != 1 {
		t.Errorf("expected 1 chunk used, got %d", result.ChunksUsed)
	}
	if !strings.Contains(result.ContextText, "func handler() {}") {
		t.Error("context should contain retrieved chunk content")
	}
}

func TestBuilder_Build_RetrievedMemories(t *testing.T) {
	// Orchestrator returns a note memory (not convention/constraint/decision).
	orch := &stubOrchestrator{
		result: &memory.RetrievalResult{
			Memories: []memory.Memory{
				{Content: "an important note", MemoryType: memory.TypeNote, Importance: 0.5},
				{Content: "a todo item", MemoryType: memory.TypeTodo, Importance: 0.6},
			},
		},
	}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "what should I work on?",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.MemoriesUsed != 2 {
		t.Errorf("expected 2 memories used, got %d", result.MemoriesUsed)
	}
	if !strings.Contains(result.ContextText, "an important note") {
		t.Error("context should contain note memory")
	}
	if !strings.Contains(result.ContextText, "a todo item") {
		t.Error("context should contain todo memory")
	}
}

func TestBuilder_Build_SkipsDuplicateTypes(t *testing.T) {
	// Orchestrator returns a convention — should be skipped since it's already in system prompt.
	orch := &stubOrchestrator{
		result: &memory.RetrievalResult{
			Memories: []memory.Memory{
				{Content: "use camelCase", MemoryType: memory.TypeConvention, Importance: 0.7},
				{Content: "always validate", MemoryType: memory.TypeConstraint, Importance: 0.8},
				{Content: "decided X", MemoryType: memory.TypeDecision, Importance: 0.8},
				{Content: "unique note", MemoryType: memory.TypeNote, Importance: 0.5},
			},
		},
	}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "review",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Only the note should count as a "memory used" — the others are skipped.
	if result.MemoriesUsed != 1 {
		t.Errorf("expected 1 memory used (note only), got %d", result.MemoriesUsed)
	}
}

func TestBuilder_Build_TokenBudget(t *testing.T) {
	// Return a large chunk to test budget enforcement.
	bigContent := strings.Repeat("word ", 5000)
	orch := &stubOrchestrator{
		result: &memory.RetrievalResult{
			Chunks: []memory.Chunk{
				{Content: bigContent, StartLine: 1, EndLine: 100, ChunkType: "code"},
			},
		},
	}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question:  "explain this",
		MaxTokens: 500, // Very small budget.
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// TokensUsed should not exceed MaxTokens by much.
	if result.TokensUsed > 600 {
		t.Errorf("expected tokens to stay near budget 500, got %d", result.TokensUsed)
	}
}

func TestBuilder_Build_ExtraFiles(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	// Create a temp file to include.
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "extra.go")
	os.WriteFile(filePath, []byte("package extra\n\nfunc Hello() {}"), 0o644)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question:   "explain this file",
		ExtraFiles: []string{filePath},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(result.ContextText, "package extra") {
		t.Error("context should contain explicitly requested file content")
	}
	foundExplicit := false
	for _, s := range result.Sources {
		if strings.Contains(s, "file (explicit)") {
			foundExplicit = true
		}
	}
	if !foundExplicit {
		t.Error("sources should list the explicit file")
	}
}

func TestBuilder_Build_ExtraFiles_Missing(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	// Non-existent file should be skipped gracefully.
	result, err := builder.Build(context.Background(), BuildOptions{
		Question:   "explain",
		ExtraFiles: []string{"/nonexistent/path/file.go"},
	})
	if err != nil {
		t.Fatalf("Build should not error on missing extra file: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBuilder_Build_SessionHistory(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	// Insert some sessions.
	store.InsertSession(memory.Session{
		Question:        "how do I deploy?",
		ContextUsed:     "{}",
		ResponseSummary: "Use docker compose.",
		ModelUsed:       "claude",
		TokensUsed:      100,
	})
	store.InsertSession(memory.Session{
		Question:        "what about CI?",
		ContextUsed:     "{}",
		ResponseSummary: "Use GitHub Actions.",
		ModelUsed:       "claude",
		TokensUsed:      100,
	})

	result, err := builder.Build(context.Background(), BuildOptions{
		Question:     "related question",
		TopKSessions: 5,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.SessionsUsed != 2 {
		t.Errorf("expected 2 sessions used, got %d", result.SessionsUsed)
	}
	if !strings.Contains(result.ContextText, "how do I deploy?") {
		t.Error("context should contain session question")
	}
}

func TestBuilder_Build_EmptyProject(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, _, builder := setupBuilderTestDB(t, orch)
	// No project seeded — should handle gracefully.

	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "what is this?",
	})
	if err != nil {
		t.Fatalf("Build should not error on empty project: %v", err)
	}
	if result.SystemPrompt == "" {
		t.Error("expected non-empty system prompt even without project")
	}
	// Should fall back to "unknown" project name.
	if !strings.Contains(result.SystemPrompt, "unknown") {
		t.Error("system prompt should contain fallback project name 'unknown'")
	}
}

func TestBuilder_Build_SourceTracking(t *testing.T) {
	orch := &stubOrchestrator{
		result: &memory.RetrievalResult{
			Memories: []memory.Memory{
				{Content: "a note", MemoryType: memory.TypeNote, Importance: 0.5},
			},
		},
	}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	store.InsertMemory(memory.Memory{Content: "a decision", MemoryType: memory.TypeDecision, Importance: 0.8})
	store.InsertSession(memory.Session{
		Question: "prev q", ContextUsed: "{}", ResponseSummary: "prev a", ModelUsed: "claude",
	})

	result, err := builder.Build(context.Background(), BuildOptions{
		Question:     "trace sources",
		TopKSessions: 5,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Should have sources for sessions, decisions, and retrieved memories.
	if len(result.Sources) == 0 {
		t.Error("expected non-empty sources")
	}

	hasDecision := false
	hasMemory := false
	hasSession := false
	for _, src := range result.Sources {
		if strings.Contains(src, "decision") {
			hasDecision = true
		}
		if strings.Contains(src, "memory") {
			hasMemory = true
		}
		if strings.Contains(src, "session") {
			hasSession = true
		}
	}
	if !hasDecision {
		t.Errorf("sources should track decisions, got: %v", result.Sources)
	}
	if !hasMemory {
		t.Errorf("sources should track retrieved memories, got: %v", result.Sources)
	}
	if !hasSession {
		t.Errorf("sources should track sessions, got: %v", result.Sources)
	}
}

func TestBuilder_Build_SessionHistory_ZeroSkips(t *testing.T) {
	orch := &stubOrchestrator{result: &memory.RetrievalResult{}}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	store.InsertSession(memory.Session{
		Question: "q", ContextUsed: "{}", ResponseSummary: "a", ModelUsed: "claude",
	})

	// TopKSessions=0 (default) should skip session history.
	result, err := builder.Build(context.Background(), BuildOptions{
		Question: "no sessions please",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.SessionsUsed != 0 {
		t.Errorf("expected 0 sessions used with default TopKSessions, got %d", result.SessionsUsed)
	}
}

func TestBuilder_Build_MultipleChunksUntilBudgetExhausted(t *testing.T) {
	// Create many small chunks — only some should fit.
	chunks := make([]memory.Chunk, 50)
	for i := range chunks {
		chunks[i] = memory.Chunk{
			Content:   fmt.Sprintf("chunk content number %d with some padding words to use tokens", i),
			StartLine: i * 10,
			EndLine:   (i + 1) * 10,
			ChunkType: "code",
		}
	}
	orch := &stubOrchestrator{
		result: &memory.RetrievalResult{Chunks: chunks},
	}
	_, store, builder := setupBuilderTestDB(t, orch)
	seedProject(t, store)

	result, err := builder.Build(context.Background(), BuildOptions{
		Question:  "explain everything",
		MaxTokens: 500,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Should have used some but not all 50 chunks.
	if result.ChunksUsed >= 50 {
		t.Errorf("expected budget to limit chunks, but all 50 were used")
	}
	if result.ChunksUsed == 0 {
		t.Error("expected at least some chunks to be used")
	}
}
