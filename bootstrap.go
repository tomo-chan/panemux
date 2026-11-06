package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"panemux/internal/board"
	"panemux/internal/session"
)

// defaultBootstrapPollInterval matches the relay interval for detection updates.
const defaultBootstrapPollInterval = 5 * time.Second

// bootstrapProbeTimeout bounds an individual remote presence probe
// checkPane makes. Deliberately shorter than boardStartupProbeTimeout
// (10s, sized for a one-shot startup call): pollOnce runs every board pane
// sequentially within one defaultBootstrapPollInterval-wide tick, so an
// unreachable host's probe must not be allowed to eat most of that budget
// and delay every other pane queued behind it in the same tick.
const bootstrapProbeTimeout = 3 * time.Second

// bootstrapWatcherConfig supplies fixed or live inputs the bootstrap
// watcher needs. Building it (which panes are board-enabled, which host each
// lives on, and participation ports) is
// the caller's job (board.go's setupBoard) — bootstrapWatcher itself takes
// no dependency on internal/config, mirroring board.RelayConfig's own
// existing dependency direction (see relay.go's RelayConfig comment).
type bootstrapWatcherConfig struct {
	CurrentPaneHosts     func() map[string]string
	SyncParticipation    func(map[string]string)
	Manager              *session.Manager
	PaneHosts            map[string]string
	ParticipationForHost func(string) board.Participation
	Observe              func(paneID, host, agentType string, instructed bool)
	Team                 string
	WatchEnabled         bool
}

// bootstrapWatcher detects agents for display only, from its single polling goroutine.
type bootstrapWatcher struct {
	currentPaneHosts     func() map[string]string
	syncParticipation    func(map[string]string)
	observe              func(string, string, string, bool)
	manager              *session.Manager
	paneHosts            map[string]string
	participationForHost func(string) board.Participation
	pending              map[string]session.Session
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
	warned       map[string]warnedPane
	team         string
	watchEnabled bool
}

// warnedPane is one pane's warning suppression: the kinds currently being
// suppressed for it, and the session they were recorded against.
//
// The session is part of it because a restarted pane is a new Session object —
// a new session's failure is a new streak, not a continuation of the old one.
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
	warnKindIdentifier = "identifier"
	warnKindDetect     = "detect"
	warnKindPath       = "path"
	warnKindProbe      = "presence-probe"
	warnKindAbsent     = "messaging-absent"
)

// newBootstrapWatcher returns a detection watcher ready to poll or run.
func newBootstrapWatcher(cfg bootstrapWatcherConfig) *bootstrapWatcher {
	return &bootstrapWatcher{
		manager:              cfg.Manager,
		currentPaneHosts:     cfg.CurrentPaneHosts,
		syncParticipation:    cfg.SyncParticipation,
		watchEnabled:         cfg.WatchEnabled,
		observe:              cfg.Observe,
		paneHosts:            cfg.PaneHosts,
		participationForHost: cfg.ParticipationForHost,
		team:                 cfg.Team,
		pending:              map[string]session.Session{},
		warned:               map[string]warnedPane{},
	}
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

// pollOnce refreshes detection and display without sending terminal input.
func (b *bootstrapWatcher) pollOnce(ctx context.Context) {
	b.refreshPanes()
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
			delete(b.pending, id)
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

// checkPane runs the bootstrap decision for one pane. See
// docs/agent-board.md's Bootstrap flow section for the algorithm this
// implements.
func (b *bootstrapWatcher) checkPane(ctx context.Context, paneID, host string) {
	participant := b.participant(host)
	if participant == nil {
		return
	}
	b.checkPaneWithParticipant(ctx, paneID, host, participant)
}

func (b *bootstrapWatcher) checkPaneWithParticipant(
	ctx context.Context, paneID, host string, participant board.Participation,
) {
	sess, ok := b.sessionFor(paneID)
	detectedKind := ""
	defer b.observePane(paneID, host, &detectedKind)
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
	// Keep presence checks debounced while detection remains visible each tick.
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
}

func (b *bootstrapWatcher) observePane(paneID, host string, kind *string) {
	if b.observe != nil {
		b.observe(paneID, host, *kind, false)
	}
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

func (b *bootstrapWatcher) participant(host string) board.Participation {
	if b.participationForHost == nil {
		return nil
	}
	return b.participationForHost(host)
}
