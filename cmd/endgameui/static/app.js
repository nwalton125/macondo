// Top-level glue: setup screen vs. analysis screen, breadcrumb/tree
// navigation, board updates, and dispatch to the endgame or peg view based
// on the current node's mode.
const App = (() => {
  const state = {
    sessionId: null,
    positions: [], // stack of PositionDTO from root to current, for breadcrumb nav
  };

  function showError(msg) {
    const el = document.getElementById('error-banner');
    el.textContent = msg;
    el.style.display = 'block';
  }
  function clearError() {
    document.getElementById('error-banner').style.display = 'none';
  }

  function onSessionCreated(sessionId, position) {
    state.sessionId = sessionId;
    state.positions = [position];
    document.getElementById('setup').style.display = 'none';
    document.getElementById('app').style.display = 'block';
    document.getElementById('headerSub').textContent =
      `${position.playerNames[0] || 'Player 1'} vs ${position.playerNames[1] || 'Player 2'}`;
    renderCurrent();
  }

  function pushPosition(position) {
    state.positions.push(position);
    renderBreadcrumb();
  }

  function jumpTo(idx) {
    state.positions = state.positions.slice(0, idx + 1);
    renderCurrent();
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

  function updateBoard(position, previewMap) {
    const panel = document.getElementById('boardPanel');
    Board.render(panel, position, previewMap);
  }

  function init() {
    SetupView.init(onSessionCreated);
    document.getElementById('newPositionBtn').addEventListener('click', () => {
      document.getElementById('app').style.display = 'none';
      document.getElementById('setup').style.display = 'block';
      clearError();
    });
  }

  return { showError, clearError, pushPosition, updateBoard, init };
})();

window.addEventListener('DOMContentLoaded', () => App.init());
