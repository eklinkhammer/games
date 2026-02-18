package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"games/internal/session"
)

// --- Static File Test ---

func TestStaticFileServing(t *testing.T) {
	env := setupTestEnv(t)

	resp, err := http.Get(env.ts.URL + "/index.html")
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
	env := setupTestEnv(t)
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, env.ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn) // drain alice's initial state

	bobConn := wsConnect(t, env.ts, code, "bob")
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
	env := setupTestEnv(t)

	t.Run("correct format succeeds", func(t *testing.T) {
		ctx, cancel := timeoutCtx(t)
		defer cancel()

		code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")
		aliceConn := wsConnect(t, env.ts, code, "alice")
		defer aliceConn.Close(websocket.StatusNormalClosure, "")
		readState(t, ctx, aliceConn)

		conn, _, err := websocket.Dial(ctx, wsURL(env.ts, code), nil)
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

		code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")
		conn, _, err := websocket.Dial(ctx, wsURL(env.ts, code), nil)
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
	env := setupTestEnv(t)
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, env.ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, env.ts, code, "bob")
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

func TestWebSocketPlayFullGame(t *testing.T) {
	env := setupTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, env.ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, env.ts, code, "bob")
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
	sm := stateMap(t, startState)
	firstPlayer, ok := sm["turn"].(string)
	if !ok {
		t.Fatalf("expected turn to be string, got %T", sm["turn"])
	}
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
		if err := sendWS(ctx, mv.conn, "action", makeAction(t, mv.cell)); err != nil {
			t.Fatalf("move %d: send: %v", i, err)
		}
		lastAliceState = readState(t, ctx, aliceConn)
		lastBobState = readState(t, ctx, bobConn)

		moveSM := stateMap(t, lastAliceState)
		board, ok := moveSM["board"].([]any)
		if !ok {
			t.Fatalf("move %d: expected board to be []any, got %T", i, moveSM["board"])
		}
		if board[mv.cell] == float64(0) {
			t.Fatalf("move %d: cell %d not updated", i, mv.cell)
		}
		// Turn alternates (except on last move when game is done)
		if i < len(moves)-1 {
			nextTurn, ok := moveSM["turn"].(string)
			if !ok {
				t.Fatalf("move %d: expected turn to be string, got %T", i, moveSM["turn"])
			}
			if nextTurn == mv.playerID {
				t.Fatalf("move %d: turn didn't alternate, still %s", i, mv.playerID)
			}
		}
	}

	// Verify final state
	finalMap := stateMap(t, lastAliceState)
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
	bobFinalMap := stateMap(t, lastBobState)
	if bobFinalMap["done"] != true {
		t.Fatal("bob: expected game to be done")
	}
}

func TestWebSocketActionEncodingCorrectness(t *testing.T) {
	env := setupTestEnv(t)
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, env.ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, env.ts, code, "bob")
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
	sm := stateMap(t, startState)
	firstPlayer, ok := sm["turn"].(string)
	if !ok {
		t.Fatalf("expected turn to be string, got %T", sm["turn"])
	}
	var conn *websocket.Conn
	if firstPlayer == "alice" {
		conn = aliceConn
	} else {
		conn = bobConn
	}
	if err := sendWS(ctx, conn, "action", makeAction(t, 0)); err != nil {
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

func TestWebSocketActionWrongTurn(t *testing.T) {
	env := setupTestEnv(t)
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, env.ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, env.ts, code, "bob")
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
	sm := stateMap(t, startState)
	firstPlayer, ok := sm["turn"].(string)
	if !ok {
		t.Fatalf("expected turn to be string, got %T", sm["turn"])
	}
	var wrongConn *websocket.Conn
	if firstPlayer == "alice" {
		wrongConn = bobConn
	} else {
		wrongConn = aliceConn
	}
	if err := sendWS(ctx, wrongConn, "action", makeAction(t, 0)); err != nil {
		t.Fatalf("send action: %v", err)
	}

	errMsg := readError(t, ctx, wrongConn)
	if !strings.Contains(errMsg, "not your turn") {
		t.Fatalf("expected 'not your turn', got %q", errMsg)
	}
}

func TestWebSocketReconnect(t *testing.T) {
	env := setupTestEnv(t)
	ctx, cancel := timeoutCtx(t)
	defer cancel()

	code := createSessionViaAPI(t, env.ts, "tictactoe", "alice")

	aliceConn := wsConnect(t, env.ts, code, "alice")
	defer aliceConn.Close(websocket.StatusNormalClosure, "")
	readState(t, ctx, aliceConn)

	bobConn := wsConnect(t, env.ts, code, "bob")
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
	sm := stateMap(t, startState)
	firstPlayer, ok := sm["turn"].(string)
	if !ok {
		t.Fatalf("expected turn to be string, got %T", sm["turn"])
	}
	var firstConn *websocket.Conn
	if firstPlayer == "alice" {
		firstConn = aliceConn
	} else {
		firstConn = bobConn
	}
	if err := sendWS(ctx, firstConn, "action", makeAction(t, 4)); err != nil {
		t.Fatalf("send action: %v", err)
	}
	readState(t, ctx, aliceConn)
	readState(t, ctx, bobConn)

	// Close bob's connection
	bobConn.Close(websocket.StatusNormalClosure, "")

	// Reconnect bob
	bobConn2 := wsConnect(t, env.ts, code, "bob")
	defer bobConn2.Close(websocket.StatusNormalClosure, "")

	// Bob should receive fresh state with game preserved
	bobState := readState(t, ctx, bobConn2)

	if bobState.SessionInfo.Status != session.StatusPlaying {
		t.Fatalf("expected playing, got %s", bobState.SessionInfo.Status)
	}
	sm2 := stateMap(t, bobState)
	board, ok := sm2["board"].([]any)
	if !ok {
		t.Fatalf("expected board to be []any, got %T", sm2["board"])
	}
	if board[4] == float64(0) {
		t.Fatal("expected cell 4 to be occupied after reconnect")
	}
}
