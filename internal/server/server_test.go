package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"games/internal/game"
	"games/internal/game/tictactoe"
	"games/internal/session"
	"games/internal/storage"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})
	mgr := session.NewManager(reg, store)

	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	srv := New(reg, mgr, webFS)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func TestListGames(t *testing.T) {
	ts := setupTestServer(t)

	resp, err := http.Get(ts.URL + "/api/games")
	if err != nil {
		t.Fatalf("GET /api/games: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var games []game.GameInfo
	if err := json.NewDecoder(resp.Body).Decode(&games); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(games) != 1 || games[0].Name != "tictactoe" {
		t.Fatalf("expected [tictactoe], got %v", games)
	}
}

func TestCreateSessionValid(t *testing.T) {
	ts := setupTestServer(t)

	body := `{"gameType":"tictactoe","playerId":"alice"}`
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Code == "" {
		t.Fatal("expected non-empty code")
	}
}

func TestCreateSessionMissingFields(t *testing.T) {
	ts := setupTestServer(t)

	body := `{"gameType":"","playerId":""}`
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateSessionInvalidBody(t *testing.T) {
	ts := setupTestServer(t)

	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateSessionUnknownGame(t *testing.T) {
	ts := setupTestServer(t)

	body := `{"gameType":"chess","playerId":"alice"}`
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetSessionFound(t *testing.T) {
	ts := setupTestServer(t)

	// Create a session first
	body := `{"gameType":"tictactoe","playerId":"alice"}`
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created createSessionResponse
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// Get it
	resp, err = http.Get(ts.URL + "/api/sessions/" + created.Code)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var info session.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Code != created.Code {
		t.Fatalf("expected code %s, got %s", created.Code, info.Code)
	}
	if len(info.Players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(info.Players))
	}
}

func TestGetSessionNotFound(t *testing.T) {
	ts := setupTestServer(t)

	resp, err := http.Get(ts.URL + "/api/sessions/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStartSessionValid(t *testing.T) {
	ts := setupTestServer(t)

	// Create session with alice
	body := `{"gameType":"tictactoe","playerId":"alice"}`
	resp, _ := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	var created createSessionResponse
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// Add bob via another create... no, we need to join bob via WS or direct manager access.
	// Instead, use the GET to get the session info and join bob via a second POST won't work.
	// The server doesn't expose a "join" REST endpoint — joining is via WebSocket.
	// So let's access the manager through the server's internal state.
	// Actually, the test server setup doesn't expose the manager. Let me create a custom setup.

	// Create a custom test with direct manager access
	store, _ := storage.New(":memory:")
	defer store.Close()
	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})
	mgr := session.NewManager(reg, store)
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	srv := New(reg, mgr, webFS)
	testSrv := httptest.NewServer(srv)
	defer testSrv.Close()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	resp, err := http.Post(testSrv.URL+"/api/sessions/"+sess.Code+"/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST start: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestStartSessionNotFound(t *testing.T) {
	ts := setupTestServer(t)

	resp, err := http.Post(ts.URL+"/api/sessions/nonexistent/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStartSessionNotEnoughPlayers(t *testing.T) {
	store, _ := storage.New(":memory:")
	defer store.Close()
	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})
	mgr := session.NewManager(reg, store)
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	srv := New(reg, mgr, webFS)
	testSrv := httptest.NewServer(srv)
	defer testSrv.Close()

	sess, _ := mgr.Create("tictactoe")
	sess.AddPlayer("alice") // only 1 player, need 2

	resp, err := http.Post(testSrv.URL+"/api/sessions/"+sess.Code+"/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
