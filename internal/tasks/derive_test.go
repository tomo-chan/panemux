package tasks

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectedAt is panemux's own clock at collection time; hostNow is the
// host's. They differ on purpose, so every test also shows that times are
// carried over by age rather than copied across two clocks.
var collectedAt = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

const hostNow int64 = 1_790_000_000

func stateJSON(pid int, sessionID, status string) []byte {
	return []byte(fmt.Sprintf(
		`{"pid":%d,"sessionId":%q,"cwd":"/workspace/user/project","status":%q,`+
			`"waitingFor":"input needed","statusUpdatedAt":%d,"startedAt":%d,"procStart":"512","kind":"interactive"}`,
		pid, sessionID, status, (hostNow-180)*1000, (hostNow-3600)*1000))
}

func claudeProc(pid, ppid int) process {
	return process{PID: pid, PPID: ppid, Command: "/usr/local/bin/claude --verbose"}
}

func findTask(t *testing.T, tasks []Task, id string) Task {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	require.Failf(t, "task not found", "id %q in %+v", id, tasks)
	return Task{}
}

func TestBuildTasks_LiveClaudeStatusMapsToState(t *testing.T) {
	cases := []struct {
		status string
		want   State
	}{
		{"busy", StateBusy},
		{"waiting", StateWait},
		{"idle", StateIdle},
		{"", StateUnknown},
		{"something-new", StateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			raw := rawSnapshot{
				Now:        hostNow,
				StateFiles: []stateFile{{Name: "121.json", Data: stateJSON(121, "sess-a", tc.status)}},
				Processes:  []process{claudeProc(121, 1)},
			}
			tasks := buildTasks("", raw, collectedAt)
			require.Len(t, tasks, 1)
			task := tasks[0]
			assert.Equal(t, tc.want, task.State)
			assert.Equal(t, "local:claude:sess-a", task.ID)
			assert.Equal(t, AgentClaude, task.Agent)
			assert.Equal(t, "sess-a", task.SessionID)
			assert.Equal(t, "/workspace/user/project", task.CWD)
			assert.Equal(t, 121, task.PID)
			require.NotNil(t, task.StatusSince)
			assert.Equal(t, collectedAt.Add(-180*time.Second), *task.StatusSince)
			require.NotNil(t, task.StartedAt)
			assert.Equal(t, collectedAt.Add(-time.Hour), *task.StartedAt)
		})
	}
}

func TestBuildTasks_WaitingForIsOnlyReportedWhileWaiting(t *testing.T) {
	raw := rawSnapshot{
		Now: hostNow,
		StateFiles: []stateFile{
			{Name: "1.json", Data: stateJSON(1, "waiting", "waiting")},
			{Name: "2.json", Data: stateJSON(2, "busy", "busy")},
		},
		Processes: []process{claudeProc(1, 0), claudeProc(2, 0)},
	}
	tasks := buildTasks("", raw, collectedAt)
	assert.Equal(t, "input needed", findTask(t, tasks, "local:claude:waiting").WaitingFor)
	assert.Empty(t, findTask(t, tasks, "local:claude:busy").WaitingFor)
}

// A state file only describes a running agent while its pid belongs to a
// claude process. A leftover file whose pid is gone, or was reused by an
// unrelated process (after a reboot, pids start over), must not be listed as
// running.
func TestBuildTasks_StateFileNeedsALiveClaudeProcess(t *testing.T) {
	cases := map[string]struct {
		procs []process
		live  bool
	}{
		"pid alive and claude": {[]process{claudeProc(121, 1)}, true},
		"claude found by any argv path": {
			[]process{{PID: 121, Command: "node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js"}}, true,
		},
		"claude binary under versions": {
			[]process{{PID: 121, Command: "/workspace/user/.local/share/claude/versions/2.1.282"}}, true,
		},
		"pid gone":                        {[]process{claudeProc(999, 1)}, false},
		"pid reused by unrelated process": {[]process{{PID: 121, Command: "/usr/sbin/sshd -D"}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw := rawSnapshot{
				Now:        hostNow,
				StateFiles: []stateFile{{Name: "121.json", Data: stateJSON(121, "sess-a", "busy")}},
				Processes:  tc.procs,
			}
			tasks := buildTasks("", raw, collectedAt)
			if tc.live {
				require.Len(t, tasks, 1)
				assert.Equal(t, StateBusy, tasks[0].State)
			} else {
				assert.Empty(t, tasks)
			}
		})
	}
}

func TestBuildTasks_UnreadableStateFileIsKeptAsUnknown(t *testing.T) {
	cases := map[string][]byte{
		"not json":           []byte("{"),
		"no session id":      []byte(`{"pid":5}`),
		"invalid session id": []byte(`{"pid":5,"sessionId":"../x"}`),
		"no pid":             []byte(`{"sessionId":"abc"}`),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			raw := rawSnapshot{
				Now:        hostNow,
				StateFiles: []stateFile{{Name: "5.json", Data: data}},
				Processes:  []process{claudeProc(5, 1)},
			}
			tasks := buildTasks("build-box", raw, collectedAt)
			require.Len(t, tasks, 1)
			task := tasks[0]
			assert.Equal(t, StateUnknown, task.State)
			assert.Equal(t, "ssh:build-box:claude:state-file:5.json", task.ID)
			assert.Equal(t, "build-box", task.Host)
			assert.Empty(t, task.SessionID)
			assert.Equal(t, LocationNone, task.Location.Kind)
		})
	}
}

func TestBuildTasks_TwoLiveFilesForOneSessionKeepTheNewest(t *testing.T) {
	older := []byte(`{"pid":10,"sessionId":"same","status":"idle","statusUpdatedAt":1000}`)
	newer := []byte(`{"pid":11,"sessionId":"same","status":"busy","statusUpdatedAt":2000}`)
	for name, files := range map[string][]stateFile{
		"newer last":  {{Name: "10.json", Data: older}, {Name: "11.json", Data: newer}},
		"newer first": {{Name: "11.json", Data: newer}, {Name: "10.json", Data: older}},
	} {
		t.Run(name, func(t *testing.T) {
			raw := rawSnapshot{
				Now:        2,
				StateFiles: files,
				Processes:  []process{claudeProc(10, 1), claudeProc(11, 1)},
			}
			tasks := buildTasks("", raw, collectedAt)
			require.Len(t, tasks, 1)
			assert.Equal(t, 11, tasks[0].PID)
			assert.Equal(t, StateBusy, tasks[0].State)
		})
	}
}

// Two live files for one session updated at the same moment keep the first
// the host listed, so the board does not flip between them.
func TestBuildTasks_ATieBetweenLiveFilesKeepsTheFirstListed(t *testing.T) {
	first := []byte(`{"pid":10,"sessionId":"same","status":"idle","statusUpdatedAt":1000}`)
	second := []byte(`{"pid":11,"sessionId":"same","status":"busy","statusUpdatedAt":1000}`)
	raw := rawSnapshot{
		Now:        2,
		StateFiles: []stateFile{{Name: "10.json", Data: first}, {Name: "11.json", Data: second}},
		Processes:  []process{claudeProc(10, 1), claudeProc(11, 1)},
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, 10, tasks[0].PID)
}

func TestBuildTasks_StatusSinceFallsBackAndIsNeverInTheFuture(t *testing.T) {
	cases := map[string]struct {
		want *time.Time
		data []byte
	}{
		"statusUpdatedAt": {
			data: []byte(`{"pid":1,"sessionId":"s","status":"idle","statusUpdatedAt":1000,"updatedAt":1500,"startedAt":500}`),
			want: ptrTime(collectedAt.Add(-1 * time.Second)),
		},
		"updatedAt when status time is missing": {
			data: []byte(`{"pid":1,"sessionId":"s","status":"idle","updatedAt":1500,"startedAt":500}`),
			want: ptrTime(collectedAt.Add(-500 * time.Millisecond)),
		},
		"startedAt as the last resort": {
			data: []byte(`{"pid":1,"sessionId":"s","status":"idle","startedAt":500}`),
			want: ptrTime(collectedAt.Add(-1500 * time.Millisecond)),
		},
		"no time at all": {
			data: []byte(`{"pid":1,"sessionId":"s","status":"idle"}`),
			want: nil,
		},
		"a host clock behind its own file is clamped to now": {
			data: []byte(`{"pid":1,"sessionId":"s","status":"idle","statusUpdatedAt":9000}`),
			want: ptrTime(collectedAt),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw := rawSnapshot{
				Now:        2, // host clock: 2000 ms
				StateFiles: []stateFile{{Name: "1.json", Data: tc.data}},
				Processes:  []process{claudeProc(1, 0)},
			}
			tasks := buildTasks("", raw, collectedAt)
			require.Len(t, tasks, 1)
			assert.Equal(t, tc.want, tasks[0].StatusSince)
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestBuildTasks_LocationFollowsTheParentChainToATmuxPane(t *testing.T) {
	cases := map[string]struct {
		want  Location
		procs []process
		panes []tmuxPane
	}{
		"agent is the pane process itself": {
			procs: []process{claudeProc(50, 1)},
			panes: []tmuxPane{{PanePID: 50, Session: "task-7c21"}},
			want:  Location{Kind: LocationTmux, TmuxSession: "task-7c21", Attachable: true},
		},
		"agent is a grandchild of the pane shell": {
			procs: []process{{PID: 40, PPID: 1, Command: "-zsh"}, {PID: 45, PPID: 40, Command: "npx"}, claudeProc(50, 45)},
			panes: []tmuxPane{{PanePID: 40, Session: "docs"}},
			want:  Location{Kind: LocationTmux, TmuxSession: "docs", Attachable: true},
		},
		"session name a pane cannot attach to": {
			procs: []process{claudeProc(50, 40), {PID: 40, PPID: 1, Command: "bash"}},
			panes: []tmuxPane{{PanePID: 40, Session: "my work"}},
			want:  Location{Kind: LocationTmux, TmuxSession: "my work", Attachable: false},
		},
		"no tmux ancestor": {
			procs: []process{{PID: 40, PPID: 1, Command: "bash"}, claudeProc(50, 40)},
			panes: []tmuxPane{{PanePID: 41, Session: "other"}},
			want:  Location{Kind: LocationOutside},
		},
		"no tmux at all": {
			procs: []process{claudeProc(50, 1)},
			want:  Location{Kind: LocationOutside},
		},
		"a parent cycle ends the walk": {
			procs: []process{{PID: 40, PPID: 50, Command: "bash"}, claudeProc(50, 40)},
			want:  Location{Kind: LocationOutside},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw := rawSnapshot{
				Now:        hostNow,
				StateFiles: []stateFile{{Name: "50.json", Data: stateJSON(50, "sess", "busy")}},
				Processes:  tc.procs,
				TmuxPanes:  tc.panes,
			}
			tasks := buildTasks("", raw, collectedAt)
			require.Len(t, tasks, 1)
			assert.Equal(t, tc.want, tasks[0].Location)
		})
	}
}

// The walk up the process tree stops after maxParentWalk processes, which is
// far deeper than any real tree; a tmux pane further up than that is not found.
func TestBuildTasks_LocationWalksAtMostMaxParentWalkProcesses(t *testing.T) {
	chain := func(depth int) rawSnapshot {
		// pid 1000 is the agent; each parent is pid+1; the tmux pane's process
		// is `depth` steps above the agent.
		procs := []process{claudeProc(1000, 1001)}
		for i := 1; i < depth; i++ {
			procs = append(procs, process{PID: 1000 + i, PPID: 1001 + i, Command: "sh"})
		}
		procs = append(procs, process{PID: 1000 + depth, PPID: 1, Command: "bash"})
		return rawSnapshot{
			Now:        hostNow,
			StateFiles: []stateFile{{Name: "1000.json", Data: stateJSON(1000, "deep", "busy")}},
			Processes:  procs,
			TmuxPanes:  []tmuxPane{{PanePID: 1000 + depth, Session: "far"}},
		}
	}

	within := buildTasks("", chain(maxParentWalk-1), collectedAt)
	require.Len(t, within, 1)
	assert.Equal(t, LocationTmux, within[0].Location.Kind)

	beyond := buildTasks("", chain(maxParentWalk), collectedAt)
	require.Len(t, beyond, 1)
	assert.Equal(t, LocationOutside, beyond[0].Location.Kind)
}

func TestBuildTasks_CodexProcessesAreRunningTasks(t *testing.T) {
	raw := rawSnapshot{
		Now: hostNow,
		Processes: []process{
			{PID: 30, PPID: 1, Command: "bash"},
			{PID: 31, PPID: 30, Command: "/usr/local/bin/codex --model gpt"},
			{PID: 32, PPID: 30, Command: "codex exec do-something"},
			{PID: 33, PPID: 30, Command: "vim codex"},
		},
		TmuxPanes:   []tmuxPane{{PanePID: 30, Session: "api"}},
		ProcessCWDs: map[int]string{31: "/workspace/user/sample-api"},
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1, "codex exec is headless and vim is not codex")
	task := tasks[0]
	assert.Equal(t, "local:codex:pid-31", task.ID)
	assert.Equal(t, AgentCodex, task.Agent)
	assert.Equal(t, StateRun, task.State)
	assert.Equal(t, 31, task.PID)
	assert.Empty(t, task.SessionID)
	assert.Equal(t, "/workspace/user/sample-api", task.CWD)
	assert.Nil(t, task.StatusSince)
	assert.Equal(t, Location{Kind: LocationTmux, TmuxSession: "api", Attachable: true}, task.Location)
}

func TestBuildTasks_TranscriptsWithoutALiveProcessAreStopped(t *testing.T) {
	raw := rawSnapshot{
		Now:        hostNow,
		StateFiles: []stateFile{{Name: "1.json", Data: stateJSON(1, "live", "busy")}},
		Processes:  []process{claudeProc(1, 0)},
		Transcripts: []transcript{
			{ModTime: hostNow - 60, SessionID: "live", CWD: "/workspace/user/project"},
			{ModTime: hostNow - 7200, SessionID: "stopped", CWD: "/workspace/user/other"},
		},
	}
	tasks := buildTasks("gpu-box", raw, collectedAt)
	require.Len(t, tasks, 2, "a live session is not listed twice")
	assert.Equal(t, StateBusy, findTask(t, tasks, "ssh:gpu-box:claude:live").State)

	stopped := findTask(t, tasks, "ssh:gpu-box:claude:stopped")
	assert.Equal(t, StateStop, stopped.State)
	assert.Equal(t, "stopped", stopped.SessionID)
	assert.Equal(t, "/workspace/user/other", stopped.CWD)
	assert.Zero(t, stopped.PID)
	assert.Equal(t, Location{Kind: LocationNone}, stopped.Location)
	require.NotNil(t, stopped.StatusSince)
	assert.Equal(t, collectedAt.Add(-2*time.Hour), *stopped.StatusSince)
	assert.Nil(t, stopped.StartedAt)
}

func TestBuildTasks_StoppedTasksAreCappedNewestFirst(t *testing.T) {
	var transcripts []transcript
	// Oldest first on input, to show the cap keeps the newest regardless.
	for i := 0; i < maxStoppedTasks+10; i++ {
		transcripts = append(transcripts, transcript{ModTime: int64(i), SessionID: fmt.Sprintf("s%03d", i)})
	}
	tasks := buildTasks("", rawSnapshot{Now: 1000, Transcripts: transcripts}, collectedAt)
	require.Len(t, tasks, maxStoppedTasks)
	assert.Equal(t, fmt.Sprintf("s%03d", maxStoppedTasks+9), tasks[0].SessionID)
	assert.Equal(t, "s010", tasks[len(tasks)-1].SessionID)
}

// Stopped sessions with the same modification time keep the order the host
// listed them in, so the column does not reshuffle between collections.
func TestBuildTasks_StoppedTasksWithEqualTimesKeepTheHostsOrder(t *testing.T) {
	raw := rawSnapshot{Now: 100, Transcripts: []transcript{
		{ModTime: 50, SessionID: "b"},
		{ModTime: 50, SessionID: "a"},
		{ModTime: 50, SessionID: "c"},
	}}
	tasks := buildTasks("", raw, collectedAt)
	ids := []string{}
	for _, task := range tasks {
		ids = append(ids, task.SessionID)
	}
	assert.Equal(t, []string{"b", "a", "c"}, ids)
}

func TestBuildTasks_DuplicateTranscriptsAreListedOnce(t *testing.T) {
	raw := rawSnapshot{Now: 100, Transcripts: []transcript{
		{ModTime: 90, SessionID: "dup", CWD: "/newer"},
		{ModTime: 50, SessionID: "dup", CWD: "/older"},
	}}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, "/newer", tasks[0].CWD)
}

func TestBuildTasks_OrderIsLiveFirstThenStoppedNewestFirst(t *testing.T) {
	raw := rawSnapshot{
		Now: hostNow,
		StateFiles: []stateFile{
			{Name: "2.json", Data: stateJSON(2, "b-live", "idle")},
			{Name: "1.json", Data: stateJSON(1, "a-live", "busy")},
		},
		Processes: []process{claudeProc(1, 0), claudeProc(2, 0), {PID: 3, Command: "codex"}},
		Transcripts: []transcript{
			{ModTime: hostNow - 10, SessionID: "newer-stop"},
			{ModTime: hostNow - 20, SessionID: "older-stop"},
		},
	}
	tasks := buildTasks("", raw, collectedAt)
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	assert.Equal(t, []string{
		"local:claude:a-live",
		"local:claude:b-live",
		"local:codex:pid-3",
		"local:claude:newer-stop",
		"local:claude:older-stop",
	}, ids)
}
