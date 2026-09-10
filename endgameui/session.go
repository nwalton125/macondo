package endgameui

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/domino14/word-golib/kwg"

	"github.com/domino14/macondo/config"
	"github.com/domino14/macondo/game"
	pb "github.com/domino14/macondo/gen/api/proto/macondo"
	"github.com/domino14/macondo/move"
)

// Node is one position in a session's explored tree. The root node is the
// position the user loaded (from a GCG turn or a CGP string); every other
// node is reached by committing a move from its parent.
type Node struct {
	ID              string
	ParentID        string
	Game            *game.Game
	CommittedMove   *move.Move // move that produced this node from its parent; nil for root
	CommittedPlayer int
	Mode            string            // "endgame" | "peg"
	Children        map[string]string // moveKey -> child node ID, for commit memoization

	// lastMoves holds the moves shown to the client for this node (from the
	// most recent solve, or from a legal-moves listing), keyed the same way
	// as MoveDTO.Key, so a later commit call can look a move up by key
	// instead of us needing to (de)serialize move.Move over JSON.
	lastMoves map[string]*move.Move
}

// Session holds one user's tree of explored positions for a single loaded
// game/lexicon. Sessions live only in memory for the life of the server
// process.
type Session struct {
	ID     string
	Config *config.Config
	Gd     *kwg.KWG

	mu     sync.Mutex
	nodes  map[string]*Node
	rootID string

	// Set only for sessions loaded from a GCG game (file/Woogles/cross-tables/
	// URL), enabling turn-by-turn navigation through the recorded game
	// independent of the solve-tree (commit-based) exploration above.
	history    *pb.GameHistory
	rules      *game.GameRules
	turnNodes  map[int]string // turn number -> node ID, memoized
	transcript []TranscriptEntryDTO
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSession creates a session rooted at the given game position.
func NewSession(cfg *config.Config, gd *kwg.KWG, g *game.Game, mode string) *Session {
	s := &Session{
		ID:     newID(),
		Config: cfg,
		Gd:     gd,
		nodes:  make(map[string]*Node),
	}
	root := &Node{
		ID:              newID(),
		Game:            g,
		CommittedPlayer: -1,
		Mode:            mode,
		Children:        make(map[string]string),
	}
	s.rootID = root.ID
	s.nodes[root.ID] = root
	return s
}

func (s *Session) RootID() string { return s.rootID }

func (s *Session) Node(id string) (*Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return nil, fmt.Errorf("node %q not found", id)
	}
	return n, nil
}

// PathTo returns the sequence of committed moves and the player who made
// each, from the session root down to (and including) the given node.
func (s *Session) PathTo(id string) ([]*move.Move, []int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var moves []*move.Move
	var players []int
	cur, ok := s.nodes[id]
	if !ok {
		return nil, nil, fmt.Errorf("node %q not found", id)
	}
	for cur.CommittedMove != nil {
		moves = append(moves, cur.CommittedMove)
		players = append(players, cur.CommittedPlayer)
		parent, ok := s.nodes[cur.ParentID]
		if !ok {
			return nil, nil, fmt.Errorf("broken tree: parent %q of %q not found", cur.ParentID, cur.ID)
		}
		cur = parent
	}
	// reverse (we walked leaf -> root)
	for i, j := 0, len(moves)-1; i < j; i, j = i+1, j-1 {
		moves[i], moves[j] = moves[j], moves[i]
		players[i], players[j] = players[j], players[i]
	}
	return moves, players, nil
}

// SetLastMoves records the moves shown to the client for a node (from a
// solve result or a legal-move listing) so that Commit can resolve a
// client-supplied key back to a *move.Move.
func (s *Session) SetLastMoves(nodeID string, moves []*move.Move) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node %q not found", nodeID)
	}
	if n.lastMoves == nil {
		n.lastMoves = make(map[string]*move.Move)
	}
	for _, m := range moves {
		n.lastMoves[moveKey(m)] = m
	}
	return nil
}

// Commit plays the move identified by key (previously surfaced to the
// client via SetLastMoves) from node onto a fresh child node, memoizing the
// result so repeated expansions of the same move return the same node.
func (s *Session) Commit(nodeID, key string) (*Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, ok := s.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %q not found", nodeID)
	}
	if childID, ok := n.Children[key]; ok {
		return s.nodes[childID], nil
	}
	m, ok := n.lastMoves[key]
	if !ok {
		return nil, fmt.Errorf("move %q was not offered for this position; solve first", key)
	}

	childGame := n.Game.Copy()
	player := childGame.PlayerOnTurn()
	if err := childGame.PlayMove(m, false, 0); err != nil {
		return nil, fmt.Errorf("committing move: %w", err)
	}

	child := &Node{
		ID:              newID(),
		ParentID:        n.ID,
		Game:            childGame,
		CommittedMove:   m,
		CommittedPlayer: player,
		Mode:            n.Mode,
		Children:        make(map[string]string),
	}
	s.nodes[child.ID] = child
	n.Children[key] = child.ID
	return child, nil
}

// SetGameSource attaches the game history this session was loaded from,
// enabling NodeForTurn/Transcript navigation. Sessions loaded from a CGP
// string or the manual editor never call this and simply have no game to
// navigate.
func (s *Session) SetGameSource(history *pb.GameHistory, rules *game.GameRules) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = history
	s.rules = rules
	s.turnNodes = make(map[int]string)
}

// HasGame reports whether this session was loaded from a GCG game (as
// opposed to a CGP string or the manual editor).
func (s *Session) HasGame() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.history != nil
}

// NodeForTurn returns (creating and memoizing if necessary) the node
// representing the position at the start of the given turn of this
// session's game. Every turn is its own independent root: solve-tree
// exploration built on one turn is untouched when navigating to another,
// and is still there if you navigate back.
func (s *Session) NodeForTurn(turn int) (*Node, error) {
	s.mu.Lock()
	if s.history == nil {
		s.mu.Unlock()
		return nil, errors.New("session has no associated game to navigate")
	}
	if id, ok := s.turnNodes[turn]; ok {
		n := s.nodes[id]
		s.mu.Unlock()
		return n, nil
	}
	history, rules := s.history, s.rules
	s.mu.Unlock()

	g, err := game.NewFromHistory(history, rules, turn)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.turnNodes[turn]; ok {
		// Someone else built this turn's node while we didn't hold the lock.
		return s.nodes[id], nil
	}
	node := &Node{
		ID:              newID(),
		Game:            g,
		CommittedPlayer: -1,
		Mode:            modeForGame(g),
		Children:        make(map[string]string),
	}
	s.nodes[node.ID] = node
	s.turnNodes[turn] = node.ID
	return node, nil
}

// Transcript returns (building and caching on first call) the full list of
// this session's game moves, for a Quackle-style move-list/game log.
func (s *Session) Transcript() ([]TranscriptEntryDTO, error) {
	s.mu.Lock()
	if s.history == nil {
		s.mu.Unlock()
		return nil, errors.New("session has no associated game to navigate")
	}
	if s.transcript != nil {
		t := s.transcript
		s.mu.Unlock()
		return t, nil
	}
	history, rules := s.history, s.rules
	s.mu.Unlock()

	entries, err := buildTranscript(history, rules)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.transcript = entries
	s.mu.Unlock()
	return entries, nil
}

// SessionStore holds all sessions for the running server process.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

func (st *SessionStore) Add(s *Session) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sessions[s.ID] = s
}

func (st *SessionStore) Get(id string) (*Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return s, nil
}
