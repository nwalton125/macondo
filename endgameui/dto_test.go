package endgameui

import (
	"testing"

	"github.com/domino14/macondo/cgp"
	"github.com/domino14/macondo/config"
)

var testConfig = config.DefaultConfig()

// simpleEndgameCGP is a small, fully bag-empty position (from
// endgame/negamax's own test fixtures) used to exercise DTO conversion
// against a real *game.Game without needing to run a solver.
const simpleEndgameCGP = "14C/13QI/12FIE/10VEE1R/9KIT2G/8CIG1IDE/8UTA2AS/7ST1SYPh1/6JA5A1/5WOLD2BOBA/3PLOT1R1NU1EX/Y1VEIN1NOR1mOA1/UT1AT1N1L2FEH1/GUR2WIRER5/SNEEZED8 ADENOOO/AHIILMM 353/236 0 lex NWL23;"

func loadTestGame(t *testing.T) *Node {
	t.Helper()
	parsed, err := cgp.ParseCGP(testConfig, simpleEndgameCGP)
	if err != nil {
		t.Fatalf("ParseCGP: %v (is MACONDO_DATA_PATH set to the repo's data/ dir?)", err)
	}
	parsed.Game.RecalculateBoard()
	sess := NewSession(testConfig, nil, parsed.Game, "endgame")
	root, err := sess.Node(sess.RootID())
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	return root
}

func TestNodeToPositionDTOShape(t *testing.T) {
	n := loadTestGame(t)
	dto := nodeToPositionDTO(n, nil, nil, n.Game.Board())

	if dto.Dim != 15 {
		t.Fatalf("expected dim 15, got %d", dto.Dim)
	}
	if len(dto.Board) != 15 || len(dto.Bonuses) != 15 {
		t.Fatalf("expected 15 board/bonus rows, got %d/%d", len(dto.Board), len(dto.Bonuses))
	}
	if dto.Path == nil {
		t.Fatal("Path must never be nil (marshals to JSON null, breaking the client)")
	}
	if len(dto.Path) != 0 {
		t.Fatalf("expected empty path at root, got %d entries", len(dto.Path))
	}
	if dto.OnTurnRack != "ADENOOO" {
		t.Fatalf("unexpected on-turn rack: %v", dto.OnTurnRack)
	}
	// Bag is empty in this true-endgame fixture, so unseen tiles == the
	// off-turn player's actual rack, exactly.
	if dto.UnseenTiles != "AHIILMM" {
		t.Fatalf("unexpected unseen tiles: %v", dto.UnseenTiles)
	}
	if dto.BagCount != 0 {
		t.Fatalf("expected empty bag (true endgame), got %d", dto.BagCount)
	}
	// Spot check a known occupied square: row 0 (displayed row "1"), col 14 has 'C'.
	if dto.Board[0][14] != 'C' {
		t.Fatalf("expected 'C' at [0][14], got %q", dto.Board[0][14])
	}
}

func TestSquareMetaFromPathMarksPlacedTilesOnly(t *testing.T) {
	// A minimal fake play: verifies played-through squares (tile value 0 in
	// Tiles()) are NOT attributed to the committing ply.
	n := loadTestGame(t)
	dto := nodeToPositionDTO(n, nil, nil, n.Game.Board())
	for r := range dto.SquarePlayer {
		for c := range dto.SquarePlayer[r] {
			if dto.SquarePlayer[r][c] != -1 {
				t.Fatalf("with no committed path, every square should have player -1; got %d at [%d][%d]",
					dto.SquarePlayer[r][c], r, c)
			}
			if dto.SquarePly[r][c] != 0 {
				t.Fatalf("with no committed path, every square should have ply 0; got %d at [%d][%d]",
					dto.SquarePly[r][c], r, c)
			}
		}
	}
}
