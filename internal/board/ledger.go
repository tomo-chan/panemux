package board

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// defaultLedgerTTL bounds how long an own-send ledger entry stays matchable.
// It is sized for "a few poll intervals" at the relay's default poll
// interval (see relay.go), long enough that a slow poll cycle still sees a
// recent Send, short enough that the ledger does not grow unbounded.
const defaultLedgerTTL = 2 * time.Minute

// ownSendKey identifies a Send call panemux itself issued, for later
// matching against a relay-observed row claiming From == PanemuxSentinel.
// BodyHash is a dedup key, not a security boundary by itself — the ledger's
// job is matching a row to a real recent Send, not re-displaying content.
type ownSendKey struct {
	DestHost string
	Team     string
	To       string
	BodyHash string
}

// ownSendLedger is a short-lived, in-memory record of Send calls panemux
// itself has issued (the broadcast handler — see relay.go's from-validation
// and docs/agent-board.md's Security model). It is what lets the relay
// distinguish a row genuinely sent by panemux's own broadcast handler from
// one merely claiming From == "_panemux", since send.sh --force never
// checks From against a roster.
type ownSendLedger struct {
	mu      sync.Mutex
	entries map[ownSendKey]time.Time // value is expiry
	ttl     time.Duration
	nowFn   func() time.Time
}

func newOwnSendLedger(ttl time.Duration) *ownSendLedger {
	return &ownSendLedger{
		entries: make(map[ownSendKey]time.Time),
		ttl:     ttl,
		nowFn:   time.Now,
	}
}

func hashBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:16]) // truncated; dedup key only
}

// Record inserts an entry for a Send panemux is about to issue (or has just
// issued) to (destHost, team, to) with the given body, with a fresh TTL.
func (l *ownSendLedger) Record(destHost, team, to, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: hashBody(body)}
	l.entries[key] = l.nowFn().Add(l.ttl)
}

// Consume reports whether (destHost, team, to, body) matches a
// still-live ledger entry, and deletes it either way (a consumed entry is
// one-shot; an expired one is stale bookkeeping, not a legitimate second
// match). Returns false for an absent or expired entry.
func (l *ownSendLedger) Consume(destHost, team, to, body string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: hashBody(body)}
	expiry, ok := l.entries[key]
	if !ok {
		return false
	}
	delete(l.entries, key)
	return !l.nowFn().After(expiry)
}

// pruneLocked drops expired entries. Must be called with l.mu held.
func (l *ownSendLedger) pruneLocked() {
	now := l.nowFn()
	for k, expiry := range l.entries {
		if now.After(expiry) {
			delete(l.entries, k)
		}
	}
}
