package memory

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/memvra/memvra/internal/db"
)

// VectorStore provides vector similarity search via sqlite-vec.
type VectorStore struct {
	conn *sql.DB
}

// NewVectorStore creates a VectorStore backed by the given DB.
func NewVectorStore(database *db.DB) *VectorStore {
	return &VectorStore{conn: database.Conn()}
}

// UpsertChunkEmbedding inserts or replaces a chunk embedding in vec_chunks.
// sqlite-vec virtual tables don't support ON CONFLICT upsert, so we
// delete the existing row first then insert.
func (v *VectorStore) UpsertChunkEmbedding(id string, embedding []float32) error {
	if len(embedding) == 0 {
		return nil
	}
	blob := float32SliceToBlob(embedding)
	if _, err := v.conn.Exec(`DELETE FROM vec_chunks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("vector: delete old chunk embedding: %w", err)
	}
	if _, err := v.conn.Exec(`INSERT INTO vec_chunks (id, embedding) VALUES (?, ?)`, id, blob); err != nil {
		return fmt.Errorf("vector: insert chunk embedding: %w", err)
	}
	return nil
}

// UpsertMemoryEmbedding inserts or replaces a memory embedding in vec_memories.
func (v *VectorStore) UpsertMemoryEmbedding(id string, embedding []float32) error {
	if len(embedding) == 0 {
		return nil
	}
	blob := float32SliceToBlob(embedding)
	if _, err := v.conn.Exec(`DELETE FROM vec_memories WHERE id = ?`, id); err != nil {
		return fmt.Errorf("vector: delete old memory embedding: %w", err)
	}
	if _, err := v.conn.Exec(`INSERT INTO vec_memories (id, embedding) VALUES (?, ?)`, id, blob); err != nil {
		return fmt.Errorf("vector: insert memory embedding: %w", err)
	}
	return nil
}

// VectorMatch represents a single similarity search result.
type VectorMatch struct {
	ID       string
	Distance float64
}

// SearchChunks finds the top-k most similar chunk embeddings to the query vector.
func (v *VectorStore) SearchChunks(query []float32, topK int, minSimilarity float64) ([]VectorMatch, error) {
	if len(query) == 0 {
		return nil, nil
	}
	blob := float32SliceToBlob(query)
	rows, err := v.conn.Query(
		`SELECT id, distance FROM vec_chunks WHERE embedding MATCH ? AND k = ?
		 ORDER BY distance`,
		blob, topK,
	)
	if err != nil {
		// sqlite-vec may not be loaded; degrade gracefully.
		return nil, nil //nolint:nilerr
	}
	defer func() { _ = rows.Close() }()
	return scanMatches(rows, minSimilarity)
}

// SearchMemories finds the top-k most similar memory embeddings to the query vector.
func (v *VectorStore) SearchMemories(query []float32, topK int, minSimilarity float64) ([]VectorMatch, error) {
	if len(query) == 0 {
		return nil, nil
	}
	blob := float32SliceToBlob(query)
	rows, err := v.conn.Query(
		`SELECT id, distance FROM vec_memories WHERE embedding MATCH ? AND k = ?
		 ORDER BY distance`,
		blob, topK,
	)
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	defer func() { _ = rows.Close() }()
	return scanMatches(rows, minSimilarity)
}

// DeleteChunkEmbedding removes a chunk embedding.
func (v *VectorStore) DeleteChunkEmbedding(id string) error {
	_, err := v.conn.Exec(`DELETE FROM vec_chunks WHERE id = ?`, id)
	return err
}

// DeleteMemoryEmbedding removes a memory embedding.
func (v *VectorStore) DeleteMemoryEmbedding(id string) error {
	_, err := v.conn.Exec(`DELETE FROM vec_memories WHERE id = ?`, id)
	return err
}

// ---- Helpers ----

func scanMatches(rows *sql.Rows, minSimilarity float64) ([]VectorMatch, error) {
	var out []VectorMatch
	for rows.Next() {
		var m VectorMatch
		if err := rows.Scan(&m.ID, &m.Distance); err != nil {
			return nil, err
		}
		// sqlite-vec returns L2 distance; convert to cosine-like similarity:
		// similarity = 1 / (1 + distance). Filter by threshold.
		similarity := 1.0 / (1.0 + m.Distance)
		if similarity >= minSimilarity {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// float32SliceToBlob serialises a float32 slice to a little-endian byte blob.
// This is the format expected by sqlite-vec's BLOB column input.
func float32SliceToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// BlobToFloat32Slice deserialises a little-endian byte blob to a float32 slice.
func BlobToFloat32Slice(b []byte) []float32 {
	result := make([]float32, len(b)/4)
	for i := range result {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		result[i] = math.Float32frombits(bits)
	}
	return result
}
