(function() {
    const gameSelect = document.getElementById("game-select");
    const createBtn = document.getElementById("create-btn");
    const joinBtn = document.getElementById("join-btn");
    const errorMsg = document.getElementById("error-msg");

    function showError(msg) {
        errorMsg.textContent = msg;
        errorMsg.hidden = false;
        setTimeout(() => errorMsg.hidden = true, 4000);
    }

    async function loadGames() {
        const resp = await fetch("/api/games");
        const games = await resp.json();
        gameSelect.innerHTML = "";
        games.forEach(g => {
            const opt = document.createElement("option");
            opt.value = g.name;
            opt.textContent = g.name + " (" + g.minPlayers + "-" + g.maxPlayers + " players)";
            gameSelect.appendChild(opt);
        });
    }

    createBtn.addEventListener("click", async () => {
        const name = document.getElementById("player-name").value.trim();
        const gameType = gameSelect.value;
        if (!name) { showError("Enter your name"); return; }

        const resp = await fetch("/api/sessions", {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({gameType: gameType, playerId: name})
        });
        const data = await resp.json();
        if (!resp.ok) { showError(data.error); return; }

        window.location.href = "/session.html?code=" + data.code + "&player=" + encodeURIComponent(name);
    });

    joinBtn.addEventListener("click", () => {
        const name = document.getElementById("join-name").value.trim();
        const code = document.getElementById("join-code").value.trim();
        if (!name) { showError("Enter your name"); return; }
        if (!code) { showError("Enter session code"); return; }

        window.location.href = "/session.html?code=" + code + "&player=" + encodeURIComponent(name);
    });

    loadGames();
})();
