package memory

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/memvra/memvra/internal/db"
)

func setupTestDB(t *testing.T) (*db.DB, *Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database, NewStore(database)
}

func TestStore_UpsertAndGetProject(t *testing.T) {
	_, store := setupTestDB(t)

	proj := Project{
		Name:      "testproject",
		RootPath:  "/tmp/test",
		TechStack: `{"language":"Go"}`,
	}
	if err := store.UpsertProject(proj); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	got, err := store.GetProject()
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "testproject" {
		t.Errorf("name: got %q, want %q", got.Name, "testproject")
	}
	if got.RootPath != "/tmp/test" {
		t.Errorf("root path: got %q", got.RootPath)
	}
}

func TestStore_UpsertProject_Updates(t *testing.T) {
	_, store := setupTestDB(t)

	proj := Project{Name: "v1", RootPath: "/tmp/test", TechStack: "{}"}
	store.UpsertProject(proj)

	proj.Name = "v2"
	store.UpsertProject(proj)

	got, _ := store.GetProject()
	if got.Name != "v2" {
		t.Errorf("expected updated name 'v2', got %q", got.Name)
	}
}

func TestStore_GetProject_NotInitialised(t *testing.T) {
	_, store := setupTestDB(t)

	_, err := store.GetProject()
	if err == nil {
		t.Error("expected error for uninitialised project")
	}
}

func TestStore_UpsertFile(t *testing.T) {
	_, store := setupTestDB(t)

	f := File{
		Path:         "main.go",
		Language:     "go",
		LastModified: time.Now(),
		ContentHash:  "abc123",
	}
	id, err := store.UpsertFile(f)
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty file ID")
	}

	// Upserting same path should update, not duplicate.
	f.ContentHash = "def456"
	id2, err := store.UpsertFile(f)
	if err != nil {
		t.Fatalf("UpsertFile (update): %v", err)
	}
	if id2 != id {
		t.Errorf("expected same ID on upsert, got %q vs %q", id, id2)
	}
}

func TestStore_GetFileByPath(t *testing.T) {
	_, store := setupTestDB(t)

	f := File{Path: "app.ts", Language: "typescript", LastModified: time.Now(), ContentHash: "hash"}
	store.UpsertFile(f)

	got, err := store.GetFileByPath("app.ts")
	if err != nil {
		t.Fatalf("GetFileByPath: %v", err)
	}
	if got.Language != "typescript" {
		t.Errorf("language: got %q", got.Language)
	}
}

func TestStore_InsertAndListChunks(t *testing.T) {
	_, store := setupTestDB(t)

	fileID, _ := store.UpsertFile(File{Path: "main.go", Language: "go", LastModified: time.Now(), ContentHash: "h"})

	c := Chunk{FileID: fileID, Content: "package main", StartLine: 1, EndLine: 5, ChunkType: "code"}
	if err := store.InsertChunk(c); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	chunks, err := store.ListAllChunks()
	if err != nil {
		t.Fatalf("ListAllChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "package main" {
		t.Errorf("content: got %q", chunks[0].Content)
	}
}

func TestStore_InsertChunkReturningID(t *testing.T) {
	_, store := setupTestDB(t)

	fileID, _ := store.UpsertFile(File{Path: "main.go", Language: "go", LastModified: time.Now(), ContentHash: "h"})

	c := Chunk{FileID: fileID, Content: "func main() {}", StartLine: 1, EndLine: 1, ChunkType: "code"}
	id, err := store.InsertChunkReturningID(c)
	if err != nil {
		t.Fatalf("InsertChunkReturningID: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty chunk ID")
	}
}

func TestStore_DeleteChunksByFileID(t *testing.T) {
	_, store := setupTestDB(t)

	fileID, _ := store.UpsertFile(File{Path: "main.go", Language: "go", LastModified: time.Now(), ContentHash: "h"})
	store.InsertChunk(Chunk{FileID: fileID, Content: "a", StartLine: 1, EndLine: 1, ChunkType: "code"})
	store.InsertChunk(Chunk{FileID: fileID, Content: "b", StartLine: 2, EndLine: 2, ChunkType: "code"})

	if err := store.DeleteChunksByFileID(fileID); err != nil {
		t.Fatalf("DeleteChunksByFileID: %v", err)
	}

	n, _ := store.CountChunks()
	if n != 0 {
		t.Errorf("expected 0 chunks after delete, got %d", n)
	}
}

func TestStore_InsertAndListMemories(t *testing.T) {
	_, store := setupTestDB(t)

	m := Memory{
		Content:    "Use PostgreSQL",
		MemoryType: TypeDecision,
		Importance: 0.8,
		Source:     "user",
	}
	id, err := store.InsertMemory(m)
	if err != nil {
		t.Fatalf("InsertMemory: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty memory ID")
	}

	memories, err := store.ListMemories("")
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Content != "Use PostgreSQL" {
		t.Errorf("content: got %q", memories[0].Content)
	}
}

func TestStore_ListMemories_FilterByType(t *testing.T) {
	_, store := setupTestDB(t)

	store.InsertMemory(Memory{Content: "a", MemoryType: TypeDecision, Importance: 0.8})
	store.InsertMemory(Memory{Content: "b", MemoryType: TypeNote, Importance: 0.5})

	decisions, _ := store.ListMemories(TypeDecision)
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(decisions))
	}

	notes, _ := store.ListMemories(TypeNote)
	if len(notes) != 1 {
		t.Errorf("expected 1 note, got %d", len(notes))
	}
}

func TestStore_DeleteMemory(t *testing.T) {
	_, store := setupTestDB(t)

	id, _ := store.InsertMemory(Memory{Content: "temp", MemoryType: TypeNote, Importance: 0.5})
	if err := store.DeleteMemory(id); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}

	memories, _ := store.ListMemories("")
	if len(memories) != 0 {
		t.Errorf("expected 0 memories after delete, got %d", len(memories))
	}
}

func TestStore_DeleteMemory_NotFound(t *testing.T) {
	_, store := setupTestDB(t)

	err := store.DeleteMemory("nonexistent")
	if err == nil {
		t.Error("expected error deleting nonexistent memory")
	}
}

func TestStore_DeleteMemoriesByType(t *testing.T) {
	_, store := setupTestDB(t)

	store.InsertMemory(Memory{Content: "a", MemoryType: TypeNote, Importance: 0.5})
	store.InsertMemory(Memory{Content: "b", MemoryType: TypeNote, Importance: 0.5})
	store.InsertMemory(Memory{Content: "c", MemoryType: TypeDecision, Importance: 0.8})

	n, err := store.DeleteMemoriesByType(TypeNote)
	if err != nil {
		t.Fatalf("DeleteMemoriesByType: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}

	remaining, _ := store.ListMemories("")
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining memory, got %d", len(remaining))
	}
}

func TestStore_DeleteAllMemories(t *testing.T) {
	_, store := setupTestDB(t)

	store.InsertMemory(Memory{Content: "a", MemoryType: TypeNote, Importance: 0.5})
	store.InsertMemory(Memory{Content: "b", MemoryType: TypeDecision, Importance: 0.8})

	n, err := store.DeleteAllMemories()
	if err != nil {
		t.Fatalf("DeleteAllMemories: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
}

func TestStore_CountMemoriesByType(t *testing.T) {
	_, store := setupTestDB(t)

	store.InsertMemory(Memory{Content: "a", MemoryType: TypeDecision, Importance: 0.8})
	store.InsertMemory(Memory{Content: "b", MemoryType: TypeDecision, Importance: 0.8})
	store.InsertMemory(Memory{Content: "c", MemoryType: TypeNote, Importance: 0.5})

	counts, err := store.CountMemoriesByType()
	if err != nil {
		t.Fatalf("CountMemoriesByType: %v", err)
	}
	if counts[TypeDecision] != 2 {
		t.Errorf("decisions: got %d, want 2", counts[TypeDecision])
	}
	if counts[TypeNote] != 1 {
		t.Errorf("notes: got %d, want 1", counts[TypeNote])
	}
}

func TestStore_InsertAndCountSessions(t *testing.T) {
	_, store := setupTestDB(t)

	sess := Session{
		Question:        "How do I deploy?",
		ContextUsed:     "{}",
		ResponseSummary: "Use docker compose.",
		ModelUsed:       "claude",
		TokensUsed:      100,
	}
	if err := store.InsertSession(sess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	n, err := store.CountSessions()
	if err != nil {
		t.Fatalf("CountSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 session, got %d", n)
	}
}

func TestStore_GetMemoryByID(t *testing.T) {
	_, store := setupTestDB(t)

	id, _ := store.InsertMemory(Memory{
		Content:      "test memory",
		MemoryType:   TypeNote,
		Importance:   0.5,
		RelatedFiles: []string{"main.go", "app.ts"},
	})

	got, err := store.GetMemoryByID(id)
	if err != nil {
		t.Fatalf("GetMemoryByID: %v", err)
	}
	if got.Content != "test memory" {
		t.Errorf("content: got %q", got.Content)
	}
	if len(got.RelatedFiles) != 2 {
		t.Errorf("related files: got %d, want 2", len(got.RelatedFiles))
	}
}

func TestStore_GetChunkByID(t *testing.T) {
	_, store := setupTestDB(t)

	fileID, _ := store.UpsertFile(File{Path: "main.go", Language: "go", LastModified: time.Now(), ContentHash: "h"})
	chunkID, _ := store.InsertChunkReturningID(Chunk{FileID: fileID, Content: "package main", StartLine: 1, EndLine: 1, ChunkType: "code"})

	got, err := store.GetChunkByID(chunkID)
	if err != nil {
		t.Fatalf("GetChunkByID: %v", err)
	}
	if got.Content != "package main" {
		t.Errorf("content: got %q", got.Content)
	}
}

func TestStore_ListFiles(t *testing.T) {
	_, store := setupTestDB(t)

	store.UpsertFile(File{Path: "a.go", Language: "go", LastModified: time.Now(), ContentHash: "h1"})
	store.UpsertFile(File{Path: "b.go", Language: "go", LastModified: time.Now(), ContentHash: "h2"})

	files, err := store.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestStore_DeleteFile(t *testing.T) {
	_, store := setupTestDB(t)

	id, _ := store.UpsertFile(File{Path: "main.go", Language: "go", LastModified: time.Now(), ContentHash: "h"})
	// Add a chunk to verify cascade delete.
	store.InsertChunk(Chunk{FileID: id, Content: "code", StartLine: 1, EndLine: 1, ChunkType: "code"})

	if err := store.DeleteFile(id); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	n, _ := store.CountFiles()
	if n != 0 {
		t.Errorf("expected 0 files after delete, got %d", n)
	}
	cn, _ := store.CountChunks()
	if cn != 0 {
		t.Errorf("expected cascade delete of chunks, got %d", cn)
	}
}

func TestStore_CountFiles(t *testing.T) {
	_, store := setupTestDB(t)

	n, _ := store.CountFiles()
	if n != 0 {
		t.Errorf("expected 0 files initially, got %d", n)
	}
}

func TestStore_InsertMemory_DefaultSource(t *testing.T) {
	_, store := setupTestDB(t)

	id, _ := store.InsertMemory(Memory{Content: "test", MemoryType: TypeNote, Importance: 0.5})
	got, _ := store.GetMemoryByID(id)
	if got.Source != "user" {
		t.Errorf("default source: got %q, want %q", got.Source, "user")
	}
}

func TestStore_PruneSessionsKeepLatest(t *testing.T) {
	_, store := setupTestDB(t)

	// Insert 5 sessions.
	for i := 0; i < 5; i++ {
		store.InsertSession(Session{
			Question:        fmt.Sprintf("question %d", i),
			ContextUsed:     "{}",
			ResponseSummary: "answer",
			ModelUsed:       "claude",
			TokensUsed:      100,
		})
	}

	// Keep latest 2.
	pruned, err := store.PruneSessionsKeepLatest(2)
	if err != nil {
		t.Fatalf("PruneSessionsKeepLatest: %v", err)
	}
	if pruned != 3 {
		t.Errorf("expected 3 pruned, got %d", pruned)
	}

	n, _ := store.CountSessions()
	if n != 2 {
		t.Errorf("expected 2 remaining, got %d", n)
	}
}

func TestStore_PruneSessions_OlderThanDays(t *testing.T) {
	_, store := setupTestDB(t)

	// Insert a session (created now).
	store.InsertSession(Session{
		Question:    "recent",
		ContextUsed: "{}",
		ModelUsed:   "claude",
	})

	// Prune sessions older than 1 day — should delete nothing (session just created).
	pruned, err := store.PruneSessions(1)
	if err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned for fresh sessions, got %d", pruned)
	}

	// Prune sessions older than 0 days — should still keep today's.
	// (datetime comparison: sessions from today are not older than 0 days ago)
	n, _ := store.CountSessions()
	if n != 1 {
		t.Errorf("expected 1 remaining, got %d", n)
	}
}

func TestStore_InsertSessionReturningID(t *testing.T) {
	_, store := setupTestDB(t)

	sess := Session{
		Question:        "How do I deploy?",
		ContextUsed:     "{}",
		ResponseSummary: "Use docker.",
		ModelUsed:       "claude",
		TokensUsed:      100,
	}
	id, err := store.InsertSessionReturningID(sess)
	if err != nil {
		t.Fatalf("InsertSessionReturningID: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestStore_UpdateSessionSummary(t *testing.T) {
	_, store := setupTestDB(t)

	id, _ := store.InsertSessionReturningID(Session{
		Question: "q", ContextUsed: "{}", ResponseSummary: "old", ModelUsed: "claude",
	})
	if err := store.UpdateSessionSummary(id, "new summary"); err != nil {
		t.Fatalf("UpdateSessionSummary: %v", err)
	}
	sessions, _ := store.GetLastNSessions(1)
	if sessions[0].ResponseSummary != "new summary" {
		t.Errorf("expected updated summary, got %q", sessions[0].ResponseSummary)
	}
}

func TestStore_GetLastNSessions(t *testing.T) {
	_, store := setupTestDB(t)

	for i := 0; i < 5; i++ {
		store.InsertSession(Session{
			Question: fmt.Sprintf("q%d", i), ContextUsed: "{}", ModelUsed: "claude",
		})
	}

	sessions, err := store.GetLastNSessions(3)
	if err != nil {
		t.Fatalf("GetLastNSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
}

func TestStore_GetLastNSessions_Zero(t *testing.T) {
	_, store := setupTestDB(t)

	sessions, err := store.GetLastNSessions(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil for n=0, got %v", sessions)
	}
}

func TestStore_PruneSessionsKeepLatest_KeepAll(t *testing.T) {
	_, store := setupTestDB(t)

	store.InsertSession(Session{Question: "q1", ContextUsed: "{}", ModelUsed: "claude"})
	store.InsertSession(Session{Question: "q2", ContextUsed: "{}", ModelUsed: "claude"})

	// Keep more than exist — should delete nothing.
	pruned, _ := store.PruneSessionsKeepLatest(100)
	if pruned != 0 {
		t.Errorf("expected 0 pruned when keeping more than exist, got %d", pruned)
	}
}
