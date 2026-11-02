package preview

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMermaidMarkdownPolicy(t *testing.T) {
	runtime := NewMermaidRuntime([]byte("/* trusted product code */"))
	for _, tc := range []struct {
		name, source string
		scripts      bool
	}{
		{"fenced", "```mermaid\nflowchart LR\nA-->B\n```", true},
		{"tilde and CRLF", "~~~mermaid\r\nsequenceDiagram\r\nA->>B: hello\r\n~~~", true},
		{"ordinary", "```go\nmermaid\n```", false},
		{"nested literal", "````markdown\n```mermaid\nA-->B\n```\n````", false},
		{"raw html", "<pre><code class=language-mermaid>A-->B</code></pre>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			serveMarkdown(rr, "diagram.md", strings.NewReader(tc.source), runtime)
			require.Equal(t, 200, rr.Code)
			policy := rr.Header().Get("Content-Security-Policy")
			if !tc.scripts {
				assert.Equal(t, markdownPolicy, policy)
				assert.NotContains(t, rr.Body.String(), "<script")
				return
			}
			assert.Equal(t, []string{"allow-scripts"}, sandboxTokens(t, policy))
			assert.Contains(t, rr.Body.String(), runtime.URL())
			nonce := strings.Split(strings.Split(policy, "'nonce-")[1], "'")[0]
			assert.Contains(t, rr.Body.String(), `nonce="`+nonce+`"`)
			assert.Contains(t, policy, "connect-src 'none'")
			assert.NotContains(t, policy, "unsafe-eval")
			again := httptest.NewRecorder()
			serveMarkdown(again, "diagram.md", strings.NewReader(tc.source), runtime)
			assert.NotEqual(t, policy, again.Header().Get("Content-Security-Policy"))
		})
	}
}

func TestMermaidSourceIsData(t *testing.T) {
	rr := httptest.NewRecorder()
	source := "```mermaid\n</script><img src=x onerror=alert(1)>\n```"
	serveMarkdown(rr, "diagram.md", strings.NewReader(source), NewMermaidRuntime([]byte("trusted")))
	assert.Contains(t, rr.Body.String(), "&lt;/script&gt;")
	assert.NotContains(t, rr.Body.String(), "<img")
	// A missing runtime degrades to readable code, with the original policy.
	rr = httptest.NewRecorder()
	serveMarkdown(rr, "diagram.md", strings.NewReader("```mermaid\nA-->B\n```"), nil)
	assert.Contains(t, rr.Body.String(), "A--&gt;B")
	assert.Equal(t, markdownPolicy, rr.Header().Get("Content-Security-Policy"))
}

func TestMermaidAssets(t *testing.T) {
	runtime := NewMermaidRuntime([]byte("trusted"))
	paths := []string{runtime.URL(), "/preview-assets/unknown.js", "/preview-assets/../secret", "/preview-assets/"}
	for _, path := range paths {
		rr := httptest.NewRecorder()
		runtime.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if path == runtime.URL() {
			assert.Equal(t, 200, rr.Code)
			assert.Equal(t, "trusted", rr.Body.String())
			assert.Equal(t, "text/javascript; charset=utf-8", rr.Header().Get("Content-Type"))
			assert.Contains(t, rr.Header().Get("Cache-Control"), "immutable")
		} else {
			assert.Equal(t, 404, rr.Code)
		}
	}
	assert.NotEqual(t, runtime.URL(), NewMermaidRuntime([]byte("changed")).URL())
}

func TestMermaidRemoteMarkdown(t *testing.T) {
	f := newResolveFixture(t)
	file := writeFile(t, filepath.Join(f.cwd, "docs", "diagram.md"), "```mermaid\nflowchart LR\nA-->B\n```")
	remote := newFakeRemote(t, nil)
	registry := NewRegistry()
	runtime := NewMermaidRuntime([]byte("trusted"))
	registry.SetMermaidRuntime(runtime)
	url, err := registry.OpenRemote(remote.Remote, f.cwd, file)
	require.NoError(t, err)
	response := serveRemote(t, registry, url)
	require.Equal(t, 200, response.Code)
	assert.Contains(t, response.Body.String(), runtime.URL())
	assert.Contains(t, response.Body.String(), "A--&gt;B")
	assert.Equal(t, []string{"allow-scripts"}, sandboxTokens(t, response.Header().Get("Content-Security-Policy")))
}

func TestMermaidRuntimeCanChangeWhileServing(t *testing.T) {
	root := realDir(t)
	file := writeFile(t, filepath.Join(root, "diagram.md"), "```mermaid\nflowchart LR\nA-->B\n```")
	registry := NewRegistry()
	url, err := registry.Open(root, file)
	require.NoError(t, err)
	token, rest, ok := strings.Cut(strings.TrimPrefix(url, RoutePrefix+"/"), "/")
	require.True(t, ok)
	runtime := NewMermaidRuntime([]byte("trusted"))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			registry.SetMermaidRuntime(runtime)
			registry.SetMermaidRuntime(nil)
		}
	}()
	for range 100 {
		rr := httptest.NewRecorder()
		registry.Serve(rr, httptest.NewRequest("GET", url, nil), token, rest)
		assert.Equal(t, 200, rr.Code)
		if rr.Header().Get("Content-Security-Policy") != markdownPolicy {
			assert.Contains(t, rr.Body.String(), runtime.URL())
		}
	}
	wg.Wait()
}
