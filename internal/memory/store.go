package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/memvra/memvra/internal/db"
)

// Store provides read/write access to the Memvra SQLite database.
type Store struct {
	db *db.DB
}

// NewStore creates a Store backed by the given DB.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// Conn exposes the underlying *sql.DB for low-level queries.
func (s *Store) Conn() *sql.DB {
	return s.db.Conn()
}

// ---- Project ----

// UpsertProject inserts or replaces the project record.
func (s *Store) UpsertProject(p Project) error {
	_, err := s.db.Conn().Exec(`
		INSERT INTO project (id, name, root_path, tech_stack, architecture, conventions, file_count, chunk_count, updated_at)
		VALUES (COALESCE((SELECT id FROM project LIMIT 1), lower(hex(randomblob(16)))),
		        ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
		    name         = excluded.name,
		    root_path    = excluded.root_path,
		    tech_stack   = excluded.tech_stack,
		    architecture = excluded.architecture,
		    conventions  = excluded.conventions,
		    file_count   = excluded.file_count,
		    chunk_count  = excluded.chunk_count,
		    updated_at   = CURRENT_TIMESTAMP`,
		p.Name, p.RootPath, p.TechStack, p.Architecture, p.Conventions,
		p.FileCount, p.ChunkCount,
	)
	return err
}

// GetProject returns the single project record, or an error if not found.
func (s *Store) GetProject() (Project, error) {
	var p Project
	row := s.db.Conn().QueryRow(`SELECT id, name, root_path, tech_stack, COALESCE(architecture,''), COALESCE(conventions,''), file_count, chunk_count, created_at, updated_at FROM project LIMIT 1`)
	var createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.RootPath, &p.TechStack, &p.Architecture, &p.Conventions,
		&p.FileCount, &p.ChunkCount, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return p, fmt.Errorf("store: project not initialised — run `memvra init` first")
	}
	if err != nil {
		return p, fmt.Errorf("store: get project: %w", err)
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return p, nil
}

// ---- Files ----

// UpsertFile inserts or updates a file record. Returns the file ID.
func (s *Store) UpsertFile(f File) (string, error) {
	var id string
	err := s.db.Conn().QueryRow(`
		INSERT INTO files (id, path, language, last_modified, content_hash)
		VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
		    language      = excluded.language,
		    last_modified = excluded.last_modified,
		    content_hash  = excluded.content_hash,
		    indexed_at    = CURRENT_TIMESTAMP
		RETURNING id`,
		f.Path, f.Language, f.LastModified.UTC(), f.ContentHash,
	).Scan(&id)
	return id, err
}

// GetFileByPath returns the file record for the given relative path.
func (s *Store) GetFileByPath(path string) (File, error) {
	var f File
	var lastMod, indexedAt string
	err := s.db.Conn().QueryRow(
		`SELECT id, path, language, last_modified, content_hash, indexed_at FROM files WHERE path = ?`, path,
	).Scan(&f.ID, &f.Path, &f.Language, &lastMod, &f.ContentHash, &indexedAt)
	if err == sql.ErrNoRows {
		return f, sql.ErrNoRows
	}
	return f, err
}

// ---- Chunks ----

// InsertChunk stores a new chunk. fileID must be a valid files.id.
func (s *Store) InsertChunk(c Chunk) error {
	_, err := s.db.Conn().Exec(`
		INSERT INTO chunks (id, file_id, content, start_line, end_line, chunk_type)
		VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?)`,
		c.FileID, c.Content, c.StartLine, c.EndLine, c.ChunkType,
	)
	return err
}

// InsertChunkReturningID inserts a chunk and returns its generated ID.
func (s *Store) InsertChunkReturningID(c Chunk) (string, error) {
	var id string
	err := s.db.Conn().QueryRow(`
		INSERT INTO chunks (id, file_id, content, start_line, end_line, chunk_type)
		VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?)
		RETURNING id`,
		c.FileID, c.Content, c.StartLine, c.EndLine, c.ChunkType,
	).Scan(&id)
	return id, err
}

// ListAllChunks returns every chunk in the database (used for bulk embedding).
func (s *Store) ListAllChunks() ([]Chunk, error) {
	rows, err := s.db.Conn().Query(
		`SELECT id, file_id, content, start_line, end_line, COALESCE(chunk_type,'code') FROM chunks`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list all chunks: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.FileID, &c.Content, &c.StartLine, &c.EndLine, &c.ChunkType); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// DeleteChunksByFileID removes all chunks for a given file (used on re-index).
func (s *Store) DeleteChunksByFileID(fileID string) error {
	_, err := s.db.Conn().Exec(`DELETE FROM chunks WHERE file_id = ?`, fileID)
	return err
}

// CountChunks returns the total number of stored chunks.
func (s *Store) CountChunks() (int, error) {
	var n int
	err := s.db.Conn().QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&n)
	return n, err
}

// CountFiles returns the total number of indexed files.
func (s *Store) CountFiles() (int, error) {
	var n int
	err := s.db.Conn().QueryRow(`SELECT COUNT(*) FROM files`).Scan(&n)
	return n, err
}

// ---- Memories ----

// InsertMemory persists a new memory and returns its generated ID.
func (s *Store) InsertMemory(m Memory) (string, error) {
	relatedJSON := "[]"
	if len(m.RelatedFiles) > 0 {
		b, _ := json.Marshal(m.RelatedFiles)
		relatedJSON = string(b)
	}
	source := m.Source
	if source == "" {
		source = "user"
	}

	var id string
	err := s.db.Conn().QueryRow(`
		INSERT INTO memories (id, content, memory_type, importance, source, related_files)
		VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?)
		RETURNING id`,
		m.Content, string(m.MemoryType), m.Importance, source, relatedJSON,
	).Scan(&id)
	return id, err
}

// DeleteMemory removes a memory by ID.
func (s *Store) DeleteMemory(id string) error {
	res, err := s.db.Conn().Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: memory %q not found", id)
	}
	return nil
}

// DeleteMemoriesByType removes all memories of a given type.
func (s *Store) DeleteMemoriesByType(t MemoryType) (int, error) {
	res, err := s.db.Conn().Exec(`DELETE FROM memories WHERE memory_type = ?`, string(t))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteAllMemories removes every memory record.
func (s *Store) DeleteAllMemories() (int, error) {
	res, err := s.db.Conn().Exec(`DELETE FROM memories`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListMemories returns all memories, optionally filtered by type.
// Pass empty string to get all types.
func (s *Store) ListMemories(filterType MemoryType) ([]Memory, error) {
	var rows *sql.Rows
	var err error

	if filterType == "" {
		rows, err = s.db.Conn().Query(
			`SELECT id, content, memory_type, importance, source, related_files, created_at, updated_at FROM memories ORDER BY importance DESC, created_at DESC`,
		)
	} else {
		rows, err = s.db.Conn().Query(
			`SELECT id, content, memory_type, importance, source, related_files, created_at, updated_at FROM memories WHERE memory_type = ? ORDER BY importance DESC, created_at DESC`,
			string(filterType),
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMemories(rows)
}

// CountMemoriesByType returns a count per memory type.
func (s *Store) CountMemoriesByType() (map[MemoryType]int, error) {
	rows, err := s.db.Conn().Query(
		`SELECT memory_type, COUNT(*) FROM memories GROUP BY memory_type`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[MemoryType]int)
	for rows.Next() {
		var t string
		var n int
		if err := rows.Scan(&t, &n); err != nil {
			return nil, err
		}
		counts[MemoryType(t)] = n
	}
	return counts, rows.Err()
}

// CountSessions returns the total number of recorded sessions.
func (s *Store) CountSessions() (int, error) {
	var n int
	err := s.db.Conn().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	return n, err
}

// InsertSession records a completed ask session.
func (s *Store) InsertSession(sess Session) error {
	_, err := s.db.Conn().Exec(`
		INSERT INTO sessions (id, question, context_used, response_summary, model_used, tokens_used)
		VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?)`,
		sess.Question, sess.ContextUsed, sess.ResponseSummary, sess.ModelUsed, sess.TokensUsed,
	)
	return err
}

// InsertSessionReturningID records a completed ask session and returns its generated ID.
func (s *Store) InsertSessionReturningID(sess Session) (string, error) {
	var id string
	err := s.db.Conn().QueryRow(`
		INSERT INTO sessions (id, question, context_used, response_summary, model_used, tokens_used)
		VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?)
		RETURNING id`,
		sess.Question, sess.ContextUsed, sess.ResponseSummary, sess.ModelUsed, sess.TokensUsed,
	).Scan(&id)
	return id, err
}

// UpdateSessionSummary replaces the response_summary for an existing session.
func (s *Store) UpdateSessionSummary(id, summary string) error {
	_, err := s.db.Conn().Exec(
		`UPDATE sessions SET response_summary = ? WHERE id = ?`,
		summary, id,
	)
	return err
}

// PruneSessions deletes sessions older than the given number of days.
// Returns the number of deleted rows.
func (s *Store) PruneSessions(olderThanDays int) (int, error) {
	res, err := s.db.Conn().Exec(
		`DELETE FROM sessions WHERE created_at < datetime('now', '-' || ? || ' days')`,
		olderThanDays,
	)
	if err != nil {
		return 0, fmt.Errorf("store: prune sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PruneSessionsKeepLatest deletes all but the latest N sessions.
// Returns the number of deleted rows.
func (s *Store) PruneSessionsKeepLatest(keep int) (int, error) {
	res, err := s.db.Conn().Exec(`
		DELETE FROM sessions WHERE id NOT IN (
			SELECT id FROM sessions ORDER BY created_at DESC LIMIT ?
		)`, keep,
	)
	if err != nil {
		return 0, fmt.Errorf("store: prune sessions keep latest: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GetLastNSessions returns the N most recent sessions, ordered newest first.
func (s *Store) GetLastNSessions(n int) ([]Session, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := s.db.Conn().Query(`
		SELECT id, question, context_used, response_summary, model_used, tokens_used, created_at
		FROM sessions
		ORDER BY created_at DESC
		LIMIT ?`, n,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get last n sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var sess Session
		var createdAt string
		if err := rows.Scan(
			&sess.ID, &sess.Question, &sess.ContextUsed,
			&sess.ResponseSummary, &sess.ModelUsed, &sess.TokensUsed,
			&createdAt,
		); err != nil {
			return nil, err
		}
		sess.CreatedAt = parseTime(createdAt)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// ---- Helpers ----

// parseTime tries multiple SQLite timestamp layouts.
// go-sqlite3 may return RFC3339 or the plain "2006-01-02 15:04:05" format depending on
// the connection string and platform.
func parseTime(s string) time.Time {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func scanMemories(rows *sql.Rows) ([]Memory, error) {
	var out []Memory
	for rows.Next() {
		var m Memory
		var mt, createdAt, updatedAt, relatedFiles string
		if err := rows.Scan(&m.ID, &m.Content, &mt, &m.Importance, &m.Source, &relatedFiles, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		m.MemoryType = MemoryType(mt)
		m.CreatedAt = parseTime(createdAt)
		m.UpdatedAt = parseTime(updatedAt)
		if relatedFiles != "" && relatedFiles != "[]" {
			_ = json.Unmarshal([]byte(relatedFiles), &m.RelatedFiles)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetChunkByID returns a single chunk by its ID.
func (s *Store) GetChunkByID(id string) (Chunk, error) {
	var c Chunk
	var createdAt string
	err := s.db.Conn().QueryRow(
		`SELECT id, file_id, content, start_line, end_line, chunk_type, created_at FROM chunks WHERE id = ?`, id,
	).Scan(&c.ID, &c.FileID, &c.Content, &c.StartLine, &c.EndLine, &c.ChunkType, &createdAt)
	if err == sql.ErrNoRows {
		return c, fmt.Errorf("store: chunk %q not found", id)
	}
	return c, err
}

// GetMemoryByID returns a single memory by its ID.
func (s *Store) GetMemoryByID(id string) (Memory, error) {
	var m Memory
	var mt, createdAt, updatedAt, relatedFiles string
	err := s.db.Conn().QueryRow(
		`SELECT id, content, memory_type, importance, source, related_files, created_at, updated_at FROM memories WHERE id = ?`, id,
	).Scan(&m.ID, &m.Content, &mt, &m.Importance, &m.Source, &relatedFiles, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return m, fmt.Errorf("store: memory %q not found", id)
	}
	if err != nil {
		return m, err
	}
	m.MemoryType = MemoryType(mt)
	m.CreatedAt = parseTime(createdAt)
	m.UpdatedAt = parseTime(updatedAt)
	if relatedFiles != "" && relatedFiles != "[]" {
		_ = json.Unmarshal([]byte(relatedFiles), &m.RelatedFiles)
	}
	return m, nil
}

// ListFiles returns every indexed file.
func (s *Store) ListFiles() ([]File, error) {
	rows, err := s.db.Conn().Query(
		`SELECT id, path, language, last_modified, content_hash, indexed_at FROM files ORDER BY path`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var lastMod, indexedAt string
		if err := rows.Scan(&f.ID, &f.Path, &f.Language, &lastMod, &f.ContentHash, &indexedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ListChunksByFileID returns all chunks belonging to a file.
func (s *Store) ListChunksByFileID(fileID string) ([]Chunk, error) {
	rows, err := s.db.Conn().Query(
		`SELECT id, file_id, content, start_line, end_line, COALESCE(chunk_type,'code') FROM chunks WHERE file_id = ?`,
		fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list chunks by file: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.FileID, &c.Content, &c.StartLine, &c.EndLine, &c.ChunkType); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// DeleteFile removes a file record. Chunks are cascade-deleted by SQLite.
func (s *Store) DeleteFile(id string) error {
	_, err := s.db.Conn().Exec(`DELETE FROM files WHERE id = ?`, id)
	return err
}

// GetFileByID returns a single file record by its ID.
func (s *Store) GetFileByID(id string) (File, error) {
	var f File
	var lastMod, indexedAt string
	err := s.db.Conn().QueryRow(
		`SELECT id, path, language, last_modified, content_hash, indexed_at FROM files WHERE id = ?`, id,
	).Scan(&f.ID, &f.Path, &f.Language, &lastMod, &f.ContentHash, &indexedAt)
	if err == sql.ErrNoRows {
		return f, fmt.Errorf("store: file %q not found", id)
	}
	return f, err
}
