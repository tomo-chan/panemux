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
type ownSendLedger struct {
	entries map[ownSendKey]time.Time // value is expiry
	now     func() time.Time
	mu      sync.Mutex
	ttl     time.Duration
}

func newOwnSendLedger() *ownSendLedger {
	return &ownSendLedger{
		entries: make(map[ownSendKey]time.Time),
		ttl:     defaultOwnSendLedgerTTL,
		now:     time.Now,
	}
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Record inserts an entry for a Send panemux itself just issued, matchable
// until it expires after l.ttl.
func (l *ownSendLedger) Record(destHost, team, to, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: bodyHash(body)}
	l.entries[key] = l.now().Add(l.ttl)
}

// Consume reports whether a matching, unexpired entry exists for the given
// send parameters, deleting it either way (a consumed entry cannot be
// matched twice, and an expired one is stale bookkeeping either way).
func (l *ownSendLedger) Consume(destHost, team, to, body string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ownSendKey{DestHost: destHost, Team: team, To: to, BodyHash: bodyHash(body)}
	expiry, ok := l.entries[key]
	if !ok {
		return false
	}
	delete(l.entries, key)
	return !l.now().After(expiry)
}
