package api

import (
	"errors"
	"net/http"

	"panemux/internal/tasks"
)

// taskCodexHookRequest is the body of POST /api/tasks/codex-hook: the host
// alone. The path and the content installed are panemux's own.
type taskCodexHookRequest struct {
	Host string `json:"host"`
}

// GetTaskCodexHook reports whether a host's codex has panemux's permission
// hook (issue #396). A host that cannot be checked answers 200 with status
// "unknown" and the reason; an unknown host name answers 404.
func (h *Handler) GetTaskCodexHook(w http.ResponseWriter, r *http.Request) {
	got, err := h.tasks.CodexHook(r.Context(), r.URL.Query().Get("host"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, got)
}

// PostTaskCodexHook installs or updates a host's permission hook and answers
// with the status read back. A host that refuses it — no codex, a hooks.json
// panemux cannot read — or fails answers 502 with the reason.
func (h *Handler) PostTaskCodexHook(w http.ResponseWriter, r *http.Request) {
	var req taskCodexHookRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	got, err := h.tasks.InstallCodexHook(r.Context(), req.Host)
	switch {
	case errors.Is(err, tasks.ErrUnknownHost):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		writeJSON(w, got)
	}
}

// taskCodexHookAutoRequest is the body of POST
// /api/tasks/codex-hook/auto-update; the answer is the setting stored.
type taskCodexHookAutoRequest struct {
	Host    string `json:"host"`
	Enabled bool   `json:"enabled"`
}

type taskCodexHookAutoResponse struct {
	AutoUpdate bool `json:"auto_update"`
}

// PostTaskCodexHookAutoUpdate turns updating a host's permission hook by
// itself on or off (issue #402). A settings file panemux cannot read or
// write answers 500 with the reason.
func (h *Handler) PostTaskCodexHookAutoUpdate(w http.ResponseWriter, r *http.Request) {
	var req taskCodexHookAutoRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	err := h.tasks.SetCodexHookAuto(req.Host, req.Enabled)
	switch {
	case errors.Is(err, tasks.ErrUnknownHost):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		writeJSON(w, taskCodexHookAutoResponse{AutoUpdate: req.Enabled})
	}
}
