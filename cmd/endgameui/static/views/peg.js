// Pre-endgame (peg) view: candidate plays ranked by points, each expandable
// into its per-draw win/draw/loss outcomes; clicking a draw traces the
// solver's structured explanation of exactly what happens along that line.
const PegView = (() => {
  function render(sessionId, position) {
    const panel = document.getElementById('leftPanel');
    panel.innerHTML = `
      <div class="pos-meta">
        Pre-endgame position &middot; ${position.bagCount} tile(s) unseen &middot; on turn: player ${position.onTurn + 1}
      </div>
      <div class="section-title">Solve</div>
      <div class="row">
        <div><label>Nested endgame plies</label><input id="peg-plies" type="number" value="4" style="width:70px"></div>
        <div><label>Threads (0 = auto)</label><input id="peg-threads" type="number" value="0" style="width:70px"></div>
        <div style="flex:0"><button id="peg-solve">Solve</button></div>
      </div>
      <div id="peg-status" style="font-size:12px;color:var(--text-dim);margin-top:8px;"></div>
      <div id="peg-results" style="margin-top:8px;"></div>
      <div class="legend">
        <span><span class="dot" style="background:#2e9e4f"></span>Win</span>
        <span><span class="dot" style="background:#c9a227"></span>Draw</span>
        <span><span class="dot" style="background:#c1442e"></span>Loss</span>
      </div>
    `;
    document.getElementById('peg-solve').addEventListener('click', () => solve(sessionId, position));
    App.updateBoard(position, null);
  }

  async function solve(sessionId, position) {
    const status = document.getElementById('peg-status');
    const results = document.getElementById('peg-results');
    const endgamePlies = parseInt(document.getElementById('peg-plies').value, 10) || 4;
    const threads = parseInt(document.getElementById('peg-threads').value, 10) || 0;
    results.innerHTML = '';
    status.textContent = 'Solving… (this can take a while)';
    try {
      const result = await Api.solvePeg(sessionId, position.nodeId, { endgamePlies, threads }, (st) => {
        status.textContent = st === 'pending' ? 'Solving… (this can take a while)' : st;
      });
      status.textContent = `Done — ${result.plays.length} play(s)`;
      renderPlays(sessionId, position, result.plays);
    } catch (e) {
      status.textContent = '';
      App.showError(e.message);
    }
  }

  function renderPlays(sessionId, position, plays) {
    const results = document.getElementById('peg-results');
    results.innerHTML = '';
    plays.forEach((p, idx) => {
      const total = p.outcomes.reduce((a, o) => a + o.count, 0) || 1;
      const wins = p.outcomes.filter((o) => o.result === 'win').reduce((a, o) => a + o.count, 0);
      const draws = p.outcomes.filter((o) => o.result === 'draw').reduce((a, o) => a + o.count, 0);
      const losses = p.outcomes.filter((o) => o.result === 'loss').reduce((a, o) => a + o.count, 0);

      const div = document.createElement('div');
      div.className = 'peg-play';
      div.innerHTML = `
        <div class="peg-play-head">
          <span class="desc">${idx + 1}. ${escapeHtml(p.move.description)} <span style="font-weight:400;color:var(--text-dim)">(${p.points.toFixed(1)} pts)</span></span>
          <span class="wdl-bar">
            <span class="w" style="width:${(100 * wins / total)}%"></span>
            <span class="d" style="width:${(100 * draws / total)}%"></span>
            <span class="l" style="width:${(100 * losses / total)}%"></span>
          </span>
        </div>
        <div class="peg-outcomes"></div>
      `;
      const head = div.querySelector('.peg-play-head');
      const outcomesEl = div.querySelector('.peg-outcomes');
      head.addEventListener('click', () => {
        const wasOpen = outcomesEl.classList.contains('open');
        div.parentElement.querySelectorAll('.peg-outcomes.open').forEach((el) => el.classList.remove('open'));
        if (!wasOpen) {
          renderOutcomes(sessionId, position, p, outcomesEl);
          outcomesEl.classList.add('open');
        }
      });
      results.appendChild(div);
    });
  }

  function renderOutcomes(sessionId, position, play, container) {
    const groups = { win: [], draw: [], loss: [] };
    play.outcomes.forEach((o) => { if (groups[o.result]) groups[o.result].push(o); });
    container.innerHTML = '';
    ['win', 'draw', 'loss'].forEach((result) => {
      if (!groups[result].length) return;
      const g = document.createElement('div');
      g.className = 'outcome-group';
      g.innerHTML = `<div class="label">${result} (${groups[result].reduce((a, o) => a + o.count, 0)} draws)</div>`;
      groups[result].forEach((o) => {
        const chip = document.createElement('span');
        chip.className = `outcome-chip ${result}`;
        chip.textContent = `${o.tiles} ×${o.count}`;
        chip.addEventListener('click', () => openTrace(sessionId, position, play, o, container));
        g.appendChild(chip);
      });
      container.appendChild(g);
    });
    const traceHolder = document.createElement('div');
    traceHolder.className = 'trace-holder';
    container.appendChild(traceHolder);
  }

  async function openTrace(sessionId, position, play, outcome, container) {
    let holder = container.querySelector('.trace-holder');
    if (!holder) {
      holder = document.createElement('div');
      holder.className = 'trace-holder';
      container.appendChild(holder);
    }
    holder.innerHTML = `<div style="font-size:12px;color:var(--text-dim);">Loading suggested draw…</div>`;
    try {
      const suggestion = await Api.pegSuggest(sessionId, position.nodeId, outcome.tiles);
      holder.innerHTML = `
        <div class="row" style="margin-top:6px;">
          <div>
            <label>Full draw order (${position.bagCount} tiles, edit to explore a different exact draw)</label>
            <input class="trace-eventuality" value="${escapeHtml(suggestion.suggested)}" style="width:100%;font-family:monospace">
          </div>
          <div style="flex:0"><button class="small trace-btn">Trace</button></div>
        </div>
        <div class="trace-result"></div>
      `;
      holder.querySelector('.trace-btn').addEventListener('click', async () => {
        const ev = holder.querySelector('.trace-eventuality').value.trim();
        const out = holder.querySelector('.trace-result');
        out.innerHTML = `<div style="font-size:12px;color:var(--text-dim);">Tracing…</div>`;
        try {
          const trace = await Api.pegTrace(sessionId, position.nodeId, play.move.key, ev);
          renderTrace(out, trace);
        } catch (e) {
          out.innerHTML = '';
          App.showError(e.message);
        }
      });
    } catch (e) {
      holder.innerHTML = '';
      App.showError(e.message);
    }
  }

  function renderTrace(container, t) {
    let html = `<div class="trace-panel">`;
    html += `<div class="verdict ${t.verdict}">${t.verdict}</div>`;
    html += `<div class="line"><b>We play</b> ${escapeHtml(t.ourPlay)} (+${t.ourScore})</div>`;
    html += `<div class="line">Rack before: <code>${t.ourRackBefore}</code> &rarr; after: <code>${t.ourRackAfter}</code></div>`;
    html += `<div class="line">Bag before: <code>${t.bagBefore || '(empty)'}</code> &rarr; after: <code>${t.bagAfter || '(empty)'}</code></div>`;
    html += `<div class="line">Opponent rack: <code>${t.oppRack}</code></div>`;
    if (t.decisiveOppPlay) {
      html += `<div class="line"><b>Decisive opponent reply:</b> ${escapeHtml(t.decisiveOppPlay)}</div>`;
      html += `<div class="line">Opp rack after: <code>${t.oppRackAfter}</code> &middot; bag after: <code>${t.bagAfterOppPlay || '(empty)'}</code></div>`;
    } else if (t.totalOppReplies) {
      html += `<div class="line">Checked ${t.totalOppReplies} opponent replies — none forced a loss.</div>`;
    }
    (t.nested || []).forEach((n) => {
      html += `<div class="nested">
        <div class="line">Nested pre-endgame (depth ${n.depth}): our rack <code>${n.ourRack}</code>, opp <code>${n.oppRack}</code>, bag <code>${n.bagContent || '(empty)'}</code></div>
        <div class="line">Unseen pool: <code>${n.unseenPool}</code></div>
        <div class="line">Best play: ${escapeHtml(n.bestPlay)} &mdash; verdict: <b class="verdict ${n.verdict}">${n.verdict}</b></div>
      </div>`;
    });
    html += `</div>`;
    container.innerHTML = html;
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  }

  return { render };
})();
