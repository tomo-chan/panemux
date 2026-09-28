package tasks

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codexProcElapsed is how long the codex process in these tests has run:
// it started at hostNow-600.
const codexProcElapsed int64 = 600

func codexProc(pid int) process {
	return process{PID: pid, PPID: 30, Command: "/opt/tools/bin/codex -c check_for_update_on_startup=false"}
}

// openRollout is session sessionID's rollout, held open by pid, last written
// age seconds before the host's clock.
func openRollout(pid int, sessionID string, age int64, turn codexTurn) codexOpenRollout {
	return codexOpenRollout{
		PID:     pid,
		Elapsed: codexProcElapsed,
		File:    rolloutName("2026-09-27T11-57-03", sessionID),
		Rollout: codexRollout{SessionID: sessionID, CWD: "/workspace/user/api", ModTime: hostNow - age, Size: 100},
		Turn:    turn,
	}
}

func codexRaw(open ...codexOpenRollout) rawSnapshot {
	return rawSnapshot{
		Now:         hostNow,
		Processes:   []process{{PID: 30, PPID: 1, Command: "bash"}, codexProc(31)},
		TmuxPanes:   []tmuxPane{{PanePID: 30, Session: "task-a5e25ebc"}},
		ProcessCWDs: map[int]string{31: "/workspace/user/process-cwd"},
		CodexAges:   map[int]int64{31: codexProcElapsed},
		CodexOpen:   open,
	}
}

func inProgressAt(startedAt int64) codexTurn {
	return codexTurn{DBStatus: "inProgress", DBStartedAt: startedAt}
}

func hostAgo(seconds int64) *time.Time {
	return ptrTime(collectedAt.Add(-time.Duration(seconds) * time.Second))
}

// The newest thread_turns row decides the state, as long as the process is
// alive: a turn in progress is busy (approval prompts included, which codex
// does not record), and a finished, interrupted or failed one is idle.
func TestBuildTasks_CodexStateFromThreadTurns(t *testing.T) {
	cases := []struct {
		wantSince *time.Time
		status    string
		want      State
	}{
		{status: "inProgress", want: StateBusy, wantSince: hostAgo(120)},
		{status: "completed", want: StateIdle, wantSince: hostAgo(30)},
		{status: "interrupted", want: StateIdle, wantSince: hostAgo(30)},
		{status: "failed", want: StateIdle, wantSince: hostAgo(30)},
	}
	for _, tt := range cases {
		t.Run(tt.status, func(t *testing.T) {
			turn := codexTurn{
				DBStatus: tt.status, DBStartedAt: hostNow - 120, LastItem: "function_call", LastItemName: "exec_command",
			}
			tasks := buildTasks("", codexRaw(openRollout(31, codexSessionA, 30, turn)), collectedAt)
			require.Len(t, tasks, 1)
			task := tasks[0]
			assert.Equal(t, "local:codex:"+codexSessionA, task.ID)
			assert.Equal(t, AgentCodex, task.Agent)
			assert.Equal(t, codexSessionA, task.SessionID)
			assert.Equal(t, tt.want, task.State)
			assert.Equal(t, 31, task.PID)
			assert.Equal(t, "/workspace/user/api", task.CWD, "the session's own cwd wins over the process's")
			assert.Equal(t, tt.wantSince, task.StatusSince)
			assert.Equal(t, hostAgo(codexProcElapsed), task.StartedAt)
			assert.Equal(t, Location{Kind: LocationTmux, TmuxSession: "task-a5e25ebc", Attachable: true}, task.Location)
			assert.Empty(t, task.WaitingFor)
			assert.Nil(t, task.Log, "codex tasks are not summarized")
		})
	}
}

// Without a thread_turns row (no sqlite3 on the host, a codex older than
// 0.157), the last turn event in the rollout's tail decides.
func TestBuildTasks_CodexStateFromTheRolloutWhenTheDatabaseHasNothing(t *testing.T) {
	startedAt := (hostNow - 90) * 1000
	cases := []struct {
		name string
		want State
		turn codexTurn
	}{
		{name: "turn started", turn: codexTurn{Event: "task_started", EventAt: startedAt}, want: StateBusy},
		{name: "turn complete", turn: codexTurn{Event: "task_complete", EventAt: startedAt}, want: StateIdle},
		{name: "turn aborted", turn: codexTurn{Event: "turn_aborted", EventAt: startedAt}, want: StateIdle},
		{name: "no turn event in the tail", turn: codexTurn{LastItem: "message"}, want: StateUnknown},
		{
			name: "a thread_turns status this build does not know falls back to the rollout",
			turn: codexTurn{DBStatus: "paused", DBStartedAt: hostNow - 90, Event: "task_complete", EventAt: startedAt},
			want: StateIdle,
		},
		{name: "an unknown status and no event", turn: codexTurn{DBStatus: "paused"}, want: StateUnknown},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tasks := buildTasks("", codexRaw(openRollout(31, codexSessionA, 30, tt.turn)), collectedAt)
			require.Len(t, tasks, 1)
			assert.Equal(t, tt.want, tasks[0].State)
			assert.Equal(t, codexSessionA, tasks[0].SessionID)
		})
	}

	started := openRollout(31, codexSessionA, 30, codexTurn{Event: "task_started", EventAt: startedAt})
	busy := buildTasks("", codexRaw(started), collectedAt)[0]
	assert.Equal(t, hostAgo(90), busy.StatusSince, "a busy task is busy since its turn started")
}

// A turn waiting on request_user_input has the call as the rollout's last
// response item, with no output after it.
func TestBuildTasks_CodexWaitsOnRequestUserInput(t *testing.T) {
	turn := codexTurn{
		DBStatus: "inProgress", DBStartedAt: hostNow - 120,
		Event: "task_started", EventAt: (hostNow - 120) * 1000,
		LastItem: "function_call", LastItemName: "request_user_input",
	}
	task := buildTasks("", codexRaw(openRollout(31, codexSessionA, 45, turn)), collectedAt)[0]
	assert.Equal(t, StateWait, task.State)
	assert.Equal(t, codexWaitingForQuestion, task.WaitingFor)
	assert.Equal(t, hostAgo(45), task.StatusSince, "waiting since the call was written")

	for _, other := range []codexTurn{
		{DBStatus: "inProgress", DBStartedAt: hostNow - 120, LastItem: "function_call_output"},
		{
			DBStatus: "inProgress", DBStartedAt: hostNow - 120,
			LastItem: "custom_tool_call", LastItemName: "request_user_input",
		},
		{DBStatus: "completed", DBStartedAt: hostNow - 120, LastItem: "function_call", LastItemName: "request_user_input"},
	} {
		task := buildTasks("", codexRaw(openRollout(31, codexSessionA, 45, other)), collectedAt)[0]
		assert.NotEqual(t, StateWait, task.State, "%+v", other)
		assert.Empty(t, task.WaitingFor)
	}
}

// A codex killed mid-turn leaves that turn inProgress for good, and a
// `codex resume` that sends nothing writes no new turn. A turn that started
// before the process did is that leftover: the session is idle.
func TestBuildTasks_CodexTurnOlderThanItsProcessIsALeftover(t *testing.T) {
	procStart := hostNow - codexProcElapsed
	cases := []struct {
		name string
		want State
		turn codexTurn
	}{
		{name: "db turn well before the process", turn: inProgressAt(procStart - 60), want: StateIdle},
		{name: "db turn three seconds before", turn: inProgressAt(procStart - 3), want: StateIdle},
		{name: "db turn within the two-second margin", turn: inProgressAt(procStart - 2), want: StateBusy},
		{name: "db turn after the process started", turn: inProgressAt(procStart + 1), want: StateBusy},
		{
			name: "rollout turn before the process",
			turn: codexTurn{Event: "task_started", EventAt: (procStart - 60) * 1000},
			want: StateIdle,
		},
		{
			name: "a leftover waiting on a question is not waiting either",
			turn: codexTurn{
				DBStatus: "inProgress", DBStartedAt: procStart - 60,
				LastItem: "function_call", LastItemName: "request_user_input",
			},
			want: StateIdle,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			task := buildTasks("", codexRaw(openRollout(31, codexSessionA, 30, tt.turn)), collectedAt)[0]
			assert.Equal(t, tt.want, task.State)
		})
	}

	unknownAge := openRollout(31, codexSessionA, 30, codexTurn{DBStatus: "inProgress", DBStartedAt: procStart - 60})
	unknownAge.Elapsed = -1
	task := buildTasks("", codexRaw(unknownAge), collectedAt)[0]
	assert.Equal(t, StateBusy, task.State, "without the process's age a turn cannot be told to be a leftover")
	assert.Nil(t, task.StartedAt)
}

// After /new or a /resume inside the TUI a process holds two rollouts open.
// The one written last is the session it is in; the other one is neither
// that task nor a stopped one while the process holds it.
func TestBuildTasks_CodexProcessWithTwoRolloutsIsInTheNewestOne(t *testing.T) {
	older := openRollout(31, codexSessionA, 300, codexTurn{DBStatus: "completed"})
	newer := openRollout(31, codexSessionB, 20, codexTurn{DBStatus: "inProgress", DBStartedAt: hostNow - 20})
	newer.File = rolloutName("2026-09-27T12-00-15", codexSessionB)
	raw := codexRaw(newer, older)
	raw.CodexRollouts = []codexRollout{
		{SessionID: codexSessionB, Originator: "codex-tui", ModTime: hostNow - 20},
		{SessionID: codexSessionA, Originator: "codex-tui", ModTime: hostNow - 300},
	}

	for _, order := range [][]codexOpenRollout{{newer, older}, {older, newer}} {
		raw.CodexOpen = order
		tasks := buildTasks("", raw, collectedAt)
		require.Len(t, tasks, 1, "the session switched away from is not listed as stopped: %+v", tasks)
		assert.Equal(t, codexSessionB, tasks[0].SessionID)
		assert.Equal(t, StateBusy, tasks[0].State)
	}
}

// Two rollouts written in the same second: the one codex created later (its
// file name carries its creation time) is the current one.
func TestBuildTasks_CodexRolloutTieGoesToTheLaterCreated(t *testing.T) {
	first := openRollout(31, codexSessionA, 20, codexTurn{DBStatus: "completed"})
	second := openRollout(31, codexSessionB, 20, codexTurn{DBStatus: "completed"})
	second.File = rolloutName("2026-09-27T12-00-15", codexSessionB)
	for _, order := range [][]codexOpenRollout{{first, second}, {second, first}} {
		tasks := buildTasks("", codexRaw(order...), collectedAt)
		require.Len(t, tasks, 1)
		assert.Equal(t, codexSessionB, tasks[0].SessionID)
	}
}

// Before its first instruction — at the composer, or held at a start-up
// screen (trusting the directory, a model notice) — codex has no rollout:
// the task is the process, running, with no session.
// A process that has run for less than a second has an age of 0, which is
// still an age: it started now.
func TestBuildTasks_CodexProcessStartedThisSecond(t *testing.T) {
	justStarted := openRollout(31, codexSessionA, 0, codexTurn{DBStatus: "inProgress", DBStartedAt: hostNow})
	justStarted.Elapsed = 0
	task := buildTasks("", codexRaw(justStarted), collectedAt)[0]
	assert.Equal(t, hostAgo(0), task.StartedAt)
	assert.Equal(t, StateBusy, task.State)
}

func TestBuildTasks_CodexWithoutARolloutIsARunningProcess(t *testing.T) {
	tasks := buildTasks("", codexRaw(), collectedAt)
	require.Len(t, tasks, 1)
	task := tasks[0]
	assert.Equal(t, "local:codex:pid-31", task.ID)
	assert.Equal(t, StateRun, task.State)
	assert.Empty(t, task.SessionID)
	assert.Equal(t, "/workspace/user/process-cwd", task.CWD)
	assert.Equal(t, hostAgo(codexProcElapsed), task.StartedAt)
}

// A rollout held by a process that is not an interactive codex (codex exec,
// the app-server) makes no task and is not a stopped one either.
func TestBuildTasks_RolloutsHeldByOtherCodexProcesses(t *testing.T) {
	raw := codexRaw(openRollout(40, codexSessionA, 30, codexTurn{DBStatus: "inProgress"}))
	raw.Processes = append(raw.Processes, process{PID: 40, PPID: 1, Command: "codex exec fix-it"})
	raw.CodexRollouts = []codexRollout{{SessionID: codexSessionA, Originator: "codex-tui", ModTime: hostNow - 30}}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, "local:codex:pid-31", tasks[0].ID)
}

// A rollout held by a pid that is not running any more (it exited between
// the fd probe and ps) belongs to no task, and its session is stopped.
func TestBuildTasks_RolloutOfAProcessThatIsGoneIsStopped(t *testing.T) {
	raw := codexRaw(openRollout(99, codexSessionA, 30, codexTurn{DBStatus: "inProgress"}))
	raw.CodexRollouts = []codexRollout{
		{SessionID: codexSessionA, Originator: "codex-tui", CWD: "/workspace/user/api", ModTime: hostNow - 30},
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 2)
	assert.Equal(t, StateStop, findTask(t, tasks, "local:codex:"+codexSessionA).State)
}

func TestBuildTasks_TwoProcessesInOneCodexSessionAreListedOnce(t *testing.T) {
	raw := codexRaw(
		openRollout(31, codexSessionA, 30, codexTurn{DBStatus: "completed"}),
		openRollout(32, codexSessionA, 30, codexTurn{DBStatus: "completed"}),
	)
	raw.Processes = append(raw.Processes, codexProc(32))
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, 31, tasks[0].PID, "the first listed process keeps the session")
}

// Stopped codex sessions are the TUI's (originator "codex-tui"), whether the
// TUI wrote its rollout itself or through codex's shared daemon: codex exec
// and the desktop app write rollouts too.
func TestBuildTasks_StoppedCodexSessions(t *testing.T) {
	raw := rawSnapshot{
		Now: hostNow,
		CodexRollouts: []codexRollout{
			{SessionID: codexSessionA, CWD: "/workspace/user/api", Originator: "codex-tui", ModTime: hostNow - 3600, Size: 9},
			{SessionID: codexSessionB, Originator: "codex_exec", ModTime: hostNow - 60},
			{SessionID: "01a0e2bd-ce26-7d81-a280-90c4de0d0046", Originator: "", ModTime: hostNow - 60},
			{SessionID: codexSessionA, CWD: "/older", Originator: "codex-tui", ModTime: hostNow - 7200},
		},
	}
	tasks := buildTasks("build-box", raw, collectedAt)
	require.Len(t, tasks, 1)
	task := tasks[0]
	assert.Equal(t, "ssh:build-box:codex:"+codexSessionA, task.ID)
	assert.Equal(t, AgentCodex, task.Agent)
	assert.Equal(t, StateStop, task.State)
	assert.Equal(t, "/workspace/user/api", task.CWD)
	assert.Equal(t, hostAgo(3600), task.StatusSince)
	assert.Equal(t, Location{Kind: LocationNone}, task.Location)
	assert.Nil(t, task.Log)
}

// The 50 stopped tasks a host lists are the newest of claude's and codex's
// together.
// codexDaemon is codex's shared app-server daemon, started by the TUI with
// pid 31, which it outlives.
func codexDaemon(pid int) process {
	return process{
		PID: pid, PPID: 31,
		Command: "/remote/home/demo/.codex/packages/app-server-daemon/releases/0.157.1/bin/codex " +
			"app-server --listen unix:// --managed-daemon",
	}
}

// A TUI started without -c or --no-daemon runs its session in codex's shared
// daemon, which holds the rollout; nothing ties the TUI to it. The session is
// a task of its own, whose state is read as any other's but which cannot be
// opened in a pane, and the TUI stays a process with no session.
func TestBuildTasks_ASessionTheCodexDaemonHoldsIsATaskOfItsOwn(t *testing.T) {
	held := openRollout(50, codexSessionA, 30, codexTurn{DBStatus: "inProgress", DBStartedAt: hostNow - 60})
	held.Rollout.Originator = codexOriginatorTUI
	held.Elapsed = 900
	other := openRollout(50, codexSessionB, 30, codexTurn{DBStatus: "completed"})
	other.Rollout.Originator = "Codex Desktop"
	raw := codexRaw(held, other)
	raw.Processes = append(raw.Processes, codexDaemon(50))
	raw.CodexRollouts = []codexRollout{
		{SessionID: codexSessionA, Originator: codexOriginatorTUI, ModTime: hostNow - 30},
		{SessionID: codexSessionB, Originator: codexOriginatorTUI, ModTime: hostNow - 30},
	}

	tasks := buildTasks("", raw, collectedAt)

	require.Len(t, tasks, 2,
		"the TUI, and the daemon's TUI session; nothing is stopped while the daemon holds it: %+v", tasks)
	session := findTask(t, tasks, "local:codex:"+codexSessionA)
	assert.Equal(t, StateBusy, session.State)
	assert.Equal(t, 50, session.PID)
	assert.Equal(t, Location{Kind: LocationDaemon}, session.Location,
		"not the tmux pane of the TUI that started the daemon")
	assert.Equal(t, hostAgo(900), session.StartedAt)
	tui := findTask(t, tasks, "local:codex:pid-31")
	assert.Equal(t, StateRun, tui.State)
}

// A session the TUI holds itself is that TUI's task, even should the daemon
// hold it too.
func TestBuildTasks_ATUIsOwnSessionIsNotListedAgainForTheDaemon(t *testing.T) {
	own := openRollout(31, codexSessionA, 30, codexTurn{DBStatus: "completed"})
	daemon := openRollout(50, codexSessionA, 30, codexTurn{DBStatus: "completed"})
	daemon.Rollout.Originator = codexOriginatorTUI
	for _, order := range [][]codexOpenRollout{{own, daemon}, {daemon, own}} {
		raw := codexRaw(order...)
		raw.Processes = append(raw.Processes, codexDaemon(50))
		tasks := buildTasks("", raw, collectedAt)
		require.Len(t, tasks, 1)
		assert.Equal(t, 31, tasks[0].PID)
	}
}

func TestIsCodexDaemon(t *testing.T) {
	assert.True(t, isCodexDaemon(codexDaemon(1).Command))
	assert.True(t, isCodexDaemon("codex -c x=y app-server"))
	assert.False(t, isCodexDaemon("codex exec app-server"))
	assert.False(t, isCodexDaemon("codex"))
	assert.False(t, isCodexDaemon("/usr/bin/app-server codex"))
}

func TestBuildTasks_StoppedClaudeAndCodexShareTheCap(t *testing.T) {
	raw := rawSnapshot{Now: 10_000}
	for i := 0; i < maxStoppedTasks; i++ {
		raw.Transcripts = append(raw.Transcripts, transcript{ModTime: int64(2 * i), SessionID: fmt.Sprintf("c%03d", i)})
		raw.CodexRollouts = append(raw.CodexRollouts, codexRollout{
			SessionID: fmt.Sprintf("01a0e2b9-d054-7cc2-9278-%012d", i), Originator: "codex-tui", ModTime: int64(2*i + 1),
		})
	}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, maxStoppedTasks)
	assert.Equal(t, "local:codex:01a0e2b9-d054-7cc2-9278-000000000049", tasks[0].ID, "newest first across agents")
	assert.Equal(t, "local:claude:c049", tasks[1].ID)
	assert.Equal(t, "local:claude:c025", tasks[len(tasks)-1].ID)
}
