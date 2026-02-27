package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	// Register sqlite-vec as an auto-extension so every SQLite connection
	// opened by this process has the vec0 virtual table module available.
	vec.Auto()
}

const (
	// DefaultEmbeddingDimension is used when creating vec0 virtual tables.
	// nomic-embed-text produces 768-dim vectors; text-embedding-3-small produces 1536.
	// We default to 768 to match nomic-embed-text (the default Ollama embed model).
	DefaultEmbeddingDimension = 768
)

// DB wraps a *sql.DB and exposes helpers.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", absPath)
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Single writer, multiple readers.
	conn.SetMaxOpenConns(1)

	if err := applyMigrations(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	if err := applyVectorTables(conn, DefaultEmbeddingDimension); err != nil {
		// Non-fatal: sqlite-vec may not be available in all build configurations.
		// Vector search will degrade gracefully to keyword/type-based retrieval.
		_ = err
	}

	return &DB{conn: conn}, nil
}

// Conn returns the underlying *sql.DB for use by store/vector layers.
func (d *DB) Conn() *sql.DB {
	return d.conn
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// Ping checks the connection is live.
func (d *DB) Ping() error {
	return d.conn.Ping()
}
