package main

import (
	"log"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/session"
	"time"
)

const (
	defaultBoardPollInterval  = 5 * time.Second
	defaultBoardPollLimit     = 200
	defaultBoardBackfillLimit = 1000
	boardHostIDLocal          = "local"
)

// setupBoard separates host execution from pane eligibility. A client is retained
// even when its host cannot be reached at startup; polling retries through the pool.
func setupBoard(
	cfg *config.Config, manager *session.Manager, execution *hostexec.Pool,
) (*board.BoardCache, *board.Relay, *bootstrapWatcher) {
	cache := board.NewBoardCache()
	paneHosts := map[string]string{}
	for _, pane := range cfg.AllPanes() {
		if pane.AgentBoard.Enabled == nil || !*pane.AgentBoard.Enabled {
			continue
		}
		target, ok := boardHostForPane(cfg, pane)
		if !ok {
			log.Printf("Warning: agent board: pane %q is excluded; "+
				"add an entry named %q to ssh_connections to enable its board", pane.ID, pane.Connection)
			continue
		}
		paneHosts[pane.ID] = target.BoardID()
	}
	clients := map[string]board.MessageClient{}
	participants := map[string]board.Participation{}
	if len(paneHosts) > 0 {
		for _, target := range execution.Names() {
			id := target.BoardID()
			runner := host.Bind(execution.WithoutDialWait(), target)
			client := agmsg.NewClient(agmsg.ClientConfig{
				HostID: id, Path: cfg.AgentBoard.AgmsgPath, Runner: runner, SendRunner: host.Bind(execution, target),
			})
			clients[id] = client
			participants[id] = client
		}
	}
	relay := board.NewRelay(cache, board.RelayConfig{
		Team: cfg.AgentBoard.Team, Clients: clients, PaneHosts: paneHosts,
		Limit: defaultBoardPollLimit, BackfillLimit: defaultBoardBackfillLimit, PersistCursors: persistBoardCursors,
	})
	if path, err := board.DefaultCursorFilePath(); err == nil {
		if entries, err := board.LoadCursorFile(path); err == nil {
			relay.LoadCursors(entries)
		} else {
			log.Printf("Warning: agent board: loading relay cursor file: %v", err)
		}
	}
	bootstrap := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager: manager, PaneHosts: paneHosts,
		PaneModes:            func() map[string]string { return currentPaneModes(cfg) },
		ParticipationForHost: func(id string) board.Participation { return participants[id] },
		Team:                 cfg.AgentBoard.Team, Persist: persistBootstrapState,
	})
	if path, err := board.DefaultBootstrapStateFilePath(); err == nil {
		if ids, err := board.LoadBootstrapState(path); err == nil {
			bootstrap.LoadPersistedState(ids)
		} else {
			log.Printf("Warning: agent board bootstrap: loading bootstrap state file: %v", err)
		}
	}
	return cache, relay, bootstrap
}
func currentPaneModes(cfg *config.Config) map[string]string {
	modes := map[string]string{}
	for _, p := range cfg.AllPanes() {
		if p.AgentBoard.Enabled != nil && *p.AgentBoard.Enabled {
			modes[p.ID] = p.AgentBoard.Mode
		}
	}
	return modes
}
func boardHostForPane(cfg *config.Config, pane *config.PaneConfig) (host.Host, bool) {
	if pane.Type == string(session.TypeSSH) || pane.Type == string(session.TypeSSHTmux) {
		_, ok := cfg.SSHConnections[pane.Connection]
		return host.Host(pane.Connection), ok
	}
	return host.Local, true
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
