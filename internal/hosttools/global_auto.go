package hosttools

import (
	"context"
	"time"

	"panemux/internal/host"
)

type globalAutoResult struct {
	err              string
	revision         uint64
	running, updated bool
}

func (s *Service) globalAutoEnabled(id ID) bool {
	enabled := s.opts.GlobalAuto[id]
	return enabled != nil && enabled()
}

// StartAutoUpdates checks every configured host at startup or on opt-in.
// Failures remain available for manual retry; there is no periodic retry.
// It never installs a missing tool. Close cancels and joins all checks/updates.
func (s *Service) StartAutoUpdates(id ID) {
	if !s.globalAutoEnabled(id) || s.opts.Tools[id] == nil {
		return
	}
	hosts := []host.Host{host.Local}
	if s.opts.Hosts != nil {
		for _, name := range s.opts.Hosts() {
			hosts = append(hosts, host.Host(name))
		}
	}
	for _, h := range hosts {
		key := callKey{id, h}
		s.mu.Lock()
		if s.globalAuto[key].running || s.closed {
			s.mu.Unlock()
			continue
		}
		previous := s.globalAuto[key]
		previous.running = true
		s.globalAuto[key] = previous
		s.runs.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.runs.Done()
			s.runGlobalAuto(id, h)
		}()
	}
}

func (s *Service) runGlobalAuto(id ID, h host.Host) {
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Minute)
	defer cancel()
	s.mu.Lock()
	revision := s.globalAuto[callKey{id, h}].revision
	s.mu.Unlock()
	tool, err := s.tool(id, h)
	var got State
	attempted := false
	if err == nil {
		got, err = s.check(ctx, id, h, tool)
		if err == nil && got.Status == Outdated && s.globalAutoEnabled(id) {
			// Recheck under the same reservation used by manual updates. A tool
			// removed since discovery must never turn an automatic Update into Install.
			got, err = s.installWhen(ctx, id, h, tool, func(state State) bool {
				attempted = state.Status == Outdated && s.globalAutoEnabled(id)
				return attempted
			})
		}
	}
	result := globalAutoResult{updated: attempted && err == nil && got.Status == Installed}
	if err != nil {
		result.err = err.Error()
	} else if got.Status == Unknown || got.Status == Invalid {
		result.err = got.Error
	}

	if s.finishGlobalAuto(id, h, revision, result) && result.err != "" {
		s.opts.Logf("%s: automatic update on %s failed: %s", id, hostName(string(h)), result.err)
	}
}

func (s *Service) finishGlobalAuto(id ID, h host.Host, revision uint64, result globalAutoResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.globalAuto[callKey{id, h}]
	if current.revision != revision {
		// A successful manual retry supersedes this background result.
		current.running = false
		s.globalAuto[callKey{id, h}] = current
		return false
	}
	result.revision = revision
	s.globalAuto[callKey{id, h}] = result
	return true
}

func (s *Service) withGlobalAuto(id ID, h host.Host, got State) State {
	if s.opts.GlobalAuto[id] == nil {
		return got
	}
	got.AutoUpdate = s.globalAutoEnabled(id)
	s.mu.Lock()
	auto := s.globalAuto[callKey{id, h}]
	s.mu.Unlock()
	got.AutoUpdated, got.AutoError = auto.updated, auto.err
	if auto.running {
		got.Status = Unknown
		got.Error = "Checking or updating automatically; reopen the menu for the result."
	}
	return got
}
