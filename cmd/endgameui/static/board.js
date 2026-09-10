// Renders a PositionDTO onto a <table>, coloring committed tiles by player
// and recency, with support for an extra "preview" overlay (an uncommitted
// variation's tiles, shown in a dashed highlight) that never touches the
// underlying position state.
const Board = (() => {
  const BONUS_CLASS = {
    '~': 'sq-tw', // quad word (super only)
    '^': 'sq-tl', // quad letter (super only)
    '=': 'sq-tw',
    '"': 'sq-tl',
    "'": 'sq-dl',
    '-': 'sq-dw',
    ' ': 'sq-normal',
  };
  const BONUS_LABEL = { '~': '4W', '^': '4L', '=': '3W', '"': '3L', "'": '2L', '-': '2W' };

  function bonusClass(ch, r, c, dim) {
    const center = Math.floor(dim / 2);
    if (r === center && c === center) return 'sq-center';
    return BONUS_CLASS[ch] || 'sq-normal';
  }

  function isBlankLetter(ch) {
    return /[a-z]/.test(ch);
  }

  // shadeColor picks a color along a per-player ramp: older plies are
  // lighter, more recent plies are darker, so a glance at saturation tells
  // you roughly how far back in the committed line a tile was placed.
  function shadeColor(player, ply, totalPlies) {
    const hue = player === 0 ? 215 : 28;
    const t = totalPlies > 0 ? Math.min(1, ply / totalPlies) : 1;
    const light = 80 - t * 42; // 80% (oldest) down to 38% (most recent)
    return `hsl(${hue}, 68%, ${light}%)`;
  }

  function textColorFor(light) {
    return light < 55 ? '#fff' : '#241d0f';
  }

  // previewMap: optional Map from "r,c" -> { letter, player, step, maxStep }
  // describing an uncommitted variation to overlay on top of the real board.
  function render(container, position, previewMap) {
    const dim = position.dim;
    const totalPlies = position.path.length;
    const table = document.createElement('table');
    table.id = 'board-table';

    for (let r = 0; r < dim; r++) {
      const tr = document.createElement('tr');
      for (let c = 0; c < dim; c++) {
        const td = document.createElement('td');
        const bonusChar = position.bonuses[r][c];
        td.className = bonusClass(bonusChar, r, c, dim);

        const existingLetter = position.board[r][c];
        const key = `${r},${c}`;
        const preview = previewMap && previewMap.get(key);

        if (preview || existingLetter !== '.') {
          const tile = document.createElement('div');
          tile.className = 'tile' + (preview ? ' preview' : '');
          let letter, light;
          if (preview) {
            letter = preview.letter;
            const color = shadeColor(preview.player, totalPlies + preview.step, totalPlies + preview.maxStep);
            tile.style.background = color;
            light = parseLight(color);
          } else {
            letter = existingLetter;
            const ply = position.squarePly[r][c];
            const player = position.squarePlayer[r][c];
            if (ply === 0 || player === -1) {
              tile.style.background = '#f4e4bc';
              light = 80;
            } else {
              const color = shadeColor(player, ply, totalPlies);
              tile.style.background = color;
              light = parseLight(color);
            }
          }
          tile.style.color = textColorFor(light);
          tile.textContent = letter.toUpperCase();
          if (isBlankLetter(letter)) tile.style.fontStyle = 'italic';
          td.appendChild(tile);
        } else {
          const label = BONUS_LABEL[bonusChar];
          if (label && !(r === Math.floor(dim / 2) && c === Math.floor(dim / 2))) {
            const span = document.createElement('span');
            span.className = 'sq-empty-label';
            span.textContent = label;
            td.appendChild(span);
          } else if (r === Math.floor(dim / 2) && c === Math.floor(dim / 2)) {
            td.innerHTML = '<span class="sq-empty-label">★</span>';
          }
        }
        tr.appendChild(td);
      }
      table.appendChild(tr);
    }

    container.innerHTML = '';
    container.appendChild(table);
  }

  function parseLight(hslStr) {
    const m = /,\s*([\d.]+)%\)$/.exec(hslStr);
    return m ? parseFloat(m[1]) : 50;
  }

  // Builds a previewMap for one variation's moves, starting from `fromPly`
  // (position.path.length at the node the variation was solved from) and
  // `onTurn` (which player moves first in the variation).
  function previewMapForVariation(moves, onTurn, uptoIndex) {
    const map = new Map();
    const n = uptoIndex === undefined ? moves.length : uptoIndex;
    for (let i = 0; i < n; i++) {
      const m = moves[i];
      if (m.action !== 'Play' || !m.tiles) continue;
      const player = (onTurn + i) % 2;
      for (let j = 0; j < m.tiles.length; j++) {
        const ch = m.tiles[j];
        if (ch === '.') continue;
        const r = m.vertical ? m.row + j : m.row;
        const c = m.vertical ? m.col : m.col + j;
        map.set(`${r},${c}`, { letter: ch, player, step: i + 1, maxStep: n });
      }
    }
    return map;
  }

  // renderRack shows the given rack as a row of large physical-style tiles,
  // e.g. below the board, Quackle-style.
  function renderRack(container, rack) {
    container.innerHTML = '';
    if (!rack) return;
    for (const ch of rack) {
      const tile = document.createElement('div');
      tile.className = 'tile tile-lg';
      tile.textContent = ch.toUpperCase();
      if (isBlankLetter(ch)) tile.style.fontStyle = 'italic';
      container.appendChild(tile);
    }
  }

  // renderPool shows the unseen-tile pool as one line per distinct letter,
  // that letter repeated for its count (e.g. "EEE"), sorted and grouped —
  // pool is expected pre-sorted so equal letters are already adjacent.
  function renderPool(container, pool) {
    container.innerHTML = '';
    if (!pool) return;
    const count = document.createElement('div');
    count.className = 'pool-count';
    count.textContent = `${pool.length} tile${pool.length === 1 ? '' : 's'} remaining`;
    container.appendChild(count);
    let i = 0;
    while (i < pool.length) {
      let j = i;
      while (j < pool.length && pool[j] === pool[i]) j++;
      const line = document.createElement('div');
      line.className = 'pool-line';
      line.textContent = pool.slice(i, j).toUpperCase();
      container.appendChild(line);
      i = j;
    }
  }

  return { render, previewMapForVariation, renderRack, renderPool };
})();
