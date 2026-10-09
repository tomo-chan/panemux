package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
)

// A host terminal (issue #314): an ssh or ssh_tmux session the task
// dashboard opens on one of ssh_connections when a host chip's "Type in
// pane" is pressed. It is a board attach in every other respect — served by
// /ws/{id}, never part of the layout, destroyed by DELETE or after
// boardAttachGrace with no WebSocket reading it — except that every request
// opens a new one.

// hostTerminalKeyPrefix starts a host terminal's key in boardAttaches.byTask,
// where no task ID can start with it.
const hostTerminalKeyPrefix = "host-terminal:"

// tmuxSessionNameUnsafe matches one character a tmux session name may not
// hold; see session.IsValidTmuxSessionName.
var tmuxSessionNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

// hostTmuxSessionName returns a new tmux session name for connection: the
// connection name with each character a tmux session name may not hold
// replaced by '-', then '-' and 8 random hex digits.
func hostTmuxSessionName(connection string) (string, error) {
	buf := make([]byte, 4)
	// crypto/rand.Read never returns an error (since Go 1.24 it crashes the
	// program instead), so there is no error to handle.
	_, _ = rand.Read(buf)
	name := tmuxSessionNameUnsafe.ReplaceAllString(connection, "-") + "-" + hex.EncodeToString(buf)
	if !session.IsValidTmuxSessionName(name) { //coverage:exempt the replacement above leaves only characters the guard accepts
		return "", fmt.Errorf("generated tmux session name %q is not valid", name)
	}
	return name, nil
}

// hostSessionNameRequest is the body of POST /api/hosts/session-name.
type hostSessionNameRequest struct {
	Connection string `json:"connection"`
}

// hostSessionNameResponse answers POST /api/hosts/session-name.
type hostSessionNameResponse struct {
	TmuxSession string `json:"tmux_session"`
}

// hostTerminalRequest is the body of POST /api/hosts/terminal.
type hostTerminalRequest struct {
	Connection  string `json:"connection"`
	Type        string `json:"type"`
	TmuxSession string `json:"tmux_session"`
}

// hostTerminalResponse answers POST /api/hosts/terminal. TmuxSession is
// empty for an ssh terminal.
type hostTerminalResponse struct {
	SessionID   string `json:"session_id"`
	TmuxSession string `json:"tmux_session"`
}

// knownConnection writes the refusal for a connection that is the panemux
// host or not in ssh_connections, and reports whether it is neither.
func (h *Handler) knownConnection(w http.ResponseWriter, connection string) bool {
	if connection == "" {
		http.Error(w, "connection is required; the panemux host is not opened from here", http.StatusBadRequest)
		return false
	}
	if _, ok := h.sshConnections()[connection]; !ok {
		http.Error(w, "no such ssh connection", http.StatusNotFound)
		return false
	}
	return true
}

// PostHostSessionName returns a new tmux session name for a host, the name
// both a host's "Open" and its "Type in pane" use for ssh_tmux.
func (h *Handler) PostHostSessionName(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req hostSessionNameRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	if !h.knownConnection(w, req.Connection) {
		return
	}
	name, err := hostTmuxSessionName(req.Connection)
	if err != nil { //coverage:exempt hostTmuxSessionName cannot fail; see there
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, hostSessionNameResponse{TmuxSession: name})
}

// hostTerminalPane validates req and returns the pane config of its
// terminal, with a tmux session name generated when ssh_tmux names none.
func hostTerminalPane(req hostTerminalRequest, id string) (*config.PaneConfig, error) {
	pane := &config.PaneConfig{ID: id, Type: req.Type, Connection: req.Connection, Title: req.Connection}
	switch session.Type(req.Type) {
	case session.TypeSSH:
		if req.TmuxSession != "" {
			return nil, errors.New("an ssh terminal takes no tmux_session")
		}
	case session.TypeSSHTmux:
		pane.TmuxSession = req.TmuxSession
		if pane.TmuxSession == "" {
			name, err := hostTmuxSessionName(req.Connection)
			if err != nil { //coverage:exempt hostTmuxSessionName cannot fail; see there
				return nil, err
			}
			pane.TmuxSession = name
		}
		if !session.IsValidTmuxSessionName(pane.TmuxSession) {
			return nil, fmt.Errorf("invalid tmux_session %q", pane.TmuxSession)
		}
	default:
		return nil, errors.New("type must be ssh or ssh_tmux")
	}
	return pane, nil
}

// PostHostTerminal opens a new host terminal on one of ssh_connections.
func (h *Handler) PostHostTerminal(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req hostTerminalRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	if !h.knownConnection(w, req.Connection) {
		return
	}
	pane, err := hostTerminalPane(req, h.newBoardAttachID())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sess, err := h.createSession(pane, h.sshConnections())
	if err != nil {
		http.Error(w, fmt.Sprintf("open a terminal on %q: %v", req.Connection, err), http.StatusBadGateway)
		return
	}
	h.registerBoardAttach(&boardAttach{
		taskID: hostTerminalKeyPrefix + pane.ID, sessionID: pane.ID, tmuxSession: pane.TmuxSession, host: true,
	}, sess)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(hostTerminalResponse{SessionID: pane.ID, TmuxSession: pane.TmuxSession})
}

// DeleteHostTerminal destroys a host terminal: an ssh terminal's shell
// logs out, and an ssh_tmux terminal's tmux client ends while the remote
// tmux session keeps running.
func (h *Handler) DeleteHostTerminal(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	attach := h.boardAttaches.forget(chi.URLParam(r, "id"), true)
	if attach == nil {
		http.Error(w, "no such host terminal", http.StatusNotFound)
		return
	}
	// The ID removed is the registry's own, minted by newBoardAttachID.
	h.removeBoardAttachSession(attach.sessionID)
	w.WriteHeader(http.StatusNoContent)
}
