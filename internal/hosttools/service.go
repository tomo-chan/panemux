// Package hosttools manages tools on the configured hosts without owning their connections.
package hosttools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"panemux/internal/host"
	"panemux/internal/hostexec"
)

type ID string

const CodexHookTool ID = "codex-hook"

type Status string

const (
	Absent    Status = "absent"
	Missing   Status = "missing"
	Installed Status = "installed"
	Outdated  Status = "outdated"
	Invalid   Status = "invalid"
	Unknown   Status = "unknown"
)

// State preserves the host menu's status and automatic-update outcomes.
type State struct {
	Status            Status `json:"status"`
	Error             string `json:"error,omitempty"`
	AutoError         string `json:"auto_error,omitempty"`
	AutoSettingsError string `json:"auto_settings_error,omitempty"`
	AutoUpdate        bool   `json:"auto_update"`
	AutoUpdated       bool   `json:"auto_updated,omitempty"`
}

// Tool is implemented by an adapter using the shared host execution port.
// Check represents an unreachable host as Unknown; Install returns write failures.
type Tool interface {
	Check(context.Context, host.Host) State
	Install(context.Context, host.Host) (State, error)
}

var ErrUnknownHost = hostexec.ErrUnknownHost
var ErrUnknownTool = errors.New("unknown tool")

type Options struct {
	Hosts         func() []string
	Tools         map[ID]Tool
	CodexHookAuto *CodexHookAutoStore
	Logf          func(string, ...any)
}

type Service struct {
	opts          Options
	codexHookAuto map[string]codexHookAuto
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	runs          sync.WaitGroup
	closed        bool
}

func New(opts Options) *Service {
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{opts: opts, ctx: ctx, cancel: cancel, codexHookAuto: map[string]codexHookAuto{}}
}

func (s *Service) checkHost(h host.Host) error {
	if h == host.Local {
		return nil
	}
	if s.opts.Hosts != nil {
		for _, name := range s.opts.Hosts() {
			if name == string(h) {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %q", ErrUnknownHost, h)
}

func (s *Service) tool(id ID, h host.Host) (Tool, error) {
	if err := s.checkHost(h); err != nil {
		return nil, err
	}
	tool := s.opts.Tools[id]
	if tool == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, id)
	}
	return tool, nil
}

func (s *Service) Check(ctx context.Context, id ID, h host.Host) (State, error) {
	tool, err := s.tool(id, h)
	if err != nil {
		return State{}, err
	}
	got := tool.Check(ctx, h)
	if id == CodexHookTool {
		got = s.autoUpdateCodexHook(ctx, string(h), got)
	}
	return got, nil
}

func (s *Service) Install(ctx context.Context, id ID, h host.Host) (State, error) {
	tool, err := s.tool(id, h)
	if err != nil {
		return State{}, err
	}
	got, err := tool.Install(ctx, h)
	if err != nil {
		return State{}, err //nolint:wrapcheck // preserve adapter errors
	}
	if id == CodexHookTool {
		s.forgetCodexHookAuto(string(h))
		got = s.withCodexHookAuto(string(h), got)
	}
	return got, nil
}

func (s *Service) checkCodexHook(ctx context.Context, name string) State {
	return s.opts.Tools[CodexHookTool].Check(ctx, host.Host(name))
}
func (s *Service) installCodexHook(ctx context.Context, name string) (State, error) {
	return s.opts.Tools[CodexHookTool].Install(ctx, host.Host(name)) //nolint:wrapcheck // preserve adapter errors
}
func hostName(name string) string {
	if name == "" {
		return "the panemux host"
	}
	return name
}

// AfterCollection receives a successful task collection; tool checks stay here.
func (s *Service) AfterCollection(h host.Host) {
	if s.opts.Tools[CodexHookTool] != nil {
		s.checkCodexHookAfterStart(string(h))
	}
}

// Wait waits for checks already started by collection notifications.
func (s *Service) Wait() { s.runs.Wait() }

// Close cancels and joins background checks, leaving the shared executor open.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.runs.Wait()
}
