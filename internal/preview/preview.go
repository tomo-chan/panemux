// Package preview serves local HTML and Markdown files clicked in a pane to
// the dashboard's split preview (issue #385), isolated from the dashboard's
// own origin.
//
// A previewed file is registered under a random token and served below
// RoutePrefix/<token>/, together with the files beside it under the same
// root, so a mock's relative stylesheets and images load. Every response is
// sandboxed by Content-Security-Policy without allow-same-origin: the page
// runs in an opaque origin, so the browser sends its requests to /api and
// /ws as cross-site, and internal/requestsecurity refuses them. See
// docs/security/preview.md.
package preview

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// RoutePrefix is where previewed files are served.
const RoutePrefix = "/preview"

// MaxDocumentBytes bounds the HTML or Markdown file a preview shows. The
// files a page loads beside it are not documents and are not bounded.
const MaxDocumentBytes int64 = 10 << 20

// The reasons a path cannot be previewed. They are shown to the operator.
var (
	ErrNotPreviewable = errors.New("only .html, .htm, .md and .markdown files can be previewed")
	ErrNoWorkdir      = errors.New("the pane's working directory is unknown, so a relative path cannot be resolved")
	ErrNotFound       = errors.New("file not found")
	ErrEscapes        = errors.New("the file resolves outside the directory it is served from")
	ErrTooLarge       = fmt.Errorf("the file is larger than the %d MiB preview limit", MaxDocumentBytes>>20)
)

// The Content-Security-Policy of each kind of response. None allows
// same-origin, which is what keeps a page out of the dashboard's origin.
const (
	htmlPolicy     = "sandbox allow-scripts; frame-ancestors 'self'"
	markdownPolicy = "sandbox; default-src 'none'; img-src * data:; style-src 'unsafe-inline'; frame-ancestors 'self'"
	otherPolicy    = "sandbox; frame-ancestors 'self'"
)

type documentKind int

const (
	kindOther documentKind = iota
	kindHTML
	kindMarkdown
)

func kindOf(name string) documentKind {
	base := path.Base(filepath.ToSlash(name))
	ext := strings.ToLower(path.Ext(base))
	if len(base) == len(ext) {
		return kindOther
	}
	switch ext {
	case ".html", ".htm":
		return kindHTML
	case ".md", ".markdown":
		return kindMarkdown
	}
	return kindOther
}

// IsPreviewable reports whether a path names an HTML or Markdown file.
func IsPreviewable(name string) bool {
	return kindOf(name) != kindOther
}

// Resolve finds the files a clicked path may name. An absolute path names
// itself; a relative one is looked for in each working directory, so a path
// that exists in two worktrees returns both for the operator to choose from.
func Resolve(name string, workdirs []string) ([]string, error) {
	if !IsPreviewable(name) {
		return nil, ErrNotPreviewable
	}
	if filepath.IsAbs(name) {
		file := filepath.Clean(name)
		if !isRegularFile(file) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		return []string{file}, nil
	}

	dirs := cleanDirs(workdirs)
	if len(dirs) == 0 {
		return nil, ErrNoWorkdir
	}
	var found []string
	for _, dir := range dirs {
		if file := filepath.Join(dir, name); isRegularFile(file) {
			found = append(found, file)
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%w: %s (looked in %s)", ErrNotFound, name, strings.Join(dirs, ", "))
	}
	return found, nil
}

// RootFor is the directory a file is served from: the innermost working
// directory holding it, or else its own directory.
func RootFor(file string, workdirs []string) string {
	root := ""
	for _, dir := range cleanDirs(workdirs) {
		if within(dir, file) && len(dir) > len(root) {
			root = dir
		}
	}
	if root == "" {
		return filepath.Dir(file)
	}
	return root
}

func cleanDirs(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

func within(dir, file string) bool {
	rel, err := filepath.Rel(dir, file)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}

func isRegularFile(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular()
}

type entry struct {
	root string
	file string // slash-separated, relative to root
}

// Registry maps the tokens of opened previews to their files. Entries are
// kept for the server's lifetime: a preview opened in another tab outlives
// the pane, popup and session it came from.
type Registry struct {
	byToken map[string]entry
	byEntry map[entry]string
	mu      sync.Mutex
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byToken: map[string]entry{}, byEntry: map[entry]string{}}
}

// Open registers file, served from root, and returns the address it is
// served at. The file must be an HTML or Markdown file within the size
// limit, and must resolve inside root after symbolic links.
func (r *Registry) Open(root, file string) (string, error) {
	if !IsPreviewable(file) {
		return "", ErrNotPreviewable
	}
	root, file = filepath.Clean(root), filepath.Clean(file)
	if !within(root, file) {
		return "", ErrEscapes
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, root)
	}
	realFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, file)
	}
	if !within(realRoot, realFile) {
		return "", ErrEscapes
	}
	info, err := os.Stat(realFile)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrNotFound, file)
	}
	if info.Size() > MaxDocumentBytes {
		return "", ErrTooLarge
	}

	rel, err := filepath.Rel(root, file)
	if err != nil { //coverage:exempt within() already proved file is below root
		return "", ErrEscapes
	}
	e := entry{root: root, file: filepath.ToSlash(rel)}
	token, err := r.tokenFor(e)
	if err != nil { //coverage:exempt tokenFor fails only when crypto/rand does
		return "", err
	}
	segments := strings.Split(e.file, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return RoutePrefix + "/" + token + "/" + strings.Join(segments, "/"), nil
}

func (r *Registry) tokenFor(e entry) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token, ok := r.byEntry[e]; ok {
		return token, nil
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil { //coverage:exempt crypto/rand does not fail on supported platforms
		return "", fmt.Errorf("preview token: %w", err)
	}
	token := hex.EncodeToString(buf)
	r.byToken[token] = e
	r.byEntry[e] = token
	return token, nil
}

func (r *Registry) lookup(token string) (entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byToken[token]
	return e, ok
}

// Serve answers a request for rest, a slash-separated path below the root
// that token was registered with. Files are opened through os.Root, so
// neither a ".." nor a symbolic link reaches outside that root.
func (r *Registry) Serve(w http.ResponseWriter, req *http.Request, token, rest string) {
	header := w.Header()
	// Set for every route by the server; a preview has to be framed by the
	// dashboard, which frame-ancestors still limits to the same origin.
	header.Del("X-Frame-Options")
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", otherPolicy)

	f, info, ok := r.open(token, rest)
	if !ok {
		http.NotFound(w, req)
		return
	}
	defer func() { _ = f.Close() }()

	kind := kindOf(rest)
	if kind != kindOther && info.Size() > MaxDocumentBytes {
		http.Error(w, ErrTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	switch kind {
	case kindHTML:
		header.Set("Content-Security-Policy", htmlPolicy)
		header.Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, req, info.Name(), info.ModTime(), f)
	case kindMarkdown:
		serveMarkdown(w, info.Name(), f)
	default:
		http.ServeContent(w, req, info.Name(), info.ModTime(), f)
	}
}

// open opens a regular file below a registered root, or reports false.
func (r *Registry) open(token, rest string) (*os.File, fs.FileInfo, bool) {
	e, ok := r.lookup(token)
	if !ok || rest == "." || !fs.ValidPath(rest) || strings.Contains(rest, `\`) {
		return nil, nil, false
	}
	root, err := os.OpenRoot(e.root)
	if err != nil {
		return nil, nil, false
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(filepath.FromSlash(rest))
	if err != nil {
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, false
	}
	return f, info, true
}

func serveMarkdown(w http.ResponseWriter, name string, f io.Reader) {
	source, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		http.Error(w, "reading the file failed", http.StatusInternalServerError)
		return
	}
	if int64(len(source)) > MaxDocumentBytes {
		http.Error(w, ErrTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	page, err := renderMarkdown(name, source)
	if err != nil { //coverage:exempt renderMarkdown fails only where its own exempt branch does
		http.Error(w, "rendering the file failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Security-Policy", markdownPolicy)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

// markdown renders GitHub-flavored Markdown. Raw HTML in the source is
// omitted and links with a script scheme are dropped (goldmark's defaults,
// left unchanged on purpose); the page's policy forbids scripts besides.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

const markdownStyle = `body{margin:0;background:#fff;color:#1f2328;` +
	`font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}` +
	`main{max-width:860px;margin:0 auto;padding:24px 28px}` +
	`h1,h2{border-bottom:1px solid #d1d9e0;padding-bottom:.3em}` +
	`code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;` +
	`background:#f6f8fa;border-radius:4px}` +
	`code{padding:.15em .35em}pre{padding:12px 14px;overflow:auto}pre code{padding:0}` +
	`table{border-collapse:collapse}th,td{border:1px solid #d1d9e0;padding:4px 10px}` +
	`img{max-width:100%}blockquote{margin:0;padding:0 1em;color:#59636e;border-left:4px solid #d1d9e0}` +
	`a{color:#0969da}`

func renderMarkdown(name string, source []byte) ([]byte, error) {
	var body bytes.Buffer
	if err := markdown.Convert(source, &body); err != nil { //coverage:exempt rendering into a bytes.Buffer does not fail
		return nil, fmt.Errorf("rendering %s: %w", name, err)
	}
	var page bytes.Buffer
	page.WriteString(`<!doctype html><html><head><meta charset="utf-8">`)
	page.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	page.WriteString("<title>" + html.EscapeString(name) + "</title>")
	page.WriteString("<style>" + markdownStyle + "</style></head><body><main>")
	page.Write(body.Bytes())
	page.WriteString("</main></body></html>")
	return page.Bytes(), nil
}
