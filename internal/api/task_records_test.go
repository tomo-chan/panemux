package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/fileops"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// recordTaskCollection is one host's collection with a running claude session
// ("run-sess"), a stopped one ("stop-sess") and a codex process, which has no
// session ID.
func recordTaskCollection() []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"run-sess","cwd":"/workspace/user/project","status":"busy","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"8 1 codex",
		"::section tmux",
		"::section cwd",
		"8 /workspace/user/project",
		"::section transcripts",
		`900	stop-sess.jsonl	"cwd":"/workspace/user/project"`,
		"::end",
	}, "\n") + "\n")
}

// useTaskRecords points the handler's record store at a file under a test
// directory and returns the file's path.
func useTaskRecords(t *testing.T, h *Handler) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panemux", "tasks.json")
	h.taskRecords = tasks.NewRecordStore(path)
	return path
}

func newRecordHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
	useTaskService(h, recordTaskCollection(), map[string]tasks.Conn{
		"dev-server": &stubTaskConn{output: recordTaskCollection()},
	})
	return h, useTaskRecords(t, h)
}

func putTaskRecord(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/tasks/records", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	setupRouterWithHandler(h).ServeHTTP(rec, req)
	return rec
}

func TestPutTaskRecord_StoresAndReturnsTheRecord(t *testing.T) {
	h, path := newRecordHandler(t)

	rec := putTaskRecord(t, h, `{"host":"dev-server","agent":"claude","session_id":"run-sess","done":true,`+
		`"labels":[" payment ","sprint-42","payment"]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t,
		`{"host":"dev-server","agent":"claude","session_id":"run-sess","done":true,"labels":["payment","sprint-42"]}`,
		rec.Body.String(), "labels come back normalized")

	stored, err := tasks.NewRecordStore(path).Records()
	require.NoError(t, err)
	assert.Equal(t, tasks.Record{
		Host: "dev-server", Agent: "claude", SessionID: "run-sess", Done: true, Labels: []string{"payment", "sprint-42"},
	}, stored[tasks.RecordKey{Host: "dev-server", Agent: "claude", SessionID: "run-sess"}])
}

// Clearing a record answers with the empty record, and the response always
// carries both fields so the dashboard can apply it as it is.
func TestPutTaskRecord_ClearedRecordCarriesBothFields(t *testing.T) {
	h, _ := newRecordHandler(t)
	require.Equal(t, http.StatusOK,
		putTaskRecord(t, h, `{"host":"","agent":"claude","session_id":"run-sess","done":true}`).Code)

	rec := putTaskRecord(t, h, `{"host":"","agent":"claude","session_id":"run-sess","done":false,"labels":[]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"host":"","agent":"claude","session_id":"run-sess","done":false,"labels":[]}`, rec.Body.String())
}

func TestPutTaskRecord_Refusals(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantBody string
		wantCode int
	}{
		{name: "not JSON", body: `{`, wantCode: http.StatusBadRequest},
		{name: "unknown field", body: `{"agent":"claude","session_id":"s","state":"done"}`, wantCode: http.StatusBadRequest},
		{
			name:     "host that is not an ssh_connections key",
			body:     `{"host":"elsewhere","agent":"claude","session_id":"s","done":true}`,
			wantCode: http.StatusNotFound, wantBody: "unknown host",
		},
		{
			name: "invalid session ID", body: `{"host":"","agent":"claude","session_id":"a b"}`,
			wantCode: http.StatusBadRequest, wantBody: "invalid session ID",
		},
		{
			name: "no session ID (a pid-keyed task)", body: `{"host":"","agent":"codex","done":true}`,
			wantCode: http.StatusBadRequest, wantBody: "invalid session ID",
		},
		{
			name: "unknown agent", body: `{"host":"","agent":"vim","session_id":"s"}`,
			wantCode: http.StatusBadRequest, wantBody: "unknown agent",
		},
		{
			name: "empty label", body: `{"host":"","agent":"claude","session_id":"s","labels":[""]}`,
			wantCode: http.StatusBadRequest, wantBody: "label is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, path := newRecordHandler(t)

			rec := putTaskRecord(t, h, tc.body)

			assert.Equal(t, tc.wantCode, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.wantBody)
			_, err := os.Stat(path)
			assert.True(t, os.IsNotExist(err), "nothing is written")
		})
	}
}

func TestPutTaskRecord_StorageFailureIs500(t *testing.T) {
	h, _ := newRecordHandler(t)
	fileops.SetOpsForTest(t, (&fileops.Spy{RenameErr: errors.New("disk full")}).Ops())

	rec := putTaskRecord(t, h, `{"host":"","agent":"claude","session_id":"run-sess","done":true}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "disk full")
}

func TestGetTasks_CarriesEachTasksRecord(t *testing.T) {
	h, _ := newRecordHandler(t)
	for _, body := range []string{
		`{"host":"","agent":"claude","session_id":"run-sess","done":true,"labels":["a"]}`,
		`{"host":"","agent":"claude","session_id":"stop-sess","labels":["b","c"]}`,
		`{"host":"dev-server","agent":"claude","session_id":"stop-sess","done":true}`,
		// A record whose session is not listed is kept but shown nowhere.
		`{"host":"","agent":"claude","session_id":"gone-sess","done":true,"labels":["old"]}`,
	} {
		require.Equal(t, http.StatusOK, putTaskRecord(t, h, body).Code)
	}

	resp := getTasks(t, h)

	running := findTaskResponse(t, resp, "local:claude:run-sess")
	assert.True(t, running.Done, "a running task keeps its done record")
	assert.Equal(t, tasks.StateBusy, running.State, "done does not change the state the host reported")
	assert.Equal(t, []string{"a"}, running.Labels)

	stopped := findTaskResponse(t, resp, "local:claude:stop-sess")
	assert.False(t, stopped.Done)
	assert.Equal(t, []string{"b", "c"}, stopped.Labels)

	remote := findTaskResponse(t, resp, "ssh:dev-server:claude:stop-sess")
	assert.True(t, remote.Done, "the record is the remote host's, not the local one's")
	assert.Empty(t, remote.Labels)

	remoteRunning := findTaskResponse(t, resp, "ssh:dev-server:claude:run-sess")
	assert.False(t, remoteRunning.Done)
	assert.Empty(t, remoteRunning.Labels)

	codex := findTaskResponse(t, resp, "local:codex:pid-8")
	assert.False(t, codex.Done)
	assert.Empty(t, codex.Labels)
	assert.Empty(t, resp.RecordsError)
	for _, task := range resp.Tasks {
		assert.NotEqual(t, "gone-sess", task.SessionID)
	}
}

func TestGetTasks_RecordFieldsOnTheWire(t *testing.T) {
	h, _ := newRecordHandler(t)
	require.Equal(t, http.StatusOK,
		putTaskRecord(t, h, `{"host":"","agent":"claude","session_id":"run-sess","done":true,"labels":["a"]}`).Code)

	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var raw struct {
		Tasks []map[string]any `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	for _, task := range raw.Tasks {
		if task["id"] == "local:claude:run-sess" {
			assert.Equal(t, true, task["done"])
			assert.Equal(t, []any{"a"}, task["labels"])
			continue
		}
		assert.NotContains(t, task, "done", "%v: done is omitted when false", task["id"])
		assert.NotContains(t, task, "labels", "%v: labels are omitted when there are none", task["id"])
	}
	assert.NotContains(t, rec.Body.String(), "records_error")
}

// A record file that cannot be read does not cost the dashboard its tasks:
// they are listed without records, and the reason is reported once.
func TestGetTasks_AnUnreadableRecordFileIsReportedNotFatal(t *testing.T) {
	h, path := newRecordHandler(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))

	resp := getTasks(t, h)

	assert.NotEmpty(t, resp.Tasks)
	assert.Contains(t, resp.RecordsError, "parsing task record file")
	for _, task := range resp.Tasks {
		assert.False(t, task.Done)
		assert.Empty(t, task.Labels)
	}

	rec := putTaskRecord(t, h, `{"host":"","agent":"claude","session_id":"run-sess","done":true}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, "a file it cannot read is not overwritten")
}

// A host removed from ssh_connections keeps its records in the file. A
// request that clears one is still accepted, so they can be removed; one
// that adds a record is refused.
func TestPutTaskRecord_ARemovedHostsRecordCanStillBeCleared(t *testing.T) {
	h, path := newRecordHandler(t)
	require.Equal(t, http.StatusOK, putTaskRecord(t, h,
		`{"host":"dev-server","agent":"claude","session_id":"s1","done":true,"labels":["a"]}`).Code)
	delete(h.cfg.SSHConnections, "dev-server")

	rec := putTaskRecord(t, h, `{"host":"dev-server","agent":"claude","session_id":"s1","done":true}`)
	assert.Equal(t, http.StatusNotFound, rec.Code, "a record is not added for a host that is not configured")

	rec = putTaskRecord(t, h, `{"host":"dev-server","agent":"claude","session_id":"s1","done":false,"labels":[]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t,
		`{"host":"dev-server","agent":"claude","session_id":"s1","done":false,"labels":[]}`, rec.Body.String())
	stored, err := tasks.NewRecordStore(path).Records()
	require.NoError(t, err)
	assert.Empty(t, stored)
}
