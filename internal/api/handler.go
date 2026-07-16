// Package api provides HTTP REST API handlers for panemux.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/sshconfig"
)

// Handler provides REST API endpoints.
//
//nolint:govet // keeps test injection hooks and binary-path overrides on one handler value
type Handler struct {
	cfg                     *config.Config
	manager                 *session.Manager
	sshConfigPath           string
	codeBinaryPath          string // empty = auto-detect; overridden in tests
	ghBinaryPath            string // empty = auto-detect; overridden in tests
	createSession           func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error)
	detectLocalShellFn      func() (string, error)
	detectRemoteShellFn     func(cfg session.SSHConfig) (string, error)
	listLocalDirectoriesFn  func(path string, showHidden bool) (directoryBrowserResponse, error)
	listRemoteDirectoriesFn func(cfg session.SSHConfig, path string, showHidden bool) (directoryBrowserResponse, error)
	readDirFn               func(name string) ([]os.DirEntry, error)
	preferredCWDMu          sync.Mutex
	preferredCWDBySession   map[string][]preferredCWDState
	restartMu               sync.Mutex
	restartInFlight         map[string]struct{}
	nowFn                   func() time.Time
	gitInfoCacheMu          sync.Mutex
	gitInfoCacheBySession   map[string]gitInfoCacheEntry
}

type preferredCWDState struct {
	CWD       string
	CommonDir string
	Root      string
}

// gitInfoCacheEntry holds a previously computed git-info response for a
// session, valid until expiresAt. Caching the whole response — not just the
// remote git/PR lookups — keeps this simple and uniform across local, tmux,
// ssh, and ssh_tmux sessions alike: local sessions also pay for process and
// transcript scanning on every request, so they benefit from the same cap.
type gitInfoCacheEntry struct {
	expiresAt time.Time
	response  gitInfoResponse
}

// gitInfoCacheTTL bounds how long a pane header may show git/PR metadata
// that is no longer perfectly current, in exchange for not re-running
// process/transcript scans, remote git inspection, and `gh pr view` lookups
// on every poll. See docs/behavior.md "Pane Git and PR metadata".
const gitInfoCacheTTL = 30 * time.Second

// activeGitContext pairs a resolved git context with the working directory
// (pane base cwd, or an agent-signaled sibling worktree path) it was
// inspected from.
type activeGitContext struct {
	CWD string
	Ctx session.GitContext
}

var gitExistsFn = func() error {
	_, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("finding git binary: %w", err)
	}
	return nil
}

var prLookupTimeout = 5 * time.Second

// localGitCommandTimeout bounds how long a single local `git` invocation used
// for pane git-context inspection (rev-parse, branch, config) may block
// before being killed. Without this, a hung local git process (e.g. a stuck
// network filesystem mount under the pane's cwd) left GetGitInfo blocked
// indefinitely. Kept separate from prLookupTimeout so shrinking one in tests
// does not affect the other.
var localGitCommandTimeout = 5 * time.Second

const responseErrorKey = "error"

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
	IdentityFile string `json:"identity_file,omitempty"`
	Port         int    `json:"port,omitempty"`
}

type sshConfigHostRequest struct {
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	User         string `json:"user"`
	IdentityFile string `json:"identity_file,omitempty"`
	Port         int    `json:"port,omitempty"`
}

type activeWorkspaceRequest struct {
	ID string `json:"id"`
}

type workspaceRequest struct {
	Title string `json:"title"`
}

type workspaceTabPositionRequest struct {
	TabPosition string `json:"tab_position"`
}

type workspaceVerticalBarWidthRequest struct {
	VerticalBarWidth int `json:"vertical_bar_width"`
}

type directoryEntryResponse struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	HasChildren bool   `json:"has_children"`
}

type directoryBrowserResponse struct {
	Path    string                   `json:"path"`
	Entries []directoryEntryResponse `json:"entries"`
}

var validHostName = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

// NewHandler creates a new API handler.
func NewHandler(cfg *config.Config, manager *session.Manager) *Handler {
	h := &Handler{
		cfg:                   cfg,
		manager:               manager,
		sshConfigPath:         sshconfig.DefaultPath(),
		preferredCWDBySession: make(map[string][]preferredCWDState),
		restartInFlight:       make(map[string]struct{}),
		nowFn:                 time.Now,
		gitInfoCacheBySession: make(map[string]gitInfoCacheEntry),
	}
	h.createSession = session.CreateFromConfig
	h.detectLocalShellFn = session.DetectLocalShell
	h.detectRemoteShellFn = session.DetectRemoteShell
	h.readDirFn = os.ReadDir
	h.listLocalDirectoriesFn = h.listLocalDirectories
	h.listRemoteDirectoriesFn = listRemoteDirectories
	return h
}

// GetLayout returns the current layout configuration.
func (h *Handler) GetLayout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.ActiveLayout())
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
		_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: err.Error()})
		return
	}

	if err := h.cfg.SaveLayout(layout); err != nil {
		http.Error(w, "failed to save layout", http.StatusInternalServerError)
		return
	}

	writeJSON(w, layout)
}

// GetWorkspaces returns the configured workspaces and active workspace.
func (h *Handler) GetWorkspaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.WorkspacesView())
}

// PutActiveWorkspace switches the active workspace.
func (h *Handler) PutActiveWorkspace(w http.ResponseWriter, r *http.Request) {
	var req activeWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !h.cfg.SetActiveWorkspace(req.ID) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if err := h.cfg.SaveWorkspaces(); err != nil {
		http.Error(w, "failed to save workspaces", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.cfg.WorkspacesView())
}

// PutWorkspaceTabPosition updates workspace tab placement.
func (h *Handler) PutWorkspaceTabPosition(w http.ResponseWriter, r *http.Request) {
	var req workspaceTabPositionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	h.applyWorkspaceSettingUpdate(w, h.cfg.SetWorkspaceTabPosition(req.TabPosition))
}

// PutWorkspaceVerticalBarWidth updates the shared vertical workspace bar width.
func (h *Handler) PutWorkspaceVerticalBarWidth(w http.ResponseWriter, r *http.Request) {
	var req workspaceVerticalBarWidthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	h.applyWorkspaceSettingUpdate(w, h.cfg.SetWorkspaceVerticalBarWidth(req.VerticalBarWidth))
}

func (h *Handler) applyWorkspaceSettingUpdate(w http.ResponseWriter, updateErr error) {
	if updateErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: updateErr.Error()})
		return
	}
	if err := h.cfg.SaveWorkspaces(); err != nil {
		http.Error(w, "failed to save workspaces", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.cfg.WorkspacesView())
}

// PostWorkspace adds a new default local workspace and makes it active.
func (h *Handler) PostWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace := h.cfg.AddDefaultWorkspace()
	for _, pane := range panesInLayout(workspace.Layout) {
		sess, err := h.createSession(pane, h.cfg.SSHConnections)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create session: %v", err), http.StatusInternalServerError)
			return
		}
		h.manager.Add(sess)
	}
	if err := h.cfg.SaveWorkspaces(); err != nil {
		http.Error(w, "failed to save workspaces", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(h.cfg.WorkspacesView())
}

// DeleteWorkspace removes a workspace.
func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	view := h.cfg.WorkspacesView()
	if len(view.Items) <= 1 {
		http.Error(w, "cannot delete the last workspace", http.StatusConflict)
		return
	}
	workspace, ok := h.cfg.RemoveWorkspace(id)
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if err := h.cfg.SaveWorkspaces(); err != nil {
		http.Error(w, "failed to save workspaces", http.StatusInternalServerError)
		return
	}
	for _, pane := range panesInLayout(workspace.Layout) {
		_ = h.manager.Remove(pane.ID)
		h.clearPreferredCWDs(pane.ID)
		h.clearGitInfoCache(pane.ID)
	}
	writeJSON(w, h.cfg.WorkspacesView())
}

// PutWorkspace updates workspace metadata.
func (h *Handler) PutWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req workspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: "workspace title must not be empty"})
		return
	}
	if !h.cfg.RenameWorkspace(id, title) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if err := h.cfg.SaveWorkspaces(); err != nil {
		http.Error(w, "failed to save workspaces", http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.cfg.WorkspacesView())
}

// PutWorkspaceLayout updates a specific workspace layout.
func (h *Handler) PutWorkspaceLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var layout config.LayoutNode
	if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	config.ExpandLayoutPaths(&layout)

	if err := config.ValidateLayout(layout); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: err.Error()})
		return
	}

	if !h.cfg.UpdateWorkspaceLayout(id, layout) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if err := h.cfg.SaveWorkspaces(); err != nil {
		http.Error(w, "failed to save workspaces", http.StatusInternalServerError)
		return
	}
	writeJSON(w, layout)
}

func panesInLayout(layout config.LayoutNode) []*config.PaneConfig {
	var panes []*config.PaneConfig
	var walk func([]config.LayoutChild)
	walk = func(children []config.LayoutChild) {
		for i := range children {
			if children[i].Pane != nil {
				panes = append(panes, children[i].Pane)
			}
			walk(children[i].Children)
		}
	}
	walk(layout.Children)
	return panes
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
		_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: err.Error()})
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
	h.clearPreferredCWDs(id)
	h.clearGitInfoCache(id)
	h.cfg.RemovePaneFromLayout(id)
	if err := h.cfg.SaveLayout(h.cfg.Layout); err != nil {
		http.Error(w, "failed to save layout", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RestartSession recreates a session from its original config.
func (h *Handler) RestartSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Creating the replacement session happens outside the manager's lock (it
	// can block on a real SSH dial), so two concurrent restarts for the same
	// pane would otherwise each build their own session independently; the
	// one that finishes last would win the manager swap while the other's
	// session is silently discarded unused. Serialize per-id instead of
	// letting that race play out.
	if !h.beginRestart(id) {
		http.Error(w, "a restart is already in progress for this session", http.StatusConflict)
		return
	}
	defer h.endRestart(id)

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

	// Create the replacement session before touching the manager. If this fails
	// (e.g. a transient SSH dial error), the existing session for id stays
	// registered instead of being orphaned, so /ws and /git-info keep working
	// against it and the frontend's disconnected-status recovery path can retry.
	sess, err := h.createSession(found, h.cfg.SSHConnections)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.manager.Remove(id) //nolint:errcheck // ok if already gone
	h.clearPreferredCWDs(id)
	h.clearGitInfoCache(id)
	h.manager.Add(sess)
	w.WriteHeader(http.StatusOK)
}

// GetDisplay returns the display configuration.
func (h *Handler) GetDisplay(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cfg.Display)
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
			_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: "host already exists"})
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
// When an interactive Codex or Claude agent is actively working in a sibling git
// worktree for the same repository, panemux prefers that worktree instead and
// keeps the last valid sibling worktree pinned until the pane changes repo context.
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

	cwd = h.resolveSinglePreferredCWD(sess, cwd)

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

	args, err := vscodeArgs(sess, cwd)
	if err != nil {
		writeValidationError(w, err.Error())
		return
	}

	// Launch VSCode detached — we do not wait for it to exit.
	cmd := exec.Command(codePath, args...)
	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf("failed to launch VSCode: %v", err), http.StatusInternalServerError)
		return
	}
	go cmd.Wait() //nolint:errcheck

	writeJSON(w, openVSCodeResponse{Cwd: cwd})
}

// validateVSCodeCWD checks that the CWD is still accessible for local/tmux sessions.
// A shell can remain in a directory after it has been deleted (the inode is kept alive
// by the open CWD reference); passing a deleted path to `code` causes VSCode to open
// files in an unsaved/detached state.
func (h *Handler) validateVSCodeCWD(
	w http.ResponseWriter,
	sess session.Session,
	cwd string,
) bool {
	switch sess.Type() {
	case session.TypeLocal, session.TypeTmux:
		safeCWD, err := sanitizeGitExecDir(cwd)
		if err != nil {
			writeValidationError(w, err.Error())
			return false
		}
		if _, err := h.readDirFn(safeCWD); err != nil && !errors.Is(err, fs.ErrPermission) {
			writeValidationError(w, "working directory no longer exists: "+cwd)
			return false
		}
	}
	return true
}

func vscodeArgs(sess session.Session, cwd string) ([]string, error) {
	switch sess.Type() {
	case session.TypeSSH, session.TypeSSHTmux:
		namer, ok := sess.(session.SSHConnNamer)
		if !ok {
			return nil, errors.New("SSH session missing connection name")
		}
		connName := namer.ConnectionName()
		if !validHostName.MatchString(connName) {
			return nil, errors.New("SSH connection name contains invalid characters")
		}
		return []string{"--remote", "ssh-remote+" + connName, cwd}, nil
	default:
		return []string{cwd}, nil
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
	return "", errors.New("code binary not found")
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

// GetDirectories returns a directory listing for the local or remote filesystem.
func (h *Handler) GetDirectories(w http.ResponseWriter, r *http.Request) {
	showHidden := false
	showHiddenParam := r.URL.Query().Get("show_hidden")
	if showHiddenParam != "" {
		parsed, err := strconv.ParseBool(showHiddenParam)
		if err != nil {
			http.Error(w, "invalid show_hidden value", http.StatusBadRequest)
			return
		}
		showHidden = parsed
	}

	path := r.URL.Query().Get("path")
	connection := r.URL.Query().Get("connection")

	var (
		resp directoryBrowserResponse
		err  error
	)
	if connection == "" {
		resp, err = h.listLocalDirectoriesFn(path, showHidden)
	} else {
		cfg, cfgErr := session.ResolveSSHConfig(connection, h.cfg.SSHConnections, h.sshConfigPath)
		if cfgErr != nil {
			http.Error(w, cfgErr.Error(), http.StatusNotFound)
			return
		}
		resp, err = h.listRemoteDirectoriesFn(cfg, path, showHidden)
	}
	if err != nil {
		writeValidationError(w, err.Error())
		return
	}

	writeJSON(w, resp)
}

type worktreeInfo struct {
	Branch   string `json:"branch,omitempty"`
	Repo     string `json:"repo,omitempty"`
	RepoURL  string `json:"repo_url,omitempty"`
	PRURL    string `json:"pr_url,omitempty"`
	PRNumber int    `json:"pr_number,omitempty"`
}

type gitInfoResponse struct {
	Branch    string         `json:"branch,omitempty"`
	PRURL     string         `json:"pr_url,omitempty"`
	Repo      string         `json:"repo,omitempty"`
	RepoURL   string         `json:"repo_url,omitempty"`
	Worktrees []worktreeInfo `json:"worktrees,omitempty"`
	PRNumber  int            `json:"pr_number,omitempty"`
	IsGit     bool           `json:"is_git"`
}

// GetGitInfo returns git repository information for the session's current working directory.
func (h *Handler) GetGitInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := h.manager.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if resp, ok := h.cachedGitInfo(id); ok {
		writeJSON(w, resp)
		return
	}

	resp := h.computeGitInfo(sess)
	h.storeGitInfoCache(id, resp)
	writeJSON(w, resp)
}

// computeGitInfo resolves a session's current git/PR metadata. This is the
// expensive path: process and transcript scanning, remote git inspection
// over SSH, and `gh pr view` lookups. Callers should go through the
// gitInfoCacheBySession TTL cache in GetGitInfo rather than calling this
// directly on every request.
func (h *Handler) computeGitInfo(sess session.Session) gitInfoResponse {
	cwdGetter, ok := sess.(session.CWDGetter)
	if !ok {
		return gitInfoResponse{IsGit: false}
	}

	cwd, err := cwdGetter.GetCWD()
	if err != nil {
		return gitInfoResponse{IsGit: false}
	}

	err = gitExistsFn()
	if err != nil {
		return gitInfoResponse{IsGit: false}
	}

	activeContexts, err := h.resolveActiveGitContexts(sess, cwd)
	if err != nil {
		return gitInfoResponse{IsGit: false}
	}

	worktrees := h.lookupPRInfoForContexts(sess, activeContexts)
	primary := worktrees[0]

	return gitInfoResponse{
		IsGit:     true,
		Branch:    primary.Branch,
		Repo:      primary.Repo,
		RepoURL:   primary.RepoURL,
		PRURL:     primary.PRURL,
		PRNumber:  primary.PRNumber,
		Worktrees: worktrees,
	}
}

// cachedGitInfo returns a still-valid cached git-info response for a
// session, if one exists.
func (h *Handler) cachedGitInfo(sessionID string) (gitInfoResponse, bool) {
	h.gitInfoCacheMu.Lock()
	defer h.gitInfoCacheMu.Unlock()

	entry, ok := h.gitInfoCacheBySession[sessionID]
	if !ok || h.nowFn().After(entry.expiresAt) {
		return gitInfoResponse{}, false
	}
	return entry.response, true
}

// storeGitInfoCache records a freshly computed git-info response, valid for
// gitInfoCacheTTL.
func (h *Handler) storeGitInfoCache(sessionID string, resp gitInfoResponse) {
	h.gitInfoCacheMu.Lock()
	defer h.gitInfoCacheMu.Unlock()

	h.gitInfoCacheBySession[sessionID] = gitInfoCacheEntry{
		response:  resp,
		expiresAt: h.nowFn().Add(gitInfoCacheTTL),
	}
}

// clearGitInfoCache discards any cached git-info response for a session, so
// a removed or recreated session doesn't serve another session's stale data
// under the same ID.
func (h *Handler) clearGitInfoCache(sessionID string) {
	h.gitInfoCacheMu.Lock()
	defer h.gitInfoCacheMu.Unlock()

	delete(h.gitInfoCacheBySession, sessionID)
}

// lookupPRInfoForContexts runs an independent PR lookup for each active git
// context concurrently, since each worktree's `gh pr view` call is
// independent network I/O with its own timeout.
func (h *Handler) lookupPRInfoForContexts(sess session.Session, contexts []activeGitContext) []worktreeInfo {
	results := make([]worktreeInfo, len(contexts))
	var wg sync.WaitGroup
	for i, active := range contexts {
		wg.Add(1)
		go func(i int, active activeGitContext) {
			defer wg.Done()
			prURL, prNumber := h.lookupPRInfo(sess, active.CWD, active.Ctx)
			results[i] = worktreeInfo{
				Branch:   active.Ctx.Branch,
				Repo:     active.Ctx.Repo,
				RepoURL:  h.repoPageURLFromOriginURL(active.Ctx.OriginURL),
				PRURL:    prURL,
				PRNumber: prNumber,
			}
		}(i, active)
	}
	wg.Wait()
	return results
}

// resolveActiveGitContexts returns the git context(s) the pane header should
// show. When the session's active agent has diverged into one or more
// sibling worktrees (same repository, different Root) — e.g. a Claude Task
// subagent that did work in another worktree while the top-level session
// stayed put — those diverged worktrees are returned instead of the pane's
// own base context, exactly as a single diverged worktree has always taken
// priority over the base directory. The base context is returned only when
// nothing has diverged from it. Falls back to a per-session "sticky" set
// remembered from the previous successful resolution when the active-workdir
// lookup transiently fails or returns nothing, so a single flaky poll does
// not make the pane header drop worktrees it was already showing.
func (h *Handler) resolveActiveGitContexts(sess session.Session, cwd string) ([]activeGitContext, error) {
	logScope := h.gitInfoLogScope(sess)

	baseCtx, err := h.inspectGitContextForSession(sess, cwd)
	if err != nil {
		log.Printf("%s", formatGitContextLookupError(logScope, "base context lookup", err))
		return nil, err
	}

	activeGetter, ok := sess.(session.ActiveWorkdirGetter)
	if !ok {
		h.clearPreferredCWDs(sess.ID())
		return []activeGitContext{{CWD: cwd, Ctx: baseCtx}}, nil
	}

	candidates, err := activeGetter.GetActiveWorkdirs()
	if err != nil {
		log.Printf("%s active workdirs lookup failed: %v", logScope, err)
	}
	if err != nil || len(candidates) == 0 {
		candidates = h.recallPreferredCWDs(sess.ID(), baseCtx)
	}

	seenRoots := map[string]bool{baseCtx.Root: true}
	var diverged []activeGitContext
	var accepted []preferredCWDState
	for _, candidate := range candidates {
		ctx, err := h.inspectGitContextForSession(sess, candidate)
		if err != nil {
			log.Printf("%s", formatGitContextLookupError(logScope, "active workdir candidate context lookup", err))
			continue
		}
		if ctx.CommonDir != baseCtx.CommonDir || ctx.Root == baseCtx.Root || seenRoots[ctx.Root] {
			continue
		}
		seenRoots[ctx.Root] = true
		diverged = append(diverged, activeGitContext{CWD: candidate, Ctx: ctx})
		accepted = append(accepted, preferredCWDState{CWD: candidate, CommonDir: ctx.CommonDir, Root: ctx.Root})
		log.Printf(
			"%s selected active workdir branch transition %q -> %q",
			logScope,
			baseCtx.Branch,
			ctx.Branch,
		)
	}

	if len(accepted) == 0 {
		h.clearPreferredCWDs(sess.ID())
		return []activeGitContext{{CWD: cwd, Ctx: baseCtx}}, nil
	}
	h.rememberPreferredCWDs(sess.ID(), accepted)
	return diverged, nil
}

// resolveSinglePreferredCWD returns the one working directory most relevant
// for an action that can only target a single location (e.g. opening an
// editor): the first sibling worktree path signaled by the active agent, or
// the pane's own cwd when none diverges.
func (h *Handler) resolveSinglePreferredCWD(sess session.Session, cwd string) string {
	results, err := h.resolveActiveGitContexts(sess, cwd)
	if err != nil || len(results) == 0 {
		return cwd
	}
	return results[0].CWD
}

func (h *Handler) rememberPreferredCWDs(sessionID string, states []preferredCWDState) {
	h.preferredCWDMu.Lock()
	defer h.preferredCWDMu.Unlock()

	h.preferredCWDBySession[sessionID] = states
}

func (h *Handler) recallPreferredCWDs(sessionID string, baseCtx session.GitContext) []string {
	h.preferredCWDMu.Lock()
	defer h.preferredCWDMu.Unlock()

	states, ok := h.preferredCWDBySession[sessionID]
	if !ok {
		return nil
	}

	var cwds []string
	for _, state := range states {
		if state.CommonDir != baseCtx.CommonDir || state.Root == baseCtx.Root || state.CWD == "" {
			continue
		}
		cwds = append(cwds, state.CWD)
	}
	if len(cwds) == 0 {
		delete(h.preferredCWDBySession, sessionID)
	}
	return cwds
}

func (h *Handler) clearPreferredCWDs(sessionID string) {
	h.preferredCWDMu.Lock()
	defer h.preferredCWDMu.Unlock()

	delete(h.preferredCWDBySession, sessionID)
}

// beginRestart claims the per-id restart guard, returning false if a restart
// for id is already in progress.
func (h *Handler) beginRestart(id string) bool {
	h.restartMu.Lock()
	defer h.restartMu.Unlock()

	if _, ok := h.restartInFlight[id]; ok {
		return false
	}
	h.restartInFlight[id] = struct{}{}
	return true
}

// endRestart releases the per-id restart guard claimed by beginRestart.
func (h *Handler) endRestart(id string) {
	h.restartMu.Lock()
	defer h.restartMu.Unlock()

	delete(h.restartInFlight, id)
}

func (h *Handler) gitInfoLogScope(sess session.Session) string {
	return fmt.Sprintf("git info pane=%q type=%q", sess.ID(), sess.Type())
}

func (h *Handler) inspectGitContextForSession(sess session.Session, cwd string) (session.GitContext, error) {
	if getter, ok := sess.(session.GitContextGetter); ok {
		ctx, err := getter.InspectGitContext(cwd)
		if err != nil {
			return session.GitContext{}, fmt.Errorf("inspect session git context: %w", err)
		}
		return ctx, nil
	}
	return h.inspectLocalGitContext(cwd)
}

// inspectLocalGitContext runs up to four sequential git subprocesses
// (rev-parse --show-toplevel, rev-parse --git-common-dir, then branch and
// origin lookups inside localGitOptionalMetadata) against a single shared
// deadline, rather than giving each an independent localGitCommandTimeout.
// A stuck working directory (e.g. a hung network filesystem mount) makes
// every git invocation against it hang the same way, so without a shared
// deadline the calls could accumulate up to 4x localGitCommandTimeout
// before the whole lookup fails through.
func (h *Handler) inspectLocalGitContext(cwd string) (session.GitContext, error) {
	safeCWD, err := sanitizeGitExecDir(cwd)
	if err != nil {
		return session.GitContext{}, session.NewGitContextError(
			"local",
			"validate local working directory",
			cwd,
			session.GitContextCauseInvalidCWD,
			err,
			"",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), localGitCommandTimeout)
	defer cancel()

	toplevelOut, err := runLocalGitContextCommand(
		ctx,
		safeCWD,
		cwd,
		"git rev-parse --show-toplevel",
		"rev-parse",
		"--show-toplevel",
	)
	if err != nil {
		return session.GitContext{}, err
	}
	toplevelOut = bytes.TrimSpace(toplevelOut)

	commonDirOut, err := runLocalGitContextCommand(
		ctx,
		safeCWD,
		cwd,
		"git rev-parse --path-format=absolute --git-common-dir",
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	)
	if err != nil {
		return session.GitContext{}, err
	}
	commonDirOut = bytes.TrimSpace(commonDirOut)

	branchOut, originOut := localGitOptionalMetadata(ctx, safeCWD)
	root := string(toplevelOut)
	return session.GitContext{
		Branch:    string(branchOut),
		CommonDir: string(commonDirOut),
		OriginURL: string(originOut),
		Repo:      filepath.Base(root),
		Root:      root,
	}, nil
}

func localGitOptionalMetadata(ctx context.Context, safeCWD string) ([]byte, []byte) {
	branchCmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	branchCmd.Dir = safeCWD
	branchOut, err := branchCmd.Output()
	if err != nil {
		// Detached HEAD returns non-zero for --show-current. Treat that as
		// a valid git context with an empty branch instead of a fatal error.
		branchOut = nil
	}
	branchOut = bytes.TrimSpace(branchOut)

	originCmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	originCmd.Dir = safeCWD
	originOut, err := originCmd.Output()
	if err != nil {
		originOut = nil
	}
	originOut = bytes.TrimSpace(originOut)
	return branchOut, originOut
}

func runLocalGitContextCommand(
	ctx context.Context, safeCWD, originalCWD, operation string, args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = safeCWD
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}

	stderr := ""
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = string(exitErr.Stderr)
	}

	cause := session.ClassifyGitFailureCause(stderr, err)
	if cause == session.GitContextCauseUnknown && errors.Is(err, exec.ErrNotFound) {
		cause = session.GitContextCauseGitNotInstalled
	}
	if cause == session.GitContextCauseUnknown {
		cause = session.GitContextCauseGitMetadata
	}

	return nil, session.NewGitContextError("local", operation, originalCWD, cause, err, stderr)
}

var validGitExecDir = regexp.MustCompile(`^(/[^[:cntrl:]\x00]*)+$`)

func sanitizeGitExecDir(cwd string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", errors.New("working directory must be an absolute path")
	}
	cleaned := filepath.Clean(cwd)
	if !validGitExecDir.MatchString(cleaned) {
		return "", fmt.Errorf("working directory contains invalid characters: %q", cwd)
	}
	return cleaned, nil
}

func formatGitContextLookupError(logScope, source string, err error) string {
	var ctxErr *session.GitContextError
	if errors.As(err, &ctxErr) {
		return fmt.Sprintf(
			"%s git context lookup failed: source=%q cause=%q remediation=%q transport=%q cwd=%q "+
				`operation=%q stderr=%q raw_error=%q`,
			logScope,
			source,
			ctxErr.CauseMessage,
			ctxErr.Remediation,
			ctxErr.Transport,
			ctxErr.CWD,
			ctxErr.Operation,
			singleLineLogValue(ctxErr.Stderr),
			singleLineLogValue(ctxErr.RawError),
		)
	}

	// Keep a defensive fallback here so future callers that wrap a non-GitContextError
	// still produce an actionable log instead of regressing to a raw `%v` message.
	return fmt.Sprintf(
		`%s git context lookup failed: source=%q cause=%q remediation=%q raw_error=%q`,
		logScope,
		source,
		"Git context lookup failed for an unknown reason",
		"inspect the raw error details to determine the next action",
		singleLineLogValue(err.Error()),
	)
}

func singleLineLogValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func (h *Handler) findGH() (string, error) {
	if h.ghBinaryPath != "" {
		return h.ghBinaryPath, nil
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("finding gh binary: %w", err)
	}
	return path, nil
}

func (h *Handler) lookupPRInfo(sess session.Session, cwd string, gitCtx session.GitContext) (string, int) {
	if gitCtx.Branch == "" {
		return "", 0
	}

	ghPath, err := h.findGH()
	if err != nil {
		return "", 0
	}

	timeout := prLookupTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		ghPath,
		"pr",
		"view",
		gitCtx.Branch,
		"--json",
		"url,number",
	)
	if repoSpec := h.repoSpecFromOriginURL(gitCtx.OriginURL); repoSpec != "" {
		cmd.Args = append(cmd.Args, "--repo", repoSpec)
	} else if _, ok := sess.(session.GitContextGetter); ok {
		// Remote SSH-backed sessions may point at repositories that do not exist
		// on the local filesystem. Without an origin-derived repo spec, `gh`
		// cannot resolve PR metadata for that remote-only checkout.
		return "", 0
	} else {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil {
		return "", 0
	}

	var resp struct {
		URL    string `json:"url"`
		Number int    `json:"number"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", 0
	}
	return strings.TrimSpace(resp.URL), resp.Number
}

func repoSpecFromOriginURL(origin string) string {
	host, path := repoHostAndPathFromOriginURL(origin)
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

func (h *Handler) repoSpecFromOriginURL(origin string) string {
	host, path, scpStyle := repoHostAndPathFromOriginURLDetailed(origin)
	if host == "" || path == "" {
		return ""
	}
	if scpStyle {
		host = h.resolveSSHConfigHostAlias(host)
	}
	return host + "/" + path
}

func repoPageURLFromOriginURL(origin string) string {
	host, path := repoHostAndPathFromOriginURL(origin)
	if host == "" || path == "" {
		return ""
	}
	return "https://" + host + "/" + path
}

func (h *Handler) repoPageURLFromOriginURL(origin string) string {
	host, path, scpStyle := repoHostAndPathFromOriginURLDetailed(origin)
	if host == "" || path == "" {
		return ""
	}
	if scpStyle {
		host = h.resolveSSHConfigHostAlias(host)
	}
	return "https://" + host + "/" + path
}

func (h *Handler) resolveSSHConfigHostAlias(host string) string {
	hosts, err := sshconfig.ParseHosts(h.sshConfigPath)
	if err != nil {
		log.Printf("parse ssh config for repo URL alias resolution failed: %v", err)
		return host
	}
	for _, candidate := range hosts {
		if candidate.Name == host {
			if candidate.Hostname != "" {
				return candidate.Hostname
			}
			return host
		}
	}
	return host
}

func repoHostAndPathFromOriginURL(origin string) (string, string) {
	host, path, _ := repoHostAndPathFromOriginURLDetailed(origin)
	return host, path
}

func repoHostAndPathFromOriginURLDetailed(origin string) (string, string, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(origin, ".git"))
	if trimmed == "" {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "git@") {
		hostPath := strings.TrimPrefix(trimmed, "git@")
		parts := strings.SplitN(hostPath, ":", 2)
		if len(parts) != 2 {
			return "", "", false
		}
		host := strings.TrimSpace(parts[0])
		path := strings.TrimPrefix(strings.TrimSpace(parts[1]), "/")
		if host == "" || path == "" {
			return "", "", false
		}
		return host, path, true
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	host := u.Hostname()
	path := strings.TrimPrefix(u.Path, "/")
	if host == "" || path == "" {
		return "", "", false
	}
	return host, path, false
}

func writeValidationError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(map[string]string{responseErrorKey: msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) listLocalDirectories(path string, showHidden bool) (directoryBrowserResponse, error) {
	resolvedPath, err := resolveLocalDirectoryBrowsePath(path)
	if err != nil {
		return directoryBrowserResponse{}, err
	}

	entries, err := h.readDirFn(resolvedPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return directoryBrowserResponse{}, errors.New("directory path does not exist")
		}
		if errors.Is(err, fs.ErrPermission) {
			return directoryBrowserResponse{}, fmt.Errorf("reading directory: %w", err)
		}
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) && errors.Is(pathErr.Err, fs.ErrInvalid) {
			return directoryBrowserResponse{}, errors.New("path is not a directory")
		}
		// readDir on a plain file commonly reports a PathError with Op "readdir".
		if errors.As(err, &pathErr) && pathErr.Op == "readdir" {
			return directoryBrowserResponse{}, errors.New("path is not a directory")
		}
		return directoryBrowserResponse{}, fmt.Errorf("reading directory: %w", err)
	}

	resp := directoryBrowserResponse{
		Path:    resolvedPath,
		Entries: []directoryEntryResponse{},
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		childPath := filepath.Join(resolvedPath, entry.Name())
		hasChildren, childErr := h.localDirectoryHasChildren(childPath)
		if childErr != nil {
			if errors.Is(childErr, fs.ErrPermission) {
				continue
			}
			return directoryBrowserResponse{}, fmt.Errorf("reading directory: %w", childErr)
		}
		resp.Entries = append(resp.Entries, directoryEntryResponse{
			Name:        entry.Name(),
			Path:        childPath,
			HasChildren: hasChildren,
		})
	}
	sort.Slice(resp.Entries, func(i, j int) bool {
		return strings.ToLower(resp.Entries[i].Name) < strings.ToLower(resp.Entries[j].Name)
	})
	return resp, nil
}

func listRemoteDirectories(cfg session.SSHConfig, path string, showHidden bool) (directoryBrowserResponse, error) {
	entries, resolvedPath, err := session.ListRemoteDirectories(cfg, path, showHidden)
	if err != nil {
		return directoryBrowserResponse{}, fmt.Errorf("listing remote directories: %w", err)
	}
	resp := directoryBrowserResponse{
		Path:    resolvedPath,
		Entries: make([]directoryEntryResponse, 0, len(entries)),
	}
	for _, entry := range entries {
		resp.Entries = append(resp.Entries, directoryEntryResponse{
			Name:        entry.Name,
			Path:        entry.Path,
			HasChildren: entry.HasChildren,
		})
	}
	return resp, nil
}

func resolveLocalDirectoryBrowsePath(path string) (string, error) {
	if path == "" || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		return filepath.Clean(home), nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		return filepath.Clean(filepath.Join(home, path[2:])), nil
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}
	return filepath.Clean(absPath), nil
}

func (h *Handler) localDirectoryHasChildren(path string) (bool, error) {
	entries, err := h.readDirFn(path)
	if err != nil {
		return false, fmt.Errorf("read dir %s: %w", path, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true, nil
		}
	}
	return false, nil
}
