package board

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// defaultOwnSendLedgerTTL is how long a recorded Send stays matchable
// against a later-observed row. "A few poll intervals" per
// docs/agent-board.md's Package layout section — the relay this ledger
// backs polls every few seconds, so this comfortably covers a handful of
// poll cycles without accumulating stale entries indefinitely.
const defaultOwnSendLedgerTTL = 30 * time.Second

// ownSendKey identifies one Send call for own-send-ledger matching purposes.
// Body is stored only as a hash, since the ledger's job is matching, not
// re-displaying content.
type ownSendKey struct {
	DestHost string
	Team     string
	To       string
	BodyHash string
}

// ownSendLedger is a short-lived, in-memory record of Send calls panemux
// itself has issued (the broadcast handler and the command center), used
// only to verify a row the relay later observes with From == SystemID
// actually corresponds to one of panemux's own sends — send.sh --force
// never checks From against a roster, so an ordinary board pane could
// otherwise forge that identity. See docs/agent-board.md's Cross-host
// relay and Security model sections.
//
// entries is a multiset, not a set: it holds one expiry per occurrence of a
// key, not one expiry per distinct key. Two broadcasts with the identical
// (destHost, team, to, body) — a duplicate message, which is ordinary
// input, not an attack — are two real, independent sends that will each
// produce their own row on the destination host. A plain map keyed by
// ownSendKey can only remember one of them: the second Record would
// overwrite the first, so only one of the two real rows would ever
// Consume successfully and the other would be dropped as "invalid from"
// and silently vanish from history, even though panemux really did send
// both. Storing a slice of expiries per key — one appended per Record, one
// removed per Consume/Forget — lets the ledger represent "N sends with this
// shape are currently in flight or awaiting poll-back" instead of just
// "a send with this shape happened once."
type ownSendLedger struct {
	entries map[ownSendKey][]time.Time // value is one expiry per occurrence
	now     func() time.Time
	mu      sync.Mutex
	ttl     time.Duration
}

func newOwnSendLedger() *ownSendLedger {
	return &ownSendLedger{
		entries: make(map[ownSendKey][]time.Time),
		ttl:     defaultOwnSendLedgerTTL,
		now:     time.Now,
	}
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Record inserts one occurrence for a Send panemux itself just issued,
// matchable until it expires after l.ttl. A second Record for the same
// destHost/team/to/body adds a second, independent occurrence rather than
// overwriting the first — see ownSendLedger's own doc comment for why.
func (l *ownSendLedger) Record(destHost, team, to, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: bodyHash(body)}
	l.entries[key] = append(l.entries[key], l.now().Add(l.ttl))
}

// Consume reports whether at least one unexpired occurrence exists for the
// given send parameters. If so, it removes exactly one such occurrence (so
// two real, distinct sends with the same shape each get their own match)
// and returns true. Every already-expired occurrence encountered along the
// way is dropped too, as ordinary garbage collection, regardless of
// whether an unexpired one is also found.
func (l *ownSendLedger) Consume(destHost, team, to, body string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: bodyHash(body)}

	now := l.now()
	matched := false
	live := make([]time.Time, 0, len(l.entries[key]))
	for _, expiry := range l.entries[key] {
		if now.After(expiry) {
			continue // expired occurrence; drop it
		}
		if !matched {
			matched = true // consume exactly this one occurrence
			continue
		}
		live = append(live, expiry)
	}
	if len(live) == 0 {
		delete(l.entries, key)
	} else {
		l.entries[key] = live
	}
	return matched
}

// Forget removes exactly one occurrence that was Recorded for a Send that
// then failed, so it never actually reached the destination host. Record
// happens before the Send call, not after it succeeds, because a fast poll
// racing the Send itself must already see the occurrence once delivery
// genuinely happens — but that means a failed Send would otherwise leave a
// live, matchable occurrence for up to the full TTL with nothing behind
// it, which any pane on destHost could exploit by producing a row with
// From == SystemID and a body whose hash it happens to match. Forget
// closes that window immediately instead of waiting out the TTL.
//
// It is safe to remove any one occurrence rather than specifically the one
// this caller's own Record just added, even under concurrent Broadcast
// calls racing on the identical key: every occurrence for a key is
// interchangeable (same destHost/team/to/bodyHash, only the expiry
// timestamp differs, and those are all within one Record-to-Forget span of
// each other). What matters is that the live count stays equal to the
// number of sends actually still in flight or delivered-but-not-yet-
// polled-back — which occurrence gets removed to reach that count doesn't
// matter. Removing the most recently added one (LIFO) is simplest to
// implement as a slice truncation.
func (l *ownSendLedger) Forget(destHost, team, to, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: bodyHash(body)}
	entries := l.entries[key]
	if len(entries) == 0 {
		return
	}
	if len(entries) == 1 {
		delete(l.entries, key)
		return
	}
	l.entries[key] = entries[:len(entries)-1]
}
