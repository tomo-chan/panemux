package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"panemux/internal/tasks"
)

// taskLaunchBodyLimit bounds a POST /api/tasks body. A prompt is at most
// 32 KiB, and JSON may escape each byte into six.
const taskLaunchBodyLimit = 256 << 10

// taskLaunchRequest is the body of POST /api/tasks.
type taskLaunchRequest struct {
	Host   string   `json:"host"`
	Agent  string   `json:"agent"`
	CWD    string   `json:"cwd"`
	Prompt string   `json:"prompt"`
	Labels []string `json:"labels"`
}

// taskLaunchResponse is a started task. Labels are the labels recorded for
// it; RecordsError is why they could not be, in which case the task was
// still started.
type taskLaunchResponse struct {
	tasks.Launched
	RecordsError string   `json:"records_error,omitempty"`
	Labels       []string `json:"labels,omitempty"`
}

// taskResumeRequest is the body of POST /api/tasks/resume.
type taskResumeRequest struct {
	Host      string `json:"host"`
	SessionID string `json:"session_id"`
}

// PostTask starts a new claude task on a host, in a detached tmux session of
// its own, and records the labels it was given (issue #257). No pane is
// created: the dashboard attaches one when the task is opened.
func (h *Handler) PostTask(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req taskLaunchRequest
	if !decodeStrict(w, r, taskLaunchBodyLimit, &req) {
		return
	}
	// Only claude, until codex tasks carry a session ID (issue #264).
	if req.Agent != tasks.AgentClaude {
		http.Error(w, "only claude tasks can be started", http.StatusBadRequest)
		return
	}
	// Labels are checked before anything starts, so a label that would be
	// refused cannot leave a running task without the labels it was meant
	// to have.
	labels, err := tasks.NormalizeLabels(req.Labels)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	launched, err := h.tasks.Launch(r.Context(), tasks.LaunchRequest{Host: req.Host, CWD: req.CWD, Prompt: req.Prompt})
	if err != nil {
		writeTaskLaunchError(w, err)
		return
	}
	resp := taskLaunchResponse{Launched: launched}
	if len(labels) > 0 {
		saved, err := h.taskRecords.Put(tasks.Record{
			Host: req.Host, Agent: tasks.AgentClaude, SessionID: launched.SessionID, Labels: labels,
		})
		if err != nil {
			resp.RecordsError = err.Error()
		} else {
			resp.Labels = saved.Labels
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// PostTaskResume runs `claude --resume` for a stopped claude task in a new
// detached tmux session. The task keeps its session ID, so its record — done
// and labels — carries over unchanged.
func (h *Handler) PostTaskResume(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req taskResumeRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	launched, err := h.tasks.Resume(r.Context(), req.Host, req.SessionID)
	if err != nil {
		writeTaskLaunchError(w, err)
		return
	}
	writeJSON(w, launched)
}

// decodeStrict decodes a JSON body, refusing unknown fields, and answers 400
// itself when it cannot.
func decodeStrict(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// writeTaskLaunchError maps a launch or resume failure to its status: the
// request was wrong (400), named no configured host (404), the host or the
// task's state refused it (409), or the host could not be reached (502).
func writeTaskLaunchError(w http.ResponseWriter, err error) {
	var refused *tasks.LaunchError
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, tasks.ErrInvalidLaunch):
		status = http.StatusBadRequest
	case errors.Is(err, tasks.ErrUnknownHost):
		status = http.StatusNotFound
	case errors.Is(err, tasks.ErrNoStoppedTask), errors.As(err, &refused):
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}
