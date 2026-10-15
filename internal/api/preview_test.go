package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/preview"
	"panemux/internal/session"
)

func previewDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func writePreviewFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func postPreview(t *testing.T, h *Handler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/preview", strings.NewReader(body))
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, req)
	return rec
}

func decodePreview(t *testing.T, rec *httptest.ResponseRecorder) previewResponse {
	t.Helper()
	var resp previewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return resp
}

func newPreviewHandler(t *testing.T, sessions ...session.Session) *Handler {
	t.Helper()
	mgr := session.NewManager()
	for _, s := range sessions {
		mgr.Add(s)
	}
	return NewHandler(defaultTestConfig(), mgr, nil, nil)
}

func cwdSession(id, cwd string, active ...string) *mockCWDSession {
	return &mockCWDSession{mockSession: *newMockSession(id), cwd: cwd, activeWorkdirs: active}
}

func TestPostSessionPreview_OpensRelativePathFromTheWorkingDirectory(t *testing.T) {
	cwd := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "mockups", "index.html"), "<p>mock</p>")
	writePreviewFile(t, filepath.Join(cwd, "mockups", "style.css"), "p{}")
	h := newPreviewHandler(t, cwdSession("pane-1", cwd))

	rec := postPreview(t, h, "pane-1", `{"path":"./mockups/index.html"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodePreview(t, rec)
	assert.Equal(t, previewStatusReady, resp.Status)
	assert.Equal(t, filepath.Join(cwd, "mockups", "index.html"), resp.Path)
	assert.Empty(t, resp.Host, "the panemux host")
	assert.Empty(t, resp.Candidates)
	assert.Regexp(t, `^/preview/[0-9a-f]{32}/mockups/index\.html$`, resp.URL,
		"served from the working directory, so ../ siblings inside it load")

	page := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(page, httptest.NewRequest(http.MethodGet, resp.URL, nil))
	assert.Equal(t, http.StatusOK, page.Code)
	assert.Equal(t, "<p>mock</p>", page.Body.String())

	css := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(css,
		httptest.NewRequest(http.MethodGet, strings.TrimSuffix(resp.URL, "index.html")+"style.css", nil))
	assert.Equal(t, http.StatusOK, css.Code)
}

func TestPostSessionPreview_AsksWhichWorktreeWhenTwoHoldThePath(t *testing.T) {
	cwd := previewDir(t)
	worktree := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "docs", "design.md"), "# main")
	writePreviewFile(t, filepath.Join(worktree, "docs", "design.md"), "# feature")
	h := newPreviewHandler(t, cwdSession("pane-1", cwd, worktree))

	rec := postPreview(t, h, "pane-1", `{"path":"docs/design.md"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodePreview(t, rec)
	assert.Equal(t, previewStatusChoose, resp.Status)
	assert.Equal(t, []string{filepath.Join(cwd, "docs", "design.md"), filepath.Join(worktree, "docs", "design.md")},
		resp.Candidates)
	assert.Empty(t, resp.URL)

	// The operator's choice comes back as an absolute path, which is served
	// from the worktree holding it.
	chosen := filepath.Join(worktree, "docs", "design.md")
	rec = postPreview(t, h, "pane-1", `{"path":`+jsonString(t, chosen)+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp = decodePreview(t, rec)
	assert.Equal(t, previewStatusReady, resp.Status)
	assert.Equal(t, chosen, resp.Path)
	assert.Regexp(t, `^/preview/[0-9a-f]{32}/docs/design\.md$`, resp.URL)
}

func TestPostSessionPreview_ActiveWorkdirAloneResolves(t *testing.T) {
	worktree := previewDir(t)
	writePreviewFile(t, filepath.Join(worktree, "notes.md"), "# notes")
	sess := cwdSession("pane-1", "", worktree)
	sess.cwdErr = errors.New("no cwd")
	h := newPreviewHandler(t, sess)

	rec := postPreview(t, h, "pane-1", `{"path":"notes.md"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, filepath.Join(worktree, "notes.md"), decodePreview(t, rec).Path)
}

func TestPostSessionPreview_Refusals(t *testing.T) {
	cwd := previewDir(t)
	outside := previewDir(t)
	writePreviewFile(t, filepath.Join(outside, "secret.html"), "secret")
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.html"), filepath.Join(cwd, "link.html")))
	writePreviewFile(t, filepath.Join(cwd, "big.md"), strings.Repeat("a", int(preview.MaxDocumentBytes)+1))

	noCWD := cwdSession("no-cwd", "")
	noCWD.cwdErr = errors.New("no cwd")
	noCWD.activeErr = errors.New("no agent")
	remote := cwdSession("remote", cwd)
	remote.typ = session.TypeSSHTmux
	plain := newMockSession("plain")

	h := newPreviewHandler(t, cwdSession("pane-1", cwd), noCWD, remote, plain)

	cases := []struct {
		name, id, body, contains string
		code                     int
	}{
		{"unknown session", "missing", `{"path":"a.md"}`, "session not found", http.StatusNotFound},
		{"malformed body", "pane-1", `{`, "invalid request body", http.StatusBadRequest},
		{"not previewable", "pane-1", `{"path":"notes.txt"}`, ".html", http.StatusBadRequest},
		{"not found", "pane-1", `{"path":"missing.md"}`, "missing.md", http.StatusNotFound},
		{"symbolic link out of the working directory", "pane-1", `{"path":"link.html"}`, "outside",
			http.StatusForbidden},
		{"too large", "pane-1", `{"path":"big.md"}`, "MiB", http.StatusRequestEntityTooLarge},
		{"no working directory", "no-cwd", `{"path":"a.md"}`, "working directory", http.StatusUnprocessableEntity},
		{"session with no working directory at all", "plain", `{"path":"a.md"}`, "working directory",
			http.StatusUnprocessableEntity},
		{"remote pane", "remote", `{"path":"a.md"}`, "panemux host", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postPreview(t, h, tc.id, tc.body)
			assert.Equal(t, tc.code, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.contains)
		})
	}
}

func TestPostSessionPreview_LocalTmuxPaneIsLocal(t *testing.T) {
	cwd := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "index.html"), "<p>x</p>")
	sess := cwdSession("tmux-1", cwd)
	sess.typ = session.TypeTmux
	h := newPreviewHandler(t, sess)

	rec := postPreview(t, h, "tmux-1", `{"path":"index.html"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestPreviewOutlivesTheSession(t *testing.T) {
	cwd := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "index.html"), "<p>kept</p>")
	h := newPreviewHandler(t, cwdSession("pane-1", cwd))

	rec := postPreview(t, h, "pane-1", `{"path":"index.html"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	url := decodePreview(t, rec).URL
	require.NoError(t, h.manager.Remove("pane-1"))

	page := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(page, httptest.NewRequest(http.MethodGet, url, nil))
	assert.Equal(t, http.StatusOK, page.Code, "a tab opened on the preview keeps working")
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}
