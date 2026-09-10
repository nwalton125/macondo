// Thin fetch wrappers around the endgameui HTTP+JSON API, plus a helper to
// poll a background solve job to completion.
const Api = (() => {
  async function req(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(data.error || `${method} ${path} failed (${res.status})`);
    }
    return data;
  }

  const get = (path) => req('GET', path);
  const post = (path, body) => req('POST', path, body ?? {});

  // Polls a job until it's done or errored, calling onTick(status) each poll.
  async function waitForJob(jobId, onTick) {
    for (;;) {
      const j = await get(`/api/jobs/${jobId}`);
      if (onTick) onTick(j.status);
      if (j.status === 'done') return j.result;
      if (j.status === 'error') throw new Error(j.error || 'job failed');
      await new Promise((r) => setTimeout(r, 500));
    }
  }

  return {
    options: () => get('/api/options'),
    gcgList: (dir) => get(`/api/gcg/list?dir=${encodeURIComponent(dir)}`),
    gcgSummary: (kind, ref) => get(`/api/gcg/summary?kind=${encodeURIComponent(kind)}&ref=${encodeURIComponent(ref)}`),

    newSessionFromGCG: (kind, ref) => post('/api/sessions/gcg', { kind, ref }),
    newSessionFromCGP: (cgp) => post('/api/sessions/cgp', { cgp }),
    newSessionManual: (rows, racks, scores, lexicon, letterDistribution) =>
      post('/api/sessions/manual', { rows, racks, scores, lexicon, letterDistribution }),

    gameTranscript: (sid) => get(`/api/sessions/${sid}/game`),
    gotoTurn: (sid, turn) => post(`/api/sessions/${sid}/turn`, { turn }),

    getNode: (sid, nid) => get(`/api/sessions/${sid}/nodes/${nid}`),
    legalMoves: (sid, nid) => get(`/api/sessions/${sid}/nodes/${nid}/legal-moves`),
    commit: (sid, nid, key) => post(`/api/sessions/${sid}/nodes/${nid}/commit`, { key }),

    solveEndgame: async (sid, nid, params, onTick, onJobId) => {
      const { jobId } = await post(`/api/sessions/${sid}/nodes/${nid}/solve/endgame`, params);
      if (onJobId) onJobId(jobId);
      return waitForJob(jobId, onTick);
    },
    solvePeg: async (sid, nid, params, onTick, onJobId) => {
      const { jobId } = await post(`/api/sessions/${sid}/nodes/${nid}/solve/peg`, params);
      if (onJobId) onJobId(jobId);
      return waitForJob(jobId, onTick);
    },
    pegSuggest: (sid, nid, tiles) => get(`/api/sessions/${sid}/nodes/${nid}/peg/suggest?tiles=${encodeURIComponent(tiles)}`),
    pegTrace: async (sid, nid, moveKey, eventuality, onTick) => {
      const { jobId } = await post(`/api/sessions/${sid}/nodes/${nid}/peg/trace`, { moveKey, eventuality });
      return waitForJob(jobId, onTick);
    },
    cancelJob: (jobId) => post(`/api/jobs/${jobId}/cancel`),
  };
})();
