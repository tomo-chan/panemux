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

// setupBoard builds the shared BoardCache and Relay from cfg/manager. It
// never fails startup — agent board is additive, never load-bearing (see
// docs/agent-board.md's Design principles) — a host whose connection isn't
// usable simply gets no AgmsgClient and is logged, not fatal.
func setupBoard(cfg *config.Config, manager *session.Manager) (*board.BoardCache, *board.Relay) {
	cache := board.NewBoardCache()

	paneHosts := map[string]string{}
	for _, pane := range cfg.AllPanes() {
		if pane.AgentBoard.Enabled == nil || !*pane.AgentBoard.Enabled {
			continue
		}
		paneHosts[pane.ID] = boardHostForPane(pane)
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

	return cache, relay
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
// this never fails. For a remote host it needs a live session on that host
// implementing board.BoardExecutor, at least once, to resolve
// agent_board.agmsg_path's leading ~/ against the remote $HOME — if no
// board-enabled pane on that host currently has a started session, the host
// is skipped with a warning rather than failing startup. The RemoteAgmsgClient
// itself is handed a dynamicBoardExecutor, not that one-time snapshot,
// so it keeps working across the relay's lifetime even if the particular
// pane used to resolve agmsg_path is later restarted or deleted — see
// dynamicBoardExecutor's own comment for why a fixed snapshot is wrong here.
func newAgmsgClientForHost(
	cfg *config.Config, manager *session.Manager, paneHosts map[string]string, host string,
) (board.AgmsgClient, bool) {
	if host == boardHostIDLocal {
		// AgentBoard.AgmsgPath is intentionally left un-expanded by
		// internal/config (see config.go's expandPaths), since a single
		// config value is shared by every host, local and remote alike.
		// The local host expands against its own home directory here; a
		// remote host expands against its own $HOME below, via
		// ResolveRemoteAgmsgPath's SSH probe.
		return board.NewLocalAgmsgClient(expandLocalAgmsgPath(cfg.AgentBoard.AgmsgPath)), true
	}

	executors := findBoardExecutors(manager, paneHosts, host)
	if len(executors) == 0 {
		log.Printf("Warning: agent board: no reachable session for host %q, skipping", host)
		return nil, false
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), boardStartupProbeTimeout)
	defer cancel()
	path, err := board.ResolveRemoteAgmsgPath(probeCtx, executors[0], cfg.AgentBoard.AgmsgPath)
	if err != nil {
		log.Printf("Warning: agent board: resolving agmsg_path on host %q: %v", host, err)
		return nil, false
	}

	dynamicExecutor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: host}
	return board.NewRemoteAgmsgClient(host, path, dynamicExecutor), true
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
