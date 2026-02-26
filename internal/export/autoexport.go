package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/memvra/memvra/internal/config"
	gitpkg "github.com/memvra/memvra/internal/git"
	"github.com/memvra/memvra/internal/memory"
	"github.com/memvra/memvra/internal/scanner"
)

// FormatToFilename maps an export format name to the file it should be written to.
func FormatToFilename(format string) string {
	switch format {
	case "claude":
		return "CLAUDE.md"
	case "cursor":
		return ".cursorrules"
	case "markdown":
		return "PROJECT_CONTEXT.md"
	case "json":
		return "memvra-context.json"
	default:
		return ""
	}
}

// AutoExport regenerates all configured export files in the project root.
// It is best-effort: failures are logged to stderr but never abort the caller.
func AutoExport(root string, store *memory.Store) {
	gcfg, _ := config.Load(root)
	if !gcfg.AutoExport.Enabled || len(gcfg.AutoExport.Formats) == 0 {
		return
	}

	proj, err := store.GetProject()
	if err != nil {
		return
	}
	ts, _ := scanner.TechStackFromJSON(proj.TechStack)

	memories, err := store.ListMemories("")
	if err != nil {
		return
	}

	sessions, _ := store.GetLastNSessions(5)
	gitState := gitpkg.CaptureWorkingState(root)

	data := ExportData{
		Project:  proj,
		Stack:    ts,
		Memories: memories,
		Sessions: sessions,
		GitState: gitState,
	}

	var exported []string
	for _, format := range gcfg.AutoExport.Formats {
		exporter, ok := Get(format)
		if !ok {
			continue
		}
		output, err := exporter.Export(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warn: auto-export %s failed: %v\n", format, err)
			continue
		}

		filename := FormatToFilename(format)
		if filename == "" {
			continue
		}
		outPath := filepath.Join(root, filename)
		if err := os.WriteFile(outPath, []byte(output), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  warn: write %s failed: %v\n", filename, err)
			continue
		}
		exported = append(exported, filename)
	}

	if len(exported) > 0 {
		fmt.Fprintf(os.Stderr, "  auto-exported: %s\n", strings.Join(exported, ", "))
	}
}
