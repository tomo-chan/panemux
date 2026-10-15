package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"panemux/internal/preview"
	"panemux/internal/session"
)

const (
	previewStatusReady  = "ready"
	previewStatusChoose = "choose"
)

const reasonPreviewRemote = "file preview is available only for panes on the panemux host"

type previewRequest struct {
	Path string `json:"path"`
}

// previewResponse is either a preview to show (status "ready") or the files
// a relative path may name, for the operator to choose from (status
// "choose").
type previewResponse struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	// Path is the absolute path of the file shown.
	Path string `json:"path,omitempty"`
	// Host is where the file is: "" for the panemux host.
	Host       string   `json:"host"`
	Candidates []string `json:"candidates,omitempty"`
}

// PostSessionPreview resolves a path the operator clicked in a pane's
// terminal and registers the HTML or Markdown file it names for the split
// preview. A relative path is looked for in the pane's working directory and
// in the worktrees its agent is working in. See docs/behavior/preview.md.
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
	if t := sess.Type(); t != session.TypeLocal && t != session.TypeTmux {
		http.Error(w, reasonPreviewRemote, http.StatusBadRequest)
		return
	}

	workdirs := sessionWorkdirs(sess)
	files, err := preview.Resolve(req.Path, workdirs)
	if err != nil {
		writePreviewError(w, err)
		return
	}
	if len(files) > 1 {
		writeJSON(w, previewResponse{Status: previewStatusChoose, Candidates: files})
		return
	}
	url, err := h.previews.Open(preview.RootFor(files[0], workdirs), files[0])
	if err != nil {
		writePreviewError(w, err)
		return
	}
	writeJSON(w, previewResponse{Status: previewStatusReady, URL: url, Path: files[0]})
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
	}
	http.Error(w, err.Error(), status)
}
