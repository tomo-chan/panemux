package api

import (
	"errors"
	"net/http"

	"panemux/internal/tasks"
)

// taskInputGivenRequest is the body of POST /api/tasks/input-given.
type taskInputGivenRequest struct {
	WaitSignature string `json:"wait_signature"`
}

// PostTaskInputGiven marks a codex approval request answered ("Input
// given", issue #384), naming it by the wait signature GET /api/tasks gave.
// The task is shown busy again from the next collection; it answers 204.
func (h *Handler) PostTaskInputGiven(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req taskInputGivenRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	if err := h.tasks.DismissWait(req.WaitSignature); errors.Is(err, tasks.ErrInvalidWait) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
