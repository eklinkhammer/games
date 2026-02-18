package session

import (
	"encoding/json"
	"regexp"
	"sort"
	"testing"
	"time"

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

// --- Session mutation tests ---

func TestRemovePlayer(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	p := sess.GetPlayer("alice")
	send := p.Send

	sess.RemovePlayer("alice")

	info := sess.Info()
	if len(info.Players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(info.Players))
	}
	// Channel should be closed
	_, ok := <-send
	if ok {
		t.Fatal("expected send channel to be closed")
	}
}

func TestRemovePlayerNonexistent(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	// Should not panic
	sess.RemovePlayer("nobody")
}

func TestConnectPlayer(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	newSend := make(chan []byte, 64)
	ok := sess.ConnectPlayer("alice", newSend)
	if !ok {
		t.Fatal("expected ConnectPlayer to return true")
	}

	// Verify the channel was replaced
	p := sess.GetPlayer("alice")
	if p.Send != newSend {
		t.Fatal("expected Send channel to be replaced")
	}
}

func TestConnectPlayerNonexistent(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	ok := sess.ConnectPlayer("nobody", make(chan []byte, 1))
	if ok {
		t.Fatal("expected ConnectPlayer to return false for unknown player")
	}
}

func TestGetPlayerFound(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	p := sess.GetPlayer("alice")
	if p == nil {
		t.Fatal("expected non-nil player")
	}
	if p.ID != "alice" {
		t.Fatalf("expected player ID alice, got %s", p.ID)
	}
}

func TestGetPlayerNotFound(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	p := sess.GetPlayer("nobody")
	if p != nil {
		t.Fatal("expected nil for unknown player")
	}
}

func TestBroadcastDelivery(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	msg := []byte(`{"type":"test"}`)
	sess.Broadcast(msg)

	aliceP := sess.GetPlayer("alice")
	bobP := sess.GetPlayer("bob")

	select {
	case got := <-aliceP.Send:
		if string(got) != string(msg) {
			t.Fatalf("alice got %s, expected %s", got, msg)
		}
	default:
		t.Fatal("expected alice to receive broadcast")
	}
	select {
	case got := <-bobP.Send:
		if string(got) != string(msg) {
			t.Fatalf("bob got %s, expected %s", got, msg)
		}
	default:
		t.Fatal("expected bob to receive broadcast")
	}
}

func TestBroadcastBufferFull(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	p := sess.GetPlayer("alice")
	// Fill the buffer
	for i := 0; i < cap(p.Send); i++ {
		p.Send <- []byte("filler")
	}

	// Should not panic or block
	sess.Broadcast([]byte(`{"type":"dropped"}`))
}

func TestPlayerIDs(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	ids := sess.PlayerIDs()
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "alice" || ids[1] != "bob" {
		t.Fatalf("expected [alice bob], got %v", ids)
	}
}

func TestHostAssignment(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	if sess.Info().HostID != "alice" {
		t.Fatalf("expected alice as host, got %s", sess.Info().HostID)
	}

	sess.AddPlayer("bob")
	if sess.Info().HostID != "alice" {
		t.Fatalf("expected host to remain alice, got %s", sess.Info().HostID)
	}
}

func TestAddPlayerDuplicate(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	err := sess.AddPlayer("alice")
	if err == nil {
		t.Fatal("expected error on duplicate player")
	}
}

func TestAddPlayerToStartedSession(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()

	err := sess.AddPlayer("charlie")
	if err == nil {
		t.Fatal("expected error adding player to started session")
	}
}

func TestStartTwice(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()

	err := sess.Start()
	if err == nil {
		t.Fatal("expected error on second Start()")
	}
}

func TestFinish(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()
	sess.Finish()

	if sess.Info().Status != StatusFinished {
		t.Fatalf("expected finished, got %s", sess.Info().Status)
	}
}

// --- Manager edge case tests ---

func TestManagerRemove(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	sess, _ := mgr.Create("tictactoe")
	code := sess.Code

	mgr.Remove(code)

	_, ok := mgr.Get(code)
	if ok {
		t.Fatal("expected session to be removed")
	}
}

func TestManagerList(t *testing.T) {
	mgr, cleanup := setupTest(t)
	defer cleanup()

	mgr.Create("tictactoe")
	mgr.Create("tictactoe")

	infos := mgr.List()
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
}

func TestManagerSaveSessionPlayers(t *testing.T) {
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

	if err := mgr.SaveSessionPlayers(sess); err != nil {
		t.Fatalf("save session players: %v", err)
	}

	// Verify roundtrip through the manager's own load method
	mgr2 := NewManager(reg, store)
	snap, err := mgr2.loadSessionPlayers(sess.Code)
	if err != nil {
		t.Fatalf("load session players: %v", err)
	}
	if len(snap.Players) != 2 {
		t.Fatalf("expected 2 players persisted, got %d", len(snap.Players))
	}
	if snap.HostID == "" {
		t.Fatal("expected non-empty hostId")
	}
}

func TestManagerCleanupFinished(t *testing.T) {
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
	sess.Finish()
	code := sess.Code

	// Cleanup with maxAge=0 should remove finished sessions
	mgr.cleanup(0)

	_, ok := mgr.Get(code)
	if ok {
		t.Fatal("expected finished session to be cleaned up")
	}
}

func TestManagerCleanupEmpty(t *testing.T) {
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})
	mgr := NewManager(reg, store)

	sess, _ := mgr.Create("tictactoe")
	code := sess.Code
	// No players added — empty session

	mgr.cleanup(time.Hour)

	_, ok := mgr.Get(code)
	if ok {
		t.Fatal("expected empty session to be cleaned up immediately")
	}
}

func TestManagerCleanupKeepsActive(t *testing.T) {
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
	code := sess.Code

	mgr.cleanup(time.Hour)

	_, ok := mgr.Get(code)
	if !ok {
		t.Fatal("expected active waiting session to be kept")
	}
}

func TestGenerateCodeFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{6}$`)
	for i := 0; i < 20; i++ {
		code := generateCode()
		if !re.MatchString(code) {
			t.Fatalf("expected 6 hex chars, got %q", code)
		}
	}
}
