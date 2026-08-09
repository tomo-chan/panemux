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
// matching against a relay-observed row claiming From == Sentinel.
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
// one merely claiming From == Sentinel, since send.sh --force never
// checks From against a roster.
type ownSendLedger struct { //nolint:govet // fieldalignment: clarity preferred
	mu sync.Mutex
	// entries holds one expiry per pending Record call for a given key,
	// oldest first. A plain map[ownSendKey]time.Time (one entry per key)
	// let two genuine broadcasts with identical (destHost, team, to, body)
	// collapse into a single overwritable entry: the second real send.sh
	// row then found nothing to Consume and was dropped through the same
	// path used for a forged Sentinel row — see the PR #163 review finding
	// this is the regression fix for. A slice per key lets each Record
	// call be independently matched and consumed by its own row.
	entries map[ownSendKey][]time.Time
	ttl     time.Duration
	nowFn   func() time.Time
}

func newOwnSendLedger(ttl time.Duration) *ownSendLedger {
	return &ownSendLedger{
		entries: make(map[ownSendKey][]time.Time),
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
// A repeated call with the same (destHost, team, to, body) appends a second,
// independently consumable entry rather than overwriting the first — see
// ownSendLedger.entries' doc comment for why that distinction matters.
func (l *ownSendLedger) Record(destHost, team, to, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: hashBody(body)}
	l.entries[key] = append(l.entries[key], l.nowFn().Add(l.ttl))
}

// Consume reports whether (destHost, team, to, body) matches a
// still-live ledger entry, and deletes it either way (a consumed entry is
// one-shot; an expired one is stale bookkeeping, not a legitimate second
// match). When multiple entries share a key (repeated identical Records),
// the oldest is consumed first (FIFO), so N genuine duplicate sends can
// each be matched by their own relayed row. Returns false for a key with
// no remaining entries.
func (l *ownSendLedger) Consume(destHost, team, to, body string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: hashBody(body)}
	expiries := l.entries[key]
	if len(expiries) == 0 {
		return false
	}
	expiry := expiries[0]
	if len(expiries) == 1 {
		delete(l.entries, key)
	} else {
		l.entries[key] = expiries[1:]
	}
	return !l.nowFn().After(expiry)
}

// pruneLocked drops expired entries, including any key whose entries are
// all expired. Must be called with l.mu held.
func (l *ownSendLedger) pruneLocked() {
	now := l.nowFn()
	for k, expiries := range l.entries {
		live := expiries[:0]
		for _, expiry := range expiries {
			if !now.After(expiry) {
				live = append(live, expiry)
			}
		}
		if len(live) == 0 {
			delete(l.entries, k)
		} else {
			l.entries[k] = live
		}
	}
}
