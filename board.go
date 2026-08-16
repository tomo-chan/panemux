package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

const (
	defaultBoardPollInterval  = 5 * time.Second
	defaultBoardPollLimit     = 200
	defaultBoardBackfillLimit = 1000
	boardHostIDLocal          = "local"

	// boardStartupProbeTimeout bounds the one-time remote $HOME probe
	// newAgmsgClientForHost makes to resolve agmsg_path's leading ~/. This
	// runs during setupBoard, before the HTTP listener is up — an
	// unresponsive SSH host must not be able to hang process startup
	// indefinitely just because one board-enabled pane happens to be
	// unreachable (board features are additive, never load-bearing).
	boardStartupProbeTimeout = 10 * time.Second
)

// setupBoard builds the shared BoardCache, Relay, and bootstrapWatcher from
// cfg/manager. It never fails startup — agent board is additive, never
// load-bearing (see docs/agent-board.md's Design principles) — a host whose
// connection isn't usable simply gets no AgmsgClient/bootstrap eligibility
// and is logged, not fatal.
func setupBoard(cfg *config.Config, manager *session.Manager) (*board.BoardCache, *board.Relay, *bootstrapWatcher) {
	cache := board.NewBoardCache()

	paneHosts := map[string]string{}
	paneModes := map[string]string{}
	for _, pane := range cfg.AllPanes() {
		if pane.AgentBoard.Enabled == nil || !*pane.AgentBoard.Enabled {
			continue
		}
		paneHosts[pane.ID] = boardHostForPane(pane)
		paneModes[pane.ID] = pane.AgentBoard.Mode
	}

	clients := map[string]board.AgmsgClient{}
	for _, host := range distinctBoardHosts(paneHosts) {
		client, ok := newAgmsgClientForHost(cfg, manager, paneHosts, host)
		if !ok {
			continue
		}
		clients[host] = client
	}

	relay := board.NewRelay(cache, board.RelayConfig{
		Team:           cfg.AgentBoard.Team,
		Clients:        clients,
		PaneHosts:      paneHosts,
		Limit:          defaultBoardPollLimit,
		BackfillLimit:  defaultBoardBackfillLimit,
		PersistCursors: persistBoardCursors,
	})

	if path, err := board.DefaultCursorFilePath(); err == nil {
		if entries, err := board.LoadCursorFile(path); err == nil {
			relay.LoadCursors(entries)
		} else {
			log.Printf("Warning: agent board: loading relay cursor file: %v", err)
		}
	}

	bootstrap := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     paneHosts,
		PaneModes:     paneModes,
		ResolvedPaths: resolveBootstrapPaths(cfg, manager, paneHosts),
		Team:          cfg.AgentBoard.Team,
		Persist:       persistBootstrapState,
	})
	if path, err := board.DefaultBootstrapStateFilePath(); err == nil {
		if paneIDs, err := board.LoadBootstrapState(path); err == nil {
			bootstrap.LoadPersistedState(paneIDs)
		} else {
			log.Printf("Warning: agent board bootstrap: loading bootstrap state file: %v", err)
		}
	}

	warnOnAgmsgVersionMismatch(manager, paneHosts, resolveBootstrapPaths(cfg, manager, paneHosts))

	return cache, relay, bootstrap
}

// warnOnAgmsgVersionMismatch reports, once per board-enabled host at
// startup, an agmsg install that is not board.TestedAgmsgVersion. It never
// blocks startup: see board.VersionMismatchWarning for why a mismatch is a
// warning rather than a refusal. An unreadable VERSION is logged at the
// same level and otherwise ignored — panemux cannot claim a mismatch it
// could not observe.
func warnOnAgmsgVersionMismatch(
	manager *session.Manager, paneHosts map[string]string, resolvedPaths map[string]string,
) {
	for _, host := range distinctBoardHosts(paneHosts) {
		agmsgPath, ok := resolvedPaths[host]
		if !ok {
			continue
		}

		var (
			installed string
			err       error
		)
		if host == boardHostIDLocal {
			installed, err = board.LocalAgmsgVersion(agmsgPath)
		} else {
			executors := findBoardExecutors(manager, paneHosts, host)
			if len(executors) == 0 {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), boardStartupProbeTimeout)
			installed, err = board.RemoteAgmsgVersion(ctx, executors[0], agmsgPath)
			cancel()
		}
		if err != nil {
			log.Printf("Warning: agent board: reading agmsg version on host %q: %v", host, err)
			continue
		}
		if warning := board.VersionMismatchWarning(host, installed); warning != "" {
			log.Printf("Warning: %s", warning)
		}
	}
}

// resolveBootstrapPaths resolves agmsg_path for every distinct board-enabled
// host, independently of newAgmsgClientForHost's own resolution for the
// relay's client map — see bootstrapWatcherConfig's ResolvedPaths comment
// for why this deliberately duplicates that probe rather than sharing its
// result.
func resolveBootstrapPaths(
	cfg *config.Config, manager *session.Manager, paneHosts map[string]string,
) map[string]string {
	resolved := map[string]string{}
	for _, host := range distinctBoardHosts(paneHosts) {
		if path, ok := resolveAgmsgPathForHost(cfg, manager, paneHosts, host); ok {
			resolved[host] = path
		}
	}
	return resolved
}

func persistBoardCursors(entries []board.CursorEntry) {
	path, err := board.DefaultCursorFilePath()
	if err != nil {
		log.Printf("Warning: agent board: resolving relay cursor file path: %v", err)
		return
	}
	if err := board.SaveCursorFile(path, entries); err != nil {
		log.Printf("Warning: agent board: persisting relay cursor: %v", err)
	}
}

func persistBootstrapState(paneIDs []string) {
	path, err := board.DefaultBootstrapStateFilePath()
	if err != nil {
		log.Printf("Warning: agent board bootstrap: resolving bootstrap state file path: %v", err)
		return
	}
	if err := board.SaveBootstrapState(path, paneIDs); err != nil {
		log.Printf("Warning: agent board bootstrap: persisting bootstrap state: %v", err)
	}
}

// boardHostForPane prefixes an SSH connection alias with "ssh:" before
// using it as a board host key. Without the prefix, an operator naming an
// SSH connection alias literally "local" would collide with
// boardHostIDLocal, silently routing that remote host's board traffic
// through the local agmsg installation and misclassifying its panes as
// local for from-validation purposes — nothing in internal/config's
// connection-name validation forbids that alias today. The prefix makes a
// collision structurally impossible instead of relying on catching the
// misconfiguration.
func boardHostForPane(pane *config.PaneConfig) string {
	switch session.Type(pane.Type) {
	case session.TypeSSH, session.TypeSSHTmux:
		return "ssh:" + pane.Connection
	default:
		return boardHostIDLocal
	}
}

func distinctBoardHosts(paneHosts map[string]string) []string {
	seen := make(map[string]bool, len(paneHosts))
	var hosts []string
	for _, host := range paneHosts {
		if seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts
}

// newAgmsgClientForHost builds the AgmsgClient for host. For the local host
// this never fails. For a remote host it needs resolveAgmsgPathForHost to
// succeed (a live session on that host implementing board.BoardExecutor, at
// least once) — if not, the host is skipped with a warning rather than
// failing startup. The RemoteAgmsgClient itself is handed a
// dynamicBoardExecutor, not that one-time snapshot, so it keeps working
// across the relay's lifetime even if the particular pane used to resolve
// agmsg_path is later restarted or deleted — see dynamicBoardExecutor's own
// comment for why a fixed snapshot is wrong here.
func newAgmsgClientForHost(
	cfg *config.Config, manager *session.Manager, paneHosts map[string]string, host string,
) (board.AgmsgClient, bool) {
	path, ok := resolveAgmsgPathForHost(cfg, manager, paneHosts, host)
	if !ok {
		return nil, false
	}
	if !agmsgPresentOnHost(manager, paneHosts, host, path) {
		// Skipping the client is what keeps an absent agmsg quiet. Building
		// one anyway means the relay polls a host that cannot answer, and
		// logs the same exec failure every few seconds for as long as
		// panemux runs — noise that buries the one line naming the cause,
		// in exactly the situation (agmsg not installed yet) the README
		// calls the most likely first failure.
		log.Printf(
			"Warning: agent board: no agmsg installation at %q on host %q, "+
				"skipping that host (panes there stay off the board)",
			path, host,
		)
		return nil, false
	}
	if host == boardHostIDLocal {
		return board.NewLocalAgmsgClient(path), true
	}

	dynamicExecutor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: host}
	return board.NewRemoteAgmsgClient(host, path, dynamicExecutor), true
}

// agmsgPresentOnHost reports whether host carries an agmsg install at path.
// A remote host with no reachable executor is reported as present: panemux
// cannot check, and refusing to build the client would turn a transient
// connectivity problem into a permanently board-less host for the rest of
// this process's life.
func agmsgPresentOnHost(
	manager *session.Manager, paneHosts map[string]string, host, path string,
) bool {
	if host == boardHostIDLocal {
		return board.LocalAgmsgPresent(path)
	}

	executors := findBoardExecutors(manager, paneHosts, host)
	if len(executors) == 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), boardStartupProbeTimeout)
	defer cancel()
	present, err := board.RemoteAgmsgPresent(ctx, executors[0], path)
	if err != nil {
		log.Printf("Warning: agent board: checking agmsg on host %q: %v", host, err)
		return true
	}
	return present
}

// resolveAgmsgPathForHost expands agent_board.agmsg_path's leading ~/ for
// host: locally against panemux's own home directory (AgentBoard.AgmsgPath
// is intentionally left un-expanded by internal/config — see config.go's
// expandPaths — since a single config value is shared by every host, local
// and remote alike), remotely via a bounded SSH $HOME probe using any live
// BoardExecutor session currently available for that host. Returns
// ok=false, having already logged a warning naming host, if no live
// session/executor is reachable or the probe itself fails — callers use
// this to skip whatever host-scoped resource they were about to build (an
// AgmsgClient, a bootstrap-eligibility entry) rather than propagating the
// error further. Shared by newAgmsgClientForHost and the bootstrap
// watcher's own setup so the two independently-scoped subsystems (relay
// client construction vs. bootstrap eligibility) never disagree about how
// a host's agmsg_path resolves, without coupling them to each other.
func resolveAgmsgPathForHost(
	cfg *config.Config, manager *session.Manager, paneHosts map[string]string, host string,
) (string, bool) {
	if host == boardHostIDLocal {
		return expandLocalAgmsgPath(cfg.AgentBoard.AgmsgPath), true
	}

	executors := findBoardExecutors(manager, paneHosts, host)
	if len(executors) == 0 {
		log.Printf("Warning: agent board: no reachable session for host %q, skipping", host)
		return "", false
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), boardStartupProbeTimeout)
	defer cancel()
	path, err := board.ResolveRemoteAgmsgPath(probeCtx, executors[0], cfg.AgentBoard.AgmsgPath)
	if err != nil {
		log.Printf("Warning: agent board: resolving agmsg_path on host %q: %v", host, err)
		return "", false
	}
	return path, true
}

// expandLocalAgmsgPath expands a leading ~/ in path against panemux's own
// local home directory. A failure to resolve the home directory leaves path
// unchanged, matching internal/config's own expandPaths behavior for other
// local-only paths (SSH key/known_hosts files).
func expandLocalAgmsgPath(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// dynamicBoardExecutor re-resolves the live sessions on host via
// findBoardExecutors on every call, rather than holding one session found at
// startup for the relay's entire lifetime. A fixed snapshot would bind a
// whole remote host's board features to whichever single pane happened to
// be picked first: an ordinary pane restart or delete (unrelated to Agent
// Board) closes that pane's underlying SSH client, and nothing would ever
// notice or fail over to another live pane on the same host, so board
// traffic to/from that host would silently and permanently break until the
// entire panemux process restarted.
//
// RunBoardCommand doesn't stop at the first candidate returned, either: a
// session can still be registered in the Manager after its underlying
// connection has died (State() reporting can lag reality, e.g. between a
// dropped SSH connection and the read loop noticing EOF), so trusting the
// first match alone can silently and repeatedly pick a dead session even
// though a different, genuinely live pane on the same host would have
// worked. Trying every candidate in turn means any one working session on
// host is enough, regardless of which pane it belongs to or in what order
// the candidates happen to be found.
type dynamicBoardExecutor struct {
	manager   *session.Manager
	paneHosts map[string]string
	host      string
}

func (d *dynamicBoardExecutor) RunBoardCommand(ctx context.Context, args []string) ([]byte, error) {
	executors := findBoardExecutors(d.manager, d.paneHosts, d.host)
	if len(executors) == 0 {
		return nil, fmt.Errorf("agent board: no reachable session for host %q", d.host)
	}

	var lastErr error
	for _, executor := range executors {
		out, err := executor.RunBoardCommand(ctx, args)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("agent board: no working session for host %q (tried %d): %w", d.host, len(executors), lastErr)
}

// findBoardExecutors returns every board.BoardExecutor currently available
// for host: every currently-started session, among the board-enabled panes
// known to live on that host, whose session.Session also implements
// board.BoardExecutor. Sorted by pane ID for deterministic ordering — map
// iteration order is randomized, and dynamicBoardExecutor's tests rely on a
// stable candidate order to be able to assert which one actually answered.
func findBoardExecutors(manager *session.Manager, paneHosts map[string]string, host string) []board.BoardExecutor {
	var paneIDs []string
	for paneID, paneHost := range paneHosts {
		if paneHost == host {
			paneIDs = append(paneIDs, paneID)
		}
	}
	sort.Strings(paneIDs)

	var executors []board.BoardExecutor
	for _, paneID := range paneIDs {
		sess, ok := manager.Get(paneID)
		if !ok {
			continue
		}
		if executor, ok := sess.(board.BoardExecutor); ok {
			executors = append(executors, executor)
		}
	}
	return executors
}
