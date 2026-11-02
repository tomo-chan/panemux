package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"

	"github.com/go-chi/chi/v5"

	"panemux/internal/preview"
	"panemux/internal/session"
)

const (
	previewStatusReady  = "ready"
	previewStatusChoose = "choose"
)

const (
	reasonPreviewURLLocal     = "a localhost URL is relayed only for panes on an SSH host"
	reasonPreviewNoConnection = "the pane names no SSH connection to read from"
)

// The kinds of a ready preview: a file served below /preview/, or a
// loopback URL of an SSH host, reached through its relay.
const (
	previewKindFile = "file"
	previewKindURL  = "url"
)

// previewRequest names a clicked path, or a clicked loopback URL.
type previewRequest struct {
	Path string `json:"path"`
	URL  string `json:"url,omitempty"`
}

// previewResponse is either a preview to show (status "ready") or the files
// a relative path may name, for the operator to choose from (status
// "choose").
type previewResponse struct {
	Status string `json:"status"`
	// Kind is "file" or "url" for a ready preview.
	Kind string `json:"kind,omitempty"`
	URL  string `json:"url,omitempty"`
	// Path is the absolute path of the file shown, or the URL as clicked.
	Path string `json:"path,omitempty"`
	// Host is where the file or URL is: "" for the panemux host, otherwise
	// the pane's ssh_connections name.
	Host       string   `json:"host"`
	Candidates []string `json:"candidates,omitempty"`
}

// PostSessionPreview resolves a path the operator clicked in a pane's
// terminal and registers the HTML or Markdown file it names for the split
// preview. Only files inside the pane's working directory and the worktrees
// its agent is working in can be previewed; a relative path is looked for
// in each of them. A file of an ssh or ssh_tmux pane is read from its host,
// and a loopback URL of one is relayed from its host (issue #387). See
// docs/behavior/preview.md.
func (h *Handler) PostSessionPreview(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	sess, ok := h.manager.Get(chi.URLParam(r, "id"))
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	t := sess.Type()
	remote := t == session.TypeSSH || t == session.TypeSSHTmux
	target, named := sess.(session.PreviewTarget)
	if remote && !named {
		http.Error(w, reasonPreviewNoConnection, http.StatusBadRequest)
		return
	}
	if req.URL != "" {
		if !remote {
			http.Error(w, reasonPreviewURLLocal, http.StatusBadRequest)
			return
		}
		h.previewRemoteURL(w, target, req.URL)
		return
	}
	if remote {
		h.previewRemoteFile(w, sess, target, req.Path)
		return
	}

	found, err := preview.Resolve(req.Path, sessionWorkdirs(sess))
	if err != nil {
		writePreviewError(w, err)
		return
	}
	if len(found) > 1 {
		writePreviewChoice(w, found, "")
		return
	}
	url, err := h.previews.Open(found[0].Root, found[0].File)
	if err != nil {
		writePreviewError(w, err)
		return
	}
	writeJSON(w, previewResponse{Status: previewStatusReady, Kind: previewKindFile, URL: url, Path: found[0].File})
}

// previewRemoteFile is PostSessionPreview for a pane whose shell runs on an
// SSH host: the file is found inside the pane's working directories on that
// host and read from it over the preview's own connection.
func (h *Handler) previewRemoteFile(
	w http.ResponseWriter, sess session.Session, target session.PreviewTarget, name string,
) {
	remote := h.previewHosts.remote(target)
	found, err := preview.ResolveRemote(remote, name, sessionWorkdirs(sess))
	if err != nil {
		writePreviewError(w, err)
		return
	}
	if len(found) > 1 {
		writePreviewChoice(w, found, remote.Name())
		return
	}
	url, err := h.previews.OpenRemote(remote, found[0].Root, found[0].File)
	//coverage:exempt ResolveRemote has just found the file in bounds; only a host change in between fails this
	if err != nil {
		writePreviewError(w, err)
		return
	}
	writeJSON(w, previewResponse{
		Status: previewStatusReady, Kind: previewKindFile, URL: url, Path: found[0].File, Host: remote.Name(),
	})
}

// previewRemoteURL relays a loopback URL of a pane's SSH host to a port of
// its own on the panemux host, and answers the URL that reaches it.
func (h *Handler) previewRemoteURL(w http.ResponseWriter, target session.PreviewTarget, raw string) {
	remote := h.previewHosts.remote(target)
	url, err := h.previewRelays.Open(remote, raw)
	if err != nil {
		writePreviewError(w, err)
		return
	}
	writeJSON(w, previewResponse{
		Status: previewStatusReady, Kind: previewKindURL, URL: url, Path: raw, Host: remote.Name(),
	})
}

func writePreviewChoice(w http.ResponseWriter, found []preview.Found, host string) {
	candidates := make([]string, len(found))
	for i, f := range found {
		candidates[i] = f.File
	}
	writeJSON(w, previewResponse{Status: previewStatusChoose, Candidates: candidates, Host: host})
}

// previewHosts holds the preview's SSH hosts. A host is the connection a
// session was dialed with (session.PreviewTarget), never its ssh_connections
// entry as it reads now: an edited entry restarts no pane, so a pane's
// previews stay on the host its shell is on (#359). Sessions dialed alike
// share one host, and with it its relays, however often the entry changes
// in between.
type previewHosts struct {
	dial  func(cfg session.SSHConfig) (preview.RemoteConn, error)
	hosts []previewHost
	mu    sync.Mutex
}

type previewHost struct {
	remote *preview.Remote
	cfg    session.SSHConfig
}

func newPreviewHosts() *previewHosts {
	return &previewHosts{
		//coverage:exempt a connection needs a reachable SSH server; internal/session tests DialPreviewConn against one
		dial: func(cfg session.SSHConfig) (preview.RemoteConn, error) {
			return session.DialPreviewConn(cfg)
		},
	}
}

// SetPreviewDialer replaces how the preview connects to an SSH host.
// internal/server's contract fixture uses it to capture a relayed URL's
// response without a real SSH server.
func (h *Handler) SetPreviewDialer(dial func(cfg session.SSHConfig) (preview.RemoteConn, error)) {
	h.previewHosts.mu.Lock()
	defer h.previewHosts.mu.Unlock()
	h.previewHosts.dial = dial
}

func (p *previewHosts) remote(target session.PreviewTarget) *preview.Remote {
	cfg := target.PreviewSSHConfig()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, host := range p.hosts {
		if reflect.DeepEqual(host.cfg, cfg) {
			return host.remote
		}
	}
	dial := p.dial
	remote := preview.NewRemote(target.ConnectionName(), func() (preview.RemoteConn, error) { return dial(cfg) })
	p.hosts = append(p.hosts, previewHost{remote: remote, cfg: cfg})
	return remote
}

func (p *previewHosts) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, host := range p.hosts {
		host.remote.Close()
	}
}

// ServePreview serves a registered preview and the files beside it.
func (h *Handler) ServePreview(w http.ResponseWriter, r *http.Request) {
	h.previews.Serve(w, r, chi.URLParam(r, "token"), chi.URLParam(r, "*"))
}

// sessionWorkdirs lists where a pane's relative paths may start: its shell's
// working directory, then each worktree its agent is working in.
func sessionWorkdirs(sess session.Session) []string {
	var dirs []string
	if getter, ok := sess.(session.CWDGetter); ok {
		if cwd, err := getter.GetCWD(); err == nil {
			dirs = append(dirs, cwd)
		}
	}
	if getter, ok := sess.(session.ActiveWorkdirGetter); ok {
		if active, err := getter.GetActiveWorkdirs(); err == nil {
			dirs = append(dirs, active...)
		}
	}
	return dirs
}

func writePreviewError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, preview.ErrNotPreviewable):
		status = http.StatusBadRequest
	case errors.Is(err, preview.ErrNoWorkdir):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, preview.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, preview.ErrEscapes):
		status = http.StatusForbidden
	case errors.Is(err, preview.ErrTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, preview.ErrNotLoopbackURL):
		status = http.StatusBadRequest
	case errors.Is(err, preview.ErrRemote), errors.Is(err, preview.ErrNoAnswer):
		status = http.StatusBadGateway
	}
	http.Error(w, err.Error(), status)
}

// SetPreviewRuntime safely installs trusted rendering code for preview requests.
func (h *Handler) SetPreviewRuntime(runtime *preview.MermaidRuntime) {
	h.previews.SetMermaidRuntime(runtime)
}
