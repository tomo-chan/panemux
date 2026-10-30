package main

import (
	"log"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/hosttools"
	"panemux/internal/session"
	"sync"
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
	cfg *config.Config, manager *session.Manager, execution *hostexec.Pool, tools *hosttools.Service,
	readers ...func(func(*config.Config)),
) (*board.BoardCache, *board.Relay, *bootstrapWatcher) {
	cache := board.NewBoardCache()
	paneHosts := configuredBoardPanes(cfg, true)
	cache.SyncParticipation(paneHosts)
	readConfig := func(visit func(*config.Config)) { visit(cfg) }
	if len(readers) > 0 {
		readConfig = readers[0]
	}
	routing := newBoardRouting(readConfig, execution, tools)
	var liveRouting func() board.Routing
	if cfg.AgentBoard.Enabled {
		liveRouting = routing
	}
	relay := board.NewRelay(cache, board.RelayConfig{
		Team: cfg.AgentBoard.Team, CurrentRouting: liveRouting, PaneHosts: paneHosts,
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
		CurrentPaneHosts:  func() map[string]string { return routing().PaneHosts },
		WatchEnabled:      cfg.AgentBoard.Enabled,
		SyncParticipation: cache.SyncParticipation,
		PaneModes: func() map[string]string {
			var modes map[string]string
			readConfig(func(current *config.Config) { modes = currentPaneModes(current) })
			return modes
		},
		ParticipationForHost: func(id string) board.Participation {
			client := routing().Clients[id]
			if client == nil {
				return nil
			}
			return client.(board.Participation)
		},
		Team: cfg.AgentBoard.Team, Persist: persistBootstrapState,
		Observe: cache.RecordParticipation,
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
func configuredBoardPanes(cfg *config.Config, warn bool) map[string]string {
	paneHosts := map[string]string{}
	for _, pane := range cfg.AllPanes() {
		if !cfg.AgentBoard.Enabled {
			continue
		}
		target, ok := boardHostForPane(cfg, pane)
		if !ok {
			if warn {
				log.Printf("Warning: agent board: pane %q is excluded; "+
					"add an entry named %q to ssh_connections to enable its board", pane.ID, pane.Connection)
			}
			continue
		}
		paneHosts[pane.ID] = target.BoardID()
	}
	return paneHosts
}

// Live routing is scheduled only for boards enabled at startup.
func newBoardRouting(
	readConfig func(func(*config.Config)), execution *hostexec.Pool, tools *hosttools.Service,
) func() board.Routing {
	var clientsMu sync.Mutex
	clients := map[host.Host]*agmsg.Client{}
	return func() board.Routing {
		result := board.Routing{PaneHosts: map[string]string{}, Clients: map[string]board.MessageClient{}}
		targets := []host.Host{}
		readConfig(func(current *config.Config) {
			targets = append(targets, host.Local)
			for name := range current.SSHConnections {
				targets = append(targets, host.Host(name))
			}
			result.PaneHosts = configuredBoardPanes(current, false)
		})
		clientsMu.Lock()
		defer clientsMu.Unlock()
		for _, target := range targets {
			if clients[target] == nil {
				var begin func() (func(), error)
				var revision func() uint64
				if tools != nil {
					begin = func() (func(), error) { return tools.BeginCall(hosttools.AgmsgTool, target) }
					revision = func() uint64 { return tools.Revision(hosttools.AgmsgTool, target) }
				}
				clients[target] = agmsg.NewClient(agmsg.ClientConfig{HostID: target.BoardID(), BeginCall: begin, Revision: revision,
					Runner: host.Bind(execution.WithoutDialWait(), target), SendRunner: host.Bind(execution, target)})
			}
			result.Clients[target.BoardID()] = clients[target]
		}
		return result
	}
}

func currentPaneModes(cfg *config.Config) map[string]string {
	modes := map[string]string{}
	for _, p := range cfg.AllPanes() {
		if cfg.AgentBoard.Enabled {
			modes[p.ID] = cfg.AgentBoard.Mode
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
