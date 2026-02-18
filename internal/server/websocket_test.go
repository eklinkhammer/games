package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"games/internal/game"
)

func TestWSJoinNewPlayer(t *testing.T) {
	env := setupTestEnv(t)
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
	env := setupTestEnv(t)
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
	env := setupTestEnv(t)
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
	env := setupTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL(env.ts, "nonexistent"), nil)
	if err == nil {
		t.Fatal("expected dial to fail for unknown session")
	}
	if resp == nil {
		// Dial failed without an HTTP response — acceptable (connection refused, etc.)
		return
	}
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWSActionValid(t *testing.T) {
	env := setupTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := env.mgr.Create("tictactoe")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := sess.AddPlayer("alice"); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := sess.AddPlayer("bob"); err != nil {
		t.Fatalf("add bob: %v", err)
	}
	if err := sess.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Connect both players
	connAlice, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial alice: %v", err)
	}
	defer connAlice.Close(websocket.StatusNormalClosure, "")

	connBob, _, err := websocket.Dial(ctx, wsURL(env.ts, sess.Code), nil)
	if err != nil {
		t.Fatalf("dial bob: %v", err)
	}
	defer connBob.Close(websocket.StatusNormalClosure, "")

	wsSend(ctx, t, connAlice, joinMsg("alice"))
	aliceState := wsRead(ctx, t, connAlice)

	wsSend(ctx, t, connBob, joinMsg("bob"))
	wsRead(ctx, t, connAlice) // broadcast from bob's join
	bobState := wsRead(ctx, t, connBob)

	// Determine who has valid actions (goes first)
	var aliceSP, bobSP statePayload
	if err := json.Unmarshal(aliceState.Payload, &aliceSP); err != nil {
		t.Fatalf("unmarshal alice state: %v", err)
	}
	if err := json.Unmarshal(bobState.Payload, &bobSP); err != nil {
		t.Fatalf("unmarshal bob state: %v", err)
	}

	// Pick the connection whose player has valid actions
	var activeConn *websocket.Conn
	if len(aliceSP.ValidActions) > 0 {
		activeConn = connAlice
	} else {
		activeConn = connBob
	}

	// Send a move action from the active player
	movePayload, _ := json.Marshal(map[string]int{"cell": 0})
	actionData, _ := json.Marshal(actionPayload{Action: game.Action{Type: "move", Payload: movePayload}})
	wsSend(ctx, t, activeConn, WSMessage{Type: "action", Payload: actionData})

	// Read the state update
	msg := wsRead(ctx, t, activeConn)
	if msg.Type != "state" {
		t.Fatalf("expected state after action, got %s", msg.Type)
	}
}

func TestWSActionGameNotStarted(t *testing.T) {
	env := setupTestEnv(t)
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
	if err := json.Unmarshal(msg.Payload, &ep); err != nil {
		t.Fatalf("unmarshal error payload: %v", err)
	}
	if ep.Message != "game not started" {
		t.Fatalf("expected 'game not started', got %q", ep.Message)
	}
}

func TestWSStartByNonHost(t *testing.T) {
	env := setupTestEnv(t)
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
	if err := json.Unmarshal(msg.Payload, &ep); err != nil {
		t.Fatalf("unmarshal error payload: %v", err)
	}
	if !strings.Contains(ep.Message, "host") {
		t.Fatalf("expected host-related error, got %q", ep.Message)
	}
}

func TestWSUnknownMessageType(t *testing.T) {
	env := setupTestEnv(t)
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
	if err := json.Unmarshal(msg.Payload, &ep); err != nil {
		t.Fatalf("unmarshal error payload: %v", err)
	}
	if !strings.Contains(ep.Message, "unknown") {
		t.Fatalf("expected 'unknown' in error message, got %q", ep.Message)
	}
}

