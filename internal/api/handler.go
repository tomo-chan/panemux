// Package api provides HTTP REST API handlers for panemux.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/sshconfig"
)

// Handler provides REST API endpoints.
type Handler struct {
	cfg                 *config.Config
	manager             *session.Manager
	editMode            atomic.Bool
	sshConfigPath       string
	codeBinaryPath      string // empty = auto-detect; overridden in tests
	gitBinaryPath       string // empty = auto-detect; overridden in tests
	createSession       func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error)
	detectLocalShellFn  func() (string, error)
	detectRemoteShellFn func(cfg session.SSHConfig) (string, error)
}

type editModeResponse struct {
	EditMode bool `json:"editMode"`
}

type sshConnectionsResponse struct {
	Names []string `json:"names"`
}

type sshConfigHostsResponse struct {
	Hosts []sshConfigHostInfo `json:"hosts"`
}

type sshConfigHostInfo struct {
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	User         string `json:"user"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
}

type sshConfigHostRequest struct {
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	User         string `json:"user"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
}

var validHostName = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

// NewHandler creates a new API handler.
func NewHandler(cfg *config.Config, manager *session.Manager) *Handler {
	h := &Handler{cfg: cfg, manager: manager, sshConfigPath: sshconfig.DefaultPath()}
	h.createSession = session.CreateFromConfig
	h.detectLocalShellFn = session.DetectLocalShell
	h.detectRemoteShellFn = session.DetectRemoteShell
	return h
}

// GetLayout returns the current layout configuration.
func (h *Handler) GetLayout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.Layout)
}

// PutLayout updates the layout configuration and persists it.
func (h *Handler) PutLayout(w http.ResponseWriter, r *http.Request) {
	var layout config.LayoutNode
	if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	config.ExpandLayoutPaths(&layout)

	if err := config.ValidateLayout(layout); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if h.editMode.Load() {
		if err := h.cfg.SaveLayout(layout); err != nil {
			http.Error(w, "failed to save layout", http.StatusInternalServerError)
			return
		}
	} else {
		h.cfg.UpdateLayout(layout)
	}

	writeJSON(w, layout)
}

// GetSessions lists all active sessions.
func (h *Handler) GetSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.manager.List()
	list := make([]sessionInfo, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, sessionInfo{
			ID:    s.ID(),
			Type:  string(s.Type()),
			Title: s.Title(),
			State: string(s.State()),
		})
	}
	writeJSON(w, list)
}

// PostSession creates a new session dynamically.
func (h *Handler) PostSession(w http.ResponseWriter, r *http.Request) {
	var pane config.PaneConfig
	if err := json.NewDecoder(r.Body).Decode(&pane); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	config.ExpandPanePaths(&pane)

	if err := config.ValidatePane(&pane); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if _, exists := h.manager.Get(pane.ID); exists {
		http.Error(w, "session already exists", http.StatusConflict)
		return
	}

	sess, err := h.createSession(&pane, h.cfg.SSHConnections)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.manager.Add(sess)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sessionInfo{
		ID:    sess.ID(),
		Type:  string(sess.Type()),
		Title: sess.Title(),
		State: string(sess.State()),
	})
}

// DeleteSession terminates a session by ID and removes it from the layout.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.manager.Remove(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.cfg.RemovePaneFromLayout(id)
	if h.editMode.Load() {
		h.cfg.SaveLayout(h.cfg.Layout) //nolint:errcheck
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestartSession recreates a session from its original config.
func (h *Handler) RestartSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var found *config.PaneConfig
	for _, p := range h.cfg.AllPanes() {
		if p.ID == id {
			found = p
			break
		}
	}
	if found == nil {
		http.Error(w, "session config not found", http.StatusNotFound)
		return
	}

	h.manager.Remove(id) //nolint:errcheck // ok if already gone

	sess, err := h.createSession(found, h.cfg.SSHConnections)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.manager.Add(sess)
	w.WriteHeader(http.StatusOK)
}

// GetDisplay returns the display configuration.
func (h *Handler) GetDisplay(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.Display)
}

// GetEditMode returns the current edit mode state.
func (h *Handler) GetEditMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, editModeResponse{EditMode: h.editMode.Load()})
}

// PutEditMode sets the edit mode state.
func (h *Handler) PutEditMode(w http.ResponseWriter, r *http.Request) {
	var req editModeResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	h.editMode.Store(req.EditMode)
	if req.EditMode {
		if err := h.cfg.SaveLayout(h.cfg.Layout); err != nil {
			http.Error(w, "failed to save layout", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, editModeResponse{EditMode: h.editMode.Load()})
}

type sessionInfo struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	State string `json:"state"`
}

// GetSSHConnections returns the sorted names of configured SSH connections,
// merging both yaml ssh_connections and ~/.ssh/config hosts (yaml takes precedence on conflict).
func (h *Handler) GetSSHConnections(w http.ResponseWriter, r *http.Request) {
	seen := make(map[string]struct{})
	names := make([]string, 0)

	// First add yaml-configured connections
	for k := range h.cfg.SSHConnections {
		seen[k] = struct{}{}
		names = append(names, k)
	}

	// Then add SSH config hosts not already in the yaml map
	hosts, _ := sshconfig.ParseHosts(h.sshConfigPath)
	for _, host := range hosts {
		if _, exists := seen[host.Name]; !exists {
			names = append(names, host.Name)
		}
	}

	sort.Strings(names)
	writeJSON(w, sshConnectionsResponse{Names: names})
}

// GetSSHConfigHosts returns all hosts from ~/.ssh/config with full details.
func (h *Handler) GetSSHConfigHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := sshconfig.ParseHosts(h.sshConfigPath)
	if err != nil {
		http.Error(w, "failed to read ssh config", http.StatusInternalServerError)
		return
	}

	infos := make([]sshConfigHostInfo, 0, len(hosts))
	for _, host := range hosts {
		infos = append(infos, sshConfigHostInfo{
			Name:         host.Name,
			Hostname:     host.Hostname,
			User:         host.User,
			Port:         host.Port,
			IdentityFile: host.IdentityFile,
		})
	}
	writeJSON(w, sshConfigHostsResponse{Hosts: infos})
}

// PostSSHConfigHost adds a new host to ~/.ssh/config.
func (h *Handler) PostSSHConfigHost(w http.ResponseWriter, r *http.Request) {
	var req sshConfigHostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate
	if req.Name == "" {
		writeValidationError(w, "name is required")
		return
	}
	if !validHostName.MatchString(req.Name) {
		writeValidationError(w, "name must contain only alphanumeric characters, hyphens, underscores, or dots")
		return
	}
	if req.Hostname == "" {
		writeValidationError(w, "hostname is required")
		return
	}
	if req.User == "" {
		writeValidationError(w, "user is required")
		return
	}
	if req.Port < 0 || req.Port > 65535 {
		writeValidationError(w, "port must be between 0 and 65535")
		return
	}

	// Check for duplicate
	hosts, err := sshconfig.ParseHosts(h.sshConfigPath)
	if err != nil {
		http.Error(w, "failed to read ssh config", http.StatusInternalServerError)
		return
	}
	for _, host := range hosts {
		if host.Name == req.Name {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "host already exists"})
			return
		}
	}

	// Append the new host
	if err := sshconfig.AppendHost(h.sshConfigPath, sshconfig.Host{
		Name:         req.Name,
		Hostname:     req.Hostname,
		User:         req.User,
		Port:         req.Port,
		IdentityFile: req.IdentityFile,
	}); err != nil {
		http.Error(w, "failed to write ssh config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

type openVSCodeResponse struct {
	Cwd string `json:"cwd"`
}

// PostOpenVSCode opens VSCode pointed at the session's current working directory.
// For local sessions it runs: code <cwd>
// For SSH sessions it runs: code --remote ssh-remote+<connection> <cwd>
func (h *Handler) PostOpenVSCode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := h.manager.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	cwdGetter, ok := sess.(session.CWDGetter)
	if !ok {
		writeValidationError(w, "this session type does not support CWD detection")
		return
	}

	cwd, err := cwdGetter.GetCWD()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get working directory: %v", err), http.StatusInternalServerError)
		return
	}

	if !h.validateVSCodeCWD(w, sess, cwd) {
		return
	}

	codePath, err := h.findVSCode()
	if err != nil {
		http.Error(
			w,
			"VSCode (code) not found: install VSCode and run 'Install code command in PATH'",
			http.StatusInternalServerError,
		)
		return
	}

	args, ok := vscodeArgs(w, sess, cwd)
	if !ok {
		return
	}

	cmd := exec.Command(codePath, args...) //nolint:gosec // codePath is from trusted lookup
	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf("failed to launch VSCode: %v", err), http.StatusInternalServerError)
		return
	}
	go cmd.Wait() //nolint:errcheck

	writeJSON(w, openVSCodeResponse{Cwd: cwd})
}

func (h *Handler) validateVSCodeCWD(
	w http.ResponseWriter,
	sess session.Session,
	cwd string,
) bool {
	switch sess.Type() {
	case session.TypeLocal, session.TypeTmux:
		if _, err := os.Stat(cwd); err != nil {
			writeValidationError(w, fmt.Sprintf("working directory no longer exists: %s", cwd))
			return false
		}
	}
	return true
}

func vscodeArgs(
	w http.ResponseWriter,
	sess session.Session,
	cwd string,
) ([]string, bool) {
	switch sess.Type() {
	case session.TypeSSH, session.TypeSSHTmux:
		namer, ok := sess.(session.SSHConnNamer)
		if !ok {
			writeValidationError(w, "SSH session missing connection name")
			return nil, false
		}
		connName := namer.ConnectionName()
		if !validHostName.MatchString(connName) {
			writeValidationError(w, "SSH connection name contains invalid characters")
			return nil, false
		}
		return []string{"--remote", "ssh-remote+" + connName, cwd}, true
	default:
		return []string{cwd}, true
	}
}

// findVSCode returns the path to the VSCode CLI binary.
func (h *Handler) findVSCode() (string, error) {
	if h.codeBinaryPath != "" {
		return h.codeBinaryPath, nil
	}
	if p, err := exec.LookPath("code"); err == nil {
		return p, nil
	}
	// macOS fallback: bundled binary inside the .app
	if runtime.GOOS == "darwin" {
		const appBin = "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
		if p, err := exec.LookPath(appBin); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("code binary not found")
}

type detectShellResponse struct {
	Shell string `json:"shell"`
}

// GetDetectShell detects the default shell for a local or SSH connection.
// Without query params: detects the local user's login shell.
// With ?connection=name: SSHs to the named connection and reads $SHELL.
func (h *Handler) GetDetectShell(w http.ResponseWriter, r *http.Request) {
	connection := r.URL.Query().Get("connection")

	var shell string
	var err error

	if connection == "" {
		shell, err = h.detectLocalShellFn()
	} else {
		cfg, cfgErr := session.ResolveSSHConfig(connection, h.cfg.SSHConnections, h.sshConfigPath)
		if cfgErr != nil {
			http.Error(w, cfgErr.Error(), http.StatusNotFound)
			return
		}
		shell, err = h.detectRemoteShellFn(cfg)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, detectShellResponse{Shell: shell})
}

type gitInfoResponse struct {
	IsGit  bool   `json:"is_git"`
	Branch string `json:"branch,omitempty"`
	Repo   string `json:"repo,omitempty"`
}

// GetGitInfo returns git repository information for the session's current working directory.
func (h *Handler) GetGitInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := h.manager.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	cwdGetter, ok := sess.(session.CWDGetter)
	if !ok {
		writeJSON(w, gitInfoResponse{IsGit: false})
		return
	}

	cwd, err := cwdGetter.GetCWD()
	if err != nil {
		writeJSON(w, gitInfoResponse{IsGit: false})
		return
	}

	gitPath, err := h.findGit()
	if err != nil {
		writeJSON(w, gitInfoResponse{IsGit: false})
		return
	}

	toplevelOut, err := exec.Command( //nolint:gosec // gitPath is from trusted lookup
		gitPath,
		"-C",
		cwd,
		"rev-parse",
		"--show-toplevel",
	).Output()
	if err != nil {
		writeJSON(w, gitInfoResponse{IsGit: false})
		return
	}
	repo := filepath.Base(strings.TrimSpace(string(toplevelOut)))

	branchOut, err := exec.Command( //nolint:gosec // gitPath is from trusted lookup
		gitPath,
		"-C",
		cwd,
		"branch",
		"--show-current",
	).Output()
	if err != nil {
		writeJSON(w, gitInfoResponse{IsGit: true, Repo: repo})
		return
	}
	branch := strings.TrimSpace(string(branchOut))

	writeJSON(w, gitInfoResponse{IsGit: true, Branch: branch, Repo: repo})
}

// findGit returns the path to the git binary.
func (h *Handler) findGit() (string, error) {
	if h.gitBinaryPath != "" {
		return h.gitBinaryPath, nil
	}
	return exec.LookPath("git")
}

func writeValidationError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
