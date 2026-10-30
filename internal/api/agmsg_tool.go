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
	writeJSON(w, agmsgToolResponse{Status: state.Status, Version: state.Version, Error: state.Error})
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
		writeJSON(w, agmsgToolResponse{Status: state.Status, Version: state.Version, Error: state.Error})
	}
}

// HostTools supplies board coordination without giving it ownership of the manager.
func (h *Handler) HostTools() *hosttools.Service { return h.hostTools }

type agmsgToolResponse struct {
	Status  hosttools.Status `json:"status"`
	Version string           `json:"version,omitempty"`
	Error   string           `json:"error,omitempty"`
}
