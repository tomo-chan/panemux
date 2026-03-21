package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	r := chi.NewRouter()
	r.Get("/api/layout", h.GetLayout)
	r.Put("/api/layout", h.PutLayout)
	r.Get("/api/sessions", h.GetSessions)
	r.Post("/api/sessions", h.PostSession)
	r.Delete("/api/sessions/{id}", h.DeleteSession)
	r.Post("/api/sessions/{id}/restart", h.RestartSession)
	r.Get("/api/display", h.GetDisplay)
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
	r := setupRouter(defaultTestConfig(), session.NewManager())
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
	r := setupRouter(cfg, mgr)

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
