package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

// The tests in this file exist for issue #195: the per-block coverage backlog
// `make coverage-blocks` reports. They are the error branches of
// internal/api/handler.go — the shape the 80% statement threshold is blind to,
// because the happy path around an `if err != nil { ... }` carries its function
// over the threshold whether or not any test ever enters the body.
//
// Nothing here changes behavior. Each test names the failure it injects, so a
// later reader can tell an intentionally-unentered branch from one nobody
// noticed.

// ── Failure injection helpers ────────────────────────────────────────────────

// breakDirectory replaces dir with a regular file of the same name, so any
// later os.MkdirAll(dir) fails with ENOTDIR.
//
// This is the injection this file uses wherever production code writes through
// a path it was handed at construction time and offers no writer seam. Dropping
// the directory's write permission would be the obvious alternative and does
// not work: the suite runs as root in CI containers, and root ignores the
// permission bits. A path component that is not a directory is refused for
// everyone.
func breakDirectory(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.RemoveAll(dir))
	require.NoError(t, os.WriteFile(dir, []byte("a regular file, not a directory\n"), 0600))
}

// newUnwritableWorkspaceHandler builds a handler over the same two-workspace
// config loadWorkspaceTestConfigFromFile uses, loaded from a real file whose
// parent directory has since been replaced by a regular file. Every
// Config.write() the handler goes on to trigger fails.
//
// The config has to be loaded from disk first: Config.filePath is unexported
// and write() returns nil when it is empty, so a hand-built Config never
// reaches the failure branches at all.
func newUnwritableWorkspaceHandler(t *testing.T) *Handler {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "config-dir")
	require.NoError(t, os.Mkdir(dir, 0750))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(workspaceTestConfigYAML), 0600))
	cfg, err := config.Load(path)
	require.NoError(t, err)

	breakDirectory(t, dir)
	require.Error(t, cfg.SaveWorkspaces(), "fixture is only meaningful while config writes fail")

	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	return h
}

// readdirOp is the Op os.ReadDir reports when the path it was handed turns out
// not to be a directory. Named once because listLocalDirectories branches on
// it, and goconst counts the package's occurrences of the literal — including
// the ones in tests — against the single use in handler.go.
const readdirOp = "readdir"

// emptyPATH points PATH at a directory containing nothing, so every
// exec.LookPath for a BARE name in the test's scope fails.
//
// It does not neutralize a lookup of an absolute path: exec.LookPath checks a
// name containing a slash directly and never consults PATH. findVSCode's darwin
// fallback is exactly that shape, which is what requireNoVSCodeAppBundle below
// exists for.
func emptyPATH(t *testing.T) {
	t.Helper()

	t.Setenv("PATH", t.TempDir())
}

// vscodeAppBundleBin mirrors the darwin fallback path findVSCode probes. It is
// duplicated here rather than exported, because what the tests below need is
// not the value the handler uses but the answer to "can this machine make
// findVSCode fail at all".
const vscodeAppBundleBin = "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"

// requireNoVSCodeAppBundle skips a test that needs findVSCode to report "not
// found" when this machine has VSCode installed where the darwin fallback looks.
//
// emptyPATH cannot hide that copy (see above), so on such a machine findVSCode
// succeeds. For TestFindVSCode_ResolutionOrder that would be a false failure;
// for the handler test it would be worse — the request runs on to cmd.Start()
// and launches the operator's real editor. CI is linux, where the branch is not
// reachable at all, so this only ever fires on a developer's Mac.
func requireNoVSCodeAppBundle(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "darwin" {
		return
	}
	if _, err := exec.LookPath(vscodeAppBundleBin); err == nil {
		t.Skip("VSCode is installed at " + vscodeAppBundleBin + ", so findVSCode cannot be made to fail here")
	}
}

// writeFakeBinary writes an executable script named name and returns its path.
func writeFakeBinary(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	require.NoError(t, os.Chmod(path, 0755))
	return path
}

// captureLog redirects the standard logger for the duration of the test and
// returns the buffer it writes into.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})
	return &buf
}

func doRequest(t *testing.T, h *Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := setupRouterWithHandler(h)
	rec := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	r.ServeHTTP(rec, req)
	return rec
}

// ── Persisting the config ────────────────────────────────────────────────────

// configWriteFailureCase is one mutating route plus whatever state it needs
// before it will get as far as persisting the config.
//
//nolint:govet // field order follows the table's reading order, not alignment
type configWriteFailureCase struct {
	prepare  func(t *testing.T, h *Handler)
	name     string
	method   string
	path     string
	body     string
	wantBody string
}

// oneWorkspaceLayout is a valid single-pane layout for the "one" workspace of
// workspaceTestConfigYAML, so a PUT gets past validation and reaches the save.
const oneWorkspaceLayout = `{"direction":"horizontal","children":[{"size":100,` +
	`"pane":{"id":"one-main","type":"local"}}]}`

// configWriteFailureCases is a package-level table rather than a local one only
// because the list is longer than funlen allows a single test function to be.
var configWriteFailureCases = []configWriteFailureCase{
	{
		name:     "put layout",
		method:   http.MethodPut,
		path:     "/api/layout",
		body:     oneWorkspaceLayout,
		wantBody: "failed to save layout",
	},
	{
		name:     "put active workspace",
		method:   http.MethodPut,
		path:     "/api/workspaces/active",
		body:     `{"id":"two"}`,
		wantBody: "failed to save workspaces",
	},
	{
		name:     "put workspace tab position",
		method:   http.MethodPut,
		path:     "/api/workspaces/tab-position",
		body:     `{"tab_position":"bottom"}`,
		wantBody: "failed to save workspaces",
	},
	{
		name:     "put workspace vertical bar width",
		method:   http.MethodPut,
		path:     "/api/workspaces/vertical-bar-width",
		body:     `{"vertical_bar_width":320}`,
		wantBody: "failed to save workspaces",
	},
	{
		name:   "post workspace",
		method: http.MethodPost,
		path:   "/api/workspaces",
		prepare: func(t *testing.T, h *Handler) {
			t.Helper()
			h.createSession = func(
				pane *config.PaneConfig, _ map[string]config.SSHConnection,
			) (session.Session, error) {
				return newMockSession(pane.ID), nil
			}
		},
		wantBody: "failed to save workspaces",
	},
	{
		name:     "delete workspace",
		method:   http.MethodDelete,
		path:     "/api/workspaces/two",
		wantBody: "failed to save workspaces",
	},
	{
		name:     "put workspace title",
		method:   http.MethodPut,
		path:     "/api/workspaces/one",
		body:     `{"title":"Renamed"}`,
		wantBody: "failed to save workspaces",
	},
	{
		name:     "put workspace layout",
		method:   http.MethodPut,
		path:     "/api/workspaces/one/layout",
		body:     oneWorkspaceLayout,
		wantBody: "failed to save workspaces",
	},
	{
		name:   "delete session",
		method: http.MethodDelete,
		path:   "/api/sessions/one-main",
		prepare: func(t *testing.T, h *Handler) {
			t.Helper()
			h.manager.Add(newMockSession("one-main"))
		},
		wantBody: "failed to save layout",
	},
}

// Every route that mutates the config persists it before answering, and each
// one has its own "failed to save" branch. They are collected in one table
// rather than beside their happy-path tests because the injected failure is
// identical and the point is that no route is missing the check.
func TestConfigWriteFails_MutatingRoutesReturn500(t *testing.T) {
	for _, tt := range configWriteFailureCases {
		t.Run(tt.name, func(t *testing.T) {
			h := newUnwritableWorkspaceHandler(t)
			if tt.prepare != nil {
				tt.prepare(t, h)
			}

			rec := doRequest(t, h, tt.method, tt.path, tt.body)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}

// DeleteWorkspace persists before it tears the workspace's sessions down, so a
// failed write must leave the manager untouched — otherwise the config on disk
// and the running sessions disagree after the 500.
func TestDeleteWorkspace_ConfigWriteFails_LeavesSessionsRunning(t *testing.T) {
	h := newUnwritableWorkspaceHandler(t)
	h.manager.Add(newMockSession("two-main"))

	rec := doRequest(t, h, http.MethodDelete, "/api/workspaces/two", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	_, ok := h.manager.Get("two-main")
	assert.True(t, ok, "the workspace's session must survive a failed save")
}

// ── Creating sessions ────────────────────────────────────────────────────────

func TestPostSession_InvalidPaneType_Returns422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")

	rec := doRequest(t, h, http.MethodPost, "/api/sessions", `{"id":"bad-pane","type":"telepathy"}`)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body[responseErrorKey], `invalid type "telepathy"`)
}

func TestPostSession_CreateFails_Returns500(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("pty allocation refused")
	}

	rec := doRequest(t, h, http.MethodPost, "/api/sessions", `{"id":"new-pane","type":"local"}`)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "pty allocation refused")
	_, ok := h.manager.Get("new-pane")
	assert.False(t, ok)
}

func TestPostWorkspace_CreateSessionFails_Returns500(t *testing.T) {
	cfg, _ := loadWorkspaceTestConfigFromFile(t)
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("pty allocation refused")
	}

	rec := doRequest(t, h, http.MethodPost, "/api/workspaces", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to create session: pty allocation refused")
}

// ── Reading and writing ~/.ssh/config ────────────────────────────────────────

// A directory in place of the ssh config file is the cheapest input that makes
// sshconfig.ParseHosts fail rather than report "no hosts": os.Open succeeds and
// the first read returns EISDIR. A missing file is deliberately NOT an error
// there, so it cannot stand in for one.
func TestSSHConfigRoutes_UnreadableConfig_Return500(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method string
		body   string
	}{
		{name: "get hosts", method: http.MethodGet},
		{
			name:   "post host",
			method: http.MethodPost,
			body:   `{"name":"newhost","hostname":"example.com","user":"demo"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
			h.sshConfigPath = t.TempDir()

			rec := doRequest(t, h, tt.method, "/api/ssh-config/hosts", tt.body)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.Contains(t, rec.Body.String(), "failed to read ssh config")
		})
	}
}

// The append has its own failure branch after the read has already succeeded,
// so it needs a path that reads clean and writes dirty: a config file inside a
// directory that is itself a dangling symlink. ParseHosts resolves it to
// ENOENT, which it treats as "no hosts yet"; AppendHost's os.MkdirAll then
// fails because the symlink exists but is not a directory.
func TestPostSSHConfigHost_WriteFails_Returns500(t *testing.T) {
	base := t.TempDir()
	sshDir := filepath.Join(base, "ssh-dir")
	require.NoError(t, os.Symlink(filepath.Join(base, "no-such-target"), sshDir))

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(sshDir, "config")

	rec := doRequest(
		t, h,
		http.MethodPost, "/api/ssh-config/hosts",
		`{"name":"newhost","hostname":"example.com","user":"demo"}`,
	)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to write ssh config")
}

// ── Opening VSCode ───────────────────────────────────────────────────────────

func TestPostOpenVSCode_CWDLookupFails_Returns500(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-cwd-err", typ: session.TypeLocal},
		cwdErr:      errors.New("shell is gone"),
	})

	rec := doRequest(t, h, http.MethodPost, "/api/sessions/local-cwd-err/open-vscode", "")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to get working directory: shell is gone")
}

func TestPostOpenVSCode_RelativeCWD_Returns422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.codeBinaryPath = "/bin/echo"
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-rel", typ: session.TypeLocal},
		cwd:         "relative/path",
	})

	rec := doRequest(t, h, http.MethodPost, "/api/sessions/local-rel/open-vscode", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "working directory must be an absolute path", body[responseErrorKey])
}

func TestPostOpenVSCode_CodeBinaryNotFound_Returns500(t *testing.T) {
	requireNoVSCodeAppBundle(t)
	emptyPATH(t)

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-nocode", typ: session.TypeLocal},
		cwd:         t.TempDir(),
	})

	rec := doRequest(t, h, http.MethodPost, "/api/sessions/local-nocode/open-vscode", "")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "VSCode (code) not found")
}

func TestPostOpenVSCode_LaunchFails_Returns500(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.codeBinaryPath = filepath.Join(t.TempDir(), "code-that-was-uninstalled")
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-launch", typ: session.TypeLocal},
		cwd:         t.TempDir(),
	})

	rec := doRequest(t, h, http.MethodPost, "/api/sessions/local-launch/open-vscode", "")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to launch VSCode")
}

// An SSH pane whose session type says "ssh" but which cannot report a
// connection name has no `--remote` target to build. No shipped session type is
// in that state; the branch exists so a future one cannot silently produce a
// `code` invocation pointed at the local machine instead of the remote host.
func TestVSCodeArgs_SSHSessionWithoutConnectionName_Errors(t *testing.T) {
	for _, typ := range []session.Type{session.TypeSSH, session.TypeSSHTmux} {
		t.Run(string(typ), func(t *testing.T) {
			args, err := vscodeArgs(&mockSession{id: "s", typ: typ}, "/workspace/user/project")

			require.Error(t, err)
			assert.Equal(t, "SSH session missing connection name", err.Error())
			assert.Nil(t, args)
		})
	}
}

func TestFindVSCode_ResolutionOrder(t *testing.T) {
	t.Run("configured path wins", func(t *testing.T) {
		h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
		h.codeBinaryPath = "/opt/vscode/bin/code"

		path, err := h.findVSCode()

		require.NoError(t, err)
		assert.Equal(t, "/opt/vscode/bin/code", path)
	})

	t.Run("falls back to PATH", func(t *testing.T) {
		dir := t.TempDir()
		want := writeFakeBinary(t, dir, "code", "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", dir)

		h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

		path, err := h.findVSCode()

		require.NoError(t, err)
		assert.Equal(t, want, path)
	})

	t.Run("reports not found", func(t *testing.T) {
		requireNoVSCodeAppBundle(t)
		emptyPATH(t)

		h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

		path, err := h.findVSCode()

		require.Error(t, err)
		assert.Equal(t, "code binary not found", err.Error())
		assert.Empty(t, path)
	})
}

// ── Browsing directories ─────────────────────────────────────────────────────

func TestGetDirectories_InvalidShowHidden_Returns400(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

	rec := doRequest(t, h, http.MethodGet, "/api/directories?path=/tmp&show_hidden=perhaps", "")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid show_hidden value")
}

func TestGetDirectories_UnknownConnection_Returns404(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")

	rec := doRequest(t, h, http.MethodGet, "/api/directories?connection=nope&path=/home", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetDetectShell_UnknownConnection_Returns404(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")

	rec := doRequest(t, h, http.MethodGet, "/api/detect-shell?connection=nope", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// listLocalDirectories translates os.ReadDir's failures into messages the
// directory browser shows the user. Each arm is a different errno and a
// different message, and only the "does not exist" one had a test.
func TestListLocalDirectories_ReadDirFailureMessages(t *testing.T) {
	target := "/workspace/user/project"

	tests := []struct {
		readDirErr error
		name       string
		wantErr    string
	}{
		{
			name:       "missing path",
			readDirErr: &fs.PathError{Op: "open", Path: target, Err: fs.ErrNotExist},
			wantErr:    "directory path does not exist",
		},
		{
			name:       "unreadable path",
			readDirErr: &fs.PathError{Op: "open", Path: target, Err: fs.ErrPermission},
			wantErr:    "reading directory: open " + target + ": permission denied",
		},
		{
			name:       "invalid path",
			readDirErr: &fs.PathError{Op: "open", Path: target, Err: fs.ErrInvalid},
			wantErr:    "path is not a directory",
		},
		{
			name:       "path is a file",
			readDirErr: &fs.PathError{Op: readdirOp, Path: target, Err: syscall.ENOTDIR},
			wantErr:    "path is not a directory",
		},
		{
			name:       "unclassified failure",
			readDirErr: errors.New("i/o error"),
			wantErr:    "reading directory: i/o error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
			h.readDirFn = func(string) ([]os.DirEntry, error) { return nil, tt.readDirErr }

			resp, err := h.listLocalDirectories(target, false)

			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
			assert.Equal(t, directoryBrowserResponse{}, resp)
		})
	}
}

// A child directory that cannot be read for a reason other than permission is
// not skipped the way an unreadable one is — it fails the whole listing, so the
// browser does not quietly present a partial tree as complete.
func TestListLocalDirectories_ChildFailureIsNotSkipped(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	require.NoError(t, os.Mkdir(child, 0755))

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	originalReadDir := h.readDirFn
	h.readDirFn = func(name string) ([]os.DirEntry, error) {
		if name == child {
			return nil, &fs.PathError{Op: readdirOp, Path: name, Err: syscall.EIO}
		}
		return originalReadDir(name)
	}

	resp, err := h.listLocalDirectories(dir, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading directory: read dir "+child)
	assert.Equal(t, directoryBrowserResponse{}, resp)
}

func TestResolveLocalDirectoryBrowsePath(t *testing.T) {
	home := t.TempDir()

	tests := []struct {
		name    string
		home    string
		path    string
		want    string
		wantErr string
	}{
		{name: "empty path is home", home: home, path: "", want: home},
		{name: "bare tilde is home", home: home, path: "~", want: home},
		{name: "tilde prefix joins home", home: home, path: "~/src/panemux", want: filepath.Join(home, "src/panemux")},
		{name: "absolute path is cleaned", home: home, path: "/tmp/sample-project/", want: "/tmp/sample-project"},
		{name: "relative path is made absolute", home: home, path: "sample", want: mustAbs(t, "sample")},
		{name: "empty path without a home", home: "", path: "", wantErr: "getting home directory"},
		{name: "tilde prefix without a home", home: "", path: "~/src", wantErr: "getting home directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)

			got, err := resolveLocalDirectoryBrowsePath(tt.path)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// listLocalDirectories reports the path resolution failure rather than
// swallowing it and listing the process's own working directory instead.
func TestListLocalDirectories_UnresolvablePath_Errors(t *testing.T) {
	t.Setenv("HOME", "")

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

	resp, err := h.listLocalDirectories("~", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "getting home directory")
	assert.Equal(t, directoryBrowserResponse{}, resp)
}

// The remote lister's own wrapper. session.ListRemoteDirectories rejects a path
// carrying shell metacharacters before it dials anything, which is what lets
// this assert the wrapping without a network round trip — the anti-pattern
// DEVELOPMENT.md names is a test that accepts any error a real dial produces.
func TestListRemoteDirectories_WrapsLookupFailure(t *testing.T) {
	resp, err := listRemoteDirectories(session.SSHConfig{Host: "remote.example.com"}, "/srv; rm -rf /", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing remote directories:")
	assert.Equal(t, directoryBrowserResponse{}, resp)
}

// runGit runs one git command in dir and fails the test if it does not succeed.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...) //nolint:gosec // G204: trusted test args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), string(out))
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()

	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return abs
}

// ── Git and PR metadata ──────────────────────────────────────────────────────

func TestGitExistsFn_ReportsAMissingGitBinary(t *testing.T) {
	emptyPATH(t)

	err := gitExistsFn()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "finding git binary")
}

// The cwd this session would have reported is a real repository, so a handler
// that ignored the error would answer IsGit true. Without that, the test passes
// with the error check deleted: an empty cwd fails sanitizeGitExecDir a few
// frames later and reaches the same IsGit false by a different route.
func TestGetGitInfo_CWDLookupFails_IsGitFalse(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghNoPRScript)
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "cwd-err", typ: session.TypeLocal},
		cwd:         initTempGitRepo(t),
		cwdErr:      errors.New("shell is gone"),
	})

	rec := doRequest(t, h, http.MethodGet, "/api/sessions/cwd-err/git-info", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.IsGit)
}

// A session that can report a cwd but has no notion of an "active workdir" —
// every SSH pane, before the agent-board work — resolves to its own directory
// and nothing else.
func TestGetGitInfo_SessionWithoutActiveWorkdirs_UsesItsOwnCWD(t *testing.T) {
	dir := initTempGitRepo(t)
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghNoPRScript)
	h.manager.Add(&mockSSHCWDSession{
		mockSession: mockSession{id: "no-workdirs", typ: session.TypeLocal},
		cwd:         dir,
		connName:    "myserver",
	})

	rec := doRequest(t, h, http.MethodGet, "/api/sessions/no-workdirs/git-info", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	require.Len(t, resp.Worktrees, 1)
}

// A failed active-workdir lookup is not fatal: the pane header falls back to
// the pane's own directory. It is logged, though, because a pane that silently
// stops following its agent's worktree looks like a panemux bug from outside.
func TestGetGitInfo_ActiveWorkdirLookupFails_FallsBackAndLogs(t *testing.T) {
	dir := initTempGitRepo(t)
	buf := captureLog(t)

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghNoPRScript)
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "workdir-err", typ: session.TypeLocal},
		cwd:         dir,
		activeErr:   errors.New("transcript unreadable"),
	})

	rec := doRequest(t, h, http.MethodGet, "/api/sessions/workdir-err/git-info", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	assert.Contains(t, buf.String(), "active workdirs lookup failed: transcript unreadable")
}

// A repository with no `origin` remote is an ordinary state — a local-only
// checkout — not a lookup failure. `git config --get remote.origin.url` exits
// non-zero for it, and the pane header must still show the branch.
func TestGetGitInfo_RepoWithoutOrigin_ReturnsBranchWithoutRepoURL(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main", ".")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "commit", "--allow-empty", "-m", "init")

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.ghBinaryPath = writeFakeGHBinary(t, ghNoPRScript)
	h.manager.Add(&mockCWDSession{
		mockSession: mockSession{id: "no-origin", typ: session.TypeLocal},
		cwd:         dir,
	})

	rec := doRequest(t, h, http.MethodGet, "/api/sessions/no-origin/git-info", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	assert.Empty(t, resp.RepoURL)
	assert.Empty(t, resp.PRURL)
}

func TestInspectLocalGitContext_RelativeCWD_ReportsInvalidCWD(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

	ctx, err := h.inspectLocalGitContext("relative/path")

	require.Error(t, err)
	assert.Equal(t, session.GitContext{}, ctx)

	var ctxErr *session.GitContextError
	require.ErrorAs(t, err, &ctxErr)
	assert.Equal(t, "local", ctxErr.Transport)
	assert.Equal(t, "validate local working directory", ctxErr.Operation)
	assert.Equal(t, "relative/path", ctxErr.CWD)
}

func TestInspectLocalGitContext_GitNotInstalled_IsNamedAsTheCause(t *testing.T) {
	dir := t.TempDir()
	emptyPATH(t)

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

	_, err := h.inspectLocalGitContext(dir)

	require.Error(t, err)
	var ctxErr *session.GitContextError
	require.ErrorAs(t, err, &ctxErr)
	assert.Equal(t, session.GitContextCauseGitNotInstalled, ctxErr.Cause)
	assert.Equal(t, "git is not installed or not available in PATH", ctxErr.CauseMessage)
}

func TestSanitizeGitExecDir_RejectsControlCharacters(t *testing.T) {
	cleaned, err := sanitizeGitExecDir("/tmp/sample\x01project")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "working directory contains invalid characters")
	assert.Empty(t, cleaned)
}

// Every git lookup failure this package raises is a *session.GitContextError,
// so the fallback arm is unreachable from today's callers. It stays because the
// alternative for a future caller that wraps a plain error is a `%v` log with
// no cause and no remediation, which is what this formatting exists to replace.
func TestFormatGitContextLookupError_PlainError_StillNamesCauseAndRemediation(t *testing.T) {
	msg := formatGitContextLookupError("git info pane=\"p1\"", "base context lookup", errors.New("boom\n  and more"))

	assert.Contains(t, msg, `source="base context lookup"`)
	assert.Contains(t, msg, `cause="Git context lookup failed for an unknown reason"`)
	assert.Contains(t, msg, `remediation="inspect the raw error details to determine the next action"`)
	assert.Contains(t, msg, `raw_error="boom and more"`)
}

func TestFindGH_ResolutionOrder(t *testing.T) {
	t.Run("configured path wins", func(t *testing.T) {
		h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
		h.ghBinaryPath = "/opt/gh/bin/gh"

		path, err := h.findGH()

		require.NoError(t, err)
		assert.Equal(t, "/opt/gh/bin/gh", path)
	})

	t.Run("falls back to PATH", func(t *testing.T) {
		dir := t.TempDir()
		want := writeFakeBinary(t, dir, "gh", "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", dir)

		h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

		path, err := h.findGH()

		require.NoError(t, err)
		assert.Equal(t, want, path)
	})

	t.Run("reports not found", func(t *testing.T) {
		emptyPATH(t)

		h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)

		path, err := h.findGH()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "finding gh binary")
		assert.Empty(t, path)
	})
}

// `gh pr view` exiting zero is not enough — the pane header only shows a PR it
// could actually parse. Both shapes of unparsable output are exercised, and
// only the second one can fail if lookupPRInfo stops checking the unmarshal
// error: encoding/json validates syntax before it decodes anything, so a
// non-JSON banner leaves the response zero-valued either way. A type mismatch
// does not — json fills the fields it can before returning the error, so
// dropping the check would leak a PR number panemux never confirmed.
func TestGetGitInfo_UnparsableGHOutput_ReportsNoPR(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
	}{
		{
			name:   "not json at all",
			stdout: "gh: a new release is available",
		},
		{
			name:   "json of the wrong shape",
			stdout: `{"url":123,"number":7}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
			h.ghBinaryPath = writeFakeGHBinary(t, "#!/bin/sh\ncat <<'JSON'\n"+tt.stdout+"\nJSON\n")
			h.manager.Add(&mockCWDSession{
				mockSession: mockSession{id: "bad-gh", typ: session.TypeLocal},
				cwd:         initTempGitRepo(t),
			})

			rec := doRequest(t, h, http.MethodGet, "/api/sessions/bad-gh/git-info", "")

			require.Equal(t, http.StatusOK, rec.Code)
			var resp gitInfoResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
			assert.True(t, resp.IsGit)
			assert.Empty(t, resp.PRURL)
			assert.Zero(t, resp.PRNumber)
		})
	}
}

// An scp-style origin names an ssh_config alias, which only ~/.ssh/config can
// resolve to a real hostname. When that file cannot be read the alias is left
// as-is rather than the repo URL being dropped: a wrong-looking link is more
// useful to the operator than no link, and the reason is logged.
func TestRepoPageURLFromOriginURL_UnreadableSSHConfig_KeepsTheAlias(t *testing.T) {
	buf := captureLog(t)

	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	h.sshConfigPath = t.TempDir()

	assert.Equal(
		t,
		"https://work-github/example/panemux",
		h.repoPageURLFromOriginURL("git@work-github:example/panemux.git"),
	)
	assert.Contains(t, buf.String(), "parse ssh config for repo URL alias resolution failed")
}

func TestRepoHostAndPathFromOriginURLDetailed_RejectsMalformedOrigins(t *testing.T) {
	tests := []struct {
		name   string
		origin string
	}{
		{name: "scp style without a colon", origin: "git@github.com/example/panemux.git"},
		{name: "scp style without a host", origin: "git@:example/panemux.git"},
		{name: "scp style without a path", origin: "git@github.com:"},
		{name: "url without a path", origin: "https://github.com"},
		{name: "url with only a slash", origin: "https://github.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path, scpStyle := repoHostAndPathFromOriginURLDetailed(tt.origin)

			assert.Empty(t, host)
			assert.Empty(t, path)
			assert.False(t, scpStyle)
			assert.Empty(t, repoSpecFromOriginURL(tt.origin))
			assert.Empty(t, repoPageURLFromOriginURL(tt.origin))
		})
	}
}
