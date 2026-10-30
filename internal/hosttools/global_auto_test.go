package hosttools

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"panemux/internal/host"
)

type globalAutoTool struct {
	fail     error
	states   map[host.Host]State
	installs []host.Host
	mu       sync.Mutex
}

func (f *globalAutoTool) Check(_ context.Context, h host.Host) State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[h]
}
func (f *globalAutoTool) Install(_ context.Context, h host.Host) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs = append(f.installs, h)
	if f.fail != nil {
		return State{}, f.fail
	}
	f.states[h] = State{Status: Installed, Version: "1.2.0"}
	return f.states[h], nil
}

func TestGlobalAutoChecksAllHostsAndOnlyUpdatesOutdated(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[on], func(t *testing.T) {
			tool := &globalAutoTool{states: map[host.Host]State{host.Local: {Status: Outdated}}}
			hosts := []string{}
			for _, status := range []Status{Outdated, Missing, Installed, Newer, Unknown, Invalid} {
				hosts = append(hosts, string(status))
				tool.states[host.Host(status)] = State{Status: status}
			}
			s := New(Options{
				Hosts:      func() []string { return hosts },
				Tools:      map[ID]Tool{AgmsgTool: tool},
				GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return on }}})
			defer s.Close()
			s.StartAutoUpdates(AgmsgTool)
			s.Wait()
			if on {
				require.ElementsMatch(t, []host.Host{host.Local, "outdated"}, tool.installs)
			} else {
				require.Empty(t, tool.installs)
			}
			got, err := s.Check(context.Background(), AgmsgTool, host.Local)
			require.NoError(t, err)
			require.Equal(t, on, got.AutoUpdate)
			require.Equal(t, on, got.AutoUpdated)
			s.StartAutoUpdates(AgmsgTool)
			s.Wait()
			if on {
				require.Len(t, tool.installs, 2)
			}
		})
	}
}

func TestGlobalAutoFailureIsRetainedUntilManualRetry(t *testing.T) {
	tool := &globalAutoTool{states: map[host.Host]State{host.Local: {Status: Outdated}},
		fail: errors.New("installer exit status 7: recover required")}
	on := false
	s := New(Options{
		Tools:      map[ID]Tool{AgmsgTool: tool},
		GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return on }}})
	defer s.Close()
	s.StartAutoUpdates(AgmsgTool)
	s.Wait()
	require.Empty(t, tool.installs)
	on = true
	s.StartAutoUpdates(AgmsgTool)
	s.Wait()
	for range 2 {
		got, err := s.Check(context.Background(), AgmsgTool, host.Local)
		require.NoError(t, err)
		require.Contains(t, got.AutoError, "exit status 7")
	}
	require.Len(t, tool.installs, 1)
	tool.fail = nil
	got, err := s.Install(context.Background(), AgmsgTool, host.Local)
	require.NoError(t, err)
	require.Empty(t, got.AutoError)
	require.False(t, got.AutoUpdated)
	require.True(t, got.AutoUpdate)
}

type changingAutoTool struct {
	next   Status
	checks int
}

func (f *changingAutoTool) Check(context.Context, host.Host) State {
	f.checks++
	if f.checks == 1 {
		return State{Status: Outdated}
	}
	return State{Status: f.next}
}
func (*changingAutoTool) Install(context.Context, host.Host) (State, error) {
	panic("ineligible tool must not be installed")
}

func TestGlobalAutoRechecksEligibilityInsideReservation(t *testing.T) {
	for _, status := range []Status{Missing, Newer, Unknown, Invalid, Installed} {
		t.Run(string(status), func(t *testing.T) {
			tool := &changingAutoTool{next: status}
			s := New(Options{
				Tools:      map[ID]Tool{AgmsgTool: tool},
				GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return true }}})
			defer s.Close()
			s.StartAutoUpdates(AgmsgTool)
			s.Wait()
			require.Equal(t, 2, tool.checks)
			require.Zero(t, s.Revision(AgmsgTool, host.Local))
		})
	}
}

func TestGlobalAutoPrerequisitesAndCancellation(t *testing.T) {
	state := State{Status: Outdated}
	tool := &prerequisiteTool{fakeTool: fakeTool{state: &state},
		checks: []Prerequisite{{Name: "node", Check: func(context.Context) error {
			return errors.New("install node on this host")
		}}},
	}
	s := New(Options{
		Tools:      map[ID]Tool{AgmsgTool: tool},
		GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return true }}})
	defer s.Close()
	s.StartAutoUpdates(AgmsgTool)
	s.Wait()
	require.Empty(t, tool.installed)
	got, err := s.Check(context.Background(), AgmsgTool, host.Local)
	require.NoError(t, err)
	require.Contains(t, got.AutoError, "install node")
	release, err := s.BeginCall(AgmsgTool, host.Local)
	require.NoError(t, err)
	release()

	blocked := blockingTool{started: make(chan struct{})}
	c := New(Options{
		Tools:      map[ID]Tool{AgmsgTool: blocked},
		GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return true }}})
	c.StartAutoUpdates(AgmsgTool)
	<-blocked.started
	c.Close()
	c.StartAutoUpdates(AgmsgTool) // closed services start no more work
	require.Contains(t, c.globalAuto[callKey{AgmsgTool, host.Local}].err, "canceled")
}

func TestGlobalAutoUsesDrainAndRejectsOverlappingManualUpdate(t *testing.T) {
	tool := &globalAutoTool{states: map[host.Host]State{host.Local: {Status: Outdated}}}
	s := New(Options{
		Tools:      map[ID]Tool{AgmsgTool: tool},
		GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return true }}})
	defer s.Close()
	release, err := s.BeginCall(AgmsgTool, host.Local)
	require.NoError(t, err)
	defer release()
	s.StartAutoUpdates(AgmsgTool)
	require.Eventually(t, func() bool {
		done, beginErr := s.BeginCall(AgmsgTool, host.Local)
		if beginErr != nil {
			return errors.Is(beginErr, ErrUpdating)
		}
		done()
		return false
	}, time.Second, time.Millisecond)
	_, err = s.Install(context.Background(), AgmsgTool, host.Local)
	require.ErrorIs(t, err, ErrUpdating)
	got, err := s.Check(context.Background(), AgmsgTool, host.Local)
	require.NoError(t, err)
	require.Equal(t, Unknown, got.Status)
	tool.mu.Lock()
	installed := len(tool.installs)
	tool.mu.Unlock()
	require.Zero(t, installed)
	release()
	s.Wait()
	require.Len(t, tool.installs, 1)
	require.Equal(t, uint64(1), s.Revision(AgmsgTool, host.Local))
}

func TestGlobalAutoTurningOffSkipsPendingUpdate(t *testing.T) {
	tool := &globalAutoTool{states: map[host.Host]State{host.Local: {Status: Outdated}}}
	var on atomic.Bool
	on.Store(true)
	s := New(Options{Tools: map[ID]Tool{AgmsgTool: tool}, GlobalAuto: map[ID]func() bool{AgmsgTool: on.Load}})
	defer s.Close()
	release, err := s.BeginCall(AgmsgTool, host.Local)
	require.NoError(t, err)
	defer release()
	s.StartAutoUpdates(AgmsgTool)
	require.Eventually(t, func() bool {
		done, beginErr := s.BeginCall(AgmsgTool, host.Local)
		if beginErr != nil {
			return errors.Is(beginErr, ErrUpdating)
		}
		done()
		return false
	}, time.Second, time.Millisecond)
	on.Store(false)
	release()
	s.Wait()
	require.Empty(t, tool.installs)
	require.Zero(t, s.Revision(AgmsgTool, host.Local))
	got, err := s.Check(context.Background(), AgmsgTool, host.Local)
	require.NoError(t, err)
	require.False(t, got.AutoUpdate)
	require.False(t, got.AutoUpdated)
}

func TestGlobalAutoManualRetrySupersedesOlderCompletion(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fails], func(t *testing.T) {
			tool := &globalAutoTool{states: map[host.Host]State{host.Local: {Status: Installed}}}
			s := New(Options{
				Tools:      map[ID]Tool{AgmsgTool: tool},
				GlobalAuto: map[ID]func() bool{AgmsgTool: func() bool { return true }},
			})
			defer s.Close()
			s.StartAutoUpdates(AgmsgTool)
			s.Wait()
			if fails {
				tool.fail = errors.New("manual retry failed")
			}
			_, err := s.Install(context.Background(), AgmsgTool, host.Local)
			require.Equal(t, fails, err != nil)
			// Deliver the earlier background outcome after the manual result. This
			// orders completions without depending on goroutine scheduling.
			s.finishGlobalAuto(AgmsgTool, host.Local, 0, globalAutoResult{err: "old automatic failure"})
			got, err := s.Check(context.Background(), AgmsgTool, host.Local)
			require.NoError(t, err)
			if fails {
				require.Equal(t, "old automatic failure", got.AutoError)
			} else {
				require.Empty(t, got.AutoError)
			}
		})
	}
}
