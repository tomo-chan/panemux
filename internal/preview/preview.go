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
	"syscall"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// RoutePrefix is where previewed files are served.
const RoutePrefix = "/preview"

// MaxDocumentBytes bounds the HTML or Markdown file a preview shows. The
// files a page loads beside it are not documents and are not bounded.
const MaxDocumentBytes int64 = 10 << 20

// The reasons a path cannot be previewed. They are shown to the operator.
var (
	ErrNotPreviewable = errors.New("only .html, .htm, .md and .markdown files can be previewed")
	ErrNoWorkdir      = errors.New("the pane's working directory is unknown, so no file can be previewed")
	ErrNotFound       = errors.New("file not found")
	ErrEscapes        = errors.New("the file is outside the pane's working directory and its agent's worktrees")
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

// Found is a file a clicked path names, with the working directory it is
// served from.
type Found struct {
	Root string
	File string
}

// Resolve finds the files a clicked path may name. Only files inside the
// pane's working directories can be previewed: an absolute path must lie
// inside one of them, and a relative one is looked for from each, so a path
// that exists in two worktrees returns both for the operator to choose from.
// Each file is served from the innermost working directory holding it, and
// is found through that directory's os.Root, so neither "..", an absolute
// path nor a symbolic link reaches a file outside it.
func Resolve(name string, workdirs []string) ([]Found, error) {
	if !IsPreviewable(name) {
		return nil, ErrNotPreviewable
	}
	dirs := cleanDirs(workdirs)
	if len(dirs) == 0 {
		return nil, ErrNoWorkdir
	}
	var paths []string
	if filepath.IsAbs(name) {
		paths = []string{filepath.Clean(name)}
	} else {
		for _, dir := range dirs {
			paths = append(paths, filepath.Join(dir, name))
		}
	}

	var found []Found
	escaped := false
	seen := map[string]bool{}
	for _, file := range paths {
		root, rel := locate(file, dirs)
		if root == "" {
			escaped = true
			continue
		}
		_, err := statIn(root, rel, file)
		switch {
		case errors.Is(err, ErrEscapes):
			escaped = true
		case err == nil && !seen[file]:
			seen[file] = true
			found = append(found, Found{Root: root, File: file})
		}
	}
	if len(found) > 0 {
		return found, nil
	}
	if escaped {
		return nil, fmt.Errorf("%w: %s (allowed: %s)", ErrEscapes, name, strings.Join(dirs, ", "))
	}
	return nil, fmt.Errorf("%w: %s (looked in %s)", ErrNotFound, name, strings.Join(dirs, ", "))
}

// locate finds the innermost of dirs holding file, and file's path relative
// to it, or "" when none does. A path is compared as written first, then by
// the identity of the directories above it: the working directories are
// reported resolved, while a shell prints the path it was given, which may
// lead in through a symbolic link (macOS's /tmp is /private/tmp) or, on a
// case-insensitive filesystem, in another letter case.
func locate(file string, dirs []string) (root, rel string) {
	if root = rootOf(file, dirs); root != "" {
		rel, _ = filepath.Rel(root, file)
		return root, rel
	}
	return locateByIdentity(file, dirs)
}

// locateByIdentity resolves the deepest directory above file that exists,
// through its symbolic links, and walks up that resolved path — which holds
// no link left to follow — to the first directory that is one of dirs. Each
// is looked up through an os.Root for the filesystem's root, by a path
// relative to it, and compared by identity, which also matches another
// letter case on a case-insensitive filesystem. A link in the clicked path's
// prefix to a working directory, to one of its parents or to a directory
// below it is therefore inside. Only directories are matched this way; the
// file is then reached only through its root's os.Root, by the rest of the
// clicked path, so a link below that point cannot leave the root.
func locateByIdentity(file string, dirs []string) (string, string) {
	var known []string
	var ids []fs.FileInfo
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err == nil {
			known = append(known, dir)
			ids = append(ids, info)
		}
	}
	host, err := os.OpenRoot(string(filepath.Separator))
	if err != nil { //coverage:exempt the filesystem's root can always be opened
		return "", ""
	}
	defer func() { _ = host.Close() }()

	// The filesystem's root always resolves, so this stops there at most.
	dir := filepath.Dir(file)
	resolved, err := filepath.EvalSymlinks(dir)
	for err != nil {
		dir = filepath.Dir(dir)
		resolved, err = filepath.EvalSymlinks(dir)
	}
	rest, _ := filepath.Rel(dir, file)
	for parent := resolved; ; parent = filepath.Dir(parent) {
		fromRoot, _ := filepath.Rel(string(filepath.Separator), parent)
		if info, err := host.Stat(fromRoot); err == nil {
			for i, id := range ids {
				if os.SameFile(info, id) {
					below, _ := filepath.Rel(parent, resolved)
					return known[i], filepath.Join(below, rest)
				}
			}
		}
		if parent == filepath.Dir(parent) {
			return "", ""
		}
	}
}

// rootOf is the innermost of dirs that holds file, or "" when none does.
func rootOf(file string, dirs []string) string {
	root := ""
	for _, dir := range dirs {
		// Two different directories that both hold file and have the same
		// length cannot exist once cleanDirs has removed duplicates.
		//mutation:exempt[CONDITIONALS_BOUNDARY] EQUIVALENT: ancestors of one file differ in length
		if within(dir, file) && len(dir) > len(root) {
			root = dir
		}
	}
	return root
}

// statIn describes the regular file at rel, a path relative to root, looking
// it up through root's os.Root: the file is reached only from the root, and
// a "..", or a symbolic link, that leaves the root is not followed. A path
// that leaves the root reports ErrEscapes; one naming nothing, or anything
// but a regular file, reports ErrNotFound. file is the path as clicked, for
// the reason shown.
func statIn(root, rel, file string) (fs.FileInfo, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, root)
	}
	defer func() { _ = r.Close() }()
	info, err := r.Stat(rel)
	switch {
	case err == nil && info.Mode().IsRegular():
		return info, nil
	case err == nil, errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return nil, fmt.Errorf("%w: %s", ErrNotFound, file)
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf("%w: %s: permission denied", ErrNotFound, file)
	}
	// os.Root refuses a symbolic link, or a "..", that leads outside it.
	return nil, fmt.Errorf("%w: %s", ErrEscapes, file)
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
// limit, inside root (see locate) and reached from it without leaving it
// (see statIn).
func (r *Registry) Open(root, file string) (string, error) {
	if !IsPreviewable(file) {
		return "", ErrNotPreviewable
	}
	root, file = filepath.Clean(root), filepath.Clean(file)
	_, rel := locate(file, []string{root})
	if rel == "" {
		return "", fmt.Errorf("%w: %s", ErrEscapes, file)
	}
	info, err := statIn(root, rel, file)
	if err != nil {
		return "", err
	}
	if info.Size() > MaxDocumentBytes {
		return "", ErrTooLarge
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
		serveHTML(w, req, info, f)
	case kindMarkdown:
		serveMarkdown(w, info.Name(), f)
	default:
		http.ServeContent(w, req, info.Name(), info.ModTime(), f)
	}
}

// serveHTML serves an HTML document at the size it was checked at. A file
// that grows after that check is cut there: http.ServeContent seeks to the
// end of what it is given to learn the length, and the file's own end may
// since lie past the limit.
func serveHTML(w http.ResponseWriter, req *http.Request, info fs.FileInfo, f io.ReaderAt) {
	w.Header().Set("Content-Security-Policy", htmlPolicy)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, req, info.Name(), info.ModTime(), io.NewSectionReader(f, 0, info.Size()))
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
// left unchanged on purpose, and dangerousLinks for the schemes they miss);
// the page's policy forbids scripts besides.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(dangerousLinks{}, 1000))),
)

// dangerousLinks empties a link or image destination that goldmark's own
// check lets through only because it looks before character references are
// decoded: "jav&#x61;script:" is rendered as "javascript:". Each destination
// is checked again as the browser will read it — references decoded, tabs
// and newlines removed, and leading spaces and control characters trimmed.
type dangerousLinks struct{}

func (dangerousLinks) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	// Each node is visited entering and leaving; emptying twice is harmless.
	_ = ast.Walk(doc, func(n ast.Node, _ bool) (ast.WalkStatus, error) {
		switch node := n.(type) {
		case *ast.Link:
			if isDangerousDestination(node.Destination) {
				node.Destination = nil
			}
		case *ast.Image:
			if isDangerousDestination(node.Destination) {
				node.Destination = nil
			}
		}
		return ast.WalkContinue, nil
	})
}

func isDangerousDestination(destination []byte) bool {
	decoded := strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, html.UnescapeString(string(destination)))
	decoded = strings.TrimLeftFunc(decoded, func(r rune) bool { return r <= ' ' })
	return goldmarkhtml.IsDangerousURL([]byte(decoded))
}

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
