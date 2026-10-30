package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"panemux/internal/host"
	"panemux/internal/hosttools"
)

func (h *Handler) agmsgToolEnabled(w http.ResponseWriter) bool {
	h.cfgMu.RLock()
	enabled := h.cfg.AgentBoard.Enabled
	h.cfgMu.RUnlock()
	if !enabled {
		http.Error(w, "Agent Board is disabled", http.StatusNotFound)
	}
	return enabled
}

func (h *Handler) GetAgmsgTool(w http.ResponseWriter, r *http.Request) {
	if !h.agmsgToolEnabled(w) {
		return
	}
	state, err := h.hostTools.Check(r.Context(), hosttools.AgmsgTool, host.Host(r.URL.Query().Get("host")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, agmsgResponse(state))
}

func (h *Handler) PostAgmsgTool(w http.ResponseWriter, r *http.Request) {
	if !h.agmsgToolEnabled(w) {
		return
	}
	var req taskCodexHookRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	state, err := h.hostTools.Install(ctx, hosttools.AgmsgTool, host.Host(req.Host))
	switch {
	case errors.Is(err, hosttools.ErrUnknownHost):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		writeJSON(w, agmsgResponse(state))
	}
}

// HostTools supplies board coordination without giving it ownership of the manager.
func (h *Handler) HostTools() *hosttools.Service { return h.hostTools }

type agmsgToolResponse struct {
	AutoError   string           `json:"auto_error,omitempty"`
	Status      hosttools.Status `json:"status"`
	Version     string           `json:"version,omitempty"`
	Error       string           `json:"error,omitempty"`
	AutoUpdate  bool             `json:"auto_update"`
	AutoUpdated bool             `json:"auto_updated,omitempty"`
}

func agmsgResponse(state hosttools.State) agmsgToolResponse {
	return agmsgToolResponse{
		Status: state.Status, Version: state.Version, Error: state.Error,
		AutoUpdate: state.AutoUpdate, AutoUpdated: state.AutoUpdated, AutoError: state.AutoError,
	}
}

// PostAgmsgAutoUpdate saves the instance-wide preference; no host selector is accepted.
func (h *Handler) PostAgmsgAutoUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.agmsgToolEnabled(w) {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	if req.Enabled == nil {
		http.Error(w, "enabled is required", http.StatusBadRequest)
		return
	}
	h.cfgMu.Lock()
	before := h.cfg.AgentBoard.AgmsgAutoUpdate
	h.cfg.AgentBoard.AgmsgAutoUpdate = *req.Enabled
	err := h.cfg.SaveWorkspaces()
	if err != nil {
		h.cfg.AgentBoard.AgmsgAutoUpdate = before
	}
	h.cfgMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if *req.Enabled && !before {
		h.hostTools.StartAutoUpdates(hosttools.AgmsgTool)
	}
	writeJSON(w, taskCodexHookAutoResponse{AutoUpdate: *req.Enabled})
}

func (h *Handler) agmsgAutoEnabled() bool {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	return h.cfg.AgentBoard.Enabled && h.cfg.AgentBoard.AgmsgAutoUpdate
}
