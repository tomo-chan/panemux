package server

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/api"
	"panemux/internal/board"
	"panemux/internal/commandcenter"
	"panemux/internal/config"
	"panemux/internal/session"
)

// This file is the real-router integration harness for the /api surface:
// gate G3(a) in docs/quality-gateway.md, and the second half of issue #180's
// roadmap item 1. Its companion, route_table_test.go, already pins *which*
// routes exist and which of them are authenticated. What was still missing is
// the other half of the same question — that each of those routes, driven
// through the router server.New() actually builds, reaches its real handler
// and answers the way the frontend expects.
//
// Until now that was covered by a handful of one-off "…RouteWired" tests
// (internal/server) plus ~161 handler tests (internal/api) that go through
// api.Handler.Mount but not through server.New(): they never see the
// middleware stack, the mount precedence between /api and /api/board, or the
// SPA catch-all that sits behind everything. A route could therefore answer
// correctly in internal/api and be shadowed, wrapped, or 404'd in production
// without a test failing. See docs/quality-gateway.md's P1 and gate G3, and
// issue #178.
//
// The table below is exhaustive *by construction*: the test walks the real
// router and fails when a registered /api route has no case here, so adding a
// route forces an integration case for it rather than relying on anyone
// remembering to add one. That is decision D3 (consolidate gates rather than
// adding them) applied to this half of the gate — the same shape as the route
// table's own exhaustiveness check, which is why no new *_routes_test.go file
// is needed for the next feature either.

// apiCase drives one route end to end.
//
// happyPath cases assert the response a working frontend request produces.
// Where a genuine 2xx would require something make check must not depend on —
// a `code` binary on PATH, a reachable remote agmsg host — the case says so
// in notHappyPathReason and asserts the nearest response that still proves
// the request reached the route's real handler rather than chi's own 404.
// Leaving such a route out of the table entirely is not an option: the
// exhaustiveness check would fail. That is deliberate, and it is design
// principle P5 (deliberate implementation pinning is documented) applied to
// coverage gaps rather than to assertions.
type apiCase struct {
	// run performs the request(s) and asserts the outcome.
	run func(t *testing.T, e *apiEnv)
	// notHappyPathReason, when non-empty, records why this case does not
	// drive the route's genuine success path.
	notHappyPathReason string
}

// apiEnv is one hermetic server plus the state a case needs to reach it.
type apiEnv struct {
	srv   *Server
	cfg   *config.Config
	mgr   *session.Manager
	cache *board.BoardCache
	home  string
	// statuses is every status code do() has returned for this case, which
	// is what lets the harness decide whether a case drove a success path
	// rather than taking the author's word for it. See
	// TestServer_APIIntegration.
	statuses []int
}

const integrationToken = "integration-token"

// newAPIEnv builds a server from the same New() the binary calls, with two
// deliberate hermeticity choices:
//
//   - HOME points at a temp directory, so ~/.ssh/config and the command
//     center's history file resolve inside the test rather than reading (or,
//     for POST /api/ssh-config/hosts, writing) the developer's own files.
//     It has to be set before New(), because api.NewHandler resolves
//     sshconfig.DefaultPath() once at construction.
//   - XDG_CACHE_HOME points inside it too. HOME alone is not enough:
//     os.UserCacheDir prefers XDG_CACHE_HOME over $HOME/.cache, and creating
//     a local pane installs the browser shim there (see
//     session.installLocalBrowserShim), so on a machine that exports the
//     variable — a CI image, a desktop session — this suite wrote three real
//     files into a directory the test never chose.
//   - the config carries no file path, which makes config.write() a no-op, so
//     the layout and workspace writes these routes perform stay in memory.
func newAPIEnv(t *testing.T) *apiEnv {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cfg := testConfigWithToken(integrationToken)
	mgr := session.NewManager()
	cache := board.NewBoardCache()
	relay := board.NewRelay(cache, board.RelayConfig{})

	e := &apiEnv{
		srv:   New(cfg, mgr, cache, relay, nil, emptyFS),
		cfg:   cfg,
		mgr:   mgr,
		cache: cache,
		home:  home,
	}
	t.Cleanup(func() {
		for _, s := range mgr.List() {
			_ = mgr.Remove(s.ID())
		}
		// New() starts a port-forward sweeper goroutine that only exits on
		// Close(), and Shutdown is what releases it — the same process-wide
		// state TestServer_ShutdownClosesPortForwards pins. Nothing fails
		// today, but one leaked goroutine per subtest is what makes a package
		// hostile to a goleak-style check later.
		_ = e.srv.Shutdown(context.Background())
	})
	return e
}

// do issues a request against the real router, including its middleware
// stack. Board routes need the bearer token; nothing else does, and that
// asymmetry is itself asserted by route_table_test.go.
func (e *apiEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// httptest.NewRequest defaults Host to example.com and RemoteAddr to a
	// documentation-range address. GET /api/session-token requires both to be
	// loopback — see api.GetBoardSessionToken's DNS-rebinding guard — and
	// every other route is indifferent to them, so the loopback pair a real
	// dashboard request carries is the right default here.
	req.Host = "127.0.0.1:8080"
	req.RemoteAddr = "127.0.0.1:54321"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.HasPrefix(path, api.BoardRoutePrefix+"/") {
		req.Header.Set("Authorization", "Bearer "+integrationToken)
	}

	rr := httptest.NewRecorder()
	e.srv.httpSrv.Handler.ServeHTTP(rr, req)
	e.statuses = append(e.statuses, rr.Code)
	return rr
}

// createPane creates a real local pane through POST /api/sessions, which is
// both the happy path for that route and the precondition for every
// per-session route below. The shell is the hardcoded /bin/sh the rest of
// this repository defaults to (see docs/security.md), and cwd is a temp
// directory rather than the checkout, so git-info answers "not a repo"
// without walking a real worktree.
func (e *apiEnv) createPane(t *testing.T, id string) {
	t.Helper()

	body := fmt.Sprintf(`{"id":%q,"type":"local","shell":"/bin/sh","cwd":%q,"title":"Integration"}`, id, e.home)
	rr := e.do(t, http.MethodPost, "/api/sessions", body)
	require.Equal(t, http.StatusCreated, rr.Code, "creating the pane this case needs: %s", rr.Body.String())
}

// apiCases maps "METHOD /pattern" — chi's own spelling, as walkRoutes
// reports it — to the case that drives it.
var apiCases = map[string]apiCase{
	"GET /api/layout": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodGet, "/api/layout", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"direction":"horizontal"`)
	}},

	"PUT /api/layout": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodPut, "/api/layout",
			`{"direction":"vertical","children":[{"size":100,"pane":{"id":"main","type":"local"}}]}`)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "vertical", e.cfg.ActiveLayout().Direction)
	}},

	"GET /api/workspaces": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodGet, "/api/workspaces", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"active":"default"`)
	}},

	"POST /api/workspaces": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodPost, "/api/workspaces", "")
		assert.Equal(t, http.StatusCreated, rr.Code)
		assert.Len(t, e.cfg.WorkspacesView().Items, 2)
	}},

	"PUT /api/workspaces/active": {run: func(t *testing.T, e *apiEnv) {
		// Switch AWAY from the starting active workspace. Asserting that
		// "default" is still active after PUTting {"id":"default"} restates
		// the fixture: it passes just as well against a handler that ignores
		// the requested id entirely. Every sibling workspace case moves a
		// value for the same reason.
		require.Equal(t, http.StatusCreated, e.do(t, http.MethodPost, "/api/workspaces", "").Code)
		items := e.cfg.WorkspacesView().Items
		require.Len(t, items, 2)

		// Creating a workspace already switches to it, so the target is
		// whichever of the two is not currently active — the assertion has to
		// be that the switch moved, not which way.
		target := items[0].ID
		if target == e.cfg.Workspaces.Active {
			target = items[1].ID
		}
		require.NotEqual(t, e.cfg.Workspaces.Active, target)

		rr := e.do(t, http.MethodPut, "/api/workspaces/active", fmt.Sprintf(`{"id":%q}`, target))
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, target, e.cfg.Workspaces.Active)
	}},

	"PUT /api/workspaces/tab-position": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodPut, "/api/workspaces/tab-position", `{"tab_position":"right"}`)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "right", e.cfg.Workspaces.TabPosition)
		// The workspace named "tab-position" does not exist; reaching the
		// settings route rather than PUT /api/workspaces/{id} is what the
		// dedicated precedence test in server_test.go pins.
		assert.Equal(t, "Default", e.cfg.Workspaces.Items[0].Title)
	}},

	"PUT /api/workspaces/vertical-bar-width": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodPut, "/api/workspaces/vertical-bar-width", `{"vertical_bar_width":320}`)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, 320, e.cfg.Workspaces.VerticalBarWidth)
		assert.Equal(t, "Default", e.cfg.Workspaces.Items[0].Title)
	}},

	"PUT /api/workspaces/{id}": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodPut, "/api/workspaces/default", `{"title":"Renamed"}`)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "Renamed", e.cfg.Workspaces.Items[0].Title)
	}},

	"DELETE /api/workspaces/{id}": {run: func(t *testing.T, e *apiEnv) {
		// The last workspace cannot be deleted, so add one first.
		require.Equal(t, http.StatusCreated, e.do(t, http.MethodPost, "/api/workspaces", "").Code)
		added := e.cfg.WorkspacesView().Items[1].ID

		rr := e.do(t, http.MethodDelete, "/api/workspaces/"+added, "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Len(t, e.cfg.WorkspacesView().Items, 1)
	}},

	"PUT /api/workspaces/{id}/layout": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodPut, "/api/workspaces/default/layout",
			`{"direction":"vertical","children":[{"size":100,"pane":{"id":"main","type":"local"}}]}`)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "vertical", e.cfg.Workspaces.Items[0].Layout.Direction)
	}},

	"GET /api/sessions": {run: func(t *testing.T, e *apiEnv) {
		e.createPane(t, "pane-list")

		rr := e.do(t, http.MethodGet, "/api/sessions", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"id":"pane-list"`)
	}},

	"POST /api/sessions": {run: func(t *testing.T, e *apiEnv) {
		e.createPane(t, "pane-created")

		_, ok := e.mgr.Get("pane-created")
		assert.True(t, ok, "the pane the route reported creating must be registered with the manager")
	}},

	"DELETE /api/sessions/{id}": {run: func(t *testing.T, e *apiEnv) {
		e.createPane(t, "pane-doomed")

		rr := e.do(t, http.MethodDelete, "/api/sessions/pane-doomed", "")
		assert.Equal(t, http.StatusNoContent, rr.Code)
		_, ok := e.mgr.Get("pane-doomed")
		assert.False(t, ok)
	}},

	"POST /api/sessions/{id}/restart": {run: func(t *testing.T, e *apiEnv) {
		// Restart recreates the pane from its *config* entry, so the pane has
		// to be in the active workspace's layout, not merely in the manager.
		e.cfg.Workspaces.Items[0].Layout.Children[0].Pane = &config.PaneConfig{
			ID: "main", Type: "local", Shell: "/bin/sh", Cwd: e.home,
		}
		e.createPane(t, "main")

		rr := e.do(t, http.MethodPost, "/api/sessions/main/restart", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		_, ok := e.mgr.Get("main")
		assert.True(t, ok, "a restarted pane must still be registered")
	}},

	"POST /api/sessions/{id}/open-vscode": {
		notHappyPathReason: "a 2xx needs a `code` binary on PATH and a real editor launch; " +
			"make check must stay hermetic, so this asserts only that the route reaches its own handler",
		run: func(t *testing.T, e *apiEnv) {
			rr := e.do(t, http.MethodPost, "/api/sessions/missing/open-vscode", "")
			assert.Equal(t, http.StatusNotFound, rr.Code)
			// chi's own 404 body is "404 page not found"; this one comes
			// from PostOpenVSCode, which is what proves the dispatch.
			assert.Contains(t, rr.Body.String(), "session not found")
		},
	},

	"POST /api/sessions/{id}/open-url": {run: func(t *testing.T, e *apiEnv) {
		e.createPane(t, "pane-url")

		rr := e.do(t, http.MethodPost, "/api/sessions/pane-url/open-url",
			`{"url":"http://localhost:53682/callback"}`)
		assert.Equal(t, http.StatusOK, rr.Code)
		// A local pane's callback already resolves on this host, so the
		// server correctly declines to bind a forward for it.
		assert.Contains(t, rr.Body.String(), `"forwarded":false`)
		// Asserting the reason, not just the flag: "forwarded":false is also
		// what an unavailable registry reports, and the two mean different
		// things to the operator reading it. (Whether New() attaches the
		// registry at all is a separate question this route cannot see for a
		// local pane — PostOpenURL returns before consulting it — and is
		// covered by TestServer_ShutdownClosesPortForwards.)
		assert.Contains(t, rr.Body.String(), "resolves locally")
	}},

	"GET /api/sessions/{id}/git-info": {run: func(t *testing.T, e *apiEnv) {
		e.createPane(t, "pane-git")

		rr := e.do(t, http.MethodGet, "/api/sessions/pane-git/git-info", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		// The pane's cwd is a temp directory, so the answer is a valid
		// "not a repository" one rather than anything about this checkout.
		assert.Contains(t, rr.Body.String(), `"is_git":false`)
	}},

	"GET /api/display": {run: func(t *testing.T, e *apiEnv) {
		// Seeded after New(), which the handler sees because it holds the
		// same *config.Config pointer — the GET /api/ssh-connections case
		// below does the same. Without a seed there is nothing distinctive
		// in DisplayConfig's two booleans to assert, and `Contains("{")` is
		// true of every JSON object, including the empty one a handler that
		// dropped the whole configuration would return.
		e.cfg.Display = config.DisplayConfig{ShowHeader: true, ShowStatusBar: false}

		rr := e.do(t, http.MethodGet, "/api/display", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"show_header":true`)
		assert.Contains(t, rr.Body.String(), `"show_status_bar":false`)
	}},

	"GET /api/ssh-connections": {run: func(t *testing.T, e *apiEnv) {
		e.cfg.SSHConnections = map[string]config.SSHConnection{
			"demo": {Host: "demo.invalid", User: "demo"},
		}

		rr := e.do(t, http.MethodGet, "/api/ssh-connections", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"names":["demo"]`)
	}},

	"GET /api/ssh-config/hosts": {run: func(t *testing.T, e *apiEnv) {
		writeSSHConfig(t, e.home, "Host demo\n  HostName demo.invalid\n  User demo\n")

		rr := e.do(t, http.MethodGet, "/api/ssh-config/hosts", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"name":"demo"`)
	}},

	"POST /api/ssh-config/hosts": {run: func(t *testing.T, e *apiEnv) {
		writeSSHConfig(t, e.home, "")

		rr := e.do(t, http.MethodPost, "/api/ssh-config/hosts",
			`{"name":"added","hostname":"added.invalid","user":"demo"}`)
		assert.Equal(t, http.StatusCreated, rr.Code)

		// The write landed in the temp HOME, not the developer's own file.
		written, err := os.ReadFile(filepath.Join(e.home, ".ssh", "config"))
		require.NoError(t, err)
		assert.Contains(t, string(written), "Host added")
	}},

	"GET /api/detect-shell": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodGet, "/api/detect-shell", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"shell":"/`)
	}},

	"GET /api/directories": {run: func(t *testing.T, e *apiEnv) {
		require.NoError(t, os.Mkdir(filepath.Join(e.home, "child"), 0o750))

		rr := e.do(t, http.MethodGet, "/api/directories?path="+e.home, "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"name":"child"`)
	}},

	"GET /api/session-token": {run: func(t *testing.T, e *apiEnv) {
		// do() gives every request a loopback RemoteAddr and Host — NOT
		// httptest's own defaults, which are 192.0.2.1:1234 and example.com,
		// neither of them loopback. This route is the reason do() overrides
		// them: api.GetBoardSessionToken's DNS-rebinding guard answers 403
		// otherwise, and that guard is the only thing between this token and
		// any LAN client (docs/security.md).
		rr := e.do(t, http.MethodGet, "/api/session-token", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), integrationToken)
		// This route also reports whether /ws/board-command is actually
		// registered, which only New() knows — it calls
		// SetCommandCenterAvailable(commandRunner != nil). Driving both
		// states is what makes that a wiring assertion rather than a
		// restatement of the zero value: an api-package handler test sets the
		// flag itself, so it cannot see New() dropping the call.
		assert.Contains(t, rr.Body.String(), `"command_center_enabled":false`)

		enabled := &apiEnv{srv: New(
			testConfigWithToken(integrationToken), session.NewManager(), e.cache, nil,
			commandcenter.NewRunner(commandcenter.RunnerConfig{}), emptyFS,
		)}
		rr = enabled.do(t, http.MethodGet, "/api/session-token", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"command_center_enabled":true`)
	}},

	"GET /api/board/status": {run: func(t *testing.T, e *apiEnv) {
		// A bare 200 is the one thing that does not distinguish this handler
		// from chi's dispatch alone, and it matters here more than most:
		// useBoardStatus polls this route and Zod-parses the body, so an
		// empty or reshaped envelope takes the dashboard down.
		e.cache.RecordStatus("pane-1", board.Status{State: "working", Summary: "integration"})

		rr := e.do(t, http.MethodGet, "/api/board/status", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"statuses":{"pane-1":`)
		assert.Contains(t, rr.Body.String(), `"state":"working"`)
	}},

	"GET /api/board/messages": {run: func(t *testing.T, e *apiEnv) {
		rr := e.do(t, http.MethodGet, "/api/board/messages", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"messages":[]`)
	}},

	"POST /api/board/broadcast": {
		notHappyPathReason: "delivery needs a reachable remote agmsg host over SSH; make check stays " +
			"hermetic, so this asserts the relay rejected an unknown pane rather than a delivery",
		run: func(t *testing.T, e *apiEnv) {
			rr := e.do(t, http.MethodPost, "/api/board/broadcast", `{"to":["pane-1"],"body":"hello"}`)
			assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
			// The message comes from board.UnknownPaneError, so the request
			// reached the real relay through the real handler.
			assert.Contains(t, rr.Body.String(), "unknown pane id(s): pane-1")
		},
	},

	"GET /api/board/command/history": {run: func(t *testing.T, e *apiEnv) {
		// HOME is a temp directory, so the history file is legitimately
		// absent — which the route reports as an empty list, not an error.
		rr := e.do(t, http.MethodGet, "/api/board/command/history", "")
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"entries":[]`)
	}},
}

func writeSSHConfig(t *testing.T, home, contents string) {
	t.Helper()
	dir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config"), []byte(contents), 0o600))
}

// TestServer_APIIntegration drives every /api route through the real router
// and fails when one has no case above. The exhaustiveness half is the point:
// a route added to api.Handler.Mount is unreachable from the frontend until
// someone proves, here, that it answers through the production wiring.
func TestServer_APIIntegration(t *testing.T) {
	registered := registeredAPIRoutes(t)

	var missing []string
	for _, route := range registered {
		if _, ok := apiCases[route]; !ok {
			missing = append(missing, route)
			continue
		}
		t.Run(route, func(t *testing.T) {
			c := apiCases[route]
			e := newAPIEnv(t)
			c.run(t, e)

			// The gap ledger below is only as good as this check. Without
			// it, notHappyPathReason is the sole input and nothing requires
			// a case that stopped asserting a success path to declare one —
			// so a happy-path case quietly rewritten into an error-path one
			// leaves documentedIntegrationGaps untouched and produces no
			// literal for a reviewer to notice. Deriving the fact from the
			// run is the same move the exhaustiveness check above makes.
			if c.notHappyPathReason == "" {
				assert.True(t,
					slices.ContainsFunc(e.statuses, func(s int) bool { return s >= 200 && s < 300 }),
					"a case with no notHappyPathReason must drive the route's success path; "+
						"observed statuses: %v", e.statuses)
			}
		})
	}
	assert.Empty(t, missing,
		"every /api route must be driven through the real router; add a case to apiCases for each")

	var stale []string
	for route := range apiCases {
		if !slices.Contains(registered, route) {
			stale = append(stale, route)
		}
	}
	sort.Strings(stale)
	assert.Empty(t, stale, "these cases name routes the router does not register")
}

// documentedIntegrationGaps is every route whose genuine success path is not
// driven above, and it is a literal on purpose: a gap can be accepted, but not
// accumulated quietly. Adding one means changing this list, which puts it in
// the diff a reviewer reads.
//
// The list is what a reviewer reads; the check inside TestServer_APIIntegration
// is what makes it true. Neither works alone — this one cannot see a case that
// stopped asserting a 2xx without declaring it, and that one produces no
// literal in the diff.
var documentedIntegrationGaps = []string{
	"POST /api/board/broadcast",
	"POST /api/sessions/{id}/open-vscode",
}

func TestServer_APIIntegration_DocumentedGaps(t *testing.T) {
	var gaps []string
	for route, c := range apiCases {
		if c.notHappyPathReason == "" {
			continue
		}
		gaps = append(gaps, route)
		t.Logf("%s does not drive its success path: %s", route, c.notHappyPathReason)
	}
	sort.Strings(gaps)

	assert.Equal(t, documentedIntegrationGaps, gaps,
		"a route whose success path is not driven here must be listed, with a reason, in documentedIntegrationGaps")
}

// registeredAPIRoutes is every /api route on the real router, as
// "METHOD /pattern". It deliberately reuses walkRoutes so this test and the
// route table cannot disagree about what "a route" is.
//
// Both runner states are walked, and that is load-bearing rather than
// thorough: registerRoutes has an `if commandRunner != nil` block, so an /api
// route registered inside it is one the frontend can reach and one this
// table's exhaustiveness check would never see. Walking only the disabled
// router made the file header above — and quality-gateway.md's rollout row —
// claim more than the code delivered. route_table_test.go pins both states
// for the same reason.
func registeredAPIRoutes(t *testing.T) []string {
	t.Helper()

	runners := []*commandcenter.Runner{nil, commandcenter.NewRunner(commandcenter.RunnerConfig{})}

	seen := map[string]bool{}
	for _, runner := range runners {
		srv := New(testConfigWithToken(integrationToken), session.NewManager(), board.NewBoardCache(), nil, runner, emptyFS)
		t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

		for _, route := range walkRoutes(t, srv) {
			_, pattern, _ := strings.Cut(route, " ")
			if strings.HasPrefix(pattern, "/api/") {
				seen[route] = true
			}
		}
	}

	out := slices.Collect(maps.Keys(seen))
	sort.Strings(out)
	require.Positive(t, len(out), "no /api routes were found — the router changed")
	return out
}
