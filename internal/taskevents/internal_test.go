package taskevents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"panemux/internal/tasks"
)

func TestInternalSubscriberKeepsObservationWithoutBrowser(t *testing.T) {
	src := newFakeSource()
	p := newTestPublisher(t, src)
	observed := make(chan []tasks.Task, 8)
	cancel := p.SubscribeInternal(func(_ string, _ tasks.HostResult, ts []tasks.Task) { observed <- ts })
	task := tasks.Task{ID: "local:claude:s", Agent: "claude", SessionID: "s",
		State: tasks.StateBusy, LivePIDs: []int{12, 13}}
	src.answer(t, "", tasks.HostResult{Status: tasks.HostOK}, task)
	require.Equal(t, []int{12, 13}, receiveInternal(t, observed)[0].LivePIDs)
	_, frames, browserCancel := p.Subscribe()
	browserCancel()
	require.Empty(t, frames)
	src.answer(t, "", tasks.HostResult{Status: tasks.HostOK}, task)
	require.Len(t, receiveInternal(t, observed), 1, "unchanged tasks must still reach internal consumers")
	cancel()
	cancel()
	p.mu.Lock()
	observing := p.observing
	p.mu.Unlock()
	require.False(t, observing)
}

func TestInternalSubscriberSurvivesSlowBrowserAndCloses(t *testing.T) {
	src := newFakeSource()
	p := New(src, Options{Interval: time.Millisecond, Buffer: 1})
	defer p.Close()
	calls := make(chan tasks.HostStatus, 8)
	cancel := p.SubscribeInternal(func(_ string, r tasks.HostResult, _ []tasks.Task) { calls <- r.Status })
	_, frames, _ := p.Subscribe()
	for _, status := range []tasks.HostStatus{tasks.HostOK, tasks.HostError, tasks.HostOK} {
		src.answer(t, "", tasks.HostResult{Status: status})
		require.Equal(t, status, receiveInternal(t, calls))
	}
	require.Equal(t, FrameHost, nextFrame(t, frames).Type)
	require.Eventually(t, func() bool {
		select {
		case _, open := <-frames:
			return !open
		default:
			return false
		}
	}, 2*time.Second, time.Millisecond, "slow browser closes while internal observation continues")
	p.Close()
	cancel()
	called := false
	p.SubscribeInternal(func(string, tasks.HostResult, []tasks.Task) { called = true })()
	require.False(t, called)
}

func TestInternalProcessSetsStayOutOfTaskJSON(t *testing.T) {
	b, err := json.Marshal(tasks.Task{LivePIDs: []int{123, 456}})
	require.NoError(t, err)
	require.NotContains(t, string(b), "123")
}

func TestInternalSubscriptionsShareAndReleaseObservation(t *testing.T) {
	src := newFakeSource()
	p := newTestPublisher(t, src)
	first := make(chan struct{}, 4)
	second := make(chan struct{}, 4)
	cancelFirst := p.SubscribeInternal(func(string, tasks.HostResult, []tasks.Task) { first <- struct{}{} })
	cancelSecond := p.SubscribeInternal(func(string, tasks.HostResult, []tasks.Task) { second <- struct{}{} })
	_, frames, cancelBrowser := p.Subscribe()
	src.answer(t, "", tasks.HostResult{Status: tasks.HostOK})
	receiveInternal(t, first)
	receiveInternal(t, second)
	nextFrame(t, frames)
	cancelFirst()
	src.answer(t, "", tasks.HostResult{Status: tasks.HostError})
	receiveInternal(t, second)
	nextFrame(t, frames)
	require.Empty(t, first, "canceled consumers receive no later observations")
	cancelSecond()
	src.answer(t, "", tasks.HostResult{Status: tasks.HostOK})
	require.Equal(t, FrameHost, nextFrame(t, frames).Type)
	require.Empty(t, second)
	cancelBrowser()
	p.mu.Lock()
	observing := p.observing
	p.mu.Unlock()
	require.False(t, observing)
}

func TestInternalSubscriberReceivesHostRemoval(t *testing.T) {
	for _, tc := range []struct {
		name    string
		known   bool
		browser bool
	}{
		{name: "known_without_browser", known: true},
		{name: "pending_without_browser"},
		{name: "known_with_browser", known: true, browser: true},
		{name: "pending_with_browser", browser: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeSource("gpu")
			p := newTestPublisher(t, src)
			events := make(chan hostAnswer, 8)
			cancel := p.SubscribeInternal(func(name string, result tasks.HostResult, ts []tasks.Task) {
				if name == "gpu" {
					events <- hostAnswer{result: result, tasks: ts}
				}
			})
			defer cancel()
			var frames <-chan Frame
			if tc.browser {
				_, next, cancelBrowser := p.Subscribe()
				frames = next
				defer cancelBrowser()
			}
			task := tasks.Task{ID: "ssh:gpu:claude:s", Agent: "claude", SessionID: "s",
				State: tasks.StateBusy, LivePIDs: []int{12}}
			if tc.known {
				src.answer(t, "gpu", tasks.HostResult{Status: tasks.HostOK}, task)
				require.Equal(t, tasks.HostOK, receiveInternal(t, events).result.Status)
			}
			// Remove the host while its next observation is still in flight.
			src.awaitStart(t, "gpu")
			src.setHosts()
			removed := receiveInternal(t, events)
			require.Equal(t, tasks.HostResult{Name: "gpu", Status: HostRemoved}, removed.result)
			require.Nil(t, removed.tasks, "removal must not replay the last live tasks")
			if tc.browser {
				for {
					frame := nextFrame(t, frames)
					if frame.Type == FrameHost && frame.Op == OpRemoved {
						require.Equal(t, "gpu", frame.Host.Name)
						break
					}
				}
			}
			// A late OK must not resurrect the removed host for internal consumers.
			src.answerChan("gpu") <- hostAnswer{result: tasks.HostResult{Status: tasks.HostOK}, tasks: []tasks.Task{task}}
			require.Eventually(t, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return !p.looping["gpu"]
			}, 2*time.Second, time.Millisecond)
			require.Empty(t, events, "removal is emitted once, and late observations are discarded")
		})
	}
}

// Bound callback waits so a broken lifecycle fails without hanging the suite.
func receiveInternal[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("internal observation was not delivered")
		var zero T
		return zero
	}
}
