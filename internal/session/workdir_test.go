package session

import (
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
)

// Every agent that declares workdir has an adapter, and no other does.
func TestWorkdirAdaptersMatchDeclarations(t *testing.T) {
	adapters := workdirAdapters()
	for _, d := range agent.All() {
		_, ok := adapters[d.ID]
		assert.Equal(t, d.Has(agent.Workdir), ok, d.ID)
	}
	for id := range adapters {
		_, ok := agent.Lookup(id)
		assert.True(t, ok, id)
	}
}

// A pane finds its agent by the Task rule (issue #375): the rows here are
// the ones the Workdir rule it replaced answered differently.
func TestNewestInteractiveAgentFollowsTheTaskRule(t *testing.T) {
	cases := map[string]bool{
		"node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js": true,
		"/home/u/.local/share/claude/versions/2.1.3":                  true,
		"claude-code":            true,
		"codex -p exec":          true,
		"codex fix the exec bug": true,
		"codex review":           false,
		"codex mcp-server":       false,
		"codex -m x exec":        false,
		"codex -c k=v exec":      false,
	}
	for command, want := range cases {
		processes := []processInfo{
			{PID: 100, PPID: 1, Command: "/bin/zsh"},
			{PID: 120, PPID: 100, Command: command},
		}
		pid, ok := newestInteractiveAgentDescendantPID(processes, 100)
		assert.Equal(t, want, ok, command)
		if want {
			assert.Equal(t, 120, pid, command)
		}
	}
}

// fakeWorkdirHost answers from maps and records what it was asked.
type fakeWorkdirHost struct {
	host        string
	stateFiles  []claudeStateFile
	stateErr    error
	projectDirs []string
	subagents   map[string][]string
	claudeLogs  map[string]string
	openFiles   map[int][]string
	codexLogs   map[string]string
	dirsRead    []string
}

func (h *fakeWorkdirHost) hostKey() string { return h.host }

func (h *fakeWorkdirHost) claudeStateFiles(int) ([]claudeStateFile, error) {
	return h.stateFiles, h.stateErr
}

func (h *fakeWorkdirHost) claudeProjectDirs(string, string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, dir := range h.projectDirs {
			h.dirsRead = append(h.dirsRead, dir)
			if !yield(dir) {
				return
			}
		}
	}
}

func (h *fakeWorkdirHost) claudeSubagentLogs(dir string) ([]string, error) {
	return h.subagents[dir], nil
}

func (h *fakeWorkdirHost) claudeLogCWD(path string) (string, error) {
	return h.claudeLogs[path], nil
}

func (h *fakeWorkdirHost) agentOpenFiles(pid int) ([]string, error) {
	files, ok := h.openFiles[pid]
	if !ok {
		return nil, errors.New("no such process")
	}
	return files, nil
}

func (h *fakeWorkdirHost) codexLogCWD(path string) (string, error) {
	return h.codexLogs[path], nil
}

func TestClaudeWorkdir_LocateReadsTheStateFileForThePID(t *testing.T) {
	h := &fakeWorkdirHost{host: "devbox", stateFiles: []claudeStateFile{
		{name: "41.json", data: []byte(`{"pid":41,"sessionId":"other","cwd":"/x"}`)},
		{name: "bad.json", data: []byte(`{`)},
		// An optional field of another type no longer hides the session.
		{name: "42.json", data: []byte(`{"pid":42,"sessionId":"s-1","cwd":"/workspace/user/project","status":5}`)},
	}}

	got, ok, err := claudeWorkdir{}.locate(h, 42)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, WorkdirSession{
		Key: agent.SessionKey{Host: "devbox", Agent: agent.Claude, SessionID: "s-1"},
		PID: 42, CWD: "/workspace/user/project",
	}, got)
}

func TestClaudeWorkdir_LocateFindsNothingUnusable(t *testing.T) {
	cases := map[string]*fakeWorkdirHost{
		"no file":        {},
		"another pid":    {stateFiles: []claudeStateFile{{data: []byte(`{"pid":41,"sessionId":"s","cwd":"/x"}`)}}},
		"no cwd":         {stateFiles: []claudeStateFile{{data: []byte(`{"pid":42,"sessionId":"s"}`)}}},
		"bad session id": {stateFiles: []claudeStateFile{{data: []byte(`{"pid":42,"sessionId":"a/b","cwd":"/x"}`)}}},
	}
	for name, h := range cases {
		_, ok, err := claudeWorkdir{}.locate(h, 42)
		require.NoError(t, err, name)
		assert.False(t, ok, name)
	}

	_, ok, err := claudeWorkdir{}.locate(&fakeWorkdirHost{stateErr: errors.New("cat failed")}, 42)
	assert.Error(t, err)
	assert.False(t, ok)
}

// The parent transcript and every subagent transcript beside it are read, a
// directory is given up for the next only when nothing under it resolves,
// and each workdir is listed once.
func TestClaudeWorkdir_WorkdirsReadsTheTranscriptsUnderTheFirstDirectoryThatResolves(t *testing.T) {
	h := &fakeWorkdirHost{
		projectDirs: []string{"/p/derived", "/p/probed", "/p/never"},
		subagents:   map[string][]string{"/p/probed/s-1/subagents": {"a.jsonl", "b.jsonl", "c.jsonl"}},
		claudeLogs: map[string]string{
			"/p/probed/s-1.jsonl":             "/repo/main",
			"/p/probed/s-1/subagents/a.jsonl": "/repo/wt-a",
			"/p/probed/s-1/subagents/b.jsonl": "/repo/main",
		},
	}
	s := WorkdirSession{Key: agent.SessionKey{Agent: agent.Claude, SessionID: "s-1"}, CWD: "/repo/main"}

	got, err := claudeWorkdir{}.workdirs(h, s)

	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/wt-a"}, got)
	assert.Equal(t, []string{"/p/derived", "/p/probed"}, h.dirsRead)
}

func TestClaudeWorkdir_WorkdirsNeedsASessionIDAndCWD(t *testing.T) {
	h := &fakeWorkdirHost{projectDirs: []string{"/p"}, claudeLogs: map[string]string{"/p/s.jsonl": "/repo"}}
	for name, s := range map[string]WorkdirSession{
		"no session id":  {Key: agent.SessionKey{Agent: agent.Claude}, CWD: "/repo"},
		"bad session id": {Key: agent.SessionKey{Agent: agent.Claude, SessionID: "../s"}, CWD: "/repo"},
		"no cwd":         {Key: agent.SessionKey{Agent: agent.Claude, SessionID: "s"}},
	} {
		got, err := claudeWorkdir{}.workdirs(h, s)
		require.NoError(t, err, name)
		assert.Empty(t, got, name)
	}
	assert.Empty(t, h.dirsRead)
}

func TestCodexWorkdir_LocateFindsTheRolloutTheProcessHasOpen(t *testing.T) {
	rollout := "/home/u/.codex/sessions/2026/10/18/rollout-2026-10-18T10-00-00-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl"
	h := &fakeWorkdirHost{host: "devbox", openFiles: map[int][]string{7: {"/dev/null", rollout}}}

	got, ok, err := codexWorkdir{}.locate(h, 7)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, WorkdirSession{
		Key: agent.SessionKey{Host: "devbox", Agent: agent.Codex, SessionID: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"},
		PID: 7, Log: rollout,
	}, got)

	_, ok, err = codexWorkdir{}.locate(&fakeWorkdirHost{openFiles: map[int][]string{7: {"/dev/null"}}}, 7)
	require.NoError(t, err)
	assert.False(t, ok, "no rollout open")
	_, _, err = codexWorkdir{}.locate(&fakeWorkdirHost{}, 7)
	assert.Error(t, err)
}

// A session built from a task carries no log; codex finds it by the PID.
func TestCodexWorkdir_WorkdirsReadsTheRollout(t *testing.T) {
	rollout := "/home/u/.codex/sessions/2026/10/18/rollout-x.jsonl"
	h := &fakeWorkdirHost{
		openFiles: map[int][]string{7: {rollout}},
		codexLogs: map[string]string{rollout: "/repo/wt"},
	}

	got, err := codexWorkdir{}.workdirs(h, WorkdirSession{PID: 7, Log: rollout})
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/wt"}, got)

	got, err = codexWorkdir{}.workdirs(h, WorkdirSession{PID: 7})
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/wt"}, got)

	got, err = codexWorkdir{}.workdirs(&fakeWorkdirHost{openFiles: map[int][]string{7: nil}}, WorkdirSession{PID: 7})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// resolveSessionWorkdirs is the pane's path through the capability: locate, then
// workdirs; an agent with no adapter resolves nothing.
func TestResolveSessionWorkdirs(t *testing.T) {
	h := &fakeWorkdirHost{
		stateFiles:  []claudeStateFile{{data: []byte(`{"pid":42,"sessionId":"s-1","cwd":"/repo"}`)}},
		projectDirs: []string{"/p"},
		claudeLogs:  map[string]string{"/p/s-1.jsonl": "/repo/wt"},
	}
	got, err := resolveSessionWorkdirs(h, agent.Claude, 42)
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/wt"}, got)

	got, err = resolveSessionWorkdirs(h, agent.Gemini, 42)
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = resolveSessionWorkdirs(&fakeWorkdirHost{}, agent.Claude, 42)
	require.NoError(t, err)
	assert.Empty(t, got)
}
