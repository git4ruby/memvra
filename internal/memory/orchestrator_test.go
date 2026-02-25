package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/memvra/memvra/internal/db"
)

// stubEmbedder returns deterministic embeddings for testing.
type stubEmbedder struct {
	embeddings [][]float32
	err        error
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	// Return one embedding per input text, cycling through available embeddings.
	out := make([][]float32, len(texts))
	for i := range texts {
		idx := i % len(s.embeddings)
		out[i] = s.embeddings[idx]
	}
	return out, nil
}

func setupOrchestratorDB(t *testing.T) (*db.DB, *Store, *VectorStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "orch_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	store := NewStore(database)
	vectors := NewVectorStore(database)
	return database, store, vectors
}

// --- Retrieve tests ---

func TestOrchestrator_Retrieve_NoEmbedder(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)

	// Seed some memories.
	store.InsertMemory(Memory{Content: "use PostgreSQL", MemoryType: TypeDecision, Importance: 0.8})
	store.InsertMemory(Memory{Content: "always validate input", MemoryType: TypeConstraint, Importance: 0.8})

	// nil embedder — should fall back to listing all memories.
	orch := NewOrchestrator(store, vectors, NewRanker(), nil)

	result, err := orch.Retrieve(context.Background(), "database choice", RetrieveOptions{
		TopKChunks:   10,
		TopKMemories: 5,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.Memories) != 2 {
		t.Errorf("expected 2 memories (fallback to all), got %d", len(result.Memories))
	}
	if len(result.Chunks) != 0 {
		t.Errorf("expected 0 chunks (no vector search), got %d", len(result.Chunks))
	}
}

func TestOrchestrator_Retrieve_EmbedError(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)

	store.InsertMemory(Memory{Content: "a note", MemoryType: TypeNote, Importance: 0.5})

	// Embedder that always errors — should fall back gracefully.
	emb := &stubEmbedder{err: errors.New("embed failed")}
	orch := NewOrchestrator(store, vectors, NewRanker(), emb)

	result, err := orch.Retrieve(context.Background(), "query", RetrieveOptions{
		TopKChunks:   10,
		TopKMemories: 5,
	})
	if err != nil {
		t.Fatalf("Retrieve should not error on embed failure: %v", err)
	}
	if len(result.Memories) != 1 {
		t.Errorf("expected 1 memory (fallback), got %d", len(result.Memories))
	}
}

func TestOrchestrator_Retrieve_WithEmbedder(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)

	// Create a file and chunk so we have something to search.
	fileID, _ := store.UpsertFile(File{Path: "main.go", Language: "go", LastModified: time.Now(), ContentHash: "h1"})
	chunkID, _ := store.InsertChunkReturningID(Chunk{
		FileID: fileID, Content: "func main() {}", StartLine: 1, EndLine: 1, ChunkType: "code",
	})

	// Create a memory.
	memID, _ := store.InsertMemory(Memory{Content: "use Go", MemoryType: TypeDecision, Importance: 0.8})

	// Store embeddings for both.
	vec := makeVec(1.0)
	vectors.UpsertChunkEmbedding(chunkID, vec)
	vectors.UpsertMemoryEmbedding(memID, vec)

	// Embedder returns a similar vector.
	emb := &stubEmbedder{embeddings: [][]float32{makeVec(1.1)}}
	orch := NewOrchestrator(store, vectors, NewRanker(), emb)

	result, err := orch.Retrieve(context.Background(), "how does main work?", RetrieveOptions{
		TopKChunks:          10,
		TopKMemories:        5,
		SimilarityThreshold: 0.0,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.Chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(result.Chunks))
	}
	if len(result.Memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(result.Memories))
	}
	if len(result.Chunks) > 0 && result.Chunks[0].Content != "func main() {}" {
		t.Errorf("unexpected chunk content: %q", result.Chunks[0].Content)
	}
}

// --- Remember tests ---

func TestOrchestrator_Remember_StoresMemory(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)
	orch := NewOrchestrator(store, vectors, NewRanker(), nil)

	mem, err := orch.Remember(context.Background(), "use PostgreSQL", TypeDecision, "user")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if mem.ID == "" {
		t.Error("expected non-empty memory ID")
	}
	if mem.Content != "use PostgreSQL" {
		t.Errorf("content: got %q", mem.Content)
	}
	if mem.MemoryType != TypeDecision {
		t.Errorf("type: got %q, want %q", mem.MemoryType, TypeDecision)
	}
	if mem.Importance != 0.8 {
		t.Errorf("importance: got %f, want 0.8", mem.Importance)
	}

	// Verify it's actually in the store.
	got, err := store.GetMemoryByID(mem.ID)
	if err != nil {
		t.Fatalf("GetMemoryByID: %v", err)
	}
	if got.Content != "use PostgreSQL" {
		t.Errorf("stored content: got %q", got.Content)
	}
}

func TestOrchestrator_Remember_WithEmbedding(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)

	emb := &stubEmbedder{embeddings: [][]float32{makeVec(2.0)}}
	orch := NewOrchestrator(store, vectors, NewRanker(), emb)

	mem, err := orch.Remember(context.Background(), "always validate input", TypeConstraint, "user")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// The embedding should be searchable.
	query := makeVec(2.0)
	matches, _ := vectors.SearchMemories(query, 10, 0.0)
	found := false
	for _, m := range matches {
		if m.ID == mem.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected memory embedding to be searchable after Remember")
	}
}

func TestOrchestrator_Remember_InvalidType(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)
	orch := NewOrchestrator(store, vectors, NewRanker(), nil)

	_, err := orch.Remember(context.Background(), "test", MemoryType("invalid"), "user")
	if err == nil {
		t.Error("expected error for invalid memory type")
	}
}

// --- Forget tests ---

func TestOrchestrator_Forget(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)

	emb := &stubEmbedder{embeddings: [][]float32{makeVec(3.0)}}
	orch := NewOrchestrator(store, vectors, NewRanker(), emb)

	mem, _ := orch.Remember(context.Background(), "to be forgotten", TypeNote, "user")

	if err := orch.Forget(mem.ID); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	// Memory should be gone from store.
	_, err := store.GetMemoryByID(mem.ID)
	if err == nil {
		t.Error("expected error for deleted memory")
	}

	// Embedding should also be gone.
	query := makeVec(3.0)
	matches, _ := vectors.SearchMemories(query, 10, 0.0)
	for _, m := range matches {
		if m.ID == mem.ID {
			t.Error("deleted memory embedding should not appear in search")
		}
	}
}

func TestOrchestrator_ForgetByType(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)
	orch := NewOrchestrator(store, vectors, NewRanker(), nil)

	orch.Remember(context.Background(), "note 1", TypeNote, "user")
	orch.Remember(context.Background(), "note 2", TypeNote, "user")
	orch.Remember(context.Background(), "keep this decision", TypeDecision, "user")

	if err := orch.ForgetByType("note"); err != nil {
		t.Fatalf("ForgetByType: %v", err)
	}

	remaining, _ := store.ListMemories("")
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining memory, got %d", len(remaining))
	}
	if remaining[0].MemoryType != TypeDecision {
		t.Errorf("expected remaining memory to be decision, got %q", remaining[0].MemoryType)
	}
}

func TestOrchestrator_ForgetByType_InvalidType(t *testing.T) {
	_, store, vectors := setupOrchestratorDB(t)
	orch := NewOrchestrator(store, vectors, NewRanker(), nil)

	err := orch.ForgetByType("bogus")
	if err == nil {
		t.Error("expected error for invalid memory type")
	}
}
