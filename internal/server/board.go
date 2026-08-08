package server

import (
	"context"
	"log"

	"panemux/internal/api"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

// wireAgentBoard builds the Agent Board relay for every board-enabled pane
// in cfg, registers it with apiHandler, and starts its poll loop in a
// background goroutine. Returns nil (no goroutine started, board endpoints
// stay disabled at 503) when no pane has agent_board.enabled: true — Agent
// Board is additive, never load-bearing, per docs/agent-board.md's Design
// principles. The returned CancelFunc, if non-nil, must be called on
// shutdown to stop the relay goroutine.
func wireAgentBoard(cfg *config.Config, manager *session.Manager, apiHandler *api.Handler) context.CancelFunc {
	panes := boardEnabledPanes(cfg)
	if len(panes) == 0 {
		return nil
	}

	// cfg.AgentBoard.Team is normally already non-empty by the time New()
	// runs (config.Load()/config.Default() both normalize it), but a
	// caller-constructed Config that skipped normalization is defended
	// against here too.
	team := cfg.AgentBoard.Team
	if team == "" {
		team = config.DefaultAgentBoardTeam
	}

	refs := make([]board.PaneRef, 0, len(panes))
	hostSet := make(map[string]bool)
	for _, p := range panes {
		host := paneBoardHostID(p)
		refs = append(refs, board.PaneRef{ID: p.ID, HostID: host})
		hostSet[host] = true
	}
	resolver := board.NewStaticPaneResolver(refs)

	pairs := make([]board.HostTeam, 0, len(hostSet))
	for host := range hostSet {
		pairs = append(pairs, board.HostTeam{Host: host, Team: team})
	}

	cursorPath, err := board.DefaultCursorPath()
	var cursors board.CursorStore
	if err != nil {
		log.Printf("board: failed to resolve default cursor path, falling back to in-memory cursors: %v", err)
		cursors = board.NewMemCursorStore()
	} else {
		cursors = board.NewFileCursorStore(cursorPath)
	}

	cache := board.NewBoardCache()
	relay := board.NewRelay(cache, resolver, cursors, pairs)

	registerBoardClients(relay, cfg, manager, panes, hostSet)

	if err := relay.LoadCursors(); err != nil {
		log.Printf("board: failed to load persisted relay cursors, starting from empty: %v", err)
	}

	apiHandler.EnableBoard(cache, relay, team)

	ctx, cancel := context.WithCancel(context.Background())
	go relay.Run(ctx, board.DefaultPollInterval)
	return cancel
}

// boardEnabledPanes returns every pane with agent_board.enabled: true.
func boardEnabledPanes(cfg *config.Config) []*config.PaneConfig {
	var out []*config.PaneConfig
	for _, p := range cfg.AllPanes() {
		if p.AgentBoard != nil && p.AgentBoard.Enabled {
			out = append(out, p)
		}
	}
	return out
}

// paneBoardHostID mirrors session.BoardHostID's local/ssh-connection-name
// split, computed from static config rather than a live session, since the
// relay's PaneResolver only needs to know host identity, never a live
// connection.
func paneBoardHostID(p *config.PaneConfig) string {
	switch p.Type {
	case "ssh", "ssh_tmux":
		return p.Connection
	default:
		return board.LocalHostID
	}
}

// registerBoardClients builds and registers one AgmsgClient per host
// referenced by hostSet: a LocalAgmsgClient for board.LocalHostID (agmsg on
// the host panemux itself runs on), and a RemoteAgmsgClient per SSH host,
// found by locating any already-live board-enabled pane session on that
// host and using its BoardExecutor/BoardHomeDirer capabilities. A host with
// no live, capable session is skipped with a warning — that host's board
// traffic is simply not relayed until a capable session exists, matching
// "additive, never load-bearing."
func registerBoardClients(
	relay *board.Relay, cfg *config.Config, manager *session.Manager, panes []*config.PaneConfig, hostSet map[string]bool,
) {
	if hostSet[board.LocalHostID] {
		localPath, err := board.ExpandLocalAgmsgPath(cfg.AgentBoard.AgmsgPath)
		if err != nil {
			log.Printf("board: failed to expand local agmsg_path %q: %v", cfg.AgentBoard.AgmsgPath, err)
		} else {
			relay.RegisterClient(board.NewLocalAgmsgClient(localPath))
		}
	}

	for host := range hostSet {
		if host == board.LocalHostID {
			continue
		}
		registerRemoteBoardClient(relay, cfg, manager, panes, host)
	}
}

func registerRemoteBoardClient(
	relay *board.Relay, cfg *config.Config, manager *session.Manager, panes []*config.PaneConfig, host string,
) {
	for _, p := range panes {
		if paneBoardHostID(p) != host {
			continue
		}
		sess, ok := manager.Get(p.ID)
		if !ok {
			continue
		}
		executor, ok := sess.(session.BoardExecutor)
		if !ok {
			continue
		}
		homeDirer, ok := sess.(session.BoardHomeDirer)
		if !ok {
			continue
		}

		agmsgPath := cfg.AgentBoard.AgmsgPath
		if agmsgPath == "~" || len(agmsgPath) >= 2 && agmsgPath[:2] == "~/" {
			home, err := homeDirer.BoardHomeDir(context.Background())
			if err != nil {
				log.Printf("board: failed to resolve remote home dir on host %q: %v", host, err)
				return
			}
			agmsgPath = board.ExpandRemoteAgmsgPath(agmsgPath, home)
		}

		relay.RegisterClient(board.NewRemoteAgmsgClient(host, agmsgPath, executor))
		return
	}
	log.Printf("board: no live, board-capable session found for host %q; its board traffic will not be relayed", host)
}
