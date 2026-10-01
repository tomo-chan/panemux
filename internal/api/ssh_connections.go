package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
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

// sshConnections is the current ssh_connections map, for a caller that does
// not hold cfgMu. The map is never changed in place once published — the
// routes below replace it — so a caller may keep and range over what it got
// after the lock is released. A caller that holds cfgMu reads
// h.cfg.SSHConnections directly: cfgMu is not reentrant.
func (h *Handler) sshConnections() map[string]config.SSHConnection {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	return h.cfg.SSHConnections
}

// connectionNameParam is the {name} path parameter, unescaped. chi matches
// on the escaped path when there is one and hands the parameter back still
// escaped, so a name written by hand in config.yaml with an @, : or / in it
// would otherwise never be found.
func connectionNameParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	name, err := url.PathUnescape(chi.URLParam(r, "name"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid ssh connection name in the path")
		return "", false
	}
	return name, true
}

// GetConfigSSHConnections lists the ssh_connections entries, sorted by name.
func (h *Handler) GetConfigSSHConnections(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	inSSHConfig := h.sshConfigHostNames()
	entries := make([]sshConnectionEntry, 0, len(h.cfg.SSHConnections))
	for name, conn := range h.cfg.SSHConnections {
		entries = append(entries, h.sshConnectionEntry(name, conn, inSSHConfig))
	}
	slices.SortFunc(entries, func(a, b sshConnectionEntry) int { return strings.Compare(a.Name, b.Name) })
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

	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()
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
	name, ok := connectionNameParam(w, r)
	if !ok {
		return
	}
	req, ok := decodeSSHConnectionRequest(w, r)
	if !ok {
		return
	}

	h.cfgMu.Lock()
	existing, exists := h.cfg.SSHConnections[name]
	if !exists {
		h.cfgMu.Unlock()
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("ssh connection %q not found", name))
		return
	}
	if req.Name != "" && req.Name != name {
		h.cfgMu.Unlock()
		writeValidationError(w, "an ssh connection cannot be renamed; panes refer to it by name")
		return
	}
	if req.ClearPassword && req.Password != "" {
		h.cfgMu.Unlock()
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
	h.cfgMu.Unlock()
	if !ok {
		return
	}
	// The dashboard's open connection was made with the old details; the
	// next collection dials again with the new ones.
	_ = h.tasks.Reconnect(name)
	writeJSON(w, saved)
}

// DeleteConfigSSHConnection removes an entry, unless that would leave a pane
// or another dashboard host unable to resolve its connection: one that uses
// the entry by name with no ~/.ssh/config Host block of that name to fall
// back to, or one whose ~/.ssh/config ProxyJump goes through it.
func (h *Handler) DeleteConfigSSHConnection(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	name, ok := connectionNameParam(w, r)
	if !ok {
		return
	}

	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()
	if _, exists := h.cfg.SSHConnections[name]; !exists {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("ssh connection %q not found", name))
		return
	}
	next := maps.Clone(h.cfg.SSHConnections)
	delete(next, name)
	if needed := h.brokenWithout(next); needed != "" {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf(
			"ssh connection %q is needed by %s: without it they no longer resolve their connection; "+
				"change them first", name, needed))
		return
	}
	if !h.replaceSSHConnections(w, next) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// brokenWithout names the panes and dashboard hosts whose connection
// resolves with the current ssh_connections and would not with next, as
// "pane a, b and dashboard host c", or "" when there are none. It resolves
// the way a pane is dialed (session.ResolveSSHConfig), so a ProxyJump through
// the entry counts as much as a direct use. The caller holds cfgMu.
func (h *Handler) brokenWithout(next map[string]config.SSHConnection) string {
	current := h.cfg.SSHConnections
	resolves := func(name string, conns map[string]config.SSHConnection) bool {
		_, err := session.ResolveSSHConfig(name, conns, h.sshConfigPath)
		return err == nil
	}
	breaks := func(name string) bool { return resolves(name, current) && !resolves(name, next) }

	var panes, hosts []string
	for _, pane := range h.cfg.AllPanes() {
		if (pane.Type == config.PaneTypeSSH || pane.Type == config.PaneTypeSSHTmux) && breaks(pane.Connection) {
			panes = append(panes, pane.ID)
		}
	}
	for other := range next {
		if breaks(other) {
			hosts = append(hosts, other)
		}
	}
	slices.Sort(panes)
	slices.Sort(hosts)
	var parts []string
	if len(panes) > 0 {
		parts = append(parts, "pane "+strings.Join(panes, ", "))
	}
	if len(hosts) > 0 {
		parts = append(parts, "dashboard host "+strings.Join(hosts, ", "))
	}
	return strings.Join(parts, " and ")
}

// storeSSHConnection validates conn, puts it under name and saves the config.
// The caller holds cfgMu. On failure the response is written and the
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
// cfgMu. The error is not echoed: it can carry nothing useful to the
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
