(function() {
    const params = new URLSearchParams(window.location.search);
    const code = params.get("code");
    const playerID = params.get("player");

    if (!code || !playerID) {
        window.location.href = "/";
        return;
    }

    document.getElementById("session-code").textContent = code;

    const errorMsg = document.getElementById("error-msg");
    const startBtn = document.getElementById("start-btn");
    const gameArea = document.getElementById("game-area");
    const resultsDiv = document.getElementById("results");

    function showError(msg) {
        errorMsg.textContent = msg;
        errorMsg.hidden = false;
        setTimeout(() => errorMsg.hidden = true, 4000);
    }

    // Game renderers keyed by game type name
    const renderers = {
        tictactoe: window.TicTacToeRenderer
    };

    let ws;
    let currentRenderer = null;

    function connect() {
        const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
        ws = new WebSocket(proto + "//" + window.location.host + "/api/sessions/" + code + "/ws");

        ws.onopen = () => {
            ws.send(JSON.stringify({type: "join", payload: JSON.stringify({playerId: playerID})}));
        };

        ws.onmessage = (evt) => {
            const msg = JSON.parse(evt.data);
            if (msg.type === "error") {
                const payload = JSON.parse(msg.payload);
                showError(payload.message);
                return;
            }
            if (msg.type === "state") {
                const payload = JSON.parse(msg.payload);
                handleState(payload);
            }
        };

        ws.onclose = () => {
            setTimeout(connect, 2000);
        };
    }

    function handleState(payload) {
        const info = payload.sessionInfo;
        document.getElementById("session-status").textContent = info.status;
        document.getElementById("game-title").textContent = info.gameType;

        // Update player list
        const playersList = document.getElementById("players");
        playersList.innerHTML = "";
        info.players.forEach(p => {
            const li = document.createElement("li");
            li.textContent = p + (p === info.hostId ? " (host)" : "") + (p === playerID ? " (you)" : "");
            playersList.appendChild(li);
        });

        // Show start button for host in waiting state
        startBtn.hidden = !(info.status === "waiting" && info.hostId === playerID);

        if (info.status === "playing" || info.status === "finished") {
            document.getElementById("players-list").hidden = true;
            gameArea.hidden = false;

            // Initialize renderer if needed
            if (!currentRenderer && renderers[info.gameType]) {
                currentRenderer = renderers[info.gameType];
                currentRenderer.init(document.getElementById("game-board"), sendAction);
            }

            if (currentRenderer && payload.state) {
                currentRenderer.render(payload.state, payload.validActions || []);
            }

            // Show status
            const statusEl = document.getElementById("game-status");
            if (payload.state && !payload.state.done) {
                if (payload.state.turn === playerID) {
                    statusEl.textContent = "Your turn!";
                } else {
                    statusEl.textContent = payload.state.turn + "'s turn";
                }
            }

            // Show results
            if (payload.results && payload.results.length > 0) {
                gameArea.hidden = true;
                resultsDiv.hidden = false;
                const list = document.getElementById("results-list");
                list.innerHTML = "";
                payload.results.forEach(r => {
                    const row = document.createElement("div");
                    row.className = "result-row";
                    row.innerHTML = "<span>" + r.playerId + "</span><span>Rank #" + r.rank + "</span>";
                    list.appendChild(row);
                });
            }
        }
    }

    function sendAction(action) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({
                type: "action",
                payload: JSON.stringify({action: action})
            }));
        }
    }

    startBtn.addEventListener("click", () => {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({type: "start", payload: "{}"}));
        }
    });

    connect();
})();
