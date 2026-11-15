package api

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/fileops"
	"panemux/internal/tasks"
)

type trackedBoardPoolConn struct {
	stubTaskConn
	closed atomic.Int32
}

func (c *trackedBoardPoolConn) Close() error { c.closed.Add(1); return nil }

func boardWithWarmTaskPool(t *testing.T) (*sshConnectionsEnv, *atomic.Int32, *trackedBoardPoolConn) {
	t.Helper()
	e, _ := observedSSHBoard(t)
	panes := e.cfg.Workspaces.Items[0].Layout.Children
	panes[len(panes)-1].Pane.TmuxSession = "task-7c21"
	old := &trackedBoardPoolConn{stubTaskConn: stubTaskConn{output: taskCollection("old-target", "")}}
	dials := &atomic.Int32{}
	e.h.SetTaskService(tasks.New(tasks.Options{
		Hosts: e.h.taskHostNames,
		Dial: func(string) (tasks.Conn, error) {
			if dials.Add(1) == 1 {
				return old, nil
			}
			return &stubTaskConn{output: []byte("new target cannot report tasks")}, nil
		},
	}))
	result, _ := e.h.TaskEventSource().CollectHostLive(context.Background(), "secret")
	require.Equal(t, tasks.HostOK, result.Status)
	require.Equal(t, "task", readIdentities(t, e.h)["rp@ssh:secret"].Source)
	require.Equal(t, int32(1), dials.Load())
	return e, dials, old
}

func replaceTaskPoolConfig(t *testing.T, e *sshConnectionsEnv) {
	t.Helper()
	// Replay the PUT helper at the precise boundary before the old unlocked Reconnect.
	e.h.cfgMu.Lock()
	next := maps.Clone(e.cfg.SSHConnections)
	conn := next["secret"]
	conn.Host = "new.invalid"
	next["secret"] = conn
	ok := e.h.replaceSSHConnections(httptest.NewRecorder(), next)
	e.h.cfgMu.Unlock()
	require.True(t, ok)
}

func TestBoardConnectionChangesRedialBeforeAcceptingFreshCollection(t *testing.T) {
	for _, change := range []string{"recreated target", "recreated same target", "updated before unlocked reconnect"} {
		t.Run(change, func(t *testing.T) {
			e, dials, old := boardWithWarmTaskPool(t)
			if change == "updated before unlocked reconnect" {
				replaceTaskPoolConfig(t, e)
			} else {
				require.Equal(t, 204, e.do(t, http.MethodDelete, sshConnectionsPath+"/secret", "").Code)
				target := "new.invalid"
				if change == "recreated same target" {
					target = "secret.invalid"
				}
				require.Equal(t, 201, e.do(t, http.MethodPost, sshConnectionsPath,
					`{"name":"secret","host":"`+target+`"}`).Code)
			}
			result, _ := e.h.TaskEventSource().CollectHostLive(context.Background(), "secret")
			assert.Equal(t, tasks.HostError, result.Status, "fresh collection must use the new target, which cannot report")
			assert.Equal(t, int32(2), dials.Load(), "the old pooled connection must not survive the config boundary")
			assert.Equal(t, int32(1), old.closed.Load())
			assert.NotContains(t, readIdentities(t, e.h), "rp@ssh:secret")
		})
	}
}

func TestBoardConnectionUnchangedAndFailedSavePreservePool(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			e, dials, old := boardWithWarmTaskPool(t)
			if fail {
				fileops.SetOpsForTest(t, (&fileops.Spy{RenameErr: errors.New("disk full")}).Ops())
			}
			e.h.cfgMu.Lock()
			next := maps.Clone(e.cfg.SSHConnections)
			if fail {
				conn := next["secret"]
				conn.Host = "new.invalid"
				next["secret"] = conn
			}
			ok := e.h.replaceSSHConnections(httptest.NewRecorder(), next)
			e.h.cfgMu.Unlock()
			require.Equal(t, !fail, ok)
			result, _ := e.h.TaskEventSource().CollectHostLive(context.Background(), "secret")
			assert.Equal(t, tasks.HostOK, result.Status)
			assert.Equal(t, int32(1), dials.Load())
			assert.Equal(t, int32(0), old.closed.Load())
			assert.Equal(t, "task", readIdentities(t, e.h)["rp@ssh:secret"].Source)
		})
	}
}
