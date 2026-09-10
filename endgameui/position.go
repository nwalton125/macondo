package endgameui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/domino14/word-golib/kwg"

	"github.com/domino14/macondo/board"
	"github.com/domino14/macondo/cgp"
	"github.com/domino14/macondo/config"
	"github.com/domino14/macondo/game"
	"github.com/domino14/macondo/gcgio"
	pb "github.com/domino14/macondo/gen/api/proto/macondo"
	"github.com/domino14/macondo/turnplayer"
)

// GCGSource identifies where to fetch a GCG's game history from, mirroring
// the shell's `load <file|xt|woogles|web> ...` command.
type GCGSource string

const (
	GCGSourceFile        GCGSource = "file"
	GCGSourceWoogles     GCGSource = "woogles"
	GCGSourceCrossTables GCGSource = "xt"
	GCGSourceWeb         GCGSource = "web"
)

// fetchGCGHistory loads a game history from the given source. ref is a
// local file path, a Woogles game ID, a cross-tables game ID, or a URL,
// depending on kind.
func fetchGCGHistory(cfg *config.Config, kind GCGSource, ref string) (*pb.GameHistory, error) {
	switch kind {
	case GCGSourceFile, "":
		return gcgio.ParseGCG(cfg, ref)
	case GCGSourceWoogles:
		return loadHistoryFromWoogles(cfg, ref)
	case GCGSourceCrossTables:
		return loadHistoryFromCrossTables(cfg, ref)
	case GCGSourceWeb:
		return loadHistoryFromWeb(cfg, ref)
	default:
		return nil, fmt.Errorf("unknown GCG source %q", kind)
	}
}

// loadHistoryFromWoogles fetches a game's GCG from Woogles by game ID, the
// same endpoint the shell's `load woogles <id>` command uses.
// gcgFetchClient bounds how long we'll wait on a third-party GCG source
// (Woogles/cross-tables/an arbitrary URL) — without this, an unresponsive
// or rate-limiting remote hangs the load request indefinitely with no
// feedback to the user.
var gcgFetchClient = &http.Client{Timeout: 20 * time.Second}

func loadHistoryFromWoogles(cfg *config.Config, gameID string) (*pb.GameHistory, error) {
	if gameID == "" {
		return nil, errors.New("need a Woogles game id")
	}
	req, err := http.NewRequest("POST", "https://woogles.io/api/game_service.GameMetadataService/GetGCG",
		strings.NewReader(`{"gameId": "`+gameID+`"}`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := gcgFetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var gcgObj struct {
		Gcg string `json:"gcg"`
	}
	if err := json.Unmarshal(body, &gcgObj); err != nil {
		return nil, err
	}
	if gcgObj.Gcg == "" {
		return nil, fmt.Errorf("woogles game %q not found or has no GCG", gameID)
	}
	return gcgio.ParseGCGFromReader(cfg, strings.NewReader(gcgObj.Gcg))
}

// loadHistoryFromCrossTables fetches a game's annotated GCG from
// cross-tables.com by game ID, the same source the shell's `load xt <id>`
// command uses.
func loadHistoryFromCrossTables(cfg *config.Config, gameIDStr string) (*pb.GameHistory, error) {
	id, err := strconv.Atoi(gameIDStr)
	if err != nil {
		return nil, errors.New("cross-tables game id must be numeric")
	}
	prefix := strconv.Itoa(id / 100)
	xtpath := "https://www.cross-tables.com/annotated/selfgcg/" + prefix + "/anno" + gameIDStr + ".gcg"

	req, err := http.NewRequest("GET", xtpath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Macondo-EndgameUI")
	resp, err := gcgFetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cross-tables returned %s for game %s", resp.Status, gameIDStr)
	}
	return gcgio.ParseGCGFromReader(cfg, resp.Body)
}

func loadHistoryFromWeb(cfg *config.Config, url string) (*pb.GameHistory, error) {
	resp, err := gcgFetchClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return gcgio.ParseGCGFromReader(cfg, resp.Body)
}

// LexiconOptions lists what's available locally for a "new position" form.
type LexiconOptions struct {
	Lexicons            []string `json:"lexicons"`
	LetterDistributions []string `json:"letterDistributions"`
}

func ListLexiconOptions(cfg *config.Config) LexiconOptions {
	opts := LexiconOptions{}
	dataPath := cfg.GetString(config.ConfigDataPath)

	if entries, err := os.ReadDir(filepath.Join(dataPath, "lexica", "gaddag")); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".kwg") {
				opts.Lexicons = append(opts.Lexicons, strings.TrimSuffix(e.Name(), ".kwg"))
			}
		}
	}
	sort.Strings(opts.Lexicons)

	if entries, err := os.ReadDir(filepath.Join(dataPath, "letterdistributions")); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				opts.LetterDistributions = append(opts.LetterDistributions, e.Name())
			}
		}
	}
	sort.Strings(opts.LetterDistributions)
	return opts
}

// GCGFileInfo describes one .gcg file available to load.
type GCGFileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ListGCGFiles lists .gcg files directly under dir (non-recursive).
func ListGCGFiles(dir string) ([]GCGFileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []GCGFileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".gcg") {
			continue
		}
		out = append(out, GCGFileInfo{Name: e.Name(), Path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GCGSummary describes a parsed GCG file enough to drive a turn picker.
type GCGSummary struct {
	Path        string    `json:"path"`
	NumTurns    int       `json:"numTurns"`
	PlayerNames [2]string `json:"playerNames"`
	Lexicon     string    `json:"lexicon"`
}

func SummarizeGCG(cfg *config.Config, kind GCGSource, ref string) (*GCGSummary, error) {
	history, err := fetchGCGHistory(cfg, kind, ref)
	if err != nil {
		return nil, err
	}
	s := &GCGSummary{Path: ref, NumTurns: len(history.Events), Lexicon: history.Lexicon}
	for i := 0; i < 2 && i < len(history.Players); i++ {
		s.PlayerNames[i] = history.Players[i].Nickname
	}
	return s, nil
}

// getKWG loads (downloading if necessary, like the shell does) the word
// graph for a lexicon.
func getKWG(cfg *config.Config, lexiconName string) (*kwg.KWG, error) {
	if err := turnplayer.EnsureKWG(lexiconName, cfg.WGLConfig()); err != nil {
		return nil, fmt.Errorf("fetching lexicon %q: %w", lexiconName, err)
	}
	return kwg.GetKWG(cfg.WGLConfig(), lexiconName)
}

func modeForGame(g *game.Game) string {
	if g.Bag().TilesRemaining() == 0 {
		return "endgame"
	}
	return "peg"
}

// LoadFromGCG loads a GCG game (from a file, Woogles, cross-tables, or a
// URL) and creates a new session rooted at its first turn. The full game
// remains attached to the session so the client can step through every
// turn afterward (see Session.NodeForTurn/Transcript) instead of having to
// pick a turn upfront.
func LoadFromGCG(cfg *config.Config, store *SessionStore, kind GCGSource, ref string) (*Session, *Node, error) {
	history, err := fetchGCGHistory(cfg, kind, ref)
	if err != nil {
		return nil, nil, err
	}
	// gcgio always resets ChallengeRule to VOID on the returned history
	// ("since we don't know or care what it is") — but VOID makes
	// game.ValidateMove strictly re-check word legality on every replayed
	// play, which breaks replay of any real game containing a phony that
	// was legitimately played and later challenged off (a PHONY_TILES_RETURNED
	// event). Use SINGLE instead, exactly like gcgio's own parser does
	// internally for the same reason: we're mechanically replaying an
	// already-adjudicated log, not re-refereeing it live.
	history.ChallengeRule = pb.ChallengeRule_SINGLE
	lexicon := history.Lexicon
	if lexicon == "" {
		lexicon = cfg.GetString(config.ConfigDefaultLexicon)
	}
	gd, err := getKWG(cfg, lexicon)
	if err != nil {
		return nil, nil, err
	}
	boardLayout, ldName, variant := game.HistoryToVariant(history)
	rules, err := game.NewBasicGameRules(cfg, lexicon, boardLayout, ldName, game.CrossScoreAndSet, variant)
	if err != nil {
		return nil, nil, err
	}
	g, err := game.NewFromHistory(history, rules, 0)
	if err != nil {
		return nil, nil, err
	}
	sess := NewSession(cfg, gd, g, modeForGame(g))
	sess.SetGameSource(history, rules)
	store.Add(sess)
	root, _ := sess.Node(sess.RootID())
	return sess, root, nil
}

// buildTranscript replays history turn-by-turn (using the same incremental
// approach game.PlayToTurn uses internally) capturing each move's
// Quackle-notated description and the running score, for a game-log view.
func buildTranscript(history *pb.GameHistory, rules *game.GameRules) ([]TranscriptEntryDTO, error) {
	g, err := game.NewFromHistory(history, rules, 0)
	if err != nil {
		return nil, err
	}
	entries := make([]TranscriptEntryDTO, 0, len(history.Events))
	for t := 0; t < len(history.Events); t++ {
		evt := history.Events[t]
		m, err := game.MoveFromEvent(evt, g.Alphabet(), g.Board())
		if err != nil {
			return nil, err
		}
		// The end-of-game bonus/penalty pseudo-moves (move.NewBonusScoreMove
		// / move.NewLostScoreMove, used for GameEvent_END_RACK_PTS/
		// _END_RACK_PENALTY etc.) never get an alphabet set by either
		// constructor or by MoveFromEvent, so any alphabet-dependent string
		// method on them (which we need, to show their tiles) panics.
		m.SetAlphabet(g.Alphabet())
		dto := moveToDTO(m, g.Board())
		if err := g.PlayTurn(t); err != nil {
			return nil, err
		}
		entries = append(entries, TranscriptEntryDTO{
			Turn:   t + 1,
			Player: int(evt.PlayerIndex),
			Move:   dto,
			Scores: [2]int{g.PointsFor(0), g.PointsFor(1)},
		})
	}
	return entries, nil
}

// LoadFromCGP parses a CGP string (used both for pasted CGP and for the
// manual position editor, which assembles one client-side) into a new
// session.
func LoadFromCGP(cfg *config.Config, store *SessionStore, cgpStr string) (*Session, *Node, error) {
	parsed, err := cgp.ParseCGP(cfg, cgpStr)
	if err != nil {
		return nil, nil, err
	}
	// ParseCGP places board tiles directly (via NewFromSnapshot) rather than
	// replaying moves, so cross-sets are never computed; movegen needs them
	// to find any plays at all.
	parsed.Game.RecalculateBoard()
	lexicon := parsed.Game.LexiconName()
	gd, err := getKWG(cfg, lexicon)
	if err != nil {
		return nil, nil, err
	}
	sess := NewSession(cfg, gd, parsed.Game, modeForGame(parsed.Game))
	store.Add(sess)
	root, _ := sess.Node(sess.RootID())
	return sess, root, nil
}

// BuildManualCGP assembles a CGP string from a manual-entry form: one row
// string per board row (already in CGP row syntax, e.g. "8DORMINE1"),
// per-player racks and scores, and a lexicon/letter-distribution pair.
func BuildManualCGP(rows []string, racks [2]string, scores [2]int, lexiconName, letterDistName string) string {
	boardPart := strings.Join(rows, "/")
	if letterDistName == "" {
		letterDistName = "english"
	}
	return fmt.Sprintf("%s %s/%s %d/%d 0 lex %s;ld %s;",
		boardPart, racks[0], racks[1], scores[0], scores[1], lexiconName, letterDistName)
}

// EmptyBoardRows returns CGP-syntax row strings for a blank board of the
// given layout, used to seed the manual editor.
func EmptyBoardRows(layoutName string) []string {
	var bd []string
	switch layoutName {
	case board.SuperCrosswordGameLayout:
		bd = board.SuperCrosswordGameBoard
	default:
		bd = board.CrosswordGameBoard
	}
	dim := len(bd)
	rows := make([]string, dim)
	for i := range rows {
		rows[i] = fmt.Sprintf("%d", dim)
	}
	return rows
}
