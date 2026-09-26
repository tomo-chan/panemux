package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo {
		return &taskGitInfo{Repo: "project", Branch: "PAY-418", PRNumber: 12, PRURL: "https://example.invalid/pr/12",
			Issues: []taskIssueLink{{Number: 3, URL: "https://example.invalid/issues/3", Repo: "example/project"}},
			Jira:   []taskJiraLink{{Key: "PAY-418", URL: "https://example.invalid/browse/PAY-418"}}}
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
		"repo": "project", "branch": "PAY-418", "pr_number": float64(12), "pr_url": "https://example.invalid/pr/12",
		"issues": []any{map[string]any{
			"number": float64(3), "url": "https://example.invalid/issues/3", "repo": "example/project",
		}},
		"jira": []any{map[string]any{"key": "PAY-418", "url": "https://example.invalid/browse/PAY-418"}},
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

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Equal(t, taskGitInfo{
		Repo: filepath.Base(dir), RepoURL: "https://github.com/example/panemux", Branch: "feature/task-dashboard",
		PRURL: "https://github.com/example/panemux/pull/253", PRNumber: 253,
	}, *info)
}

// ghClosingIssuesScript answers `gh pr view` the way gh 2.101.0 exports
// closingIssuesReferences (api/export_pr.go), and only when the call asks
// for the fields the task dashboard needs.
const ghClosingIssuesScript = `#!/bin/sh
case "$*" in
*"--json url,number,title,closingIssuesReferences"*) ;;
*) echo "unexpected arguments: $*" >&2; exit 1 ;;
esac
cat <<'JSON'
{"url":"https://github.com/example/payment/pull/87","number":87,"title":"PAY-418: add retry backoff (OPS-77)",
 "closingIssuesReferences":[
  {"id":"I_1","number":252,"url":"https://github.com/example/payment/issues/252",
   "repository":{"id":"R_1","name":"payment","owner":{"id":"U_1","login":"example"}}},
  {"id":"I_2","number":9,"url":"https://github.com/example/infra/issues/9",
   "repository":{"id":"R_2","name":"infra","owner":{"id":"U_1","login":"example"}}},
  {"id":"I_3","number":10,"url":"javascript:alert(1)",
   "repository":{"id":"R_2","name":"infra","owner":{"id":"U_1","login":"example"}}},
  {"id":"I_4","number":0,"url":"https://github.com/example/infra/issues/0",
   "repository":{"id":"R_2","name":"infra","owner":{"id":"U_1","login":"example"}}}
 ]}
JSON
`

// A running task's directory gets the issues its pull request closes and
// the Jira keys in its branch name and PR title, linked into the configured
// Jira site. A URL that is not http(s) and a number that is not an issue
// number are dropped rather than failing the whole response in the browser.
func TestTaskGitInfo_IssuesThePRClosesAndJiraKeys(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "-b", "PAY-418-retry-backoff").CombinedOutput()
	require.NoError(t, err, string(out))

	cfg := defaultTestConfig()
	cfg.TaskDashboard.JiraURL = "https://example.atlassian.net/"
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghClosingIssuesScript)

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Equal(t, 87, info.PRNumber)
	assert.Equal(t, []taskIssueLink{
		{Number: 252, URL: "https://github.com/example/payment/issues/252", Repo: "example/payment"},
		{Number: 9, URL: "https://github.com/example/infra/issues/9", Repo: "example/infra"},
	}, info.Issues)
	assert.Equal(t, []taskJiraLink{
		{Key: "PAY-418", URL: "https://example.atlassian.net/browse/PAY-418"},
		{Key: "OPS-77", URL: "https://example.atlassian.net/browse/OPS-77"},
	}, info.Jira)
}

// ghBefore272Script answers like gh before 2.72.0, which has no
// closingIssuesReferences field: it refuses the whole call with the message
// pkg/cmdutil/json_flags.go prints (checked in v2.71.2), and answers the
// fields it knows. Every call is logged to the file in $GH_CALLS.
const ghBefore272Script = `#!/bin/sh
echo "$*" >> "$GH_CALLS"
case "$*" in
*closingIssuesReferences*)
  printf 'Unknown JSON field: "closingIssuesReferences"\nAvailable fields:\n  number\n  title\n  url\n' >&2
  exit 1 ;;
esac
echo '{"url":"https://github.com/example/payment/pull/87","number":87}'
`

// A gh too old for closingIssuesReferences refuses the whole call. The task
// keeps its PR link — the pane header's own lookup still finds that PR —
// and goes without issues and the title's Jira keys.
func TestTaskGitInfo_AGHWithoutClosingIssuesStillFindsThePR(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "-b", "PAY-418-retry").CombinedOutput()
	require.NoError(t, err, string(out))
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("GH_CALLS", calls)

	cfg := defaultTestConfig()
	cfg.TaskDashboard.JiraURL = "https://example.atlassian.net"
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghBefore272Script)

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Equal(t, 87, info.PRNumber)
	assert.Equal(t, "https://github.com/example/payment/pull/87", info.PRURL)
	assert.Nil(t, info.Issues)
	assert.Equal(t, []taskJiraLink{{Key: "PAY-418", URL: "https://example.atlassian.net/browse/PAY-418"}}, info.Jira)

	logged, err := os.ReadFile(calls)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(logged)), "\n")
	require.Len(t, lines, 2, "the task fields, then the pane header's")
	assert.Contains(t, lines[0], "--json "+prTaskFields)
	assert.Contains(t, lines[1], "--json "+prBasicFields)
}

// A branch without a PR is the common case. gh fails then too, but not for
// an unknown field, so it is not asked again.
//
//efficacy:exempt guards the scope of this branch's old-gh fallback; before it no second call existed
func TestTaskGitInfo_NoPRRunsGHOnce(t *testing.T) {
	dir := initTempGitRepo(t)
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("GH_CALLS", calls)
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, "#!/bin/sh\necho \"$*\" >> \"$GH_CALLS\"\n"+
		"echo 'no pull requests found for branch \"main\"' >&2\nexit 1\n")

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Zero(t, info.PRNumber)

	logged, err := os.ReadFile(calls)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(logged), "\n"))
}

// Without a Jira site in the config there is nothing to link a key to.
func TestTaskGitInfo_NoJiraSiteNoJiraLinks(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "-b", "PAY-418-retry-backoff").CombinedOutput()
	require.NoError(t, err, string(out))

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghClosingIssuesScript)

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Len(t, info.Issues, 2)
	assert.Nil(t, info.Jira)
}

// No pull request means no issues; the branch name still carries a Jira key.
func TestTaskGitInfo_WithoutAPROnlyTheBranchGivesJiraKeys(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "-b", "feature/PAY-418-retry").CombinedOutput()
	require.NoError(t, err, string(out))

	cfg := defaultTestConfig()
	cfg.TaskDashboard.JiraURL = "https://example.atlassian.net"
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghNoPRScript)

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Zero(t, info.PRNumber)
	assert.Nil(t, info.Issues)
	assert.Equal(t, []taskJiraLink{{Key: "PAY-418", URL: "https://example.atlassian.net/browse/PAY-418"}}, info.Jira)
}

// Without `gh` on the panemux host there is no PR and so no issues, but the
// branch name still carries its Jira key.
func TestTaskGitInfo_WithoutGHTheBranchStillGivesJiraKeys(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "-b", "PAY-418-retry").CombinedOutput()
	require.NoError(t, err, string(out))
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	onlyGit := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(onlyGit, "git")))
	t.Setenv("PATH", onlyGit)
	_, err = exec.LookPath("gh")
	require.Error(t, err, "gh must not be on PATH")

	cfg := defaultTestConfig()
	cfg.TaskDashboard.JiraURL = "https://example.atlassian.net"
	h := NewHandler(cfg, session.NewManager(), nil, nil)

	info := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Zero(t, info.PRNumber)
	assert.Nil(t, info.Issues)
	assert.Equal(t, []taskJiraLink{{Key: "PAY-418", URL: "https://example.atlassian.net/browse/PAY-418"}}, info.Jira)
}

// The edges of each range and the byte on either side of it: what decides
// whether a character touching a Jira key makes it part of a longer word.
func TestIsASCIIAlnum(t *testing.T) {
	for _, b := range []byte("09AZaz") {
		assert.True(t, isASCIIAlnum(b), "%q", b)
	}
	for _, b := range []byte("/:@[`{-_ ") {
		assert.False(t, isASCIIAlnum(b), "%q", b)
	}
	assert.False(t, isASCIIAlnum(0xC3), "the first byte of a non-ASCII letter")
}

func TestJiraKeys(t *testing.T) {
	tests := []struct {
		name  string
		texts []string
		want  []string
	}{
		{"none", []string{"main", "fix typo"}, nil},
		{"branch prefix", []string{"PAY-418-retry-backoff"}, []string{"PAY-418"}},
		{"after a slash", []string{"feature/PAY-418"}, []string{"PAY-418"}},
		{"after an underscore", []string{"feature_PAY-418"}, []string{"PAY-418"}},
		{"digits and underscore in the project key", []string{"A1_B-7"}, []string{"A1_B-7"}},
		{"one-letter project key", []string{"P-1"}, nil},
		{"lower case", []string{"pay-418"}, nil},
		{"leading zero", []string{"PAY-0418"}, nil},
		{"letter before", []string{"xPAY-418"}, nil},
		{"digit before", []string{"1PAY-418"}, nil},
		{"letter after", []string{"PAY-418a"}, nil},
		{"digit after the key is part of it", []string{"PAY-4180"}, []string{"PAY-4180"}},
		{"punctuation around", []string{"[PAY-418]: (OPS-7)."}, []string{"PAY-418", "OPS-7"}},
		{"branch first, then title, no repeats", []string{"PAY-418-x", "OPS-7 and PAY-418"},
			[]string{"PAY-418", "OPS-7"}},
		{"a rejected match does not hide a later one", []string{"xPAY-1 OPS-2"}, []string{"OPS-2"}},
		{"empty texts", []string{"", ""}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, jiraKeys(tt.texts...))
		})
	}
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

	local := h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, local)
	assert.Equal(t, 9, local.PRNumber)

	onRemote := h.lookupTaskGit(context.Background(), "dev-server", dir, true)
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
	assert.Nil(t, h.lookupTaskGit(context.Background(), "", t.TempDir(), true))
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
	h.taskGitLookup = func(_ context.Context, host, cwd string, _ bool) *taskGitInfo {
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
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo {
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
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }

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
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo { return nil }
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
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo {
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
	info := h.lookupTaskGit(ctx, "", dir, true)
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

	linked := &taskGitInfo{Repo: "r", Branch: "PAY-1", PRNumber: 1,
		Issues: []taskIssueLink{{Number: 2, URL: "https://example.invalid/r/issues/2"}},
		Jira:   []taskJiraLink{{Key: "PAY-1", URL: "https://example.invalid/browse/PAY-1"}}}
	assert.Same(t, linked, taskGitFor(running, linked))
	assert.Equal(t, &taskGitInfo{Repo: "r"}, taskGitFor(stopped, linked),
		"a stopped task reports no issues or Jira keys, which come from the branch and PR it no longer has")
}

// collectionOf renders one host's collection output with the given state
// files (session ID → cwd, each a busy claude process) and stopped
// conversation logs (session ID → cwd).
func collectionOf(running, stopped map[string]string) []byte {
	lines := []string{"::panemux-tasks v1", "::now 1000", "::section state"}
	var ps []string
	pid := 7
	for _, id := range sortedKeys(running) {
		lines = append(lines, "::file "+strconv.Itoa(pid)+".json",
			`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"`+id+`","cwd":"`+running[id]+`","status":"busy"}`)
		ps = append(ps, strconv.Itoa(pid)+" 1 claude")
		pid++
	}
	lines = append(lines, "::section ps")
	lines = append(lines, ps...)
	lines = append(lines, "::section transcripts")
	for _, id := range sortedKeys(stopped) {
		lines = append(lines, "900\t"+id+".jsonl\t\"cwd\":\""+stopped[id]+"\"")
	}
	return []byte(strings.Join(append(lines, "::end"), "\n") + "\n")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// A stopped task reports no pull request (taskGitFor), so a directory only
// stopped tasks use is looked up without running `gh pr view`.
func TestGetTasks_PRIsLookedUpOnlyForADirectoryARunningTaskUses(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	useTaskService(h, collectionOf(
		map[string]string{"live": "/workspace/user/live"},
		map[string]string{"gone": "/workspace/user/live", "old": "/workspace/user/old"},
	), nil)
	withPR := map[string]bool{}
	var mu sync.Mutex
	h.taskGitLookup = func(_ context.Context, _, cwd string, pr bool) *taskGitInfo {
		mu.Lock()
		defer mu.Unlock()
		withPR[cwd] = pr
		return &taskGitInfo{Repo: "r"}
	}

	getTasks(t, h)
	assert.Equal(t, map[string]bool{"/workspace/user/live": true, "/workspace/user/old": false}, withPR)
}

// A cached lookup made without a PR does not serve a running task that
// needs one; a cached lookup with a PR serves a stopped task too.
func TestGetTasks_ACachedLookupWithoutAPRIsNotEnoughForARunningTask(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	h.nowFn = func() time.Time { return now }
	var local []byte
	h.tasks = tasks.New(tasks.Options{
		Hosts:    h.taskHostNames,
		RunLocal: func(context.Context, string) ([]byte, error) { return local, nil },
	})
	var lookups []bool
	var mu sync.Mutex
	h.taskGitLookup = func(_ context.Context, _, _ string, pr bool) *taskGitInfo {
		mu.Lock()
		defer mu.Unlock()
		lookups = append(lookups, pr)
		if pr {
			return &taskGitInfo{Repo: "r", Branch: "b", PRNumber: 5}
		}
		return &taskGitInfo{Repo: "r", Branch: "b"}
	}
	stoppedOnly := collectionOf(nil, map[string]string{"gone": "/workspace/user/project"})
	running := collectionOf(map[string]string{"live": "/workspace/user/project"}, nil)

	local = stoppedOnly
	getTasks(t, h)
	assert.Equal(t, []bool{false}, lookups)

	local = running
	resp := getTasks(t, h)
	assert.Equal(t, []bool{false, true}, lookups, "looked up again, with the PR, within gitInfoCacheTTL")
	live := findTaskResponse(t, resp, "local:claude:live")
	require.NotNil(t, live.Git)
	assert.Equal(t, 5, live.Git.PRNumber)

	local = stoppedOnly
	getTasks(t, h)
	local = running
	getTasks(t, h)
	assert.Equal(t, []bool{false, true}, lookups, "the lookup with the PR serves both")
}

func TestTaskGitInfo_WithoutPRDoesNotRunGH(t *testing.T) {
	dir := initTempGitRepo(t)
	marker := filepath.Join(t.TempDir(), "gh-ran")
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, "#!/bin/sh\ntouch '"+marker+"'\n"+
		"echo '{\"url\":\"https://github.com/example/panemux/pull/9\",\"number\":9}'\n")

	info := h.lookupTaskGit(context.Background(), "", dir, false)
	require.NotNil(t, info)
	assert.Equal(t, "main", info.Branch)
	assert.Zero(t, info.PRNumber)
	assert.NoFileExists(t, marker)

	info = h.lookupTaskGit(context.Background(), "", dir, true)
	require.NotNil(t, info)
	assert.Equal(t, 9, info.PRNumber)
	assert.FileExists(t, marker)
}

// A lookup the request abandoned is not a result: `gh` or the remote git
// run was stopped, not answered. It is not cached, so the next request
// looks the directory up again instead of showing no PR for 30 seconds.
func TestTaskGitInfos_ALookupTheRequestAbandonedIsNotCached(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	list := []tasks.Task{{Host: "", CWD: "/workspace/user/project", State: tasks.StateBusy}}
	collected := map[string]bool{"": true}
	var lookups int
	ctx, cancel := context.WithCancel(context.Background())
	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo {
		lookups++
		cancel() // the browser went away while the lookup ran
		return &taskGitInfo{Repo: "r", Branch: "b"}
	}

	h.taskGitInfos(ctx, list, collected)
	require.Equal(t, 1, lookups)

	h.taskGitLookup = func(context.Context, string, string, bool) *taskGitInfo {
		lookups++
		return &taskGitInfo{Repo: "r", Branch: "b", PRNumber: 5}
	}
	got := h.taskGitInfos(context.Background(), list, collected)
	assert.Equal(t, 2, lookups, "looked up again")
	assert.Equal(t, 5, got[taskGitKey("", "/workspace/user/project")].PRNumber)

	h.taskGitInfos(context.Background(), list, collected)
	assert.Equal(t, 2, lookups, "a completed lookup is cached")
}
