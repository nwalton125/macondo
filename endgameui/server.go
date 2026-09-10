package endgameui

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/domino14/word-golib/tilemapping"

	"github.com/domino14/macondo/config"
)

// Server is the HTTP+JSON backend for the endgame/pre-endgame UI.
type Server struct {
	cfg      *config.Config
	sessions *SessionStore
	jobs     *JobManager
	cache    *SolveCache
	mux      *http.ServeMux
}

func NewServer(cfg *config.Config, staticFS fs.FS) *Server {
	s := &Server{
		cfg:      cfg,
		sessions: NewSessionStore(),
		jobs:     NewJobManager(),
		cache:    NewSolveCache(500),
		mux:      http.NewServeMux(),
	}
	s.routes(staticFS)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) routes(staticFS fs.FS) {
	s.mux.HandleFunc("GET /api/options", s.handleOptions)
	s.mux.HandleFunc("GET /api/gcg/list", s.handleGCGList)
	s.mux.HandleFunc("GET /api/gcg/summary", s.handleGCGSummary)

	s.mux.HandleFunc("POST /api/sessions/gcg", s.handleNewSessionFromGCG)
	s.mux.HandleFunc("POST /api/sessions/cgp", s.handleNewSessionFromCGP)
	s.mux.HandleFunc("POST /api/sessions/manual", s.handleNewSessionManual)

	s.mux.HandleFunc("GET /api/sessions/{sid}/game", s.handleGameTranscript)
	s.mux.HandleFunc("POST /api/sessions/{sid}/turn", s.handleGotoTurn)

	s.mux.HandleFunc("GET /api/sessions/{sid}/nodes/{nid}", s.handleGetNode)
	s.mux.HandleFunc("GET /api/sessions/{sid}/nodes/{nid}/legal-moves", s.handleLegalMoves)
	s.mux.HandleFunc("POST /api/sessions/{sid}/nodes/{nid}/commit", s.handleCommit)
	s.mux.HandleFunc("POST /api/sessions/{sid}/nodes/{nid}/solve/endgame", s.handleSolveEndgame)
	s.mux.HandleFunc("POST /api/sessions/{sid}/nodes/{nid}/solve/peg", s.handleSolvePeg)
	s.mux.HandleFunc("GET /api/sessions/{sid}/nodes/{nid}/peg/suggest", s.handlePegSuggest)
	s.mux.HandleFunc("POST /api/sessions/{sid}/nodes/{nid}/peg/trace", s.handlePegTrace)

	s.mux.HandleFunc("GET /api/jobs/{jid}", s.handleGetJob)
	s.mux.HandleFunc("POST /api/jobs/{jid}/cancel", s.handleCancelJob)

	s.mux.Handle("GET /", http.FileServerFS(staticFS))
}

func (s *Server) sessionAndNode(w http.ResponseWriter, r *http.Request) (*Session, *Node, bool) {
	sess, err := s.sessions.Get(r.PathValue("sid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, nil, false
	}
	n, err := sess.Node(r.PathValue("nid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, nil, false
	}
	return sess, n, true
}

func (s *Server) positionResponse(sess *Session, n *Node) (PositionDTO, error) {
	path, players, err := sess.PathTo(n.ID)
	if err != nil {
		return PositionDTO{}, err
	}
	root, err := sess.Node(sess.RootID())
	if err != nil {
		return PositionDTO{}, err
	}
	return nodeToPositionDTO(n, path, players, root.Game.Board()), nil
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListLexiconOptions(s.cfg))
}

func (s *Server) handleGCGList(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeErr(w, http.StatusBadRequest, errString("dir is required"))
		return
	}
	files, err := ListGCGFiles(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleGCGSummary(w http.ResponseWriter, r *http.Request) {
	kind := GCGSource(r.URL.Query().Get("kind"))
	ref := r.URL.Query().Get("ref")
	summary, err := SummarizeGCG(s.cfg, kind, ref)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

type newSessionResponse struct {
	SessionID string      `json:"sessionId"`
	Position  PositionDTO `json:"position"`
	HasGame   bool        `json:"hasGame"` // true if loaded from a GCG game (turn navigation available)
}

func (s *Server) respondNewSession(w http.ResponseWriter, sess *Session, root *Node) {
	pos, err := s.positionResponse(sess, root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, newSessionResponse{SessionID: sess.ID, Position: pos, HasGame: sess.HasGame()})
}

func (s *Server) handleNewSessionFromGCG(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"` // "file" (default), "woogles", "xt", or "web"
		Ref  string `json:"ref"`  // file path, Woogles game id, cross-tables game id, or URL
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	kind := GCGSource(req.Kind)
	if kind == "" {
		kind = GCGSourceFile
	}
	sess, root, err := LoadFromGCG(s.cfg, s.sessions, kind, req.Ref)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.respondNewSession(w, sess, root)
}

func (s *Server) handleNewSessionFromCGP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CGP string `json:"cgp"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, root, err := LoadFromCGP(s.cfg, s.sessions, req.CGP)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.respondNewSession(w, sess, root)
}

func (s *Server) handleNewSessionManual(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rows               []string  `json:"rows"`
		Racks              [2]string `json:"racks"`
		Scores             [2]int    `json:"scores"`
		Lexicon            string    `json:"lexicon"`
		LetterDistribution string    `json:"letterDistribution"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cgpStr := BuildManualCGP(req.Rows, req.Racks, req.Scores, req.Lexicon, req.LetterDistribution)
	sess, root, err := LoadFromCGP(s.cfg, s.sessions, cgpStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.respondNewSession(w, sess, root)
}

func (s *Server) handleGameTranscript(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessions.Get(r.PathValue("sid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	entries, err := sess.Transcript()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleGotoTurn(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessions.Get(r.PathValue("sid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var req struct {
		Turn int `json:"turn"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	n, err := sess.NodeForTurn(req.Turn)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	pos, err := s.positionResponse(sess, n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, pos)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	sess, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	pos, err := s.positionResponse(sess, n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, pos)
}

func (s *Server) handleLegalMoves(w http.ResponseWriter, r *http.Request) {
	sess, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	moves, err := LegalMoves(sess, n)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, moves)
}

func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	sess, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	child, err := sess.Commit(n.ID, req.Key)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	pos, err := s.positionResponse(sess, child)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, pos)
}

type jobResponse struct {
	JobID string `json:"jobId"`
}

func (s *Server) handleSolveEndgame(w http.ResponseWriter, r *http.Request) {
	sess, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	var req struct {
		NumLines string `json:"numLines"`
		Plies    int    `json:"plies"`
		Threads  int    `json:"threads"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.NumLines == "" {
		req.NumLines = "1"
	}
	params := EndgameSolveParams{NumLines: req.NumLines, Plies: req.Plies, Threads: req.Threads}
	job := s.jobs.Start(func(ctx context.Context) (any, error) {
		return SolveEndgame(ctx, sess, n, params, s.cache)
	})
	writeJSON(w, http.StatusAccepted, jobResponse{JobID: job.ID})
}

func (s *Server) handleSolvePeg(w http.ResponseWriter, r *http.Request) {
	sess, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	var req struct {
		EndgamePlies int `json:"endgamePlies"`
		Threads      int `json:"threads"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	params := PegSolveParams{EndgamePlies: req.EndgamePlies, Threads: req.Threads}
	job := s.jobs.Start(func(ctx context.Context) (any, error) {
		return SolvePeg(ctx, sess, n, params, s.cache)
	})
	writeJSON(w, http.StatusAccepted, jobResponse{JobID: job.ID})
}

func (s *Server) handlePegSuggest(w http.ResponseWriter, r *http.Request) {
	_, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	tilesStr := r.URL.Query().Get("tiles")
	mls, err := tilemapping.ToMachineLetters(strings.ToUpper(tilesStr), n.Game.Alphabet())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"suggested": SuggestEventuality(n.Game, mls)})
}

func (s *Server) handlePegTrace(w http.ResponseWriter, r *http.Request) {
	sess, n, ok := s.sessionAndNode(w, r)
	if !ok {
		return
	}
	var req struct {
		MoveKey     string `json:"moveKey"`
		Eventuality string `json:"eventuality"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	params := PegTraceParams{MoveKey: req.MoveKey, Eventuality: req.Eventuality}
	job := s.jobs.Start(func(ctx context.Context) (any, error) {
		return TracePeg(ctx, sess, n, params)
	})
	writeJSON(w, http.StatusAccepted, jobResponse{JobID: job.ID})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.jobs.Get(r.PathValue("jid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	status, result, errMsg := j.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "result": result, "error": errMsg})
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if err := s.jobs.Cancel(r.PathValue("jid")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type errString string

func (e errString) Error() string { return string(e) }
