package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"panemux/internal/tasks"
)

// taskSummaryRequest is the body of POST /api/tasks/summary.
type taskSummaryRequest struct {
	Host      string `json:"host"`
	SessionID string `json:"session_id"`
}

// PostTaskSummary asks for one task's summary (issue #258). The dashboard
// sends it when a task is selected: a stopped task is summarized only when
// asked, and a failed summary is retried only when asked. It answers 202 at
// once with where the summary stands; the summary itself arrives with a
// later GET /api/tasks.
func (h *Handler) PostTaskSummary(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	if !h.cfg.TaskDashboard.Summary.Enabled {
		http.Error(w, "task summaries are disabled (task_dashboard.summary.enabled)", http.StatusConflict)
		return
	}
	var req taskSummaryRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	view, err := h.tasks.RequestSummary(req.Host, req.SessionID)
	switch {
	case errors.Is(err, tasks.ErrInvalidSummary):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, tasks.ErrNoSummaryTask):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(view)
}
