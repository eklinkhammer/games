package game

import (
	"fmt"
	"sync"
)

// Registry holds all registered game types.
type Registry struct {
	mu    sync.RWMutex
	games map[string]Game
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{games: make(map[string]Game)}
}

// Register adds a game type. Panics on duplicate names.
func (r *Registry) Register(g Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := g.Info().Name
	if _, exists := r.games[name]; exists {
		panic(fmt.Sprintf("game %q already registered", name))
	}
	r.games[name] = g
}

// Get returns a game by name.
func (r *Registry) Get(name string) (Game, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.games[name]
	return g, ok
}

// List returns info for all registered games.
func (r *Registry) List() []GameInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]GameInfo, 0, len(r.games))
	for _, g := range r.games {
		infos = append(infos, g.Info())
	}
	return infos
}
