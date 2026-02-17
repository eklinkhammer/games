package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"games/internal/game"
	"games/internal/storage"
)

// Manager manages all active sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	registry *game.Registry
	store    *storage.Store
}

// NewManager creates a session manager.
func NewManager(registry *game.Registry, store *storage.Store) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		registry: registry,
		store:    store,
	}
}

// Create makes a new session and persists it.
func (m *Manager) Create(gameType string) (*Session, error) {
	g, ok := m.registry.Get(gameType)
	if !ok {
		return nil, fmt.Errorf("unknown game type: %s", gameType)
	}
	code := generateCode()
	if err := m.store.CreateSession(code, gameType); err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}
	s := NewSession(code, gameType, g)
	m.mu.Lock()
	m.sessions[code] = s
	m.mu.Unlock()
	return s, nil
}

// Get returns a session by code.
func (m *Manager) Get(code string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[code]
	return s, ok
}

// List returns info for all active sessions.
func (m *Manager) List() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

// SaveMatchState persists the current match state for a session.
func (m *Manager) SaveMatchState(s *Session) error {
	s.mu.RLock()
	match := s.Match
	status := s.Status
	s.mu.RUnlock()

	if err := m.store.UpdateSessionStatus(s.Code, string(status)); err != nil {
		return err
	}
	if match == nil {
		return nil
	}
	data, err := match.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal match state: %w", err)
	}
	return m.store.SaveMatchState(s.Code, string(data))
}

// Restore loads sessions from the database on startup.
func (m *Manager) Restore() error {
	rows, err := m.store.ListSessions("")
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	for _, row := range rows {
		if row.Status == "finished" {
			continue
		}
		g, ok := m.registry.Get(row.GameType)
		if !ok {
			log.Printf("skipping session %s: unknown game type %s", row.Code, row.GameType)
			continue
		}
		s := NewSession(row.Code, row.GameType, g)
		s.Status = Status(row.Status)

		if row.Status == "playing" {
			stateJSON, err := m.store.GetMatchState(row.Code)
			if err != nil {
				log.Printf("skipping session %s: no match state: %v", row.Code, err)
				continue
			}
			match := g.NewMatch(game.MatchConfig{PlayerIDs: []string{"_", "_"}})
			if err := match.UnmarshalJSON([]byte(stateJSON)); err != nil {
				log.Printf("skipping session %s: unmarshal error: %v", row.Code, err)
				continue
			}
			s.Match = match
		}
		m.mu.Lock()
		m.sessions[row.Code] = s
		m.mu.Unlock()
	}
	return nil
}

// Remove deletes a session from memory and storage.
func (m *Manager) Remove(code string) {
	m.mu.Lock()
	delete(m.sessions, code)
	m.mu.Unlock()
	m.store.DeleteSession(code)
}

// CleanupLoop removes stale sessions periodically.
func (m *Manager) CleanupLoop(interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.cleanup(maxAge)
	}
}

func (m *Manager) cleanup(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for code, s := range m.sessions {
		s.mu.RLock()
		empty := len(s.Players) == 0
		finished := s.Status == StatusFinished
		s.mu.RUnlock()

		if finished || empty {
			row, err := m.store.GetSession(code)
			if err != nil {
				delete(m.sessions, code)
				continue
			}
			if now.Sub(row.CreatedAt) > maxAge || empty {
				log.Printf("cleaning up session %s", code)
				m.store.DeleteSession(code)
				delete(m.sessions, code)
			}
		}
	}
}

func generateCode() string {
	b := make([]byte, 3) // 6 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}

// MarshalSessionPlayers is a helper for persisting player list with match state.
type sessionSnapshot struct {
	Players []string `json:"players"`
	HostID  string   `json:"hostId"`
}

func (m *Manager) SaveSessionPlayers(s *Session) error {
	s.mu.RLock()
	snap := sessionSnapshot{
		Players: make([]string, 0, len(s.Players)),
		HostID:  s.HostID,
	}
	for id := range s.Players {
		snap.Players = append(snap.Players, id)
	}
	s.mu.RUnlock()
	data, _ := json.Marshal(snap)
	return m.store.SaveMatchState(s.Code+"_players", string(data))
}
