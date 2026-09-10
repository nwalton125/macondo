// Top-level glue: setup screen vs. analysis screen, breadcrumb/tree
// navigation, board updates, and dispatch to the endgame or peg view based
// on the current node's mode.
const App = (() => {
  const state = {
    sessionId: null,
    positions: [], // stack of PositionDTO from root to current, for breadcrumb nav
    hasGame: false,
    leftTab: 'analysis', // 'history' | 'analysis' — which sub-tab of leftPanel is showing
  };

  function showError(msg) {
    const el = document.getElementById('error-banner');
    el.textContent = msg;
    el.style.display = 'block';
  }
  function clearError() {
    document.getElementById('error-banner').style.display = 'none';
  }

  function onSessionCreated(sessionId, position, hasGame) {
    state.sessionId = sessionId;
    state.positions = [position];
    state.hasGame = !!hasGame;
    state.leftTab = state.hasGame ? 'history' : 'analysis';
    document.getElementById('setup').style.display = 'none';
    document.getElementById('app').style.display = 'block';
    document.getElementById('headerSub').textContent =
      `${position.playerNames[0] || 'Player 1'} vs ${position.playerNames[1] || 'Player 2'}`;
    renderLeftTabs();
    renderCurrent();
    if (state.hasGame) GameNav.init(sessionId);
  }

  function renderLeftTabs() {
    const bar = document.getElementById('leftTabs');
    bar.style.display = state.hasGame ? '' : 'none';
    bar.querySelectorAll('button').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.lt === state.leftTab);
      btn.onclick = () => { state.leftTab = btn.dataset.lt; showLeftTab(); };
    });
    showLeftTab();
  }

  function showLeftTab() {
    document.getElementById('ht-content').style.display = state.leftTab === 'history' ? '' : 'none';
    document.getElementById('at-content').style.display = state.leftTab === 'analysis' ? '' : 'none';
  }

  function pushPosition(position) {
    state.positions.push(position);
    renderBreadcrumb();
  }

  // Called by GameNav when the user steps to a different turn of the loaded
  // game: that turn is an independent root, so the solve-tree breadcrumb
  // resets to just it (any exploration built on other turns is untouched
  // and still there if you step back to them).
  function setPositionFromGameNav(position) {
    autoSolveAt(position);
  }

  function jumpTo(idx) {
    state.positions = state.positions.slice(0, idx + 1);
    renderCurrent();
    autoSolveClick();
  }

  function autoSolveAt(position) {
    state.positions = [position];
    renderCurrent();
    autoSolveClick();
  }

  // Immediately shows this position's lines again rather than making the
  // user re-click Solve; the solve is cache-backed so this is fast if it
  // (or a superset of it) was already computed.
  function autoSolveClick() {
    const solveBtn = document.getElementById('eg-solve') || document.getElementById('peg-solve');
    if (solveBtn) solveBtn.click();
  }

  function renderCurrent() {
    const position = state.positions[state.positions.length - 1];
    renderBreadcrumb();
    if (position.mode === 'peg') {
      PegView.render(state.sessionId, position);
    } else {
      EndgameView.render(state.sessionId, position);
    }
  }

  function renderBreadcrumb() {
    const el = document.getElementById('breadcrumb');
    el.innerHTML = '';
    state.positions.forEach((pos, idx) => {
      if (idx > 0) {
        const sep = document.createElement('span');
        sep.className = 'sep';
        sep.textContent = '›';
        el.appendChild(sep);
      }
      const span = document.createElement('span');
      const isCurrent = idx === state.positions.length - 1;
      span.className = 'crumb' + (isCurrent ? ' current' : '');
      if (idx === 0) {
        span.textContent = 'Start';
      } else {
        const step = pos.path[pos.path.length - 1];
        span.textContent = `${idx}. ${step.move.description}`;
      }
      if (!isCurrent) span.addEventListener('click', () => jumpTo(idx));
      el.appendChild(span);
    });
  }

  function currentPlayerNames() {
    const root = state.positions[0];
    return [root.playerNames[0] || 'Player 1', root.playerNames[1] || 'Player 2'];
  }

  function updateBoard(position, previewMap) {
    const panel = document.getElementById('boardPanel');
    Board.render(panel, position, previewMap);
    // Rack/pool live outside the left-panel tabs (History/Analysis) so they
    // update immediately on any real navigation — turn nav, breadcrumb
    // jumps, committing into a subline — regardless of which tab is
    // showing. They intentionally don't change on a mere hover-preview of
    // an uncommitted variation, since previewMap doesn't change `position`.
    Board.renderRack(document.getElementById('rackBar'), position.onTurnRack);
    Board.renderPool(document.getElementById('poolPanel'), position.unseenTiles);
  }

  // Shared "who's playing, what's the score" header used at the top of both
  // the endgame and peg left panels. Rack and unseen tiles aren't shown
  // here anymore — they're always visible now (rack bar under the board,
  // pool panel on the right), independent of which left-panel tab is open.
  function posMetaHTML(position, extraLine) {
    const scoreRow = (i) => {
      const name = position.playerNames[i] || `Player ${i + 1}`;
      const onTurn = position.onTurn === i ? ' ◀ on turn' : '';
      return `<div class="rack-row"><span>${escapeHtml(name)}${onTurn}</span><span>${position.scores[i]}</span></div>`;
    };
    return `<div class="pos-meta">
      ${scoreRow(0)}${scoreRow(1)}
      <div style="margin-top:4px">${extraLine}</div>
    </div>`;
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  }

  function init() {
    SetupView.init(onSessionCreated);
    document.getElementById('newPositionBtn').addEventListener('click', () => {
      document.getElementById('app').style.display = 'none';
      document.getElementById('setup').style.display = 'block';
      GameNav.hide();
      clearError();
    });
  }

  return {
    showError, clearError, pushPosition, setPositionFromGameNav, updateBoard,
    posMetaHTML, currentPlayerNames, init,
  };
})();

window.addEventListener('DOMContentLoaded', () => App.init());
