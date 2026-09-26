package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"panemux/internal/tasks"
)

// taskRecordBodyLimit bounds a PUT /api/tasks/records body. The largest valid
// record — twenty labels of 32 characters — is well under 4 KiB.
const taskRecordBodyLimit = 64 << 10

// taskRecordPayload is the body of PUT /api/tasks/records and its response.
// Both fields are always present in the response, so the dashboard can apply
// it to the task as it is.
type taskRecordPayload struct {
	Host      string   `json:"host"`
	Agent     string   `json:"agent"`
	SessionID string   `json:"session_id"`
	Labels    []string `json:"labels"`
	Done      bool     `json:"done"`
}

// PutTaskRecord replaces what a person recorded about one task: whether it is
// done, and its labels (issue #256). The task is named by host, agent and
// session ID; a task without a session ID cannot carry a record.
func (h *Handler) PutTaskRecord(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req taskRecordPayload
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, taskRecordBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Host != "" {
		if _, ok := h.cfg.SSHConnections[req.Host]; !ok {
			http.Error(w, tasks.ErrUnknownHost.Error(), http.StatusNotFound)
			return
		}
	}

	saved, err := h.taskRecords.Put(tasks.Record{
		Host: req.Host, Agent: req.Agent, SessionID: req.SessionID, Done: req.Done, Labels: req.Labels,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, tasks.ErrInvalidRecord) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	labels := saved.Labels
	if labels == nil {
		labels = []string{}
	}
	writeJSON(w, taskRecordPayload{
		Host: saved.Host, Agent: saved.Agent, SessionID: saved.SessionID, Done: saved.Done, Labels: labels,
	})
}

// applyTaskRecords sets each task's record on its response entry. Only a task
// with a session ID can have one. A record whose task is not listed is left
// in the store: the session may be listed again.
func applyTaskRecords(list []taskResponse, records map[tasks.RecordKey]tasks.Record) {
	for i := range list {
		task := &list[i]
		if task.SessionID == "" {
			continue
		}
		rec, ok := records[tasks.RecordKey{Host: task.Host, Agent: task.Agent, SessionID: task.SessionID}]
		if !ok {
			continue
		}
		task.Done = rec.Done
		task.Labels = rec.Labels
	}
}
