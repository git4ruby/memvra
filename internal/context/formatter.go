package context

import (
	"fmt"
	"strings"

	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

// Formatter renders project context sections into prompt-ready strings.
type Formatter struct{}

// NewFormatter creates a Formatter.
func NewFormatter() *Formatter { return &Formatter{} }

// FormatProjectProfile renders the project profile block.
func (f *Formatter) FormatProjectProfile(proj memory.Project, ts scanner.TechStack) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Project Profile\n\n")
	fmt.Fprintf(&b, "- **Project:** %s\n", proj.Name)
	if ts.Language != "" {
		fmt.Fprintf(&b, "- **Language:** %s\n", ts.Language)
	}
	if ts.Framework != "" {
		fmt.Fprintf(&b, "- **Framework:** %s\n", ts.Framework)
	}
	if ts.Database != "" {
		fmt.Fprintf(&b, "- **Database:** %s\n", ts.Database)
	}
	if ts.Architecture != "" {
		fmt.Fprintf(&b, "- **Architecture:** %s\n", ts.Architecture)
	}
	if ts.TestFramework != "" {
		fmt.Fprintf(&b, "- **Tests:** %s\n", ts.TestFramework)
	}
	if len(ts.DetectedPatterns) > 0 {
		fmt.Fprintf(&b, "- **Patterns:** %s\n", strings.Join(ts.DetectedPatterns, ", "))
	}
	return b.String()
}

// FormatMemories renders a slice of memories as a markdown list.
func (f *Formatter) FormatMemories(memType memory.MemoryType, items []memory.Memory) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	label := strings.Title(string(memType)) + "s" //nolint:staticcheck
	fmt.Fprintf(&b, "## %s\n\n", label)
	for _, m := range items {
		fmt.Fprintf(&b, "- %s\n", m.Content)
	}
	b.WriteString("\n")
	return b.String()
}

// FormatChunk renders a single code chunk with its source location.
func (f *Formatter) FormatChunk(c memory.Chunk, filePath string) string {
	var b strings.Builder
	if filePath != "" {
		fmt.Fprintf(&b, "### %s (lines %d-%d)\n", filePath, c.StartLine, c.EndLine)
	}
	lang := chunkLang(c.ChunkType)
	fmt.Fprintf(&b, "```%s\n%s\n```\n\n", lang, c.Content)
	return b.String()
}

// FormatSystemPrompt builds the system prompt from profile + conventions + constraints.
func (f *Formatter) FormatSystemPrompt(proj memory.Project, ts scanner.TechStack, conventions, constraints []memory.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are an AI assistant working on the project %q.\n\n", proj.Name)
	b.WriteString(f.FormatProjectProfile(proj, ts))
	if len(conventions) > 0 {
		b.WriteString(f.FormatMemories(memory.TypeConvention, conventions))
	}
	if len(constraints) > 0 {
		b.WriteString(f.FormatMemories(memory.TypeConstraint, constraints))
	}
	b.WriteString("\nWhen answering:\n")
	b.WriteString("1. Respect established conventions and constraints\n")
	b.WriteString("2. Reference specific files and line numbers when relevant\n")
	b.WriteString("3. Be consistent with existing patterns in the codebase\n")
	b.WriteString("4. Flag if a suggestion contradicts stored decisions or constraints\n")
	return b.String()
}

// FormatSessionHistory renders recent sessions as a context block.
// Sessions are assumed newest-first (as returned by GetLastNSessions);
// they are reversed here to chronological order for natural reading.
func (f *Formatter) FormatSessionHistory(sessions []memory.Session) string {
	if len(sessions) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Recent Sessions\n\n")
	for i := len(sessions) - 1; i >= 0; i-- {
		s := sessions[i]
		ts := s.CreatedAt.Format("2006-01-02 15:04")
		fmt.Fprintf(&b, "**[%s]** %s\n", ts, s.Question)
		if s.ResponseSummary != "" {
			fmt.Fprintf(&b, "%s\n", s.ResponseSummary)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func chunkLang(chunkType string) string {
	switch chunkType {
	case "config":
		return "yaml"
	case "docs":
		return "markdown"
	default:
		return ""
	}
}
