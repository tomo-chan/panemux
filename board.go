package main

import (
	"context"
	"fmt"
	"log"
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

func boardHostForPane(pane *config.PaneConfig) string {
	switch session.Type(pane.Type) {
	case session.TypeSSH, session.TypeSSHTmux:
		return pane.Connection
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
		return board.NewLocalAgmsgClient(cfg.AgentBoard.AgmsgPath), true
	}

	executor, ok := findBoardExecutor(manager, paneHosts, host)
	if !ok {
		log.Printf("Warning: agent board: no reachable session for host %q, skipping", host)
		return nil, false
	}

	path, err := board.ResolveRemoteAgmsgPath(context.Background(), executor, cfg.AgentBoard.AgmsgPath)
	if err != nil {
		log.Printf("Warning: agent board: resolving agmsg_path on host %q: %v", host, err)
		return nil, false
	}

	dynamicExecutor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: host}
	return board.NewRemoteAgmsgClient(host, path, dynamicExecutor), true
}

// dynamicBoardExecutor re-resolves a live session on host via
// findBoardExecutor on every call, rather than holding one session found at
// startup for the relay's entire lifetime. A fixed snapshot would bind a
// whole remote host's board features to whichever single pane happened to
// be picked first: an ordinary pane restart or delete (unrelated to Agent
// Board) closes that pane's underlying SSH client, and nothing would ever
// notice or fail over to another live pane on the same host, so board
// traffic to/from that host would silently and permanently break until the
// entire panemux process restarted. Re-resolving keeps working as long as
// any board-enabled pane on host has a live session, regardless of which
// one that is at any given moment.
type dynamicBoardExecutor struct {
	manager   *session.Manager
	paneHosts map[string]string
	host      string
}

func (d *dynamicBoardExecutor) RunBoardCommand(ctx context.Context, args []string) ([]byte, error) {
	executor, ok := findBoardExecutor(d.manager, d.paneHosts, d.host)
	if !ok {
		return nil, fmt.Errorf("agent board: no reachable session for host %q", d.host)
	}
	out, err := executor.RunBoardCommand(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("agent board: %w", err)
	}
	return out, nil
}

// findBoardExecutor returns a board.BoardExecutor for host by finding any
// currently-started session, among the board-enabled panes known to live
// on that host, whose session.Session also implements board.BoardExecutor.
func findBoardExecutor(manager *session.Manager, paneHosts map[string]string, host string) (board.BoardExecutor, bool) {
	for paneID, paneHost := range paneHosts {
		if paneHost != host {
			continue
		}
		sess, ok := manager.Get(paneID)
		if !ok {
			continue
		}
		if executor, ok := sess.(board.BoardExecutor); ok {
			return executor, true
		}
	}
	return nil, false
}
