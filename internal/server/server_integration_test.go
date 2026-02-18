package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"nhooyr.io/websocket"

	"games/internal/game"
	"games/internal/game/tictactoe"
	"games/internal/session"
	"games/internal/storage"
)

// --- Helpers ---

func setupIntegrationTest(t *testing.T) (*httptest.Server, *session.Manager, func()) {
	t.Helper()
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	reg := game.NewRegistry()
	reg.Register(tictactoe.TicTacToe{})
	mgr := session.NewManager(reg, store)
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><body>test</body></html>"),
		},
	}
	srv := New(reg, mgr, webFS)
	ts := httptest.NewServer(srv)
	return ts, mgr, func() {
		ts.Close()
		store.Close()
	}
}

func createSessionViaAPI(t *testing.T, ts *httptest.Server, gameType, playerID string) string {
	t.Helper()
	body := fmt.Sprintf(`{"gameType":%q,"playerId":%q}`, gameType, playerID)
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var result createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result.Code
}

func wsEndpoint(ts *httptest.Server, code string) string {
	return strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/sessions/" + code + "/ws"
}

func wsConnect(t *testing.T, ts *httptest.Server, code, playerID string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsEndpoint(ts, code), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	if err := sendWS(ctx, conn, "join", joinPayload{PlayerID: playerID}); err != nil {
		t.Fatalf("send join: %v", err)
	}
	return conn
}

func sendWS(ctx context.Context, conn *websocket.Conn, msgType string, payload any) error {
	p, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg, err := json.Marshal(WSMessage{Type: msgType, Payload: p})
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, msg)
}

func readWS(ctx context.Context, conn *websocket.Conn) (WSMessage, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return WSMessage{}, err
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return WSMessage{}, err
	}
	return msg, nil
}

func readState(t *testing.T, ctx context.Context, conn *websocket.Conn) statePayload {
	t.Helper()
	msg, err := readWS(ctx, conn)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if msg.Type != "state" {
		t.Fatalf("expected state message, got %q: %s", msg.Type, string(msg.Payload))
	}
	var sp statePayload
	if err := json.Unmarshal(msg.Payload, &sp); err != nil {
		t.Fatalf("unmarshal state payload: %v", err)
	}
	return sp
}

func readError(t *testing.T, ctx context.Context, conn *websocket.Conn) string {
	t.Helper()
	msg, err := readWS(ctx, conn)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if msg.Type != "error" {
		t.Fatalf("expected error message, got %q: %s", msg.Type, string(msg.Payload))
	}
	var ep errorPayload
	if err := json.Unmarshal(msg.Payload, &ep); err != nil {
		t.Fatalf("unmarshal error payload: %v", err)
	}
	return ep.Message
}

func timeoutCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func containsPlayer(players []string, id string) bool {
	for _, p := range players {
		if p == id {
			return true
		}
	}
	return false
}

func makeAction(cell int) actionPayload {
	payload, _ := json.Marshal(map[string]int{"cell": cell})
	return actionPayload{
		Action: game.Action{Type: "move", Payload: payload},
	}
}

// --- REST API Tests ---

func TestIntegrationListGames(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/games")
	if err != nil {
		t.Fatalf("get games: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var games []game.GameInfo
	if err := json.NewDecoder(resp.Body).Decode(&games); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("expected 1 game, got %d", len(games))
	}
	if games[0].Name != "tictactoe" {
		t.Fatalf("expected tictactoe, got %s", games[0].Name)
	}
	if games[0].MinPlayers != 2 || games[0].MaxPlayers != 2 {
		t.Fatalf("expected min=2 max=2, got min=%d max=%d", games[0].MinPlayers, games[0].MaxPlayers)
	}
}

func TestCreateSession(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")
	if code == "" {
		t.Fatal("expected non-empty code")
	}

	resp, err := http.Get(ts.URL + "/api/sessions/" + code)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var info session.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(info.Players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(info.Players))
	}
	if info.Status != session.StatusWaiting {
		t.Fatalf("expected waiting, got %s", info.Status)
	}
}

func TestCreateSessionValidation(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	tests := []struct {
		name string
		body string
	}{
		{"missing gameType", `{"playerId":"alice"}`},
		{"missing playerId", `{"gameType":"tictactoe"}`},
		{"unknown gameType", `{"gameType":"chess","playerId":"alice"}`},
		{"invalid JSON", `not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestIntegrationGetSessionNotFound(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/sessions/nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStaticFileServing(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/index.html")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "test") {
		t.Fatalf("expected test content, got %s", string(body))
	}
}

// --- WebSocket Join & Encoding Tests ---

func TestWebSocketJoin(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn) // drain alice's initial state

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")

	// Both receive state broadcast with both players
	aliceState := readState(t, ctx, aliceConn)
	bobState := readState(t, ctx, bobConn)

	if len(aliceState.SessionInfo.Players) != 2 {
		t.Fatalf("alice: expected 2 players, got %d", len(aliceState.SessionInfo.Players))
	}
	if len(bobState.SessionInfo.Players) != 2 {
		t.Fatalf("bob: expected 2 players, got %d", len(bobState.SessionInfo.Players))
	}
	if !containsPlayer(aliceState.SessionInfo.Players, "alice") || !containsPlayer(aliceState.SessionInfo.Players, "bob") {
		t.Fatalf("expected both players: %v", aliceState.SessionInfo.Players)
	}
}

func TestWebSocketJoinEncodingNotDoubleEncoded(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	t.Run("correct format succeeds", func(t *testing.T) {
		ctx, cancel := timeoutCtx(t)
		defer cancel()

		code := createSessionViaAPI(t, ts, "tictactoe", "alice")
		aliceConn := wsConnect(t, ts, code, "alice")
		defer aliceConn.Close(websocket.StatusNormalClosure, "")
		readState(t, ctx, aliceConn)

		conn, _, err := websocket.Dial(ctx, wsEndpoint(ts, code), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Correct format: payload is a JSON object
		raw := `{"type":"join","payload":{"playerId":"bob"}}`
		if err := conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
			t.Fatalf("write: %v", err)
		}

		msg, err := readWS(ctx, conn)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type != "state" {
			t.Fatalf("expected state, got %q", msg.Type)
		}
	})

	t.Run("double-encoded format fails", func(t *testing.T) {
		ctx, cancel := timeoutCtx(t)
		defer cancel()

		code := createSessionViaAPI(t, ts, "tictactoe", "alice")
		conn, _, err := websocket.Dial(ctx, wsEndpoint(ts, code), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Double-encoded: payload is a JSON string, not an object
		raw := `{"type":"join","payload":"{\"playerId\":\"charlie\"}"}`
		if err := conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
			t.Fatalf("write: %v", err)
		}

		errMsg := readError(t, ctx, conn)
		if !strings.Contains(errMsg, "invalid join payload") {
			t.Fatalf("expected 'invalid join payload', got %q", errMsg)
		}
	})
}

// --- WebSocket Game Flow Tests ---

func TestWebSocketStartGame(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Host sends start
	if err := sendWS(ctx, aliceConn, "start", nil); err != nil {
		t.Fatalf("send start: %v", err)
	}

	aliceState := readState(t, ctx, aliceConn)
	bobState := readState(t, ctx, bobConn)

	if aliceState.SessionInfo.Status != session.StatusPlaying {
		t.Fatalf("alice: expected playing, got %s", aliceState.SessionInfo.Status)
	}
	if bobState.SessionInfo.Status != session.StatusPlaying {
		t.Fatalf("bob: expected playing, got %s", bobState.SessionInfo.Status)
	}
	if aliceState.State == nil {
		t.Fatal("alice: expected non-nil game state")
	}
	if bobState.State == nil {
		t.Fatal("bob: expected non-nil game state")
	}
}

func TestWebSocketStartGameNonHost(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Non-host sends start
	if err := sendWS(ctx, bobConn, "start", nil); err != nil {
		t.Fatalf("send start: %v", err)
	}

	errMsg := readError(t, ctx, bobConn)
	if !strings.Contains(errMsg, "only the host can start") {
		t.Fatalf("expected 'only the host can start', got %q", errMsg)
	}
}

func TestWebSocketPlayFullGame(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Start game
	if err := sendWS(ctx, aliceConn, "start", nil); err != nil {
		t.Fatalf("send start: %v", err)
	}
	startState := readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Determine who goes first (map iteration order is non-deterministic)
	stateMap := startState.State.(map[string]any)
	firstPlayer := stateMap["turn"].(string)
	var firstConn, secondConn *websocket.Conn
	var secondPlayer string
	if firstPlayer == "alice" {
		firstConn = aliceConn
		secondConn = bobConn
		secondPlayer = "bob"
	} else {
		firstConn = bobConn
		secondConn = aliceConn
		secondPlayer = "alice"
	}

	// First player takes 0, 1, 2 (wins row 0); second takes 3, 4
	moves := []struct {
		conn     *websocket.Conn
		playerID string
		cell     int
	}{
		{firstConn, firstPlayer, 0},
		{secondConn, secondPlayer, 3},
		{firstConn, firstPlayer, 1},
		{secondConn, secondPlayer, 4},
		{firstConn, firstPlayer, 2}, // wins
	}

	var lastAliceState, lastBobState statePayload
	for i, mv := range moves {
		if err := sendWS(ctx, mv.conn, "action", makeAction(mv.cell)); err != nil {
			t.Fatalf("move %d: send: %v", i, err)
		}
		lastAliceState = readState(t, ctx, aliceConn)
		lastBobState = readState(t, ctx, bobConn)

		sm := lastAliceState.State.(map[string]any)
		board := sm["board"].([]any)
		if board[mv.cell] == float64(0) {
			t.Fatalf("move %d: cell %d not updated", i, mv.cell)
		}
		// Turn alternates (except on last move when game is done)
		if i < len(moves)-1 {
			nextTurn := sm["turn"].(string)
			if nextTurn == mv.playerID {
				t.Fatalf("move %d: turn didn't alternate, still %s", i, mv.playerID)
			}
		}
	}

	// Verify final state
	finalMap := lastAliceState.State.(map[string]any)
	if finalMap["done"] != true {
		t.Fatal("expected game to be done")
	}
	if lastAliceState.Results == nil {
		t.Fatal("expected results")
	}
	var winnerFound bool
	for _, r := range lastAliceState.Results {
		if r.PlayerID == firstPlayer && r.Rank == 1 {
			winnerFound = true
		}
	}
	if !winnerFound {
		t.Fatalf("expected %s to win, results: %+v", firstPlayer, lastAliceState.Results)
	}
	bobFinalMap := lastBobState.State.(map[string]any)
	if bobFinalMap["done"] != true {
		t.Fatal("bob: expected game to be done")
	}
}

func TestWebSocketActionEncodingCorrectness(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Start
	if err := sendWS(ctx, aliceConn, "start", nil); err != nil {
		t.Fatalf("send start: %v", err)
	}
	startState := readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Make a move with whoever goes first
	stateMap := startState.State.(map[string]any)
	firstPlayer := stateMap["turn"].(string)
	var conn *websocket.Conn
	if firstPlayer == "alice" {
		conn = aliceConn
	} else {
		conn = bobConn
	}
	if err := sendWS(ctx, conn, "action", makeAction(0)); err != nil {
		t.Fatalf("send action: %v", err)
	}

	// Read raw WS bytes and check encoding
	_, data, err := aliceConn.Read(ctx)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	// payload should be an object, not a double-encoded string
	switch raw["payload"].(type) {
	case map[string]any:
		// correct
	case string:
		t.Fatal("payload is a string (double-encoded), expected a JSON object")
	default:
		t.Fatalf("unexpected payload type: %T", raw["payload"])
	}
}

// --- WebSocket Error Handling Tests ---

func TestWebSocketActionBeforeStart(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	if err := sendWS(ctx, aliceConn, "action", makeAction(0)); err != nil {
		t.Fatalf("send action: %v", err)
	}

	errMsg := readError(t, ctx, aliceConn)
	if !strings.Contains(errMsg, "game not started") {
		t.Fatalf("expected 'game not started', got %q", errMsg)
	}
}

func TestWebSocketActionWrongTurn(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Start
	if err := sendWS(ctx, aliceConn, "start", nil); err != nil {
		t.Fatalf("send start: %v", err)
	}
	startState := readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Send action from wrong player
	stateMap := startState.State.(map[string]any)
	firstPlayer := stateMap["turn"].(string)
	var wrongConn *websocket.Conn
	if firstPlayer == "alice" {
		wrongConn = bobConn
	} else {
		wrongConn = aliceConn
	}
	if err := sendWS(ctx, wrongConn, "action", makeAction(0)); err != nil {
		t.Fatalf("send action: %v", err)
	}

	errMsg := readError(t, ctx, wrongConn)
	if !strings.Contains(errMsg, "not your turn") {
		t.Fatalf("expected 'not your turn', got %q", errMsg)
	}
}

func TestWebSocketReconnect(t *testing.T) {
	ts, _, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, ts, code, "bob")
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Start
	if err := sendWS(ctx, aliceConn, "start", nil); err != nil {
		t.Fatalf("send start: %v", err)
	}
	startState := readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Make a move
	stateMap := startState.State.(map[string]any)
	firstPlayer := stateMap["turn"].(string)
	var firstConn *websocket.Conn
	if firstPlayer == "alice" {
		firstConn = aliceConn
	} else {
		firstConn = bobConn
	}
	if err := sendWS(ctx, firstConn, "action", makeAction(4)); err != nil {
		t.Fatalf("send action: %v", err)
	}
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Close bob's connection
	bobConn.Close(websocket.StatusNormalClosure, "")

	// Reconnect bob
	bobConn2 := wsConnect(t, ts, code, "bob")
	defer bobConn2.Close(websocket.StatusNormalClosure, "")

	// Bob should receive fresh state with game preserved
	bobState := readState(t, ctx, bobConn2)

	if bobState.SessionInfo.Status != session.StatusPlaying {
		t.Fatalf("expected playing, got %s", bobState.SessionInfo.Status)
	}
	stateMap2 := bobState.State.(map[string]any)
	board := stateMap2["board"].([]any)
	if board[4] == float64(0) {
		t.Fatal("expected cell 4 to be occupied after reconnect")
	}
}
