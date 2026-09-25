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
		"pid gone":                        {[]process{{PID: 999, PPID: 1, Command: "bash"}}, false},
		"pid reused by unrelated process": {[]process{{PID: 121, Command: "/usr/sbin/sshd -D"}}, false},
		"pid reused by a process reading a file under ~/.claude": {
			[]process{{PID: 121, Command: "less /workspace/user/.claude/projects/x/sess-a.jsonl"}}, false,
		},
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

// A state file the dashboard cannot read is still evidence of a running
// agent when the pid in its name is a live claude process: it is shown as
// unknown, located like any running task, rather than dropped.
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
				Now:         hostNow,
				StateFiles:  []stateFile{{Name: "5.json", Data: data}},
				Processes:   []process{claudeProc(5, 4), {PID: 4, PPID: 1, Command: "bash"}},
				TmuxPanes:   []tmuxPane{{PanePID: 4, Session: "work"}},
				ProcessCWDs: map[int]string{5: "/workspace/user/project"},
			}
			tasks := buildTasks("build-box", raw, collectedAt)
			require.Len(t, tasks, 1)
			task := tasks[0]
			assert.Equal(t, StateUnknown, task.State)
			assert.Equal(t, "ssh:build-box:claude:state-file:5.json", task.ID)
			assert.Equal(t, "build-box", task.Host)
			assert.Empty(t, task.SessionID)
			assert.Equal(t, 5, task.PID)
			assert.Equal(t, "/workspace/user/project", task.CWD)
			assert.Equal(t, Location{Kind: LocationTmux, TmuxSession: "work", Attachable: true}, task.Location)
		})
	}
}

// The file that loses a tie between two live files for one session still
// describes its process, which is therefore not listed again as a claude
// process that no state file names — even when the file is not named after
// its pid.
func TestBuildTasks_ALosingStateFileStillExplainsItsProcess(t *testing.T) {
	raw := rawSnapshot{
		Now: 2,
		StateFiles: []stateFile{
			{Name: "a.json", Data: []byte(`{"pid":10,"sessionId":"same","status":"idle","statusUpdatedAt":1000}`)},
			{Name: "b.json", Data: []byte(`{"pid":11,"sessionId":"same","status":"busy","statusUpdatedAt":2000}`)},
		},
		Processes: []process{claudeProc(10, 1), claudeProc(11, 1)},
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, "local:claude:same", tasks[0].ID)
}

func TestBuildTasks_UnreadableStateFileWithoutALiveProcess(t *testing.T) {
	t.Run("a dead pid in its name is a leftover and is dropped", func(t *testing.T) {
		raw := rawSnapshot{Now: hostNow, StateFiles: []stateFile{{Name: "5.json", Data: []byte("{")}}}
		assert.Empty(t, buildTasks("", raw, collectedAt))
	})
	for _, name := range []string{"odd.json", "0.json", "7.txt"} {
		t.Run("a name with no pid cannot be checked and is kept: "+name, func(t *testing.T) {
			raw := rawSnapshot{Now: hostNow, StateFiles: []stateFile{{Name: name, Data: []byte("{")}}}
			tasks := buildTasks("", raw, collectedAt)
			require.Len(t, tasks, 1)
			assert.Equal(t, "local:claude:state-file:"+name, tasks[0].ID)
			assert.Equal(t, LocationNone, tasks[0].Location.Kind)
			assert.Zero(t, tasks[0].PID)
		})
	}
}

// A running claude process that no state file describes — the files moved
// or stopped being written — is shown as running with an unknown state, not
// as stopped, and the conversation log it is most likely writing (the
// newest one in its directory) is not listed as a second, stopped card.
func TestBuildTasks_ClaudeProcessWithoutAStateFileIsUnknownNotStopped(t *testing.T) {
	raw := rawSnapshot{
		Now: hostNow,
		Processes: []process{
			{PID: 6, PPID: 1, Command: "bash"},
			claudeProc(7, 6),
			{PID: 8, PPID: 6, Command: "claude -p summarize"},
		},
		TmuxPanes:   []tmuxPane{{PanePID: 6, Session: "work"}},
		ProcessCWDs: map[int]string{7: "/workspace/user/project", 8: "/workspace/user/project"},
		Transcripts: []transcript{
			{ModTime: hostNow - 10, SessionID: "current", CWD: "/workspace/user/project"},
			{ModTime: hostNow - 3600, SessionID: "earlier", CWD: "/workspace/user/project"},
			{ModTime: hostNow - 20, SessionID: "elsewhere", CWD: "/workspace/user/other"},
		},
	}
	tasks := buildTasks("", raw, collectedAt)

	ids := []string{}
	for _, task := range tasks {
		ids = append(ids, task.ID+"="+string(task.State))
	}
	assert.Equal(t, []string{
		"local:claude:pid-7=unknown",
		"local:claude:elsewhere=stop",
		"local:claude:earlier=stop",
	}, ids, "claude -p is headless and not a task; the newest log in pid 7's directory is its own")

	orphan := findTask(t, tasks, "local:claude:pid-7")
	assert.Equal(t, 7, orphan.PID)
	assert.Equal(t, "/workspace/user/project", orphan.CWD)
	assert.Equal(t, Location{Kind: LocationTmux, TmuxSession: "work", Attachable: true}, orphan.Location)
}

// The review's second case: a session whose state file is corrupt is one
// card, not an unknown card plus a stopped one.
func TestBuildTasks_UnreadableStateFileClaimsItsNewestLog(t *testing.T) {
	raw := rawSnapshot{
		Now:         hostNow,
		StateFiles:  []stateFile{{Name: "7.json", Data: []byte("{")}},
		Processes:   []process{claudeProc(7, 1)},
		ProcessCWDs: map[int]string{7: "/workspace/user/project"},
		Transcripts: []transcript{{ModTime: hostNow - 10, SessionID: "abc", CWD: "/workspace/user/project"}},
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, "local:claude:state-file:7.json", tasks[0].ID)
}

func TestIsClaudeProcess(t *testing.T) {
	cases := map[string]bool{
		"claude":                          true,
		"claude --resume abc":             true,
		"/usr/local/bin/claude --verbose": true,
		"/workspace/user/.local/share/claude/versions/2.1.282":        true,
		"node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js": true,
		"/usr/bin/bun /opt/claude-code/cli.js":                        true,
		"less /workspace/user/.claude/projects/x/abc.jsonl":           false,
		"tail -f /workspace/user/.claude/sessions/7.json":             false,
		"vim claude":                         false,
		"/usr/bin/claude-notes":              false,
		"node /workspace/user/app/server.js": false,
		"node":                               false,
		"":                                   false,
	}
	for command, want := range cases {
		assert.Equal(t, want, isClaudeProcess(command), command)
	}
}

// Observed on macOS: `/resume` keeps the process and its <pid>.json, and
// rewrites the file's sessionId to the resumed session. The session switched
// away from then has a log and no live file, so it is stopped; the resumed one
// runs where the process runs.
func TestBuildTasks_ResumeSwitchesTheTaskInPlace(t *testing.T) {
	raw := rawSnapshot{
		Now:        hostNow,
		StateFiles: []stateFile{{Name: "48471.json", Data: stateJSON(48471, "resumed", "idle")}},
		Processes:  []process{{PID: 48000, PPID: 1, Command: "-zsh"}, {PID: 48471, PPID: 48000, Command: "claude"}},
		TmuxPanes:  []tmuxPane{{PanePID: 48000, Session: "work"}},
		Transcripts: []transcript{
			{ModTime: hostNow - 5, SessionID: "resumed", CWD: "/workspace/user/project"},
			{ModTime: hostNow - 60, SessionID: "started-with", CWD: "/workspace/user/project"},
		},
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 2)

	resumed := findTask(t, tasks, "local:claude:resumed")
	assert.Equal(t, StateIdle, resumed.State)
	assert.Equal(t, 48471, resumed.PID)
	assert.Equal(t, Location{Kind: LocationTmux, TmuxSession: "work", Attachable: true}, resumed.Location)

	assert.Equal(t, StateStop, findTask(t, tasks, "local:claude:started-with").State)
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

// Checked against `codex --help` of codex-cli 0.157.0: with no subcommand the
// arguments start the interactive TUI (a prompt included), `resume` and
// `fork` reopen an interactive session, and every other subcommand is not
// interactive. Options that take a value are skipped with their value.
func TestIsInteractiveCodex(t *testing.T) {
	cases := map[string]bool{
		"codex":                               true,
		"/usr/local/bin/codex --model gpt-5":  true,
		"codex please exec the migration":     true,
		"codex resume --last":                 true,
		"codex fork":                          true,
		"codex -m o3 exec":                    false,
		"codex -c model=o3 app-server":        false,
		"codex --config=model=o3 exec":        false,
		"codex -C /workspace/user/api resume": true,
		"codex --search review":               false,
		"codex -- exec":                       true,
		"codex exec do-something":             false,
		"codex e do-something":                false,
		"/usr/local/bin/codex app-server":     false,
		"codex mcp-server":                    false,
		"codex mcp list":                      false,
		"codex login":                         false,
		"vim codex":                           false,
		"node /usr/lib/node_modules/@openai/codex/bin/codex.js": false,
	}
	for command, want := range cases {
		assert.Equal(t, want, isInteractiveCodex(command), command)
	}
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
