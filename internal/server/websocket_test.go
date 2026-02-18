package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"nhooyr.io/websocket"

	"games/internal/game"
	"games/internal/game/tictactoe"
	"games/internal/session"
	"games/internal/storage"

	"net/http/httptest"
)

type wsTestEnv struct {
	ts  *httptest.Server
	mgr *session.Manager
	reg *game.Registry
}

func setupWSTest(t *testing.T) *wsTestEnv {
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

	return &wsTestEnv{ts: ts, mgr: mgr, reg: reg}
}

func wsURL(ts *httptest.Server, code string) string {
	return strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/sessions/" + code + "/ws"
}

func wsSend(ctx context.Context, t *testing.T, conn *websocket.Conn, msg WSMessage) {
	t.Helper()
	data, _ := json.Marshal(msg)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func wsRead(ctx context.Context, t *testing.T, conn *websocket.Conn) WSMessage {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal ws message: %v", err)
	}
	return msg
}

func joinMsg(playerID string) WSMessage {
	payload, _ := json.Marshal(joinPayload{PlayerID: playerID})
	return WSMessage{Type: "join", Payload: payload}
}

func TestWSJoinAndReceiveState(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, conn, joinMsg("alice"))

	msg := wsRead(ctx, t, conn)
	if msg.Type != "state" {
		t.Fatalf("expected state message, got %s", msg.Type)
	}

	var sp statePayload
	if err := json.Unmarshal(msg.Payload, &sp); err != nil {
		t.Fatalf("unmarshal state payload: %v", err)
	}
	if sp.SessionInfo.Code != sess.Code {
		t.Fatalf("expected session code %s, got %s", sess.Code, sp.SessionInfo.Code)
	}
}

func TestWSJoinNewPlayer(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	// Don't pre-add "alice" — let the WS handler add them
	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, conn, joinMsg("alice"))

	msg := wsRead(ctx, t, conn)
	if msg.Type != "state" {
		t.Fatalf("expected state, got %s", msg.Type)
	}

	// Verify the player was added to the session
	p := sess.GetPlayer("alice")
	if p == nil {
		t.Fatal("expected alice to be added to session")
	}
}

func TestWSJoinInvalidPayload(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send join with empty playerId
	payload, _ := json.Marshal(joinPayload{PlayerID: ""})
	wsSend(ctx, t, conn, WSMessage{Type: "join", Payload: payload})

	msg := wsRead(ctx, t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error, got %s", msg.Type)
	}
}

func TestWSFirstMessageNotJoin(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send an action as first message instead of join
	actionPayload, _ := json.Marshal(map[string]string{"action": "move"})
	wsSend(ctx, t, conn, WSMessage{Type: "action", Payload: actionPayload})

	msg := wsRead(ctx, t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error, got %s", msg.Type)
	}
}

func TestWSSessionNotFound(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL(env.ts, "nonexistent"), nil)
	if err == nil {
		t.Fatal("expected dial to fail for unknown session")
	}
	if resp != nil && resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWSActionValid(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()

	// Connect alice
	connAlice, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial alice: %v", err)
	}
	defer connAlice.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, connAlice, joinMsg("alice"))
	// Read the state broadcast after join
	wsRead(ctx, t, connAlice)

	// Determine who goes first
	ids := sess.PlayerIDs()
	sess.RLock()
	match := sess.Match
	sess.RUnlock()

	firstPlayer := ""
	for _, id := range ids {
		actions := match.ValidActions(id)
		if len(actions) > 0 {
			firstPlayer = id
			break
		}
	}

	if firstPlayer != "alice" {
		t.Skip("alice is not the first player in this match order")
	}

	// Send a move action
	movePayload, _ := json.Marshal(map[string]int{"cell": 0})
	actionData, _ := json.Marshal(actionPayload{Action: game.Action{Type: "move", Payload: movePayload}})
	wsSend(ctx, t, connAlice, WSMessage{Type: "action", Payload: actionData})

	// Read the state update
	msg := wsRead(ctx, t, connAlice)
	if msg.Type != "state" {
		t.Fatalf("expected state after action, got %s", msg.Type)
	}
}

func TestWSActionGameNotStarted(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, conn, joinMsg("alice"))
	wsRead(ctx, t, conn) // state broadcast after join

	// Send action before game started
	movePayload, _ := json.Marshal(map[string]int{"cell": 0})
	actionData, _ := json.Marshal(actionPayload{Action: game.Action{Type: "move", Payload: movePayload}})
	wsSend(ctx, t, conn, WSMessage{Type: "action", Payload: actionData})

	msg := wsRead(ctx, t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error, got %s", msg.Type)
	}

	var ep errorPayload
	json.Unmarshal(msg.Payload, &ep)
	if ep.Message != "game not started" {
		t.Fatalf("expected 'game not started', got %q", ep.Message)
	}
}

func TestWSStartByHost(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// alice is host (first player added)
	wsSend(ctx, t, conn, joinMsg("alice"))
	wsRead(ctx, t, conn) // join state broadcast

	// Send start
	wsSend(ctx, t, conn, WSMessage{Type: "start", Payload: json.RawMessage("null")})

	msg := wsRead(ctx, t, conn)
	if msg.Type != "state" {
		t.Fatalf("expected state after start, got %s", msg.Type)
	}

	// Verify the game actually started
	var sp statePayload
	json.Unmarshal(msg.Payload, &sp)
	if sp.SessionInfo.Status != session.StatusPlaying {
		t.Fatalf("expected playing status, got %s", sp.SessionInfo.Status)
	}
}

func TestWSStartByNonHost(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice") // host
	sess.AddPlayer("bob")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Connect as bob (not host)
	wsSend(ctx, t, conn, joinMsg("bob"))
	wsRead(ctx, t, conn) // join state

	wsSend(ctx, t, conn, WSMessage{Type: "start", Payload: json.RawMessage("null")})

	msg := wsRead(ctx, t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error, got %s", msg.Type)
	}

	var ep errorPayload
	json.Unmarshal(msg.Payload, &ep)
	if !strings.Contains(ep.Message, "host") {
		t.Fatalf("expected host-related error, got %q", ep.Message)
	}
}

func TestWSUnknownMessageType(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, conn, joinMsg("alice"))
	wsRead(ctx, t, conn) // join state

	wsSend(ctx, t, conn, WSMessage{Type: "unknown", Payload: json.RawMessage("null")})

	msg := wsRead(ctx, t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error, got %s", msg.Type)
	}

	var ep errorPayload
	json.Unmarshal(msg.Payload, &ep)
	if !strings.Contains(ep.Message, "unknown") {
		t.Fatalf("expected 'unknown' in error message, got %q", ep.Message)
	}
}

func TestWSPayloadEncoding(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()

	conn, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, conn, joinMsg("alice"))

	msg := wsRead(ctx, t, conn)
	if msg.Type != "state" {
		t.Fatalf("expected state, got %s", msg.Type)
	}

	// The key test: verify the payload unmarshals correctly without double-decoding.
	// If double-encoded, msg.Payload would be a JSON string containing escaped JSON
	// and unmarshalling into statePayload would fail or produce empty fields.
	var sp statePayload
	if err := json.Unmarshal(msg.Payload, &sp); err != nil {
		t.Fatalf("unmarshal state payload: %v (double-encoding bug?)", err)
	}
	if sp.SessionInfo.Code != sess.Code {
		t.Fatalf("expected code %s, got %s — possible double-encoding", sess.Code, sp.SessionInfo.Code)
	}
	if sp.SessionInfo.Status != session.StatusPlaying {
		t.Fatalf("expected playing status, got %s", sp.SessionInfo.Status)
	}

	// Also verify state field is present and not a string (would be if double-encoded)
	stateBytes, err := json.Marshal(sp.State)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	// State should marshal to a JSON object, not a string
	if stateBytes[0] == '"' {
		t.Fatal("state is a JSON string — likely double-encoded")
	}
}

func TestWSGameCompletion(t *testing.T) {
	env := setupWSTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, _ := env.mgr.Create("tictactoe")
	sess.AddPlayer("alice")
	sess.AddPlayer("bob")
	sess.Start()

	// Get the match to determine player order
	sess.RLock()
	match := sess.Match
	sess.RUnlock()

	// Determine which player is X (goes first)
	firstPlayer := ""
	secondPlayer := ""
	for _, id := range sess.PlayerIDs() {
		actions := match.ValidActions(id)
		if len(actions) > 0 {
			firstPlayer = id
		} else {
			secondPlayer = id
		}
	}
	if firstPlayer == "" || secondPlayer == "" {
		t.Fatal("could not determine player order")
	}

	// Connect both players
	connFirst, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial first: %v", err)
	}
	defer connFirst.Close(websocket.StatusNormalClosure, "")

	connSecond, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer connSecond.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, connFirst, joinMsg(firstPlayer))
	wsRead(ctx, t, connFirst) // join state

	wsSend(ctx, t, connSecond, joinMsg(secondPlayer))
	// After second player reconnects, both get broadcast
	wsRead(ctx, t, connFirst)  // state broadcast from second join
	wsRead(ctx, t, connSecond) // state broadcast from second join

	sendMove := func(conn *websocket.Conn, cell int) {
		moveP, _ := json.Marshal(map[string]int{"cell": cell})
		actionData, _ := json.Marshal(actionPayload{Action: game.Action{Type: "move", Payload: moveP}})
		wsSend(ctx, t, conn, WSMessage{Type: "action", Payload: actionData})
	}

	// Play a winning game for first player (X wins):
	// X: 0, 1, 2 (top row)
	// O: 3, 4

	// Move 1: X plays cell 0
	sendMove(connFirst, 0)
	wsRead(ctx, t, connFirst)  // state
	wsRead(ctx, t, connSecond) // state

	// Move 2: O plays cell 3
	sendMove(connSecond, 3)
	wsRead(ctx, t, connFirst)  // state
	wsRead(ctx, t, connSecond) // state

	// Move 3: X plays cell 1
	sendMove(connFirst, 1)
	wsRead(ctx, t, connFirst)  // state
	wsRead(ctx, t, connSecond) // state

	// Move 4: O plays cell 4
	sendMove(connSecond, 4)
	wsRead(ctx, t, connFirst)  // state
	wsRead(ctx, t, connSecond) // state

	// Move 5: X plays cell 2 — wins!
	sendMove(connFirst, 2)

	// Read final state for first player
	finalMsg := wsRead(ctx, t, connFirst)
	if finalMsg.Type != "state" {
		t.Fatalf("expected state, got %s", finalMsg.Type)
	}

	var sp statePayload
	json.Unmarshal(finalMsg.Payload, &sp)

	if sp.Results == nil {
		t.Fatal("expected results in final state")
	}
	if len(sp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(sp.Results))
	}

	// Verify winner
	var winner game.PlayerResult
	for _, r := range sp.Results {
		if r.Rank == 1 && r.Score == 1 {
			winner = r
		}
	}
	if winner.PlayerID != firstPlayer {
		t.Fatalf("expected %s to win, got %s", firstPlayer, winner.PlayerID)
	}

	if sp.SessionInfo.Status != session.StatusFinished {
		t.Fatalf("expected finished status, got %s", sp.SessionInfo.Status)
	}
}
