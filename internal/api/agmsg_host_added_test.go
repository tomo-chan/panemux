package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/fileops"
	"panemux/internal/host"
	"panemux/internal/hosttools"
)

type addedHostTool struct {
	status           hosttools.Status
	checked, updated []host.Host
	mu               sync.Mutex
}

func (t *addedHostTool) Check(_ context.Context, h host.Host) hosttools.State {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.checked = append(t.checked, h)
	return hosttools.State{Status: t.status}
}
func (t *addedHostTool) Install(context.Context, host.Host) (hosttools.State, error) {
	panic("never Install automatically")
}
func (t *addedHostTool) Update(_ context.Context, h host.Host) (hosttools.State, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.updated = append(t.updated, h)
	return hosttools.State{Status: hosttools.Installed}, nil
}

func TestAddedHostGetsGlobalAutoCheckOnlyAfterSuccessfulSave(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		status                               hosttools.Status
		board, auto, saveFail, check, update bool
	}{
		{"older", hosttools.Outdated, true, true, false, true, true},
		{"missing", hosttools.Missing, true, true, false, true, false},
		{"auto off", hosttools.Outdated, true, false, false, false, false},
		{"board off", hosttools.Outdated, false, true, false, false, false},
		{"save failure", hosttools.Outdated, true, true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSSHConnectionsEnv(t, fromSSHConfigBlock).h
			t.Cleanup(h.Close)
			h.cfg.AgentBoard.Enabled, h.cfg.AgentBoard.AgmsgAutoUpdate = tc.board, tc.auto
			h.hostTools.Close()
			tool := &addedHostTool{status: tc.status}
			h.hostTools = hosttools.New(hosttools.Options{
				Hosts: h.taskHostNames, Tools: map[hosttools.ID]hosttools.Tool{hosttools.AgmsgTool: tool},
				GlobalAuto: map[hosttools.ID]func() bool{hosttools.AgmsgTool: h.agmsgAutoEnabled},
			})
			if tc.saveFail {
				fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errors.New("disk full")}).Ops())
			}
			w := httptest.NewRecorder()
			setupRouterWithHandler(h).ServeHTTP(w, httptest.NewRequest(http.MethodPost, sshConnectionsPath,
				strings.NewReader(`{"name":"build-box","host":"build.invalid"}`)))
			if tc.saveFail {
				require.Equal(t, 500, w.Code)
			} else {
				require.Equal(t, 201, w.Code, w.Body.String())
			}
			h.hostTools.Wait()
			if tc.check {
				require.NotEmpty(t, tool.checked)
				for _, target := range tool.checked {
					require.Equal(t, host.Host("build-box"), target)
				}
			} else {
				require.Empty(t, tool.checked)
			}
			if tc.update {
				require.Equal(t, []host.Host{"build-box"}, tool.updated)
			} else {
				require.Empty(t, tool.updated)
			}
		})
	}
}
