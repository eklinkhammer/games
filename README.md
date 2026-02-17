# Games

A real-time multiplayer game platform built with Go and vanilla JavaScript. Players can create and join game sessions using shareable codes and play via WebSocket.

Currently includes **Tic-Tac-Toe**, with an extensible architecture for adding new games.

## Running

```bash
go run ./cmd/server/main.go
```

Then open http://localhost:8080.

### Environment Variables

| Variable  | Default    | Description          |
|-----------|------------|----------------------|
| `PORT`    | `8080`     | Server port          |
| `DB_PATH` | `games.db` | SQLite database path |

## Project Structure

```
cmd/server/main.go          # Entry point
internal/
  game/                     # Game interfaces and registry
    tictactoe/              # Tic-Tac-Toe implementation
  server/                   # HTTP server and WebSocket handler
  session/                  # Session state and lifecycle management
  storage/                  # SQLite persistence
web/                        # Frontend (HTML, CSS, vanilla JS)
```

## Adding a New Game

1. Implement the `Game` and `Match` interfaces from `internal/game/game.go`
2. Register the game in `cmd/server/main.go`
3. Add a frontend renderer in `web/js/games/`
