package endgameui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/domino14/word-golib/tilemapping"

	"github.com/domino14/macondo/board"
	"github.com/domino14/macondo/endgame/negamax"
	"github.com/domino14/macondo/game"
	"github.com/domino14/macondo/move"
	"github.com/domino14/macondo/movegen"
	"github.com/domino14/macondo/preendgame"
)

// EndgameSolveParams are the (client-controlled) knobs for one endgame
// solve, mirroring the shell's `endgame` command options.
type EndgameSolveParams struct {
	NumLines string // an integer as a string, or "all"
	Plies    int    // 0 = pick a sane default from the position
	Threads  int    // 0 = pick a sane default
}

const maxAllLines = 200

// resolveNumLines turns the client's numLines request ("all" or an int)
// into a concrete count, using movegen to size "all" against the actual
// number of legal first plies available.
func resolveNumLines(g *game.Game, mg *movegen.GordonGenerator, numLines string) (int, bool, error) {
	if strings.EqualFold(numLines, "all") {
		rack := g.RackFor(g.PlayerOnTurn())
		plays := mg.GenAll(rack, false)
		n := len(plays)
		if n < 1 {
			n = 1
		}
		capped := false
		if n > maxAllLines {
			n = maxAllLines
			capped = true
		}
		return n, capped, nil
	}
	n := 0
	if _, err := fmt.Sscanf(numLines, "%d", &n); err != nil || n < 1 {
		return 0, false, fmt.Errorf("numLines must be a positive integer or \"all\", got %q", numLines)
	}
	return n, false, nil
}

func defaultPlies(g *game.Game) int {
	n := g.RackFor(0).NumTiles() + g.RackFor(1).NumTiles()
	if n < 4 {
		n = 4
	}
	return int(n)
}

// SolveEndgameResult is the JSON-serializable result of an endgame solve.
type SolveEndgameResult struct {
	Variations []VariationDTO `json:"variations"`
	Capped     bool           `json:"capped"`
	RootOnTurn int            `json:"rootOnTurn"`
	// PliesUsed is the actual plies depth this solve ran at (resolved from
	// the request's 0/"auto" if the client didn't set one explicitly), so
	// the client can derive a consistent depth for a child position — one
	// fewer ply, since one move's worth of the search horizon was consumed
	// — instead of recomputing an unrelated default there.
	PliesUsed int `json:"pliesUsed"`
}

// SolveEndgame runs (or reuses a cached run of) the negamax endgame solver
// against a node's position and returns up to numLines ranked variations.
func SolveEndgame(ctx context.Context, sess *Session, n *Node, params EndgameSolveParams, cache *SolveCache) (*SolveEndgameResult, error) {
	g := n.Game
	if g.Bag().TilesRemaining() > 0 {
		return nil, errors.New("bag is not empty; use pre-endgame (peg) analysis for this position")
	}

	mg := movegen.NewGordonGenerator(sess.Gd, g.Board(), g.Bag().LetterDistribution())
	numLines, capped, err := resolveNumLines(g, mg, params.NumLines)
	if err != nil {
		return nil, err
	}
	plies := params.Plies
	if plies <= 0 {
		plies = defaultPlies(g)
	}
	threads := params.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	if numLines > 1 && threads < 2 {
		threads = 2
	}

	sig := positionSignature(g)
	cacheKey := fmt.Sprintf("endgame|%s|plies=%d", sig, plies)
	if cached, ok := cache.Get(cacheKey); ok {
		entry := cached.(*EndgameCacheEntry)
		if entry.NumLines >= numLines {
			vars := entry.Variations
			if len(vars) > numLines {
				vars = vars[:numLines]
			}
			recordMovesFromVariations(sess, n, vars)
			return &SolveEndgameResult{Variations: vars, Capped: entry.Capped, RootOnTurn: g.PlayerOnTurn(), PliesUsed: plies}, nil
		}
	}

	searchGame := g.Copy()
	searchGame.SetBackupMode(game.SimulationMode)
	mg2 := movegen.NewGordonGenerator(sess.Gd, searchGame.Board(), searchGame.Bag().LetterDistribution())

	solver := new(negamax.Solver)
	if err := solver.Init(mg2, searchGame); err != nil {
		return nil, err
	}
	solver.SetThreads(threads)
	solver.SetIterativeDeepening(true)
	solver.SetTranspositionTableOptim(true)
	if err := solver.SetParallelAlgorithm(negamax.ParallelAlgoAuto); err != nil {
		return nil, err
	}
	solver.SetSolveMultipleVariations(numLines)

	val, seq, err := solver.Solve(ctx, plies)
	if err != nil {
		return nil, err
	}

	// solver.Variations() is only populated when solving with more than one
	// variation (SetSolveMultipleVariations(n>1)); for the common n==1 case
	// it stays empty, so the best line always comes from Solve()'s own
	// return values, matching how the shell renders it (endgameSequenceString
	// for the primary line, then "Other variations" from Variations()[1:]).
	variations := make([]VariationDTO, 0, numLines)
	variations = append(variations, VariationDTO{
		FinalSpread: int(val) + g.CurrentSpread(),
		Moves:       buildLineMoveDTOs(g.Board(), seq),
	})

	pvLines := solver.Variations()
	for i := 1; i < len(pvLines); i++ {
		pv := &pvLines[i]
		pv.MaterializeFull()
		moves := make([]*move.Move, 0, pv.NumMoves())
		for j := 0; j < pv.NumMoves(); j++ {
			if pv.Moves[j] == nil {
				break
			}
			moves = append(moves, pv.Moves[j])
		}
		variations = append(variations, VariationDTO{
			FinalSpread: int(pv.Score()) + g.CurrentSpread(),
			Moves:       buildLineMoveDTOs(g.Board(), moves),
		})
	}

	entry := &EndgameCacheEntry{NumLines: numLines, Capped: capped, Variations: variations}
	cache.Set(cacheKey, entry)
	recordMovesFromVariations(sess, n, variations)

	return &SolveEndgameResult{Variations: variations, Capped: capped, RootOnTurn: g.PlayerOnTurn(), PliesUsed: plies}, nil
}

// buildLineMoveDTOs converts a sequence of alternating-player moves into
// MoveDTOs, walking a copy of rootBoard forward so each move's
// played-through tiles are rendered against the board state as it stood
// right before that move (Quackle-style parens, not bare dots).
func buildLineMoveDTOs(rootBoard *board.GameBoard, moves []*move.Move) []MoveDTO {
	bd := rootBoard.Copy()
	dtos := make([]MoveDTO, 0, len(moves))
	for _, m := range moves {
		dtos = append(dtos, moveToDTO(m, bd))
		if m.Action() == move.MoveTypePlay {
			bd.PlaceMoveTiles(m)
		}
	}
	return dtos
}

// recordMovesFromVariations re-parses the first move of every variation
// against the node's real game (the cached DTOs came from a detached search
// copy) so Session.Commit can look them up by key. We only need the first
// ply of each variation since that's the only one ever committed directly.
func recordMovesFromVariations(sess *Session, n *Node, variations []VariationDTO) {
	g := n.Game
	mg := movegen.NewGordonGenerator(sess.Gd, g.Board(), g.Bag().LetterDistribution())
	rack := g.RackFor(g.PlayerOnTurn())
	plays := mg.GenAll(rack, false)
	byKey := make(map[string]*move.Move, len(plays))
	for _, p := range plays {
		byKey[moveKey(p)] = p
	}
	var toRecord []*move.Move
	for _, v := range variations {
		if len(v.Moves) == 0 {
			continue
		}
		if p, ok := byKey[v.Moves[0].Key]; ok {
			toRecord = append(toRecord, p)
		}
	}
	_ = sess.SetLastMoves(n.ID, toRecord)
}

// LegalMoves returns every legal move for the player on turn at a node,
// used for manual "also solve this play" pickers and generic validation.
func LegalMoves(sess *Session, n *Node) ([]MoveDTO, error) {
	g := n.Game
	mg := movegen.NewGordonGenerator(sess.Gd, g.Board(), g.Bag().LetterDistribution())
	rack := g.RackFor(g.PlayerOnTurn())
	plays := mg.GenAll(rack, g.Bag().TilesRemaining() > 0)
	if err := sess.SetLastMoves(n.ID, plays); err != nil {
		return nil, err
	}
	dtos := make([]MoveDTO, 0, len(plays))
	for _, p := range plays {
		dtos = append(dtos, moveToDTO(p, g.Board()))
	}
	sort.Slice(dtos, func(i, j int) bool { return dtos[i].Score > dtos[j].Score })
	return dtos, nil
}

// --- Pre-endgame (peg) ---

type PegSolveParams struct {
	EndgamePlies int
	Threads      int
	MaxSolutions int
}

type SolvePegResult struct {
	Plays []PegPlayDTO `json:"plays"`
}

func outcomeResultString(o preendgame.PEGOutcome) string {
	switch o {
	case preendgame.PEGWin:
		return "win"
	case preendgame.PEGDraw:
		return "draw"
	case preendgame.PEGLoss:
		return "loss"
	}
	return "unknown"
}

// SolvePeg runs (or reuses a cached run of) the pre-endgame solver against a
// node's position and returns every candidate play with its per-draw
// win/draw/loss outcomes.
func SolvePeg(ctx context.Context, sess *Session, n *Node, params PegSolveParams, cache *SolveCache) (*SolvePegResult, error) {
	g := n.Game
	if g.Bag().TilesRemaining() == 0 {
		return nil, errors.New("bag is empty; use endgame analysis for this position")
	}

	endgamePlies := params.EndgamePlies
	if endgamePlies <= 0 {
		endgamePlies = 4
	}
	sig := positionSignature(g)
	cacheKey := fmt.Sprintf("peg|%s|egplies=%d", sig, endgamePlies)
	if cached, ok := cache.Get(cacheKey); ok {
		result := cached.(*SolvePegResult)
		recordMovesFromPegPlays(sess, n, result.Plays)
		return result, nil
	}

	solver := new(preendgame.Solver)
	if err := solver.Init(g.Copy(), sess.Gd); err != nil {
		return nil, err
	}
	if params.Threads > 0 {
		solver.SetThreads(params.Threads)
	}
	solver.SetEndgamePlies(endgamePlies)

	plays, err := solver.Solve(ctx)
	if err != nil {
		return nil, err
	}

	dtos := make([]PegPlayDTO, 0, len(plays))
	for _, p := range plays {
		dto := PegPlayDTO{
			Move:        moveToDTO(p.Play, g.Board()),
			Points:      p.Points,
			FoundLosses: p.FoundLosses,
			Ignored:     p.Ignore,
			HasSpread:   p.HasSpread(),
			Outcomes:    []PegOutcomeDTO{},
		}
		if dto.HasSpread {
			dto.Spread = p.GetSpread()
		}
		for _, o := range p.OutcomesArray() {
			dto.Outcomes = append(dto.Outcomes, PegOutcomeDTO{
				Tiles:  tilemapping.MachineWord(o.Tiles()).UserVisible(g.Alphabet()),
				Count:  o.Count(),
				Result: outcomeResultString(o.OutcomeResult()),
			})
		}
		dtos = append(dtos, dto)
	}

	result := &SolvePegResult{Plays: dtos}
	cache.Set(cacheKey, result)
	recordMovesFromPegPlays(sess, n, result.Plays)
	return result, nil
}

func recordMovesFromPegPlays(sess *Session, n *Node, plays []PegPlayDTO) {
	g := n.Game
	mg := movegen.NewGordonGenerator(sess.Gd, g.Board(), g.Bag().LetterDistribution())
	rack := g.RackFor(g.PlayerOnTurn())
	legal := mg.GenAll(rack, true)
	byKey := make(map[string]*move.Move, len(legal))
	for _, p := range legal {
		byKey[moveKey(p)] = p
	}
	var toRecord []*move.Move
	for _, p := range plays {
		if m, ok := byKey[p.Move.Key]; ok {
			toRecord = append(toRecord, m)
		}
	}
	_ = sess.SetLastMoves(n.ID, toRecord)
}

// PegTraceParams requests a single-permutation "eventuality" trace: what
// happens for one fully-specified draw order of the remaining unseen tiles.
type PegTraceParams struct {
	MoveKey     string
	Eventuality string // letters, length must equal the bag's tile count at n
}

// NestedLevelDTO mirrors preendgame.NestedLevelExplanation, with the
// PEGOutcome verdict rendered as a string for the browser.
type NestedLevelDTO struct {
	Depth      int    `json:"depth"`
	OurRack    string `json:"ourRack"`
	OppRack    string `json:"oppRack"`
	BagContent string `json:"bagContent"`
	UnseenPool string `json:"unseenPool"`
	BestPlay   string `json:"bestPlay"`
	Verdict    string `json:"verdict"`
}

// PegTraceResult mirrors preendgame.EventualityExplanation (already a
// plain, human-readable struct) with PEGOutcome fields rendered as strings.
type PegTraceResult struct {
	OurPlay       string `json:"ourPlay"`
	OurScore      int    `json:"ourScore"`
	OurRackBefore string `json:"ourRackBefore"`
	OurRackAfter  string `json:"ourRackAfter"`
	BagBefore     string `json:"bagBefore"`
	BagAfter      string `json:"bagAfter"`
	OppRack       string `json:"oppRack"`

	TotalOppReplies int    `json:"totalOppReplies"`
	DecisiveOppPlay string `json:"decisiveOppPlay"`
	OppRackAfter    string `json:"oppRackAfter"`
	BagAfterOppPlay string `json:"bagAfterOppPlay"`

	Nested  []NestedLevelDTO `json:"nested"`
	Verdict string           `json:"verdict"`
}

func pegTraceResultFromExplanation(e *preendgame.EventualityExplanation) *PegTraceResult {
	r := &PegTraceResult{
		OurPlay: e.OurPlay, OurScore: e.OurScore,
		OurRackBefore: e.OurRackBefore, OurRackAfter: e.OurRackAfter,
		BagBefore: e.BagBefore, BagAfter: e.BagAfter, OppRack: e.OppRack,
		TotalOppReplies: e.TotalOppReplies, DecisiveOppPlay: e.DecisiveOppPlay,
		OppRackAfter: e.OppRackAfter, BagAfterOppPlay: e.BagAfterOppPlay,
		Verdict: outcomeResultString(e.Verdict),
	}
	for _, n := range e.Nested {
		r.Nested = append(r.Nested, NestedLevelDTO{
			Depth: n.Depth, OurRack: n.OurRack, OppRack: n.OppRack,
			BagContent: n.BagContent, UnseenPool: n.UnseenPool, BestPlay: n.BestPlay,
			Verdict: outcomeResultString(n.Verdict),
		})
	}
	return r
}

// SuggestEventuality builds a plausible full-length bag-tail permutation for
// one outcome bucket: the bucket's own (small) determinative tile set,
// padded with the rest of the bag's tiles in an arbitrary but deterministic
// order. Any completion of an outcome bucket must produce the same verdict,
// by definition of the bucketing, so this is a safe (if not unique) default
// for the trace input the user can edit before tracing.
func SuggestEventuality(g *game.Game, outcomeTiles []tilemapping.MachineLetter) string {
	remaining := append([]tilemapping.MachineLetter(nil), g.Bag().Peek()...)
	used := make(map[tilemapping.MachineLetter]int)
	for _, t := range outcomeTiles {
		used[t]++
	}
	full := append([]tilemapping.MachineLetter(nil), outcomeTiles...)
	for _, t := range remaining {
		if used[t] > 0 {
			used[t]--
			continue
		}
		full = append(full, t)
	}
	return tilemapping.MachineWord(full).UserVisible(g.Alphabet())
}

// TracePeg re-solves a single candidate play in single-threaded, single-perm
// "eventuality" explain mode targeting one exact draw order, and returns the
// solver's structured explanation of what happens along that line.
func TracePeg(ctx context.Context, sess *Session, n *Node, params PegTraceParams) (*PegTraceResult, error) {
	g := n.Game
	m, ok := n.lastMoves[params.MoveKey]
	if !ok {
		return nil, fmt.Errorf("move %q was not offered for this position; solve first", params.MoveKey)
	}
	nib := g.Bag().TilesRemaining()
	mls, err := tilemapping.ToMachineLetters(strings.ToUpper(params.Eventuality), g.Alphabet())
	if err != nil {
		return nil, err
	}
	if len(mls) != nib {
		return nil, fmt.Errorf("eventuality %q has %d tile(s) but the bag has %d — they must match exactly",
			params.Eventuality, len(mls), nib)
	}

	solver := new(preendgame.Solver)
	if err := solver.Init(g.Copy(), sess.Gd); err != nil {
		return nil, err
	}
	solver.SetThreads(1)
	solver.SetIterativeDeepening(false)
	solver.SetSkipTiebreaker(true)
	solver.SetSolveOnly([]*move.Move{m})
	solver.SetTraceTargetBagTail(mls)
	solver.SetTraceOnce(true)
	solver.SetExplainMode(true)

	if _, err := solver.Solve(ctx); err != nil {
		return nil, err
	}
	res := solver.ExplainResult()
	if res == nil {
		return nil, errors.New("solve completed without producing an explanation for this draw")
	}
	return pegTraceResultFromExplanation(res), nil
}
