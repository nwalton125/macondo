// Package endgameui implements a local HTTP+JSON server and browser UI for
// interactively running macondo's endgame and pre-endgame (peg) solvers,
// exploring resulting lines, and committing plays to walk through a tree of
// positions.
package endgameui

import (
	"github.com/domino14/word-golib/tilemapping"

	"github.com/domino14/macondo/board"
	"github.com/domino14/macondo/game"
	"github.com/domino14/macondo/move"
)

// MoveDTO is a plain, JSON-friendly description of a move, built from
// move.Move's accessor methods (following the same convention as
// gameanalysis/results.go rather than trying to serialize move.Move itself).
type MoveDTO struct {
	Key         string  `json:"key"` // stable key, unique among a node's legal moves; used to commit
	Description string  `json:"description"`
	Action      string  `json:"action"`
	Coords      string  `json:"coords,omitempty"`
	Row         int     `json:"row"`
	Col         int     `json:"col"`
	Vertical    bool    `json:"vertical"`
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

func moveToDTO(m *move.Move) MoveDTO {
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
	if m.Action() == move.MoveTypePlay {
		dto.Coords = m.BoardCoords()
		dto.Tiles = m.TilesString()
	} else if m.Action() == move.MoveTypeExchange {
		dto.Tiles = m.TilesStringExchange()
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

	Racks       [2]string `json:"racks"`
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
// path from the session root (path and players are parallel slices).
func nodeToPositionDTO(n *Node, path []*move.Move, players []int) PositionDTO {
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
		dto.Racks[i] = g.RackFor(i).String()
		dto.Scores[i] = g.PointsFor(i)
		dto.PlayerNames[i] = playerNickname(g, i)
	}
	for i, m := range path {
		dto.Path = append(dto.Path, CommittedMoveDTO{
			Ply:    i + 1,
			Player: players[i],
			Move:   moveToDTO(m),
		})
	}
	return dto
}

func playerNickname(g *game.Game, idx int) string {
	if h := g.History(); h != nil && idx < len(h.Players) {
		return h.Players[idx].Nickname
	}
	return ""
}
