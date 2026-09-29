package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"panemux/internal/portforward"
	"panemux/internal/session"
)

// Reasons reported when a URL needs no port forward. They are shown to the
// operator, so they explain the situation rather than just naming a state.
const (
	reasonNoCallbackPort = "the URL has no loopback callback port, so nothing needs forwarding"
	reasonLocalPane      = "this pane runs on the panemux host, so its callback already resolves locally"
	reasonUnavailable    = "port forwarding is not available on this server"
)

type openURLRequest struct {
	URL string `json:"url"`
}

type openURLResponse struct {
	URL       string `json:"url"`
	Reason    string `json:"reason,omitempty"`
	Port      int    `json:"port,omitempty"`
	Forwarded bool   `json:"forwarded"`
}

// SetPortForwards attaches the loopback port-forward registry. When it is not
// set (as in handler unit tests), open-url requests still succeed but report
// that no forward was established.
func (h *Handler) SetPortForwards(registry *portforward.Registry) {
	h.forwards = registry
}

// PostOpenURL prepares the panemux host for a URL the browser is about to
// open on a pane's behalf. For `ssh` and `ssh_tmux` panes it republishes the
// URL's loopback callback port at the identical port on the panemux host, so
// the OAuth redirect to http://localhost:<port>/… reaches the CLI that is
// waiting for it on the remote host. The browser tab itself is opened by the
// frontend, not here — the browser panemux should drive is the one showing
// the dashboard. See docs/behavior.md "Opening URLs from a pane".
func (h *Handler) PostOpenURL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req openURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := portforward.ValidateOpenURL(req.URL); err != nil {
		writeValidationError(w, err.Error())
		return
	}

	sess, ok := h.manager.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	port, ok := portforward.CallbackPort(req.URL)
	if !ok {
		writeJSON(w, openURLResponse{URL: req.URL, Reason: reasonNoCallbackPort})
		return
	}
	dialer, ok := sess.(session.LoopbackDialer)
	if !ok {
		writeJSON(w, openURLResponse{URL: req.URL, Reason: reasonLocalPane})
		return
	}
	if h.forwards == nil {
		writeJSON(w, openURLResponse{URL: req.URL, Reason: reasonUnavailable})
		return
	}

	if _, err := h.forwards.Ensure(id, port, dialer); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, portforward.ErrPortUnavailable) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, openURLResponse{URL: req.URL, Port: port, Forwarded: true})
}

// closeSessionForwards drops every forward belonging to a pane. The listener
// they point at lives in that pane's shell, so it is gone once the pane is
// deleted or restarted.
func (h *Handler) closeSessionForwards(id string) {
	if h.forwards != nil {
		h.forwards.CloseSession(id)
	}
}
