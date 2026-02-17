package game

import "encoding/json"

// GameInfo describes a game type for the lobby.
type GameInfo struct {
	Name       string `json:"name"`
	MinPlayers int    `json:"minPlayers"`
	MaxPlayers int    `json:"maxPlayers"`
}

// MatchConfig holds settings for creating a new match.
type MatchConfig struct {
	PlayerIDs []string
}

// Action represents a move a player can make.
type Action struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// PlayerResult holds the outcome for one player.
type PlayerResult struct {
	PlayerID string `json:"playerId"`
	Rank     int    `json:"rank"`   // 1 = first place
	Score    int    `json:"score"`
}

// Game describes a game type (chess, poker, etc.)
type Game interface {
	Info() GameInfo
	NewMatch(config MatchConfig) Match
}

// Match is one in-progress game session.
type Match interface {
	State(playerID string) any
	ValidActions(playerID string) []Action
	ApplyAction(playerID string, action Action) error
	IsOver() bool
	Results() []PlayerResult
	// MarshalJSON / UnmarshalJSON support for persistence
	MarshalJSON() ([]byte, error)
	UnmarshalJSON(data []byte) error
}
