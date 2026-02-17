package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"nhooyr.io/websocket"

	"games/internal/game"
	"games/internal/session"
)

// WSMessage is the JSON envelope for WebSocket messages.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type joinPayload struct {
	PlayerID string `json:"playerId"`
}

type actionPayload struct {
	Action game.Action `json:"action"`
}

type statePayload struct {
	State        any                 `json:"state"`
	ValidActions []game.Action       `json:"validActions"`
	SessionInfo  session.Info        `json:"sessionInfo"`
	Results      []game.PlayerResult `json:"results,omitempty"`
}

type errorPayload struct {
	Message string `json:"message"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sess, ok := s.manager.Get(code)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // allow any origin for dev
	})
	if err != nil {
		log.Printf("websocket accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// First message must be a join
	_, data, err := conn.Read(ctx)
	if err != nil {
		return
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "join" {
		sendWSError(ctx, conn, "first message must be a join")
		return
	}
	var join joinPayload
	if err := json.Unmarshal(msg.Payload, &join); err != nil || join.PlayerID == "" {
		sendWSError(ctx, conn, "invalid join payload")
		return
	}

	playerID := join.PlayerID
	send := make(chan []byte, 64)

	// Try to reconnect existing player, or add new one
	if !sess.ConnectPlayer(playerID, send) {
		if err := sess.AddPlayer(playerID); err != nil {
			sendWSError(ctx, conn, err.Error())
			return
		}
		sess.ConnectPlayer(playerID, send)
	}

	// Notify all players about the roster change
	s.broadcastState(sess)

	// Writer goroutine: send messages from the channel to the websocket
	go func() {
		for msg := range send {
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		}
	}()

	// Reader loop: handle incoming messages
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			sendWSMsg(send, "error", errorPayload{Message: "invalid message"})
			continue
		}
		s.handleMessage(sess, playerID, send, msg)
	}

	// Player disconnected — don't remove, allow reconnect
	log.Printf("player %s disconnected from session %s", playerID, code)
}

func (s *Server) handleMessage(sess *session.Session, playerID string, send chan []byte, msg WSMessage) {
	switch msg.Type {
	case "action":
		var ap actionPayload
		if err := json.Unmarshal(msg.Payload, &ap); err != nil {
			sendWSMsg(send, "error", errorPayload{Message: "invalid action payload"})
			return
		}
		sess.Lock()
		if sess.Match == nil {
			sess.Unlock()
			sendWSMsg(send, "error", errorPayload{Message: "game not started"})
			return
		}
		if err := sess.Match.ApplyAction(playerID, ap.Action); err != nil {
			sess.Unlock()
			sendWSMsg(send, "error", errorPayload{Message: err.Error()})
			return
		}
		if sess.Match.IsOver() {
			sess.Status = session.StatusFinished
		}
		sess.Unlock()

		if err := s.manager.SaveMatchState(sess); err != nil {
			log.Printf("save match state: %v", err)
		}
		s.broadcastState(sess)

	case "start":
		if sess.Info().HostID != playerID {
			sendWSMsg(send, "error", errorPayload{Message: "only the host can start"})
			return
		}
		if err := sess.Start(); err != nil {
			sendWSMsg(send, "error", errorPayload{Message: err.Error()})
			return
		}
		if err := s.manager.SaveMatchState(sess); err != nil {
			log.Printf("save match state: %v", err)
		}
		s.broadcastState(sess)

	default:
		sendWSMsg(send, "error", errorPayload{Message: "unknown message type: " + msg.Type})
	}
}

func (s *Server) broadcastState(sess *session.Session) {
	sess.RLock()
	info := sess.InfoLocked()
	match := sess.Match
	status := sess.Status
	sess.RUnlock()

	for _, pid := range info.Players {
		p := sess.GetPlayer(pid)
		if p == nil {
			continue
		}
		sp := statePayload{SessionInfo: info}
		if match != nil && status != session.StatusWaiting {
			sp.State = match.State(pid)
			sp.ValidActions = match.ValidActions(pid)
			if match.IsOver() {
				sp.Results = match.Results()
			}
		}
		sendWSMsg(p.Send, "state", sp)
	}
}

func sendWSMsg(send chan []byte, msgType string, payload any) {
	p, _ := json.Marshal(payload)
	msg, _ := json.Marshal(WSMessage{Type: msgType, Payload: p})
	select {
	case send <- msg:
	default:
	}
}

func sendWSError(ctx context.Context, conn *websocket.Conn, message string) {
	p, _ := json.Marshal(errorPayload{Message: message})
	msg, _ := json.Marshal(WSMessage{Type: "error", Payload: p})
	conn.Write(ctx, websocket.MessageText, msg)
}
