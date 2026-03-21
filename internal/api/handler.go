package api

import (
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
)

// Handler provides REST API endpoints.
type Handler struct {
	cfg      *config.Config
	manager  *session.Manager
	editMode atomic.Bool
}

type editModeResponse struct {
	EditMode bool `json:"editMode"`
}

// NewHandler creates a new API handler.
func NewHandler(cfg *config.Config, manager *session.Manager) *Handler {
	return &Handler{cfg: cfg, manager: manager}
}

// GetLayout returns the current layout configuration.
func (h *Handler) GetLayout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.Layout)
}

// PutLayout updates the layout configuration and persists it.
func (h *Handler) PutLayout(w http.ResponseWriter, r *http.Request) {
	var layout config.LayoutNode
	if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := config.ValidateLayout(layout); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if h.editMode.Load() {
		if err := h.cfg.SaveLayout(layout); err != nil {
			http.Error(w, "failed to save layout", http.StatusInternalServerError)
			return
		}
	} else {
		h.cfg.UpdateLayout(layout)
	}

	writeJSON(w, layout)
}

// GetSessions lists all active sessions.
func (h *Handler) GetSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.manager.List()
	list := make([]sessionInfo, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, sessionInfo{
			ID:    s.ID(),
			Type:  string(s.Type()),
			Title: s.Title(),
			State: string(s.State()),
		})
	}
	writeJSON(w, list)
}

// PostSession creates a new session dynamically.
func (h *Handler) PostSession(w http.ResponseWriter, r *http.Request) {
	var pane config.PaneConfig
	if err := json.NewDecoder(r.Body).Decode(&pane); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := config.ValidatePane(&pane); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if _, exists := h.manager.Get(pane.ID); exists {
		http.Error(w, "session already exists", http.StatusConflict)
		return
	}

	sess, err := session.CreateFromConfig(&pane, h.cfg.SSHConnections)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.manager.Add(sess)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sessionInfo{
		ID:    sess.ID(),
		Type:  string(sess.Type()),
		Title: sess.Title(),
		State: string(sess.State()),
	})
}

// DeleteSession terminates a session by ID and removes it from the layout.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.manager.Remove(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.cfg.RemovePaneFromLayout(id)
	if h.editMode.Load() {
		h.cfg.SaveLayout(h.cfg.Layout) //nolint:errcheck
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestartSession recreates a session from its original config.
func (h *Handler) RestartSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var found *config.PaneConfig
	for _, p := range h.cfg.AllPanes() {
		if p.ID == id {
			found = p
			break
		}
	}
	if found == nil {
		http.Error(w, "session config not found", http.StatusNotFound)
		return
	}

	h.manager.Remove(id) //nolint:errcheck -- ok if already gone

	sess, err := session.CreateFromConfig(found, h.cfg.SSHConnections)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.manager.Add(sess)
	w.WriteHeader(http.StatusOK)
}

// GetDisplay returns the display configuration.
func (h *Handler) GetDisplay(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.Display)
}

// GetEditMode returns the current edit mode state.
func (h *Handler) GetEditMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, editModeResponse{EditMode: h.editMode.Load()})
}

// PutEditMode sets the edit mode state.
func (h *Handler) PutEditMode(w http.ResponseWriter, r *http.Request) {
	var req editModeResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	h.editMode.Store(req.EditMode)
	if req.EditMode {
		if err := h.cfg.SaveLayout(h.cfg.Layout); err != nil {
			http.Error(w, "failed to save layout", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, editModeResponse{EditMode: h.editMode.Load()})
}

type sessionInfo struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	State string `json:"state"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
