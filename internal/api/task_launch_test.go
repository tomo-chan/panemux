package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// launchSessionID is the session ID launchRand makes a launch mint.
const launchSessionID = "0f0e0d0c-0b0a-4908-8706-050403020100"

// resumeSessionID is the stopped session in launchCollection.
const resumeSessionID = "5d7e3a90-1b2c-4d3e-8f40-51627384a5b6"

func launchRand() *bytes.Reader {
	return bytes.NewReader(append(
		[]byte{0x0f, 0x0e, 0x0d, 0x0c, 0x0b, 0x0a, 0x09, 0x08, 0x87, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x00},
		bytes.Repeat([]byte{0xab}, 64)...,
	))
}

// launchCollection holds one running claude session and one stopped one.
func launchCollection() []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"run-sess","cwd":"/workspace/user/project","status":"busy","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::section tmux",
		"::section cwd",
		"::section transcripts",
		"900\t" + resumeSessionID + `.jsonl	"cwd":"/workspace/user/project"`,
		"::end",
	}, "\n") + "\n")
}

// launchHost stands in for the panemux host: it answers the collection
// script with launchCollection and a launch script with answer, and keeps
// every launch script it was given.
type launchHost struct {
	answer  string
	runErr  error
	scripts []string
	mu      sync.Mutex
}

func (l *launchHost) run(_ context.Context, script string) ([]byte, error) {
	if !strings.Contains(script, "::panemux-launch") {
		return launchCollection(), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scripts = append(l.scripts, script)
	return []byte(l.answer), l.runErr
}

func (l *launchHost) launches() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.scripts)
}

func newLaunchHandler(t *testing.T, host *launchHost) (*Handler, string) {
	t.Helper()
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.SetTaskService(tasks.New(tasks.Options{
		Hosts:    h.taskHostNames,
		Dial:     func(string) (tasks.Conn, error) { return nil, errors.New("i/o timeout") },
		RunLocal: host.run,
		Rand:     launchRand(),
	}))
	t.Cleanup(h.Close)
	return h, useTaskRecords(t, h)
}

func postJSON(t *testing.T, h *Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	setupRouterWithHandler(h).ServeHTTP(rec, req)
	return rec
}

func TestPostTask_LaunchesAndRecordsTheLabels(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"claude","cwd":"/workspace/user/project",`+
		`"prompt":"fix the flaky test","labels":[" payment ","payment","sprint-42"]}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp taskLaunchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, taskLaunchResponse{
		Launched: tasks.Launched{
			TaskID: "local:claude:" + launchSessionID, SessionID: launchSessionID, TmuxSession: "task-0f0e0d0c",
		},
		Labels: []string{"payment", "sprint-42"},
	}, resp)
	records, err := h.taskRecords.Records()
	require.NoError(t, err)
	assert.Equal(t, []string{"payment", "sprint-42"},
		records[tasks.RecordKey{Host: "", Agent: "claude", SessionID: launchSessionID}].Labels)
	require.Equal(t, 1, host.launches())
	assert.Contains(t, host.scripts[0], "\nfix the flaky test\n")
}

func TestPostTask_WithoutLabelsWritesNoRecord(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, path := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"claude","cwd":"/w","prompt":"go"}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "labels")
	assert.NoFileExists(t, path)
}

func TestPostTask_LaunchedEvenWhenTheLabelsCannotBeSaved(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, path := newLaunchHandler(t, host)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"claude","cwd":"/w","prompt":"go","labels":["a"]}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp taskLaunchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, launchSessionID, resp.SessionID)
	assert.NotEmpty(t, resp.RecordsError)
	assert.Empty(t, resp.Labels)
}

func TestPostTask_Refusals(t *testing.T) {
	tests := []struct {
		runErr     error
		headers    map[string]string
		name       string
		body       string
		answer     string
		wantBody   string
		wantStatus int
		launched   bool
	}{
		{name: "another site's page", body: `{"host":"","agent":"claude","cwd":"/w","prompt":"go"}`,
			headers: map[string]string{"Origin": "https://evil.example"}, wantStatus: http.StatusForbidden},
		{name: "not JSON", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: `{"host":"","agent":"claude","cwd":"/w","prompt":"go","shell":"x"}`,
			wantStatus: http.StatusBadRequest},
		{name: "codex", body: `{"host":"","agent":"codex","cwd":"/w","prompt":"go"}`,
			wantStatus: http.StatusBadRequest, wantBody: "only claude"},
		{name: "missing agent", body: `{"host":"","cwd":"/w","prompt":"go"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid label", body: `{"host":"","agent":"claude","cwd":"/w","prompt":"go","labels":["a\u0007"]}`,
			wantStatus: http.StatusBadRequest},
		{name: "relative directory", body: `{"host":"","agent":"claude","cwd":"w","prompt":"go"}`,
			wantStatus: http.StatusBadRequest},
		{name: "empty prompt", body: `{"host":"","agent":"claude","cwd":"/w","prompt":"  "}`,
			wantStatus: http.StatusBadRequest},
		{name: "unknown host", body: `{"host":"nowhere","agent":"claude","cwd":"/w","prompt":"go"}`,
			wantStatus: http.StatusNotFound},
		{name: "host refuses", body: `{"host":"","agent":"claude","cwd":"/w","prompt":"go"}`,
			answer: "::panemux-launch error no-cwd\n", wantStatus: http.StatusConflict,
			wantBody: "the working directory does not exist on the host", launched: true},
		{name: "host cannot be reached", body: `{"host":"dev-server","agent":"claude","cwd":"/w","prompt":"go"}`,
			wantStatus: http.StatusBadGateway},
		{name: "run fails", body: `{"host":"","agent":"claude","cwd":"/w","prompt":"go"}`,
			runErr: errors.New("boom"), wantStatus: http.StatusBadGateway, launched: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := &launchHost{answer: tt.answer, runErr: tt.runErr}
			h, path := newLaunchHandler(t, host)

			rec := postJSON(t, h, "/api/tasks", tt.body, tt.headers)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tt.wantBody)
			assert.Equal(t, tt.launched, host.launches() > 0)
			assert.NoFileExists(t, path, "a launch that failed records nothing")
		})
	}
}

func TestPostTaskResume_ResumesAStoppedTask(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks/resume", `{"host":"","session_id":"`+resumeSessionID+`"}`, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp tasks.Launched
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, tasks.Launched{
		TaskID: "local:claude:" + resumeSessionID, SessionID: resumeSessionID, TmuxSession: "task-5d7e3a90",
	}, resp)
	require.Equal(t, 1, host.launches())
	assert.Contains(t, host.scripts[0], "mode='resume'")
}

func TestPostTaskResume_Refusals(t *testing.T) {
	tests := []struct {
		headers    map[string]string
		name       string
		body       string
		answer     string
		wantStatus int
	}{
		{name: "another site's page", body: `{"host":"","session_id":"` + resumeSessionID + `"}`,
			headers: map[string]string{"Origin": "https://evil.example"}, wantStatus: http.StatusForbidden},
		{name: "not JSON", body: `[`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: `{"host":"","session_id":"` + resumeSessionID + `","cwd":"/"}`,
			wantStatus: http.StatusBadRequest},
		{name: "session id that is not a UUID", body: `{"host":"","session_id":"--help"}`,
			wantStatus: http.StatusBadRequest},
		{name: "unknown host", body: `{"host":"nowhere","session_id":"` + resumeSessionID + `"}`,
			wantStatus: http.StatusNotFound},
		{name: "not a stopped task", body: `{"host":"","session_id":"99999999-1b2c-4d3e-8f40-51627384a5b6"}`,
			wantStatus: http.StatusConflict},
		{name: "host refuses", body: `{"host":"","session_id":"` + resumeSessionID + `"}`,
			answer: "::panemux-launch error tmux-exists\n", wantStatus: http.StatusConflict},
		{name: "host cannot be collected", body: `{"host":"dev-server","session_id":"` + resumeSessionID + `"}`,
			wantStatus: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := &launchHost{answer: tt.answer}
			h, _ := newLaunchHandler(t, host)
			rec := postJSON(t, h, "/api/tasks/resume", tt.body, tt.headers)
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tt.answer != "", host.launches() > 0, "only a request the host itself refused reached it")
		})
	}
}
