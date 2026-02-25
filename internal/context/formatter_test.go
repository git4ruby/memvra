package context

import (
	"strings"
	"testing"
	"time"

	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

func TestFormatProjectProfile(t *testing.T) {
	f := NewFormatter()
	proj := memory.Project{Name: "myapp"}
	ts := scanner.TechStack{
		Language:         "Go",
		Framework:        "Gin",
		Database:         "PostgreSQL",
		Architecture:     "API + SPA",
		TestFramework:    "testing",
		DetectedPatterns: []string{"background-jobs"},
	}

	result := f.FormatProjectProfile(proj, ts)
	checks := []string{
		"## Project Profile",
		"myapp",
		"Go",
		"Gin",
		"PostgreSQL",
		"API + SPA",
		"testing",
		"background-jobs",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("missing %q in profile", check)
		}
	}
}

func TestFormatProjectProfile_MinimalStack(t *testing.T) {
	f := NewFormatter()
	proj := memory.Project{Name: "bare"}
	ts := scanner.TechStack{} // Everything empty.

	result := f.FormatProjectProfile(proj, ts)
	if !strings.Contains(result, "bare") {
		t.Error("missing project name")
	}
	// Should not contain empty field labels.
	if strings.Contains(result, "**Language:**") {
		t.Error("should not show Language when empty")
	}
}

func TestFormatMemories(t *testing.T) {
	f := NewFormatter()
	items := []memory.Memory{
		{Content: "Use React"},
		{Content: "Use TypeScript"},
	}

	result := f.FormatMemories(memory.TypeConvention, items)
	if !strings.Contains(result, "## Conventions") {
		t.Error("missing heading")
	}
	if !strings.Contains(result, "- Use React") {
		t.Error("missing first item")
	}
	if !strings.Contains(result, "- Use TypeScript") {
		t.Error("missing second item")
	}
}

func TestFormatMemories_Empty(t *testing.T) {
	f := NewFormatter()
	result := f.FormatMemories(memory.TypeDecision, nil)
	if result != "" {
		t.Errorf("expected empty string for no items, got %q", result)
	}
}

func TestFormatChunk(t *testing.T) {
	f := NewFormatter()
	c := memory.Chunk{
		Content:   "package main\n\nfunc main() {}",
		StartLine: 1,
		EndLine:   3,
		ChunkType: "code",
	}

	result := f.FormatChunk(c, "main.go")
	if !strings.Contains(result, "### main.go (lines 1-3)") {
		t.Error("missing file header")
	}
	if !strings.Contains(result, "```\n") {
		t.Error("missing code fence")
	}
	if !strings.Contains(result, "package main") {
		t.Error("missing code content")
	}
}

func TestFormatChunk_NoFilePath(t *testing.T) {
	f := NewFormatter()
	c := memory.Chunk{Content: "some code", ChunkType: "code"}

	result := f.FormatChunk(c, "")
	if strings.Contains(result, "###") {
		t.Error("should not include header when no file path")
	}
	if !strings.Contains(result, "some code") {
		t.Error("missing content")
	}
}

func TestFormatChunk_ConfigType(t *testing.T) {
	f := NewFormatter()
	c := memory.Chunk{Content: "key: value", ChunkType: "config"}

	result := f.FormatChunk(c, "config.yaml")
	if !strings.Contains(result, "```yaml") {
		t.Error("config chunks should use yaml language tag")
	}
}

func TestFormatChunk_DocsType(t *testing.T) {
	f := NewFormatter()
	c := memory.Chunk{Content: "# Hello", ChunkType: "docs"}

	result := f.FormatChunk(c, "README.md")
	if !strings.Contains(result, "```markdown") {
		t.Error("docs chunks should use markdown language tag")
	}
}

func TestFormatSystemPrompt(t *testing.T) {
	f := NewFormatter()
	proj := memory.Project{Name: "myapp"}
	ts := scanner.TechStack{Language: "Go"}
	conventions := []memory.Memory{{Content: "Use camelCase"}}
	constraints := []memory.Memory{{Content: "Never commit secrets"}}

	result := f.FormatSystemPrompt(proj, ts, conventions, constraints)
	checks := []string{
		`"myapp"`,
		"Project Profile",
		"Use camelCase",
		"Never commit secrets",
		"Respect established conventions",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("system prompt missing %q", check)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	short := "hello"
	if truncateStr(short, 10) != short {
		t.Error("should not truncate short strings")
	}

	long := "this is a long string"
	truncated := truncateStr(long, 10)
	if len(truncated) > 13 { // 10 + "..."
		t.Errorf("truncated too long: %q", truncated)
	}
	if !strings.HasSuffix(truncated, "...") {
		t.Error("should end with ...")
	}
}

func TestFormatSessionHistory_WithSummary(t *testing.T) {
	f := NewFormatter()
	sessions := []memory.Session{
		{
			Question:        "How do I deploy?",
			ResponseSummary: "Use docker compose up.",
			CreatedAt:       time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC),
		},
	}
	result := f.FormatSessionHistory(sessions)
	if !strings.Contains(result, "## Recent Sessions") {
		t.Error("missing heading")
	}
	if !strings.Contains(result, "How do I deploy?") {
		t.Error("missing question")
	}
	if !strings.Contains(result, "Use docker compose up.") {
		t.Error("missing summary")
	}
}

func TestFormatSessionHistory_Empty(t *testing.T) {
	f := NewFormatter()
	result := f.FormatSessionHistory(nil)
	if result != "" {
		t.Errorf("expected empty string for no sessions, got %q", result)
	}
}
