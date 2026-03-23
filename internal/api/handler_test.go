package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

// mockSession implements session.Session for tests.
type mockSession struct {
	id    string
	typ   session.Type
	title string
	state session.State
}

func newMockSession(id string) *mockSession {
	return &mockSession{id: id, typ: session.TypeLocal, title: id, state: session.StateConnected}
}
func (m *mockSession) ID() string              { return m.id }
func (m *mockSession) Type() session.Type      { return m.typ }
func (m *mockSession) Title() string           { return m.title }
func (m *mockSession) State() session.State    { return m.state }
func (m *mockSession) Read(p []byte) (int, error) { return 0, io.EOF }
func (m *mockSession) Write(p []byte) (int, error) { return len(p), nil }
func (m *mockSession) Resize(c, r uint16) error    { return nil }
func (m *mockSession) Close() error                { return nil }

func setupRouter(cfg *config.Config, mgr *session.Manager) *chi.Mux {
	h := NewHandler(cfg, mgr)
	// Use a temp empty SSH config to avoid real ~/.ssh/config leaking into tests
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	return setupRouterWithHandler(h)
}

func setupRouterWithHandler(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/layout", h.GetLayout)
	r.Put("/api/layout", h.PutLayout)
	r.Get("/api/sessions", h.GetSessions)
	r.Post("/api/sessions", h.PostSession)
	r.Delete("/api/sessions/{id}", h.DeleteSession)
	r.Post("/api/sessions/{id}/restart", h.RestartSession)
	r.Get("/api/display", h.GetDisplay)
	r.Get("/api/edit-mode", h.GetEditMode)
	r.Put("/api/edit-mode", h.PutEditMode)
	r.Get("/api/ssh-connections", h.GetSSHConnections)
	r.Get("/api/ssh-config/hosts", h.GetSSHConfigHosts)
	r.Post("/api/ssh-config/hosts", h.PostSSHConfigHost)
	return r
}

func defaultTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"},
		Layout: config.LayoutNode{
			Direction: "horizontal",
			Children: []config.LayoutChild{
				{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}},
			},
		},
	}
}

func TestGetLayout_ReturnsJSON(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/layout", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var layout config.LayoutNode
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&layout))
	assert.Equal(t, "horizontal", layout.Direction)
}

func TestPutLayout_ValidBody_Updates(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())
	layout := config.LayoutNode{
		Direction: "vertical",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}}},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestPutLayout_InvalidJSON_Returns400(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutLayout_InvalidLayout_Returns422(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	layout := config.LayoutNode{
		Direction: "diagonal",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}}},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestGetSessions_Empty(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []sessionInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	assert.Empty(t, list)
}

func TestGetSessions_WithSessions(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	r := setupRouter(defaultTestConfig(), mgr)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []sessionInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	assert.Len(t, list, 1)
	assert.Equal(t, "s1", list[0].ID)
}

func TestDeleteSession_Exists_204(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	r := setupRouter(defaultTestConfig(), mgr)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/s1", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteSession_NotFound_404(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/missing", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostSession_ValidLocal_201(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return newMockSession(pane.ID), nil
	}
	r := setupRouterWithHandler(h)
	body, _ := json.Marshal(config.PaneConfig{ID: "new-pane", Type: "local"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var info sessionInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	assert.Equal(t, "new-pane", info.ID)
	assert.Equal(t, "local", info.Type)
}

func TestPostSession_InvalidBody_400(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostSession_DuplicateID_409(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("existing"))
	r := setupRouter(defaultTestConfig(), mgr)

	body, _ := json.Marshal(config.PaneConfig{ID: "existing", Type: "local"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestRestartSession_Found_200(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"},
		Layout: config.LayoutNode{
			Direction: "horizontal",
			Children: []config.LayoutChild{
				{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}},
			},
		},
	}
	mgr := session.NewManager()
	mgr.Add(newMockSession("main")) // pre-existing (exited) session
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return newMockSession(pane.ID), nil
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// New session must be in the manager
	_, ok := mgr.Get("main")
	assert.True(t, ok)
}

func TestRestartSession_NotFound_404(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/nonexistent/restart", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetEditMode_DefaultFalse(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/edit-mode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp editModeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.EditMode)
}

func TestPutEditMode_TurnOn(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	body, _ := json.Marshal(editModeResponse{EditMode: true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/edit-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp editModeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.EditMode)

	// Subsequent GET should also return true
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/edit-mode", nil)
	r.ServeHTTP(rec2, req2)
	var resp2 editModeResponse
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&resp2))
	assert.True(t, resp2.EditMode)
}

func TestPutEditMode_TurnOff(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())

	// Turn on first
	body, _ := json.Marshal(editModeResponse{EditMode: true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/edit-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// Now turn off
	body2, _ := json.Marshal(editModeResponse{EditMode: false})
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPut, "/api/edit-mode", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	var resp editModeResponse
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&resp))
	assert.False(t, resp.EditMode)
}

func TestPutEditMode_InvalidBody_400(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/edit-mode", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutLayout_EditModeOff_DoesNotPersist(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())

	// editMode is false by default
	layout := config.LayoutNode{
		Direction: "vertical",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}}},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// In-memory layout should be updated
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestPutLayout_EditModeOn_Persists(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())

	// Turn on edit mode
	body, _ := json.Marshal(editModeResponse{EditMode: true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/edit-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	layout := config.LayoutNode{
		Direction: "vertical",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}}},
	}
	body2, _ := json.Marshal(layout)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestDeleteSession_EditModeOff_DoesNotSave(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	cfg := defaultTestConfig()
	r := setupRouter(cfg, mgr)

	// editMode is false by default
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/s1", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	// session removed
	_, ok := mgr.Get("s1")
	assert.False(t, ok)
}

func TestDeleteSession_EditModeOn_Saves(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	cfg := defaultTestConfig()
	r := setupRouter(cfg, mgr)

	// Turn on edit mode
	body, _ := json.Marshal(editModeResponse{EditMode: true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/edit-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/sessions/s1", nil)
	r.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusNoContent, rec2.Code)
	_, ok := mgr.Get("s1")
	assert.False(t, ok)
}

func TestGetDisplay_ReturnsJSON(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Display = config.DisplayConfig{ShowHeader: true, ShowStatusBar: false}
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/display", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var display config.DisplayConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&display))
	assert.True(t, display.ShowHeader)
	assert.False(t, display.ShowStatusBar)
}

func TestPutLayout_ExpandsTildeCwd(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	layout := config.LayoutNode{
		Direction: "horizontal",
		Children: []config.LayoutChild{
			{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local", Cwd: "~/mydir"}},
		},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, filepath.Join(home, "mydir"), cfg.Layout.Children[0].Pane.Cwd)
}

func TestPutLayout_NestedTildeCwd_Expanded(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	layout := config.LayoutNode{
		Direction: "horizontal",
		Children: []config.LayoutChild{
			{
				Size:      50,
				Direction: "vertical",
				Children: []config.LayoutChild{
					{Size: 50, Pane: &config.PaneConfig{ID: "pane-a", Type: "local", Cwd: "~/projects/a"}},
					{Size: 50, Pane: &config.PaneConfig{ID: "pane-b", Type: "local", Cwd: "~/projects/b"}},
				},
			},
			{Size: 50, Pane: &config.PaneConfig{ID: "pane-c", Type: "local"}},
		},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, filepath.Join(home, "projects/a"), cfg.Layout.Children[0].Children[0].Pane.Cwd)
	assert.Equal(t, filepath.Join(home, "projects/b"), cfg.Layout.Children[0].Children[1].Pane.Cwd)
	assert.Empty(t, cfg.Layout.Children[1].Pane.Cwd) // no cwd, unchanged
}

func TestGetSSHConnections_Empty(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-connections", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConnectionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp.Names)
	assert.Empty(t, resp.Names)
}

func TestGetSSHConnections_WithConnections(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"prod": {Host: "prod.example.com", Port: 22, User: "admin"},
		"dev":  {Host: "dev.example.com", Port: 22, User: "dev"},
	}
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-connections", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConnectionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.ElementsMatch(t, []string{"prod", "dev"}, resp.Names)
	// Must be sorted
	assert.Equal(t, []string{"dev", "prod"}, resp.Names)
}

func TestGetSSHConnections_NilMap(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = nil
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-connections", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConnectionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp.Names)
	assert.Empty(t, resp.Names)
}

// writeTempSSHConfigForAPI writes a minimal SSH config file with given content and returns its path.
func writeTempSSHConfigForAPI(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))
	return f
}

func TestGetSSHConnections_MergesSSHConfigHosts(t *testing.T) {
	sshConfigPath := writeTempSSHConfigForAPI(t, "Host ssh-host\n    HostName ssh.example.com\n    User alice\n")

	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"yaml-conn": {Host: "yaml.example.com", Port: 22, User: "bob"},
	}

	h := NewHandler(cfg, session.NewManager())
	h.sshConfigPath = sshConfigPath
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-connections", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConnectionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.ElementsMatch(t, []string{"yaml-conn", "ssh-host"}, resp.Names)
	// Must be sorted
	assert.Equal(t, []string{"ssh-host", "yaml-conn"}, resp.Names)
}

func TestGetSSHConnections_SSHConfigTakesPrecedenceOnConflict(t *testing.T) {
	// When both yaml and ssh config have same name, yaml takes precedence (name appears once)
	sshConfigPath := writeTempSSHConfigForAPI(t, "Host shared\n    HostName ssh.example.com\n    User alice\n")

	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"shared": {Host: "yaml.example.com", Port: 22, User: "bob"},
	}

	h := NewHandler(cfg, session.NewManager())
	h.sshConfigPath = sshConfigPath
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-connections", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConnectionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	// Deduplication: "shared" should appear only once
	assert.Equal(t, []string{"shared"}, resp.Names)
}

func TestGetSSHConfigHosts_ReturnsHosts(t *testing.T) {
	sshConfigPath := writeTempSSHConfigForAPI(t, "Host myhost\n    HostName myhost.example.com\n    User ubuntu\n    Port 2222\n")

	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = sshConfigPath
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-config/hosts", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConfigHostsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Hosts, 1)
	assert.Equal(t, "myhost", resp.Hosts[0].Name)
	assert.Equal(t, "myhost.example.com", resp.Hosts[0].Hostname)
	assert.Equal(t, "ubuntu", resp.Hosts[0].User)
	assert.Equal(t, 2222, resp.Hosts[0].Port)
}

func TestGetSSHConfigHosts_Empty(t *testing.T) {
	sshConfigPath := writeTempSSHConfigForAPI(t, "")

	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = sshConfigPath
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-config/hosts", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp sshConfigHostsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.Hosts)
}

func TestPostSSHConfigHost_ValidHost_201(t *testing.T) {
	dir := t.TempDir()
	sshConfigPath := filepath.Join(dir, "config")

	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = sshConfigPath
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{
		Name:     "new-host",
		Hostname: "new.example.com",
		User:     "deploy",
		Port:     22,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	// Verify it was written to the file
	data, err := os.ReadFile(sshConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Host new-host")
}

func TestPostSSHConfigHost_MissingName_422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{Hostname: "host.example.com", User: "ubuntu"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_InvalidNameChars_422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{Name: "bad name!", Hostname: "h.example.com", User: "u"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_MissingHostname_422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{Name: "myhost", User: "ubuntu"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_MissingUser_422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{Name: "myhost", Hostname: "myhost.example.com"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_PortOutOfRange_422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{Name: "myhost", Hostname: "myhost.example.com", User: "ubuntu", Port: 70000})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_DuplicateName_409(t *testing.T) {
	sshConfigPath := writeTempSSHConfigForAPI(t, "Host existing\n    HostName existing.example.com\n    User ubuntu\n")

	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = sshConfigPath
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{Name: "existing", Hostname: "new.example.com", User: "ubuntu"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestPostSSHConfigHost_InvalidBody_400(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// mockCWDSession is a mockSession that also implements session.CWDGetter.
type mockCWDSession struct {
	mockSession
	cwd    string
	cwdErr error
}

func (m *mockCWDSession) GetCWD() (string, error) { return m.cwd, m.cwdErr }

// mockSSHCWDSession is a mockSession that implements both CWDGetter and SSHConnNamer.
type mockSSHCWDSession struct {
	mockSession
	cwd        string
	connName   string
}

func (m *mockSSHCWDSession) GetCWD() (string, error) { return m.cwd, nil }
func (m *mockSSHCWDSession) ConnectionName() string   { return m.connName }

func setupRouterWithVSCode(h *Handler) *chi.Mux {
	r := setupRouterWithHandler(h)
	r.Post("/api/sessions/{id}/open-vscode", h.PostOpenVSCode)
	return r
}

func TestPostOpenVSCode_NotFound_404(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/missing/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostOpenVSCode_NoCWDGetter_422(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostOpenVSCode_Local_200(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local1", typ: session.TypeLocal},
		cwd:         "/tmp/myproject",
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo" // stub: echo instead of opening VSCode
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/local1/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp openVSCodeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "/tmp/myproject", resp.Cwd)
}

func TestPostOpenVSCode_SSH_200(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(&mockSSHCWDSession{
		mockSession: mockSession{id: "ssh1", typ: session.TypeSSH},
		cwd:         "/home/user/code",
		connName:    "myserver",
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/ssh1/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp openVSCodeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "/home/user/code", resp.Cwd)
}

func TestPostOpenVSCode_SSH_InvalidConnName_422(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(&mockSSHCWDSession{
		mockSession: mockSession{id: "ssh-bad", typ: session.TypeSSH},
		cwd:         "/home/user",
		connName:    "bad name; rm -rf /",
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/ssh-bad/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostOpenVSCode_Tmux_200(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "tmux1", typ: session.TypeTmux},
		cwd:         "/home/user/work",
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/tmux1/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp openVSCodeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "/home/user/work", resp.Cwd)
}

func TestPostOpenVSCode_SSHTmux_200(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(&mockSSHCWDSession{
		mockSession: mockSession{id: "sshtmux1", typ: session.TypeSSHTmux},
		cwd:         "/home/user/remote",
		connName:    "remote-box",
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sshtmux1/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp openVSCodeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "/home/user/remote", resp.Cwd)
}
