package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
	"panemux/internal/tasks"
)

// summaryCollection is a panemux host with an idle claude session "idle-s"
// and a stopped session "stopped-s", each with a conversation log.
func summaryCollection() []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"idle-s","cwd":"/workspace/user/project","status":"idle","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::section tmux",
		"::section cwd",
		"::section transcripts",
		"995\tidle-s.jsonl\t\t120",
		"900\tstopped-s.jsonl\t\t80",
		"::end",
	}, "\n") + "\n")
}

func summaryLog() []byte {
	body := `{"type":"user","message":{"role":"user","content":"Fix the flaky test"}}` + "\n"
	return []byte("::panemux-transcript v1 " + strconv.Itoa(len(body)) + "\n" + body + "\n::end\n")
}

type countingSummarizer struct {
	calls int
	mu    sync.Mutex
}

func (c *countingSummarizer) summarize(context.Context, string) (tasks.Summary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return tasks.Summary{Text: "Fixing a flaky test.", Remaining: []string{"Run make check"}}, nil
}

func (c *countingSummarizer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// useSummaryTaskService gives h a collector whose panemux host lists
// summaryCollection and serves summaryLog, and whose summaries are made by
// summarizer.
func useSummaryTaskService(t *testing.T, h *Handler, summarizer *countingSummarizer) {
	t.Helper()
	h.SetTaskService(tasks.New(tasks.Options{
		Hosts: h.taskHostNames,
		Dial:  func(string) (tasks.Conn, error) { return nil, errors.New("unreachable") },
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			if strings.Contains(script, "::panemux-transcript") {
				return summaryLog(), nil
			}
			return summaryCollection(), nil
		},
		Summarize: summarizer.summarize,
	}))
	t.Cleanup(h.Close)
}

func summaryHandler(t *testing.T, enabled bool) (*Handler, *countingSummarizer) {
	t.Helper()
	cfg := defaultTestConfig()
	cfg.TaskDashboard.Summary.Enabled = enabled
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
	useTaskRecords(t, h)
	summarizer := &countingSummarizer{}
	useSummaryTaskService(t, h, summarizer)
	return h, summarizer
}

// Summaries are off unless task_dashboard.summary.enabled is set: nothing is
// summarized and no task carries a summary.
func TestGetTasks_SummariesAreOffUnlessEnabled(t *testing.T) {
	h, summarizer := summaryHandler(t, false)

	resp := getTasks(t, h)
	assert.False(t, resp.SummariesEnabled)
	for _, task := range resp.Tasks {
		assert.Nil(t, task.Summary)
	}
	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, summarizer.count())
}

// With summaries on, an idle task is summarized by the poll and the answer
// comes back on a later poll; the stopped task waits to be asked.
func TestGetTasks_SummarizesAnIdleTask(t *testing.T) {
	h, summarizer := summaryHandler(t, true)

	resp := getTasks(t, h)
	assert.True(t, resp.SummariesEnabled)
	require.NotNil(t, findTaskResponse(t, resp, "local:claude:idle-s").Summary)

	var idle taskResponse
	require.Eventually(t, func() bool {
		idle = findTaskResponse(t, getTasks(t, h), "local:claude:idle-s")
		return idle.Summary != nil && idle.Summary.State == tasks.SummaryReady
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, "Fixing a flaky test.", idle.Summary.Text)
	assert.Equal(t, []string{"Run make check"}, idle.Summary.Remaining)
	assert.Nil(t, findTaskResponse(t, getTasks(t, h), "local:claude:stopped-s").Summary)
	assert.Equal(t, 1, summarizer.count(), "polls reuse the summary")
}

func TestGetTasks_SummaryWireFieldNames(t *testing.T) {
	h, _ := summaryHandler(t, true)
	getTasks(t, h)

	var raw map[string]any
	require.Eventually(t, func() bool {
		rec := httptest.NewRecorder()
		setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
		raw = nil
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
		for _, task := range raw["tasks"].([]any) {
			summary, _ := task.(map[string]any)["summary"].(map[string]any)
			if summary != nil && summary["state"] == "ready" {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, true, raw["summaries_enabled"])
	for _, task := range raw["tasks"].([]any) {
		summary, _ := task.(map[string]any)["summary"].(map[string]any)
		if summary == nil {
			continue
		}
		for _, key := range []string{"state", "text", "remaining", "summarized_at"} {
			assert.Contains(t, summary, key)
		}
	}
}

func postTaskSummary(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/api/tasks/summary", strings.NewReader(body)))
	return rec
}

func TestPostTaskSummary(t *testing.T) {
	h, summarizer := summaryHandler(t, true)
	getTasks(t, h)

	rec := postTaskSummary(t, h, `{"host":"","session_id":"stopped-s"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var view tasks.SummaryView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Contains(t, []tasks.SummaryState{tasks.SummaryPending, tasks.SummaryReady}, view.State)

	require.Eventually(t, func() bool {
		stopped := findTaskResponse(t, getTasks(t, h), "local:claude:stopped-s")
		return stopped.Summary != nil && stopped.Summary.State == tasks.SummaryReady
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, 2, summarizer.count(), "the idle task by the poll, the stopped one on request")
}

func TestPostTaskSummary_Refusals(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		status  int
		enabled bool
	}{
		{name: "summaries off", body: `{"host":"","session_id":"stopped-s"}`, status: http.StatusConflict},
		{name: "not JSON", body: `x`, status: http.StatusBadRequest, enabled: true},
		{name: "unknown field", body: `{"host":"","session_id":"s","x":1}`, status: http.StatusBadRequest, enabled: true},
		{name: "invalid session ID", body: `{"host":"","session_id":"a b"}`, status: http.StatusBadRequest, enabled: true},
		{name: "not listed", body: `{"host":"","session_id":"nope"}`, status: http.StatusNotFound, enabled: true},
		{
			name: "unknown host", body: `{"host":"gpu-box","session_id":"stopped-s"}`,
			status: http.StatusNotFound, enabled: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, summarizer := summaryHandler(t, tc.enabled)
			getTasks(t, h)
			rec := postTaskSummary(t, h, tc.body)
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			time.Sleep(20 * time.Millisecond)
			if !tc.enabled {
				assert.Zero(t, summarizer.count())
			}
		})
	}
}

// Asking for a summary runs claude, so another site's page must not be able
// to ask.
func TestPostTaskSummary_RefusesCrossSiteRequests(t *testing.T) {
	h, summarizer := summaryHandler(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/summary",
		strings.NewReader(`{"host":"","session_id":"stopped-s"}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, summarizer.count())
}

// A summary is not started once the dashboard's collector has been closed
// (panemux is shutting down).
func TestPostTaskSummary_AfterCloseIsUnavailable(t *testing.T) {
	h, summarizer := summaryHandler(t, true)
	require.Eventually(t, func() bool {
		idle := findTaskResponse(t, getTasks(t, h), "local:claude:idle-s")
		return idle.Summary != nil && idle.Summary.State == tasks.SummaryReady
	}, 5*time.Second, 20*time.Millisecond, "the poll's own summary has finished")
	h.Close()

	rec := postTaskSummary(t, h, `{"host":"","session_id":"stopped-s"}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 1, summarizer.count())
}
