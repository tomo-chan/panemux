package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

// mockSession implements session.Session for tests.
//
//nolint:govet // test helper layout is not performance-sensitive
type mockSession struct {
	buf     chan []byte
	id      string
	typ     session.Type
	title   string
	state   session.State
	bufOnce sync.Once
	closed  bool
}

func (m *mockSession) ensureBuf() {
	m.bufOnce.Do(func() {
		m.buf = make(chan []byte, 64)
	})
}

func newMockSession(id string) *mockSession {
	m := &mockSession{id: id, typ: session.TypeLocal, title: id, state: session.StateConnected}
	m.ensureBuf()
	return m
}
func (m *mockSession) ID() string           { return m.id }
func (m *mockSession) Type() session.Type   { return m.typ }
func (m *mockSession) Title() string        { return m.title }
func (m *mockSession) State() session.State { return m.state }
func (m *mockSession) Read(p []byte) (int, error) {
	m.ensureBuf()
	data, ok := <-m.buf
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, data)
	return n, nil
}
func (m *mockSession) Write(p []byte) (int, error) {
	m.ensureBuf()
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	m.buf <- cp
	return len(p), nil
}
func (m *mockSession) Resize(c, r uint16) error { return nil }
func (m *mockSession) Close() error {
	if !m.closed {
		m.closed = true
		close(m.buf)
	}
	return nil
}

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
	r.Get("/api/workspaces", h.GetWorkspaces)
	r.Post("/api/workspaces", h.PostWorkspace)
	r.Put("/api/workspaces/active", h.PutActiveWorkspace)
	r.Put("/api/workspaces/tab-position", h.PutWorkspaceTabPosition)
	r.Put("/api/workspaces/vertical-bar-width", h.PutWorkspaceVerticalBarWidth)
	r.Put("/api/workspaces/{id}", h.PutWorkspace)
	r.Delete("/api/workspaces/{id}", h.DeleteWorkspace)
	r.Put("/api/workspaces/{id}/layout", h.PutWorkspaceLayout)
	r.Get("/api/sessions", h.GetSessions)
	r.Post("/api/sessions", h.PostSession)
	r.Delete("/api/sessions/{id}", h.DeleteSession)
	r.Post("/api/sessions/{id}/restart", h.RestartSession)
	r.Get("/api/sessions/{id}/git-info", h.GetGitInfo)
	r.Get("/api/display", h.GetDisplay)
	r.Get("/api/ssh-connections", h.GetSSHConnections)
	r.Get("/api/ssh-config/hosts", h.GetSSHConfigHosts)
	r.Post("/api/ssh-config/hosts", h.PostSSHConfigHost)
	r.Get("/api/detect-shell", h.GetDetectShell)
	r.Get("/api/directories", h.GetDirectories)
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

func workspaceTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"},
		Workspaces: config.WorkspacesConfig{
			Active:           "one",
			TabPosition:      "top",
			VerticalBarWidth: 280,
			Items: []config.WorkspaceConfig{
				{
					ID:    "one",
					Title: "One",
					Layout: config.LayoutNode{
						Direction: "horizontal",
						Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "one-main", Type: "local"}}},
					},
				},
				{
					ID:    "two",
					Title: "Two",
					Layout: config.LayoutNode{
						Direction: "vertical",
						Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "two-main", Type: "local"}}},
					},
				},
			},
		},
	}
}

func loadWorkspaceTestConfigFromFile(t *testing.T) (*config.Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `
server:
  port: 8080
  host: "127.0.0.1"
workspaces:
  active: one
  tab_position: top
  vertical_bar_width: 280
  items:
    - id: one
      title: One
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: one-main
              type: local
    - id: two
      title: Two
      layout:
        direction: vertical
        children:
          - size: 100
            pane:
              id: two-main
              type: local
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg, path
}

func assertWorkspaceSettingRejected(
	t *testing.T,
	r *chi.Mux,
	path string,
	body string,
	assertState func(t *testing.T),
	wantError string,
) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertState(t)
	assert.Contains(t, rec.Body.String(), wantError)
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

func TestGetLayout_ReturnsActiveWorkspaceLayout(t *testing.T) {
	cfg := workspaceTestConfig()
	require.True(t, cfg.SetActiveWorkspace("two"))
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/layout", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var layout config.LayoutNode
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&layout))
	assert.Equal(t, "vertical", layout.Direction)
	assert.Equal(t, "two-main", layout.Children[0].Pane.ID)
}

func TestGetWorkspaces_ReturnsJSON(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var workspaces config.WorkspacesConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&workspaces))
	assert.Equal(t, "default", workspaces.Active)
	assert.Equal(t, "top", workspaces.TabPosition)
	assert.Equal(t, 280, workspaces.VerticalBarWidth)
	require.Len(t, workspaces.Items, 1)
	assert.Equal(t, "Default", workspaces.Items[0].Title)
}

func TestPutActiveWorkspace_UpdatesActiveLayout(t *testing.T) {
	cfg := workspaceTestConfig()
	r := setupRouter(cfg, session.NewManager())
	body, _ := json.Marshal(activeWorkspaceRequest{ID: "two"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/active", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "two", cfg.Workspaces.Active)
	assert.Equal(t, "vertical", cfg.ActiveLayout().Direction)
}

func TestPutActiveWorkspace_PersistsWithoutEditMode(t *testing.T) {
	cfg, path := loadWorkspaceTestConfigFromFile(t)
	r := setupRouter(cfg, session.NewManager())
	body, _ := json.Marshal(activeWorkspaceRequest{ID: "two"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/active", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "two", loaded.Workspaces.Active)
}

func TestPutActiveWorkspace_InvalidBody_Returns400(t *testing.T) {
	r := setupRouter(workspaceTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/active", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutActiveWorkspace_NotFound_Returns404AndKeepsActive(t *testing.T) {
	cfg := workspaceTestConfig()
	r := setupRouter(cfg, session.NewManager())
	body, _ := json.Marshal(activeWorkspaceRequest{ID: "missing"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/active", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "one", cfg.Workspaces.Active)
}

func TestPutWorkspaceTabPosition_UpdatesEveryValidPositionAndPersists(t *testing.T) {
	for _, position := range []string{"top", "bottom", "left", "right"} {
		t.Run(position, func(t *testing.T) {
			cfg, path := loadWorkspaceTestConfigFromFile(t)
			h := NewHandler(cfg, session.NewManager())
			h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
			r := setupRouterWithHandler(h)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodPut,
				"/api/workspaces/tab-position",
				bytes.NewBufferString(`{"tab_position":"`+position+`"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			var workspaces config.WorkspacesConfig
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&workspaces))
			assert.Equal(t, position, workspaces.TabPosition)

			loaded, err := config.Load(path)
			require.NoError(t, err)
			assert.Equal(t, position, loaded.Workspaces.TabPosition)
		})
	}
}

func TestPutWorkspaceTabPosition_InvalidBody_Returns400(t *testing.T) {
	h := NewHandler(workspaceTestConfig(), session.NewManager())
	r := setupRouterWithHandler(h)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/tab-position", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutWorkspaceTabPosition_InvalidPosition_Returns422AndKeepsExistingValue(t *testing.T) {
	cfg := workspaceTestConfig()
	h := NewHandler(cfg, session.NewManager())
	r := setupRouterWithHandler(h)
	assertWorkspaceSettingRejected(
		t,
		r,
		"/api/workspaces/tab-position",
		`{"tab_position":"diagonal"}`,
		func(t *testing.T) {
			assert.Equal(t, "top", cfg.Workspaces.TabPosition)
		},
		"invalid tab_position",
	)
}

func TestPutWorkspaceVerticalBarWidth_UpdatesAndPersists(t *testing.T) {
	cfg, path := loadWorkspaceTestConfigFromFile(t)
	h := NewHandler(cfg, session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/workspaces/vertical-bar-width",
		bytes.NewBufferString(`{"vertical_bar_width":320}`),
	)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var workspaces config.WorkspacesConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&workspaces))
	assert.Equal(t, 320, workspaces.VerticalBarWidth)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 320, loaded.Workspaces.VerticalBarWidth)
}

func TestPutWorkspaceVerticalBarWidth_InvalidBody_Returns400(t *testing.T) {
	h := NewHandler(workspaceTestConfig(), session.NewManager())
	r := setupRouterWithHandler(h)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/vertical-bar-width", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutWorkspaceVerticalBarWidth_InvalidWidth_Returns422AndKeepsExistingValue(t *testing.T) {
	cfg := workspaceTestConfig()
	h := NewHandler(cfg, session.NewManager())
	r := setupRouterWithHandler(h)
	assertWorkspaceSettingRejected(
		t,
		r,
		"/api/workspaces/vertical-bar-width",
		`{"vertical_bar_width":120}`,
		func(t *testing.T) {
			assert.Equal(t, 280, cfg.Workspaces.VerticalBarWidth)
		},
		"invalid vertical_bar_width",
	)
}

func TestPostWorkspace_AddsDefaultLocalWorkspaceAndPersists(t *testing.T) {
	cfg, path := loadWorkspaceTestConfigFromFile(t)
	mgr := session.NewManager()
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return newMockSession(pane.ID), nil
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	var workspaces config.WorkspacesConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&workspaces))
	require.Len(t, workspaces.Items, 3)
	added := workspaces.Items[2]
	assert.Equal(t, "workspace-3", added.ID)
	assert.Equal(t, "Workspace 3", added.Title)
	assert.Equal(t, "workspace-3", workspaces.Active)
	require.Len(t, added.Layout.Children, 1)
	assert.Equal(t, "workspace-3-main", added.Layout.Children[0].Pane.ID)
	assert.Equal(t, "local", added.Layout.Children[0].Pane.Type)
	_, ok := mgr.Get("workspace-3-main")
	assert.True(t, ok)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "workspace-3", loaded.Workspaces.Active)
	require.Len(t, loaded.Workspaces.Items, 3)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "\nlayout:")
}

func TestDeleteWorkspace_NotFound_Returns404(t *testing.T) {
	cfg := workspaceTestConfig()
	h := NewHandler(cfg, session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/missing", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteWorkspace_LastWorkspace_Returns409(t *testing.T) {
	cfg := defaultTestConfig()
	h := NewHandler(cfg, session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/default", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestDeleteWorkspace_RemovesWorkspaceSessionsAndPersists(t *testing.T) {
	cfg, path := loadWorkspaceTestConfigFromFile(t)
	require.True(t, cfg.SetActiveWorkspace("two"))
	mgr := session.NewManager()
	mgr.Add(newMockSession("one-main"))
	mgr.Add(newMockSession("two-main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/two", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var workspaces config.WorkspacesConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&workspaces))
	assert.Equal(t, "one", workspaces.Active)
	require.Len(t, workspaces.Items, 1)
	assert.Equal(t, "one", workspaces.Items[0].ID)
	_, ok := mgr.Get("two-main")
	assert.False(t, ok)
	_, ok = mgr.Get("one-main")
	assert.True(t, ok)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "one", loaded.Workspaces.Active)
	require.Len(t, loaded.Workspaces.Items, 1)
}

func TestDeleteWorkspace_ClearsPreferredCWDForRemovedPanes(t *testing.T) {
	cfg, _ := loadWorkspaceTestConfigFromFile(t)
	require.True(t, cfg.SetActiveWorkspace("two"))
	mgr := session.NewManager()
	mgr.Add(newMockSession("one-main"))
	mgr.Add(newMockSession("two-main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.preferredCWDBySession["two-main"] = []preferredCWDState{{
		CWD:       "/tmp/worktree",
		CommonDir: "/repo/.git",
		Root:      "/tmp/worktree",
	}}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/two", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	_, ok := h.preferredCWDBySession["two-main"]
	assert.False(t, ok)
}

func TestPutWorkspace_RenamesWorkspaceAndPersists(t *testing.T) {
	cfg, path := loadWorkspaceTestConfigFromFile(t)
	h := NewHandler(cfg, session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	body := bytes.NewBufferString(`{"title":"Renamed Workspace"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/one", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var workspaces config.WorkspacesConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&workspaces))
	assert.Equal(t, "Renamed Workspace", workspaces.Items[0].Title)
	assert.Equal(t, "Two", workspaces.Items[1].Title)
	assert.Equal(t, "one", workspaces.Active)
	assert.Equal(t, "horizontal", cfg.ActiveLayout().Direction)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "Renamed Workspace", loaded.Workspaces.Items[0].Title)
}

func TestPutWorkspace_InvalidBody_Returns400(t *testing.T) {
	h := NewHandler(workspaceTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/one", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutWorkspace_BlankTitle_Returns422(t *testing.T) {
	h := NewHandler(workspaceTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	body := bytes.NewBufferString(`{"title":"   "}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/one", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPutWorkspace_NotFound_Returns404(t *testing.T) {
	h := NewHandler(workspaceTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	r := setupRouterWithHandler(h)

	body := bytes.NewBufferString(`{"title":"Missing"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/missing", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPutWorkspaceLayout_UpdatesOnlyTargetWorkspace(t *testing.T) {
	cfg := workspaceTestConfig()
	r := setupRouter(cfg, session.NewManager())
	layout := config.LayoutNode{
		Direction: "horizontal",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "two-main", Type: "local"}}},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/two/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "horizontal", cfg.Workspaces.Items[1].Layout.Direction)
	assert.Equal(t, "horizontal", cfg.Workspaces.Items[0].Layout.Direction)
}

func TestPutWorkspaceLayout_ActiveWorkspaceAlsoUpdatesCompatibilityLayout(t *testing.T) {
	cfg := workspaceTestConfig()
	r := setupRouter(cfg, session.NewManager())
	layout := config.LayoutNode{
		Direction: "vertical",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "one-main", Type: "local"}}},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/one/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "vertical", cfg.Workspaces.Items[0].Layout.Direction)
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestPutWorkspaceLayout_InvalidBody_Returns400(t *testing.T) {
	r := setupRouter(workspaceTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces/one/layout", bytes.NewBufferString("not json"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutWorkspaceLayout_InvalidLayout_Returns422(t *testing.T) {
	r := setupRouter(workspaceTestConfig(), session.NewManager())
	layout := config.LayoutNode{
		Direction: "diagonal",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "one-main", Type: "local"}}},
	}
	rec := putLayout(t, r, "/api/workspaces/one/layout", layout)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPutWorkspaceLayout_NotFound_Returns404(t *testing.T) {
	r := setupRouter(workspaceTestConfig(), session.NewManager())
	layout := config.LayoutNode{
		Direction: "horizontal",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "missing-main", Type: "local"}}},
	}
	rec := putLayout(t, r, "/api/workspaces/missing/layout", layout)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPutWorkspaceLayout_PersistsWorkspaces(t *testing.T) {
	cfg, path := loadWorkspaceTestConfigFromFile(t)
	r := setupRouter(cfg, session.NewManager())

	layout := config.LayoutNode{
		Direction: "vertical",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "one-main", Type: "local"}}},
	}
	rec2 := putLayout(t, r, "/api/workspaces/one/layout", layout)
	require.Equal(t, http.StatusOK, rec2.Code)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "workspaces:")
	assert.Contains(t, string(data), "one-main")
	assert.NotContains(t, string(data), "\nlayout:")
}

func TestPutLayout_ValidBody_Updates(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())
	rec := putVerticalLayout(t, r)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestPutLayout_UpdatesActiveWorkspaceOnly(t *testing.T) {
	cfg := workspaceTestConfig()
	require.True(t, cfg.SetActiveWorkspace("two"))
	r := setupRouter(cfg, session.NewManager())
	layout := config.LayoutNode{
		Direction: "horizontal",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "two-main", Type: "local"}}},
	}
	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/layout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "horizontal", cfg.Workspaces.Items[1].Layout.Direction)
	assert.Equal(t, "horizontal", cfg.Layout.Direction)
	assert.Equal(t, "horizontal", cfg.Workspaces.Items[0].Layout.Direction)
}

func putVerticalLayout(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	layout := config.LayoutNode{
		Direction: "vertical",
		Children:  []config.LayoutChild{{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}}},
	}
	return putLayout(t, r, "/api/layout", layout)
}

func putLayout(t *testing.T, r http.Handler, path string, layout config.LayoutNode) *httptest.ResponseRecorder {
	t.Helper()

	body, _ := json.Marshal(layout)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
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
	rec := putLayout(t, r, "/api/layout", layout)
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

func TestRestartSession_ClearsPreferredCWD(t *testing.T) {
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
	mgr.Add(newMockSession("main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.preferredCWDBySession["main"] = []preferredCWDState{{
		CWD:       "/tmp/worktree",
		CommonDir: "/repo/.git",
		Root:      "/tmp/worktree",
	}}
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return newMockSession(pane.ID), nil
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	_, ok := h.preferredCWDBySession["main"]
	assert.False(t, ok)
}

func TestRestartSession_NotFound_404(t *testing.T) {
	r := setupRouter(defaultTestConfig(), session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/nonexistent/restart", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRestartSession_CreateFails_OldSessionStaysRegistered(t *testing.T) {
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
	original := newMockSession("main")
	mgr.Add(original)
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("dial failed")
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	// The pre-existing session must still be registered under the same ID
	// so subsequent /ws and /git-info requests do not 404 permanently.
	got, ok := mgr.Get("main")
	assert.True(t, ok)
	assert.Same(t, session.Session(original), got)
}

func TestRestartSession_CreateFails_PreservesPreferredCWD(t *testing.T) {
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
	mgr.Add(newMockSession("main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.preferredCWDBySession["main"] = []preferredCWDState{{
		CWD:       "/remote/home/demo",
		CommonDir: "/remote/home/demo/.git",
		Root:      "/remote/home/demo",
	}}
	h.createSession = func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("dial failed")
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	_, ok := h.preferredCWDBySession["main"]
	assert.True(t, ok)
}

func TestRestartSession_CreateFails_500Body(t *testing.T) {
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
	mgr.Add(newMockSession("main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("dial tcp: lookup host.example: no such host")
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "no such host")
}

func TestRestartSession_CreateFails_NoPanicWhenNoPriorSession(t *testing.T) {
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
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("dial failed")
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	assert.NotPanics(t, func() {
		r.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	_, ok := mgr.Get("main")
	assert.False(t, ok)
}

// TestRestartSession_ConcurrentRequests_SecondReturns409 guards against a
// race the create-first reordering (TestRestartSession_CreateFails_*) newly
// exposed: two concurrent /restart calls for the same pane each build their
// own replacement session independently, and whichever finishes last wins
// the manager swap while the other's freshly-created (and never-used)
// session is orphaned/leaked. A per-id in-flight guard rejects the second
// concurrent call instead.
func TestRestartSession_ConcurrentRequests_SecondReturns409(t *testing.T) {
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
	mgr.Add(newMockSession("main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")

	started := make(chan struct{})
	proceed := make(chan struct{})
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		close(started)
		<-proceed
		return newMockSession(pane.ID), nil
	}
	r := setupRouterWithHandler(h)

	firstDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
		r.ServeHTTP(rec, req)
		firstDone <- rec.Code
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first restart to start")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
	r.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusConflict, rec2.Code)

	close(proceed)

	select {
	case code := <-firstDone:
		assert.Equal(t, http.StatusOK, code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first restart to finish")
	}

	_, ok := mgr.Get("main")
	assert.True(t, ok)
}

// TestRestartSession_GuardReleasedAfterCompletion verifies the per-id
// in-flight guard is released once a restart finishes (success or failure),
// so it never permanently locks a pane out of future restarts.
func TestRestartSession_GuardReleasedAfterCompletion(t *testing.T) {
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
	mgr.Add(newMockSession("main"))
	h := NewHandler(cfg, mgr)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("dial failed")
	}
	r := setupRouterWithHandler(h)

	for range 2 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil)
		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	}
}

func TestPutLayout_PersistsImmediately(t *testing.T) {
	cfg := defaultTestConfig()
	r := setupRouter(cfg, session.NewManager())
	rec := putVerticalLayout(t, r)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestDeleteSession_RemovesSessionAndSaves(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	cfg := defaultTestConfig()
	r := setupRouter(cfg, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/s1", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	_, ok := mgr.Get("s1")
	assert.False(t, ok)
}

func TestDeleteSession_ClearsPreferredCWD(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	cfg := defaultTestConfig()
	h := NewHandler(cfg, mgr)
	h.preferredCWDBySession["s1"] = []preferredCWDState{{
		CWD:       "/tmp/worktree",
		CommonDir: "/repo/.git",
		Root:      "/tmp/worktree",
	}}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/s1", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	_, ok := h.preferredCWDBySession["s1"]
	assert.False(t, ok)
}

func TestGetDisplay_ReturnsJSON(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Display = config.DisplayConfig{ShowHeader: true, ShowStatusBar: true}
	r := setupRouter(cfg, session.NewManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/display", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var display config.DisplayConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&display))
	assert.True(t, display.ShowHeader)
	assert.True(t, display.ShowStatusBar)
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
	sshConfigPath := writeTempSSHConfigForAPI(
		t,
		"Host myhost\n    HostName myhost.example.com\n    User ubuntu\n    Port 2222\n",
	)

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
	rec := postSSHConfigHost(t, sshConfigHostRequest{Hostname: "host.example.com", User: "ubuntu"})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func postSSHConfigHost(t *testing.T, body sshConfigHostRequest) *httptest.ResponseRecorder {
	t.Helper()

	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	payload, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ssh-config/hosts", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
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
	rec := postSSHConfigHost(t, sshConfigHostRequest{Name: "myhost", User: "ubuntu"})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_MissingUser_422(t *testing.T) {
	rec := postSSHConfigHost(t, sshConfigHostRequest{Name: "myhost", Hostname: "myhost.example.com"})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostSSHConfigHost_PortOutOfRange_422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(t.TempDir(), "config")
	r := setupRouterWithHandler(h)

	body, _ := json.Marshal(sshConfigHostRequest{
		Name:     "myhost",
		Hostname: "myhost.example.com",
		User:     "ubuntu",
		Port:     70000,
	})
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
//
//nolint:govet // test helper layout is not performance-sensitive
type mockCWDSession struct {
	activeErr error
	cwdErr    error
	cwd       string
	mockSession
	activeWorkdirs []string
	getCWDCalls    int
}

func (m *mockCWDSession) GetCWD() (string, error) {
	m.getCWDCalls++
	return m.cwd, m.cwdErr
}
func (m *mockCWDSession) GetActiveWorkdirs() ([]string, error) {
	return m.activeWorkdirs, m.activeErr
}

// mockSSHCWDSession is a mockSession that implements both CWDGetter and SSHConnNamer.
//
//nolint:govet // test helper layout is not performance-sensitive
type mockSSHCWDSession struct {
	connName string
	cwd      string
	mockSession
}

func (m *mockSSHCWDSession) GetCWD() (string, error) { return m.cwd, nil }
func (m *mockSSHCWDSession) ConnectionName() string  { return m.connName }

// mockRemoteGitSession is a mock SSH-like session that resolves git metadata
// on the remote host instead of through the local filesystem.
//
//nolint:govet // test helper layout is not performance-sensitive
type mockRemoteGitSession struct {
	activeErr   error
	cwdErr      error
	gitContexts map[string]session.GitContext
	gitErrs     map[string]error
	cwd         string
	mockSession
	activeWorkdirs []string
}

func (m *mockRemoteGitSession) GetCWD() (string, error) { return m.cwd, m.cwdErr }
func (m *mockRemoteGitSession) GetActiveWorkdirs() ([]string, error) {
	return m.activeWorkdirs, m.activeErr
}
func (m *mockRemoteGitSession) InspectGitContext(cwd string) (session.GitContext, error) {
	if err, ok := m.gitErrs[cwd]; ok {
		return session.GitContext{}, err
	}
	ctx, ok := m.gitContexts[cwd]
	if !ok {
		return session.GitContext{}, session.NewGitContextError(
			"ssh",
			"git rev-parse --show-toplevel",
			cwd,
			session.GitContextCauseNotGitRepo,
			errors.New("Process exited with status 128"),
			"fatal: not a git repository (or any of the parent directories): .git",
		)
	}
	return ctx, nil
}

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
	dir := t.TempDir()
	resp := postOpenVSCodeOK(t, "local1", &mockCWDSession{
		mockSession: mockSession{id: "local1", typ: session.TypeLocal},
		cwd:         dir,
	})

	assert.Equal(t, dir, resp.Cwd)
}

func TestPostOpenVSCode_ActiveAgentWorkdir_PrefersWorktree(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeDir := addTempGitWorktree(t, repoDir, "feature/open-in-worktree")

	resp := postOpenVSCodeOK(t, "local-worktree", &mockCWDSession{
		mockSession:    mockSession{id: "local-worktree", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeDir},
		cwd:            repoDir,
	})

	assert.Equal(t, worktreeDir, resp.Cwd)
}

func TestPostOpenVSCode_EndedAgentFallsBackToPaneCWD(t *testing.T) {
	repoDir := initTempGitRepo(t)
	_ = addTempGitWorktree(t, repoDir, "feature/open-in-worktree")

	resp := postOpenVSCodeOK(t, "local-fallback", &mockCWDSession{
		mockSession:    mockSession{id: "local-fallback", typ: session.TypeLocal},
		activeWorkdirs: nil,
		cwd:            repoDir,
	})

	assert.Equal(t, repoDir, resp.Cwd)
}

func TestPostOpenVSCode_EndedAgentKeepsLastWorktree(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeDir := addTempGitWorktree(t, repoDir, "feature/open-in-worktree")

	sess := &mockCWDSession{
		mockSession:    mockSession{id: "local-sticky", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeDir},
		cwd:            repoDir,
	}

	mgr := session.NewManager()
	mgr.Add(sess)
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	resp := postOpenVSCodeOKWithRouter(t, r, "local-sticky")
	assert.Equal(t, worktreeDir, resp.Cwd)

	sess.activeWorkdirs = nil

	resp = postOpenVSCodeOKWithRouter(t, r, "local-sticky")
	assert.Equal(t, worktreeDir, resp.Cwd)
}

func TestPostOpenVSCode_StaleStickyWorktreeFallsBackToPaneCWD(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeDir := addTempGitWorktree(t, repoDir, "feature/open-in-worktree")

	sess := &mockCWDSession{
		mockSession:    mockSession{id: "local-open-stale", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeDir},
		cwd:            repoDir,
	}

	mgr := session.NewManager()
	mgr.Add(sess)
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	resp := postOpenVSCodeOKWithRouter(t, r, "local-open-stale")
	assert.Equal(t, worktreeDir, resp.Cwd)

	sess.activeWorkdirs = nil
	out, err := exec.Command( //nolint:gosec // trusted test args
		"git",
		"-C",
		repoDir,
		"worktree",
		"remove",
		worktreeDir,
		"--force",
	).CombinedOutput()
	require.NoError(t, err, "git worktree remove failed: %s", string(out))

	resp = postOpenVSCodeOKWithRouter(t, r, "local-open-stale")
	assert.Equal(t, repoDir, resp.Cwd)
	_, ok := h.preferredCWDBySession["local-open-stale"]
	assert.False(t, ok)
}

func TestResolveActiveGitContexts_ReusesInspectedContext(t *testing.T) {
	sess := &mockRemoteGitSession{
		mockSession:    mockSession{id: "pane-ctx", typ: session.TypeSSH},
		activeWorkdirs: []string{"/repo/base-worktree"},
		cwd:            "/repo/base",
		gitContexts: map[string]session.GitContext{
			"/repo/base": {
				Branch:    "main",
				CommonDir: "/repo/.git",
				Repo:      "base",
				Root:      "/repo/base",
			},
			"/repo/base-worktree": {
				Branch:    "feature/worktree",
				CommonDir: "/repo/.git",
				Repo:      "base",
				Root:      "/repo/base-worktree",
			},
		},
	}

	h := NewHandler(defaultTestConfig(), session.NewManager())
	contexts, err := h.resolveActiveGitContexts(sess, sess.cwd)
	require.NoError(t, err)
	require.Len(t, contexts, 1)
	assert.Equal(t, "/repo/base-worktree", contexts[0].CWD)
	assert.Equal(t, "feature/worktree", contexts[0].Ctx.Branch)
}

func postOpenVSCodeOK(t *testing.T, id string, sess session.Session) openVSCodeResponse {
	t.Helper()

	mgr := session.NewManager()
	mgr.Add(sess)
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	return postOpenVSCodeOKWithRouter(t, r, id)
}

func postOpenVSCodeOKWithRouter(t *testing.T, r *chi.Mux, id string) openVSCodeResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp openVSCodeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

func TestPostOpenVSCode_Local_DeletedDir_422(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Remove(dir)) // delete it so Stat fails
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-del", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.codeBinaryPath = "/bin/echo"
	r := setupRouterWithVSCode(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/local-del/open-vscode", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostOpenVSCode_SSH_200(t *testing.T) {
	resp := postOpenVSCodeOK(t, "ssh1", &mockSSHCWDSession{
		mockSession: mockSession{id: "ssh1", typ: session.TypeSSH},
		cwd:         "/home/user/code",
		connName:    "myserver",
	})

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
	dir := t.TempDir()
	resp := postOpenVSCodeOK(t, "tmux1", &mockCWDSession{
		mockSession: mockSession{id: "tmux1", typ: session.TypeTmux},
		cwd:         dir,
	})

	assert.Equal(t, dir, resp.Cwd)
}

func TestPostOpenVSCode_SSHTmux_200(t *testing.T) {
	resp := postOpenVSCodeOK(t, "sshtmux1", &mockSSHCWDSession{
		mockSession: mockSession{id: "sshtmux1", typ: session.TypeSSHTmux},
		cwd:         "/home/user/remote",
		connName:    "remote-box",
	})

	assert.Equal(t, "/home/user/remote", resp.Cwd)
}

func TestGetDetectShell_Local_Success(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "nonexistent")
	h.detectLocalShellFn = func() (string, error) { return "/usr/bin/zsh", nil }
	h.detectRemoteShellFn = func(cfg session.SSHConfig) (string, error) {
		return "", errors.New("should not be called")
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/detect-shell", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Shell string `json:"shell"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "/usr/bin/zsh", resp.Shell)
}

func TestGetDetectShell_SSH_Success(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"myhost": {Host: "myhost.example.com", User: "admin"},
	}
	h := NewHandler(cfg, session.NewManager())
	h.detectRemoteShellFn = func(sshCfg session.SSHConfig) (string, error) {
		return "/bin/bash", nil
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/detect-shell?connection=myhost", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Shell string `json:"shell"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "/bin/bash", resp.Shell)
}

func TestGetDetectShell_SSH_ConnectionNotFound(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-empty-ssh-cfg")
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/detect-shell?connection=nonexistent", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetDetectShell_DetectFails(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.detectLocalShellFn = func() (string, error) { return "", errors.New("cannot detect") }
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/detect-shell", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// initTempGitRepo creates a temporary directory with an initialized git repo
// on a branch named "main" and returns the directory path.
func initTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main", dir},
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "config", "commit.gpgsign", "false"},
		{"git", "-C", dir, "remote", "add", "origin", "git@github.com:example/panemux.git"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput() //nolint:gosec // G204: trusted test args
		require.NoError(t, err, "git init step failed: %s", string(out))
	}
	return dir
}

func addTempGitWorktree(t *testing.T, repoDir, branchName string) string {
	t.Helper()
	worktreeDir := filepath.Join(t.TempDir(), "worktree")
	out, err := exec.Command( //nolint:gosec // G204: trusted test args
		"git",
		"-C",
		repoDir,
		"worktree",
		"add",
		worktreeDir,
		"-b",
		branchName,
	).CombinedOutput()
	require.NoError(t, err, "git worktree add failed: %s", string(out))
	return worktreeDir
}

func writeFakeGHBinary(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	require.NoError(t, os.Chmod(path, 0755))
	return path
}

func setupRouterWithGitInfo(h *Handler) *chi.Mux {
	r := setupRouterWithHandler(h)
	r.Get("/api/sessions/{id}/git-info", h.GetGitInfo)
	return r
}

func TestGetGitInfo_NotFound_404(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/missing/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetGitInfo_NoCWDGetter_IsGitFalse(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newMockSession("s1"))
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/s1/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.IsGit)
}

func TestGetGitInfo_NotAGitRepo_IsGitFalse(t *testing.T) {
	dir := t.TempDir() // plain directory, no git
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local1", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local1/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.IsGit)
}

func TestGetGitInfo_NotAGitRepo_LogsCauseAndRemediation(t *testing.T) {
	dir := t.TempDir()
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-log", typ: session.TypeLocal},
		cwd:         dir,
	})

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-log/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, buf.String(), `source="base context lookup"`)
	assert.Contains(t, buf.String(), `cause="current directory is outside a Git repository"`)
	assert.Contains(
		t,
		buf.String(),
		`remediation="move the pane to a Git repository or worktree directory, `+
			`or update the pane cwd to the repository root"`,
	)
	assert.Contains(t, buf.String(), `operation="git rev-parse --show-toplevel"`)
}

func TestGetGitInfo_IsGitRepo_ReturnsBranchAndRepo(t *testing.T) {
	dir := initTempGitRepo(t)
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local2", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local2/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	assert.NotEmpty(t, resp.Repo)
	assert.Equal(t, "https://github.com/example/panemux", resp.RepoURL)
}

func TestGetGitInfo_IsGitRepo_WithLinkedPR_ReturnsPRInfo(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command( //nolint:gosec // G204: trusted test args
		"git",
		"-C",
		dir,
		"checkout",
		"-b",
		"feature/pane-pr-link",
	).CombinedOutput()
	require.NoError(t, err, "git checkout failed: %s", string(out))

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-pr", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/123\",\"number\":123}'\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-pr/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "feature/pane-pr-link", resp.Branch)
	assert.Equal(t, "https://github.com/example/panemux", resp.RepoURL)
	assert.Equal(t, "https://github.com/example/panemux/pull/123", resp.PRURL)
	assert.Equal(t, 123, resp.PRNumber)
}

func TestGetGitInfo_SubdirOfGitRepo_ReturnsBranchAndRepo(t *testing.T) {
	dir := initTempGitRepo(t)
	subdir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(subdir, 0755))

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "sub1", typ: session.TypeLocal},
		cwd:         subdir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sub1/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
}

func TestGetGitInfo_PRLookupFails_StillReturnsGitInfo(t *testing.T) {
	dir := initTempGitRepo(t)
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-pr-miss", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(t, "#!/bin/sh\nexit 1\n")
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-pr-miss/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	assert.Empty(t, resp.PRURL)
	assert.Zero(t, resp.PRNumber)
}

func TestGetGitInfo_DetachedHead_StillReturnsGitInfo(t *testing.T) {
	dir := initTempGitRepo(t)
	out, err := exec.Command("git", "-C", dir, "checkout", "HEAD~0").CombinedOutput() //nolint:gosec // trusted test args
	require.NoError(t, err, "git checkout detached head failed: %s", string(out))

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "detached", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/detached/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "", resp.Branch)
	assert.NotEmpty(t, resp.Repo)
}

func TestLookupPRInfo_TimesOutAndFallsBack(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.ghBinaryPath = writeFakeGHBinary(t, "#!/bin/sh\nsleep 1\n")
	prev := prLookupTimeout
	prLookupTimeout = 10 * time.Millisecond
	t.Cleanup(func() { prLookupTimeout = prev })

	url, number := h.lookupPRInfo(newMockSession("s1"), t.TempDir(), session.GitContext{Branch: "feature/slow"})
	assert.Empty(t, url)
	assert.Zero(t, number)
}

func TestGetGitInfo_ActiveAgentWorkdir_PrefersWorktreeBranch(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeDir := addTempGitWorktree(t, repoDir, "feature/worktree-pr")

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession:    mockSession{id: "local-worktree", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeDir},
		cwd:            repoDir,
	})

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/456\",\"number\":456}'\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-worktree/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "feature/worktree-pr", resp.Branch)
	assert.Equal(t, "https://github.com/example/panemux/pull/456", resp.PRURL)
	assert.Equal(t, 456, resp.PRNumber)
}

func TestGetGitInfo_MultipleActiveWorktrees_ReturnsAllWorktreesWithPRs(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeA := addTempGitWorktree(t, repoDir, "feature/worktree-a")
	worktreeB := addTempGitWorktree(t, repoDir, "feature/worktree-b")

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession:    mockSession{id: "local-multi", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeA, worktreeB},
		cwd:            repoDir,
	})

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(t, ""+
		"#!/bin/sh\n"+
		"case \"$3\" in\n"+
		"feature/worktree-a) echo '{\"url\":\"https://github.com/example/panemux/pull/111\",\"number\":111}' ;;\n"+
		"feature/worktree-b) echo '{\"url\":\"https://github.com/example/panemux/pull/222\",\"number\":222}' ;;\n"+
		"*) exit 1 ;;\n"+
		"esac\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-multi/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	require.Len(t, resp.Worktrees, 2)

	byBranch := map[string]worktreeInfo{}
	for _, wt := range resp.Worktrees {
		byBranch[wt.Branch] = wt
	}
	require.Contains(t, byBranch, "feature/worktree-a")
	require.Contains(t, byBranch, "feature/worktree-b")
	assert.Equal(t, 111, byBranch["feature/worktree-a"].PRNumber)
	assert.Equal(t, 222, byBranch["feature/worktree-b"].PRNumber)

	// The top-level fields stay populated with the first worktree for
	// consumers that only read the single-worktree shape (e.g. WorkspaceTabs).
	assert.Equal(t, resp.Worktrees[0].Branch, resp.Branch)
	assert.Equal(t, resp.Worktrees[0].PRNumber, resp.PRNumber)
}

func TestGetGitInfo_MultipleActiveWorktrees_DuplicateRootDeduped(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeA := addTempGitWorktree(t, repoDir, "feature/worktree-a")

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local-dup", typ: session.TypeLocal},
		// Two subagents both reported the same worktree path; it must not be
		// double-counted.
		activeWorkdirs: []string{worktreeA, worktreeA},
		cwd:            repoDir,
	})

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/111\",\"number\":111}'\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-dup/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Worktrees, 1)
	assert.Equal(t, "feature/worktree-a", resp.Worktrees[0].Branch)
}

func TestGetGitInfo_MultipleActiveWorktrees_OnePRLookupFails_OthersStillReturned(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeA := addTempGitWorktree(t, repoDir, "feature/worktree-a")
	worktreeB := addTempGitWorktree(t, repoDir, "feature/worktree-b")

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession:    mockSession{id: "local-partial", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeA, worktreeB},
		cwd:            repoDir,
	})

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(t, ""+
		"#!/bin/sh\n"+
		"case \"$3\" in\n"+
		"feature/worktree-a) echo '{\"url\":\"https://github.com/example/panemux/pull/111\",\"number\":111}' ;;\n"+
		"*) exit 1 ;;\n"+
		"esac\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-partial/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Worktrees, 2)

	byBranch := map[string]worktreeInfo{}
	for _, wt := range resp.Worktrees {
		byBranch[wt.Branch] = wt
	}
	assert.Equal(t, 111, byBranch["feature/worktree-a"].PRNumber)
	assert.Empty(t, byBranch["feature/worktree-b"].PRURL)
	assert.Zero(t, byBranch["feature/worktree-b"].PRNumber)
}

func TestGetGitInfo_EndedAgentFallsBackToPaneCWD(t *testing.T) {
	repoDir := initTempGitRepo(t)
	_ = addTempGitWorktree(t, repoDir, "feature/worktree-pr")

	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession:    mockSession{id: "local-fallback", typ: session.TypeLocal},
		activeWorkdirs: nil,
		cwd:            repoDir,
	})

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/789\",\"number\":789}'\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-fallback/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
}

func TestGetGitInfo_EndedAgentKeepsLastWorktree(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeDir := addTempGitWorktree(t, repoDir, "feature/worktree-pr")

	sess := &mockCWDSession{
		mockSession:    mockSession{id: "local-sticky", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeDir},
		cwd:            repoDir,
	}
	mgr := session.NewManager()
	mgr.Add(sess)

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/999\",\"number\":999}'\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-sticky/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	sess.activeWorkdirs = nil

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/local-sticky/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "feature/worktree-pr", resp.Branch)
	assert.Equal(t, "https://github.com/example/panemux/pull/999", resp.PRURL)
	assert.Equal(t, 999, resp.PRNumber)
}

func TestGetGitInfo_StaleStickyWorktreeFallsBackToPaneCWD(t *testing.T) {
	repoDir := initTempGitRepo(t)
	worktreeDir := addTempGitWorktree(t, repoDir, "feature/worktree-pr")

	sess := &mockCWDSession{
		mockSession:    mockSession{id: "local-stale", typ: session.TypeLocal},
		activeWorkdirs: []string{worktreeDir},
		cwd:            repoDir,
	}
	mgr := session.NewManager()
	mgr.Add(sess)

	h := NewHandler(defaultTestConfig(), mgr)
	now := time.Now()
	h.nowFn = func() time.Time { return now }
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-stale/git-info", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	sess.activeWorkdirs = nil
	out, err := exec.Command( //nolint:gosec // trusted test args
		"git",
		"-C",
		repoDir,
		"worktree",
		"remove",
		worktreeDir,
		"--force",
	).CombinedOutput()
	require.NoError(t, err, "git worktree remove failed: %s", string(out))

	// Advance past the git-info response cache TTL so the second request
	// recomputes instead of replaying the first response's cached worktree.
	now = now.Add(gitInfoCacheTTL + time.Second)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/local-stale/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	_, ok := h.preferredCWDBySession["local-stale"]
	assert.False(t, ok)
}

func TestResolveSinglePreferredCWD_StickyWorktreeIgnoredAfterRepoChange(t *testing.T) {
	baseRepo := "/repo/base"
	worktreeDir := "/repo/base-worktree"
	otherRepo := "/repo/other"

	sess := &mockRemoteGitSession{
		mockSession:    mockSession{id: "remote-sticky", typ: session.TypeSSH},
		activeWorkdirs: []string{worktreeDir},
		cwd:            baseRepo,
		gitContexts: map[string]session.GitContext{
			baseRepo: {
				Branch:    "main",
				CommonDir: "/repo/.git",
				Repo:      "base",
				Root:      baseRepo,
			},
			worktreeDir: {
				Branch:    "feature/worktree",
				CommonDir: "/repo/.git",
				Repo:      "base",
				Root:      worktreeDir,
			},
			otherRepo: {
				Branch:    "main",
				CommonDir: "/repo/other/.git",
				Repo:      "other",
				Root:      otherRepo,
			},
		},
	}

	h := NewHandler(defaultTestConfig(), session.NewManager())
	assert.Equal(t, worktreeDir, h.resolveSinglePreferredCWD(sess, sess.cwd))

	sess.activeWorkdirs = nil
	sess.cwd = otherRepo

	assert.Equal(t, otherRepo, h.resolveSinglePreferredCWD(sess, sess.cwd))
}

func TestResolveSinglePreferredCWD_LogsPaneIdentity(t *testing.T) {
	sess := &mockRemoteGitSession{
		mockSession:    mockSession{id: "pane-123", typ: session.TypeSSHTmux},
		activeWorkdirs: []string{"/repo/base-worktree"},
		cwd:            "/repo/base",
		gitContexts: map[string]session.GitContext{
			"/repo/base": {
				Branch:    "main",
				CommonDir: "/repo/.git",
				Repo:      "base",
				Root:      "/repo/base",
			},
			"/repo/base-worktree": {
				Branch:    "feature/worktree",
				CommonDir: "/repo/.git",
				Repo:      "base",
				Root:      "/repo/base-worktree",
			},
		},
	}

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	h := NewHandler(defaultTestConfig(), session.NewManager())
	assert.Equal(t, "/repo/base-worktree", h.resolveSinglePreferredCWD(sess, sess.cwd))
	assert.Contains(t, buf.String(), `git info pane="pane-123" type="ssh_tmux" selected active workdir`)
}

func TestGetGitInfo_RemoteEndedAgentKeepsLastWorktree(t *testing.T) {
	const (
		baseRepo     = "/home/demo/panemux"
		worktreeRepo = "/home/demo/panemux-worktree"
	)

	sess := &mockRemoteGitSession{
		mockSession:    mockSession{id: "ssh-sticky", typ: session.TypeSSH},
		activeWorkdirs: []string{worktreeRepo},
		cwd:            baseRepo,
		gitContexts: map[string]session.GitContext{
			baseRepo: {
				Branch:    "main",
				CommonDir: "/home/demo/panemux/.git",
				OriginURL: "git@github.com:example/panemux.git",
				Repo:      "panemux",
				Root:      baseRepo,
			},
			worktreeRepo: {
				Branch:    "feature/remote-worktree",
				CommonDir: "/home/demo/panemux/.git",
				OriginURL: "git@github.com:example/panemux.git",
				Repo:      "panemux",
				Root:      worktreeRepo,
			},
		},
	}
	mgr := session.NewManager()
	mgr.Add(sess)

	h := NewHandler(defaultTestConfig(), mgr)
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\necho '{\"url\":\"https://github.com/example/panemux/pull/654\",\"number\":654}'\n",
	)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-sticky/git-info", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	sess.activeWorkdirs = nil

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-sticky/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "feature/remote-worktree", resp.Branch)
	assert.Equal(t, "https://github.com/example/panemux/pull/654", resp.PRURL)
	assert.Equal(t, 654, resp.PRNumber)
}

func TestGetGitInfo_GitNotFound_IsGitFalse(t *testing.T) {
	dir := initTempGitRepo(t)
	mgr := session.NewManager()
	mgr.Add(&mockCWDSession{
		mockSession: mockSession{id: "local3", typ: session.TypeLocal},
		cwd:         dir,
	})
	h := NewHandler(defaultTestConfig(), mgr)
	prev := gitExistsFn
	gitExistsFn = func() error { return errors.New("git not found") }
	t.Cleanup(func() { gitExistsFn = prev })
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local3/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.IsGit)
}

func TestGetGitInfo_SecondRequestWithinTTL_ServesCachedResponseWithoutRecomputing(t *testing.T) {
	dir := initTempGitRepo(t)
	sess := &mockCWDSession{
		mockSession: mockSession{id: "local-cached", typ: session.TypeLocal},
		cwd:         dir,
	}
	mgr := session.NewManager()
	mgr.Add(sess)
	h := NewHandler(defaultTestConfig(), mgr)
	now := time.Now()
	h.nowFn = func() time.Time { return now }
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-cached/git-info", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, sess.getCWDCalls)

	// Well within gitInfoCacheTTL: the second request must be served from
	// cache, not recomputed.
	now = now.Add(5 * time.Second)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/local-cached/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, sess.getCWDCalls, "expected the cached response to be served without recomputing")

	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
}

func TestGetGitInfo_RequestAfterTTLExpires_Recomputes(t *testing.T) {
	dir := initTempGitRepo(t)
	sess := &mockCWDSession{
		mockSession: mockSession{id: "local-expired", typ: session.TypeLocal},
		cwd:         dir,
	}
	mgr := session.NewManager()
	mgr.Add(sess)
	h := NewHandler(defaultTestConfig(), mgr)
	now := time.Now()
	h.nowFn = func() time.Time { return now }
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-expired/git-info", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, sess.getCWDCalls)

	now = now.Add(gitInfoCacheTTL + time.Second)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/local-expired/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 2, sess.getCWDCalls, "expected the cache to expire and the response to be recomputed")
}

func TestGetGitInfo_AfterSessionRecreatedWithSameID_DoesNotServeOldSessionsCache(t *testing.T) {
	oldDir := initTempGitRepo(t)
	oldSess := &mockCWDSession{
		mockSession: mockSession{id: "local-recreated", typ: session.TypeLocal},
		cwd:         oldDir,
	}
	oldSess.ensureBuf()
	mgr := session.NewManager()
	mgr.Add(oldSess)
	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/local-recreated/git-info", nil)
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var oldResp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&oldResp))
	assert.Equal(t, "main", oldResp.Branch)

	require.NoError(t, mgr.Remove("local-recreated"))
	h.clearGitInfoCache("local-recreated")

	newDir := initTempGitRepo(t)
	out, err := exec.Command( //nolint:gosec // G204: trusted test args
		"git",
		"-C",
		newDir,
		"checkout",
		"-b",
		"feature/recreated-session",
	).CombinedOutput()
	require.NoError(t, err, "git checkout failed: %s", string(out))
	newSess := &mockCWDSession{
		mockSession: mockSession{id: "local-recreated", typ: session.TypeLocal},
		cwd:         newDir,
	}
	newSess.ensureBuf()
	mgr.Add(newSess)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/local-recreated/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var newResp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&newResp))
	assert.Equal(t, "feature/recreated-session", newResp.Branch)
}

func TestGetGitInfo_RemoteGitContext_ReturnsBranchAndRepo(t *testing.T) {
	const remoteRepo = "/home/demo/panemux"

	mgr := session.NewManager()
	mgr.Add(&mockRemoteGitSession{
		mockSession: mockSession{id: "ssh-remote", typ: session.TypeSSH},
		cwd:         remoteRepo,
		gitContexts: map[string]session.GitContext{
			remoteRepo: {
				Branch:    "main",
				CommonDir: "/home/demo/panemux/.git",
				Repo:      "panemux",
				Root:      remoteRepo,
			},
		},
	})

	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-remote/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "main", resp.Branch)
	assert.Equal(t, "panemux", resp.Repo)
	assert.Empty(t, resp.RepoURL)
}

func TestGetGitInfo_RemoteGitContextFailure_LogsCauseAndRemediation(t *testing.T) {
	const remoteDir = "/home/demo"

	mgr := session.NewManager()
	mgr.Add(&mockRemoteGitSession{
		mockSession: mockSession{id: "ssh-log", typ: session.TypeSSH},
		cwd:         remoteDir,
		gitErrs: map[string]error{
			remoteDir: session.NewGitContextError(
				"ssh",
				"git rev-parse --show-toplevel",
				remoteDir,
				session.GitContextCauseNotGitRepo,
				errors.New("Process exited with status 128"),
				"fatal: not a git repository (or any of the parent directories): .git",
			),
		},
	})

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-log/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, buf.String(), `source="base context lookup"`)
	assert.Contains(t, buf.String(), `transport="ssh"`)
	assert.Contains(t, buf.String(), `cause="current directory is outside a Git repository"`)
	assert.Contains(t, buf.String(), `stderr="fatal: not a git repository (or any of the parent directories): .git"`)
}

func TestGetGitInfo_RemoteActiveWorkdir_PrefersRemoteWorktreeBranch(t *testing.T) {
	const (
		remoteRepo     = "/home/demo/panemux"
		remoteWorktree = "/home/demo/panemux-worktree"
		commonDir      = "/home/demo/panemux/.git"
	)

	mgr := session.NewManager()
	mgr.Add(&mockRemoteGitSession{
		mockSession:    mockSession{id: "ssh-worktree", typ: session.TypeSSHTmux},
		cwd:            remoteRepo,
		activeWorkdirs: []string{remoteWorktree},
		gitContexts: map[string]session.GitContext{
			remoteRepo: {
				Branch:    "main",
				CommonDir: commonDir,
				Repo:      "panemux",
				Root:      remoteRepo,
			},
			remoteWorktree: {
				Branch:    "feature/remote-worktree",
				CommonDir: commonDir,
				Repo:      "panemux",
				Root:      remoteWorktree,
			},
		},
	})

	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-worktree/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "feature/remote-worktree", resp.Branch)
	assert.Equal(t, "panemux", resp.Repo)
	assert.Empty(t, resp.RepoURL)
}

func TestGetGitInfo_RemoteGitContext_WithOrigin_ReturnsRepoURL(t *testing.T) {
	const remoteRepo = "/home/demo/panemux"

	mgr := session.NewManager()
	mgr.Add(&mockRemoteGitSession{
		mockSession: mockSession{id: "ssh-remote-origin", typ: session.TypeSSH},
		cwd:         remoteRepo,
		gitContexts: map[string]session.GitContext{
			remoteRepo: {
				Branch:    "main",
				CommonDir: "/home/demo/panemux/.git",
				OriginURL: "git@github.com:example/panemux.git",
				Repo:      "panemux",
				Root:      remoteRepo,
			},
		},
	})

	h := NewHandler(defaultTestConfig(), mgr)
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-remote-origin/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "https://github.com/example/panemux", resp.RepoURL)
}

func TestGetGitInfo_RemoteGitContext_WithSSHConfigAliasOrigin_ReturnsResolvedRepoURL(t *testing.T) {
	const remoteRepo = "/home/demo/panemux"

	mgr := session.NewManager()
	mgr.Add(&mockRemoteGitSession{
		mockSession: mockSession{id: "ssh-remote-alias-origin", typ: session.TypeSSH},
		cwd:         remoteRepo,
		gitContexts: map[string]session.GitContext{
			remoteRepo: {
				Branch:    "main",
				CommonDir: "/home/demo/panemux/.git",
				OriginURL: "git@github-work:example/panemux.git",
				Repo:      "panemux",
				Root:      remoteRepo,
			},
		},
	})

	h := NewHandler(defaultTestConfig(), mgr)
	h.sshConfigPath = writeTempSSHConfigForAPI(t, "Host github-work\n    HostName github.com\n    User git\n")
	r := setupRouterWithGitInfo(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ssh-remote-alias-origin/git-info", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp gitInfoResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsGit)
	assert.Equal(t, "https://github.com/example/panemux", resp.RepoURL)
}

func TestLookupPRInfo_RemoteSessionWithoutOriginSkipsLookup(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.ghBinaryPath = writeFakeGHBinary(t, "#!/bin/sh\nexit 99\n")

	url, number := h.lookupPRInfo(&mockRemoteGitSession{}, "/home/demo/panemux", session.GitContext{
		Branch: "feature/no-origin",
	})
	assert.Empty(t, url)
	assert.Zero(t, number)
}

func TestLookupPRInfo_RemoteSessionWithSSHConfigAliasOrigin_UsesResolvedRepoSpec(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = writeTempSSHConfigForAPI(t, "Host github-work\n    HostName github.com\n    User git\n")
	h.ghBinaryPath = writeFakeGHBinary(
		t,
		"#!/bin/sh\n"+
			"if [ \"$6\" != \"--repo\" ] || [ \"$7\" != \"github.com/example/panemux\" ]; then\n"+
			"  echo \"unexpected repo args: $6 $7\" >&2\n"+
			"  exit 1\n"+
			"fi\n"+
			"echo '{\"url\":\"https://github.com/example/panemux/pull/999\",\"number\":999}'\n",
	)

	url, number := h.lookupPRInfo(&mockRemoteGitSession{}, "/home/demo/panemux", session.GitContext{
		Branch:    "feature/alias-pr",
		OriginURL: "git@github-work:example/panemux.git",
	})

	assert.Equal(t, "https://github.com/example/panemux/pull/999", url)
	assert.Equal(t, 999, number)
}

func TestSanitizeGitExecDir_ValidAbsolutePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.MkdirAll(dir, 0755))

	got, err := sanitizeGitExecDir(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestRepoSpecFromOriginURL(t *testing.T) {
	credentialOrigin := "https://user:" + "placeholder" + "@github.com/example/panemux.git"
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "scp style ssh",
			origin: "git@github.com:example/panemux.git",
			want:   "github.com/example/panemux",
		},
		{
			name:   "https",
			origin: "https://github.com/example/panemux.git",
			want:   "github.com/example/panemux",
		},
		{
			name:   "ssh url",
			origin: "ssh://git@git.example.com/team/panemux.git",
			want:   "git.example.com/team/panemux",
		},
		{
			name:   "ssh url with explicit port",
			origin: "ssh://git@git.example.com:2222/team/panemux.git",
			want:   "git.example.com/team/panemux",
		},
		{
			name:   "https without git suffix",
			origin: "https://github.com/example/panemux",
			want:   "github.com/example/panemux",
		},
		{
			name:   "https with embedded credentials",
			origin: credentialOrigin,
			want:   "github.com/example/panemux",
		},
		{
			name:   "invalid",
			origin: "not-a-url",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repoSpecFromOriginURL(tt.origin))
		})
	}
}

func TestRepoPageURLFromOriginURL(t *testing.T) {
	credentialOrigin := "https://user:" + "placeholder" + "@github.com/example/panemux.git"
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "scp style ssh",
			origin: "git@github.com:example/panemux.git",
			want:   "https://github.com/example/panemux",
		},
		{
			name:   "https",
			origin: "https://github.com/example/panemux.git",
			want:   "https://github.com/example/panemux",
		},
		{
			name:   "ssh url",
			origin: "ssh://git@git.example.com/team/panemux.git",
			want:   "https://git.example.com/team/panemux",
		},
		{
			name:   "ssh url with explicit port",
			origin: "ssh://git@git.example.com:2222/team/panemux.git",
			want:   "https://git.example.com/team/panemux",
		},
		{
			name:   "https with embedded credentials",
			origin: credentialOrigin,
			want:   "https://github.com/example/panemux",
		},
		{
			name:   "invalid",
			origin: "not-a-url",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repoPageURLFromOriginURL(tt.origin))
		})
	}
}

func TestHandlerRepoPageURLFromOriginURL_ResolvesSSHConfigAlias(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = writeTempSSHConfigForAPI(t, "Host github-work\n    HostName github.com\n    User git\n")

	got := h.repoPageURLFromOriginURL("git@github-work:example/panemux.git")

	assert.Equal(t, "https://github.com/example/panemux", got)
}

func TestHandlerRepoPageURLFromOriginURL_LeavesUnknownSCPHostUntouched(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = writeTempSSHConfigForAPI(t, "Host github-work\n    HostName github.com\n    User git\n")

	got := h.repoPageURLFromOriginURL("git@source:example/panemux.git")

	assert.Equal(t, "https://source/example/panemux", got)
}

func TestHandlerRepoSpecFromOriginURL_ResolvesSSHConfigAlias(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.sshConfigPath = writeTempSSHConfigForAPI(t, "Host github-work\n    HostName github.com\n    User git\n")

	got := h.repoSpecFromOriginURL("git@github-work:example/panemux.git")

	assert.Equal(t, "github.com/example/panemux", got)
}

func TestSanitizeGitExecDir_RejectsRelativePath(t *testing.T) {
	_, err := sanitizeGitExecDir("relative/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestGetDirectories_LocalPathReturnsDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "visible"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".hidden"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0600))

	h := NewHandler(defaultTestConfig(), session.NewManager())
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/directories?path="+dir+"&show_hidden=false", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp directoryBrowserResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "visible", resp.Entries[0].Name)
	assert.Equal(t, filepath.Join(dir, "visible"), resp.Entries[0].Path)
}

func TestGetDirectories_ShowHiddenIncludesHiddenDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".hidden"), 0755))

	h := NewHandler(defaultTestConfig(), session.NewManager())
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/directories?path="+dir+"&show_hidden=true", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp directoryBrowserResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, ".hidden", resp.Entries[0].Name)
}

func TestGetDirectories_NoVisibleChildDirectoriesReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".hidden"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0600))

	h := NewHandler(defaultTestConfig(), session.NewManager())
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/directories?path="+dir+"&show_hidden=false", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"path":"`+dir+`","entries":[]}`, rec.Body.String())
}

func TestGetDirectories_UsesRemoteConnectionWhenProvided(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	h.listRemoteDirectoriesFn = func(
		cfg session.SSHConfig,
		path string,
		showHidden bool,
	) (directoryBrowserResponse, error) {
		assert.Equal(t, "remote.example.com", cfg.Host)
		assert.Equal(t, "/home/ubuntu", path)
		assert.True(t, showHidden)
		return directoryBrowserResponse{
			Path: "/home/ubuntu",
			Entries: []directoryEntryResponse{
				{Name: "app", Path: "/home/ubuntu/app", HasChildren: true},
			},
		}, nil
	}
	h.cfg.SSHConnections = map[string]config.SSHConnection{
		"prod": {Host: "remote.example.com", User: "ubuntu", Port: 22},
	}
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/directories?connection=prod&path=/home/ubuntu&show_hidden=true", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp directoryBrowserResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "app", resp.Entries[0].Name)
}

func TestGetDirectories_InvalidLocalPathReturns422(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager())
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/directories?path=/definitely/missing/path", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestGetDirectories_SkipsUnreadableChildDirectories(t *testing.T) {
	dir := t.TempDir()
	readableDir := filepath.Join(dir, "readable")
	unreadableDir := filepath.Join(dir, "restricted")
	require.NoError(t, os.Mkdir(readableDir, 0755))
	require.NoError(t, os.Mkdir(filepath.Join(readableDir, "nested"), 0755))
	require.NoError(t, os.Mkdir(unreadableDir, 0755))

	h := NewHandler(defaultTestConfig(), session.NewManager())
	originalReadDir := h.readDirFn
	h.readDirFn = func(name string) ([]os.DirEntry, error) {
		if name == unreadableDir {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrPermission}
		}
		return originalReadDir(name)
	}
	t.Cleanup(func() {
		h.readDirFn = originalReadDir
	})

	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/directories?path="+dir+"&show_hidden=false", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp directoryBrowserResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "readable", resp.Entries[0].Name)
	assert.True(t, resp.Entries[0].HasChildren)
}
