package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

// setupTestServer creates a Server backed by a temp SQLite DB with a seeded project.
func setupTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()

	dbDir := filepath.Join(root, ".memvra")
	os.MkdirAll(dbDir, 0o755)

	database, err := db.Open(filepath.Join(dbDir, "memvra.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	store := memory.NewStore(database)
	store.UpsertProject(memory.Project{
		Name:      "testproject",
		RootPath:  root,
		TechStack: `{"language":"Go","framework":"Gin"}`,
	})

	// Write project config so auto-export finds it.
	config.SaveProject(root, config.ProjectConfig{
		Project: config.ProjectMeta{Name: "testproject"},
	})

	return &Server{
		root:     root,
		database: database,
		store:    store,
		vectors:  memory.NewVectorStore(database),
	}
}

// callTool is a test helper that creates a CallToolRequest from a map of args.
func callTool(name string, args map[string]interface{}) mcplib.CallToolRequest {
	return mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
}

func TestSaveProgress_InsertsSession(t *testing.T) {
	srv := setupTestServer(t)

	req := callTool("memvra_save_progress", map[string]interface{}{
		"task":    "implementing auth middleware",
		"summary": "Added JWT validation in routes.go, need to add refresh token flow",
		"model":   "claude",
	})

	result, err := srv.handleSaveProgress(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	// Verify session was stored.
	sessions, _ := srv.store.GetLastNSessions(1)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Question != "implementing auth middleware" {
		t.Errorf("question: got %q", sessions[0].Question)
	}
	if sessions[0].ModelUsed != "claude" {
		t.Errorf("model: got %q", sessions[0].ModelUsed)
	}
	if !strings.Contains(sessions[0].ResponseSummary, "JWT validation") {
		t.Errorf("summary should contain work details, got %q", sessions[0].ResponseSummary)
	}
}

func TestSaveProgress_IncludesFilesTouched(t *testing.T) {
	srv := setupTestServer(t)

	req := callTool("memvra_save_progress", map[string]interface{}{
		"task":          "refactoring auth",
		"summary":       "Moved auth logic to separate package",
		"model":         "gemini",
		"files_touched": []interface{}{"internal/auth.go", "internal/routes.go"},
	})

	result, err := srv.handleSaveProgress(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	sessions, _ := srv.store.GetLastNSessions(1)
	if !strings.Contains(sessions[0].ResponseSummary, "internal/auth.go") {
		t.Errorf("summary should include files touched, got %q", sessions[0].ResponseSummary)
	}
}

func TestSaveProgress_MissingRequired(t *testing.T) {
	srv := setupTestServer(t)

	// Missing "task".
	req := callTool("memvra_save_progress", map[string]interface{}{
		"summary": "some work",
		"model":   "claude",
	})

	result, err := srv.handleSaveProgress(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for missing required param")
	}
}

func TestRemember_StoresMemory(t *testing.T) {
	srv := setupTestServer(t)

	req := callTool("memvra_remember", map[string]interface{}{
		"content": "Use PostgreSQL for JSONB support",
		"type":    "decision",
	})

	result, err := srv.handleRemember(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	memories, _ := srv.store.ListMemories(memory.TypeDecision)
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Content != "Use PostgreSQL for JSONB support" {
		t.Errorf("content: got %q", memories[0].Content)
	}
	if memories[0].MemoryType != memory.TypeDecision {
		t.Errorf("type: got %q", memories[0].MemoryType)
	}
}

func TestRemember_AutoClassifies(t *testing.T) {
	srv := setupTestServer(t)

	// "TODO" keyword should auto-classify as todo.
	req := callTool("memvra_remember", map[string]interface{}{
		"content": "TODO: add rate limiting to API",
	})

	result, err := srv.handleRemember(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	// Should be classified as todo.
	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "todo") {
		t.Errorf("expected auto-classification as todo, got: %s", text)
	}
}

func TestRemember_InvalidType(t *testing.T) {
	srv := setupTestServer(t)

	req := callTool("memvra_remember", map[string]interface{}{
		"content": "some fact",
		"type":    "invalid",
	})

	result, err := srv.handleRemember(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for invalid type")
	}
}

func TestForget_DeletesMemory(t *testing.T) {
	srv := setupTestServer(t)

	// Insert a memory first.
	id, _ := srv.store.InsertMemory(memory.Memory{
		Content:    "temporary fact",
		MemoryType: memory.TypeNote,
		Importance: 0.5,
	})

	req := callTool("memvra_forget", map[string]interface{}{
		"id": id,
	})

	result, err := srv.handleForget(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	// Memory should be gone.
	_, getErr := srv.store.GetMemoryByID(id)
	if getErr == nil {
		t.Error("expected memory to be deleted")
	}
}

func TestProjectStatus_ReturnsStats(t *testing.T) {
	srv := setupTestServer(t)

	// Add some data.
	srv.store.InsertMemory(memory.Memory{Content: "use JWT", MemoryType: memory.TypeDecision, Importance: 0.8})
	srv.store.InsertSession(memory.Session{Question: "how to auth?", ModelUsed: "claude"})

	result, err := srv.handleProjectStatus(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "testproject") {
		t.Error("should contain project name")
	}
	if !strings.Contains(text, "Go") {
		t.Error("should contain language")
	}
	if !strings.Contains(text, "1 decision") {
		t.Error("should contain memory count")
	}
}

func TestListMemories_All(t *testing.T) {
	srv := setupTestServer(t)

	srv.store.InsertMemory(memory.Memory{Content: "use JWT", MemoryType: memory.TypeDecision, Importance: 0.8})
	srv.store.InsertMemory(memory.Memory{Content: "camelCase", MemoryType: memory.TypeConvention, Importance: 0.7})

	req := callTool("memvra_list_memories", map[string]interface{}{})

	result, err := srv.handleListMemories(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "use JWT") {
		t.Error("should contain first memory")
	}
	if !strings.Contains(text, "camelCase") {
		t.Error("should contain second memory")
	}
}

func TestListMemories_FiltersByType(t *testing.T) {
	srv := setupTestServer(t)

	srv.store.InsertMemory(memory.Memory{Content: "use JWT", MemoryType: memory.TypeDecision, Importance: 0.8})
	srv.store.InsertMemory(memory.Memory{Content: "camelCase", MemoryType: memory.TypeConvention, Importance: 0.7})

	req := callTool("memvra_list_memories", map[string]interface{}{
		"type": "decision",
	})

	result, err := srv.handleListMemories(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "use JWT") {
		t.Error("should contain decision memory")
	}
	if strings.Contains(text, "camelCase") {
		t.Error("should NOT contain convention memory when filtering by decision")
	}
}

func TestListSessions_RespectsLimit(t *testing.T) {
	srv := setupTestServer(t)

	for i := 0; i < 5; i++ {
		srv.store.InsertSession(memory.Session{
			Question:  "question " + string(rune('A'+i)),
			ModelUsed: "claude",
		})
	}

	req := callTool("memvra_list_sessions", map[string]interface{}{
		"limit": float64(2), // JSON numbers are float64
	})

	result, err := srv.handleListSessions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	// Should only have 2 sessions.
	count := strings.Count(text, "(claude)")
	if count != 2 {
		t.Errorf("expected 2 sessions, got %d in output:\n%s", count, text)
	}
}

func TestListSessions_Empty(t *testing.T) {
	srv := setupTestServer(t)

	req := callTool("memvra_list_sessions", map[string]interface{}{})

	result, err := srv.handleListSessions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "No sessions") {
		t.Errorf("expected 'No sessions' message, got: %s", text)
	}
}

func TestGetContext_ReturnsProjectInfo(t *testing.T) {
	srv := setupTestServer(t)

	// Add a memory so context has content.
	srv.store.InsertMemory(memory.Memory{
		Content:    "Use JWT auth",
		MemoryType: memory.TypeDecision,
		Importance: 0.8,
	})

	req := callTool("memvra_get_context", map[string]interface{}{})

	result, err := srv.handleGetContext(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "testproject") {
		t.Error("context should contain project name")
	}
}

func TestInstallMCPConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-mcp.json")

	// First install.
	if err := installMCPConfig(path); err != nil {
		t.Fatalf("first install: %v", err)
	}

	data, _ := os.ReadFile(path)
	var cfg mcpConfig
	json.Unmarshal(data, &cfg)

	if cfg.MCPServers["memvra"].Command != "memvra" {
		t.Errorf("command: got %q", cfg.MCPServers["memvra"].Command)
	}

	// Install again — should not clobber other servers.
	cfg.MCPServers["other-tool"] = mcpServerEntry{Command: "other", Args: []string{"serve"}}
	updated, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(path, updated, 0o644)

	if err := installMCPConfig(path); err != nil {
		t.Fatalf("second install: %v", err)
	}

	data2, _ := os.ReadFile(path)
	var cfg2 mcpConfig
	json.Unmarshal(data2, &cfg2)

	if cfg2.MCPServers["memvra"].Command != "memvra" {
		t.Error("memvra entry should still exist")
	}
	if cfg2.MCPServers["other-tool"].Command != "other" {
		t.Error("other-tool entry should not be clobbered")
	}
}
