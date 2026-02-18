package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"games/internal/game"
	"games/internal/session"
)

func TestListGames(t *testing.T) {
	env := setupTestEnv(t)

	resp, err := http.Get(env.ts.URL + "/api/games")
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
	env := setupTestEnv(t)

	body := `{"gameType":"tictactoe","playerId":"alice"}`
	resp, err := http.Post(env.ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
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
	env := setupTestEnv(t)

	body := `{"gameType":"","playerId":""}`
	resp, err := http.Post(env.ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateSessionInvalidBody(t *testing.T) {
	env := setupTestEnv(t)

	resp, err := http.Post(env.ts.URL+"/api/sessions", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateSessionUnknownGame(t *testing.T) {
	env := setupTestEnv(t)

	body := `{"gameType":"chess","playerId":"alice"}`
	resp, err := http.Post(env.ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetSessionFound(t *testing.T) {
	env := setupTestEnv(t)

	// Create a session first
	body := `{"gameType":"tictactoe","playerId":"alice"}`
	resp, err := http.Post(env.ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	resp.Body.Close()

	// Get it
	resp, err = http.Get(env.ts.URL + "/api/sessions/" + created.Code)
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
	env := setupTestEnv(t)

	resp, err := http.Get(env.ts.URL + "/api/sessions/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStartSessionValid(t *testing.T) {
	env := setupTestEnv(t)

	sess, err := env.mgr.Create("tictactoe")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sess.AddPlayer("alice"); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := sess.AddPlayer("bob"); err != nil {
		t.Fatalf("add bob: %v", err)
	}

	resp, err := http.Post(env.ts.URL+"/api/sessions/"+sess.Code+"/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST start: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestStartSessionNotFound(t *testing.T) {
	env := setupTestEnv(t)

	resp, err := http.Post(env.ts.URL+"/api/sessions/nonexistent/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStartSessionNotEnoughPlayers(t *testing.T) {
	env := setupTestEnv(t)

	sess, err := env.mgr.Create("tictactoe")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sess.AddPlayer("alice"); err != nil {
		t.Fatalf("add alice: %v", err)
	}

	resp, err := http.Post(env.ts.URL+"/api/sessions/"+sess.Code+"/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
