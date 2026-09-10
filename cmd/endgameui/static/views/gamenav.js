// Quackle-style game log: once a session is loaded from an actual played
// game (GCG file/Woogles/cross-tables/URL), this renders the full move
// list as a two-column (one column per player) round-by-round table, with
// Prev/Next stepping and click-to-jump on any move — independent of the
// solve-tree exploration you build on top of whichever turn you land on.
const GameNav = (() => {
  // Event types that describe something happening to the *previous* move
  // rather than a move of their own (a challenged-off phony, a challenge
  // bonus, end-of-game tile penalties/bonuses) — folded into that move's
  // cell instead of getting their own row, matching Quackle's
  // "[Challenged Off]" style annotation.
  const ATTACHED_TYPES = new Set([
    'PhonyTilesReturned', 'ChallengeBonus', 'EndgameTiles',
    'LostTileScore', 'LostScoreOnTime',
  ]);

  let sessionId = null;
  let rawEntries = [];
  let rounds = []; // [{ turn: [p0turn|null, p1turn|null], cell: [html|null, html|null], scores }]
  let currentTurn = 0; // 0 = start of game, before any moves
  let playerNames = ['Player 1', 'Player 2'];

  async function init(sid) {
    sessionId = sid;
    currentTurn = 0;
    const content = document.getElementById('ht-content');
    content.innerHTML = `<div style="font-size:12px;color:var(--text-dim)">Loading game log…</div>`;
    try {
      const res = await Api.gameTranscript(sid);
      rawEntries = res.entries || [];
      playerNames = App.currentPlayerNames();
      buildRounds();
      render();
    } catch (e) {
      App.showError(e.message);
    }
  }

  function hide() {
    sessionId = null;
    rawEntries = [];
    rounds = [];
  }

  function buildRounds() {
    // Pass 1: fold attached events into the preceding same-player entry.
    const merged = [];
    for (const e of rawEntries) {
      const prev = merged[merged.length - 1];
      if (prev && prev.player === e.player && ATTACHED_TYPES.has(e.move.action)) {
        prev.attached.push(e);
      } else {
        merged.push({ ...e, attached: [] });
      }
    }
    // Pass 2: pair consecutive entries into rounds, one column per player.
    rounds = [];
    let i = 0;
    while (i < merged.length) {
      const a = merged[i];
      const b = merged[i + 1] && merged[i + 1].player !== a.player ? merged[i + 1] : null;
      const row = { cells: [null, null], turns: [null, null], scores: a.scores };
      row.cells[a.player] = cellHtml(a);
      row.turns[a.player] = a.turn;
      if (b) {
        row.cells[b.player] = cellHtml(b);
        row.turns[b.player] = b.turn;
        row.scores = b.scores;
        i += 2;
      } else {
        i += 1;
      }
      rounds.push(row);
    }
  }

  function cellHtml(e) {
    let text = escapeHtml(e.move.description);
    for (const a of e.attached) {
      text += ` <span class="attached">[${escapeHtml(shortAttachedLabel(a.move))}]</span>`;
    }
    return text;
  }

  function shortAttachedLabel(m) {
    if (m.action === 'PhonyTilesReturned') return 'Challenged off';
    return m.description;
  }

  function render() {
    const content = document.getElementById('ht-content');
    const maxTurn = rawEntries.length;
    content.innerHTML = `
      <div class="game-nav-bar">
        <button class="small secondary" id="gn-first" title="Start of game">⏮</button>
        <button class="small secondary" id="gn-prev" title="Previous turn (←)">◀</button>
        <span class="turn-indicator">${currentTurn} / ${maxTurn}</span>
        <button class="small secondary" id="gn-next" title="Next turn (→)">▶</button>
        <button class="small secondary" id="gn-last" title="End of game">⏭</button>
      </div>
      <div style="overflow-x:auto">
        <table class="game-log-table">
          <thead><tr><th class="turn-col">#</th><th>${escapeHtml(playerNames[0])}</th><th>${escapeHtml(playerNames[1])}</th></tr></thead>
          <tbody id="gn-rows"></tbody>
        </table>
      </div>
    `;
    document.getElementById('gn-first').addEventListener('click', () => gotoTurn(0));
    document.getElementById('gn-prev').addEventListener('click', () => gotoTurn(currentTurn - 1));
    document.getElementById('gn-next').addEventListener('click', () => gotoTurn(currentTurn + 1));
    document.getElementById('gn-last').addEventListener('click', () => gotoTurn(maxTurn));

    const rowsEl = document.getElementById('gn-rows');
    rounds.forEach((row, idx) => {
      const tr = document.createElement('tr');
      // A move's own row highlights when we're sitting at the position it
      // was played from (currentTurn is "events played so far", so the
      // move about to be made next is currentTurn + 1).
      const isCurrentRow = row.turns.includes(currentTurn + 1);
      if (isCurrentRow) tr.className = 'current';
      const cell = (p) => {
        if (row.cells[p] === null) return '<td></td>';
        return `<td data-turn="${row.turns[p]}">${row.cells[p]}</td>`;
      };
      tr.innerHTML = `<td class="turn-col">${idx + 1}</td>${cell(0)}${cell(1)}`;
      tr.querySelectorAll('td[data-turn]').forEach((td) => {
        // Jump to the position *before* this move (the rack it was made
        // from), not the position right after it.
        td.addEventListener('click', () => gotoTurn(parseInt(td.dataset.turn, 10) - 1));
      });
      rowsEl.appendChild(tr);
    });

    if (rounds.length) {
      const last = rawEntries[rawEntries.length - 1];
      const tr = document.createElement('tr');
      if (currentTurn === maxTurn) tr.className = 'current';
      tr.innerHTML = `<td class="turn-col">—</td><td class="score-col"><b>Final: ${last.scores[0]}</b></td><td class="score-col"><b>Final: ${last.scores[1]}</b></td>`;
      rowsEl.appendChild(tr);
    }
  }

  async function gotoTurn(turn) {
    if (!sessionId) return;
    turn = Math.max(0, Math.min(rawEntries.length, turn));
    if (turn === currentTurn) return;
    App.clearError();
    try {
      const position = await Api.gotoTurn(sessionId, turn);
      currentTurn = turn;
      render();
      App.setPositionFromGameNav(position);
    } catch (e) {
      App.showError(e.message);
    }
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  }

  document.addEventListener('keydown', (e) => {
    if (!sessionId) return;
    const tag = document.activeElement && document.activeElement.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    if (e.key === 'ArrowLeft') gotoTurn(currentTurn - 1);
    else if (e.key === 'ArrowRight') gotoTurn(currentTurn + 1);
  });

  return { init, hide, gotoTurn };
})();
