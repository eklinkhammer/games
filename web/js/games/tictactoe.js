window.TicTacToeRenderer = (function() {
    let boardEl = null;
    let onAction = null;
    const marks = ["", "X", "O"];

    function init(container, actionCallback) {
        boardEl = container;
        onAction = actionCallback;
    }

    function render(state, validActions) {
        boardEl.innerHTML = "";
        const grid = document.createElement("div");
        grid.className = "ttt-board";

        const validCells = new Set();
        validActions.forEach(a => {
            if (a.payload && a.payload.cell !== undefined) {
                validCells.add(a.payload.cell);
            }
        });

        for (let i = 0; i < 9; i++) {
            const cell = document.createElement("div");
            cell.className = "ttt-cell";
            const val = state.board[i];
            cell.textContent = marks[val];
            if (val === 1) cell.classList.add("x");
            if (val === 2) cell.classList.add("o");

            if (validCells.has(i)) {
                cell.addEventListener("click", () => {
                    onAction({type: "move", payload: {cell: i}});
                });
            } else {
                cell.classList.add("disabled");
            }
            grid.appendChild(cell);
        }
        boardEl.appendChild(grid);
    }

    return {init, render};
})();
