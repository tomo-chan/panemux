package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"panemux/internal/board"
	"panemux/internal/session"
)

// defaultBootstrapPollInterval matches defaultBoardPollInterval. The
// bootstrap watcher's own 2-tick debounce (see checkPane) adds roughly one
// extra interval of latency between an agent process starting and the
// onboarding instruction actually landing in its PTY; polling every 5s
// rather than 10s keeps that added latency from being felt twice.
const defaultBootstrapPollInterval = 5 * time.Second

// boardModeTurn and boardModeBoth mirror internal/config's own (unexported)
// agentBoardModeTurn/agentBoardModeBoth string values. Duplicated here
// rather than exported from internal/config because bootstrapWatcher
// deliberately takes no dependency on internal/config at all — see
// bootstrapWatcherConfig's own comment.
const (
	boardModeTurn = "turn"
	boardModeBoth = "both"
)

// bootstrapWatcherConfig is the precomputed, static input the bootstrap
// watcher needs. Building it (which panes are board-enabled, which host each
// lives on, each pane's configured mode, each host's resolved agmsg_path) is
// the caller's job (board.go's setupBoard) — bootstrapWatcher itself takes
// no dependency on internal/config, mirroring board.RelayConfig's own
// existing dependency direction (see relay.go's RelayConfig comment).
type bootstrapWatcherConfig struct {
	Manager       *session.Manager
	PaneHosts     map[string]string
	PaneModes     map[string]string
	ResolvedPaths map[string]string
	Persist       func(paneIDs []string)
	Team          string
}

// bootstrapWatcher polls board-enabled panes for a newly-started, agmsg-
// detectable coding-agent process and writes a one-time onboarding
// instruction into that pane's PTY. See docs/agent-board.md's Bootstrap flow
// section for the full design.
//
// No mutex: unlike Relay, every field here is touched only from the single
// goroutine driving runLoop — nothing else ever reads or writes this
// watcher's state.
type bootstrapWatcher struct {
	manager          *session.Manager
	paneHosts        map[string]string
	paneModes        map[string]string
	resolvedPaths    map[string]string
	persist          func(paneIDs []string)
	bootstrapped     map[string]session.Session
	pending          map[string]session.Session
	presenceWarned   map[string]bool
	team             string
	persistedPaneIDs []string
	seeded           bool
}

// newBootstrapWatcher returns a bootstrapWatcher ready to have persisted
// state loaded (LoadPersistedState) and then be polled or run.
func newBootstrapWatcher(cfg bootstrapWatcherConfig) *bootstrapWatcher {
	return &bootstrapWatcher{
		manager:        cfg.Manager,
		paneHosts:      cfg.PaneHosts,
		paneModes:      cfg.PaneModes,
		resolvedPaths:  cfg.ResolvedPaths,
		team:           cfg.Team,
		persist:        cfg.Persist,
		bootstrapped:   map[string]session.Session{},
		pending:        map[string]session.Session{},
		presenceWarned: map[string]bool{},
	}
}

// LoadPersistedState seeds the pane IDs that were already bootstrapped as of
// the last save, consumed exactly once by the first pollOnce call. Call
// before Run/pollOnce, mirroring Relay.LoadCursors.
func (b *bootstrapWatcher) LoadPersistedState(paneIDs []string) {
	b.persistedPaneIDs = paneIDs
}

// HasWork reports whether there is any board-enabled pane to watch at all.
// Callers use this to skip starting the poll loop entirely, mirroring
// Relay.HasClients.
func (b *bootstrapWatcher) HasWork() bool {
	return len(b.paneHosts) > 0
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
// a panemux restart (an already-onboarded, still-running agent is seeded
// before its pane is ever checked, so it is never re-bootstrapped) while
// still allowing a pane that restarts within this same process's lifetime to
// be bootstrapped again (its new Session object won't match the seeded one).
func (b *bootstrapWatcher) pollOnce(ctx context.Context) {
	if !b.seeded {
		for _, paneID := range b.persistedPaneIDs {
			if sess, ok := b.manager.Get(paneID); ok {
				b.bootstrapped[paneID] = sess
			}
		}
		b.seeded = true
	}
	for paneID, host := range b.paneHosts {
		b.checkPane(ctx, paneID, host)
	}
}

// checkPane runs the bootstrap decision for one pane. See
// docs/agent-board.md's Bootstrap flow section for the algorithm this
// implements.
func (b *bootstrapWatcher) checkPane(ctx context.Context, paneID, host string) {
	sess, ok := b.manager.Get(paneID)
	if !ok {
		delete(b.pending, paneID)
		return
	}
	if existing, alreadyBootstrapped := b.bootstrapped[paneID]; alreadyBootstrapped && existing == sess {
		return
	}

	detector, ok := sess.(session.AgentTypeDetector)
	if !ok {
		delete(b.pending, paneID)
		return
	}
	agmsgType, detected, err := detector.DetectInteractiveAgentType()
	if err != nil {
		log.Printf("Warning: agent board bootstrap: detecting agent type for pane %q: %v", paneID, err)
		delete(b.pending, paneID)
		return
	}
	if !detected {
		delete(b.pending, paneID)
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

	present, checked := b.agmsgPresent(ctx, paneID, host)
	if !checked {
		return
	}
	if !present {
		b.warnOnce(paneID, fmt.Sprintf(
			"agent board bootstrap: agmsg not found on host %q for pane %q, skipping bootstrap", host, paneID,
		))
		return
	}

	instruction := buildBootstrapInstruction(b.resolvedPaths[host], b.team, paneID, agmsgType, b.paneModes[paneID])
	payload := []byte(instruction + "\r")
	n, err := sess.Write(payload)
	if err != nil || n != len(payload) {
		log.Printf(
			"Warning: agent board bootstrap: writing onboarding instruction to pane %q: %v (wrote %d/%d bytes)",
			paneID, err, n, len(payload),
		)
		return
	}

	b.bootstrapped[paneID] = sess
	delete(b.pending, paneID)
	b.persistBootstrapped()
}

// agmsgPresent reports whether agmsg is present on host, and whether that
// question could be answered at all (checked=false means "couldn't tell this
// tick, try again next tick" — a transport error or an unreachable host, not
// "not present").
func (b *bootstrapWatcher) agmsgPresent(ctx context.Context, paneID, host string) (present bool, checked bool) {
	path, ok := b.resolvedPaths[host]
	if !ok {
		b.warnOnce(paneID, fmt.Sprintf("agent board bootstrap: no resolved agmsg_path for host %q (pane %q)", host, paneID))
		return false, false
	}

	if host == boardHostIDLocal {
		return board.LocalAgmsgPresent(path), true
	}

	executors := findBoardExecutors(b.manager, b.paneHosts, host)
	if len(executors) == 0 {
		b.warnOnce(paneID, fmt.Sprintf("agent board bootstrap: no reachable session for host %q (pane %q)", host, paneID))
		return false, false
	}

	probeCtx, cancel := context.WithTimeout(ctx, boardStartupProbeTimeout)
	defer cancel()
	present, err := board.RemoteAgmsgPresent(probeCtx, executors[0], path)
	if err != nil {
		b.warnOnce(paneID, fmt.Sprintf(
			"agent board bootstrap: checking agmsg presence on host %q (pane %q): %v", host, paneID, err,
		))
		return false, false
	}
	return present, true
}

func (b *bootstrapWatcher) warnOnce(paneID, message string) {
	if b.presenceWarned[paneID] {
		return
	}
	b.presenceWarned[paneID] = true
	log.Printf("Warning: %s", message)
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

// buildBootstrapInstruction builds the one-time onboarding instruction
// written into a pane's PTY. It tells the agent to invoke agmsg's own
// scripts (join.sh/send.sh/delivery.sh) directly with their verified
// positional-argument signatures, rather than agmsg's slash-command
// shorthand (`/agmsg ...` or `$agmsg ...` depending on agent type — the
// prefix itself differs per agmsg's own per-type `cmd_prefix` driver
// setting, so hardcoding either one would silently be wrong for the other
// half of agmsg's supported agent types). agmsgType is the agmsg-recognized
// type string session.AgentTypeDetector reported (e.g. "claude-code",
// "gemini") and is passed as both join.sh's and delivery.sh's own `<type>`
// argument. mode is the pane's agent_board.mode config value ("" is
// treated the same as "monitor"/"off": no step 4 at all, since agmsg's own
// default already behaves like monitor without any explicit call).
func buildBootstrapInstruction(agmsgPath, team, paneID, agmsgType, mode string) string {
	var b strings.Builder
	b.WriteString("Please set up Agent Board messaging for this session using agmsg's own scripts directly")
	b.WriteString(" (not the /agmsg or $agmsg slash-command, since its exact prefix differs by agent type):\n")
	fmt.Fprintf(&b, "1. Join the agmsg team %q by running:\n", team)
	fmt.Fprintf(&b, "   %s/scripts/join.sh %q %q %q \"$(pwd)\" --force\n", agmsgPath, team, paneID, agmsgType)
	b.WriteString("   Use exactly that agent_id — not a name of your own choosing — since every cross-pane\n")
	b.WriteString("   and relay address in Agent Board assumes from/to values are pane IDs.\n")
	b.WriteString("2. From now on, send every board-related message (status reports and messages to other\n")
	b.WriteString("   panes) with:\n")
	fmt.Fprintf(&b, "   %s/scripts/send.sh %q %q \"<to>\" \"<body>\" --force\n", agmsgPath, team, paneID)
	b.WriteString("3. Periodically self-report your status to \"_system\" using that same send.sh invocation,\n")
	b.WriteString("   with a JSON body shaped exactly like:\n")
	b.WriteString("   {\"kind\":\"board_status\",\"state\":\"working|idle|waiting\",\"cwd\":\"...\",\"branch\":\"...\",\n")
	b.WriteString("   \"repo\":\"...\",\"pr_url\":\"...\",\"last_tool\":\"...\",\"summary\":\"...\"}\n")
	b.WriteString("   Omit fields you don't currently have. Send an update whenever your state changes\n")
	b.WriteString("   meaningfully.\n")
	if mode == boardModeTurn || mode == boardModeBoth {
		b.WriteString("4. Also run:\n")
		fmt.Fprintf(&b, "   %s/scripts/delivery.sh set %q %q \"$(pwd)\"\n", agmsgPath, mode, agmsgType)
		b.WriteString("   This prints an AGMSG-DIRECTIVE: block — read it and follow its instructions exactly;\n")
		b.WriteString("   it configures how you receive incoming board messages from now on.\n")
	}
	return b.String()
}
