package storage

import (
	"database/sql"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateSession(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("abc123", "tictactoe"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Duplicate code should error
	if err := s.CreateSession("abc123", "tictactoe"); err == nil {
		t.Fatal("expected error on duplicate code")
	}
}

func TestGetSession(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("abc123", "tictactoe")

	row, err := s.GetSession("abc123")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if row.Code != "abc123" {
		t.Fatalf("expected code abc123, got %s", row.Code)
	}
	if row.GameType != "tictactoe" {
		t.Fatalf("expected gameType tictactoe, got %s", row.GameType)
	}
	if row.Status != "waiting" {
		t.Fatalf("expected status waiting, got %s", row.Status)
	}
	if row.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSession("nonexistent")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestUpdateSessionStatus(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("abc123", "tictactoe")

	if err := s.UpdateSessionStatus("abc123", "playing"); err != nil {
		t.Fatalf("update status: %v", err)
	}
	row, err := s.GetSession("abc123")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if row.Status != "playing" {
		t.Fatalf("expected playing, got %s", row.Status)
	}
}

func TestListSessionsAll(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("aaa", "tictactoe")
	s.CreateSession("bbb", "tictactoe")
	s.CreateSession("ccc", "tictactoe")

	rows, err := s.ListSessions("")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(rows))
	}
}

func TestListSessionsFiltered(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("aaa", "tictactoe")
	s.CreateSession("bbb", "tictactoe")
	s.UpdateSessionStatus("bbb", "playing")

	rows, err := s.ListSessions("waiting")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 waiting session, got %d", len(rows))
	}
	if rows[0].Code != "aaa" {
		t.Fatalf("expected code aaa, got %s", rows[0].Code)
	}
}

func TestSaveAndGetMatchState(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("abc123", "tictactoe")

	stateJSON := `{"board":[0,0,0,0,1,0,0,0,0],"turn":1}`
	if err := s.SaveMatchState("abc123", stateJSON); err != nil {
		t.Fatalf("save match state: %v", err)
	}
	got, err := s.GetMatchState("abc123")
	if err != nil {
		t.Fatalf("get match state: %v", err)
	}
	if got != stateJSON {
		t.Fatalf("expected %s, got %s", stateJSON, got)
	}
}

func TestSaveMatchStateUpsert(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("abc123", "tictactoe")

	s.SaveMatchState("abc123", `{"v":1}`)
	s.SaveMatchState("abc123", `{"v":2}`)

	got, err := s.GetMatchState("abc123")
	if err != nil {
		t.Fatalf("get match state: %v", err)
	}
	if got != `{"v":2}` {
		t.Fatalf("expected upserted value, got %s", got)
	}
}

func TestDeleteSession(t *testing.T) {
	s := newTestStore(t)
	s.CreateSession("abc123", "tictactoe")
	s.SaveMatchState("abc123", `{"v":1}`)

	if err := s.DeleteSession("abc123"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	_, err := s.GetSession("abc123")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
	_, err = s.GetMatchState("abc123")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for match state after delete, got %v", err)
	}
}

func TestGetMatchStateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetMatchState("nonexistent")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
