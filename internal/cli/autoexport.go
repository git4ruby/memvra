package cli

import (
	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/export"
	"github.com/memvra/memvra/internal/memory"
)

// autoExportFilenames returns the filenames that auto-export would generate
// for the given config.
func autoExportFilenames(cfg config.AutoExportConfig) []string {
	var names []string
	for _, f := range cfg.Formats {
		if name := export.FormatToFilename(f); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// AutoExport regenerates all configured export files in the project root.
// Delegates to export.AutoExport.
func AutoExport(root string, store *memory.Store) {
	export.AutoExport(root, store)
}
