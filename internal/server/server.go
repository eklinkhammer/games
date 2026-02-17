package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"games/internal/game"
	"games/internal/session"
)

// Server is the HTTP server.
type Server struct {
	mux      *http.ServeMux
	registry *game.Registry
	manager  *session.Manager
	webFS    fs.FS
}

// New creates a server with all routes.
// webFS should be the "web" subdirectory of the embedded filesystem.
func New(registry *game.Registry, manager *session.Manager, webFS fs.FS) *Server {
	s := &Server{
		mux:      http.NewServeMux(),
		registry: registry,
		manager:  manager,
		webFS:    webFS,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// API routes
	s.mux.HandleFunc("GET /api/games", s.handleListGames)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions/{code}", s.handleGetSession)
	s.mux.HandleFunc("GET /api/sessions/{code}/ws", s.handleWebSocket)
	s.mux.HandleFunc("POST /api/sessions/{code}/start", s.handleStartSession)

	// Static files
	s.mux.Handle("/", http.FileServer(http.FS(s.webFS)))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.registry.List())
}

type createSessionRequest struct {
	GameType string `json:"gameType"`
	PlayerID string `json:"playerId"`
}

type createSessionResponse struct {
	Code string `json:"code"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.GameType = strings.TrimSpace(req.GameType)
	req.PlayerID = strings.TrimSpace(req.PlayerID)
	if req.GameType == "" || req.PlayerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gameType and playerId required"})
		return
	}

	sess, err := s.manager.Create(req.GameType)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := sess.AddPlayer(req.PlayerID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, createSessionResponse{Code: sess.Code})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sess, ok := s.manager.Get(code)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, sess.Info())
}

func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sess, ok := s.manager.Get(code)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if err := sess.Start(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.manager.SaveMatchState(sess); err != nil {
		log.Printf("save match state: %v", err)
	}
	// Broadcast new state to all players
	s.broadcastState(sess)
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
