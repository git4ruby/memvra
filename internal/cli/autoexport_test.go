package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/export"
	"github.com/memvra/memvra/internal/memory"
)

func TestFormatToFilename(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"claude", "CLAUDE.md"},
		{"cursor", ".cursorrules"},
		{"markdown", "PROJECT_CONTEXT.md"},
		{"json", "memvra-context.json"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := export.FormatToFilename(tt.format)
			if got != tt.want {
				t.Errorf("FormatToFilename(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestAutoExportFilenames(t *testing.T) {
	cfg := config.AutoExportConfig{
		Enabled: true,
		Formats: []string{"claude", "cursor", "markdown", "json"},
	}
	names := autoExportFilenames(cfg)
	if len(names) != 4 {
		t.Fatalf("expected 4 filenames, got %d", len(names))
	}
	expected := []string{"CLAUDE.md", ".cursorrules", "PROJECT_CONTEXT.md", "memvra-context.json"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("index %d: got %q, want %q", i, names[i], want)
		}
	}
}

func setupAutoExportTestDB(t *testing.T) (string, *memory.Store) {
	t.Helper()
	root := t.TempDir()

	// Create .memvra directory and DB.
	dbDir := filepath.Join(root, ".memvra")
	os.MkdirAll(dbDir, 0o755)

	database, err := db.Open(filepath.Join(dbDir, "memvra.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	store := memory.NewStore(database)

	// Seed a project.
	store.UpsertProject(memory.Project{
		Name:      "testproject",
		RootPath:  root,
		TechStack: `{"language":"Go","framework":"net/http"}`,
	})

	// Write a project config that enables auto-export.
	pcfg := config.ProjectConfig{
		Project: config.ProjectMeta{Name: "testproject"},
	}
	config.SaveProject(root, pcfg)

	return root, store
}

func TestAutoExport_WritesAllFiles(t *testing.T) {
	root, store := setupAutoExportTestDB(t)

	// Add a memory so export has content.
	store.InsertMemory(memory.Memory{
		Content:    "use PostgreSQL for JSONB support",
		MemoryType: memory.TypeDecision,
		Importance: 0.8,
		Source:     "user",
	})

	AutoExport(root, store)

	// All 4 files should exist.
	files := map[string]string{
		"CLAUDE.md":           "PostgreSQL",
		".cursorrules":        "PostgreSQL",
		"PROJECT_CONTEXT.md":  "PostgreSQL",
		"memvra-context.json": "PostgreSQL",
	}
	for filename, mustContain := range files {
		path := filepath.Join(root, filename)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: expected file to exist, got error: %v", filename, err)
			continue
		}
		if !strings.Contains(string(content), mustContain) {
			t.Errorf("%s: expected to contain %q, got:\n%s", filename, mustContain, string(content))
		}
	}
}

func TestAutoExport_IncludesProjectName(t *testing.T) {
	root, store := setupAutoExportTestDB(t)

	AutoExport(root, store)

	// CLAUDE.md should include project name.
	content, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("expected CLAUDE.md, got error: %v", err)
	}
	if !strings.Contains(string(content), "testproject") {
		t.Error("CLAUDE.md should contain project name")
	}
}

func TestAutoExport_NoProject(t *testing.T) {
	// AutoExport should not panic when the store has no project.
	emptyRoot := t.TempDir()
	dbDir := filepath.Join(emptyRoot, ".memvra")
	os.MkdirAll(dbDir, 0o755)

	database, err := db.Open(filepath.Join(dbDir, "memvra.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	store := memory.NewStore(database)

	// Should not panic — just bail silently because GetProject fails.
	AutoExport(emptyRoot, store)

	// No CLAUDE.md should be written.
	if _, err := os.Stat(filepath.Join(emptyRoot, "CLAUDE.md")); err == nil {
		t.Error("expected no CLAUDE.md for uninitialized project")
	}
}

func TestAutoExport_MultipleMemoryTypes(t *testing.T) {
	root, store := setupAutoExportTestDB(t)

	store.InsertMemory(memory.Memory{Content: "use PostgreSQL", MemoryType: memory.TypeDecision, Importance: 0.8})
	store.InsertMemory(memory.Memory{Content: "use camelCase", MemoryType: memory.TypeConvention, Importance: 0.7})
	store.InsertMemory(memory.Memory{Content: "never expose keys", MemoryType: memory.TypeConstraint, Importance: 0.8})
	store.InsertMemory(memory.Memory{Content: "refactor auth", MemoryType: memory.TypeTodo, Importance: 0.6})
	store.InsertMemory(memory.Memory{Content: "API uses REST", MemoryType: memory.TypeNote, Importance: 0.5})

	AutoExport(root, store)

	// CLAUDE.md should contain all memory types.
	content, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("expected CLAUDE.md: %v", err)
	}
	text := string(content)
	for _, want := range []string{"PostgreSQL", "camelCase", "never expose keys", "refactor auth", "API uses REST"} {
		if !strings.Contains(text, want) {
			t.Errorf("CLAUDE.md should contain %q", want)
		}
	}
}

func TestAutoExport_IncludesSessions(t *testing.T) {
	root, store := setupAutoExportTestDB(t)

	// Insert a session.
	store.InsertSession(memory.Session{
		Question:        "How should I implement auth?",
		ResponseSummary: "Use JWT with middleware pattern",
		ModelUsed:       "claude",
	})

	AutoExport(root, store)

	content, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("expected CLAUDE.md: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "Recent Activity") {
		t.Error("CLAUDE.md should contain Recent Activity section")
	}
	if !strings.Contains(text, "implement auth") {
		t.Error("CLAUDE.md should contain session question")
	}
	if !strings.Contains(text, "JWT") {
		t.Error("CLAUDE.md should contain session summary")
	}
	if !strings.Contains(text, "claude") {
		t.Error("CLAUDE.md should contain model name")
	}
}

func TestAutoExport_UpdatesOnRerun(t *testing.T) {
	root, store := setupAutoExportTestDB(t)

	// First export with one memory.
	store.InsertMemory(memory.Memory{Content: "use PostgreSQL", MemoryType: memory.TypeDecision, Importance: 0.8})
	AutoExport(root, store)

	// Add another memory and re-export.
	store.InsertMemory(memory.Memory{Content: "switched to Redis", MemoryType: memory.TypeDecision, Importance: 0.8})
	AutoExport(root, store)

	// Both should be in the file.
	content, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	text := string(content)
	if !strings.Contains(text, "PostgreSQL") {
		t.Error("should still contain first memory")
	}
	if !strings.Contains(text, "Redis") {
		t.Error("should contain newly added memory")
	}
}
