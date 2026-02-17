package tictactoe

import (
	"encoding/json"
	"fmt"

	"games/internal/game"
)

// TicTacToe implements game.Game.
type TicTacToe struct{}

func (t TicTacToe) Info() game.GameInfo {
	return game.GameInfo{
		Name:       "tictactoe",
		MinPlayers: 2,
		MaxPlayers: 2,
	}
}

func (t TicTacToe) NewMatch(config game.MatchConfig) game.Match {
	m := &Match{
		Players: [2]string{config.PlayerIDs[0], config.PlayerIDs[1]},
		Board:   [9]int{},
		Turn:    0,
	}
	return m
}

// Match implements game.Match for tic-tac-toe.
type Match struct {
	Players [2]string `json:"players"`
	Board   [9]int    `json:"board"` // 0=empty, 1=player0(X), 2=player1(O)
	Turn    int       `json:"turn"`  // index into Players
	Done    bool      `json:"done"`
	Winner  int       `json:"winner"` // -1=draw, 0 or 1=winner index
}

type stateView struct {
	Board   [9]int   `json:"board"`
	Turn    string   `json:"turn"`
	You     int      `json:"you"`    // 1=X, 2=O
	Players []string `json:"players"`
	Done    bool     `json:"done"`
	Winner  string   `json:"winner,omitempty"`
}

func (m *Match) State(playerID string) any {
	you := 0
	if playerID == m.Players[1] {
		you = 1
	}
	view := stateView{
		Board:   m.Board,
		Turn:    m.Players[m.Turn],
		You:     you + 1,
		Players: m.Players[:],
		Done:    m.Done,
	}
	if m.Done {
		if m.Winner == -1 {
			view.Winner = "draw"
		} else {
			view.Winner = m.Players[m.Winner]
		}
	}
	return view
}

type movePayload struct {
	Cell int `json:"cell"`
}

func (m *Match) ValidActions(playerID string) []game.Action {
	if m.Done {
		return nil
	}
	if playerID != m.Players[m.Turn] {
		return nil
	}
	var actions []game.Action
	for i, v := range m.Board {
		if v == 0 {
			payload, _ := json.Marshal(movePayload{Cell: i})
			actions = append(actions, game.Action{
				Type:    "move",
				Payload: payload,
			})
		}
	}
	return actions
}

func (m *Match) ApplyAction(playerID string, action game.Action) error {
	if m.Done {
		return fmt.Errorf("game is over")
	}
	if playerID != m.Players[m.Turn] {
		return fmt.Errorf("not your turn")
	}
	if action.Type != "move" {
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
	var move movePayload
	if err := json.Unmarshal(action.Payload, &move); err != nil {
		return fmt.Errorf("invalid move payload: %w", err)
	}
	if move.Cell < 0 || move.Cell > 8 {
		return fmt.Errorf("cell %d out of range", move.Cell)
	}
	if m.Board[move.Cell] != 0 {
		return fmt.Errorf("cell %d already occupied", move.Cell)
	}

	m.Board[move.Cell] = m.Turn + 1 // 1 for X, 2 for O
	if m.checkWin(m.Turn + 1) {
		m.Done = true
		m.Winner = m.Turn
	} else if m.boardFull() {
		m.Done = true
		m.Winner = -1
	} else {
		m.Turn = 1 - m.Turn
	}
	return nil
}

func (m *Match) IsOver() bool {
	return m.Done
}

func (m *Match) Results() []game.PlayerResult {
	if !m.Done {
		return nil
	}
	if m.Winner == -1 {
		return []game.PlayerResult{
			{PlayerID: m.Players[0], Rank: 1, Score: 0},
			{PlayerID: m.Players[1], Rank: 1, Score: 0},
		}
	}
	loser := 1 - m.Winner
	return []game.PlayerResult{
		{PlayerID: m.Players[m.Winner], Rank: 1, Score: 1},
		{PlayerID: m.Players[loser], Rank: 2, Score: 0},
	}
}

func (m *Match) MarshalJSON() ([]byte, error) {
	type alias Match
	return json.Marshal((*alias)(m))
}

func (m *Match) UnmarshalJSON(data []byte) error {
	type alias Match
	return json.Unmarshal(data, (*alias)(m))
}

var winLines = [][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, // rows
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8}, // cols
	{0, 4, 8}, {2, 4, 6}, // diags
}

func (m *Match) checkWin(mark int) bool {
	for _, line := range winLines {
		if m.Board[line[0]] == mark && m.Board[line[1]] == mark && m.Board[line[2]] == mark {
			return true
		}
	}
	return false
}

func (m *Match) boardFull() bool {
	for _, v := range m.Board {
		if v == 0 {
			return false
		}
	}
	return true
}
