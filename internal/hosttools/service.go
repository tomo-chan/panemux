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
const AgmsgTool ID = "agmsg"

type Status string

const (
	Absent    Status = "absent"
	Missing   Status = "missing"
	Installed Status = "installed"
	Outdated  Status = "outdated"
	Invalid   Status = "invalid"
	Unknown   Status = "unknown"
	Newer     Status = "newer"
)

// State preserves the host menu's status and automatic-update outcomes.
type State struct {
	Version           string `json:"version,omitempty"`
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

// UpdateTool preserves update-only intent through an adapter's final probe.
// Global automatic updates never fall back to Install.
type UpdateTool interface {
	Update(context.Context, host.Host) (State, error)
}

var ErrUnknownHost = hostexec.ErrUnknownHost
var ErrUnknownTool = errors.New("unknown tool")

type Options struct {
	Hosts         func() []string
	Tools         map[ID]Tool
	CodexHookAuto *CodexHookAutoStore
	// GlobalAuto reads instance-wide preferences; saving belongs to composition.
	GlobalAuto map[ID]func() bool
	Logf       func(string, ...any)
}

type Service struct {
	opts          Options
	globalAuto    map[callKey]globalAutoResult
	codexHookAuto map[string]codexHookAuto
	ctx           context.Context
	cancel        context.CancelFunc
	calls         CallGate
	mu            sync.Mutex
	runs          sync.WaitGroup
	closed        bool
}

func New(opts Options) *Service {
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{opts: opts, ctx: ctx, cancel: cancel,
		codexHookAuto: map[string]codexHookAuto{}, globalAuto: map[callKey]globalAutoResult{}}
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
	got, err := s.check(ctx, id, h, tool)
	if err != nil {
		got = State{Status: Unknown, Error: err.Error()}
	}
	if id == CodexHookTool {
		got = s.autoUpdateCodexHook(ctx, string(h), got)
	}
	return s.withGlobalAuto(id, h, got), nil
}

func (s *Service) Install(ctx context.Context, id ID, h host.Host) (State, error) {
	tool, err := s.tool(id, h)
	if err != nil {
		return State{}, err
	}
	got, err := s.install(ctx, id, h, tool)
	if err != nil {
		return State{}, err //nolint:wrapcheck // preserve adapter errors
	}
	if id == CodexHookTool {
		s.forgetCodexHookAuto(string(h))
		got = s.withCodexHookAuto(string(h), got)
	}
	s.mu.Lock()
	if auto, ok := s.globalAuto[callKey{id, h}]; ok {
		auto.err, auto.updated = "", false
		//mutation:exempt[INCREMENT_DECREMENT] equivalent: generation equality ignores the direction of change
		auto.revision++
		s.globalAuto[callKey{id, h}] = auto
	}
	s.mu.Unlock()
	return s.withGlobalAuto(id, h, got), nil
}

func (s *Service) checkCodexHook(ctx context.Context, name string) State {
	got, err := s.check(ctx, CodexHookTool, host.Host(name), s.opts.Tools[CodexHookTool])
	if err != nil {
		return State{Status: Unknown, Error: err.Error()}
	}
	return got
}
func (s *Service) installCodexHook(ctx context.Context, name string) (State, error) {
	return s.install(ctx, CodexHookTool, host.Host(name), s.opts.Tools[CodexHookTool])
}

func (s *Service) check(ctx context.Context, id ID, h host.Host, tool Tool) (State, error) {
	release, err := s.calls.Begin(id, h)
	if err != nil {
		return State{}, err
	}
	defer release()
	return tool.Check(ctx, h), nil
}

func (s *Service) install(ctx context.Context, id ID, h host.Host, tool Tool) (State, error) {
	return s.installWhen(ctx, id, h, tool, nil)
}

func (s *Service) installWhen(
	ctx context.Context, id ID, h host.Host, tool Tool, eligible func(State) bool,
) (State, error) {
	resume, err := s.calls.Pause(ctx, id, h)
	if err != nil {
		return State{}, err
	}
	defer resume()
	var checked State
	if eligible != nil {
		checked = tool.Check(ctx, h)
		if !eligible(checked) {
			return checked, nil
		}
	}
	install := tool.Install
	if eligible != nil {
		updater, ok := tool.(UpdateTool)
		if !ok {
			return State{}, errors.New("tool does not support update without installation")
		}
		install = updater.Update
	}
	var checks []Prerequisite
	if p, ok := tool.(PrerequisiteTool); ok {
		checks = p.Prerequisites(h)
	}
	if err := CheckPrerequisites(ctx, checks); err != nil {
		return State{}, err
	}
	// Acquisition may have waited for another host's source preparation.
	// Honor opt-out again before admitting the installer, using the same
	// checked state; UpdateTool performs its own final host probe.
	if eligible != nil && !eligible(checked) {
		return checked, nil
	}
	defer s.invalidate(id, h)
	return install(ctx, h) //nolint:wrapcheck // preserve adapter errors
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
