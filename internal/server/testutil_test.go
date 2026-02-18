package server

import (
	"context"
	"encoding/json"
	"fmt"
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

// --- Test environment ---

type testEnv struct {
	ts  *httptest.Server
	mgr *session.Manager
}

func setupTestEnv(t *testing.T) *testEnv {
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
		"index.html": &fstest.MapFile{Data: []byte("<html><body>test</body></html>")},
	}
	srv := New(reg, mgr, webFS)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return &testEnv{ts: ts, mgr: mgr}
}

// --- Context helpers ---

func timeoutCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// --- REST API helpers ---

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

// --- WebSocket helpers ---

func wsURL(ts *httptest.Server, code string) string {
	return strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/sessions/" + code + "/ws"
}

// wsConnect dials a WebSocket, sends a join message, and returns the connection.
// The caller is responsible for closing the connection.
func wsConnect(t *testing.T, ts *httptest.Server, code, playerID string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(ts, code), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	if err := sendWS(ctx, conn, "join", joinPayload{PlayerID: playerID}); err != nil {
		t.Fatalf("send join: %v", err)
	}
	return conn
}

// sendWS marshals and sends a typed WebSocket message. Returns an error on failure.
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

// readWS reads and unmarshals a single WebSocket message. Returns an error on failure.
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

// wsSend marshals and writes a pre-built WSMessage, calling t.Fatal on error.
func wsSend(ctx context.Context, t *testing.T, conn *websocket.Conn, msg WSMessage) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal ws message: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

// wsRead reads and unmarshals a WebSocket message, calling t.Fatal on error.
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

// joinMsg builds a WSMessage for a join request.
func joinMsg(playerID string) WSMessage {
	payload, _ := json.Marshal(joinPayload{PlayerID: playerID})
	return WSMessage{Type: "join", Payload: payload}
}

// readState reads a WebSocket message and expects it to be a "state" message.
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

// readError reads a WebSocket message and expects it to be an "error" message.
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

// --- Game helpers ---

// makeAction builds an actionPayload for a tic-tac-toe move.
func makeAction(t *testing.T, cell int) actionPayload {
	t.Helper()
	payload, err := json.Marshal(map[string]int{"cell": cell})
	if err != nil {
		t.Fatalf("marshal action payload: %v", err)
	}
	return actionPayload{
		Action: game.Action{Type: "move", Payload: payload},
	}
}

// stateMap extracts State from a statePayload as map[string]any, failing the test if
// the type assertion fails.
func stateMap(t *testing.T, sp statePayload) map[string]any {
	t.Helper()
	m, ok := sp.State.(map[string]any)
	if !ok {
		t.Fatalf("expected State to be map[string]any, got %T", sp.State)
	}
	return m
}

func containsPlayer(players []string, id string) bool {
	for _, p := range players {
		if p == id {
			return true
		}
	}
	return false
}
