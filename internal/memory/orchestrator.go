package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/memvra/memvra/internal/adapter"
)

// Orchestrator coordinates storage, embedding, and retrieval of memories and chunks.
type Orchestrator struct {
	store    *Store
	vectors  *VectorStore
	ranker   *Ranker
	embedder adapter.Embedder
}

// NewOrchestrator creates an Orchestrator.
func NewOrchestrator(store *Store, vectors *VectorStore, ranker *Ranker, embedder adapter.Embedder) *Orchestrator {
	return &Orchestrator{
		store:    store,
		vectors:  vectors,
		ranker:   ranker,
		embedder: embedder,
	}
}

// RetrieveOptions controls how many results to pull back.
type RetrieveOptions struct {
	TopKChunks          int
	TopKMemories        int
	SimilarityThreshold float64
}

// RetrievalResult holds ranked results for context building.
type RetrievalResult struct {
	Chunks   []Chunk
	Memories []Memory
}

// Retrieve embeds the query and returns ranked chunks and memories.
func (o *Orchestrator) Retrieve(ctx context.Context, query string, opts RetrieveOptions) (*RetrievalResult, error) {
	// No embedder configured — fall back to listing all memories without ranking.
	if o.embedder == nil {
		mems, _ := o.store.ListMemories("")
		return &RetrievalResult{Memories: mems}, nil
	}

	// Embed the query.
	vecs, err := o.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		// Graceful degradation: no embeddings available — fall back to all memories.
		mems, _ := o.store.ListMemories("")
		return &RetrievalResult{Memories: mems}, nil
	}
	queryVec := vecs[0]

	// Vector search for chunks.
	chunkMatches, _ := o.vectors.SearchChunks(queryVec, opts.TopKChunks, opts.SimilarityThreshold)

	// Vector search for memories.
	memMatches, _ := o.vectors.SearchMemories(queryVec, opts.TopKMemories, opts.SimilarityThreshold)

	// Fetch full chunk records and build similarity map.
	chunkSimMap := make(map[string]float64, len(chunkMatches))
	chunks := make([]Chunk, 0, len(chunkMatches))
	for _, m := range chunkMatches {
		sim := 1.0 / (1.0 + m.Distance)
		chunkSimMap[m.ID] = sim
		c, err := o.store.GetChunkByID(m.ID)
		if err != nil {
			continue
		}
		chunks = append(chunks, c)
	}

	// Fetch full memory records and build similarity map.
	memSimMap := make(map[string]float64, len(memMatches))
	memories := make([]Memory, 0, len(memMatches))
	for _, m := range memMatches {
		sim := 1.0 / (1.0 + m.Distance)
		memSimMap[m.ID] = sim
		mem, err := o.store.GetMemoryByID(m.ID)
		if err != nil {
			continue
		}
		memories = append(memories, mem)
	}

	// Rank results.
	rankedChunks := o.ranker.RankChunks(chunks, chunkSimMap)
	rankedMems := o.ranker.RankMemories(memories, memSimMap)

	// Convert back to plain slices for the caller.
	outChunks := make([]Chunk, len(rankedChunks))
	for i, rc := range rankedChunks {
		outChunks[i] = rc.Chunk
	}
	outMems := make([]Memory, len(rankedMems))
	for i, rm := range rankedMems {
		outMems[i] = rm.Memory
	}

	return &RetrievalResult{
		Chunks:   outChunks,
		Memories: outMems,
	}, nil
}

// Remember stores a memory with its embedding.
func (o *Orchestrator) Remember(ctx context.Context, content string, memType MemoryType, source string) (Memory, error) {
	if !ValidMemoryType(memType) {
		return Memory{}, fmt.Errorf("orchestrator: invalid memory type %q", memType)
	}

	m := Memory{
		Content:    content,
		MemoryType: memType,
		Importance: defaultImportance(memType),
		Source:     source,
	}

	id, err := o.store.InsertMemory(m)
	if err != nil {
		return Memory{}, fmt.Errorf("orchestrator: insert memory: %w", err)
	}
	m.ID = id

	// Generate and store embedding (best-effort — non-fatal on failure).
	if o.embedder != nil {
		vecs, err := o.embedder.Embed(ctx, []string{content})
		if err == nil && len(vecs) > 0 {
			_ = o.vectors.UpsertMemoryEmbedding(id, vecs[0])
		}
	}

	return m, nil
}

// Forget removes a memory by ID (and its vector embedding).
func (o *Orchestrator) Forget(id string) error {
	if err := o.store.DeleteMemory(id); err != nil {
		return err
	}
	_ = o.vectors.DeleteMemoryEmbedding(id)
	return nil
}

// ForgetByType removes all memories of a given type.
func (o *Orchestrator) ForgetByType(typeName string) error {
	mt := MemoryType(typeName)
	if !ValidMemoryType(mt) {
		return fmt.Errorf("orchestrator: unknown memory type %q", typeName)
	}
	_, err := o.store.DeleteMemoriesByType(mt)
	return err
}

// ClassifyMemoryType returns the best-guess MemoryType for a statement.
func ClassifyMemoryType(statement string) MemoryType {
	lower := strings.ToLower(statement)
	switch {
	case strings.HasPrefix(lower, "todo") || strings.Contains(lower, "need to ") || strings.Contains(lower, "should "):
		return TypeTodo
	case strings.Contains(lower, "decided") || strings.Contains(lower, "switched") ||
		strings.Contains(lower, "chose") || strings.Contains(lower, "migrated"):
		return TypeDecision
	case strings.Contains(lower, "must ") || strings.Contains(lower, "never ") ||
		strings.Contains(lower, "always ") || strings.Contains(lower, "only "):
		return TypeConstraint
	case strings.Contains(lower, "convention") || strings.Contains(lower, "pattern") ||
		strings.Contains(lower, "style") || strings.Contains(lower, "format"):
		return TypeConvention
	default:
		return TypeNote
	}
}

func defaultImportance(t MemoryType) float64 {
	switch t {
	case TypeDecision, TypeConstraint:
		return 0.8
	case TypeConvention:
		return 0.7
	case TypeTodo:
		return 0.6
	default:
		return 0.5
	}
}
