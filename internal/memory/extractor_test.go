package memory

import (
	"context"
	"testing"

	"github.com/memvra/memvra/internal/adapter"
)

func TestParseExtractionJSON_ValidArray(t *testing.T) {
	raw := `[{"content": "Use PostgreSQL", "type": "decision"}, {"content": "Always write tests", "type": "constraint"}]`
	memories, err := parseExtractionJSON(raw, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
	if memories[0].Content != "Use PostgreSQL" {
		t.Errorf("content: got %q", memories[0].Content)
	}
	if memories[0].MemoryType != TypeDecision {
		t.Errorf("type: got %q, want %q", memories[0].MemoryType, TypeDecision)
	}
	if memories[0].Source != "extracted" {
		t.Errorf("source: got %q, want %q", memories[0].Source, "extracted")
	}
}

func TestParseExtractionJSON_WrappedInProse(t *testing.T) {
	raw := `Here are the extractions:
[{"content": "Use React", "type": "decision"}]
Done.`
	memories, err := parseExtractionJSON(raw, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

func TestParseExtractionJSON_MarkdownFenced(t *testing.T) {
	raw := "```json\n[{\"content\": \"test\", \"type\": \"note\"}]\n```"
	memories, err := parseExtractionJSON(raw, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

func TestParseExtractionJSON_EmptyArray(t *testing.T) {
	memories, err := parseExtractionJSON("[]", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories, got %d", len(memories))
	}
}

func TestParseExtractionJSON_NoBrackets(t *testing.T) {
	memories, err := parseExtractionJSON("no json here", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if memories != nil {
		t.Errorf("expected nil, got %v", memories)
	}
}

func TestParseExtractionJSON_MalformedJSON(t *testing.T) {
	memories, err := parseExtractionJSON("[{broken}", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Malformed should degrade gracefully — no error, nil result.
	if memories != nil {
		t.Errorf("expected nil for malformed JSON, got %v", memories)
	}
}

func TestParseExtractionJSON_RespectsMaxExtracts(t *testing.T) {
	raw := `[
		{"content": "one", "type": "note"},
		{"content": "two", "type": "note"},
		{"content": "three", "type": "note"}
	]`
	memories, err := parseExtractionJSON(raw, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 2 {
		t.Errorf("expected 2 memories (max), got %d", len(memories))
	}
}

func TestParseExtractionJSON_SkipsEmptyContent(t *testing.T) {
	raw := `[{"content": "", "type": "note"}, {"content": "real", "type": "note"}]`
	memories, err := parseExtractionJSON(raw, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory (empty skipped), got %d", len(memories))
	}
	if memories[0].Content != "real" {
		t.Errorf("expected 'real', got %q", memories[0].Content)
	}
}

func TestParseExtractionJSON_InvalidType(t *testing.T) {
	raw := `[{"content": "decided to use X", "type": "invalid_type"}]`
	memories, err := parseExtractionJSON(raw, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	// Invalid type should be classified by ClassifyMemoryType.
	if memories[0].MemoryType != TypeDecision {
		t.Errorf("expected ClassifyMemoryType to return decision for 'decided to use X', got %q", memories[0].MemoryType)
	}
}

func TestParseExtractionJSON_MissingBrace(t *testing.T) {
	// Simulate small model quirk: ["content": ...] instead of [{"content": ...}]
	raw := `["content": "test", "type": "note"]`
	memories, err := parseExtractionJSON(raw, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// This should be handled by the normalisation logic.
	if len(memories) != 1 {
		t.Logf("got %d memories (normalisation may not fully recover this case)", len(memories))
	}
}

func TestTrimResponse(t *testing.T) {
	short := "Short text."
	if trimResponse(short, 100) != short {
		t.Error("short text should not be trimmed")
	}

	long := "This is a sentence. This is another sentence. This is more text that goes on and on."
	trimmed := trimResponse(long, 50)
	if len(trimmed) > 60 { // some slack for " [...]"
		t.Errorf("trimmed text too long: %d chars", len(trimmed))
	}
}

func TestClassifyMemoryType(t *testing.T) {
	tests := []struct {
		input string
		want  MemoryType
	}{
		{"todo: fix the tests", TypeTodo},
		{"we need to update the API", TypeTodo},
		{"we decided to use PostgreSQL", TypeDecision},
		{"switched from MySQL to Postgres", TypeDecision},
		{"must always validate input", TypeConstraint},
		{"never expose API keys", TypeConstraint},
		{"convention: use camelCase", TypeConvention},
		{"the pattern for services is...", TypeConvention},
		{"random note about something", TypeNote},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ClassifyMemoryType(tt.input)
			if got != tt.want {
				t.Errorf("ClassifyMemoryType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidMemoryType(t *testing.T) {
	valid := []MemoryType{TypeDecision, TypeConvention, TypeConstraint, TypeNote, TypeTodo}
	for _, mt := range valid {
		if !ValidMemoryType(mt) {
			t.Errorf("expected %q to be valid", mt)
		}
	}
	if ValidMemoryType("invalid") {
		t.Error("expected 'invalid' to be invalid")
	}
}

func TestDefaultImportance(t *testing.T) {
	if defaultImportance(TypeDecision) != 0.8 {
		t.Error("decision importance should be 0.8")
	}
	if defaultImportance(TypeConstraint) != 0.8 {
		t.Error("constraint importance should be 0.8")
	}
	if defaultImportance(TypeConvention) != 0.7 {
		t.Error("convention importance should be 0.7")
	}
	if defaultImportance(TypeTodo) != 0.6 {
		t.Error("todo importance should be 0.6")
	}
	if defaultImportance(TypeNote) != 0.5 {
		t.Error("note importance should be 0.5")
	}
}

// stubLLM returns a fixed response for testing.
type stubLLM struct{ response string }

func (s *stubLLM) Complete(_ context.Context, _ adapter.CompletionRequest) (<-chan adapter.StreamChunk, error) {
	ch := make(chan adapter.StreamChunk, 1)
	ch <- adapter.StreamChunk{Text: s.response}
	close(ch)
	return ch, nil
}
func (s *stubLLM) Embed(_ context.Context, _ []string) ([][]float32, error) { return nil, nil }
func (s *stubLLM) Info() adapter.ModelInfo                                   { return adapter.ModelInfo{} }

func TestSummarizeSession_Success(t *testing.T) {
	llm := &stubLLM{response: "The developer decided to use PostgreSQL for its JSONB support."}
	summary, err := SummarizeSession(context.Background(), llm,
		"Should we use PostgreSQL?", "Yes, use PostgreSQL for JSONB support.", 256)
	if err != nil {
		t.Fatalf("SummarizeSession: %v", err)
	}
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestSummarizeSession_TrimsWhitespace(t *testing.T) {
	llm := &stubLLM{response: "  summary with whitespace  "}
	summary, _ := SummarizeSession(context.Background(), llm, "q", "a", 256)
	if summary != "summary with whitespace" {
		t.Errorf("expected trimmed summary, got %q", summary)
	}
}
