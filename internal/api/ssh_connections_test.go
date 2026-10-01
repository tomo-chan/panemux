package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/fileops"
	"panemux/internal/homedir"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

const sshConnectionsPath = "/api/config/ssh-connections"

// sshConnectionsTestYAML has one entry a pane uses, one with a password and
// one that is only a name, so each of the entry's derived fields has a case.
const sshConnectionsTestYAML = `
server:
  port: 8080
  host: "127.0.0.1"
ssh_connections:
  used:
    host: used.invalid
    user: demo
  secret:
    host: secret.invalid
    user: demo
    port: 2222
    key_file: /remote/home/demo/.ssh/id_ed25519
    known_hosts_file: /remote/home/demo/.ssh/known_hosts
    password: hunter2-original
  from-ssh-config: {}
workspaces:
  active: one
  items:
    - id: one
      title: One
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: one-ssh
              type: ssh
              connection: used
`

// sshConnectionsEnv is a handler over a config loaded from a real file, so a
// test can read back what a route persisted, plus the ~/.ssh/config it sees.
type sshConnectionsEnv struct {
	h       *Handler
	cfg     *config.Config
	path    string
	sshPath string
}

func newSSHConnectionsEnv(t *testing.T, sshConfig string) *sshConnectionsEnv {
	t.Helper()
	home := t.TempDir()
	homedir.SetForTest(t, home)
	sshPath := filepath.Join(home, ".ssh", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(sshPath), 0o700))
	require.NoError(t, os.WriteFile(sshPath, []byte(sshConfig), 0o600))

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(sshConnectionsTestYAML), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)

	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.sshConfigPath = sshPath
	return &sshConnectionsEnv{h: h, cfg: cfg, path: path, sshPath: sshPath}
}

const fromSSHConfigBlock = "Host from-ssh-config\n  HostName from.invalid\n  User demo\n"

func (e *sshConnectionsEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	setupRouterWithHandler(e.h).ServeHTTP(rec, req)
	return rec
}

func (e *sshConnectionsEnv) reload(t *testing.T) map[string]config.SSHConnection {
	t.Helper()
	cfg, err := config.Load(e.path)
	require.NoError(t, err)
	return cfg.SSHConnections
}

func decodeSSHConnectionEntry(t *testing.T, rec *httptest.ResponseRecorder) sshConnectionEntry {
	t.Helper()
	var entry sshConnectionEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &entry), rec.Body.String())
	return entry
}

func TestGetConfigSSHConnections_ListsEveryEntryWithoutItsPassword(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)

	rec := e.do(t, http.MethodGet, sshConnectionsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2-original")
	assert.NotContains(t, rec.Body.String(), `"password"`)
	var resp sshConnectionsListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []sshConnectionEntry{
		{Name: "from-ssh-config", InSSHConfig: true, Panes: []string{}},
		{
			Name: "secret", Host: "secret.invalid", User: "demo", Port: 2222,
			KeyFile:        "/remote/home/demo/.ssh/id_ed25519",
			KnownHostsFile: "/remote/home/demo/.ssh/known_hosts",
			HasPassword:    true, Panes: []string{},
		},
		{Name: "used", Host: "used.invalid", User: "demo", Panes: []string{"one-ssh"}},
	}, resp.Connections)
}

func TestGetConfigSSHConnections_NoEntriesIsAnEmptyList(t *testing.T) {
	cfg := defaultTestConfig()
	rec := httptest.NewRecorder()
	setupRouter(cfg, session.NewManager()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, sshConnectionsPath, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"connections":[]}`, rec.Body.String())
}

func TestPostConfigSSHConnection_CreatesPersistsAndBecomesADashboardHost(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
	useTaskService(e.h, taskCollection("l", ""), map[string]tasks.Conn{})

	rec := e.do(t, http.MethodPost, sshConnectionsPath,
		`{"name":"gpu","host":"gpu.invalid","user":"demo","port":2200,"key_file":"~/.ssh/id","password":"new-secret"}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "new-secret")
	entry := decodeSSHConnectionEntry(t, rec)
	home, err := homedir.Dir()
	require.NoError(t, err)
	wantKey := filepath.Join(home, ".ssh", "id")
	assert.Equal(t, sshConnectionEntry{
		Name: "gpu", Host: "gpu.invalid", User: "demo", Port: 2200, KeyFile: wantKey,
		HasPassword: true, Panes: []string{},
	}, entry)

	want := config.SSHConnection{Host: "gpu.invalid", User: "demo", Port: 2200, KeyFile: wantKey, Password: "new-secret"}
	assert.Equal(t, want, e.cfg.SSHConnections["gpu"])
	assert.Equal(t, want, e.reload(t)["gpu"])

	var hosts []string
	for _, host := range getTasks(t, e.h).Hosts {
		hosts = append(hosts, host.Name)
	}
	assert.Contains(t, hosts, "gpu")
}

func TestPostConfigSSHConnection_NameOnlyEntryForASSHConfigHost(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock+"Host bastion\n  HostName bastion.invalid\n")

	rec := e.do(t, http.MethodPost, sshConnectionsPath, `{"name":"bastion"}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, sshConnectionEntry{Name: "bastion", InSSHConfig: true, Panes: []string{}},
		decodeSSHConnectionEntry(t, rec))
	assert.Equal(t, config.SSHConnection{}, e.reload(t)["bastion"])
}

// postSSHConnectionRejections is every POST body the route refuses, with why.
var postSSHConnectionRejections = []struct {
	name       string
	body       string
	wantError  string
	wantStatus int
}{
	{name: "malformed JSON", body: `{`, wantStatus: http.StatusBadRequest, wantError: "invalid request body"},
	{
		name:       "unknown field",
		body:       `{"name":"x","hostname":"x"}`,
		wantStatus: http.StatusBadRequest,
		wantError:  "invalid request body",
	},
	{
		name:       "no name",
		body:       `{"host":"x.invalid"}`,
		wantStatus: http.StatusUnprocessableEntity,
		wantError:  "name is required",
	},
	{
		name:       "bad name",
		body:       `{"name":"a b","host":"x.invalid"}`,
		wantStatus: http.StatusUnprocessableEntity,
		wantError:  "name must contain only",
	},
	{
		name:       "bad port",
		body:       `{"name":"x","host":"x.invalid","port":70000}`,
		wantStatus: http.StatusUnprocessableEntity,
		wantError:  "port must be between",
	},
	{
		name:       "relative key file",
		body:       `{"name":"x","host":"x.invalid","key_file":"id"}`,
		wantStatus: http.StatusUnprocessableEntity,
		wantError:  "key_file must be",
	},
	{
		name:       "control character",
		body:       `{"name":"x","host":"x\ninvalid"}`,
		wantStatus: http.StatusUnprocessableEntity,
		wantError:  "host must not contain",
	},
	{
		name: "no host and no ssh config block", body: `{"name":"nowhere"}`,
		wantStatus: http.StatusUnprocessableEntity, wantError: `~/.ssh/config has no Host block named "nowhere"`,
	},
	{
		name:       "clear_password on create",
		body:       `{"name":"x","host":"x.invalid","clear_password":true}`,
		wantStatus: http.StatusUnprocessableEntity,
		wantError:  "clear_password",
	},
	{
		name:       "duplicate name",
		body:       `{"name":"used","host":"x.invalid"}`,
		wantStatus: http.StatusConflict,
		wantError:  `ssh connection "used" already exists`,
	},
}

func TestPostConfigSSHConnection_Rejections(t *testing.T) {
	for _, tt := range postSSHConnectionRejections {
		t.Run(tt.name, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
			before := e.reload(t)

			rec := e.do(t, http.MethodPost, sshConnectionsPath, tt.body)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, responseError(t, rec), tt.wantError)
			assert.Equal(t, before, e.cfg.SSHConnections)
			assert.Equal(t, before, e.reload(t))
		})
	}
}

func TestConfigSSHConnectionRoutes_RefuseCrossSiteRequests(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, sshConnectionsPath, ""},
		{http.MethodPost, sshConnectionsPath, `{"name":"x","host":"x.invalid"}`},
		{http.MethodPut, sshConnectionsPath + "/used", `{"host":"x.invalid"}`},
		{http.MethodDelete, sshConnectionsPath + "/secret", ""},
	} {
		t.Run(route.method, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
			before := e.reload(t)
			req := httptest.NewRequest(route.method, route.path, bytes.NewBufferString(route.body))
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			rec := httptest.NewRecorder()

			setupRouterWithHandler(e.h).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.Equal(t, before, e.reload(t))
		})
	}
}

func TestPutConfigSSHConnection_PasswordContract(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantPassword string
	}{
		{name: "left out keeps it", body: `{"host":"moved.invalid","user":"demo"}`, wantPassword: "hunter2-original"},
		{
			name:         "empty keeps it",
			body:         `{"host":"moved.invalid","user":"demo","password":""}`,
			wantPassword: "hunter2-original",
		},
		{
			name:         "a value replaces it",
			body:         `{"host":"moved.invalid","user":"demo","password":"replaced"}`,
			wantPassword: "replaced",
		},
		{
			name:         "clear_password removes it",
			body:         `{"host":"moved.invalid","user":"demo","clear_password":true}`,
			wantPassword: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, fromSSHConfigBlock)

			rec := e.do(t, http.MethodPut, sshConnectionsPath+"/secret", tt.body)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "hunter2-original")
			assert.NotContains(t, rec.Body.String(), "replaced")
			entry := decodeSSHConnectionEntry(t, rec)
			assert.Equal(t, tt.wantPassword != "", entry.HasPassword)
			// The fields the body sets replace the entry's: port, key_file
			// and known_hosts_file were left out, so they are cleared.
			want := config.SSHConnection{Host: "moved.invalid", User: "demo", Password: tt.wantPassword}
			assert.Equal(t, want, e.cfg.SSHConnections["secret"])
			assert.Equal(t, want, e.reload(t)["secret"])
		})
	}
}

func TestPutConfigSSHConnection_Rejections(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		wantError  string
		wantStatus int
	}{
		{
			name:       "unknown entry",
			path:       "/missing",
			body:       `{"host":"x.invalid"}`,
			wantStatus: http.StatusNotFound,
			wantError:  `ssh connection "missing" not found`,
		},
		{
			name:       "malformed JSON",
			path:       "/secret",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "renaming",
			path:       "/secret",
			body:       `{"name":"other","host":"x.invalid"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "cannot be renamed",
		},
		{
			name:       "bad port",
			path:       "/secret",
			body:       `{"host":"x.invalid","port":-1}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "port must be between",
		},
		{
			name: "password and clear_password", path: "/secret",
			body:       `{"host":"x.invalid","password":"p","clear_password":true}`,
			wantStatus: http.StatusUnprocessableEntity, wantError: "clear_password",
		},
		{
			name: "clearing the host with no ssh config block", path: "/used", body: `{"user":"demo"}`,
			wantStatus: http.StatusUnprocessableEntity, wantError: `~/.ssh/config has no Host block named "used"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
			before := e.reload(t)

			rec := e.do(t, http.MethodPut, sshConnectionsPath+tt.path, tt.body)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, responseError(t, rec), tt.wantError)
			assert.NotContains(t, rec.Body.String(), "hunter2-original")
			assert.Equal(t, before, e.cfg.SSHConnections)
			assert.Equal(t, before, e.reload(t))
		})
	}
}

func TestPutConfigSSHConnection_SameNameInBodyIsAccepted(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)

	rec := e.do(t, http.MethodPut, sshConnectionsPath+"/used", `{"name":"used","host":"used2.invalid"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "used2.invalid", e.reload(t)["used"].Host)
}

// An edited entry is dialed again with its new details on the next
// collection, rather than the dashboard going on using the connection it
// opened with the old ones.
func TestPutConfigSSHConnection_DropsTheDashboardConnection(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
	conn := &closeCountingConn{stubTaskConn: stubTaskConn{output: taskCollection("s", "")}}
	useTaskService(e.h, taskCollection("l", ""), map[string]tasks.Conn{"used": conn})
	e.h.taskGitLookup = noTaskGit
	getTasks(t, e.h)
	require.Equal(t, 0, conn.closed)

	rec := e.do(t, http.MethodPut, sshConnectionsPath+"/used", `{"host":"used2.invalid"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, conn.closed)
}

func TestDeleteConfigSSHConnection_RemovesAndPersists(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)

	rec := e.do(t, http.MethodDelete, sshConnectionsPath+"/secret", "")

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.NotContains(t, e.cfg.SSHConnections, "secret")
	assert.NotContains(t, e.reload(t), "secret")
}

func TestDeleteConfigSSHConnection_RefusesToBreakAPane(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)

	rec := e.do(t, http.MethodDelete, sshConnectionsPath+"/used", "")

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, responseError(t, rec), `ssh connection "used" is needed by pane one-ssh`)
	assert.Contains(t, e.reload(t), "used")
}

// A pane whose connection is also a ~/.ssh/config Host block keeps working
// without the entry, so the entry can go.
func TestDeleteConfigSSHConnection_PaneFallsBackToSSHConfig(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock+"Host used\n  HostName used.invalid\n")

	rec := e.do(t, http.MethodDelete, sshConnectionsPath+"/used", "")

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.NotContains(t, e.reload(t), "used")
}

func TestDeleteConfigSSHConnection_UnknownEntryIs404(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)

	rec := e.do(t, http.MethodDelete, sshConnectionsPath+"/missing", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A write that fails leaves the running config as it was: the change is
// neither used nor persisted by the next successful save (issue #204).
func TestConfigSSHConnectionRoutes_SaveFailureRollsBack(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, sshConnectionsPath, `{"name":"gpu","host":"gpu.invalid","password":"typed-secret"}`},
		{http.MethodPut, sshConnectionsPath + "/secret", `{"host":"moved.invalid","password":"typed-secret"}`},
		{http.MethodDelete, sshConnectionsPath + "/secret", ""},
	} {
		t.Run(route.method, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
			before := e.reload(t)
			fileops.SetOpsForTest(t, (&fileops.Spy{RenameErr: errors.New("disk full")}).Ops())

			rec := e.do(t, route.method, route.path, route.body)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.Contains(t, rec.Body.String(), "failed to save ssh_connections")
			assert.NotContains(t, rec.Body.String(), "typed-secret")
			assert.NotContains(t, rec.Body.String(), "hunter2-original")
			assert.Equal(t, before, e.cfg.SSHConnections)
			assert.Equal(t, before, e.reload(t))
		})
	}
}

func TestPostConfigSSHConnection_UnreadableSSHConfig(t *testing.T) {
	e := newSSHConnectionsEnv(t, "")
	e.h.sshConfigPath = t.TempDir() // a directory: opening it succeeds, reading it fails

	rec := e.do(t, http.MethodPost, sshConnectionsPath, `{"name":"nowhere"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "~/.ssh/config could not be read")
}

func noTaskGit(context.Context, string, string, bool) *taskGitInfo { return nil }

// responseError is the error message of a JSON error response.
func responseError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return rec.Body.String()
	}
	return body[responseErrorKey]
}

// A name written by hand in config.yaml can carry characters the new-entry
// rule refuses; the dialog sends it URL-escaped, and the routes still find it.
func TestConfigSSHConnectionRoutes_FindEscapedNames(t *testing.T) {
	for _, tt := range []struct{ name, escaped string }{
		{"deploy@prod", "deploy%40prod"},
		{"h:22", "h%3A22"},
		{"a/b", "a%2Fb"},
		{"a b", "a%20b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
			e.cfg.SSHConnections = map[string]config.SSHConnection{
				"used":  {Host: "used.invalid"}, // one-ssh's connection, so the config reloads
				tt.name: {Host: "x.invalid"},
			}

			rec := e.do(t, http.MethodPut, sshConnectionsPath+"/"+tt.escaped, `{"host":"y.invalid"}`)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, tt.name, decodeSSHConnectionEntry(t, rec).Name)
			assert.Equal(t, "y.invalid", e.reload(t)[tt.name].Host)

			rec = e.do(t, http.MethodDelete, sshConnectionsPath+"/"+tt.escaped, "")
			require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
			assert.NotContains(t, e.reload(t), tt.name)
		})
	}
}

// An entry can be needed without being named: a ~/.ssh/config Host block's
// ProxyJump resolves through ssh_connections too. Deleting it is refused when
// that would leave a pane or another dashboard host unable to resolve.
func TestDeleteConfigSSHConnection_RefusesToBreakAProxyJump(t *testing.T) {
	const jumpBlock = "Host gpu\n  HostName gpu.invalid\n  ProxyJump bastion\n"
	tests := []struct {
		name      string
		prepare   func(e *sshConnectionsEnv)
		wantError string
	}{
		{
			name: "a pane connects through it",
			prepare: func(e *sshConnectionsEnv) {
				e.cfg.Workspaces.Items[0].Layout.Children[0].Pane.Connection = "gpu"
				e.cfg.SSHConnections = map[string]config.SSHConnection{"bastion": {Host: "bastion.invalid"}}
			},
			wantError: `ssh connection "bastion" is needed by pane one-ssh`,
		},
		{
			name: "another dashboard host connects through it",
			prepare: func(e *sshConnectionsEnv) {
				e.cfg.Workspaces.Items[0].Layout.Children[0].Pane.Type = config.PaneTypeLocal
				e.cfg.SSHConnections = map[string]config.SSHConnection{"bastion": {Host: "bastion.invalid"}, "gpu": {}}
			},
			wantError: `ssh connection "bastion" is needed by dashboard host gpu`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSSHConnectionsEnv(t, jumpBlock)
			tt.prepare(e)

			rec := e.do(t, http.MethodDelete, sshConnectionsPath+"/bastion", "")

			assert.Equal(t, http.StatusConflict, rec.Code)
			assert.Contains(t, responseError(t, rec), tt.wantError)
			assert.Contains(t, e.cfg.SSHConnections, "bastion")
		})
	}
}

// The ssh_connections routes and the workspace routes write the same file
// from the same in-memory config. Run under -race, this fails when one of
// them changes the config or writes it while another does; without -race it
// still checks that no route's change is lost from the file.
func TestConfigRoutes_SSHConnectionAndWorkspaceWritesAreSerialized(t *testing.T) {
	e := newSSHConnectionsEnv(t, fromSSHConfigBlock)
	e.cfg.Workspaces.Items = append(e.cfg.Workspaces.Items, config.WorkspaceConfig{
		ID: "two", Title: "Two", Layout: config.LayoutNode{Direction: "horizontal", Children: []config.LayoutChild{
			{Size: 100, Pane: &config.PaneConfig{ID: "two-main", Type: config.PaneTypeLocal}},
		}},
	})
	router := setupRouterWithHandler(e.h)
	send := func(method, path, body string) int {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	const rounds = 20
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range rounds {
			assert.Equal(t, http.StatusCreated,
				send(http.MethodPost, sshConnectionsPath, fmt.Sprintf(`{"name":"host-%d","host":"h%d.invalid"}`, i, i)))
		}
	}()
	go func() {
		defer wg.Done()
		for i := range rounds {
			active := []string{"one", "two"}[i%2]
			assert.Equal(t, http.StatusOK, send(http.MethodPut, "/api/workspaces/active", fmt.Sprintf(`{"id":%q}`, active)))
		}
	}()
	wg.Wait()

	saved := e.reload(t)
	for i := range rounds {
		assert.Contains(t, saved, fmt.Sprintf("host-%d", i))
	}
}
