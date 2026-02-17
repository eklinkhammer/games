package main

import (
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	games "games"
	"games/internal/game"
	"games/internal/game/tictactoe"
	"games/internal/server"
	"games/internal/session"
	"games/internal/storage"
)

func main() {
	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	dbPath := "games.db"
	if p := os.Getenv("DB_PATH"); p != "" {
		dbPath = p
	}

	store, err := storage.New(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	registry := game.NewRegistry()
	registry.Register(tictactoe.TicTacToe{})

	mgr := session.NewManager(registry, store)
	if err := mgr.Restore(); err != nil {
		log.Printf("warning: restore sessions: %v", err)
	}

	go mgr.CleanupLoop(1*time.Minute, 1*time.Hour)

	webFS, err := fs.Sub(games.WebFS, "web")
	if err != nil {
		log.Fatalf("web fs: %v", err)
	}
	srv := server.New(registry, mgr, webFS)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}
