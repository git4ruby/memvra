package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// mcpConfig represents the MCP configuration file structure used by
// Claude Code and Cursor.
type mcpConfig struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// InstallClaudeCode registers Memvra as an MCP server in Claude Code's
// global config at ~/.claude/mcp.json.
func InstallClaudeCode() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	path := filepath.Join(home, ".claude", "mcp.json")
	return installMCPConfig(path)
}

// InstallCursor registers Memvra as an MCP server in Cursor's
// project-level config at .cursor/mcp.json.
func InstallCursor(projectRoot string) error {
	path := filepath.Join(projectRoot, ".cursor", "mcp.json")
	return installMCPConfig(path)
}

// installMCPConfig reads an existing MCP config (or creates a new one),
// merges the memvra server entry, and writes it back.
func installMCPConfig(path string) error {
	cfg := mcpConfig{
		MCPServers: make(map[string]mcpServerEntry),
	}

	// Read existing config if present.
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
		if cfg.MCPServers == nil {
			cfg.MCPServers = make(map[string]mcpServerEntry)
		}
	}

	// Add/update memvra entry.
	cfg.MCPServers["memvra"] = mcpServerEntry{
		Command: "memvra",
		Args:    []string{"mcp"},
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
