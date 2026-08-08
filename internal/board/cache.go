package board

import (
	"sync"
	"time"
)

// defaultMaxHistory bounds the in-memory history ring buffer. It is
// deliberately generous: BoardCache is the only source GET
// /api/board/messages reads from (see docs/agent-board.md's Architecture
// section), so trimming too aggressively would silently drop rows a client
// paging with ?since=<seq> could otherwise still retrieve.
const defaultMaxHistory = 5000

// Message pairs a Row with the panemux-local sequence number BoardCache
// assigned it, which is what GET /api/board/messages?since=<seq> actually
// paginates on (agmsg's own per-host ids are not comparable across hosts).
type Message struct { //nolint:govet // fieldalignment: clarity preferred
	Seq int64
	Row Row
}

// BoardCache is the in-memory, panemux-owned view of recent board activity.
// Only the relay writes to it, as a side effect of the polling it already
// does for message forwarding; both dashboard-facing endpoints only ever
// read it, never calling AgmsgClient directly at request time.
type BoardCache struct { //nolint:govet // fieldalignment: clarity preferred
	mu         sync.RWMutex
	status     map[string]Status
	nextSeq    int64
	history    []Message
	maxHistory int
	nowFn      func() time.Time
}

// NewBoardCache creates an empty BoardCache with the default history bound.
func NewBoardCache() *BoardCache {
	return NewBoardCacheWithLimit(defaultMaxHistory)
}

// NewBoardCacheWithLimit creates an empty BoardCache bounding history to at
// most limit entries (oldest dropped first).
func NewBoardCacheWithLimit(limit int) *BoardCache {
	return &BoardCache{
		status:     make(map[string]Status),
		maxHistory: limit,
		nowFn:      time.Now,
	}
}

// RecordStatus stores s as the latest known status for paneID, overwriting
// any previous status for the same pane. UpdatedAt is always set to now,
// regardless of what the caller passed in s.
func (c *BoardCache) RecordStatus(paneID string, s Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s.UpdatedAt = c.nowFn()
	c.status[paneID] = s
}

// AppendMessage appends r to history, assigning it the next monotonically
// increasing Seq, and returns that Seq.
func (c *BoardCache) AppendMessage(r Row) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSeq++
	seq := c.nextSeq
	c.history = append(c.history, Message{Seq: seq, Row: r})
	if c.maxHistory > 0 && len(c.history) > c.maxHistory {
		c.history = c.history[len(c.history)-c.maxHistory:]
	}
	return seq
}

// StatusSnapshot returns a copy of the current per-pane status map.
func (c *BoardCache) StatusSnapshot() map[string]Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]Status, len(c.status))
	for k, v := range c.status {
		out[k] = v
	}
	return out
}

// MessagesSince returns every history entry whose Seq is strictly greater
// than afterSeq, oldest first. An empty or zero afterSeq returns full
// history. A cache with no history yet (fresh start, or after a restart
// before the relay has repopulated it) returns an empty, non-nil-error
// result.
func (c *BoardCache) MessagesSince(afterSeq int64) []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Message, 0, len(c.history))
	for _, m := range c.history {
		if m.Seq > afterSeq {
			out = append(out, m)
		}
	}
	return out
}

// LatestSeq returns the most recently assigned Seq, or 0 if history is
// empty. Callers use this to compute the next ?since= cursor value.
func (c *BoardCache) LatestSeq() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nextSeq
}
