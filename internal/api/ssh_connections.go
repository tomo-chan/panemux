package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/sshconfig"
)

// The /api/config/ssh-connections routes manage config.yaml's ssh_connections
// (issue #272): the hosts the task dashboard collects from, which panes can
// also use. They are distinct from POST /api/ssh-config/hosts, which writes
// ~/.ssh/config.
//
// A password is never sent back: an entry reports only whether it has one.
// On an update an empty password keeps the saved one, and clear_password
// removes it.

// sshConnectionBodyLimit bounds a request body; an entry is a handful of
// short strings.
const sshConnectionBodyLimit = 16 << 10

type sshConnectionEntry struct {
	Name           string   `json:"name"`
	Host           string   `json:"host,omitempty"`
	User           string   `json:"user,omitempty"`
	KeyFile        string   `json:"key_file,omitempty"`
	KnownHostsFile string   `json:"known_hosts_file,omitempty"`
	Panes          []string `json:"panes"`
	Port           int      `json:"port,omitempty"`
	HasPassword    bool     `json:"has_password"`
	// InSSHConfig is whether ~/.ssh/config has a Host block of the same
	// name, whose values fill in the fields the entry leaves empty.
	InSSHConfig bool `json:"in_ssh_config"`
}

type sshConnectionsListResponse struct {
	Connections []sshConnectionEntry `json:"connections"`
}

type sshConnectionRequest struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	User           string `json:"user"`
	KeyFile        string `json:"key_file"`
	KnownHostsFile string `json:"known_hosts_file"`
	Password       string `json:"password"`
	Port           int    `json:"port"`
	ClearPassword  bool   `json:"clear_password"`
}

// sshConnections is the current ssh_connections map. The map is never
// changed in place once published — the routes below replace it — so a
// caller may keep and range over what it got without holding the lock.
func (h *Handler) sshConnections() map[string]config.SSHConnection {
	h.sshConnMu.RLock()
	defer h.sshConnMu.RUnlock()
	return h.cfg.SSHConnections
}

// GetConfigSSHConnections lists the ssh_connections entries, sorted by name.
func (h *Handler) GetConfigSSHConnections(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	conns := h.sshConnections()
	inSSHConfig := h.sshConfigHostNames()
	entries := make([]sshConnectionEntry, 0, len(conns))
	for name, conn := range conns {
		entries = append(entries, h.sshConnectionEntry(name, conn, inSSHConfig))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	writeJSON(w, sshConnectionsListResponse{Connections: entries})
}

// PostConfigSSHConnection adds an entry.
func (h *Handler) PostConfigSSHConnection(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	req, ok := decodeSSHConnectionRequest(w, r)
	if !ok {
		return
	}
	if err := config.ValidateSSHConnectionName(req.Name); err != nil {
		writeValidationError(w, err.Error())
		return
	}
	if req.ClearPassword {
		writeValidationError(w, "clear_password applies only to an existing entry")
		return
	}
	conn := req.connection()
	conn.Password = req.Password

	h.sshConnMu.Lock()
	defer h.sshConnMu.Unlock()
	if _, exists := h.cfg.SSHConnections[req.Name]; exists {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("ssh connection %q already exists", req.Name))
		return
	}
	saved, ok := h.storeSSHConnection(w, req.Name, conn)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(saved)
}

// PutConfigSSHConnection replaces an entry's fields. The name cannot change:
// panes refer to the entry by it.
func (h *Handler) PutConfigSSHConnection(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	name := chi.URLParam(r, "name")
	req, ok := decodeSSHConnectionRequest(w, r)
	if !ok {
		return
	}

	h.sshConnMu.Lock()
	existing, exists := h.cfg.SSHConnections[name]
	if !exists {
		h.sshConnMu.Unlock()
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("ssh connection %q not found", name))
		return
	}
	if req.Name != "" && req.Name != name {
		h.sshConnMu.Unlock()
		writeValidationError(w, "an ssh connection cannot be renamed; panes refer to it by name")
		return
	}
	if req.ClearPassword && req.Password != "" {
		h.sshConnMu.Unlock()
		writeValidationError(w, "set either password or clear_password, not both")
		return
	}
	conn := req.connection()
	switch {
	case req.ClearPassword:
	case req.Password != "":
		conn.Password = req.Password
	default:
		conn.Password = existing.Password
	}
	saved, ok := h.storeSSHConnection(w, name, conn)
	h.sshConnMu.Unlock()
	if !ok {
		return
	}
	// The dashboard's open connection was made with the old details; the
	// next collection dials again with the new ones.
	_ = h.tasks.Reconnect(name)
	writeJSON(w, saved)
}

// DeleteConfigSSHConnection removes an entry, unless a pane uses it and
// ~/.ssh/config has no Host block of the same name for the pane to fall back
// to.
func (h *Handler) DeleteConfigSSHConnection(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	name := chi.URLParam(r, "name")

	h.sshConnMu.Lock()
	defer h.sshConnMu.Unlock()
	if _, exists := h.cfg.SSHConnections[name]; !exists {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("ssh connection %q not found", name))
		return
	}
	if panes := h.cfg.PanesUsingConnection(name); len(panes) > 0 && !h.sshConfigHostNames()[name] {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf(
			"ssh connection %q is used by pane %s, and ~/.ssh/config has no Host block of that name; "+
				"change those panes' connection first",
			name, strings.Join(panes, ", ")))
		return
	}
	next := maps.Clone(h.cfg.SSHConnections)
	delete(next, name)
	if !h.replaceSSHConnections(w, next) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// storeSSHConnection validates conn, puts it under name and saves the config.
// The caller holds sshConnMu. On failure the response is written and the
// running config is unchanged.
func (h *Handler) storeSSHConnection(
	w http.ResponseWriter, name string, conn config.SSHConnection,
) (sshConnectionEntry, bool) {
	if err := config.ValidateSSHConnection(conn); err != nil {
		writeValidationError(w, err.Error())
		return sshConnectionEntry{}, false
	}
	if conn.Host == "" {
		hosts, err := sshconfig.ParseHosts(h.sshConfigPath)
		if err != nil {
			writeValidationError(w, fmt.Sprintf(
				"host is required: ~/.ssh/config could not be read to take it from (%v)", err))
			return sshConnectionEntry{}, false
		}
		if !hasSSHConfigHost(hosts, name) {
			writeValidationError(w, fmt.Sprintf(
				"host is required: ~/.ssh/config has no Host block named %q to take it from", name))
			return sshConnectionEntry{}, false
		}
	}
	conn = config.ExpandSSHConnectionPaths(conn)
	next := maps.Clone(h.cfg.SSHConnections)
	if next == nil {
		next = make(map[string]config.SSHConnection)
	}
	next[name] = conn
	if !h.replaceSSHConnections(w, next) {
		return sshConnectionEntry{}, false
	}
	return h.sshConnectionEntry(name, conn, h.sshConfigHostNames()), true
}

// replaceSSHConnections publishes next and saves the config, putting the
// previous map back when the save fails (issue #204). The caller holds
// sshConnMu. The error is not echoed: it can carry nothing useful to the
// dashboard, and the message stays free of anything the entry held.
func (h *Handler) replaceSSHConnections(w http.ResponseWriter, next map[string]config.SSHConnection) bool {
	previous := h.cfg.SSHConnections
	h.cfg.SSHConnections = next
	if err := h.cfg.SaveSSHConnections(); err != nil {
		h.cfg.SSHConnections = previous
		writeJSONError(w, http.StatusInternalServerError, "failed to save ssh_connections")
		return false
	}
	return true
}

func (h *Handler) sshConnectionEntry(
	name string, conn config.SSHConnection, inSSHConfig map[string]bool,
) sshConnectionEntry {
	panes := h.cfg.PanesUsingConnection(name)
	if panes == nil {
		panes = []string{}
	}
	return sshConnectionEntry{
		Name: name, Host: conn.Host, User: conn.User, Port: conn.Port,
		KeyFile: conn.KeyFile, KnownHostsFile: conn.KnownHostsFile,
		HasPassword: conn.Password != "", InSSHConfig: inSSHConfig[name], Panes: panes,
	}
}

// sshConfigHostNames is the set of Host names in ~/.ssh/config; empty when
// the file cannot be read.
func (h *Handler) sshConfigHostNames() map[string]bool {
	names := map[string]bool{}
	hosts, _ := sshconfig.ParseHosts(h.sshConfigPath)
	for _, host := range hosts {
		names[host.Name] = true
	}
	return names
}

func hasSSHConfigHost(hosts []sshconfig.Host, name string) bool {
	for _, host := range hosts {
		if host.Name == name {
			return true
		}
	}
	return false
}

func decodeSSHConnectionRequest(w http.ResponseWriter, r *http.Request) (sshConnectionRequest, bool) {
	var req sshConnectionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, sshConnectionBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return req, false
	}
	return req, true
}

// connection is the request's fields other than the password, which each
// route settles on its own.
func (req sshConnectionRequest) connection() config.SSHConnection {
	return config.SSHConnection{
		Host:           strings.TrimSpace(req.Host),
		User:           strings.TrimSpace(req.User),
		KeyFile:        strings.TrimSpace(req.KeyFile),
		KnownHostsFile: strings.TrimSpace(req.KnownHostsFile),
		Port:           req.Port,
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: msg})
}
