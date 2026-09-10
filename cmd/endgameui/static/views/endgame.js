// Endgame analysis view: solve controls + ranked variation list on the
// left, with hover/click preview of a line on the board, and an "Expand"
// button per line that commits its first move and auto-solves for the
// opponent from the resulting position.
const EndgameView = (() => {
  function render(sessionId, position) {
    const panel = document.getElementById('at-content');
    panel.innerHTML = `
      ${App.posMetaHTML(position, `Endgame position &middot; ${position.bagCount} tile(s) in bag`)}
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
        <div style="flex:0"><button id="eg-cancel" class="secondary" disabled>Cancel</button></div>
      </div>
      <div class="section-title">Advanced</div>
      <div class="row">
        <div><label>Plies (0 = auto)</label><input id="eg-plies" type="number" value="0" style="width:70px"></div>
        <div><label>Threads (0 = auto)</label><input id="eg-threads" type="number" value="0" style="width:70px"></div>
        <div><label>Auto-expand lines</label><input id="eg-expandlines" type="number" value="5" style="width:70px"></div>
      </div>
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
    const solveBtn = document.getElementById('eg-solve');
    const cancelBtn = document.getElementById('eg-cancel');
    const numLines = document.getElementById('eg-all').checked
      ? 'all'
      : (document.getElementById('eg-numlines').value || '1');
    const plies = parseInt(document.getElementById('eg-plies').value, 10) || 0;
    const threads = parseInt(document.getElementById('eg-threads').value, 10) || 0;

    results.innerHTML = '';
    const startedAt = Date.now();
    const tick = () => { status.textContent = `Solving… (${Math.round((Date.now() - startedAt) / 1000)}s)`; };
    tick();
    const timer = setInterval(tick, 1000);

    let jobId = null;
    solveBtn.disabled = true;
    cancelBtn.disabled = false;
    cancelBtn.onclick = () => { if (jobId) Api.cancelJob(jobId).catch(() => {}); };

    try {
      const result = await Api.solveEndgame(
        sessionId, position.nodeId, { numLines, plies, threads },
        null,
        (id) => { jobId = id; },
      );
      status.textContent = result.capped
        ? `Done (capped at ${result.variations.length} lines)`
        : `Done — ${result.variations.length} line(s)`;
      renderVariations(sessionId, position, result);
    } catch (e) {
      status.textContent = 'Canceled or failed.';
      App.showError(e.message);
    } finally {
      clearInterval(timer);
      solveBtn.disabled = false;
      cancelBtn.disabled = true;
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
        await expand(sessionId, position, v, result.pliesUsed);
      });
      results.appendChild(div);
    });
  }

  // parentPliesUsed is the actual depth the parent position was solved to
  // (not just whatever was in the Plies field — that may have been "0 =
  // auto"). The child gets one fewer, since committing a move consumes one
  // ply of that search horizon; this keeps the depth consistent down a
  // line instead of each step picking its own unrelated default. The
  // child's Plies field is set to show exactly what's being used, and
  // stays editable if you want to resolve at a different depth.
  async function expand(sessionId, position, variation, parentPliesUsed) {
    if (!variation.moves.length) return;
    try {
      const child = await Api.commit(sessionId, position.nodeId, variation.moves[0].key);
      App.pushPosition(child);
      const expandLines = document.getElementById('eg-expandlines');
      const n = expandLines ? (expandLines.value || '5') : '5';
      const childPlies = parentPliesUsed ? Math.max(1, parentPliesUsed - 1) : 0;
      render(sessionId, child);
      document.getElementById('eg-plies').value = childPlies || '';
      const status = document.getElementById('eg-status');
      status.textContent = `Solving opponent's replies…`;
      const result = await Api.solveEndgame(sessionId, child.nodeId, { numLines: n, plies: childPlies, threads: 0 });
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
