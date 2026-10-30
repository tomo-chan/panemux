package hosttools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"panemux/internal/host"
)

func TestCallGateDrainsAndRefusesNewCalls(t *testing.T) {
	var gate CallGate
	first, err := gate.Begin("messages", "build-box")
	require.NoError(t, err)
	second, err := gate.Begin("messages", "build-box")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	paused := make(chan func(), 1)
	go func() {
		resume, pauseErr := gate.Pause(ctx, "messages", "build-box")
		if pauseErr == nil {
			paused <- resume
		}
	}()
	require.Eventually(t, func() bool { return gate.Updating("messages", "build-box") }, time.Second, time.Millisecond)
	_, err = gate.Begin("messages", "build-box")
	require.ErrorIs(t, err, ErrUpdating)
	_, err = gate.Pause(ctx, "messages", "build-box")
	require.ErrorIs(t, err, ErrUpdating)
	for _, key := range []struct {
		id ID
		h  host.Host
	}{{"other", "build-box"}, {"messages", host.Local}} {
		release, beginErr := gate.Begin(key.id, key.h)
		require.NoError(t, beginErr)
		release()
	}
	first()
	first() // Release must be safe to defer as well as call explicitly.
	select {
	case <-paused:
		t.Fatal("installer admitted with a call still active")
	default:
	}
	second()
	var resume func()
	select {
	case resume = <-paused:
	case <-ctx.Done():
		t.Fatal("drain did not finish")
	}
	require.True(t, gate.Updating("messages", "build-box"))
	resume()
	resume()
	require.False(t, gate.Updating("messages", "build-box"))
	release, err := gate.Begin("messages", "build-box")
	require.NoError(t, err)
	release()
}

func TestCallGateCanceledDrainResumesCalls(t *testing.T) {
	for _, active := range []bool{false, true} {
		var gate CallGate
		if active {
			release, err := gate.Begin("tool", host.Local)
			require.NoError(t, err)
			defer release()
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := gate.Pause(ctx, "tool", host.Local)
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, gate.Updating("tool", host.Local))
	}
	var gate CallGate
	release, err := gate.Begin("tool", host.Local)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = gate.Pause(ctx, "tool", host.Local)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, gate.Updating("tool", host.Local))
	next, err := gate.Begin("tool", host.Local)
	require.NoError(t, err)
	next()
	release()
	resume, err := gate.Pause(callTestContext(t), "tool", host.Local)
	require.NoError(t, err)
	resume()
}

type prerequisiteTool struct {
	install func(context.Context) error
	checks  []Prerequisite
	fakeTool
}

func TestToolRevisionIncreasesAfterSuccessfulOrPartialInstall(t *testing.T) {
	tool := &fakeTool{}
	s := New(Options{Tools: map[ID]Tool{AgmsgTool: tool}})
	defer s.Close()
	require.Zero(t, s.Revision(AgmsgTool, host.Local))
	_, err := s.Install(callTestContext(t), AgmsgTool, host.Local)
	require.NoError(t, err)
	require.Equal(t, uint64(1), s.Revision(AgmsgTool, host.Local))
	tool.fail = errors.New("partial install")
	_, err = s.Install(callTestContext(t), AgmsgTool, host.Local)
	require.ErrorIs(t, err, tool.fail)
	require.Equal(t, uint64(2), s.Revision(AgmsgTool, host.Local))
	require.Zero(t, s.Revision(CodexHookTool, host.Local))
}

func (p *prerequisiteTool) Prerequisites(host.Host) []Prerequisite { return p.checks }
func (p *prerequisiteTool) Install(ctx context.Context, h host.Host) (State, error) {
	if p.install != nil {
		return State{Status: Installed}, p.install(ctx)
	}
	return p.fakeTool.Install(ctx, h)
}

func TestServicePrerequisitesPreventInstallAndReportEveryFailure(t *testing.T) {
	missing := errors.New("not found")
	var checked []string
	tool := &prerequisiteTool{checks: []Prerequisite{
		{Name: "bash", Check: func(context.Context) error { checked = append(checked, "bash"); return missing }},
		{Name: "node", Check: func(context.Context) error { checked = append(checked, "node"); return nil }},
		{Name: "source", Check: func(context.Context) error {
			checked = append(checked, "source")
			return errors.New("offline")
		}},
	}}
	s := New(Options{Tools: map[ID]Tool{"tool": tool}})
	defer s.Close()
	_, err := s.Install(callTestContext(t), "tool", host.Local)
	require.ErrorIs(t, err, missing)
	require.ErrorContains(t, err, "bash: not found")
	require.ErrorContains(t, err, "source: offline")
	require.Equal(t, []string{"bash", "node", "source"}, checked)
	require.Empty(t, tool.installed)
	tool.checks = nil
	_, err = s.Install(callTestContext(t), "tool", host.Local)
	require.NoError(t, err)
	require.Equal(t, []host.Host{host.Local}, tool.installed)
}

func TestServiceInstallDrainsAndAlwaysResumes(t *testing.T) {
	for _, failure := range []string{"success", "installer", "timeout", "prerequisite"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			started, finish := make(chan struct{}), make(chan struct{})
			var installs atomic.Int32
			tool := updateTestTool(failure, cancel, started, finish, &installs)
			s := New(Options{Tools: map[ID]Tool{"tool": tool}})
			defer s.Close()
			release, err := s.BeginCall("tool", host.Local)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { _, installErr := s.Install(ctx, "tool", host.Local); done <- installErr }()
			require.Eventually(t, func() bool { return s.Updating("tool", host.Local) }, time.Second, time.Millisecond)
			select {
			case <-started:
				t.Fatal("prerequisite/installer started before drain")
			default:
			}
			paused, checkErr := s.Check(context.Background(), "tool", host.Local)
			require.NoError(t, checkErr)
			require.Equal(t, State{Status: Unknown, Error: ErrUpdating.Error()}, paused)
			release()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("update did not start")
			}
			if failure != "timeout" {
				_, err = s.Install(ctx, "tool", host.Local)
				require.ErrorIs(t, err, ErrUpdating)
			}
			close(finish)
			err = <-done
			if failure == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.False(t, s.Updating("tool", host.Local))
			_, err = s.Check(context.Background(), "tool", host.Local)
			require.NoError(t, err)
			want := int32(1)
			if failure == "prerequisite" {
				want = 0
			}
			require.Equal(t, want, installs.Load())
		})
	}
}

func TestPrerequisitesCancellationStopsRemainingChecks(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		ctx, cancel := context.WithCancel(context.Background())
		checks := []Prerequisite{
			{Name: "first", Check: func(context.Context) error { cancel(); return context.Canceled }},
			{Name: "second", Check: func(context.Context) error { t.Fatal("check after cancellation"); return nil }},
		}
		if count == 0 {
			cancel()
		}
		err := CheckPrerequisites(ctx, checks[:count])
		cancel()
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestServiceCallAdmissionValidatesIdentityAndPausesAutomaticCalls(t *testing.T) {
	s := New(Options{Tools: map[ID]Tool{CodexHookTool: &fakeTool{}}})
	defer s.Close()
	_, err := s.BeginCall(CodexHookTool, "unknown")
	require.ErrorIs(t, err, ErrUnknownHost)
	_, err = s.BeginCall("unknown", host.Local)
	require.ErrorIs(t, err, ErrUnknownTool)
	resume, err := s.calls.Pause(callTestContext(t), CodexHookTool, host.Local)
	require.NoError(t, err)
	got := s.checkCodexHook(context.Background(), "")
	require.Equal(t, Unknown, got.Status)
	require.Equal(t, ErrUpdating.Error(), got.Error)
	_, err = s.installCodexHook(callTestContext(t), "")
	require.ErrorIs(t, err, ErrUpdating)
	resume()
	got, err = s.installCodexHook(callTestContext(t), "")
	require.NoError(t, err)
	require.Equal(t, Installed, got.Status)
}

func updateTestTool(
	failure string, cancel context.CancelFunc, started, finish chan struct{}, installs *atomic.Int32,
) *prerequisiteTool {
	tool := &prerequisiteTool{install: func(ctx context.Context) error {
		installs.Add(1)
		close(started)
		if failure == "timeout" {
			cancel()
			<-ctx.Done()
			return ctx.Err()
		}
		<-finish
		if failure == "installer" {
			return errors.New("installer failed")
		}
		return nil
	}}
	if failure == "prerequisite" {
		tool.checks = []Prerequisite{{Name: "tar", Check: func(context.Context) error {
			close(started)
			<-finish
			return errors.New("missing")
		}}}
	}
	return tool
}

func callTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}
