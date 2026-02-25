package memory

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"

	"github.com/memvra/memvra/internal/db"
)

func TestFloat32SliceToBlob(t *testing.T) {
	input := []float32{1.0, 2.0, 3.0}
	blob := float32SliceToBlob(input)

	if len(blob) != 12 { // 3 floats * 4 bytes each
		t.Fatalf("expected 12 bytes, got %d", len(blob))
	}

	// Verify first float.
	bits := binary.LittleEndian.Uint32(blob[0:4])
	val := math.Float32frombits(bits)
	if val != 1.0 {
		t.Errorf("first float: got %f, want 1.0", val)
	}
}

func TestBlobToFloat32Slice(t *testing.T) {
	original := []float32{1.5, -2.5, 3.14}
	blob := float32SliceToBlob(original)
	result := BlobToFloat32Slice(blob)

	if len(result) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(original))
	}
	for i, v := range result {
		if v != original[i] {
			t.Errorf("index %d: got %f, want %f", i, v, original[i])
		}
	}
}

func TestFloat32RoundTrip(t *testing.T) {
	input := []float32{0.0, -1.0, 1e-10, 1e10, math.MaxFloat32}
	blob := float32SliceToBlob(input)
	output := BlobToFloat32Slice(blob)

	for i := range input {
		if input[i] != output[i] {
			t.Errorf("round-trip failed at index %d: %f != %f", i, input[i], output[i])
		}
	}
}

func TestFloat32SliceToBlob_Empty(t *testing.T) {
	blob := float32SliceToBlob(nil)
	if len(blob) != 0 {
		t.Errorf("expected empty blob for nil input, got %d bytes", len(blob))
	}
}

func TestBlobToFloat32Slice_Empty(t *testing.T) {
	result := BlobToFloat32Slice(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice for nil blob, got %d elements", len(result))
	}
}

// ---- VectorStore DB integration tests ----

// makeVec creates a 768-dim vector with a distinguishable pattern.
// Using dim=0 makes it a "base" vector; varying dims create distance.
func makeVec(base float32) []float32 {
	v := make([]float32, 768)
	for i := range v {
		v[i] = base
	}
	return v
}

func setupVectorTestDB(t *testing.T) (*db.DB, *VectorStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vec_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database, NewVectorStore(database)
}

func TestVectorStore_UpsertAndSearchChunks(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	// Insert two chunk embeddings with different patterns.
	vecA := makeVec(1.0)
	vecB := makeVec(5.0)

	if err := vs.UpsertChunkEmbedding("chunk-1", vecA); err != nil {
		t.Fatalf("UpsertChunkEmbedding A: %v", err)
	}
	if err := vs.UpsertChunkEmbedding("chunk-2", vecB); err != nil {
		t.Fatalf("UpsertChunkEmbedding B: %v", err)
	}

	// Search with a query close to vecA.
	query := makeVec(1.1)
	matches, err := vs.SearchChunks(query, 10, 0.0)
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected at least 2 matches, got %d", len(matches))
	}
	// The closest match should be chunk-1.
	if matches[0].ID != "chunk-1" {
		t.Errorf("expected chunk-1 as closest match, got %q", matches[0].ID)
	}
}

func TestVectorStore_UpsertAndSearchMemories(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	vecA := makeVec(1.0)
	vecB := makeVec(10.0)

	if err := vs.UpsertMemoryEmbedding("mem-1", vecA); err != nil {
		t.Fatalf("UpsertMemoryEmbedding A: %v", err)
	}
	if err := vs.UpsertMemoryEmbedding("mem-2", vecB); err != nil {
		t.Fatalf("UpsertMemoryEmbedding B: %v", err)
	}

	query := makeVec(1.1)
	matches, err := vs.SearchMemories(query, 10, 0.0)
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected at least 2 matches, got %d", len(matches))
	}
	if matches[0].ID != "mem-1" {
		t.Errorf("expected mem-1 as closest match, got %q", matches[0].ID)
	}
}

func TestVectorStore_Search_EmptyQuery(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	matches, err := vs.SearchChunks(nil, 10, 0.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil for empty query, got %v", matches)
	}

	matches, err = vs.SearchMemories([]float32{}, 10, 0.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil for empty query, got %v", matches)
	}
}

func TestVectorStore_Search_SimilarityThreshold(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	// Insert two vectors: one close to query, one far away.
	close := makeVec(1.0)
	far := makeVec(100.0)

	vs.UpsertChunkEmbedding("close", close)
	vs.UpsertChunkEmbedding("far", far)

	// Search with a high threshold — should only return the close match.
	query := makeVec(1.0)
	matches, err := vs.SearchChunks(query, 10, 0.99)
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	// At minimum, the exact match should pass the threshold.
	found := false
	for _, m := range matches {
		if m.ID == "close" {
			found = true
		}
		if m.ID == "far" {
			t.Error("far vector should not pass high similarity threshold")
		}
	}
	if !found {
		t.Error("expected close vector to pass threshold")
	}
}

func TestVectorStore_Upsert_EmptyEmbedding(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	// Empty embeddings should be no-ops (no error).
	if err := vs.UpsertChunkEmbedding("id", nil); err != nil {
		t.Errorf("expected no error for nil embedding, got: %v", err)
	}
	if err := vs.UpsertChunkEmbedding("id", []float32{}); err != nil {
		t.Errorf("expected no error for empty embedding, got: %v", err)
	}
	if err := vs.UpsertMemoryEmbedding("id", nil); err != nil {
		t.Errorf("expected no error for nil embedding, got: %v", err)
	}
}

func TestVectorStore_DeleteChunkEmbedding(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	vec := makeVec(1.0)
	vs.UpsertChunkEmbedding("to-delete", vec)

	if err := vs.DeleteChunkEmbedding("to-delete"); err != nil {
		t.Fatalf("DeleteChunkEmbedding: %v", err)
	}

	// Searching should return no results.
	matches, _ := vs.SearchChunks(vec, 10, 0.0)
	for _, m := range matches {
		if m.ID == "to-delete" {
			t.Error("deleted embedding should not appear in search results")
		}
	}
}

func TestVectorStore_DeleteMemoryEmbedding(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	vec := makeVec(1.0)
	vs.UpsertMemoryEmbedding("to-delete", vec)

	if err := vs.DeleteMemoryEmbedding("to-delete"); err != nil {
		t.Fatalf("DeleteMemoryEmbedding: %v", err)
	}

	matches, _ := vs.SearchMemories(vec, 10, 0.0)
	for _, m := range matches {
		if m.ID == "to-delete" {
			t.Error("deleted embedding should not appear in search results")
		}
	}
}

func TestVectorStore_Upsert_Replaces(t *testing.T) {
	_, vs := setupVectorTestDB(t)

	original := makeVec(1.0)
	updated := makeVec(50.0)

	vs.UpsertChunkEmbedding("replace-me", original)
	// Upsert with a very different vector.
	vs.UpsertChunkEmbedding("replace-me", updated)

	// Search with a query near the updated vector — should find it.
	query := makeVec(50.0)
	matches, _ := vs.SearchChunks(query, 10, 0.0)

	found := false
	for _, m := range matches {
		if m.ID == "replace-me" {
			found = true
			// Distance should be ~0 since we're querying with the same vector.
			if m.Distance > 1.0 {
				t.Errorf("expected small distance after upsert, got %f", m.Distance)
			}
		}
	}
	if !found {
		t.Error("upserted embedding not found in search results")
	}
}
