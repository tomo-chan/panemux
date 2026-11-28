package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/fileops"
	"panemux/internal/taskevents"
	"panemux/internal/tasks"
)

func TestBoardIdentitiesRequireUnambiguousPaneForEachTask(t *testing.T) {
	for _, tc := range []struct {
		name, firstHost, secondHost string
		ambiguous                   bool
	}{
		{name: "shared local tmux", ambiguous: true},
		{name: "shared remote tmux", firstHost: "build", secondHost: "build", ambiguous: true},
		{name: "same session on different hosts", secondHost: "build"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := board.NewBoardCache()
			h := setupBoardRouter(c, nil)
			defer h.Close()
			h.cfg.SSHConnections = map[string]config.SSHConnection{"build": {}}
			h.cfg.Workspaces.Items = nil
			h.cfg.Layout.Children = nil
			for i, target := range []string{tc.firstHost, tc.secondHost} {
				name := []string{"pane-a", "pane-b"}[i]
				kind, boardHost := "tmux", "local"
				if target != "" {
					kind, boardHost = "ssh_tmux", "ssh:"+target
				}
				h.cfg.Workspaces.Items = append(h.cfg.Workspaces.Items, config.WorkspaceConfig{ID: name,
					Layout: config.LayoutNode{Children: []config.LayoutChild{{Pane: &config.PaneConfig{
						ID: name, Type: kind, Connection: target, TmuxSession: "shared",
					}}}}})
				if i == 0 {
					c.RecordStatus(name+"@"+boardHost, board.Status{State: "working"})
				}
			}
			for _, target := range []string{"", "build"} {
				task := tasks.Task{Host: target, State: tasks.StateWait, WaitingFor: "review",
					Location: tasks.Location{Kind: tasks.LocationTmux, TmuxSession: "shared"}}
				h.recordBoardTasks(h.beginBoardTaskCollection(),
					tasks.HostResult{Name: target, Status: tasks.HostOK}, []tasks.Task{task})
			}
			got := readIdentities(t, h)
			for i, target := range []string{tc.firstHost, tc.secondHost} {
				boardHost := "local"
				if target != "" {
					boardHost = "ssh:" + target
				}
				e := got[[]string{"pane-a", "pane-b"}[i]+"@"+boardHost]
				if !tc.ambiguous {
					assert.Equal(t, "task", e.Source)
					continue
				}
				want := "unreported"
				if i == 0 {
					want = "self_report"
				}
				assert.Equal(t, want, e.Source)
				assert.Nil(t, e.StatusSince)
				assert.Nil(t, e.WaitingFor)
			}
		})
	}
}

func observedSSHBoard(t *testing.T) (*sshConnectionsEnv, tasks.Task) {
	t.Helper()
	e := newSSHConnectionsEnv(t, "Host secret\n HostName secret.invalid\n")
	t.Cleanup(e.h.Close)
	e.h.boardCache = board.NewBoardCache()
	e.cfg.Workspaces.Items[0].Layout.Children = append(e.cfg.Workspaces.Items[0].Layout.Children,
		config.LayoutChild{Pane: &config.PaneConfig{ID: "rp", Type: "ssh_tmux", Connection: "secret", TmuxSession: "shared"}})
	task := tasks.Task{Host: "secret", State: tasks.StateWait,
		Location: tasks.Location{Kind: tasks.LocationTmux, TmuxSession: "shared"}}
	return e, task
}

func TestBoardIdentitiesInvalidateChangedOrRecreatedConnection(t *testing.T) {
	for _, change := range []string{"update", "delete and re-add same target", "delete and re-add new target"} {
		t.Run(change, func(t *testing.T) {
			e, task := observedSSHBoard(t)
			generation := e.h.beginBoardTaskCollection()
			result := tasks.HostResult{Name: "secret", Status: tasks.HostOK}
			e.h.recordBoardTasks(generation, result, []tasks.Task{task})
			require.Equal(t, "task", readIdentities(t, e.h)["rp@ssh:secret"].Source)
			if change == "update" {
				require.Equal(t, 200, e.do(t, http.MethodPut, sshConnectionsPath+"/secret", `{"host":"new.invalid"}`).Code)
			} else {
				require.Equal(t, 204, e.do(t, http.MethodDelete, sshConnectionsPath+"/secret", "").Code)
				assert.NotContains(t, readIdentities(t, e.h), "rp@ssh:secret")
				e.h.recordBoardTasks(generation, result, []tasks.Task{task})
				target := "secret.invalid"
				if change == "delete and re-add new target" {
					target = "new.invalid"
				}
				require.Equal(t, 201, e.do(t, http.MethodPost, sshConnectionsPath,
					`{"name":"secret","host":"`+target+`"}`).Code)
			}
			assert.NotContains(t, readIdentities(t, e.h), "rp@ssh:secret")
			e.h.recordBoardTasks(generation, result, []tasks.Task{task})
			assert.NotContains(t, readIdentities(t, e.h), "rp@ssh:secret", "in-flight old-target result must stay invalid")
			e.h.recordBoardTasks(e.h.beginBoardTaskCollection(), result, []tasks.Task{task})
			assert.Equal(t, "task", readIdentities(t, e.h)["rp@ssh:secret"].Source)
		})
	}
}

func TestBoardIdentitiesDeferLiveResultUntilPublisherAcceptance(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	defer h.Close()
	h.cfg.Layout.Children[0].Pane.Type = "tmux"
	h.cfg.Layout.Children[0].Pane.TmuxSession = "task-7c21"
	h.SetTaskService(tasks.New(tasks.Options{RunLocal: func(context.Context, string) ([]byte, error) {
		return taskCollection("sample", ""), nil
	}}))
	source, ok := h.TaskEventSource().(taskevents.DeferredSource)
	require.True(t, ok)
	_, _, accept := source.CollectHostLiveDeferred(context.Background(), "")
	assert.NotContains(t, readIdentities(t, h), "main@local")
	accept()
	assert.Equal(t, "task", readIdentities(t, h)["main@local"].Source)
}

func TestBoardIdentitiesConnectionInvalidationPreservesOtherAndUnchangedHosts(t *testing.T) {
	e, task := observedSSHBoard(t)
	result := tasks.HostResult{Name: "secret", Status: tasks.HostOK}
	e.h.recordBoardTasks(e.h.beginBoardTaskCollection(), result, []tasks.Task{task})
	other := tasks.Task{Host: "used", State: tasks.StateIdle,
		Location: tasks.Location{Kind: tasks.LocationOutside, PaneID: "one-ssh"}}
	e.h.recordBoardTasks(e.h.beginBoardTaskCollection(),
		tasks.HostResult{Name: "used", Status: tasks.HostOK}, []tasks.Task{other})
	require.Equal(t, 200, e.do(t, http.MethodPut, sshConnectionsPath+"/secret", `{"host":"new.invalid"}`).Code)
	assert.Equal(t, "task", readIdentities(t, e.h)["one-ssh@ssh:used"].Source)
	require.Equal(t, 200, e.do(t, http.MethodPut, sshConnectionsPath+"/used",
		`{"host":"used.invalid","user":"demo"}`).Code)
	assert.Equal(t, "task", readIdentities(t, e.h)["one-ssh@ssh:used"].Source)
}

func TestBoardIdentitiesFailedConnectionSavePreservesObservation(t *testing.T) {
	e, task := observedSSHBoard(t)
	e.h.recordBoardTasks(e.h.beginBoardTaskCollection(),
		tasks.HostResult{Name: "secret", Status: tasks.HostOK}, []tasks.Task{task})
	fileops.SetOpsForTest(t, (&fileops.Spy{RenameErr: errors.New("disk full")}).Ops())
	require.Equal(t, 500, e.do(t, http.MethodPut, sshConnectionsPath+"/secret", `{"host":"new.invalid"}`).Code)
	assert.Equal(t, "task", readIdentities(t, e.h)["rp@ssh:secret"].Source)
}
