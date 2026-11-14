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
// (dead SSH connection, wedged message transport process) would stall pollHost — and
// therefore every other host queued behind it in the same Poll pass, since
// hosts are polled sequentially — indefinitely instead of just for this one
// poll cycle.
const hostOperationTimeout = 10 * time.Second

// Routing is an immutable snapshot supplied by the composition root.
type Routing struct {
	PaneHosts map[string]string
	Clients   map[string]MessageClient
}

// RelayConfig supplies fixed transports or a live routing snapshot. Building it
// (which hosts exist, which MessageClient reaches each, which board-enabled
// pane IDs live on which host) is the caller's job — internal/board does
// not import internal/config or internal/session, mirroring
// MessageClient/host.Runner's own existing dependency direction.
type RelayConfig struct {
	CurrentRouting func() Routing
	Clients        map[string]MessageClient // keyed by MessageClient.HostID()
	PaneHosts      map[string]string        // board-enabled pane ID -> host (a key into Clients)
	PersistCursors func(entries []CursorEntry)
	Team           string
	Limit          int // steady-state row limit
	BackfillLimit  int // cold-start row limit
	// OperationTimeout bounds every individual Since/Send call against one
	// host. Zero (the default for every real caller) falls back to
	// hostOperationTimeout; tests that need to exercise the timeout itself
	// without an actual multi-second wait can set a small value here.
	OperationTimeout time.Duration
}

// Relay is panemux's message poller: it reads every known host's transport
// team, updates BoardCache, and relays cross-host messages. See
// docs/agent-board.md's Cross-host relay section for the full design this
// implements.
type Relay struct {
	cache          *BoardCache
	ledger         *ownSendLedger
	clients        map[string]MessageClient
	paneHosts      map[string]string
	currentRouting func() Routing
	persist        func([]CursorEntry)
	cursors        map[string]string // host -> host-local opaque cursor ID
	team           string
	lastPollError  string
	limit          int
	backfillLimit  int
	opTimeout      time.Duration
	mu             sync.Mutex
	backfilled     bool
}

// NewRelay returns a Relay that writes into cache as it polls.
func NewRelay(cache *BoardCache, cfg RelayConfig) *Relay {
	opTimeout := cfg.OperationTimeout
	if opTimeout <= 0 {
		opTimeout = hostOperationTimeout
	}
	return &Relay{
		cache:          cache,
		ledger:         newOwnSendLedger(),
		clients:        cfg.Clients,
		paneHosts:      cfg.PaneHosts,
		currentRouting: cfg.CurrentRouting,
		persist:        cfg.PersistCursors,
		cursors:        make(map[string]string),
		team:           cfg.Team,
		limit:          cfg.Limit,
		backfillLimit:  cfg.BackfillLimit,
		opTimeout:      opTimeout,
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

// HasClients reports whether the relay knows of at least one message transport-reachable
// host. Callers can use this to skip starting the poll loop altogether when
// there is nothing to poll — see main.go's use of this before starting Run.
func (r *Relay) HasClients() bool {
	return len(r.routing().Clients) > 0
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
	for _, host := range sortedHostKeys(r.routing().Clients) {
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
	client := r.routing().Clients[host]
	if client == nil {
		return false, nil
	}
	client, release, err := leaseMessages(client)
	if err != nil {
		return false, err
	}
	defer release()

	r.mu.Lock()
	cursor := r.cursors[host]
	r.mu.Unlock()

	sinceCtx, cancel := context.WithTimeout(ctx, r.opTimeout)
	rows, err := client.Since(sinceCtx, r.team, cursor, limit)
	cancel()
	if err != nil {
		return false, fmt.Errorf("since: %w", err)
	}

	// Admit all involved destinations before touching cache, ledger or cursor.
	// A paused destination leaves the source batch retryable on the next poll.
	routing := r.routing()
	leased := map[string]MessageClient{host: client}
	for _, row := range rows {
		target, ok := resolveRecipient(row.To, routing)
		dest := target.Host
		if !ok || row.From == SystemID || !r.senderEligibleUsing(host, row, routing) ||
			dest == "" || (dest == host && !strings.Contains(row.To, "@")) ||
			leased[dest] != nil || routing.Clients[dest] == nil {

			continue
		}
		c, done, err := leaseMessages(routing.Clients[dest])
		if err != nil {
			return false, err
		}
		defer done()
		leased[dest] = c
	}
	routing.Clients = leased
	for _, row := range rows {
		r.processRowUsing(ctx, host, row, routing)
	}

	newCursor := lastRowID(rows, cursor)
	if newCursor == cursor {
		return false, nil
	}
	r.mu.Lock()
	r.cursors[host] = newCursor
	r.mu.Unlock()
	return true, nil
}

// lastRowID returns the cursor to store after a poll: the id of the LAST
// row the transport returned, which is the newest one, since the response is
// ordered by a transport-native counter. IDs are opaque: the relay never parses
// or compares them. An empty
// response leaves the previous cursor in place.
func lastRowID(rows []Row, prev string) string {
	if len(rows) == 0 {
		return prev
	}
	return rows[len(rows)-1].ID
}

// processRow records accepted originals at their source host. Qualified senders
// are copies (or forgeries), so they never enter history, status or relay again.
func (r *Relay) processRow(ctx context.Context, sourceHost string, row Row) {
	r.processRowUsing(ctx, sourceHost, row, r.routing())
}

func (r *Relay) processRowUsing(ctx context.Context, sourceHost string, row Row, routing Routing) {
	target, ok := resolveRecipient(row.To, routing)
	if !ok || !r.validFromUsing(sourceHost, row, routing) {
		log.Printf("agent board relay: dropping invalid row from host %q: from %q to %q", sourceHost, row.From, row.To)
		return
	}

	row.Host = sourceHost
	r.cache.AppendMessage(row)
	if row.To == SystemID {
		if status, ok := IsStatusRow(row); ok {
			r.cache.RecordStatus((Identity{Name: row.From, Host: sourceHost}).String(), status)
		}
		return
	}
	if row.From == SystemID {
		// A ledger match is an already delivered Broadcast, not a relay request.
		return
	}

	from := row.From
	if target.Host == "" {
		// Ordinary unqualified names are local to agmsg; never search other hosts.
		return
	}
	_, paneSender := routing.PaneHosts[row.From]
	qualified := strings.Contains(row.To, "@")
	if !qualified && target.Host == sourceHost {
		return
	}
	if qualified || !paneSender {
		from = (Identity{Name: row.From, Host: sourceHost}).String()
	}

	client := routing.Clients[target.Host]
	if client == nil {
		log.Printf("agent board relay: no client for host %q while relaying to pane %q", target.Host, row.To)
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, r.opTimeout)
	defer cancel()
	if err := client.Send(sendCtx, row.Team, from, target.Name, row.Body); err != nil {
		log.Printf("agent board relay: relaying to host %q failed: %v", target.Host, err)
	}
}

// resolveRecipient preserves unqualified pane routing for compatibility. A
// qualified address needs only a configured host, never a pane or roster entry.
func resolveRecipient(raw string, routing Routing) (Identity, bool) {
	id, err := ParseIdentity(raw)
	if err != nil {
		return Identity{}, false
	}
	if id.Host != "" {
		return id, routing.Clients[id.Host] != nil
	}
	id.Host = routing.PaneHosts[id.Name]
	return id, true
}

func (r *Relay) validFrom(sourceHost string, row Row) bool {
	return r.validFromUsing(sourceHost, row, r.routing())
}

func (r *Relay) validFromUsing(sourceHost string, row Row, routing Routing) bool {
	if !r.senderEligibleUsing(sourceHost, row, routing) {
		return false
	}
	if row.From == SystemID {
		return r.ledger.Consume(sourceHost, row.Team, row.To, row.Body)
	}
	return true
}

// senderEligibleUsing is side-effect free so lease admission cannot consume a
// system send's ledger entry before the batch is admitted.
func (r *Relay) senderEligibleUsing(sourceHost string, row Row, routing Routing) bool {
	id, err := ParseIdentity(row.From)
	if row.Team != r.team || err != nil || id.Host != "" {
		return false
	}
	if row.From == SystemID {
		return true
	}
	owner, known := routing.PaneHosts[row.From]
	return !known || owner == sourceHost
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
	err := r.Poll(ctx)
	state := ""
	if err != nil {
		state = err.Error()
	}
	r.mu.Lock()
	changed := state != r.lastPollError
	r.lastPollError = state
	r.mu.Unlock()
	if changed && err != nil {
		log.Printf("agent board relay: poll error: %v", err)
	}
}

// UnknownPaneError retains the existing API error type for unresolved recipients,
// including malformed qualified addresses or unknown Board hosts.
type UnknownPaneError struct{ IDs []string }

func (e *UnknownPaneError) Error() string {
	return "agent board: unknown pane id(s): " + strings.Join(e.IDs, ", ")
}

// Broadcast sends body from `from` to qualified identities or legacy pane IDs.
// All recipients are resolved before any Send is
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
	routing := r.routing()
	targets := make(map[string]Identity, len(to))
	var unknown []string
	for _, pane := range to {
		target, ok := resolveRecipient(pane, routing)
		if !ok || target.Host == "" {
			unknown = append(unknown, pane)
			continue
		}
		targets[pane] = target
	}
	if len(unknown) > 0 {
		return nil, &UnknownPaneError{IDs: unknown}
	}

	delivered := make([]string, 0, len(to))
	for _, pane := range to {
		target := targets[pane]
		host := target.Host
		client, ok := routing.Clients[host]
		if !ok {
			return delivered, fmt.Errorf("agent board: no client for host %q (pane %q)", host, pane)
		}
		client, release, err := leaseMessages(client)
		if err != nil {
			return delivered, err
		}
		if from == SystemID {
			r.ledger.Record(host, r.team, target.Name, body)
		}
		err = client.Send(ctx, r.team, from, target.Name, body)
		release()
		if err != nil {
			if from == SystemID {
				r.ledger.Forget(host, r.team, target.Name, body)
			}
			return delivered, fmt.Errorf("agent board: broadcast to %q failed: %w", pane, err)
		}
		delivered = append(delivered, pane)
	}
	return delivered, nil
}

func (r *Relay) routing() Routing {
	if r.currentRouting != nil {
		return r.currentRouting()
	}
	return Routing{PaneHosts: r.paneHosts, Clients: r.clients}
}

func sortedHostKeys(m map[string]MessageClient) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
