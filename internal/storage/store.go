package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SessionRow represents a session in the database.
type SessionRow struct {
	Code      string
	GameType  string
	Status    string // "waiting", "playing", "finished"
	CreatedAt time.Time
}

// MatchStateRow represents serialized match state.
type MatchStateRow struct {
	SessionCode string
	StateJSON   string
	UpdatedAt   time.Time
}

// Store handles SQLite persistence.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the database and runs migrations.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// WAL mode for better concurrent reads
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			code       TEXT PRIMARY KEY,
			game_type  TEXT NOT NULL,
			status     TEXT NOT NULL DEFAULT 'waiting',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS match_state (
			session_code TEXT PRIMARY KEY REFERENCES sessions(code),
			state_json   TEXT NOT NULL,
			updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

// CreateSession inserts a new session.
func (s *Store) CreateSession(code, gameType string) error {
	_, err := s.db.Exec(
		"INSERT INTO sessions (code, game_type, status) VALUES (?, ?, 'waiting')",
		code, gameType,
	)
	return err
}

// GetSession retrieves a session by code.
func (s *Store) GetSession(code string) (*SessionRow, error) {
	row := s.db.QueryRow("SELECT code, game_type, status, created_at FROM sessions WHERE code = ?", code)
	var sr SessionRow
	if err := row.Scan(&sr.Code, &sr.GameType, &sr.Status, &sr.CreatedAt); err != nil {
		return nil, err
	}
	return &sr, nil
}

// UpdateSessionStatus changes a session's status.
func (s *Store) UpdateSessionStatus(code, status string) error {
	_, err := s.db.Exec("UPDATE sessions SET status = ? WHERE code = ?", status, code)
	return err
}

// ListSessions returns all sessions with the given status (or all if status is empty).
func (s *Store) ListSessions(status string) ([]SessionRow, error) {
	var rows *sql.Rows
	var err error
	if status == "" {
		rows, err = s.db.Query("SELECT code, game_type, status, created_at FROM sessions ORDER BY created_at DESC")
	} else {
		rows, err = s.db.Query("SELECT code, game_type, status, created_at FROM sessions WHERE status = ? ORDER BY created_at DESC", status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SessionRow
	for rows.Next() {
		var sr SessionRow
		if err := rows.Scan(&sr.Code, &sr.GameType, &sr.Status, &sr.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, sr)
	}
	return result, rows.Err()
}

// SaveMatchState upserts match state JSON.
func (s *Store) SaveMatchState(sessionCode, stateJSON string) error {
	_, err := s.db.Exec(`
		INSERT INTO match_state (session_code, state_json, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(session_code) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at
	`, sessionCode, stateJSON)
	return err
}

// GetMatchState retrieves match state JSON.
func (s *Store) GetMatchState(sessionCode string) (string, error) {
	var stateJSON string
	err := s.db.QueryRow("SELECT state_json FROM match_state WHERE session_code = ?", sessionCode).Scan(&stateJSON)
	return stateJSON, err
}

// DeleteSession removes a session and its match state.
func (s *Store) DeleteSession(code string) error {
	_, err := s.db.Exec("DELETE FROM match_state WHERE session_code = ?", code)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM sessions WHERE code = ?", code)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
