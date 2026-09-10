// Endgame analysis view: solve controls + ranked variation list on the
// left, with hover/click preview of a line on the board, and an "Expand"
// button per line that commits its first move and auto-solves for the
// opponent from the resulting position.
const EndgameView = (() => {
  function render(sessionId, position) {
    const panel = document.getElementById('leftPanel');
    panel.innerHTML = `
      <div class="pos-meta">
        Endgame position &middot; ${position.bagCount} tile(s) in bag &middot; on turn: player ${position.onTurn + 1}
      </div>
      <div class="section-title">Solve</div>
      <div class="row">
        <div>
          <label>Lines to show</label>
          <input id="eg-numlines" value="1" style="width:70px">
        </div>
        <div>
          <label><input type="checkbox" id="eg-all"> All</label>
        </div>
        <div style="flex:0"><button id="eg-solve">Solve</button></div>
      </div>
      <details class="advanced">
        <summary>Advanced</summary>
        <div class="row">
          <div><label>Plies (0 = auto)</label><input id="eg-plies" type="number" value="0" style="width:70px"></div>
          <div><label>Threads (0 = auto)</label><input id="eg-threads" type="number" value="0" style="width:70px"></div>
          <div><label>Auto-expand lines</label><input id="eg-expandlines" type="number" value="5" style="width:70px"></div>
        </div>
      </details>
      <div id="eg-status" style="font-size:12px;color:var(--text-dim);margin-top:8px;"></div>
      <div id="eg-results" style="margin-top:8px;"></div>
      <div class="legend">
        <span><span class="dot" style="background:hsl(215,68%,60%)"></span>Self plays</span>
        <span><span class="dot" style="background:hsl(28,68%,45%)"></span>Opponent plays</span>
        <span><span class="dot" style="background:#f4e4bc"></span>Pre-existing</span>
      </div>
    `;

    document.getElementById('eg-all').addEventListener('change', (e) => {
      document.getElementById('eg-numlines').disabled = e.target.checked;
    });

    document.getElementById('eg-solve').addEventListener('click', () => solve(sessionId, position));

    App.updateBoard(position, null);
  }

  async function solve(sessionId, position) {
    const status = document.getElementById('eg-status');
    const results = document.getElementById('eg-results');
    const numLines = document.getElementById('eg-all').checked
      ? 'all'
      : (document.getElementById('eg-numlines').value || '1');
    const plies = parseInt(document.getElementById('eg-plies').value, 10) || 0;
    const threads = parseInt(document.getElementById('eg-threads').value, 10) || 0;

    results.innerHTML = '';
    status.textContent = 'Solving…';
    try {
      const result = await Api.solveEndgame(sessionId, position.nodeId, { numLines, plies, threads }, (st) => {
        status.textContent = st === 'pending' ? 'Solving…' : st;
      });
      status.textContent = result.capped
        ? `Done (capped at ${result.variations.length} lines)`
        : `Done — ${result.variations.length} line(s)`;
      renderVariations(sessionId, position, result);
    } catch (e) {
      status.textContent = '';
      App.showError(e.message);
    }
  }

  function renderVariations(sessionId, position, result) {
    const results = document.getElementById('eg-results');
    results.innerHTML = '';
    result.variations.forEach((v, idx) => {
      const div = document.createElement('div');
      div.className = 'variation';
      const seqHtml = v.moves.map((m, i) => {
        const who = i % 2 === 0 ? 'Self' : 'Opp';
        return `<b>${i + 1}.</b> ${who}: ${escapeHtml(m.description)} (${m.score >= 0 ? '+' : ''}${m.score})`;
      }).join('<br>');
      div.innerHTML = `
        <span class="spread">${v.finalSpread >= 0 ? '+' : ''}${v.finalSpread}</span>
        <div>Line ${idx + 1}</div>
        <div class="seq">${seqHtml}</div>
        <button class="small expand-btn" data-idx="${idx}">Expand ▸ (commit &amp; see opponent's replies)</button>
      `;
      div.addEventListener('mouseenter', () => {
        const map = Board.previewMapForVariation(v.moves, position.onTurn);
        App.updateBoard(position, map);
      });
      div.addEventListener('mouseleave', () => App.updateBoard(position, null));
      div.querySelector('.expand-btn').addEventListener('click', async (e) => {
        e.stopPropagation();
        await expand(sessionId, position, v);
      });
      results.appendChild(div);
    });
  }

  async function expand(sessionId, position, variation) {
    if (!variation.moves.length) return;
    try {
      const child = await Api.commit(sessionId, position.nodeId, variation.moves[0].key);
      App.pushPosition(child);
      const expandLines = document.getElementById('eg-expandlines');
      const n = expandLines ? (expandLines.value || '5') : '5';
      render(sessionId, child);
      const status = document.getElementById('eg-status');
      status.textContent = `Solving opponent's replies…`;
      const result = await Api.solveEndgame(sessionId, child.nodeId, { numLines: n, plies: 0, threads: 0 });
      status.textContent = `Done — ${result.variations.length} line(s)`;
      renderVariations(sessionId, child, result);
    } catch (e) {
      App.showError(e.message);
    }
  }

  function escapeHtml(s) {
    return s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  }

  return { render };
})();
