package tui

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type SessionRecord struct {
	ID           string
	Title        string
	Status       string // "active" / "archived"
	AgentName    string
	ProviderName string
	ModelID      string
	Metadata     string // JSON string
	MessageCount int
	LastMessage  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SessionStore struct {
	db      *sql.DB
	baseDir string
}

const createTableSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    agent_name    TEXT NOT NULL DEFAULT '',
    provider_name TEXT NOT NULL DEFAULT '',
    model_id      TEXT NOT NULL DEFAULT '',
    metadata      TEXT,
    message_count INTEGER NOT NULL DEFAULT 0,
    last_message  TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
`

func NewSessionStore(dbPath, baseDir string) (*SessionStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	if _, err := db.Exec(createTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	return &SessionStore{db: db, baseDir: baseDir}, nil
}

func (s *SessionStore) Create(rec *SessionRecord) error {
	if rec.ID == "" {
		rec.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	if rec.Status == "" {
		rec.Status = "active"
	}

	sessionDir := filepath.Join(s.baseDir, rec.ID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	_, err := s.db.Exec(
		`INSERT INTO sessions (id, title, status, agent_name, provider_name, model_id, metadata, message_count, last_message, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Title, rec.Status, rec.AgentName, rec.ProviderName,
		rec.ModelID, rec.Metadata, rec.MessageCount, rec.LastMessage,
		rec.CreatedAt.Format(sqliteTimeFormat), rec.UpdatedAt.Format(sqliteTimeFormat),
	)
	if err != nil {
		os.RemoveAll(sessionDir)
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *SessionStore) Get(id string) (*SessionRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, title, status, agent_name, provider_name, model_id, metadata, message_count, last_message, created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)
	return scanSessionRecord(row)
}

func (s *SessionStore) List() ([]*SessionRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, title, status, agent_name, provider_name, model_id, metadata, message_count, last_message, created_at, updated_at
		 FROM sessions ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var records []*SessionRecord
	for rows.Next() {
		rec, err := scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

func (s *SessionStore) Update(rec *SessionRecord) error {
	rec.UpdatedAt = time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE sessions SET title=?, status=?, agent_name=?, provider_name=?, model_id=?, metadata=?, message_count=?, last_message=?, updated_at=?
		 WHERE id=?`,
		rec.Title, rec.Status, rec.AgentName, rec.ProviderName,
		rec.ModelID, rec.Metadata, rec.MessageCount, rec.LastMessage,
		rec.UpdatedAt.Format(sqliteTimeFormat), rec.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func (s *SessionStore) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session row: %w", err)
	}
	sessionDir := filepath.Join(s.baseDir, id)
	if err := os.RemoveAll(sessionDir); err != nil {
		// SQL 行已删除但目录残留，记录后继续（孤儿目录比孤儿元数据危害更小）
		return fmt.Errorf("remove session dir (row already deleted): %w", err)
	}
	return nil
}

func (s *SessionStore) UpdateSummary(id string, msgCount int, lastMsg string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET message_count=?, last_message=?, updated_at=? WHERE id=?`,
		msgCount, lastMsg, time.Now().UTC().Format(sqliteTimeFormat), id,
	)
	if err != nil {
		return fmt.Errorf("update summary: %w", err)
	}
	return nil
}

func (s *SessionStore) BaseDir() string {
	return s.baseDir
}

func (s *SessionStore) Close() error {
	return s.db.Close()
}

const sqliteTimeFormat = "2006-01-02 15:04:05"

var timeFormats = []string{
	time.RFC3339,
	time.RFC3339Nano,
	sqliteTimeFormat,
	"2006-01-02T15:04:05Z",
}

func parseTime(s string) time.Time {
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func scanSessionRecord(row *sql.Row) (*SessionRecord, error) {
	var rec SessionRecord
	var createdAt, updatedAt string
	var metadata sql.NullString
	err := row.Scan(
		&rec.ID, &rec.Title, &rec.Status, &rec.AgentName, &rec.ProviderName,
		&rec.ModelID, &metadata, &rec.MessageCount, &rec.LastMessage,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = parseTime(createdAt)
	rec.UpdatedAt = parseTime(updatedAt)
	rec.Metadata = metadata.String
	return &rec, nil
}

func scanSessionRows(rows *sql.Rows) (*SessionRecord, error) {
	var rec SessionRecord
	var createdAt, updatedAt string
	var metadata sql.NullString
	err := rows.Scan(
		&rec.ID, &rec.Title, &rec.Status, &rec.AgentName, &rec.ProviderName,
		&rec.ModelID, &metadata, &rec.MessageCount, &rec.LastMessage,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = parseTime(createdAt)
	rec.UpdatedAt = parseTime(updatedAt)
	rec.Metadata = metadata.String
	return &rec, nil
}
