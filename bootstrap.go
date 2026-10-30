package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"panemux/internal/agent"
	"panemux/internal/board"
	"panemux/internal/session"
)

// defaultBootstrapPollInterval matches defaultBoardPollInterval. The
// bootstrap watcher's own 2-tick debounce (see checkPane) adds roughly one
// extra interval of latency between an agent process starting and the
// onboarding instruction actually landing in its PTY; polling every 5s
// rather than 10s keeps that added latency from being felt twice.
const defaultBootstrapPollInterval = 5 * time.Second

// bootstrapProbeTimeout bounds an individual remote presence probe
// checkPane makes. Deliberately shorter than boardStartupProbeTimeout
// (10s, sized for a one-shot startup call): pollOnce runs every board pane
// sequentially within one defaultBootstrapPollInterval-wide tick, so an
// unreachable host's probe must not be allowed to eat most of that budget
// and delay every other pane queued behind it in the same tick.
const bootstrapProbeTimeout = 3 * time.Second

// maxBootstrapWriteAttempts bounds how many times checkPane retries a
// failed (but not short/partial) Session.Write before giving up on a pane
// for the rest of that session's lifetime. A write that returns n==0 with
// an error hasn't put anything into the pane yet, so a few retries are
// safe; see the giveUp/writeAttempts fields for why a *short* write is
// never retried at all.
const maxBootstrapWriteAttempts = 3

// boardModeTurn and boardModeBoth mirror internal/config's own (unexported)
// agentBoardModeTurn/agentBoardModeBoth string values. Duplicated here
// rather than exported from internal/config because bootstrapWatcher
// deliberately takes no dependency on internal/config at all — see
// bootstrapWatcherConfig's own comment.
const (
	boardModeMonitor = "monitor"
	boardModeTurn    = "turn"
	boardModeBoth    = "both"
)

// bootstrapWatcherConfig supplies fixed or live inputs the bootstrap
// watcher needs. Building it (which panes are board-enabled, which host each
// lives on, each pane's configured mode, and participation ports) is
// the caller's job (board.go's setupBoard) — bootstrapWatcher itself takes
// no dependency on internal/config, mirroring board.RelayConfig's own
// existing dependency direction (see relay.go's RelayConfig comment).
type bootstrapWatcherConfig struct {
	CurrentPaneHosts  func() map[string]string
	SyncParticipation func(map[string]string)
	Manager           *session.Manager
	PaneHosts         map[string]string
	// PaneModes is a function, not a map, because a pane's mode can change
	// while panemux runs. The composition root reads this under the config lock.
	PaneModes            func() map[string]string
	ParticipationForHost func(string) board.Participation
	Persist              func(paneIDs []string)
	Observe              func(paneID, host, agentType string, instructed bool)
	Team                 string
	WatchEnabled         bool
}

// bootstrapWatcher polls board-enabled panes for a newly-started, detectable
// coding-agent process and writes a one-time onboarding
// instruction into that pane's PTY. See docs/agent-board.md's Bootstrap flow
// section for the full design.
//
// No mutex: unlike Relay, every field here is touched only from the single
// goroutine driving runLoop — nothing else ever reads or writes this
// watcher's state.
type bootstrapWatcher struct {
	currentPaneHosts     func() map[string]string
	syncParticipation    func(map[string]string)
	observe              func(string, string, string, bool)
	manager              *session.Manager
	paneHosts            map[string]string
	paneModes            func() map[string]string
	participationForHost func(string) board.Participation
	persist              func(paneIDs []string)
	bootstrapped         map[string]session.Session
	pending              map[string]session.Session
	// givenUp holds panes checkPane will never attempt to write to again for
	// the life of the current session object: either a Write returned a
	// nonzero-but-short n (any retry would type on top of an already
	// half-written line — see checkPane), or a clean (n==0) failure recurred
	// maxBootstrapWriteAttempts times in a row. Keyed and compared by
	// session identity exactly like bootstrapped, so a pane that is later
	// restarted (a new Session object) is eligible again.
	givenUp map[string]session.Session
	// writeAttempts counts consecutive clean (n==0) Write failures per pane,
	// reset on success and on giving up. Not persisted: it only needs to
	// survive within one session object's lifetime.
	writeAttempts map[string]int
	// warned tracks which warning kinds are currently being suppressed for a
	// pane. A kind is added when its warning is logged and removed when the
	// condition it reports clears, so each warning is logged once per failing
	// streak rather than once per tick
	// (the poll loop would otherwise repeat it for as long as the condition
	// lasts, which for a dead tmux server or a dropped SSH connection is the
	// life of the process) and rather than once for the life of the pane (a
	// failure that returns after a recovery is news again). The kinds are
	// separate keys because the conditions are independent: one firing must
	// not silence the others. See #218.
	warned           map[string]warnedPane
	team             string
	persistedPaneIDs []string
	seeded           bool
	watchEnabled     bool
}

// warnedPane is one pane's warning suppression: the kinds currently being
// suppressed for it, and the session they were recorded against.
//
// The session is part of it because a restarted pane is a new Session object —
// the same signal bootstrapped and givenUp compare by identity — and a new
// session's failure is a new streak, not a continuation of the old one.
// Without it, the one case where an operator would hear nothing at all is the
// strongest recovery signal there is: the pane was torn down and recreated.
type warnedPane struct {
	sess  session.Session
	kinds map[string]bool
}

// The warning kinds bootstrapWatcher.warnOnce suppresses independently of
// one another. Each names a distinct condition a pane can be in, so a pane
// can hit several of them in sequence and each still gets reported once.
const (
	warnKindIdentifier  = "identifier"
	warnKindDetect      = "detect"
	warnKindInstruction = "instruction"
	warnKindPath        = "path"
	warnKindProbe       = "presence-probe"
	warnKindAbsent      = "messaging-absent"
)

// newBootstrapWatcher returns a bootstrapWatcher ready to have persisted
// state loaded (LoadPersistedState) and then be polled or run.
func newBootstrapWatcher(cfg bootstrapWatcherConfig) *bootstrapWatcher {
	return &bootstrapWatcher{
		manager:              cfg.Manager,
		currentPaneHosts:     cfg.CurrentPaneHosts,
		syncParticipation:    cfg.SyncParticipation,
		watchEnabled:         cfg.WatchEnabled,
		observe:              cfg.Observe,
		paneHosts:            cfg.PaneHosts,
		paneModes:            cfg.PaneModes,
		participationForHost: cfg.ParticipationForHost,
		team:                 cfg.Team,
		persist:              cfg.Persist,
		bootstrapped:         map[string]session.Session{},
		pending:              map[string]session.Session{},
		givenUp:              map[string]session.Session{},
		writeAttempts:        map[string]int{},
		warned:               map[string]warnedPane{},
	}
}

// LoadPersistedState seeds the pane IDs that were already bootstrapped as of
// the last save, consumed exactly once by the first pollOnce call. Call
// before Run/pollOnce, mirroring Relay.LoadCursors.
func (b *bootstrapWatcher) LoadPersistedState(paneIDs []string) {
	b.persistedPaneIDs = paneIDs
}

// HasWork keeps enabled live configurations running even when there are no panes.
// Callers use this to skip starting the poll loop entirely, mirroring
// Relay.HasClients.
func (b *bootstrapWatcher) HasWork() bool {
	return b.watchEnabled || len(b.paneHosts) > 0
}

// Run polls every interval until ctx is canceled, running one immediate
// pollOnce first. Mirrors Relay.Run.
func (b *bootstrapWatcher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	b.runLoop(ctx, ticker.C)
}

// runLoop is Run's actual loop, factored out so tests can drive it with an
// injected tick channel instead of real time. Mirrors Relay.runLoop.
func (b *bootstrapWatcher) runLoop(ctx context.Context, tick <-chan time.Time) {
	b.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			b.pollOnce(ctx)
		}
	}
}

// pollOnce checks every board-enabled pane once. On its very first call it
// seeds bootstrapped from persistedPaneIDs — using each persisted pane ID's
// *currently live* Session, if any, purely to give bootstrapped an identity
// to compare future ticks against. persistedPaneIDs is never consulted
// again after this: from here on, only bootstrapped's own session-identity
// map governs re-bootstrap decisions. This is what makes seeding safe across
// a panemux restart, but only for a pane whose underlying agent process can
// actually have survived that restart: seeding only ever applies to
// TypeTmux/TypeSSHTmux sessions, which reattach to an independently-running
// tmux session on every panemux startup. A TypeLocal/TypeSSH pane's
// Session.CreateFromConfig always spawns a brand-new shell with nothing
// running in it — seeding those from persisted state would permanently
// suppress bootstrap for that pane on every future run, mistaking "we
// bootstrapped a previous, now-gone process" for "the current process is
// already onboarded". A pane that restarts within this same process's
// lifetime is unaffected either way (its new Session object won't match
// whatever was seeded).
func (b *bootstrapWatcher) pollOnce(ctx context.Context) {
	b.refreshPanes()
	if !b.seeded {
		for _, paneID := range b.persistedPaneIDs {
			sess, ok := b.manager.Get(paneID)
			if !ok {
				continue
			}
			if sess.Type() != session.TypeTmux && sess.Type() != session.TypeSSHTmux {
				continue
			}
			b.bootstrapped[paneID] = sess
		}
		b.seeded = true
	}
	for paneID, host := range b.paneHosts {
		b.checkPane(ctx, paneID, host)
	}
}

func (b *bootstrapWatcher) refreshPanes() {
	if b.currentPaneHosts == nil {
		return
	}
	current := b.currentPaneHosts()
	for id, host := range b.paneHosts {
		if next, exists := current[id]; !exists || next != host {
			delete(b.bootstrapped, id)
			delete(b.pending, id)
			delete(b.givenUp, id)
			delete(b.writeAttempts, id)
			delete(b.warned, id)
		}
	}
	b.paneHosts = current
	if b.syncParticipation != nil {
		b.syncParticipation(current)
	}
}

// sessionFor returns the pane's live session, or false when the manager no
// longer has one for it. It also keeps the per-pane bookkeeping that depends
// on session identity in step: a pane the manager no longer knows about is not
// in a failing streak — if it comes back it starts a new one — and dropping its
// entry is what keeps warned from growing for panes that no longer exist.
func (b *bootstrapWatcher) sessionFor(paneID string) (session.Session, bool) {
	sess, ok := b.manager.Get(paneID)
	if !ok {
		delete(b.pending, paneID)
		delete(b.warned, paneID)
		return nil, false
	}
	b.noteSession(paneID, sess)
	return sess, true
}

// modeFor reads the pane's current board mode, defaulting to monitor when
// the pane carries no explicit value — matching internal/config's own
// default rather than sending an empty mode into the instruction.
func (b *bootstrapWatcher) modeFor(paneID string) string {
	if b.paneModes == nil {
		return boardModeMonitor
	}
	mode := b.paneModes()[paneID]
	if mode == "" {
		return boardModeMonitor
	}
	return mode
}

// checkPane runs the bootstrap decision for one pane. See
// docs/agent-board.md's Bootstrap flow section for the algorithm this
// implements.
func (b *bootstrapWatcher) checkPane(ctx context.Context, paneID, host string) {
	participant := b.participant(host)
	if participant == nil {
		return
	}
	if leaser, supportsLease := participant.(board.ParticipationLeaser); supportsLease {
		leased, release, err := leaser.LeaseParticipation()
		if err != nil {
			return
		}
		defer release()
		participant = leased
	}
	b.checkPaneWithParticipant(ctx, paneID, host, participant)
}

func (b *bootstrapWatcher) checkPaneWithParticipant(
	ctx context.Context, paneID, host string, participant board.Participation,
) {
	sess, ok := b.sessionFor(paneID)
	detectedKind := ""
	defer b.observePane(paneID, host, &detectedKind, sess)
	if !ok {
		return
	}

	if err := participant.ValidateAddress(b.team, paneID); err != nil {
		b.warnOnce(paneID, warnKindIdentifier, fmt.Sprintf("agent board bootstrap: %v, skipping bootstrap", err))
		delete(b.pending, paneID)
		return
	}
	b.clearWarning(paneID, warnKindIdentifier)

	detector, ok := sess.(session.AgentTypeDetector)
	if !ok {
		delete(b.pending, paneID)
		return
	}
	agentID, detected, err := detector.DetectInteractiveAgent()
	if err != nil {
		// Warned once per failing streak, not once per tick: detection runs
		// a command inside the pane, so it keeps failing for as long as the
		// tmux server is gone or the SSH connection is down, and dropping
		// the pane from pending suppresses nothing — the next tick finds the
		// same live session and checks it again. See #218.
		b.warnOnce(paneID, warnKindDetect, fmt.Sprintf(
			"agent board bootstrap: detecting agent type for pane %q: %v", paneID, err,
		))
		delete(b.pending, paneID)
		return
	}
	b.clearWarning(paneID, warnKindDetect)
	if !detected {
		delete(b.pending, paneID)
		return
	}
	detectedKind = string(agentID)
	if b.bootstrapped[paneID] == sess || b.givenUp[paneID] == sess {
		return
	}

	// Debounce: only proceed once the same session has been seen with an
	// active, known agent type on two consecutive ticks. This is a partial
	// mitigation, not a complete one, against writing into a pane the
	// instant its shell is still settling — see docs/agent-board.md's
	// Bootstrap flow section for the honest tradeoff this represents.
	if pendingSess, isPending := b.pending[paneID]; !isPending || pendingSess != sess {
		b.pending[paneID] = sess
		return
	}

	present, checked := b.messagingPresent(ctx, paneID, host, participant)
	if !checked {
		return
	}
	if !present {
		b.warnOnce(paneID, warnKindAbsent, fmt.Sprintf(
			"agent board bootstrap: %s not found on host %q for pane %q, skipping bootstrap",
			participant.Description().Name, host, paneID,
		))
		return
	}
	b.clearWarning(paneID, warnKindAbsent)

	b.joinPane(ctx, participant, paneID, agentID, sess)
}

func (b *bootstrapWatcher) observePane(paneID, host string, kind *string, sess session.Session) {
	if b.observe != nil {
		b.observe(paneID, host, *kind, sess != nil && b.bootstrapped[paneID] == sess)
	}
}

func (b *bootstrapWatcher) joinPane(
	ctx context.Context, participant board.Participation, paneID string, agentID agent.ID, sess session.Session,
) {
	instruction, err := participant.JoinInstruction(ctx, board.JoinRequest{
		Agent: agentID, Team: b.team, PaneID: paneID, Mode: b.modeFor(paneID),
	})
	if err != nil {
		b.warnOnce(paneID, warnKindInstruction, fmt.Sprintf(
			"agent board bootstrap: creating participation instruction for pane %q: %v", paneID, err))
		return
	}
	b.clearWarning(paneID, warnKindInstruction)
	b.writeInstruction(paneID, sess, instruction)
}

// writeInstruction attempts the actual PTY write for paneID's onboarding
// instruction and updates bootstrapped/givenUp/writeAttempts/pending based
// on the outcome. Split out of checkPane to keep each function's branching
// manageable on its own.
//
// A short write (n > 0) has already put part of the instruction into the
// pane — retrying would type the full instruction again on top of that
// half-written line, compounding the corruption rather than fixing it, so
// checkPane gives up on that pane immediately. A clean failure (n == 0,
// nothing written yet) is safe to retry, so it's retried up to
// maxBootstrapWriteAttempts times before giving up the same way.
func (b *bootstrapWatcher) writeInstruction(paneID string, sess session.Session, instruction string) {
	payload := []byte(instruction + "\r")
	n, err := sess.Write(payload)
	if err == nil && n == len(payload) {
		delete(b.writeAttempts, paneID)
		b.bootstrapped[paneID] = sess
		delete(b.pending, paneID)
		b.persistBootstrapped()
		return
	}

	if n > 0 {
		log.Printf(
			"Warning: agent board bootstrap: partial write to pane %q (%d/%d bytes); "+
				"not retrying, since a retry would type on top of a half-written instruction",
			paneID, n, len(payload),
		)
		b.givenUp[paneID] = sess
		delete(b.pending, paneID)
		delete(b.writeAttempts, paneID)
		return
	}

	b.writeAttempts[paneID]++
	if b.writeAttempts[paneID] >= maxBootstrapWriteAttempts {
		log.Printf(
			"Warning: agent board bootstrap: giving up writing onboarding instruction to pane %q "+
				"after %d failed attempts: %v",
			paneID, b.writeAttempts[paneID], err,
		)
		b.givenUp[paneID] = sess
		delete(b.pending, paneID)
		delete(b.writeAttempts, paneID)
		return
	}
	log.Printf(
		"Warning: agent board bootstrap: writing onboarding instruction to pane %q: %v (attempt %d/%d)",
		paneID, err, b.writeAttempts[paneID], maxBootstrapWriteAttempts,
	)
}

// messagingPresent probes the host without depending on pane transport.
func (b *bootstrapWatcher) messagingPresent(
	ctx context.Context, paneID, host string, participants ...board.Participation,
) (bool, bool) {
	participant := b.participant(host)
	if len(participants) > 0 {
		participant = participants[0]
	}
	if participant == nil {
		return false, false
	}
	probeCtx, cancel := context.WithTimeout(ctx, bootstrapProbeTimeout)
	defer cancel()
	present, err := participant.Available(probeCtx)
	description := participant.Description()
	if errors.Is(err, board.ErrParticipationPath) {
		b.warnOnce(paneID, warnKindPath, fmt.Sprintf(
			"agent board bootstrap: no resolved %s for host %q (pane %q)", description.PathSetting, host, paneID))
		return false, false
	}
	b.clearWarning(paneID, warnKindPath)
	if err != nil {
		b.warnOnce(paneID, warnKindProbe, fmt.Sprintf(
			"agent board bootstrap: checking %s presence on host %q (pane %q): %v", description.Name, host, paneID, err))
		return false, false
	}
	b.clearWarning(paneID, warnKindProbe)
	return present, true
}

// warnOnce logs message unless kind is already being suppressed for paneID,
// i.e. unless this pane is already inside a failing streak of that kind.
// clearWarning ends the streak.
func (b *bootstrapWatcher) warnOnce(paneID, kind, message string) {
	entry := b.warned[paneID]
	if entry.kinds[kind] {
		return
	}
	if entry.kinds == nil {
		entry.kinds = map[string]bool{}
	}
	entry.kinds[kind] = true
	b.warned[paneID] = entry
	log.Printf("Warning: %s", message)
}

// noteSession records which session paneID's warnings are being suppressed
// against, dropping every suppression the moment that session is replaced.
//
// The entry is kept once created, even with no kinds suppressed, because it
// carries that identity: dropping it when the last kind clears would leave the
// next warning recorded against no session at all, and the following tick
// would read that as a replacement and re-warn — every tick, which is the
// behavior #218 exists to remove. checkPane's own "pane is gone" arm is what
// removes the entry.
func (b *bootstrapWatcher) noteSession(paneID string, sess session.Session) {
	if entry, seen := b.warned[paneID]; seen && entry.sess == sess {
		return
	}
	b.warned[paneID] = warnedPane{sess: sess, kinds: map[string]bool{}}
}

// clearWarning ends paneID's failing streak of this kind, so the next
// occurrence is logged again. Called on the success side of every condition
// warnOnce reports: a condition that clears and comes back is a new streak,
// and by then the operator has been told the pane recovered.
func (b *bootstrapWatcher) clearWarning(paneID, kind string) {
	// No nil check: deleting from a nil map, or a key that is not there, is a
	// no-op — so a pane with no suppressed warnings needs no special case.
	// The entry itself stays, because it also carries the session identity
	// noteSession compares against.
	delete(b.warned[paneID].kinds, kind)
}

// persistBootstrapped saves the current set of bootstrapped pane IDs,
// mirroring cursor_store.go's "persist only what actually changed" pattern:
// this is only ever called right after bootstrapped gains a new entry, never
// during seeding (which reflects state already on disk, not a change to it).
func (b *bootstrapWatcher) persistBootstrapped() {
	if b.persist == nil {
		return
	}
	ids := make([]string, 0, len(b.bootstrapped))
	for id := range b.bootstrapped {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b.persist(ids)
}

func (b *bootstrapWatcher) participant(host string) board.Participation {
	if b.participationForHost == nil {
		return nil
	}
	return b.participationForHost(host)
}
