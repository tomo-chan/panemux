// Package agmsg implements the board messaging and participation ports using
// operator-installed agmsg scripts and the shared host execution port.
package agmsg

import (
	"context"
	"errors"
	"fmt"
	"log"
	"panemux/internal/board"
	"panemux/internal/host"
	"sync"
	"time"
)

const boardStartupProbeTimeout = 10 * time.Second
const DefaultPath = "~/.agents/skills/agmsg"

// ClientConfig binds a message transport to one host. Polling does not wait for
// a pending dial; sends can use the waiting execution lane independently.
type ClientConfig struct {
	Runner     host.Runner
	SendRunner host.Runner
	HostID     string
	Path       string
}

func NewClient(cfg ClientConfig) *Client {
	path := cfg.Path
	if path == "" {
		path = DefaultPath
	}
	return &Client{id: cfg.HostID, path: path, runner: cfg.Runner, sendRunner: cfg.SendRunner}
}

var _ board.MessageClient = (*Client)(nil)
var _ board.Participation = (*Client)(nil)

func resolveHostAgmsgPath(ctx context.Context, runner host.Runner, path string) (string, error) {
	resolved, err := ResolveRemoteAgmsgPath(ctx, runner, path)
	if err != nil {
		return "", fmt.Errorf("resolve host agmsg path: %w", err)
	}
	return resolved, nil
}

// Client resolves a host lazily. Transport failures leave it retryable;
// a confirmed absent install is remembered, without detecting updates or installing tools.
// Discovery is published once; probes leave the cache mutex free while doing I/O.
type Client struct {
	runner            host.Runner
	sendRunner        host.Runner
	resolvedPath      *string
	participationPath string
	id, path          string
	participationMu   sync.Mutex
	mu                sync.Mutex
	absent            bool
}

func (c *Client) HostID() string { return c.id }
func (c *Client) ready(ctx context.Context, runner host.Runner) (board.MessageClient, error) {
	c.mu.Lock()
	client, known := c.cachedClient(runner)
	c.mu.Unlock()
	if known {
		return client, nil
	}
	ctx, cancel := context.WithTimeout(ctx, boardStartupProbeTimeout)
	defer cancel()
	path, err := resolveHostAgmsgPath(ctx, runner, c.path)
	if err != nil {
		return nil, err
	}
	present, err := RemoteAgmsgPresent(ctx, runner, path)
	if err != nil {
		return nil, fmt.Errorf("probe agmsg presence: %w", err)
	}
	if !present {
		return c.finishProbe(runner, path, false, "", nil), nil
	}
	version, versionErr := RemoteAgmsgVersion(ctx, runner, path)
	return c.finishProbe(runner, path, true, version, versionErr), nil
}

// cachedClient requires mu. The cache records discovery, not the execution lane.
func (c *Client) cachedClient(runner host.Runner) (board.MessageClient, bool) {
	if c.absent {
		return nil, true
	}
	if c.resolvedPath == nil {
		return nil, false
	}
	return NewRemoteAgmsgClient(c.id, *c.resolvedPath, runner), true
}

// Probes run outside mu so a send waiting for SSH cannot block polling.
// Concurrent successful discoveries publish one cache entry and one warning.
func (c *Client) finishProbe(
	runner host.Runner, path string, present bool, version string, versionErr error,
) board.MessageClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	if client, known := c.cachedClient(runner); known {
		return client
	}
	if !present {
		c.absent = true
		log.Printf("Warning: agent board: no agmsg installation at %q on host %q, skipping that host", path, c.id)
		return nil
	}
	if versionErr != nil {
		log.Printf("Warning: agent board: reading agmsg version on host %q: %v", c.id, versionErr)
	} else if warning := VersionMismatchWarning(c.id, version); warning != "" {
		log.Printf("Warning: %s", warning)
	}
	c.resolvedPath = &path
	return NewRemoteAgmsgClient(c.id, path, runner)
}

func (c *Client) Since(ctx context.Context, team, after string, limit int) ([]board.Row, error) {
	client, err := c.ready(ctx, c.runner)
	if err != nil || client == nil {
		return nil, err
	}
	rows, err := client.Since(ctx, team, after, limit)
	if err != nil {
		return nil, fmt.Errorf("read host messages: %w", err)
	}
	return rows, nil
}
func (c *Client) Send(ctx context.Context, team, from, to, body string) error {
	runner := c.sendRunner
	if runner == nil {
		runner = c.runner
	}
	client, err := c.ready(ctx, runner)
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

// participationRoot keeps bootstrap path discovery separate from relay absence
// caching: a missing install is re-probed on every eligible bootstrap tick.
func (c *Client) participationRoot(ctx context.Context) (string, error) {
	c.participationMu.Lock()
	defer c.participationMu.Unlock()
	if c.participationPath != "" {
		return c.participationPath, nil
	}
	path, err := resolveHostAgmsgPath(ctx, c.runner, c.path)
	if err != nil {
		return "", err
	}
	c.participationPath = path
	return path, nil
}
func (c *Client) Description() board.ParticipationDescription {
	return board.ParticipationDescription{Name: "agmsg", PathSetting: "agmsg installation path"}
}
func (c *Client) ValidateAddress(team, paneID string) error {
	if err := validateIdentifiers(team, paneID, paneID); err != nil {
		return fmt.Errorf("pane %q or team %q is not a valid agmsg identifier", paneID, team)
	}
	return nil
}
func (c *Client) Available(ctx context.Context) (bool, error) {
	path, err := c.participationRoot(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: %w", board.ErrParticipationPath, err)
	}
	return RemoteAgmsgPresent(ctx, c.runner, path)
}
func (c *Client) JoinInstruction(ctx context.Context, request board.JoinRequest) (string, error) {
	if err := c.ValidateAddress(request.Team, request.PaneID); err != nil {
		return "", err
	}
	var typeName string
	for _, entry := range boardAdapters() {
		if entry.id == request.Agent {
			typeName = entry.agmsgType
			break
		}
	}
	if typeName == "" {
		return "", fmt.Errorf("agmsg: no board adapter for agent %q", request.Agent)
	}
	path, err := c.participationRoot(ctx)
	if err != nil {
		return "", err
	}
	return buildBootstrapInstruction(path, request.Team, request.PaneID, typeName, request.Mode), nil
}
