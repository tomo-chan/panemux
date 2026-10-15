package preview

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// realDir resolves t.TempDir through its symbolic links: macOS's temporary
// directory sits behind /var -> /private/var, and a pane reports its working
// directory resolved.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func TestIsPreviewable(t *testing.T) {
	for path, want := range map[string]bool{
		"index.html":           true,
		"index.htm":            true,
		"docs/design.md":       true,
		"NOTES.MARKDOWN":       true,
		"/abs/Page.HTML":       true,
		"report.pdf":           false,
		"design.md.bak":        false,
		"html":                 false,
		".md":                  false,
		"":                     false,
		"script.js":            false,
		"mockups/index.html/x": false,
	} {
		assert.Equal(t, want, IsPreviewable(path), path)
	}
}

// resolveFixture lays out a working directory, a worktree with a
// subdirectory, and a directory outside both, with links between them.
type resolveFixture struct{ cwd, other, outside, sub, sub2 string }

func newResolveFixture(t *testing.T) resolveFixture {
	t.Helper()
	cwd := realDir(t)
	other := realDir(t)
	outside := realDir(t)
	writeFile(t, filepath.Join(cwd, "mockups", "index.html"), "<p>cwd</p>")
	writeFile(t, filepath.Join(cwd, "docs", "design.md"), "# cwd")
	writeFile(t, filepath.Join(other, "docs", "design.md"), "# other")
	writeFile(t, filepath.Join(other, "sub", "page.html"), "<p>sub</p>")
	writeFile(t, filepath.Join(outside, "secret.html"), "<p>secret</p>")
	require.NoError(t, os.MkdirAll(filepath.Join(cwd, "folder.md"), 0o755))
	writeFile(t, filepath.Join(cwd, "plain"), "x")
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.html"), filepath.Join(cwd, "link.html")))
	require.NoError(t, os.Symlink(outside, filepath.Join(cwd, "linkdir")))
	require.NoError(t, os.Symlink(filepath.Join("docs", "design.md"), filepath.Join(cwd, "inside.md")))
	require.NoError(t, os.Symlink(filepath.Join(cwd, "docs", "design.md"), filepath.Join(cwd, "absolute.md")))
	sub := filepath.Join(other, "sub")
	sub2 := filepath.Join(other, "sub2")
	require.NoError(t, os.MkdirAll(sub2, 0o755))

	return resolveFixture{cwd: cwd, other: other, outside: outside, sub: sub, sub2: sub2}
}

func TestResolve(t *testing.T) {
	f := newResolveFixture(t)
	cwd, other, sub, sub2 := f.cwd, f.other, f.sub, f.sub2
	cases := []struct {
		name     string
		path     string
		workdirs []string
		want     []Found
	}{
		{
			name: "relative path in the working directory", path: "./mockups/index.html",
			workdirs: []string{cwd, other},
			want:     []Found{{Root: cwd, File: filepath.Join(cwd, "mockups", "index.html")}},
		},
		{
			name: "relative path in two worktrees", path: "docs/design.md",
			workdirs: []string{cwd, other},
			want: []Found{
				{Root: cwd, File: filepath.Join(cwd, "docs", "design.md")},
				{Root: other, File: filepath.Join(other, "docs", "design.md")},
			},
		},
		{
			name: "an empty working directory is skipped", path: "docs/design.md",
			workdirs: []string{"", cwd}, want: []Found{{Root: cwd, File: filepath.Join(cwd, "docs", "design.md")}},
		},
		{
			name: "a working directory listed twice counts once", path: "docs/design.md",
			workdirs: []string{cwd, cwd + "/"}, want: []Found{{Root: cwd, File: filepath.Join(cwd, "docs", "design.md")}},
		},
		{
			name: "absolute path inside a worktree", path: filepath.Join(other, "docs", "design.md"),
			workdirs: []string{cwd, other},
			want:     []Found{{Root: other, File: filepath.Join(other, "docs", "design.md")}},
		},
		{
			name: "served from the innermost working directory holding it", path: filepath.Join(sub, "page.html"),
			workdirs: []string{other, sub}, want: []Found{{Root: sub, File: filepath.Join(sub, "page.html")}},
		},
		{
			name: "../ that stays inside a worktree", path: "../docs/design.md",
			workdirs: []string{sub, other},
			want:     []Found{{Root: other, File: filepath.Join(other, "docs", "design.md")}},
		},
		{
			name: "two paths naming the same file count once", path: "../sub/page.html",
			workdirs: []string{sub, sub2},
			want:     []Found{{Root: sub, File: filepath.Join(sub, "page.html")}},
		},
		{
			name: "a symbolic link that stays inside", path: "inside.md", workdirs: []string{cwd},
			want: []Found{{Root: cwd, File: filepath.Join(cwd, "inside.md")}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.path, tc.workdirs)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveRefuses(t *testing.T) {
	f := newResolveFixture(t)
	cwd, other, outside := f.cwd, f.other, f.outside
	cases := []struct {
		err      error
		name     string
		path     string
		workdirs []string
	}{
		{name: "absolute path outside every working directory", path: filepath.Join(outside, "secret.html"),
			workdirs: []string{cwd, other}, err: ErrEscapes},
		{name: "../ out of the working directory", path: "../" + filepath.Base(outside) + "/secret.html",
			workdirs: []string{cwd}, err: ErrEscapes},
		{name: "a symbolic link out of the working directory", path: "link.html", workdirs: []string{cwd},
			err: ErrEscapes},
		{name: "a symbolic link to an absolute path, even inside", path: "absolute.md", workdirs: []string{cwd},
			err: ErrEscapes},
		{name: "a directory link out of the working directory", path: "linkdir/secret.html",
			workdirs: []string{cwd}, err: ErrEscapes},
		{name: "absolute path with no working directory", path: filepath.Join(other, "docs", "design.md"),
			err: ErrNoWorkdir},
		{name: "relative path with no working directory", path: "docs/design.md", err: ErrNoWorkdir},
		{name: "relative path found nowhere", path: "missing.md", workdirs: []string{cwd}, err: ErrNotFound},
		{name: "absolute path that does not exist", path: filepath.Join(cwd, "missing.html"),
			workdirs: []string{cwd}, err: ErrNotFound},
		{name: "a file used as a directory", path: "plain/index.html", workdirs: []string{cwd}, err: ErrNotFound},
		{name: "a directory is not a file", path: "folder.md", workdirs: []string{cwd}, err: ErrNotFound},
		{name: "a working directory that does not exist", path: "a.md",
			workdirs: []string{filepath.Join(cwd, "gone")}, err: ErrNotFound},
		{name: "not a previewable type", path: "notes.txt", workdirs: []string{cwd}, err: ErrNotPreviewable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.path, tc.workdirs)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestResolveFoundAndEscapedReportsTheFound(t *testing.T) {
	cwd := realDir(t)
	worktree := realDir(t)
	writeFile(t, filepath.Join(worktree, "notes.md"), "# notes")
	require.NoError(t, os.Symlink(filepath.Join(worktree, "notes.md"), filepath.Join(cwd, "notes.md")))

	got, err := Resolve("notes.md", []string{cwd, worktree})
	require.NoError(t, err)
	assert.Equal(t, []Found{{Root: worktree, File: filepath.Join(worktree, "notes.md")}}, got,
		"a link from one working directory into another is refused; the worktree's own file is not")
}

func TestResolveNotFoundNamesWhereItLooked(t *testing.T) {
	cwd := realDir(t)
	_, err := Resolve("missing.md", []string{cwd})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "missing.md")
	assert.Contains(t, err.Error(), cwd)
}

func TestResolveEscapeNamesTheAllowedDirectories(t *testing.T) {
	cwd := realDir(t)
	worktree := realDir(t)
	_, err := Resolve("/tmp/sample-project/index.html", []string{cwd, worktree})
	require.ErrorIs(t, err, ErrEscapes)
	assert.Contains(t, err.Error(), "/tmp/sample-project/index.html")
	assert.Contains(t, err.Error(), cwd)
	assert.Contains(t, err.Error(), worktree)
}

func TestRootOf(t *testing.T) {
	workdirs := []string{"/workspace/user/project", "/workspace/user/project/sub"}
	assert.Equal(t, "/workspace/user/project/sub", rootOf("/workspace/user/project/sub/a/index.html", workdirs))
	assert.Equal(t, "/workspace/user/project", rootOf("/workspace/user/project/docs/design.md", workdirs))
	assert.Empty(t, rootOf("/tmp/sample-project/docs/design.md", workdirs), "outside every working directory")
	assert.Empty(t, rootOf("/workspace/user/project-other.md", workdirs),
		"a sibling sharing the prefix is not inside the working directory")
	assert.Empty(t, rootOf("/workspace/user/project", workdirs), "a working directory is not inside itself")
}

func TestRegistryOpen(t *testing.T) {
	root := realDir(t)
	file := writeFile(t, filepath.Join(root, "my docs", "design #1.md"), "# hello")

	reg := NewRegistry()
	url, err := reg.Open(root, file)
	require.NoError(t, err)
	assert.Regexp(t, `^/preview/[0-9a-f]{32}/my%20docs/design%20%231\.md$`, url)

	again, err := reg.Open(root, file)
	require.NoError(t, err)
	assert.Equal(t, url, again, "the same file keeps its address")

	otherRoot := realDir(t)
	otherFile := writeFile(t, filepath.Join(otherRoot, "my docs", "design #1.md"), "# other")
	other, err := reg.Open(otherRoot, otherFile)
	require.NoError(t, err)
	assert.NotEqual(t, url, other)
}

func TestRegistryOpenRefuses(t *testing.T) {
	root := realDir(t)
	outside := realDir(t)
	writeFile(t, filepath.Join(outside, "secret.html"), "<p>secret</p>")
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.html"), filepath.Join(root, "link.html")))
	writeFile(t, filepath.Join(root, "real.html"), "<p>real</p>")
	require.NoError(t, os.Symlink("real.html", filepath.Join(root, "inside.html")))
	big := writeFile(t, filepath.Join(root, "big.md"), strings.Repeat("a", int(MaxDocumentBytes)+1))
	exact := writeFile(t, filepath.Join(root, "exact.md"), strings.Repeat("a", int(MaxDocumentBytes)))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dir.html"), 0o755))

	reg := NewRegistry()
	_, err := reg.Open(root, filepath.Join(root, "link.html"))
	require.ErrorIs(t, err, ErrEscapes, "a symbolic link out of the root")

	_, err = reg.Open(root, filepath.Join(outside, "secret.html"))
	require.ErrorIs(t, err, ErrEscapes, "a file outside the root")

	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linkdir")))
	_, err = reg.Open(root, filepath.Join(root, "linkdir", "secret.html"))
	require.ErrorIs(t, err, ErrEscapes, "a directory link out of the root")

	_, err = reg.Open(root, filepath.Join(root, "inside.html"))
	require.NoError(t, err, "a symbolic link that stays inside the root")

	_, err = reg.Open(root, big)
	require.ErrorIs(t, err, ErrTooLarge)

	_, err = reg.Open(root, exact)
	require.NoError(t, err, "a file of exactly the limit")

	_, err = reg.Open(root, filepath.Join(root, "dir.html"))
	require.ErrorIs(t, err, ErrNotFound)

	_, err = reg.Open(root, filepath.Join(root, "missing.html"))
	require.ErrorIs(t, err, ErrNotFound)

	_, err = reg.Open(root, writeFile(t, filepath.Join(root, "notes.txt"), "x"))
	require.ErrorIs(t, err, ErrNotPreviewable)

	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "index.html"), "x")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, err = reg.Open(root, filepath.Join(locked, "index.html"))
	require.ErrorIs(t, err, ErrNotFound, "an unreadable directory is not an escape")
	assert.Contains(t, err.Error(), "permission denied")

	missingRoot := filepath.Join(root, "missing")
	_, err = reg.Open(missingRoot, filepath.Join(missingRoot, "index.html"))
	require.ErrorIs(t, err, ErrNotFound, "a root that does not exist")
}

func TestServeAfterTheRootIsGone(t *testing.T) {
	parent := realDir(t)
	root := filepath.Join(parent, "site")
	file := writeFile(t, filepath.Join(root, "index.html"), "<p>x</p>")

	reg := NewRegistry()
	url, err := reg.Open(root, file)
	require.NoError(t, err)
	require.NoError(t, os.Rename(root, filepath.Join(parent, "moved")))

	assert.Equal(t, http.StatusNotFound, serve(t, reg, url).code)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk gone") }

// serveMarkdown reads through a bounded reader, so a file that fails or
// grows after it was checked is answered rather than read without end.
func TestServeMarkdownReading(t *testing.T) {
	rr := httptest.NewRecorder()
	serveMarkdown(rr, "notes.md", failingReader{})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.NotContains(t, rr.Header().Get("Content-Type"), "text/html")

	rr = httptest.NewRecorder()
	serveMarkdown(rr, "notes.md", strings.NewReader(strings.Repeat("a", int(MaxDocumentBytes)+1)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)

	rr = httptest.NewRecorder()
	serveMarkdown(rr, "notes.md", strings.NewReader(strings.Repeat("a", int(MaxDocumentBytes))))
	assert.Equal(t, http.StatusOK, rr.Code, "a file of exactly the limit")

	rr = httptest.NewRecorder()
	serveMarkdown(rr, "notes.md", strings.NewReader("# ok"))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<h1>ok</h1>")
}

type served struct {
	header http.Header
	body   string
	code   int
}

func serve(t *testing.T, reg *Registry, url string) served {
	t.Helper()
	token, rest, ok := strings.Cut(strings.TrimPrefix(url, RoutePrefix+"/"), "/")
	require.True(t, ok, url)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	// The middleware in front of every route sets this; preview pages must
	// drop it so the dashboard can frame them.
	rr.Header().Set("X-Frame-Options", "DENY")
	reg.Serve(rr, req, token, rest)
	return served{header: rr.Header(), body: rr.Body.String(), code: rr.Code}
}

func sandboxTokens(t *testing.T, csp string) []string {
	t.Helper()
	for _, directive := range strings.Split(csp, ";") {
		fields := strings.Fields(directive)
		if len(fields) > 0 && fields[0] == "sandbox" {
			return fields[1:]
		}
	}
	t.Fatalf("no sandbox directive in %q", csp)
	return nil
}

func TestServeHTMLIsIsolated(t *testing.T) {
	root := realDir(t)
	file := writeFile(t, filepath.Join(root, "mockups", "index.html"),
		`<link rel="stylesheet" href="style.css"><script>fetch('/api/layout')</script><p>mock</p>`)
	writeFile(t, filepath.Join(root, "mockups", "style.css"), "p { color: red }")

	reg := NewRegistry()
	url, err := reg.Open(root, file)
	require.NoError(t, err)

	page := serve(t, reg, url)
	require.Equal(t, http.StatusOK, page.code)
	assert.Contains(t, page.body, "<p>mock</p>", "HTML is served as written")
	assert.Equal(t, "text/html; charset=utf-8", page.header.Get("Content-Type"))
	csp := page.header.Get("Content-Security-Policy")
	assert.Equal(t, []string{"allow-scripts"}, sandboxTokens(t, csp),
		"scripts run in an opaque origin: never allow-same-origin")
	assert.Contains(t, csp, "frame-ancestors 'self'")
	assert.Empty(t, page.header.Get("X-Frame-Options"))
	assert.Equal(t, "no-store", page.header.Get("Cache-Control"))

	css := serve(t, reg, strings.TrimSuffix(url, "index.html")+"style.css")
	require.Equal(t, http.StatusOK, css.code)
	assert.Equal(t, "p { color: red }", css.body)
	assert.Empty(t, sandboxTokens(t, css.header.Get("Content-Security-Policy")),
		"anything else is sandboxed without scripts")
}

func TestServeMarkdownRendersWithoutScripts(t *testing.T) {
	root := realDir(t)
	file := writeFile(t, filepath.Join(root, "docs", "design.md"), strings.Join([]string{
		"# Design & notes",
		"",
		"<script>alert(1)</script>",
		"",
		"<img src=x onerror=alert(2)>",
		"",
		"[click](javascript:alert(3))",
		"",
		"| a | b |",
		"|---|---|",
		"| 1 | 2 |",
		"",
		"![shot](images/shot.png)",
	}, "\n"))

	reg := NewRegistry()
	url, err := reg.Open(root, file)
	require.NoError(t, err)

	page := serve(t, reg, url)
	require.Equal(t, http.StatusOK, page.code)
	assert.Equal(t, "text/html; charset=utf-8", page.header.Get("Content-Type"))
	assert.Contains(t, page.body, "<h1>Design &amp; notes</h1>")
	assert.Contains(t, page.body, "<table>", "GitHub-flavored tables render")
	assert.Contains(t, page.body, `<img src="images/shot.png" alt="shot">`)
	assert.Contains(t, page.body, "<title>design.md</title>")
	assert.NotContains(t, page.body, "<script>")
	assert.NotContains(t, page.body, "onerror")
	assert.NotContains(t, page.body, "javascript:")

	csp := page.header.Get("Content-Security-Policy")
	assert.Empty(t, sandboxTokens(t, csp), "a Markdown page runs no script at all")
	assert.Contains(t, csp, "default-src 'none'")
	assert.Contains(t, csp, "frame-ancestors 'self'")
	assert.Empty(t, page.header.Get("X-Frame-Options"))
	assert.Equal(t, "no-store", page.header.Get("Cache-Control"))
}

func TestServeRefuses(t *testing.T) {
	root := realDir(t)
	outside := realDir(t)
	file := writeFile(t, filepath.Join(root, "site", "index.html"), "<p>ok</p>")
	writeFile(t, filepath.Join(root, "secret.css"), "secret")
	writeFile(t, filepath.Join(outside, "secret.css"), "secret")
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.css"), filepath.Join(root, "site", "escape.css")))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "site", "assets"), 0o755))

	reg := NewRegistry()
	url, err := reg.Open(filepath.Join(root, "site"), file)
	require.NoError(t, err)
	base := strings.TrimSuffix(url, "index.html")

	for name, path := range map[string]string{
		"parent directory":      base + "../secret.css",
		"encoded parent":        base + "..%2fsecret.css",
		"symbolic link escape":  base + "escape.css",
		"directory":             base + "assets",
		"directory with slash":  base + "assets/",
		"missing file":          base + "missing.css",
		"unknown token":         RoutePrefix + "/0123456789abcdef0123456789abcdef/index.html",
		"empty path":            base,
		"absolute inner path":   base + "/etc/passwd",
		"backslash traversal":   base + `..\secret.css`,
		"dot segment":           base + "./index.html",
		"empty segment":         base + "assets//x.css",
		"traversal past prefix": RoutePrefix + "/../secret.css",
	} {
		t.Run(name, func(t *testing.T) {
			token, rest, _ := strings.Cut(strings.TrimPrefix(path, RoutePrefix+"/"), "/")
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()
			reg.Serve(rr, req, token, rest)
			assert.Equal(t, http.StatusNotFound, rr.Code)
			assert.NotContains(t, rr.Body.String(), "secret")
		})
	}
}

func TestServeRechecksTheSizeLimit(t *testing.T) {
	root := realDir(t)
	html := writeFile(t, filepath.Join(root, "index.html"), "<p>small</p>")
	md := writeFile(t, filepath.Join(root, "notes.md"), "small")

	reg := NewRegistry()
	htmlURL, err := reg.Open(root, html)
	require.NoError(t, err)
	mdURL, err := reg.Open(root, md)
	require.NoError(t, err)

	big := strings.Repeat("a", int(MaxDocumentBytes)+1)
	writeFile(t, html, big)
	writeFile(t, md, big)

	assert.Equal(t, http.StatusRequestEntityTooLarge, serve(t, reg, htmlURL).code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, serve(t, reg, mdURL).code)

	exact := strings.Repeat("a", int(MaxDocumentBytes))
	writeFile(t, html, exact)
	assert.Equal(t, http.StatusOK, serve(t, reg, htmlURL).code, "a file of exactly the limit")

	// A sibling resource is not a document: the limit is the previewed
	// page's, not every file it loads.
	writeFile(t, filepath.Join(root, "video.bin"), big)
	assert.Equal(t, http.StatusOK, serve(t, reg, strings.TrimSuffix(htmlURL, "index.html")+"video.bin").code)
}

func TestServeKeepsServingAfterTheFileReturns(t *testing.T) {
	root := realDir(t)
	file := writeFile(t, filepath.Join(root, "index.html"), "<p>v1</p>")

	reg := NewRegistry()
	url, err := reg.Open(root, file)
	require.NoError(t, err)

	require.NoError(t, os.Remove(file))
	assert.Equal(t, http.StatusNotFound, serve(t, reg, url).code)

	writeFile(t, file, "<p>v2</p>")
	page := serve(t, reg, url)
	assert.Equal(t, http.StatusOK, page.code)
	assert.Contains(t, page.body, "v2", "reload shows the file as it is now")
}

func TestErrorsAreDistinct(t *testing.T) {
	all := []error{ErrNotPreviewable, ErrNoWorkdir, ErrNotFound, ErrEscapes, ErrTooLarge}
	for i, a := range all {
		for j, b := range all {
			if i != j {
				assert.False(t, errors.Is(a, b), "%v is %v", a, b)
			}
		}
	}
}
