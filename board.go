package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/session"
	"sync"
	"time"
)

const (
	defaultBoardPollInterval  = 5 * time.Second
	defaultBoardPollLimit     = 200
	defaultBoardBackfillLimit = 1000
	boardHostIDLocal          = "local"
	boardStartupProbeTimeout  = 10 * time.Second
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
	clients := map[string]board.AgmsgClient{}
	runners := map[string]host.Runner{}
	if len(paneHosts) > 0 {
		for _, target := range execution.Names() {
			id := target.BoardID()
			runner := host.Bind(execution.WithoutDialWait(), target)
			runners[id] = runner
			clients[id] = &hostAgmsgClient{id: id, path: cfg.AgentBoard.AgmsgPath, runner: runner}
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
		PaneModes:     func() map[string]string { return currentPaneModes(cfg) },
		RunnerForHost: func(id string) host.Runner { return runners[id] },
		ResolvePath: func(ctx context.Context, id string) (string, error) {
			return resolveHostAgmsgPath(ctx, runners[id], cfg.AgentBoard.AgmsgPath)
		}, Team: cfg.AgentBoard.Team, Persist: persistBootstrapState})
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
func resolveHostAgmsgPath(ctx context.Context, runner host.Runner, path string) (string, error) {
	resolved, err := board.ResolveRemoteAgmsgPath(ctx, runner, path)
	if err != nil {
		return "", fmt.Errorf("resolve host agmsg path: %w", err)
	}
	return resolved, nil
}

// hostAgmsgClient resolves a host lazily. Transport failures leave it retryable;
// a confirmed absent install is remembered, without detecting updates or installing tools.
// Initialization is serialized; established commands use independent exec channels.
type hostAgmsgClient struct {
	client   board.AgmsgClient
	runner   host.Runner
	id, path string
	mu       sync.Mutex
	absent   bool
}

func (c *hostAgmsgClient) HostID() string { return c.id }
func (c *hostAgmsgClient) ready(ctx context.Context) (board.AgmsgClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil || c.absent {
		return c.client, nil
	}
	ctx, cancel := context.WithTimeout(ctx, boardStartupProbeTimeout)
	defer cancel()
	path, err := resolveHostAgmsgPath(ctx, c.runner, c.path)
	if err != nil {
		return nil, err
	}
	present, err := board.RemoteAgmsgPresent(ctx, c.runner, path)
	if err != nil {
		return nil, fmt.Errorf("probe agmsg presence: %w", err)
	}
	if !present {
		c.absent = true
		log.Printf("Warning: agent board: no agmsg installation at %q on host %q, skipping that host", path, c.id)
		return nil, nil
	}
	version, err := board.RemoteAgmsgVersion(ctx, c.runner, path)
	if err != nil {
		log.Printf("Warning: agent board: reading agmsg version on host %q: %v", c.id, err)
	} else if warning := board.VersionMismatchWarning(c.id, version); warning != "" {
		log.Printf("Warning: %s", warning)
	}
	c.client = board.NewRemoteAgmsgClient(c.id, path, c.runner)
	return c.client, nil
}
func (c *hostAgmsgClient) Since(ctx context.Context, team, after string, limit int) ([]board.Row, error) {
	client, err := c.ready(ctx)
	if err != nil || client == nil {
		return nil, err
	}
	rows, err := client.Since(ctx, team, after, limit)
	if err != nil {
		return nil, fmt.Errorf("read host messages: %w", err)
	}
	return rows, nil
}
func (c *hostAgmsgClient) Send(ctx context.Context, team, from, to, body string) error {
	client, err := c.ready(ctx)
	if err != nil {
		return err
	}
	if client == nil {
		return errors.New("agmsg is not installed on host")
	}
	if err := client.Send(ctx, team, from, to, body); err != nil {
		return fmt.Errorf("send host message: %w", err)
	}
	return nil
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
