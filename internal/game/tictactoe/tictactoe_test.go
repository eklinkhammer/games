package tictactoe

import (
	"encoding/json"
	"testing"

	"games/internal/game"
)

func newTestMatch() *Match {
	g := TicTacToe{}
	return g.NewMatch(game.MatchConfig{PlayerIDs: []string{"alice", "bob"}}).(*Match)
}

func makeMove(cell int) game.Action {
	payload, _ := json.Marshal(movePayload{Cell: cell})
	return game.Action{Type: "move", Payload: payload}
}

func TestNewMatch(t *testing.T) {
	m := newTestMatch()
	if m.Players[0] != "alice" || m.Players[1] != "bob" {
		t.Fatalf("unexpected players: %v", m.Players)
	}
	if m.Turn != 0 {
		t.Fatalf("expected turn 0, got %d", m.Turn)
	}
	if m.Done {
		t.Fatal("game should not be over")
	}
}

func TestValidActions(t *testing.T) {
	m := newTestMatch()
	actions := m.ValidActions("alice")
	if len(actions) != 9 {
		t.Fatalf("expected 9 actions, got %d", len(actions))
	}
	// Bob has no actions on alice's turn
	actions = m.ValidActions("bob")
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions for bob, got %d", len(actions))
	}
}

func TestApplyAction(t *testing.T) {
	m := newTestMatch()
	err := m.ApplyAction("alice", makeMove(4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Board[4] != 1 {
		t.Fatalf("expected X at cell 4, got %d", m.Board[4])
	}
	if m.Turn != 1 {
		t.Fatalf("expected turn 1, got %d", m.Turn)
	}
}

func TestWrongTurn(t *testing.T) {
	m := newTestMatch()
	err := m.ApplyAction("bob", makeMove(0))
	if err == nil {
		t.Fatal("expected error for wrong turn")
	}
}

func TestOccupiedCell(t *testing.T) {
	m := newTestMatch()
	m.ApplyAction("alice", makeMove(0))
	err := m.ApplyAction("bob", makeMove(0))
	if err == nil {
		t.Fatal("expected error for occupied cell")
	}
}

func TestWinDetection(t *testing.T) {
	m := newTestMatch()
	// Alice wins with top row: 0, 1, 2
	m.ApplyAction("alice", makeMove(0))
	m.ApplyAction("bob", makeMove(3))
	m.ApplyAction("alice", makeMove(1))
	m.ApplyAction("bob", makeMove(4))
	m.ApplyAction("alice", makeMove(2))

	if !m.IsOver() {
		t.Fatal("game should be over")
	}
	results := m.Results()
	if results[0].PlayerID != "alice" || results[0].Rank != 1 {
		t.Fatalf("expected alice to win, got %+v", results)
	}
	if results[1].PlayerID != "bob" || results[1].Rank != 2 {
		t.Fatalf("expected bob to lose, got %+v", results)
	}
}

func TestDraw(t *testing.T) {
	m := newTestMatch()
	// Fill board without a winner:
	// X O X
	// X X O
	// O X O
	moves := []struct {
		player string
		cell   int
	}{
		{"alice", 0}, {"bob", 1}, {"alice", 2},
		{"bob", 5}, {"alice", 3}, {"bob", 6},
		{"alice", 4}, {"bob", 8}, {"alice", 7},
	}
	for _, mv := range moves {
		err := m.ApplyAction(mv.player, makeMove(mv.cell))
		if err != nil {
			t.Fatalf("unexpected error at cell %d by %s: %v", mv.cell, mv.player, err)
		}
	}
	if !m.IsOver() {
		t.Fatal("game should be over")
	}
	results := m.Results()
	if results[0].Rank != 1 || results[1].Rank != 1 {
		t.Fatalf("expected draw (both rank 1), got %+v", results)
	}
}

func TestStateHidesNothing(t *testing.T) {
	// Tic-tac-toe has no hidden info, but State should still work
	m := newTestMatch()
	m.ApplyAction("alice", makeMove(4))
	state := m.State("alice").(stateView)
	if state.You != 1 {
		t.Fatalf("expected alice to be player 1 (X), got %d", state.You)
	}
	if state.Board[4] != 1 {
		t.Fatalf("expected X at cell 4")
	}
	state2 := m.State("bob").(stateView)
	if state2.You != 2 {
		t.Fatalf("expected bob to be player 2 (O), got %d", state2.You)
	}
}

func TestMarshalUnmarshal(t *testing.T) {
	m := newTestMatch()
	m.ApplyAction("alice", makeMove(0))
	m.ApplyAction("bob", makeMove(4))

	data, err := m.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	m2 := &Match{}
	if err := m2.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if m2.Board != m.Board || m2.Turn != m.Turn || m2.Players != m.Players {
		t.Fatalf("state mismatch after round-trip")
	}
}

func TestGameInfo(t *testing.T) {
	g := TicTacToe{}
	info := g.Info()
	if info.Name != "tictactoe" {
		t.Fatalf("expected name tictactoe, got %s", info.Name)
	}
	if info.MinPlayers != 2 || info.MaxPlayers != 2 {
		t.Fatalf("expected 2 players, got min=%d max=%d", info.MinPlayers, info.MaxPlayers)
	}
}

func TestActionAfterGameOver(t *testing.T) {
	m := newTestMatch()
	m.ApplyAction("alice", makeMove(0))
	m.ApplyAction("bob", makeMove(3))
	m.ApplyAction("alice", makeMove(1))
	m.ApplyAction("bob", makeMove(4))
	m.ApplyAction("alice", makeMove(2)) // alice wins

	err := m.ApplyAction("bob", makeMove(5))
	if err == nil {
		t.Fatal("expected error for action after game over")
	}

	actions := m.ValidActions("alice")
	if len(actions) != 0 {
		t.Fatal("expected no valid actions after game over")
	}
}
