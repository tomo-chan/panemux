package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/tasks"
)

type overlappingBoardTaskConn struct {
	entered chan struct{}
	release chan struct{}
	stubTaskConn
	once  sync.Once
	block atomic.Bool
}

func (c *overlappingBoardTaskConn) Run(ctx context.Context, _ string, _ io.Reader) ([]byte, error) {
	if c.block.Load() {
		c.once.Do(func() { close(c.entered) })
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return taskCollection("remote", ""), nil
}

func newOverlappingBoardTaskPool(
	t *testing.T, remote *overlappingBoardTaskConn, failLocal *atomic.Bool,
) *hostexec.Pool {
	t.Helper()
	pool := hostexec.New(hostexec.Options{
		Hosts: func() []string { return []string{"slow"} },
		Dial:  func(string) (hostexec.Conn, error) { return remote, nil },
		RunLocal: func(context.Context, string) ([]byte, error) {
			if failLocal.Load() {
				return nil, errors.New("newer local collection failed")
			}
			return taskCollection("local", ""), nil
		},
	})
	t.Cleanup(pool.Close)
	_, err := pool.Run(context.Background(), host.Host("slow"), "warm connection")
	require.NoError(t, err)
	return pool
}

func TestBoardIdentitiesOlderFullCollectionCannotRestoreTaskAfterLiveFailure(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	defer h.Close()
	h.cfg.Layout.Children[0].Pane.Type = "tmux"
	h.cfg.Layout.Children[0].Pane.TmuxSession = "task-7c21"
	h.cfg.SSHConnections = map[string]config.SSHConnection{"slow": {}}
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
	remote := &overlappingBoardTaskConn{entered: make(chan struct{}), release: make(chan struct{})}
	var failLocal atomic.Bool
	pool := newOverlappingBoardTaskPool(t, remote, &failLocal)
	remote.block.Store(true)
	localCollected := make(chan struct{})
	var localOnce sync.Once
	var releaseOnce sync.Once
	releaseRemote := func() { releaseOnce.Do(func() { close(remote.release) }) }
	h.SetTaskService(tasks.New(tasks.Options{
		Execution: pool, Hosts: func() []string { return []string{"slow"} },
		AfterCollection: func(target host.Host) {
			if target == host.Local {
				localOnce.Do(func() { close(localCollected) })
			}
		},
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.GetTasks(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	}()
	// Always release and join before closing the handler, including a failed assertion.
	defer func() {
		releaseRemote()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("full collection did not finish")
		}
	}()
	for _, signal := range []chan struct{}{localCollected, remote.entered} {
		select {
		case <-signal:
		case <-time.After(5 * time.Second):
			t.Fatal("collection did not reach barrier")
		}
	}
	failLocal.Store(true)
	result, _ := h.TaskEventSource().CollectHostLive(context.Background(), "")
	require.Equal(t, tasks.HostError, result.Status)
	require.NotContains(t, readIdentities(t, h), "main@local")
	// The full collection may publish its older local success only after the remote host completes.
	releaseRemote()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("full collection did not finish")
	}
	assert.NotContains(t, readIdentities(t, h), "main@local")
}

func TestBoardIdentitiesOrderAllHostOutcomesByCollectionStart(t *testing.T) {
	for _, tc := range []struct {
		name         string
		older, newer tasks.HostStatus
		wantTask     bool
	}{
		{name: "older success cannot undo failure", older: tasks.HostOK, newer: tasks.HostError},
		{name: "older success cannot undo connecting", older: tasks.HostOK, newer: tasks.HostConnecting},
		{name: "older failure cannot erase success", older: tasks.HostError, newer: tasks.HostOK, wantTask: true},
		{name: "older connecting cannot erase success", older: tasks.HostConnecting, newer: tasks.HostOK, wantTask: true},
		{name: "older success cannot replace newer success", older: tasks.HostOK, newer: tasks.HostOK, wantTask: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := setupBoardRouter(board.NewBoardCache(), nil)
			defer h.Close()
			oldGeneration := h.beginBoardTaskCollection()
			newGeneration := h.beginBoardTaskCollection()
			oldTask := tasks.Task{
				State: tasks.StateWait, WaitingFor: "old wait",
				Location: tasks.Location{Kind: tasks.LocationOutside, PaneID: "main"},
			}
			newTask := oldTask
			newTask.State = tasks.StateIdle
			newTask.WaitingFor = ""
			// Identical wall-clock collection timestamps cannot establish observation order.
			at := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
			h.recordBoardTasks(newGeneration, tasks.HostResult{Status: tc.newer, CollectedAt: &at}, []tasks.Task{newTask})
			h.recordBoardTasks(oldGeneration, tasks.HostResult{Status: tc.older, CollectedAt: &at}, []tasks.Task{oldTask})
			got := readIdentities(t, h)
			if tc.wantTask {
				require.Contains(t, got, "main@local")
				assert.Equal(t, "idle", *got["main@local"].State)
				assert.Nil(t, got["main@local"].WaitingFor)
			} else {
				assert.NotContains(t, got, "main@local")
			}
			// The newer local watermark must not discard an older result from a different host.
			h.cfg.Layout.Children = append(h.cfg.Layout.Children, config.LayoutChild{
				Pane: &config.PaneConfig{ID: "worker", Type: "ssh", Connection: "build"},
			})
			h.cfg.SSHConnections = map[string]config.SSHConnection{"build": {}}
			remote := tasks.Task{Host: "build", State: tasks.StateBusy,
				Location: tasks.Location{Kind: tasks.LocationOutside, PaneID: "worker"}}
			h.recordBoardTasks(oldGeneration, tasks.HostResult{Name: "build", Status: tasks.HostOK}, []tasks.Task{remote})
			assert.Equal(t, "task", readIdentities(t, h)["worker@ssh:build"].Source)
		})
	}
}

func TestBoardIdentitiesSameGenerationReplacesPreviousOutcome(t *testing.T) {
	for _, status := range []tasks.HostStatus{tasks.HostOK, tasks.HostError, tasks.HostConnecting} {
		t.Run(string(status), func(t *testing.T) {
			h := setupBoardRouter(board.NewBoardCache(), nil)
			defer h.Close()
			generation := h.beginBoardTaskCollection()
			firstAt := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
			nextAt := firstAt.Add(time.Minute)
			task := tasks.Task{State: tasks.StateWait, WaitingFor: "old wait",
				Location: tasks.Location{Kind: tasks.LocationOutside, PaneID: "main"}}
			h.recordBoardTasks(generation, tasks.HostResult{Status: tasks.HostOK, CollectedAt: &firstAt}, []tasks.Task{task})
			require.Equal(t, "task", readIdentities(t, h)["main@local"].Source)
			task.State = tasks.StateIdle
			task.WaitingFor = ""
			h.recordBoardTasks(generation, tasks.HostResult{Status: status, CollectedAt: &nextAt}, []tasks.Task{task})
			got := readIdentities(t, h)
			if status == tasks.HostOK {
				require.Contains(t, got, "main@local")
				assert.Equal(t, "idle", *got["main@local"].State)
				assert.Nil(t, got["main@local"].WaitingFor)
				assert.Equal(t, nextAt, *got["main@local"].TaskObservedAt)
			} else {
				assert.NotContains(t, got, "main@local")
			}
		})
	}
}
