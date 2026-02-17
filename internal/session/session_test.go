package session

import (
	"encoding/json"
	"testing"

	"games/internal/game"
	"games/internal/game/tictactoe"
	"games/internal/storage"
)

func setupTest(t *testing.T) (*Manager, func()) {
	t.Helper()
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})
	mgr := NewManager(reg, store)
	return mgr, func() { store.Close() }
}

func TestCreateAndJoin(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, err := mgr.Create("tictactoe")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.Code == "" {
		t.Fatal("expected non-empty code")
	}

	if err := sess.AddPlayer("alice"); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := sess.AddPlayer("bob"); err != nil {
		t.Fatalf("add bob: %v", err)
	}

	info := sess.Info()
	if len(info.Players) != 2 {
		t.Fatalf("expected 2 players, got %d", len(info.Players))
	}
	if info.Status != StatusWaiting {
		t.Fatalf("expected waiting, got %s", info.Status)
	}
}

func TestSessionFull(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	err := sess.AddPlayer("charlie")
	if err == nil {
		t.Fatal("expected error for full session")
	}
}

func TestStartAndPlay(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	if err := sess.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if sess.Status != StatusPlaying {
		t.Fatalf("expected playing, got %s", sess.Status)
	}
	if sess.Match == nil {
		t.Fatal("expected match to be created")
	}
}

func TestStartNotEnoughPlayers(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	err := sess.Start()
	if err == nil {
		t.Fatal("expected error for not enough players")
	}
}

func TestPersistence(t *testing.T) {
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})

	mgr := NewManager(reg, store)
	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()

	// Make a move
	payload, _ := json.Marshal(map[string]int{"cell": 4})
	sess.Match.ApplyAction(sess.PlayerIDs()[0], game.Action{Type: "move", Payload: payload})

	// Save state
	if err := mgr.SaveMatchState(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Create new manager from same store, restore
	mgr2 := NewManager(reg, store)
	if err := mgr2.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	sess2, ok := mgr2.Get(sess.Code)
	if !ok {
		t.Fatal("session not restored")
	}
	if sess2.Status != StatusPlaying {
		t.Fatalf("expected playing, got %s", sess2.Status)
	}
	if sess2.Match == nil {
		t.Fatal("match not restored")
	}
}

func TestUnknownGameType(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	_, err := mgr.Create("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown game type")
	}
}
