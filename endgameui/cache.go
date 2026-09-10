package endgameui

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/domino14/word-golib/tilemapping"

	"github.com/domino14/macondo/game"
)

// positionSignature returns a content hash of a game position (board,
// racks, scores, bag contents, turn, lexicon) so that identical positions
// reached via different tree paths (or different sessions) share solve
// results. It intentionally excludes anything not relevant to what a
// solver would compute (move history, player nicknames, etc).
func positionSignature(g *game.Game) string {
	h := sha256.New()
	b := g.Board()
	dim := b.Dim()
	for r := 0; r < dim; r++ {
		for c := 0; c < dim; c++ {
			if b.HasLetter(r, c) {
				fmt.Fprintf(h, "%d,", b.GetLetter(r, c))
			} else {
				fmt.Fprint(h, "_,")
			}
		}
	}
	for i := 0; i < g.NumPlayers(); i++ {
		tiles := g.RackFor(i).TilesOn()
		sort.Slice(tiles, func(a, bIdx int) bool { return tiles[a] < tiles[bIdx] })
		fmt.Fprintf(h, "|rack%d:%v|score%d:%d", i, tiles, i, g.PointsFor(i))
	}
	bagTiles := append([]tilemapping.MachineLetter(nil), g.Bag().Peek()...)
	sort.Slice(bagTiles, func(a, bIdx int) bool { return bagTiles[a] < bagTiles[bIdx] })
	fmt.Fprintf(h, "|bag:%v|onturn:%d|lex:%s", bagTiles, g.PlayerOnTurn(), g.LexiconName())
	return hex.EncodeToString(h.Sum(nil))
}

// EndgameCacheEntry is what's stored per position+plies key: the largest
// number of variations computed so far for it.
type EndgameCacheEntry struct {
	NumLines   int
	Capped     bool
	Variations []VariationDTO
}

// SolveCache is a small bounded LRU cache, content-addressed by position (+
// engine parameters), shared across all sessions in the process. The
// negamax/preendgame solvers prune search, so a request for more lines (or
// tiles-left) than were previously computed for the same position is a
// legitimate cache miss that triggers real recomputation rather than a bug.
type SolveCache struct {
	mu       sync.Mutex
	ll       *list.List
	items    map[string]*list.Element
	capacity int
}

type cacheEntry struct {
	key   string
	value any
}

func NewSolveCache(capacity int) *SolveCache {
	return &SolveCache{
		ll:       list.New(),
		items:    make(map[string]*list.Element),
		capacity: capacity,
	}
}

func (c *SolveCache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*cacheEntry).value, true
}

func (c *SolveCache) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*cacheEntry).value = value
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&cacheEntry{key: key, value: value})
	c.items[key] = el
	for c.ll.Len() > c.capacity {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.ll.Remove(back)
		delete(c.items, back.Value.(*cacheEntry).key)
	}
}
