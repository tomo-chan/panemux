package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/fileops"
	"panemux/internal/tasks"
)

// A new codex task has no session ID when it starts — codex picks one once
// it has its first instruction — so its labels are held and the response
// names only the tmux session the task will be found in.
func TestPostTask_CodexTaskHoldsItsLabelsUntilItsSessionIsKnown(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, path := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"codex","cwd":"/workspace/user/api",`+
		`"prompt":"-h fix it","labels":[" api ","api","payment"]}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"tmux_session":"task-0f0e0d0c","pending_labels":["api","payment"]}`, rec.Body.String())
	require.Equal(t, 1, host.launches())
	assert.Contains(t, host.scripts[0], "agent='codex'")
	assert.NoFileExists(t, path, "nothing is recorded before the session ID is known")
	assert.Equal(t, 1, h.pendingTaskLabels.Len())
}

func TestPostTask_CodexTaskWithoutLabelsHoldsNothing(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"codex","cwd":"/w","prompt":"go"}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"tmux_session":"task-0f0e0d0c"}`, rec.Body.String())
	assert.Equal(t, 0, h.pendingTaskLabels.Len())
}

func TestPostTask_ACodexLaunchThatFailedHoldsNoLabels(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch error no-codex\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"codex","cwd":"/w","prompt":"go","labels":["a"]}`, nil)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "codex was not found on the host")
	assert.Equal(t, 0, h.pendingTaskLabels.Len())
}

func TestPostTaskResume_ResumesAStoppedCodexTask(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks/resume", `{"host":"","agent":"codex","session_id":"`+resumeCodexSessionID+`"}`, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp tasks.Launched
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, tasks.Launched{
		TaskID: "local:codex:" + resumeCodexSessionID, SessionID: resumeCodexSessionID, TmuxSession: "task-5ebcc524",
	}, resp)
	require.Equal(t, 1, host.launches())
	assert.Contains(t, host.scripts[0], "agent='codex'")
	assert.Contains(t, host.scripts[0], "\n/workspace/user/api\nPANEMUX_CWD_")
}

func TestPostTaskResume_AnExplicitClaudeAgent(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)
	rec := postJSON(t, h, "/api/tasks/resume", `{"host":"","agent":"claude","session_id":"`+resumeSessionID+`"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, host.scripts[0], "agent='claude'")
}

// codexLaunchHost answers the launch and, once started is set, collects a
// codex process in the new task's tmux session holding its rollout open.
type codexLaunchHost struct {
	started bool
	mu      sync.Mutex
}

func (c *codexLaunchHost) run(_ context.Context, script string) ([]byte, error) {
	if strings.Contains(script, "::panemux-launch") {
		c.mu.Lock()
		c.started = true
		c.mu.Unlock()
		return []byte("::panemux-launch ok\n"), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	lines := []string{"::panemux-tasks v1", "::now 1000", "::section ps"}
	if c.started {
		lines = append(lines,
			"40 1 -bash",
			"41 40 /usr/local/bin/codex -c check_for_update_on_startup=false -- go",
			"::section tmux",
			"40 task-0f0e0d0c",
			"::section codex-open",
			"41\t00:30",
			"41\t00:30\t999 10\tcompleted 980\t\t\t\t/h/.codex/sessions/2026/09/27/"+
				"rollout-2026-09-27T11-57-03-"+resumeCodexSessionID+".jsonl",
		)
	}
	return []byte(strings.Join(append(lines, "::end"), "\n") + "\n"), nil
}

// The first GET /api/tasks that finds codex's session in the task's tmux
// session records the held labels under it — added to anything already
// recorded — and lists the task with them.
func TestGetTasks_RecordsTheLabelsACodexTaskWasStartedWith(t *testing.T) {
	h, path := newLaunchHandler(t, &launchHost{})
	host := &codexLaunchHost{}
	h.SetTaskService(tasks.New(tasks.Options{
		Hosts: func() []string { return nil }, RunLocal: host.run, Rand: launchRand(),
	}))
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
	_, err := h.taskRecords.Put(tasks.Record{
		Agent: tasks.AgentCodex, SessionID: resumeCodexSessionID, Done: true, Labels: []string{"payment", "older"},
	})
	require.NoError(t, err)

	rec := postJSON(t, h, "/api/tasks",
		`{"host":"","agent":"codex","cwd":"/w","prompt":"go","labels":["api","payment"]}`, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	resp := getTasks(t, h)
	task := findTaskResponse(t, resp, "local:codex:"+resumeCodexSessionID)
	assert.Equal(t, []string{"payment", "older", "api"}, task.Labels)
	assert.True(t, task.Done, "the rest of the record is kept")
	assert.Equal(t, 0, h.pendingTaskLabels.Len())
	assert.FileExists(t, path)

	records, err := h.taskRecords.Records()
	require.NoError(t, err)
	assert.Equal(t, []string{"payment", "older", "api"},
		records[tasks.RecordKey{Agent: tasks.AgentCodex, SessionID: resumeCodexSessionID}].Labels)
}

// Held labels that cannot be recorded stay held, and the tasks are listed
// all the same: the record file cannot be read, the labels would take the
// task past tasks.MaxLabels, or the file cannot be written.
func TestGetTasks_KeepsACodexTasksLabelsItCouldNotRecord(t *testing.T) {
	tooMany := make([]string, tasks.MaxLabels)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("l%02d", i)
	}
	for _, tt := range []struct {
		setup func(t *testing.T, h *Handler, path string)
		name  string
	}{
		{name: "unreadable record file", setup: func(t *testing.T, _ *Handler, path string) {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
		}},
		{name: "too many labels", setup: func(t *testing.T, h *Handler, _ string) {
			_, err := h.taskRecords.Put(tasks.Record{Agent: tasks.AgentCodex, SessionID: resumeCodexSessionID, Labels: tooMany})
			require.NoError(t, err)
		}},
		{name: "record file cannot be written", setup: func(t *testing.T, _ *Handler, _ string) {
			fileops.SetOpsForTest(t, (&fileops.Spy{RenameErr: errors.New("disk full")}).Ops())
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, path := newLaunchHandler(t, &launchHost{})
			host := &codexLaunchHost{}
			h.SetTaskService(tasks.New(tasks.Options{
				Hosts: func() []string { return nil }, RunLocal: host.run, Rand: launchRand(),
			}))
			h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
			rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"codex","cwd":"/w","prompt":"go","labels":["api"]}`, nil)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			tt.setup(t, h, path)

			resp := getTasks(t, h)

			findTaskResponse(t, resp, "local:codex:"+resumeCodexSessionID)
			assert.Equal(t, 1, h.pendingTaskLabels.Len(), "kept for the next collection")
		})
	}
}
