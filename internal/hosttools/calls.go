package hosttools

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"panemux/internal/host"
)

var ErrUpdating = errors.New("tool is updating")

type callKey struct {
	id   ID
	host host.Host
}

type callSlot struct {
	idle     chan struct{}
	active   int
	revision uint64
	updating bool
}

// CallGate coordinates calls and updates per tool and host. Its zero value is
// ready for use. A lease must cover an entire operation, including probes; a
// refused operation must not advance a relay cursor. No agent-owned calls can
// be stopped by this gate.
type CallGate struct {
	slots map[callKey]*callSlot
	mu    sync.Mutex
}

func (g *CallGate) slot(key callKey) *callSlot {
	if g.slots == nil {
		g.slots = map[callKey]*callSlot{}
	}
	if g.slots[key] == nil {
		g.slots[key] = &callSlot{}
	}
	return g.slots[key]
}

// Begin refuses promptly once an update has been announced. The returned
// release is idempotent and must be called after the operation finishes.
func (g *CallGate) Begin(id ID, h host.Host) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.slot(callKey{id, h})
	if s.updating {
		return nil, ErrUpdating
	}
	if s.active == 0 {
		s.idle = make(chan struct{})
	}
	s.active++
	return sync.OnceFunc(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		s.active--
		if s.active == 0 {
			close(s.idle)
		}
	}), nil
}

// Pause announces the update before waiting for existing calls. Cancellation
// while draining lifts the pause. On success the caller must defer resume,
// including when prerequisites, installation or its timeout fail.
func (g *CallGate) Pause(ctx context.Context, id ID, h host.Host) (func(), error) {
	g.mu.Lock()
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return nil, fmt.Errorf("pause tool calls: %w", err)
	}
	s := g.slot(callKey{id, h})
	if s.updating {
		g.mu.Unlock()
		return nil, ErrUpdating
	}
	s.updating = true
	active, idle := s.active, s.idle
	g.mu.Unlock()
	resume := sync.OnceFunc(func() { g.mu.Lock(); s.updating = false; g.mu.Unlock() })
	if active > 0 {
		select {
		case <-idle:
		case <-ctx.Done():
			resume()
			return nil, fmt.Errorf("drain tool calls: %w", ctx.Err())
		}
	}
	return resume, nil
}

// Updating is a notification snapshot, not admission authority. Use Begin to
// avoid racing an update; use this method to display or skip paused work.
func (g *CallGate) Updating(id ID, h host.Host) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.slots[callKey{id, h}]
	return s != nil && s.updating
}

// BeginCall admits one consumer operation on a configured host/tool pair.
func (s *Service) BeginCall(id ID, h host.Host) (func(), error) {
	if _, err := s.tool(id, h); err != nil {
		return nil, err
	}
	return s.calls.Begin(id, h)
}

func (s *Service) Updating(id ID, h host.Host) bool { return s.calls.Updating(id, h) }

// Revision changes after installer attempts, including partial failures.
func (s *Service) Revision(id ID, h host.Host) uint64 {
	s.calls.mu.Lock()
	defer s.calls.mu.Unlock()
	return s.calls.slot(callKey{id, h}).revision
}

func (s *Service) invalidate(id ID, h host.Host) {
	s.calls.mu.Lock()
	defer s.calls.mu.Unlock()
	s.calls.slot(callKey{id, h}).revision++
}

// Prerequisite is an adapter-defined check: it can probe the target, the
// panemux host, or verified local cache. Checks must not install dependencies.
type Prerequisite struct {
	Check func(context.Context) error
	Name  string
}

// PrerequisiteTool optionally supplies checks before each manual or automatic
// installation. Existing tools without this port retain their own checks.
type PrerequisiteTool interface {
	Prerequisites(host.Host) []Prerequisite
}

// CheckPrerequisites reports all failed checks so the operator can set up the
// prerequisites together. Cancellation stops further checks and installation.
func CheckPrerequisites(ctx context.Context, checks []Prerequisite) error {
	var failures []error
	for _, check := range checks {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if err := check.Check(ctx); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", check.Name, err))
		}
	}
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return fmt.Errorf("set up tool prerequisites: %w", errors.Join(failures...))
	}
	return nil
}
