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
	manager       *session.Manager
	paneHosts     map[string]string
	paneModes     map[string]string
	resolvedPaths map[string]string
	persist       func(paneIDs []string)
	bootstrapped  map[string]session.Session
	pending       map[string]session.Session
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
	writeAttempts    map[string]int
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
		givenUp:        map[string]session.Session{},
		writeAttempts:  map[string]int{},
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
	if existing, gaveUp := b.givenUp[paneID]; gaveUp && existing == sess {
		return
	}

	// A pane ID or team that doesn't match agmsg's own identifier alphabet
	// (internal/board.ValidAgmsgIdentifier — the same allowlist
	// RemoteAgmsgClient already enforces before any RunBoardCommand call)
	// can never be bootstrapped correctly: it would either break the shell
	// command the onboarding instruction tells the agent to run, or join
	// agmsg under an identity the relay can never address back. Skip and
	// warn once rather than write a broken instruction.
	if !board.ValidAgmsgIdentifier(paneID) || !board.ValidAgmsgIdentifier(b.team) {
		b.warnOnce(paneID, fmt.Sprintf(
			"agent board bootstrap: pane %q or team %q is not a valid agmsg identifier, skipping bootstrap",
			paneID, b.team,
		))
		delete(b.pending, paneID)
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

// agmsgPresent reports whether agmsg is present on host, and whether that
// question could be answered at all (checked=false means "couldn't tell this
// tick, try again next tick" — a transport error or an unreachable host, not
// "not present"). For a remote host, the probe goes through a
// dynamicBoardExecutor rather than a single findBoardExecutors candidate
// directly: a session can still be registered (and report a normal State())
// after its underlying connection has actually died, so trusting only the
// first candidate could permanently and silently treat a bootstrap-eligible
// host as unreachable even while some other pane on it is genuinely
// live — see dynamicBoardExecutor's own comment in board.go for the full
// rationale, which applies identically here.
func (b *bootstrapWatcher) agmsgPresent(ctx context.Context, paneID, host string) (present bool, checked bool) {
	path, ok := b.resolvedPaths[host]
	if !ok {
		b.warnOnce(paneID, fmt.Sprintf("agent board bootstrap: no resolved agmsg_path for host %q (pane %q)", host, paneID))
		return false, false
	}

	if host == boardHostIDLocal {
		return board.LocalAgmsgPresent(path), true
	}

	executor := &dynamicBoardExecutor{manager: b.manager, paneHosts: b.paneHosts, host: host}
	probeCtx, cancel := context.WithTimeout(ctx, bootstrapProbeTimeout)
	defer cancel()
	present, err := board.RemoteAgmsgPresent(probeCtx, executor, path)
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
