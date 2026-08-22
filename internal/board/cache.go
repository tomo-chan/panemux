package board

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
	"time"
)

// defaultBoardCacheHistoryLimit bounds the in-memory history ring buffer.
// It comfortably exceeds the relay's cold-start backfill limit (see
// docs/agent-board.md's Cross-host relay section) so a freshly repopulated
// cache isn't immediately trimmed back down.
const defaultBoardCacheHistoryLimit = 2000

// CachedRow pairs a Row with the BoardCache-local Seq it was assigned when
// appended. Exported because GET /api/board/messages?since=<seq> needs the
// Seq back from every returned row to use as its next since cursor — a bare
// []Row would discard exactly the value the caller needs.
type CachedRow struct {
	Row Row
	Seq int64
}

// BoardCache is the in-memory, panemux-owned view of recent board activity.
// Only the relay (added in a later phase) writes to it, as a side effect of
// its own polling; dashboard-facing endpoints only ever read it, never
// calling AgmsgClient directly at request time. Seq is BoardCache's own
// monotonically increasing, panemux-local sequence number — assigned here
// because agmsg IDs from different hosts are not comparable or even
// guaranteed non-colliding with each other.
type BoardCache struct {
	status     map[string]Status
	now        func() time.Time
	epoch      string
	history    []CachedRow
	nextSeq    int64
	maxHistory int
	mu         sync.RWMutex
}

// NewBoardCache returns an empty BoardCache, as it is on every panemux
// process start (the cache is never persisted to disk).
func NewBoardCache() *BoardCache {
	return &BoardCache{
		status:     make(map[string]Status),
		now:        time.Now,
		epoch:      newCacheEpoch(),
		maxHistory: defaultBoardCacheHistoryLimit,
	}
}

// Epoch identifies this particular cache instance. Because the cache is
// never persisted, Seq restarts at 1 on every process start, and a client
// holding a since cursor from before a restart would poll forever against
// rows numbered below it — its feed stopping silently rather than visibly
// failing. Epoch gives that client something to compare: a changed value
// means the cursor it holds refers to a cache that no longer exists and must
// be reset. It is deliberately opaque; callers must only test it for
// equality, never parse or order it.
func (c *BoardCache) Epoch() string {
	return c.epoch
}

// newCacheEpoch returns an opaque per-instance marker. Randomness only has
// to make an accidental collision between two caches implausible; it is not
// a security boundary, so a failed read from the system source falls back to
// the wall clock rather than failing process start over it.
func newCacheEpoch() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}

// RecordStatus stores s as paneID's latest self-reported status, overwriting
// any previous status for that pane. UpdatedAt is set here, not by the
// caller, to when the write actually happened.
func (c *BoardCache) RecordStatus(paneID string, s Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s.UpdatedAt = c.now()
	c.status[paneID] = s
}

// AppendMessage appends r to the history ring buffer, assigning it the next
// Seq. When the buffer exceeds its bound, the oldest rows are dropped —
// the same accepted truncation tradeoff the relay's own bounded polling
// already makes (see docs/agent-board.md).
func (c *BoardCache) AppendMessage(r Row) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSeq++
	c.history = append(c.history, CachedRow{Seq: c.nextSeq, Row: r})
	if overflow := len(c.history) - c.maxHistory; overflow > 0 {
		c.history = c.history[overflow:]
	}
}

// StatusSnapshot returns a copy of the current pane-ID -> Status map.
func (c *BoardCache) StatusSnapshot() map[string]Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]Status, len(c.status))
	for k, v := range c.status {
		out[k] = v
	}
	return out
}

// MessagesSince returns every history row (with its Seq) whose Seq is
// greater than afterSeq, oldest first. An empty or fully-consumed history
// returns nil, never an error.
func (c *BoardCache) MessagesSince(afterSeq int64) []CachedRow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []CachedRow
	for _, cr := range c.history {
		if cr.Seq > afterSeq {
			out = append(out, cr)
		}
	}
	return out
}
