package games

import (
	"io/fs"
	"testing"
)

func TestWebFSContainsFiles(t *testing.T) {
	files := []string{
		"web/index.html",
		"web/session.html",
		"web/css/style.css",
		"web/js/lobby.js",
		"web/js/session.js",
		"web/js/games/tictactoe.js",
	}
	for _, path := range files {
		data, err := fs.ReadFile(WebFS, path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("expected non-empty content for %s", path)
		}
	}
}

func TestWebFSSub(t *testing.T) {
	sub, err := fs.Sub(WebFS, "web")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	// Should be able to read index.html without the "web/" prefix
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) via Sub: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty index.html")
	}
}
