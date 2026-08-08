package board

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Default poll tuning. DefaultPollLimit is sized for "a few seconds' worth
// of new rows" (steady-state polling); DefaultBackfillLimit is the
// one-time, larger cold-start pass — see docs/agent-board.md's Cross-host
// relay section.
const (
	DefaultPollLimit     = 300
	DefaultBackfillLimit = 1000
	DefaultPollInterval  = 5 * time.Second
)

// HostTeam identifies one (host, team) the relay polls.
type HostTeam struct {
	Host string
	Team string
}

// Relay is the single goroutine that polls every known host's agmsg
// installation, forwards cross-host messages, and updates BoardCache as a
// side effect — see docs/agent-board.md's Architecture and Cross-host relay
// sections. The zero value is not usable; construct with NewRelay.
type Relay struct { //nolint:govet // fieldalignment: clarity preferred
	mu       sync.Mutex
	clients  map[string]AgmsgClient
	cache    *BoardCache
	ledger   *ownSendLedger
	resolver PaneResolver
	cursors  CursorStore
	state    map[CursorKey]string
	pairs    []HostTeam

	pollLimit     int
	backfillLimit int
	logf          func(format string, args ...any)
}

// RelayOption customizes a Relay constructed by NewRelay.
type RelayOption func(*Relay)

// WithPollLimit overrides the steady-state poll --limit.
func WithPollLimit(n int) RelayOption { return func(r *Relay) { r.pollLimit = n } }

// WithBackfillLimit overrides the one-time cold-start backfill --limit.
func WithBackfillLimit(n int) RelayOption { return func(r *Relay) { r.backfillLimit = n } }

// WithLogf overrides the relay's logging function (default log.Printf).
func WithLogf(f func(format string, args ...any)) RelayOption {
	return func(r *Relay) { r.logf = f }
}

// NewRelay creates a Relay that polls each of pairs, using cache as the
// shared status/history store, resolver to validate from/to, and cursors to
// persist per-(host,team) progress across restarts.
func NewRelay(
	cache *BoardCache, resolver PaneResolver, cursors CursorStore, pairs []HostTeam, opts ...RelayOption,
) *Relay {
	r := &Relay{
		clients:       make(map[string]AgmsgClient),
		cache:         cache,
		ledger:        newOwnSendLedger(defaultLedgerTTL),
		resolver:      resolver,
		cursors:       cursors,
		state:         make(map[CursorKey]string),
		pairs:         append([]HostTeam(nil), pairs...),
		pollLimit:     DefaultPollLimit,
		backfillLimit: DefaultBackfillLimit,
		logf:          log.Printf,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// RegisterClient makes client available to the relay for its HostID().
func (r *Relay) RegisterClient(client AgmsgClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[client.HostID()] = client
}

// RecordOwnSend registers a Send panemux itself is about to issue (the
// broadcast handler), so a relay-observed row later claiming
// From == PanemuxSentinel can be matched back to it. See the own-send
// ledger's doc comment and docs/agent-board.md's Security model.
func (r *Relay) RecordOwnSend(destHost, team, to, body string) {
	r.ledger.Record(destHost, team, to, body)
}

// LoadCursors loads persisted cursor state via the configured CursorStore.
// Call once before Run/Backfill.
func (r *Relay) LoadCursors() error {
	cursors, err := r.cursors.Load()
	if err != nil {
		return fmt.Errorf("loading relay cursors: %w", err)
	}
	r.mu.Lock()
	r.state = cursors
	r.mu.Unlock()
	return nil
}

func (r *Relay) client(host string) (AgmsgClient, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[host]
	return c, ok
}

func (r *Relay) cursorFor(key CursorKey) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state[key]
}

func (r *Relay) setCursor(key CursorKey, id string) {
	r.mu.Lock()
	r.state[key] = id
	snapshot := make(map[CursorKey]string, len(r.state))
	for k, v := range r.state {
		snapshot[k] = v
	}
	r.mu.Unlock()
	if err := r.cursors.Save(snapshot); err != nil {
		r.logf("board relay: failed to persist cursor: %v", err)
	}
}

// Backfill performs exactly one larger-`--limit` poll per (host, team)
// before steady-state polling begins, per docs/agent-board.md's
// "Cold-start backfill". It does not change any correctness property of
// the regular poll — it is still a bounded --limit read with the same
// accepted truncation risk.
func (r *Relay) Backfill(ctx context.Context) {
	for _, pair := range r.pairs {
		if err := r.pollOnce(ctx, pair, r.backfillLimit); err != nil {
			r.logf("board relay: backfill poll failed for host=%s team=%s: %v", pair.Host, pair.Team, err)
		}
	}
}

// Run performs one Backfill pass, then polls every (host, team) pair every
// interval until ctx is canceled.
func (r *Relay) Run(ctx context.Context, interval time.Duration) {
	r.Backfill(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.PollAll(ctx)
		}
	}
}

// PollAll performs one steady-state poll of every registered (host, team)
// pair.
func (r *Relay) PollAll(ctx context.Context) {
	for _, pair := range r.pairs {
		if err := r.pollOnce(ctx, pair, r.pollLimit); err != nil {
			r.logf("board relay: poll failed for host=%s team=%s: %v", pair.Host, pair.Team, err)
		}
	}
}

// pollOnce runs a single bounded poll for one (host, team) pair and routes
// every row it observes. See docs/agent-board.md's Integration with agmsg
// section for why this is a bounded --limit poll with client-side afterID
// filtering rather than a true incremental read, and the accepted
// truncation risk that implies: if more than limit genuinely new rows
// landed since the last poll, the oldest of that overflow are silently
// skipped — this function keeps the newest rows within the limit and does
// not attempt to detect or recover the gap.
func (r *Relay) pollOnce(ctx context.Context, pair HostTeam, limit int) error {
	client, ok := r.client(pair.Host)
	if !ok {
		return fmt.Errorf("no agmsg client registered for host %q", pair.Host)
	}
	key := CursorKey(pair)
	cursor := r.cursorFor(key)

	rows, err := client.Since(ctx, pair.Team, cursor, limit)
	if err != nil {
		return fmt.Errorf("polling host %q team %q: %w", pair.Host, pair.Team, err)
	}
	if len(rows) == 0 {
		return nil
	}

	lastID := cursor
	for _, row := range rows {
		r.handleRow(ctx, row)
		lastID = row.ID
	}
	r.setCursor(key, lastID)
	return nil
}

// handleRow validates and routes a single row observed on row.Host, per
// docs/agent-board.md's Cross-host relay section:
//  1. from-validation: a known local pane ID on the row's source host, or a
//     ledger-matched "_panemux", passes; anything else is dropped and
//     logged, never cached or relayed.
//  2. to == PanemuxSentinel: a valid board_status body updates the status
//     cache only (never appended to history, never relayed — status
//     reports are local bookkeeping, not messages meant for another pane).
//     Any other body addressed to _panemux is an ordinary message and is
//     appended to history.
//  3. Otherwise: to must resolve to a known pane, or the row is dropped and
//     logged, the same as an invalid from. A resolved row is always
//     appended to history; if its host differs from the row's source host,
//     it is additionally relayed (Send, always --force) to the destination
//     host's AgmsgClient. Same-host delivery needs no relay.
func (r *Relay) handleRow(ctx context.Context, row Row) {
	if !r.fromPasses(row) {
		r.logf("board relay: dropping row id=%s host=%s: from %q failed validation", row.ID, row.Host, row.From)
		return
	}

	if row.To == PanemuxSentinel {
		if st, ok := ParseStatus(row.Body); ok {
			r.cache.RecordStatus(row.From, st)
			return
		}
		r.cache.AppendMessage(row)
		return
	}

	destHost, ok := r.resolver.HostForPane(row.To)
	if !ok {
		r.logf("board relay: dropping row id=%s host=%s: to %q does not resolve to a known pane", row.ID, row.Host, row.To)
		return
	}

	r.cache.AppendMessage(row)

	if destHost == row.Host {
		return // same-host: sender and receiver already share one agmsg team
	}

	destClient, ok := r.client(destHost)
	if !ok {
		r.logf("board relay: no agmsg client for destination host %q (pane %q)", destHost, row.To)
		return
	}
	if err := destClient.Send(ctx, row.Team, row.From, row.To, row.Body); err != nil {
		r.logf("board relay: relaying row id=%s to host=%s failed: %v", row.ID, destHost, err)
	}
}

func (r *Relay) fromPasses(row Row) bool {
	if row.From == PanemuxSentinel {
		return r.ledger.Consume(row.Host, row.Team, row.To, row.Body)
	}
	return r.resolver.KnownPane(row.Host, row.From)
}
