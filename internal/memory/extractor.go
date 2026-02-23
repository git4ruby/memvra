package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/memvra/memvra/internal/adapter"
)

// extractCandidate is the JSON shape returned by the extraction prompt.
type extractCandidate struct {
	Content string `json:"content"`
	Type    string `json:"type"`
}

// ExtractMemories sends the LLM response to the LLM and asks it to identify
// memorable decisions, constraints, and conventions. Returns up to maxExtracts
// Memory values (without IDs — callers must persist them via Orchestrator.Remember).
func ExtractMemories(ctx context.Context, llm adapter.LLMAdapter, responseText string, maxExtracts int) ([]Memory, error) {
	if maxExtracts <= 0 {
		maxExtracts = 3
	}

	trimmed := trimResponse(responseText, 3000)

	prompt := fmt.Sprintf(`From the assistant response below, extract any decisions, constraints, or conventions that were explicitly stated or recommended. These are things the team should remember for future sessions.

Return ONLY a compact JSON array. Each element: {"content": "...", "type": "decision|constraint|convention|todo|note"}.
- decision: something chosen ("we will use X", "we switched to Y")
- constraint: a hard rule ("must", "never", "always", "only")
- convention: a style or pattern guideline
- todo: a future task or follow-up
- note: anything else worth remembering

If nothing qualifies, return []. No prose, no markdown — only the JSON array.
Maximum %d items.

--- ASSISTANT RESPONSE ---
%s
--- END ---`, maxExtracts, trimmed)

	stream, err := llm.Complete(ctx, adapter.CompletionRequest{
		UserMessage: prompt,
		MaxTokens:   512,
		Temperature: 0.1,
		Stream:      false,
	})
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		sb.WriteString(chunk.Text)
	}

	return parseExtractionJSON(sb.String(), maxExtracts)
}

// parseExtractionJSON extracts Memory values from the LLM's JSON output.
// Lenient: searches for the first '[' and last ']' to handle models that
// wrap the array in extra prose or markdown fences.
func parseExtractionJSON(raw string, max int) ([]Memory, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end <= start {
		return nil, nil // nothing extractable — not an error
	}

	var candidates []extractCandidate
	if err := json.Unmarshal([]byte(raw[start:end+1]), &candidates); err != nil {
		return nil, nil // malformed JSON — degrade gracefully
	}

	var out []Memory
	for _, c := range candidates {
		if len(out) >= max {
			break
		}
		content := strings.TrimSpace(c.Content)
		if content == "" {
			continue
		}
		mt := MemoryType(strings.ToLower(strings.TrimSpace(c.Type)))
		if !ValidMemoryType(mt) {
			mt = ClassifyMemoryType(content)
		}
		out = append(out, Memory{
			Content:    content,
			MemoryType: mt,
			Importance: defaultImportance(mt),
			Source:     "extracted",
		})
	}
	return out, nil
}

// trimResponse caps the response text at approximately maxChars characters,
// trimming at a sentence boundary if possible.
func trimResponse(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	trimmed := s[:maxChars]
	if idx := strings.LastIndexAny(trimmed, ".!?\n"); idx > maxChars/2 {
		trimmed = trimmed[:idx+1]
	}
	return trimmed + " [...]"
}
