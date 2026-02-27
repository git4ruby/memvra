// Package config manages global (~/.config/memvra/config.toml) and
// per-project (.memvra/config.toml) configuration for Memvra.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// GlobalConfig holds user-wide settings.
type GlobalConfig struct {
	DefaultModel    string              `toml:"default_model"`
	DefaultEmbedder string              `toml:"default_embedder"`
	Keys            KeysConfig          `toml:"keys"`
	Ollama          OllamaConfig        `toml:"ollama"`
	Context         ContextConfig       `toml:"context"`
	Output          OutputConfig        `toml:"output"`
	Extraction      ExtractionConfig    `toml:"extraction"`
	Summarization   SummarizationConfig `toml:"summarization"`
	AutoExport      AutoExportConfig    `toml:"auto_export"`
}

// AutoExportConfig controls automatic regeneration of export files
// (CLAUDE.md, .cursorrules, etc.) whenever memories change.
type AutoExportConfig struct {
	Enabled bool     `toml:"enabled"`
	Formats []string `toml:"formats"`
}

// ExtractionConfig controls auto-extraction of memories from LLM responses.
type ExtractionConfig struct {
	Enabled     bool `toml:"enabled"`
	MaxExtracts int  `toml:"max_extracts"`
}

// SummarizationConfig controls auto-summarization of session responses.
type SummarizationConfig struct {
	Enabled   bool `toml:"enabled"`
	MaxTokens int  `toml:"max_tokens"`
}

type KeysConfig struct {
	Anthropic string `toml:"anthropic"`
	OpenAI    string `toml:"openai"`
	Gemini    string `toml:"gemini"`
}

type OllamaConfig struct {
	Host            string `toml:"host"`
	EmbedModel      string `toml:"embed_model"`
	CompletionModel string `toml:"completion_model"`
}

type ContextConfig struct {
	MaxTokens          int     `toml:"max_tokens"`
	ChunkMaxLines      int     `toml:"chunk_max_lines"`
	SimilarityThreshold float64 `toml:"similarity_threshold"`
	TopKChunks         int     `toml:"top_k_chunks"`
	TopKMemories       int     `toml:"top_k_memories"`
	TopKSessions       int     `toml:"top_k_sessions"`
	SessionTokenBudget int     `toml:"session_token_budget"`
}

type OutputConfig struct {
	Stream  bool `toml:"stream"`
	Color   bool `toml:"color"`
	Verbose bool `toml:"verbose"`
}

// ProjectConfig holds per-project overrides stored in .memvra/config.toml.
type ProjectConfig struct {
	DefaultModel  string            `toml:"default_model"`
	Project       ProjectMeta       `toml:"project"`
	Conventions   map[string]string `toml:"conventions"`
	AlwaysInclude []string          `toml:"always_include"`
	Exclude       []string          `toml:"exclude"`
}

type ProjectMeta struct {
	Name string `toml:"name"`
}

// DefaultGlobal returns sensible defaults.
func DefaultGlobal() GlobalConfig {
	return GlobalConfig{
		DefaultModel:    "claude",
		DefaultEmbedder: "ollama",
		Ollama: OllamaConfig{
			Host:            "http://localhost:11434",
			EmbedModel:      "nomic-embed-text",
			CompletionModel: "llama3.2",
		},
		Context: ContextConfig{
			MaxTokens:           8000,
			ChunkMaxLines:       150,
			SimilarityThreshold: 0.3,
			TopKChunks:          10,
			TopKMemories:        5,
			TopKSessions:        3,
			SessionTokenBudget:  500,
		},
		Output: OutputConfig{
			Stream: true,
			Color:  true,
		},
		Extraction: ExtractionConfig{
			Enabled:     false,
			MaxExtracts: 3,
		},
		Summarization: SummarizationConfig{
			Enabled:   true,
			MaxTokens: 256,
		},
		AutoExport: AutoExportConfig{
			Enabled: true,
			Formats: []string{"claude", "cursor", "markdown", "json"},
		},
	}
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "memvra", "config.toml"), nil
}

// LoadGlobal loads the global config, applying defaults for any missing values.
func LoadGlobal() (GlobalConfig, error) {
	cfg := DefaultGlobal()

	path, err := GlobalConfigPath()
	if err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			if _, decodeErr := toml.DecodeFile(path, &cfg); decodeErr != nil {
				return cfg, fmt.Errorf("config: load global: %w", decodeErr)
			}
		}
	}

	// Let env vars override config file API keys (always applied).
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.Keys.Anthropic = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.Keys.OpenAI = v
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.Keys.Gemini = v
	}

	return cfg, nil
}

// SaveGlobal writes the global config to disk.
func SaveGlobal(cfg GlobalConfig) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("config: create global config: %w", err)
	}
	defer func() { _ = f.Close() }()

	return toml.NewEncoder(f).Encode(cfg)
}

// LoadProject loads .memvra/config.toml from the given project root.
func LoadProject(root string) (ProjectConfig, error) {
	var cfg ProjectConfig
	path := filepath.Join(root, ".memvra", "config.toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("config: load project: %w", err)
	}
	return cfg, nil
}

// ProjectDBPath returns the path to the project's SQLite database.
func ProjectDBPath(root string) string {
	return filepath.Join(root, ".memvra", "memvra.db")
}

// ProjectConfigDirPath returns the path to the project's .memvra/ directory.
func ProjectConfigDirPath(root string) string {
	return filepath.Join(root, ".memvra")
}

// Load returns the effective config for a project root (global merged with project).
// It is a convenience wrapper used by CLI commands.
func Load(root string) (GlobalConfig, error) {
	global, err := LoadGlobal()
	if err != nil {
		global = DefaultGlobal()
	}

	project, err := LoadProject(root)
	if err == nil {
		// Apply project overrides.
		if project.DefaultModel != "" {
			global.DefaultModel = project.DefaultModel
		}
		for k, v := range project.Conventions {
			_ = k
			_ = v
			// Conventions are accessible via project config directly.
		}
	}

	return global, nil
}

// SaveProject writes the project config to .memvra/config.toml.
func SaveProject(root string, cfg ProjectConfig) error {
	dir := filepath.Join(root, ".memvra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir project: %w", err)
	}

	path := filepath.Join(dir, "config.toml")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("config: create project config: %w", err)
	}
	defer func() { _ = f.Close() }()

	return toml.NewEncoder(f).Encode(cfg)
}
