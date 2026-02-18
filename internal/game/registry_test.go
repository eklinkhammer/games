package game

import (
	"encoding/json"
	"testing"
)

// stubGame is a minimal Game implementation for testing the registry.
type stubGame struct {
	name       string
	minPlayers int
	maxPlayers int
}

func (s stubGame) Info() GameInfo {
	return GameInfo{Name: s.name, MinPlayers: s.minPlayers, MaxPlayers: s.maxPlayers}
}

func (s stubGame) NewMatch(config MatchConfig) Match {
	return &stubMatch{}
}

// stubMatch is a minimal Match implementation.
type stubMatch struct{}

func (m *stubMatch) State(playerID string) any              { return nil }
func (m *stubMatch) ValidActions(playerID string) []Action   { return nil }
func (m *stubMatch) ApplyAction(string, Action) error        { return nil }
func (m *stubMatch) IsOver() bool                            { return false }
func (m *stubMatch) Results() []PlayerResult                 { return nil }
func (m *stubMatch) MarshalJSON() ([]byte, error)            { return json.Marshal(struct{}{}) }
func (m *stubMatch) UnmarshalJSON(data []byte) error         { return nil }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	g := stubGame{name: "test", minPlayers: 2, maxPlayers: 4}
	r.Register(g)

	got, ok := r.Get("test")
	if !ok {
		t.Fatal("expected to find registered game")
	}
	if got.Info().Name != "test" {
		t.Fatalf("expected name test, got %s", got.Info().Name)
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("expected not found for unregistered game")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(stubGame{name: "a", minPlayers: 1, maxPlayers: 2})
	r.Register(stubGame{name: "b", minPlayers: 2, maxPlayers: 4})

	infos := r.List()
	if len(infos) != 2 {
		t.Fatalf("expected 2 games, got %d", len(infos))
	}

	names := map[string]bool{}
	for _, info := range infos {
		names[info.Name] = true
	}
	if !names["a"] || !names["b"] {
		t.Fatalf("expected games a and b, got %v", names)
	}
}

func TestRegistryListEmpty(t *testing.T) {
	r := NewRegistry()
	infos := r.List()
	if len(infos) != 0 {
		t.Fatalf("expected 0 games, got %d", len(infos))
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	g := stubGame{name: "test", minPlayers: 2, maxPlayers: 4}
	r.Register(g)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.Register(g) // should panic
}
