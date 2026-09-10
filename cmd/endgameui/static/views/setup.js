// The "load a position" screen: GCG replay, CGP paste, or a manual board
// editor (which just assembles a CGP string and reuses that same endpoint).
const SetupView = (() => {
  const MANUAL_DIM = 15; // standard board only, for simplicity

  function init(onLoaded) {
    // Scoped to the outer tab bar only: #tab-gcg has its own nested .tabs
    // for picking a GCG source, which is wired up separately in initGCGTab.
    document.querySelectorAll('#setup > .tabs > button').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#setup > .tabs > button').forEach((b) => b.classList.remove('active'));
        document.querySelectorAll('.setup-panel').forEach((p) => p.classList.remove('active'));
        btn.classList.add('active');
        document.getElementById(`tab-${btn.dataset.tab}`).classList.add('active');
      });
    });

    initGCGTab(onLoaded);
    initCGPTab(onLoaded);
    initManualTab(onLoaded);
  }

  function initGCGTab(onLoaded) {
    let selectedKind = 'file';
    let selectedRef = null;
    const listBtn = document.getElementById('gcgListBtn');
    const fileListEl = document.getElementById('gcgFileList');
    const turnPicker = document.getElementById('gcgTurnPicker');
    const slider = document.getElementById('gcgTurnSlider');
    const turnValue = document.getElementById('gcgTurnValue');
    const turnLabel = document.getElementById('gcgTurnLabel');

    document.querySelectorAll('#tab-gcg .tabs button[data-gcgsrc]').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#tab-gcg .tabs button[data-gcgsrc]').forEach((b) => b.classList.remove('active'));
        document.querySelectorAll('.gcg-src-panel').forEach((p) => p.classList.remove('active'));
        btn.classList.add('active');
        document.getElementById(`gcgsrc-${btn.dataset.gcgsrc}`).classList.add('active');
        selectedKind = btn.dataset.gcgsrc;
        selectedRef = null;
        turnPicker.style.display = 'none';
      });
    });

    async function fetchSummary(kind, ref) {
      App.clearError();
      turnPicker.style.display = 'none';
      try {
        const summary = await Api.gcgSummary(kind, ref);
        selectedKind = kind;
        selectedRef = ref;
        slider.max = summary.numTurns;
        slider.value = summary.numTurns;
        turnValue.textContent = summary.numTurns;
        turnLabel.textContent = `Turn (${summary.playerNames.join(' vs ')}, ${summary.lexicon})`;
        turnPicker.style.display = '';
      } catch (e) { App.showError(e.message); }
    }

    listBtn.addEventListener('click', async () => {
      App.clearError();
      try {
        const files = await Api.gcgList(document.getElementById('gcgDir').value.trim());
        fileListEl.innerHTML = '';
        files.forEach((f) => {
          const div = document.createElement('div');
          div.textContent = f.name;
          div.addEventListener('click', () => {
            fileListEl.querySelectorAll('div').forEach((d) => d.classList.remove('selected'));
            div.classList.add('selected');
            fetchSummary('file', f.path);
          });
          fileListEl.appendChild(div);
        });
      } catch (e) { App.showError(e.message); }
    });

    document.getElementById('gcgWooglesFetchBtn').addEventListener('click', () => {
      fetchSummary('woogles', document.getElementById('gcgWooglesId').value.trim());
    });
    document.getElementById('gcgXtFetchBtn').addEventListener('click', () => {
      fetchSummary('xt', document.getElementById('gcgXtId').value.trim());
    });
    document.getElementById('gcgWebFetchBtn').addEventListener('click', () => {
      fetchSummary('web', document.getElementById('gcgWebUrl').value.trim());
    });

    slider.addEventListener('input', () => { turnValue.textContent = slider.value; });

    document.getElementById('gcgLoadBtn').addEventListener('click', async () => {
      if (!selectedRef) return;
      App.clearError();
      try {
        const res = await Api.newSessionFromGCG(selectedKind, selectedRef, parseInt(slider.value, 10));
        onLoaded(res.sessionId, res.position);
      } catch (e) { App.showError(e.message); }
    });
  }

  function initCGPTab(onLoaded) {
    document.getElementById('cgpLoadBtn').addEventListener('click', async () => {
      App.clearError();
      try {
        const cgp = document.getElementById('cgpInput').value.trim();
        const res = await Api.newSessionFromCGP(cgp);
        onLoaded(res.sessionId, res.position);
      } catch (e) { App.showError(e.message); }
    });
  }

  async function initManualTab(onLoaded) {
    const grid = document.getElementById('manualGrid');
    for (let r = 0; r < MANUAL_DIM; r++) {
      const tr = document.createElement('tr');
      for (let c = 0; c < MANUAL_DIM; c++) {
        const td = document.createElement('td');
        const input = document.createElement('input');
        input.maxLength = 1;
        input.dataset.r = r;
        input.dataset.c = c;
        td.appendChild(input);
        tr.appendChild(td);
      }
      grid.appendChild(tr);
    }

    try {
      const opts = await Api.options();
      const lexSel = document.getElementById('manualLexicon');
      const ldSel = document.getElementById('manualLD');
      opts.lexicons.forEach((l) => lexSel.appendChild(new Option(l, l)));
      opts.letterDistributions.forEach((l) => ldSel.appendChild(new Option(l, l)));
    } catch (e) { App.showError(e.message); }

    document.getElementById('manualLoadBtn').addEventListener('click', async () => {
      App.clearError();
      try {
        const rows = [];
        for (let r = 0; r < MANUAL_DIM; r++) {
          const chars = [];
          for (let c = 0; c < MANUAL_DIM; c++) {
            const input = grid.querySelector(`input[data-r="${r}"][data-c="${c}"]`);
            chars.push(input.value || '');
          }
          rows.push(rowToCGPRow(chars));
        }
        const racks = [
          document.getElementById('manualRack1').value.trim().toUpperCase(),
          document.getElementById('manualRack2').value.trim().toUpperCase(),
        ];
        const scores = [
          parseInt(document.getElementById('manualScore1').value, 10) || 0,
          parseInt(document.getElementById('manualScore2').value, 10) || 0,
        ];
        const lexicon = document.getElementById('manualLexicon').value;
        const letterDistribution = document.getElementById('manualLD').value;
        const res = await Api.newSessionManual(rows, racks, scores, lexicon, letterDistribution);
        onLoaded(res.sessionId, res.position);
      } catch (e) { App.showError(e.message); }
    });
  }

  function rowToCGPRow(chars) {
    let out = '';
    let run = 0;
    for (const ch of chars) {
      if (!ch || ch === ' ') {
        run++;
      } else {
        if (run > 0) { out += run; run = 0; }
        out += ch;
      }
    }
    if (run > 0) out += run;
    return out || String(MANUAL_DIM);
  }

  return { init };
})();
