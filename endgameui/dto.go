// Package endgameui implements a local HTTP+JSON server and browser UI for
// interactively running macondo's endgame and pre-endgame (peg) solvers,
// exploring resulting lines, and committing plays to walk through a tree of
// positions.
package endgameui

import (
	"fmt"
	"sort"

	"github.com/domino14/word-golib/tilemapping"

	"github.com/domino14/macondo/board"
	"github.com/domino14/macondo/game"
	"github.com/domino14/macondo/move"
)

// MoveDTO is a plain, JSON-friendly description of a move, built from
// move.Move's accessor methods (following the same convention as
// gameanalysis/results.go rather than trying to serialize move.Move itself).
type MoveDTO struct {
	Key         string `json:"key"` // stable key, unique among a node's legal moves; used to commit
	Description string `json:"description"`
	Action      string `json:"action"`
	Coords      string `json:"coords,omitempty"`
	Row         int    `json:"row"`
	Col         int    `json:"col"`
	Vertical    bool   `json:"vertical"`
	// Tiles is the *positional* per-square encoding (one character per
	// board square the play spans, '.' for a played-through square) — this
	// is what the client indexes into to know which board square each
	// letter lands on. It is never for display; Description already carries
	// the human-readable Quackle-style notation ("S(MOR)G(AS)...").
	Tiles       string  `json:"tiles,omitempty"`
	Leave       string  `json:"leave"`
	Score       int     `json:"score"`
	Equity      float64 `json:"equity"`
	TilesPlayed int     `json:"tilesPlayed"`
}

// moveKey returns a stable, unique-per-position key for a move, used so the
// browser can refer back to a move it was shown without us needing a full
// move (de)serialization layer.
func moveKey(m *move.Move) string {
	return m.ShortDescription()
}

// moveToDTO converts a move using bd, the board state immediately *before*
// the move is played, to render Description in Quackle-style notation —
// maximal runs of played-through tiles wrapped in parentheses showing the
// letters already on the board there (e.g. "S(MOR)G(AS)B(O)R(D)"). Tiles
// stays the plain positional (dot) encoding regardless, since the client
// indexes into it square-by-square; only Description is for display. bd is
// unused (may be nil) for non-Play moves.
func moveToDTO(m *move.Move, bd *board.GameBoard) MoveDTO {
	row, col, vertical := m.CoordsAndVertical()
	dto := MoveDTO{
		Key:         moveKey(m),
		Description: m.ShortDescription(),
		Action:      m.MoveTypeString(),
		Row:         row,
		Col:         col,
		Vertical:    vertical,
		Leave:       m.LeaveString(),
		Score:       m.Score(),
		Equity:      m.Equity(),
		TilesPlayed: m.TilesPlayed(),
	}
	switch m.Action() {
	case move.MoveTypePlay:
		dto.Coords = m.BoardCoords()
		dto.Tiles = m.TilesString() // positional: one char per square, '.' for played-through
		dto.Description = fmt.Sprintf("%3v %s", dto.Coords, bd.WordWithPlaythrough(m))
	case move.MoveTypeExchange:
		dto.Tiles = m.TilesStringExchange()
	case move.MoveTypePhonyTilesReturned:
		dto.Description = fmt.Sprintf("Phony challenged off (%+d)", m.Score())
	case move.MoveTypeChallengeBonus:
		dto.Description = fmt.Sprintf("Challenge bonus (%+d)", m.Score())
	case move.MoveTypeEndgameTiles:
		dto.Description = fmt.Sprintf("Unplayed tile penalty/bonus (%+d)", m.Score())
	case move.MoveTypeLostTileScore:
		dto.Description = fmt.Sprintf("Lost tile score (%+d)", m.Score())
	case move.MoveTypeLostScoreOnTime:
		dto.Description = fmt.Sprintf("Time penalty (%+d)", m.Score())
	}
	return dto
}

// VariationDTO is one ranked line returned by an endgame solve: a sequence
// of moves alternating between players, and the resulting spread.
type VariationDTO struct {
	Moves       []MoveDTO `json:"moves"`
	FinalSpread int       `json:"finalSpread"`
}

// PegOutcomeDTO is one bucket of opponent draws sharing the same verdict for
// a candidate pre-endgame play.
type PegOutcomeDTO struct {
	Tiles  string `json:"tiles"`
	Count  int    `json:"count"`
	Result string `json:"result"` // "win" | "draw" | "loss"
}

// PegPlayDTO describes one candidate play under pre-endgame analysis.
type PegPlayDTO struct {
	Move        MoveDTO         `json:"move"`
	Points      float32         `json:"points"`
	FoundLosses float32         `json:"foundLosses"`
	Spread      int             `json:"spread"`
	HasSpread   bool            `json:"hasSpread"`
	Ignored     bool            `json:"ignored"`
	Outcomes    []PegOutcomeDTO `json:"outcomes"`
}

// CommittedMoveDTO is one step already committed on the path from a
// session's root node down to a given node.
type CommittedMoveDTO struct {
	Ply    int     `json:"ply"` // 1-based ply index from the root
	Player int     `json:"player"`
	Move   MoveDTO `json:"move"`
}

// TranscriptEntryDTO is one turn of a loaded game's move list (a Quackle-
// style game log), independent of the solve-tree exploration a user builds
// on top of any given turn.
type TranscriptEntryDTO struct {
	Turn   int     `json:"turn"` // 1-based
	Player int     `json:"player"`
	Move   MoveDTO `json:"move"`
	Scores [2]int  `json:"scores"` // cumulative scores after this turn
}

// PositionDTO is a full snapshot of a node's position, including enough
// per-square metadata for the browser to color tiles by who/when they were
// committed, without it needing to replay any move logic itself.
type PositionDTO struct {
	NodeID   string `json:"nodeId"`
	ParentID string `json:"parentId,omitempty"`
	Mode     string `json:"mode"`

	Dim     int      `json:"dim"`
	Board   []string `json:"board"`   // dim rows; each rune is a letter (lowercase = blank) or '.'
	Bonuses []string `json:"bonuses"` // dim rows of raw bonus square chars

	// SquarePly[r][c] is 0 for a tile that existed before analysis started
	// (or the square is empty), otherwise the 1-based ply (index into Path)
	// at which the tile currently on that square was placed.
	SquarePly [][]int `json:"squarePly"`
	// SquarePlayer[r][c] is the player index that placed the tile currently
	// on that square, or -1 if not applicable.
	SquarePlayer [][]int `json:"squarePlayer"`

	// OnTurnRack is the rack of whoever is on turn — the only rack shown in
	// the UI. The other player's rack is deliberately never sent: showing it
	// would be information a real analyst wouldn't have. UnseenTiles is what
	// they'd actually know instead — the bag plus the other player's rack,
	// combined and sorted (when the bag is empty, as in a true endgame, this
	// happens to exactly spell out the opponent's rack, which is fine: that
	// case is genuinely deducible, not peeked at).
	OnTurnRack  string    `json:"onTurnRack"`
	UnseenTiles string    `json:"unseenTiles"`
	Scores      [2]int    `json:"scores"`
	OnTurn      int       `json:"onTurn"`
	BagCount    int       `json:"bagCount"`
	PlayerNames [2]string `json:"playerNames"`

	Path []CommittedMoveDTO `json:"path"`
}

// boardDTO renders a game's board into row strings and a bonus-square grid.
func boardDTO(b *board.GameBoard, alph *tilemapping.TileMapping) ([]string, []string) {
	dim := b.Dim()
	rows := make([]string, dim)
	bonuses := make([]string, dim)
	for r := 0; r < dim; r++ {
		rowRunes := make([]byte, dim)
		bonusRunes := make([]byte, dim)
		for c := 0; c < dim; c++ {
			bonusRunes[c] = byte(b.GetBonus(r, c))
			if b.HasLetter(r, c) {
				rowRunes[c] = []byte(b.GetLetter(r, c).UserVisible(alph, false))[0]
			} else {
				rowRunes[c] = '.'
			}
		}
		rows[r] = string(rowRunes)
		bonuses[r] = string(bonusRunes)
	}
	return rows, bonuses
}

// squareMetaFromPath walks the committed-move path and marks, for every
// square touched by a play, which ply (1-based) and player put a tile there
// last. Squares never touched by the path keep ply 0 (pre-existing/empty).
func squareMetaFromPath(dim int, path []*move.Move, players []int) ([][]int, [][]int) {
	ply := make([][]int, dim)
	player := make([][]int, dim)
	for r := 0; r < dim; r++ {
		ply[r] = make([]int, dim)
		player[r] = make([]int, dim)
		for c := 0; c < dim; c++ {
			player[r][c] = -1
		}
	}
	for i, m := range path {
		if m.Action() != move.MoveTypePlay {
			continue
		}
		row, col, vertical := m.CoordsAndVertical()
		tiles := m.Tiles()
		for j, t := range tiles {
			if t == 0 {
				continue // played-through; pre-existing tile, not this ply's
			}
			r, c := row, col
			if vertical {
				r += j
			} else {
				c += j
			}
			ply[r][c] = i + 1
			player[r][c] = players[i]
		}
	}
	return ply, player
}

// nodeToPositionDTO builds a PositionDTO for a node given its full committed
// path from the session root (path and players are parallel slices) and the
// root's board, needed to correctly render played-through tiles at each
// step (rootBoard is walked forward as the path is replayed).
func nodeToPositionDTO(n *Node, path []*move.Move, players []int, rootBoard *board.GameBoard) PositionDTO {
	g := n.Game
	alph := g.Alphabet()
	rows, bonuses := boardDTO(g.Board(), alph)
	ply, player := squareMetaFromPath(g.Board().Dim(), path, players)

	dto := PositionDTO{
		NodeID:       n.ID,
		ParentID:     n.ParentID,
		Mode:         n.Mode,
		Dim:          g.Board().Dim(),
		Board:        rows,
		Bonuses:      bonuses,
		SquarePly:    ply,
		SquarePlayer: player,
		OnTurn:       g.PlayerOnTurn(),
		BagCount:     g.Bag().TilesRemaining(),
		Path:         []CommittedMoveDTO{}, // never nil, so the client can always .map/.length it
	}
	for i := 0; i < 2 && i < g.NumPlayers(); i++ {
		dto.Scores[i] = g.PointsFor(i)
		dto.PlayerNames[i] = playerNickname(g, i)
	}
	dto.OnTurnRack = g.RackFor(g.PlayerOnTurn()).String()
	dto.UnseenTiles = unseenTilesString(g)
	var walkBoard *board.GameBoard
	if rootBoard != nil {
		walkBoard = rootBoard.Copy()
	}
	for i, m := range path {
		dto.Path = append(dto.Path, CommittedMoveDTO{
			Ply:    i + 1,
			Player: players[i],
			Move:   moveToDTO(m, walkBoard),
		})
		if walkBoard != nil && m.Action() == move.MoveTypePlay {
			walkBoard.PlaceMoveTiles(m)
		}
	}
	return dto
}

func playerNickname(g *game.Game, idx int) string {
	if h := g.History(); h != nil && idx < len(h.Players) {
		return h.Players[idx].Nickname
	}
	return ""
}

// unseenTilesString is the bag plus the off-turn player's rack, combined
// and sorted — everything the on-turn player doesn't actually know the
// location of. (With an empty bag, as in a true endgame, this is exactly
// the opponent's rack; that's a legitimate deduction, not a peek.)
func unseenTilesString(g *game.Game) string {
	offTurn := 1 - g.PlayerOnTurn()
	tiles := append([]tilemapping.MachineLetter(nil), g.Bag().Peek()...)
	tiles = append(tiles, g.RackFor(offTurn).TilesOn()...)
	sort.Slice(tiles, func(i, j int) bool { return tiles[i] < tiles[j] })
	return tilemapping.MachineWord(tiles).UserVisible(g.Alphabet())
}
