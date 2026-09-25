package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// taskCollection renders one host's collection output with a single busy
// claude session in cwd.
func taskCollection(sessionID, cwd string) []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"` + sessionID + `","cwd":"` + cwd + `","status":"waiting",` +
			`"waitingFor":"input needed","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::section tmux",
		"1 task-7c21",
		"::section cwd",
		"::section transcripts",
		"::end",
	}, "\n") + "\n")
}

type stubTaskConn struct {
	gitErr  error
	output  []byte
	gitCWDs []string
	mu      sync.Mutex
}

func (c *stubTaskConn) Run(context.Context, string, io.Reader) ([]byte, error) { return c.output, nil }

func (c *stubTaskConn) InspectGitContext(_ context.Context, cwd string) (session.GitContext, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gitCWDs = append(c.gitCWDs, cwd)
	if c.gitErr != nil {
		return session.GitContext{}, c.gitErr
	}
	return session.GitContext{
		Root: cwd, Branch: "PAY-418-retry-backoff", Repo: "payment",
		OriginURL: "git@github.com:example-org/payment.git",
	}, nil
}

func (c *stubTaskConn) Close() error { return nil }

func (c *stubTaskConn) Ping(context.Context) error { return nil }

// useTaskService swaps the handler's collector for one whose local output
// and remote connections the test controls.
func useTaskService(h *Handler, local []byte, conns map[string]tasks.Conn) {
	h.tasks = tasks.New(tasks.Options{
		Hosts: h.taskHostNames,
		Dial: func(name string) (tasks.Conn, error) {
			if conn, ok := conns[name]; ok {
				return conn, nil
			}
			return nil, errors.New("i/o timeout")
		},
		RunLocal: func(context.Context, string) ([]byte, error) { return local, nil },
	})
}

// newTaskServiceWithLocal is the production collector — the real hosts and
// the real dialer — with only the panemux host's own collection canned.
func newTaskServiceWithLocal(h *Handler, local []byte) *tasks.Service {
	opts := taskServiceOptions(h)
	opts.RunLocal = func(context.Context, string) ([]byte, error) { return local, nil }
	return tasks.New(opts)
}

func getTasks(t *testing.T, h *Handler) tasksResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp tasksResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func findTaskResponse(t *testing.T, resp tasksResponse, id string) taskResponse {
	t.Helper()
	for _, task := range resp.Tasks {
		if task.ID == id {
			return task
		}
	}
	require.Failf(t, "task not found", "%q in %+v", id, resp.Tasks)
	return taskResponse{}
}

func TestGetTasks_ReportsEveryConfiguredHost(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"dev-server": {Host: "dev.invalid", User: "demo"},
		"gpu-box":    {Host: "gpu.invalid", User: "demo"},
	}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghNoPRScript)
	h.sshConfigPath = filepath.Join(t.TempDir(), "no-ssh-config")
	remote := &stubTaskConn{output: taskCollection("remote-sess", "/remote/home/demo/service-a")}
	useTaskService(h, taskCollection("local-sess", "/workspace/user/not-a-repo"), map[string]tasks.Conn{
		"dev-server": remote,
	})

	resp := getTasks(t, h)

	require.Len(t, resp.Hosts, 3)
	assert.Equal(t, "", resp.Hosts[0].Name)
	assert.Equal(t, tasks.HostOK, resp.Hosts[0].Status)
	assert.Equal(t, "dev-server", resp.Hosts[1].Name)
	assert.Equal(t, tasks.HostOK, resp.Hosts[1].Status)
	assert.Equal(t, "gpu-box", resp.Hosts[2].Name)
	assert.Equal(t, tasks.HostError, resp.Hosts[2].Status)
	assert.Contains(t, resp.Hosts[2].Error, "i/o timeout")

	local := findTaskResponse(t, resp, "local:claude:local-sess")
	assert.Equal(t, tasks.StateWait, local.State)
	assert.Equal(t, "input needed", local.WaitingFor)
	assert.Equal(t, tasks.Location{Kind: tasks.LocationTmux, TmuxSession: "task-7c21", Attachable: true}, local.Location)
	assert.Nil(t, local.Git, "a directory that is not a repository has no git info")

	onRemote := findTaskResponse(t, resp, "ssh:dev-server:claude:remote-sess")
	assert.Equal(t, "dev-server", onRemote.Host)
	require.NotNil(t, onRemote.Git)
	assert.Equal(t, taskGitInfo{
		Repo: "payment", RepoURL: "https://github.com/example-org/payment", Branch: "PAY-418-retry-backoff",
	}, *onRemote.Git)
}

func TestGetTasks_ResponseCarriesTheWireFieldNames(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	useTaskService(h, taskCollection("s", "/workspace/user/project"), nil)
	h.taskGitLookup = func(context.Context, string, string) *taskGitInfo {
		return &taskGitInfo{Repo: "project", Branch: "main", PRNumber: 12, PRURL: "https://example.invalid/pr/12"}
	}

	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var raw struct {
		Hosts []map[string]any `json:"hosts"`
		Tasks []map[string]any `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Len(t, raw.Tasks, 1)
	task := raw.Tasks[0]
	for _, key := range []string{"id", "host", "agent", "session_id", "cwd", "state", "waiting_for",
		"status_since", "location", "pid", "git"} {
		assert.Contains(t, task, key)
	}
	assert.Equal(t, map[string]any{
		"repo": "project", "branch": "main", "pr_number": float64(12), "pr_url": "https://example.invalid/pr/12",
	}, task["git"])
	assert.Contains(t, raw.Hosts[0], "collected_at")
}

func TestGetTasks_NoTasksIsAnEmptyList(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	useTaskService(h, []byte("::panemux-tasks v1\n::now 1\n::end\n"), nil)

	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"tasks":[]`)
}

func TestTaskGitInfo_LocalRepositoryWithPR(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "-b", "feature/task-dashboard").CombinedOutput()
	require.NoError(t, err, string(out))

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/253\",\"number\":253}'\n")

	info := h.lookupTaskGit(context.Background(), "", dir)
	require.NotNil(t, info)
	assert.Equal(t, taskGitInfo{
		Repo: filepath.Base(dir), RepoURL: "https://github.com/example/panemux", Branch: "feature/task-dashboard",
		PRURL: "https://github.com/example/panemux/pull/253", PRNumber: 253,
	}, *info)
}

// Without an origin URL there is no repository to name for `gh`, so a PR is
// looked up by running `gh` inside the directory — which only works on the
// panemux host. A remote directory with no origin gets no PR lookup at all.
func TestTaskGitInfo_WithoutAnOriginOnlyTheLocalHostLooksUpAPR(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "remote", "remove", "origin").CombinedOutput()
	require.NoError(t, err, string(out))

	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/9\",\"number\":9}'\n")
	remote := &noOriginConn{stubTaskConn: stubTaskConn{output: taskCollection("r", dir)}}
	useTaskService(h, taskCollection("l", dir), map[string]tasks.Conn{"dev-server": remote})
	getTasks(t, h) // opens the dev-server connection

	local := h.lookupTaskGit(context.Background(), "", dir)
	require.NotNil(t, local)
	assert.Equal(t, 9, local.PRNumber)

	onRemote := h.lookupTaskGit(context.Background(), "dev-server", dir)
	require.NotNil(t, onRemote)
	assert.Equal(t, "main", onRemote.Branch)
	assert.Zero(t, onRemote.PRNumber)
}

type noOriginConn struct{ stubTaskConn }

func (c *noOriginConn) InspectGitContext(_ context.Context, cwd string) (session.GitContext, error) {
	return session.GitContext{Root: cwd, Branch: "main", Repo: "panemux"}, nil
}

func TestTaskGitInfo_NoGitBinary(t *testing.T) {
	original := gitExistsFn
	gitExistsFn = func() error { return errors.New("no git") }
	t.Cleanup(func() { gitExistsFn = original })

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	assert.Nil(t, h.lookupTaskGit(context.Background(), "", t.TempDir()))
}

func TestTaskGitInfo_RemoteFailureIsNoGitInfo(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	conn := &stubTaskConn{output: taskCollection("s", "/remote/home/demo/x"), gitErr: errors.New("not a git repository")}
	useTaskService(h, taskCollection("l", ""), map[string]tasks.Conn{"dev-server": conn})

	resp := getTasks(t, h)
	assert.Nil(t, findTaskResponse(t, resp, "ssh:dev-server:claude:s").Git)
}

func TestGetTasks_GitLookupsAreSharedPerDirectoryAndCached(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	h.nowFn = func() time.Time { return now }
	two := []byte(strings.Join([]string{
		"::panemux-tasks v1", "::now 1000", "::section state",
		"::file 7.json", `{"pid":7,"sessionId":"a","cwd":"/workspace/user/project","status":"busy"}`,
		"::file 8.json", `{"pid":8,"sessionId":"b","cwd":"/workspace/user/project","status":"idle"}`,
		"::file 9.json", `{"pid":9,"sessionId":"c","status":"idle"}`,
		"::section ps", "7 1 claude", "8 1 claude", "9 1 claude",
		"::end",
	}, "\n") + "\n")
	useTaskService(h, two, nil)
	var lookups []string
	var mu sync.Mutex
	h.taskGitLookup = func(_ context.Context, host, cwd string) *taskGitInfo {
		mu.Lock()
		defer mu.Unlock()
		lookups = append(lookups, host+"|"+cwd)
		return &taskGitInfo{Branch: "main"}
	}

	resp := getTasks(t, h)
	assert.Equal(t, []string{"|/workspace/user/project"}, lookups, "one lookup per directory; none without one")
	assert.NotNil(t, findTaskResponse(t, resp, "local:claude:a").Git)
	assert.NotNil(t, findTaskResponse(t, resp, "local:claude:b").Git)
	assert.Nil(t, findTaskResponse(t, resp, "local:claude:c").Git)

	getTasks(t, h)
	assert.Len(t, lookups, 1, "cached within gitInfoCacheTTL")

	now = now.Add(gitInfoCacheTTL + time.Second)
	getTasks(t, h)
	assert.Len(t, lookups, 2, "looked up again once the cache entry expires")
}

func TestGetTasks_NoGitLookupOnAHostThatFailed(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"gpu-box": {Host: "gpu.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	useTaskService(h, []byte("garbage"), nil)
	called := false
	h.taskGitLookup = func(context.Context, string, string) *taskGitInfo {
		called = true
		return nil
	}

	resp := getTasks(t, h)
	assert.Empty(t, resp.Tasks)
	assert.False(t, called)
}

func TestGetTasks_AHostThatCannotBeDialedReportsWhy(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"gpu-box": {Host: "gpu.invalid", User: "demo", KeyFile: "relative/id_ed25519"},
	}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.SetTaskService(newTaskServiceWithLocal(h, taskCollection("l", "")))

	resp := getTasks(t, h)
	require.Len(t, resp.Hosts, 2)
	assert.Equal(t, tasks.HostError, resp.Hosts[1].Status)
	assert.Contains(t, resp.Hosts[1].Error, "connect to gpu-box: dial:")
	assert.Contains(t, resp.Hosts[1].Error, "key file")
}

func TestPostTaskHostReconnect(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"gpu-box": {Host: "gpu.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/hosts/gpu-box/reconnect", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/hosts/unknown/reconnect", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlerClose_ClosesTaskConnections(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	conn := &closeCountingConn{stubTaskConn: stubTaskConn{output: taskCollection("s", "")}}
	useTaskService(h, taskCollection("l", ""), map[string]tasks.Conn{"dev-server": conn})
	h.taskGitLookup = func(context.Context, string, string) *taskGitInfo { return nil }

	getTasks(t, h)
	h.Close()
	assert.Equal(t, 1, conn.closed)
}

type closeCountingConn struct {
	stubTaskConn
	closed int
}

func (c *closeCountingConn) Close() error {
	c.closed++
	return nil
}

func TestSetTaskService_ClosesTheOneItReplaces(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	conn := &closeCountingConn{stubTaskConn: stubTaskConn{output: taskCollection("s", "")}}
	useTaskService(h, taskCollection("l", ""), map[string]tasks.Conn{"dev-server": conn})
	h.taskGitLookup = func(context.Context, string, string) *taskGitInfo { return nil }
	getTasks(t, h)

	h.SetTaskService(tasks.New(tasks.Options{
		RunLocal: func(context.Context, string) ([]byte, error) { return taskCollection("replaced", ""), nil },
	}))
	assert.Equal(t, 1, conn.closed)
	assert.Equal(t, "local:claude:replaced", getTasks(t, h).Tasks[0].ID)
}

// GET /api/tasks makes panemux dial every host and run a script there, so a
// page on another site must not be able to trigger it — an <img> pointing at
// the route would otherwise collect whenever that page is open.
func TestTaskRoutes_RefuseCrossSiteRequests(t *testing.T) {
	cases := []struct {
		headers map[string]string
		name    string
		allowed bool
	}{
		{name: "no browser headers (curl, the Go client)", allowed: true},
		{name: "same-origin fetch", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, allowed: true},
		{name: "typed into the address bar", headers: map[string]string{"Sec-Fetch-Site": "none"}, allowed: true},
		{
			name: "loopback origin (the Vite dev server)", headers: map[string]string{"Origin": "http://localhost:5173"},
			allowed: true,
		},
		{
			name: "the server's own origin", headers: map[string]string{"Origin": "http://panemux.test:8080"},
			allowed: true,
		},
		{name: "cross-site fetch or image", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{name: "same-site but another origin", headers: map[string]string{"Sec-Fetch-Site": "same-site"}},
		{name: "foreign origin", headers: map[string]string{"Origin": "https://evil.example"}},
		{name: "malformed origin", headers: map[string]string{"Origin": "::"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultTestConfig()
			cfg.SSHConnections = map[string]config.SSHConnection{"gpu-box": {Host: "gpu.invalid"}}
			h := NewHandler(cfg, session.NewManager(), nil, nil)
			collections := 0
			h.SetTaskService(tasks.New(tasks.Options{
				Hosts: h.taskHostNames,
				Dial:  func(string) (tasks.Conn, error) { return nil, errors.New("unreachable") },
				RunLocal: func(context.Context, string) ([]byte, error) {
					collections++
					return taskCollection("l", ""), nil
				},
			}))
			r := setupRouterWithHandler(h)

			for _, req := range []*http.Request{
				httptest.NewRequest(http.MethodGet, "/api/tasks", nil),
				httptest.NewRequest(http.MethodPost, "/api/tasks/hosts/gpu-box/reconnect", nil),
			} {
				req.Host = "panemux.test:8080"
				for k, v := range tc.headers {
					req.Header.Set(k, v)
				}
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				if tc.allowed {
					assert.Less(t, rec.Code, 300, "%s %s", req.Method, req.URL.Path)
				} else {
					assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s", req.Method, req.URL.Path)
				}
			}
			if tc.allowed {
				assert.Equal(t, 1, collections)
			} else {
				assert.Zero(t, collections, "a refused request collects nothing")
			}
		})
	}
}

// Git metadata is read from the directory as it is now. For a stopped task
// that is not the branch it worked on, so only the repository is reported.
func TestGetTasks_StoppedTaskReportsItsRepositoryButNotBranchOrPR(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	stopped := []byte(strings.Join([]string{
		"::panemux-tasks v1", "::now 1000",
		"::section state",
		"::file 7.json", `{"pid":7,"sessionId":"live","cwd":"/workspace/user/project","status":"busy"}`,
		"::section ps", "7 1 claude",
		"::section transcripts",
		"900\tgone.jsonl\t\"cwd\":\"/workspace/user/project\"",
		"::end",
	}, "\n") + "\n")
	useTaskService(h, stopped, nil)
	h.taskGitLookup = func(context.Context, string, string) *taskGitInfo {
		return &taskGitInfo{Repo: "project", RepoURL: "https://example.invalid/project", Branch: "today",
			PRNumber: 3, PRURL: "https://example.invalid/project/pull/3"}
	}

	resp := getTasks(t, h)
	live := findTaskResponse(t, resp, "local:claude:live")
	require.NotNil(t, live.Git)
	assert.Equal(t, "today", live.Git.Branch)
	assert.Equal(t, 3, live.Git.PRNumber)

	gone := findTaskResponse(t, resp, "local:claude:gone")
	require.NotNil(t, gone.Git)
	assert.Equal(t, taskGitInfo{Repo: "project", RepoURL: "https://example.invalid/project"}, *gone.Git)
}

// The PR lookup runs under the request's context, so a request the browser
// abandoned does not keep `gh` running.
func TestTaskGitInfo_PRLookupStopsWithTheRequest(t *testing.T) {
	dir := initTempGitRepo(t)
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/9\",\"number\":9}'\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info := h.lookupTaskGit(ctx, "", dir)
	require.NotNil(t, info)
	assert.Equal(t, "main", info.Branch)
	assert.Zero(t, info.PRNumber)
}

func TestTaskGitFor(t *testing.T) {
	full := &taskGitInfo{Repo: "r", RepoURL: "https://example.invalid/r", Branch: "b", PRNumber: 1,
		PRURL: "https://example.invalid/r/pull/1"}
	running := tasks.Task{State: tasks.StateBusy}
	stopped := tasks.Task{State: tasks.StateStop}

	assert.Same(t, full, taskGitFor(running, full), "a running task gets everything")
	assert.Nil(t, taskGitFor(running, nil))
	assert.Nil(t, taskGitFor(stopped, nil))
	assert.Equal(t, &taskGitInfo{Repo: "r", RepoURL: "https://example.invalid/r"}, taskGitFor(stopped, full))
	assert.Equal(t, &taskGitInfo{Repo: "r"}, taskGitFor(stopped, &taskGitInfo{Repo: "r", Branch: "b"}))
	assert.Equal(t, &taskGitInfo{RepoURL: "https://example.invalid/r"},
		taskGitFor(stopped, &taskGitInfo{RepoURL: "https://example.invalid/r", Branch: "b"}))
	assert.Nil(t, taskGitFor(stopped, &taskGitInfo{Branch: "b"}), "nothing left to report")
}
