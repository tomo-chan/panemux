package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
	"panemux/internal/tasks"
)

const (
	inputGivenSession = "01a0e2b9-d054-7cc2-9278-a5e25ebcc524"
	inputGivenTurn    = "01a0ec57-1885-7d80-82a2-420432147b62"
)

// inputGivenHandler is a panemux host with a codex session whose turn
// waits on an approval request its hook recorded.
func inputGivenHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
	useTaskRecords(t, h)
	out := strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1790000000",
		"::section ps",
		"31 1 codex",
		"::section codex-open",
		"31\t10:00\t1789999970 100\tinProgress 1789999880\t" +
			`{"timestamp":"2026-09-27T11:58:30.057Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` +
			inputGivenTurn + `"}}` + "\t\t\t\"originator\":\"codex-tui\"\t" +
			"/remote/home/demo/.codex/sessions/2026/09/27/rollout-2026-09-27T11-57-03-" + inputGivenSession + ".jsonl",
		"::section codex-permission",
		inputGivenSession + " " + inputGivenTurn + " 1789999960",
		"::end",
	}, "\n") + "\n"
	h.SetTaskService(tasks.New(tasks.Options{
		Hosts:    h.taskHostNames,
		Dial:     func(string) (tasks.Conn, error) { return nil, errors.New("unreachable") },
		RunLocal: func(context.Context, string) ([]byte, error) { return []byte(out), nil },
	}))
	t.Cleanup(h.Close)
	return h
}

func postInputGiven(h *Handler, body string, crossSite bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/input-given", strings.NewReader(body))
	if crossSite {
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, req)
	return rec
}

// "Input given" takes the codex task out of its approval wait.
func TestPostTaskInputGiven(t *testing.T) {
	h := inputGivenHandler(t)
	id := "local:codex:" + inputGivenSession
	waiting := findTaskResponse(t, getTasks(t, h), id)
	require.Equal(t, tasks.StateWait, waiting.State)
	require.NotEmpty(t, waiting.WaitSignature)

	rec := postInputGiven(h, `{"wait_signature":"`+waiting.WaitSignature+`"}`, false)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	assert.Equal(t, tasks.StateBusy, findTaskResponse(t, getTasks(t, h), id).State)
}

func TestPostTaskInputGiven_Refusals(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		status    int
		crossSite bool
	}{
		{name: "not JSON", body: `x`, status: http.StatusBadRequest},
		{name: "unknown field", body: `{"wait_signature":"w1-a","x":1}`, status: http.StatusBadRequest},
		{name: "no signature", body: `{"wait_signature":""}`, status: http.StatusBadRequest},
		{name: "another site's page", body: `{"wait_signature":"w1-a"}`, status: http.StatusForbidden, crossSite: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := inputGivenHandler(t)
			id := "local:codex:" + inputGivenSession
			signature := findTaskResponse(t, getTasks(t, h), id).WaitSignature
			body := strings.ReplaceAll(tc.body, "w1-a", signature)
			assert.Equal(t, tc.status, postInputGiven(h, body, tc.crossSite).Code)
			assert.Equal(t, tasks.StateWait, findTaskResponse(t, getTasks(t, h), id).State)
		})
	}
}
