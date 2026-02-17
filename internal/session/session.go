package session

import (
	"fmt"
	"sync"

	"games/internal/game"
)

// Status represents the session lifecycle.
type Status string

const (
	StatusWaiting  Status = "waiting"
	StatusPlaying  Status = "playing"
	StatusFinished Status = "finished"
)

// Player represents a connected player.
type Player struct {
	ID   string
	Send chan []byte // outbound messages
}

// Session is one game session with connected players.
type Session struct {
	mu       sync.RWMutex
	Code     string
	GameType string
	Status   Status
	HostID   string
	Players  map[string]*Player
	Match    game.Match
	game     game.Game
}

// NewSession creates a session in the waiting state.
func NewSession(code, gameType string, g game.Game) *Session {
	return &Session{
		Code:     code,
		GameType: gameType,
		Status:   StatusWaiting,
		Players:  make(map[string]*Player),
		game:     g,
	}
}

// AddPlayer adds a player to the session. Returns error if full or already playing.
func (s *Session) AddPlayer(playerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Status != StatusWaiting {
		return fmt.Errorf("session is not accepting players")
	}
	info := s.game.Info()
	if len(s.Players) >= info.MaxPlayers {
		return fmt.Errorf("session is full")
	}
	if _, exists := s.Players[playerID]; exists {
		return fmt.Errorf("player %s already in session", playerID)
	}
	s.Players[playerID] = &Player{
		ID:   playerID,
		Send: make(chan []byte, 64),
	}
	if s.HostID == "" {
		s.HostID = playerID
	}
	return nil
}

// RemovePlayer removes a player from the session.
func (s *Session) RemovePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.Players[playerID]; ok {
		close(p.Send)
		delete(s.Players, playerID)
	}
}

// ConnectPlayer replaces the Send channel for a reconnecting player.
func (s *Session) ConnectPlayer(playerID string, send chan []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.Players[playerID]
	if !ok {
		return false
	}
	p.Send = send
	return true
}

// PlayerIDs returns the list of player IDs.
func (s *Session) PlayerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.Players))
	for id := range s.Players {
		ids = append(ids, id)
	}
	return ids
}

// Start transitions the session from waiting to playing.
func (s *Session) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Status != StatusWaiting {
		return fmt.Errorf("session is not in waiting state")
	}
	info := s.game.Info()
	if len(s.Players) < info.MinPlayers {
		return fmt.Errorf("need at least %d players, have %d", info.MinPlayers, len(s.Players))
	}

	ids := make([]string, 0, len(s.Players))
	for id := range s.Players {
		ids = append(ids, id)
	}
	s.Match = s.game.NewMatch(game.MatchConfig{PlayerIDs: ids})
	s.Status = StatusPlaying
	return nil
}

// Finish marks the session as finished.
func (s *Session) Finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusFinished
}

// Broadcast sends a message to all connected players.
func (s *Session) Broadcast(msg []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.Players {
		select {
		case p.Send <- msg:
		default:
			// drop message if buffer full
		}
	}
}

// GetPlayer returns a player's send channel, or nil if not found.
func (s *Session) GetPlayer(playerID string) *Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Players[playerID]
}

// Info returns session info for the API.
type Info struct {
	Code     string   `json:"code"`
	GameType string   `json:"gameType"`
	Status   Status   `json:"status"`
	Players  []string `json:"players"`
	HostID   string   `json:"hostId"`
}

func (s *Session) Info() Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.infoLocked()
}

// InfoLocked returns info without acquiring the lock (caller must hold it).
func (s *Session) InfoLocked() Info {
	return s.infoLocked()
}

func (s *Session) infoLocked() Info {
	ids := make([]string, 0, len(s.Players))
	for id := range s.Players {
		ids = append(ids, id)
	}
	return Info{
		Code:     s.Code,
		GameType: s.GameType,
		Status:   s.Status,
		Players:  ids,
		HostID:   s.HostID,
	}
}

// Lock/RLock/Unlock/RUnlock expose the mutex for the server's websocket handler.
func (s *Session) Lock()    { s.mu.Lock() }
func (s *Session) Unlock()  { s.mu.Unlock() }
func (s *Session) RLock()   { s.mu.RLock() }
func (s *Session) RUnlock() { s.mu.RUnlock() }
