package board

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// hostOperationTimeout bounds every individual Since/Send call the relay
// makes against one host. Poll's own ctx is process-lifetime (canceled only
// on shutdown), so without a per-operation bound one unresponsive host
// (dead SSH connection, wedged agmsg process) would stall pollHost — and
// therefore every other host queued behind it in the same Poll pass, since
// hosts are polled sequentially — indefinitely instead of just for this one
// poll cycle.
const hostOperationTimeout = 10 * time.Second

// RelayConfig is the static, precomputed input the relay needs. Building it
// (which hosts exist, which AgmsgClient reaches each, which board-enabled
// pane IDs live on which host) is the caller's job — internal/board does
// not import internal/config or internal/session, mirroring
// AgmsgClient/BoardExecutor's own existing dependency direction.
type RelayConfig struct {
	Clients        map[string]AgmsgClient // keyed by AgmsgClient.HostID()
	PaneHosts      map[string]string      // board-enabled pane ID -> host (a key into Clients)
	PersistCursors func(entries []CursorEntry)
	Team           string
	Limit          int // steady-state --limit
	BackfillLimit  int // cold-start --limit
	// OperationTimeout bounds every individual Since/Send call against one
	// host. Zero (the default for every real caller) falls back to
	// hostOperationTimeout; tests that need to exercise the timeout itself
	// without an actual multi-second wait can set a small value here.
	OperationTimeout time.Duration
}

// Relay is panemux's own agmsg poller: it reads every known host's agmsg
// team, updates BoardCache, and relays cross-host messages. See
// docs/agent-board.md's Cross-host relay section for the full design this
// implements.
type Relay struct {
	cache         *BoardCache
	ledger        *ownSendLedger
	clients       map[string]AgmsgClient
	paneHosts     map[string]string
	hostPanes     map[string]map[string]bool // host -> set of board-enabled pane IDs on that host
	persist       func([]CursorEntry)
	cursors       map[string]string // host -> agmsg-native cursor id
	team          string
	limit         int
	backfillLimit int
	opTimeout     time.Duration
	mu            sync.Mutex
	backfilled    bool
}

// NewRelay returns a Relay that writes into cache as it polls.
func NewRelay(cache *BoardCache, cfg RelayConfig) *Relay {
	hostPanes := make(map[string]map[string]bool, len(cfg.Clients))
	for pane, host := range cfg.PaneHosts {
		if hostPanes[host] == nil {
			hostPanes[host] = make(map[string]bool)
		}
		hostPanes[host][pane] = true
	}
	opTimeout := cfg.OperationTimeout
	if opTimeout <= 0 {
		opTimeout = hostOperationTimeout
	}
	return &Relay{
		cache:         cache,
		ledger:        newOwnSendLedger(),
		clients:       cfg.Clients,
		paneHosts:     cfg.PaneHosts,
		hostPanes:     hostPanes,
		persist:       cfg.PersistCursors,
		cursors:       make(map[string]string),
		team:          cfg.Team,
		limit:         cfg.Limit,
		backfillLimit: cfg.BackfillLimit,
		opTimeout:     opTimeout,
	}
}

// LoadCursors seeds cursor state from a previously persisted snapshot. Call
// before the first Poll/Run. Entries for a different team than this Relay's
// own are ignored.
func (r *Relay) LoadCursors(entries []CursorEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range entries {
		if e.Team != r.team {
			continue
		}
		r.cursors[e.Host] = e.Cursor
	}
}

// Cursors returns a snapshot of current cursor state in the same shape
// LoadCursors accepts, sorted by host for deterministic output.
func (r *Relay) Cursors() []CursorEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := make([]CursorEntry, 0, len(r.cursors))
	for host, cursor := range r.cursors {
		entries = append(entries, CursorEntry{Host: host, Team: r.team, Cursor: cursor})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Host < entries[j].Host })
	return entries
}

// HasClients reports whether the relay knows of at least one agmsg-reachable
// host. Callers can use this to skip starting the poll loop altogether when
// there is nothing to poll — see main.go's use of this before starting Run.
func (r *Relay) HasClients() bool {
	return len(r.clients) > 0
}

// Poll runs exactly one pass: on the very first call it performs the
// cold-start backfill (one BackfillLimit-sized Since call per known host),
// then every call performs the steady-state Limit-sized poll, processes
// returned rows, and advances cursors. If PersistCursors is set, it is
// called only when at least one host's cursor actually advanced this pass
// — a poll that finds nothing new (the common steady-state case, and the
// permanent case for an operator with no board-enabled panes at all) must
// not turn into a disk write every single interval forever. Poll never
// panics on a per-host error; it logs and continues with the remaining
// hosts, returning a joined error only for callers that want to observe
// failures.
func (r *Relay) Poll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("agent board relay: %w", err)
	}

	r.mu.Lock()
	backfill := !r.backfilled
	r.backfilled = true
	r.mu.Unlock()

	limit := r.limit
	if backfill {
		limit = r.backfillLimit
	}

	var errs []error
	changed := false
	for _, host := range sortedHostKeys(r.clients) {
		hostChanged, err := r.pollHost(ctx, host, limit)
		if err != nil {
			errs = append(errs, fmt.Errorf("host %q: %w", host, err))
			continue
		}
		changed = changed || hostChanged
	}

	if changed && r.persist != nil {
		r.persist(r.Cursors())
	}

	if len(errs) > 0 {
		return fmt.Errorf("agent board relay: %w", errors.Join(errs...))
	}
	return nil
}

// pollHost polls one host and reports whether its cursor advanced.
func (r *Relay) pollHost(ctx context.Context, host string, limit int) (bool, error) {
	client := r.clients[host]

	r.mu.Lock()
	cursor := r.cursors[host]
	r.mu.Unlock()

	sinceCtx, cancel := context.WithTimeout(ctx, r.opTimeout)
	rows, err := client.Since(sinceCtx, r.team, cursor, limit)
	cancel()
	if err != nil {
		return false, fmt.Errorf("since: %w", err)
	}

	for _, row := range rows {
		r.processRow(ctx, host, row)
	}

	newCursor := maxRowID(rows, cursor)
	if newCursor == cursor {
		return false, nil
	}
	r.mu.Lock()
	r.cursors[host] = newCursor
	r.mu.Unlock()
	return true, nil
}

// maxRowID returns the largest numerically-parseable row ID among rows,
// falling back to prev if no row's ID parses (or rows is empty) — a row
// whose ID doesn't parse never regresses the cursor.
func maxRowID(rows []Row, prev string) string {
	best := prev
	bestN, bestOK := parseAgmsgID(prev)
	for _, row := range rows {
		n, ok := parseAgmsgID(row.ID)
		if !ok {
			continue
		}
		if !bestOK || n > bestN {
			best = row.ID
			bestN = n
			bestOK = true
		}
	}
	return best
}

// processRow applies the row-processing algorithm documented in
// docs/agent-board.md's Cross-host relay section:
//  1. from-validation fails -> drop+log, never cached.
//  2. to == SystemID -> always appended to history; a board_status body also
//     updates the status cache; never relayed further (status stays local).
//  3. to doesn't resolve to a known pane -> drop+log, never cached (same as
//     an invalid from).
//  4. to resolves to a known pane -> appended to history; relayed via
//     AgmsgClient.Send (always --force) only when the destination host
//     differs from sourceHost.
//
// A row this method relays to another host keeps its original From (the
// real pane ID, not SystemID). When that host is polled next, from-
// validation for that row is evaluated against *that host's* own known
// panes — the original pane is not local there, so the row is correctly
// dropped rather than re-appended to history a second time. This is
// intentional, not a bug: see the relay design notes in the project's
// implementation plan.
func (r *Relay) processRow(ctx context.Context, sourceHost string, row Row) {
	if !r.validFrom(sourceHost, row) {
		log.Printf("agent board relay: dropping row from host %q: invalid from %q", sourceHost, row.From)
		return
	}

	if row.To == SystemID {
		r.cache.AppendMessage(row)
		if status, ok := IsStatusRow(row); ok {
			r.cache.RecordStatus(row.From, status)
		}
		return
	}

	destHost, ok := r.paneHosts[row.To]
	if !ok {
		log.Printf("agent board relay: dropping row from host %q: unknown to %q", sourceHost, row.To)
		return
	}

	r.cache.AppendMessage(row)

	if destHost == sourceHost {
		return
	}

	client, ok := r.clients[destHost]
	if !ok {
		log.Printf("agent board relay: no client for host %q while relaying to pane %q", destHost, row.To)
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, r.opTimeout)
	defer cancel()
	if err := client.Send(sendCtx, row.Team, row.From, row.To, row.Body); err != nil {
		log.Printf("agent board relay: relaying to host %q failed: %v", destHost, err)
	}
}

func (r *Relay) validFrom(sourceHost string, row Row) bool {
	if row.From == SystemID {
		return r.ledger.Consume(sourceHost, row.Team, row.To, row.Body)
	}
	return r.hostPanes[sourceHost][row.From]
}

// Run polls every interval until ctx is canceled, running one immediate
// Poll first so the cold-start backfill happens as soon as possible rather
// than waiting a full interval.
func (r *Relay) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	r.runLoop(ctx, ticker.C)
}

// runLoop is Run's actual loop, factored out so tests can drive it with an
// injected tick channel instead of real time.
func (r *Relay) runLoop(ctx context.Context, tick <-chan time.Time) {
	r.pollLogged(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			r.pollLogged(ctx)
		}
	}
}

func (r *Relay) pollLogged(ctx context.Context) {
	if err := r.Poll(ctx); err != nil {
		log.Printf("agent board relay: poll error: %v", err)
	}
}

// UnknownPaneError is returned by Broadcast when one or more `to` pane IDs
// don't resolve to a known board-enabled pane.
type UnknownPaneError struct{ IDs []string }

func (e *UnknownPaneError) Error() string {
	return "agent board: unknown pane id(s): " + strings.Join(e.IDs, ", ")
}

// Broadcast sends body from `from` to every pane ID in `to`. All `to` IDs
// are validated against known board-enabled panes before any Send is
// issued (all-or-nothing): an unknown pane returns *UnknownPaneError naming
// every unresolved ID, and no Send happens at all. When from == SystemID,
// an own-send-ledger entry is recorded for each target immediately before
// its Send — matching exactly what the relay's own from-validation later
// checks for such a row, closing the from-forgery gap docs/security.md and
// docs/agent-board.md describe. If that Send then fails, the just-recorded
// entry is immediately forgotten again: leaving it live would let any pane
// on that destination host impersonate SystemID for up to the ledger's TTL
// with nothing ever actually delivered behind it. On a
// resolved-but-unreachable-host or a Send failure, Broadcast stops at the
// first failure and returns the pane IDs successfully delivered so far,
// plus the error (fail-fast — a deliberate PR2 simplification, not an
// oversight).
func (r *Relay) Broadcast(ctx context.Context, from string, to []string, body string) ([]string, error) {
	hosts := make(map[string]string, len(to))
	var unknown []string
	for _, pane := range to {
		host, ok := r.paneHosts[pane]
		if !ok {
			unknown = append(unknown, pane)
			continue
		}
		hosts[pane] = host
	}
	if len(unknown) > 0 {
		return nil, &UnknownPaneError{IDs: unknown}
	}

	delivered := make([]string, 0, len(to))
	for _, pane := range to {
		host := hosts[pane]
		client, ok := r.clients[host]
		if !ok {
			return delivered, fmt.Errorf("agent board: no client for host %q (pane %q)", host, pane)
		}
		if from == SystemID {
			r.ledger.Record(host, r.team, pane, body)
		}
		if err := client.Send(ctx, r.team, from, pane, body); err != nil {
			if from == SystemID {
				r.ledger.Forget(host, r.team, pane, body)
			}
			return delivered, fmt.Errorf("agent board: broadcast to %q failed: %w", pane, err)
		}
		delivered = append(delivered, pane)
	}
	return delivered, nil
}

func sortedHostKeys(m map[string]AgmsgClient) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
