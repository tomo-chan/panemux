package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// attentionCollection is one host's collection with a waiting claude task
// and a stopped one, as the full script would print it.
func attentionCollection(sessionID string) []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"` + sessionID + `","cwd":"/workspace/user/project","status":"waiting",` +
			`"waitingFor":"input needed","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::section transcripts",
		"990\tstopped-sess.jsonl\t\"cwd\":\"/workspace/user/project\"\t10",
		"::end",
	}, "\n") + "\n")
}

func getAttention(t *testing.T, h *Handler) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks/attention", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.Bytes()
}

// GET /api/tasks/attention answers with the hosts and the running tasks
// only, each with the same wait signature GET /api/tasks gives it, and
// nothing the dashboard adds: no git lookup, records, labels or summaries.
func TestGetTasksAttention_ListsRunningTasksWithoutDashboardExtras(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"dev-server": {Host: "dev.invalid"},
		"gpu-box":    {Host: "gpu.invalid"},
	}
	cfg.TaskDashboard.Summary.Enabled = true
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	useTaskRecords(t, h)
	useTaskService(h, attentionCollection("local-sess"), map[string]tasks.Conn{
		"dev-server": &stubTaskConn{output: attentionCollection("remote-sess")},
	})
	var lookups atomic.Int32
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo {
		lookups.Add(1)
		return &taskGitInfo{Branch: "main"}
	}
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/tasks/records",
		strings.NewReader(`{"host":"","agent":"claude","session_id":"local-sess","done":true,"labels":["x"]}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	full := getTasks(t, h)
	lookups.Store(0)
	body := getAttention(t, h)
	assert.Zero(t, lookups.Load(), "no git or pull request lookup")

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	assert.ElementsMatch(t, []string{"hosts", "tasks"}, keysOf(raw))

	var resp struct {
		Hosts []tasks.HostResult `json:"hosts"`
		Tasks []map[string]any   `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Len(t, resp.Hosts, 3)
	assert.Equal(t, []string{"", "dev-server", "gpu-box"},
		[]string{resp.Hosts[0].Name, resp.Hosts[1].Name, resp.Hosts[2].Name})
	assert.Equal(t, tasks.HostOK, resp.Hosts[1].Status)
	assert.Equal(t, tasks.HostError, resp.Hosts[2].Status)
	assert.Contains(t, resp.Hosts[2].Error, "i/o timeout")

	require.Len(t, resp.Tasks, 2, "the stopped sessions are not listed: %v", resp.Tasks)
	for _, task := range resp.Tasks {
		for _, key := range []string{"git", "summary", "labels", "done"} {
			assert.NotContains(t, task, key)
		}
		id, _ := task["id"].(string)
		want := findTaskResponse(t, full, id)
		require.NotEmpty(t, want.WaitSignature)
		assert.Equal(t, want.WaitSignature, task["wait_signature"], id)
		assert.Equal(t, "wait", task["state"])
	}
}

func TestGetTasksAttention_AllHostsFailingIsStillOK(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	useTaskService(h, []byte("garbage"), nil)

	body := getAttention(t, h)
	assert.Contains(t, string(body), `"tasks":[]`)
	assert.Contains(t, string(body), `"status":"error"`)
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
