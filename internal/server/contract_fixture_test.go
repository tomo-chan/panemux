package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/commandcenter"
	"panemux/internal/config"
	"panemux/internal/session"
)

// This file is the Go half of gate G3(c) in docs/quality-gateway.md — Zod
// schema round-trips — and issue #191's item (c).
//
// The problem it closes: frontend/src/schemas/index.test.ts had 103 tests and
// not one of them saw JSON that Go produced. Every case was a hand-written
// TypeScript object, so renaming a field on a Go struct and leaving the Zod
// schema alone kept BOTH suites green, and the mismatch surfaced only when
// the dashboard failed to parse a live response in a browser.
//
// The direction is deliberately one-way: Go -> fixture -> TypeScript. This
// test captures what the real router really returned, normalizes the handful
// of machine-dependent values listed below, and rewrites the fixtures under
// testdata/api-contract/ on every run. frontend/src/schemas/contract.test.ts
// then parses each one with the schema that owns it. Because the Go suite
// runs first — in `make test`, in `make check`, and in ci.yml — a Go-side
// field rename reaches the fixture before vitest reads it, and the frontend
// test is what goes red. That is the property issue #191 asked to be
// verified by perturbation, and it only holds in that order.
//
// Rewriting rather than diffing is the other half of that choice. A golden
// test that FAILS on drift would stop at the Go suite and the frontend would
// never see the new shape — which is the failure this gate exists to catch.
// The cost is that a fixture is only as current as the last `make test`; the
// benefit is that `git status` shows the contract change as a diff a reviewer
// reads, next to the struct that caused it.

// contractFixtureDir is where the captured responses live, relative to this
// package's own directory. It sits at the repository root rather than under
// either side, because neither side owns it: Go writes it and TypeScript
// reads it. See its README.md.
const contractFixtureDir = "../../testdata/api-contract"

// Values replaced during normalization. Each one is machine-dependent or
// per-run random, so leaving it verbatim would rewrite the fixture on every
// run and turn a clean checkout dirty. Every replacement is a value, never a
// key or a type — the shape a schema validates is never touched.
const (
	// fixtureTimestamp replaces every RFC3339 value. Timestamps are wall
	// clock by construction (BoardCache.RecordStatus stamps its own, and
	// history entries carry the moment the query ran).
	fixtureTimestamp = "2026-01-01T00:00:00Z"
	// fixtureHomePath replaces the test's temp HOME. Required by
	// DEVELOPMENT.md's path-sanitization rule as much as by determinism: a
	// captured directory listing would otherwise commit a real path from
	// whichever machine last ran the suite.
	fixtureHomePath = "/tmp/sample-project"
	// fixtureEpoch replaces BoardCache.Epoch(), which is random per process.
	fixtureEpoch = "0000000000000000"
	// fixtureShell replaces whatever GET /api/detect-shell resolved to,
	// which is the shell of the machine running the suite.
	fixtureShell = "/bin/sh"
)

// contractFixture captures one response, exactly as the server emitted it.
type contractFixture struct {
	// capture returns the JSON body, plus the literal substitutions to apply
	// to it. The keys are values observed at capture time (a temp path, a
	// random epoch); nothing here is guessed ahead of the run.
	capture func(t *testing.T) (body []byte, literals map[string]string)
}

// Which optional fields a capture leaves unpopulated is NOT tracked here, and
// the first version of this file got that wrong. It carried a prose
// `optionalFieldsUnexercised` string per fixture plus a literal list of the
// fixtures carrying one, in the shape api_integration_test.go's
// documentedIntegrationGaps uses — but the two inputs were the same hand-
// written set, so the check could only ever catch a *declared* gap going
// undeclared, never an undeclared one. It duly reported full coverage while
// `open-url` never populated `port` and `workspaces` never populated
// LayoutNode's own `pane`.
//
// Optionality is declared in the Zod schemas, so the check belongs where it
// can be derived rather than asserted: frontend/src/schemas/contract.test.ts
// walks each schema against its capture and names every optional field the
// capture leaves absent, against a declared list with a reason for each.

// contractFixtures maps a fixture file name (without .json) to its capture.
// Every schema the frontend parses a server response with must appear here;
// frontend/src/schemas/contract.test.ts enforces the other direction, failing
// when a schema has no fixture.
var contractFixtures = map[string]contractFixture{
	"workspaces": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		seedRichLayout(e.cfg)

		rr := e.do(t, http.MethodGet, "/api/workspaces", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"display": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		e.cfg.Display = config.DisplayConfig{ShowHeader: true, ShowStatusBar: false}

		rr := e.do(t, http.MethodGet, "/api/display", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"detect-shell": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)

		rr := e.do(t, http.MethodGet, "/api/detect-shell", "")
		require.Equal(t, http.StatusOK, rr.Code)

		var resp struct {
			Shell string `json:"shell"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		return rr.Body.Bytes(), map[string]string{resp.Shell: fixtureShell}
	}},

	"ssh-connections": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		e.cfg.SSHConnections = map[string]config.SSHConnection{
			"build-box": {Host: "build.invalid", User: "demo"},
			"gpu-box":   {Host: "gpu.invalid", User: "demo"},
		}

		rr := e.do(t, http.MethodGet, "/api/ssh-connections", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"ssh-config-hosts": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		// Two hosts: one with every optional field the schema allows, one
		// with none of them, so the fixture covers both sides of `port` and
		// `identity_file` being omitted.
		writeSSHConfig(t, e.home, strings.Join([]string{
			"Host build-box",
			"  HostName build.invalid",
			"  User demo",
			"  Port 2222",
			"  IdentityFile /remote/home/demo/.ssh/id_ed25519",
			"",
			"Host gpu-box",
			"  HostName gpu.invalid",
			"  User demo",
			"",
		}, "\n"))

		rr := e.do(t, http.MethodGet, "/api/ssh-config/hosts", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"directories": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		require.NoError(t, os.MkdirAll(filepath.Join(e.home, "project", "src"), 0o750))
		require.NoError(t, os.Mkdir(filepath.Join(e.home, "notes"), 0o750))

		rr := e.do(t, http.MethodGet, "/api/directories?path="+e.home, "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), map[string]string{e.home: fixtureHomePath}
	}},

	"sessions": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		// Fake panes rather than real ones: a real PTY pane's reported state
		// depends on how far its shell has got, and this fixture is read by
		// a schema whose `state` is an enum. api_integration_test.go drives
		// the genuine POST /api/sessions path.
		e.mgr.Add(newWSFakeSession("pane-editor"))
		e.mgr.Add(newWSFakeSession("pane-build"))

		rr := e.do(t, http.MethodGet, "/api/sessions", "")
		require.Equal(t, http.StatusOK, rr.Code)
		// Sorted, because this is the one capture whose order is not the
		// server's own. GetSessions returns session.Manager.List(), which
		// ranges over a map, and Go randomizes map iteration — so the two
		// panes swapped places between runs (measured: 4 of 20). Nothing
		// failed, because D10 has these files rewritten rather than diffed,
		// so an unrelated `make test-go` just left a reordered file behind,
		// and DEVELOPMENT.md's "commit the fixture diff" then carried the
		// noise into someone's branch. A fixture diff that is sometimes
		// meaningless is a weaker signal than one that is always real.
		//
		// Sorted here rather than in GetSessions: no client depends on the
		// order, so imposing one on the API would be this fixture changing
		// production behavior to suit itself.
		return sortJSONArrayByID(t, rr.Body.Bytes()), nil
	}},

	// Only the is_git:false shape is reachable hermetically; every other
	// field needs a pane whose live cwd is a real repository, which means
	// session.CWDGetter (procfs on Linux, lsof on macOS) and a `gh pr view`
	// lookup. The frontend's own declared-gap list names each missing field.
	"git-info": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		e.mgr.Add(newWSFakeSession("pane-editor"))

		rr := e.do(t, http.MethodGet, "/api/sessions/pane-editor/git-info", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"session-token": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)

		rr := e.do(t, http.MethodGet, "/api/session-token", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"open-url": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		e.createPane(t, "pane-url")

		rr := e.do(t, http.MethodPost, "/api/sessions/pane-url/open-url",
			`{"url":"http://localhost:53682/callback"}`)
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), map[string]string{e.home: fixtureHomePath}
	}},

	"board-status": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		// One pane reporting everything an agent can report, one reporting
		// only what it must, so the fixture covers both ends of a schema
		// whose fields are all optional but one.
		e.cache.RecordStatus("pane-editor", board.Status{
			State:    "working",
			CWD:      "/workspace/user/project",
			Branch:   "feature/contract-fixtures",
			Repo:     "user/project",
			PRURL:    "https://example.invalid/user/project/pull/1",
			LastTool: "Edit",
			Summary:  "Adding the schema round-trip",
		})
		e.cache.RecordStatus("pane-build", board.Status{State: "idle"})

		rr := e.do(t, http.MethodGet, "/api/board/status", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"board-messages": {capture: func(t *testing.T) ([]byte, map[string]string) {
		e := newAPIEnv(t)
		e.cache.AppendMessage(board.Row{
			At: time.Now(), ID: "01a02760-c340-7ec7-8f18-071cce739579", Host: "local",
			Team: "panemux", From: "pane-editor", To: "pane-build",
			Body: "the build is green",
		})
		// A status row: whether a message is one is computed server-side by
		// board.IsStatusRow, and the frontend is required not to re-derive
		// it (see BoardMessageSchema's own comment), so a fixture carrying
		// only ordinary rows would leave that field's true case unproven.
		statusRow := board.Row{
			At: time.Now(), ID: "01a02760-c340-7ae0-81eb-31430e02886a", Host: "local",
			Team: "panemux", From: "pane-build", To: board.SystemID,
			Body: `{"kind":"board_status","state":"idle","summary":"waiting for review"}`,
		}
		// Asserted against board.IsStatusRow itself rather than against the
		// response body: both halves of its rule matter (addressed to
		// board.SystemID AND a body whose key is "kind", not "type"), and
		// this fixture got the second one wrong first time round, capturing
		// two ordinary rows while looking complete. Checking the row rather
		// than the JSON keeps this from also pinning the response field's
		// name — which is the contract the fixture exists to carry to the
		// frontend, and would stop there if a rename failed here first.
		_, isStatus := board.IsStatusRow(statusRow)
		require.True(t, isStatus, "the seeded row must read as a status self-report")
		e.cache.AppendMessage(statusRow)

		rr := e.do(t, http.MethodGet, "/api/board/messages", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), map[string]string{e.cache.Epoch(): fixtureEpoch}
	}},

	"board-command-history": {capture: func(t *testing.T) ([]byte, map[string]string) {
		// The history is written by the Runner while it streams a query, and
		// read back by the route from the operator's own config directory —
		// so the two only meet when the Runner's HistoryPath is the path the
		// route resolves. Pointing it anywhere else would have captured the
		// empty-list shape and quietly proven nothing.
		e := newAPIEnv(t)
		runQueryThroughWS(t, e.home)

		rr := e.do(t, http.MethodGet, "/api/board/command/history", "")
		require.Equal(t, http.StatusOK, rr.Code)
		return rr.Body.Bytes(), nil
	}},

	"ws-control-frames": {capture: func(t *testing.T) ([]byte, map[string]string) {
		return captureWSControlFrames(t), nil
	}},

	"ws-board-command-frames": {capture: func(t *testing.T) ([]byte, map[string]string) {
		return captureBoardCommandFrames(t), nil
	}},
}

// sortJSONArrayByID orders a JSON array of objects by their "id".
//
// The elements stay json.RawMessage and are never decoded into a struct. That
// is the whole point: decoding into a typed value and re-encoding would drop
// any key the struct does not name, so a Go field added to the response would
// vanish from the fixture — silently deleting the contract this file exists to
// carry. (internal/api's sessionInfo is unexported anyway, so there is no
// struct here to decode into.)
func sortJSONArrayByID(t *testing.T, body []byte) []byte {
	t.Helper()

	var rows []json.RawMessage
	require.NoError(t, json.Unmarshal(body, &rows))

	sort.SliceStable(rows, func(i, j int) bool { return rawJSONID(t, rows[i]) < rawJSONID(t, rows[j]) })

	out, err := json.Marshal(rows)
	require.NoError(t, err)
	return out
}

func rawJSONID(t *testing.T, row json.RawMessage) string {
	t.Helper()

	var probe struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(row, &probe))
	return probe.ID
}

// seedRichLayout replaces the starting single-pane layout with one that
// exercises every field PaneConfigSchema and LayoutChildSchema allow: a
// nested split, all four pane types' distinguishing fields, the optional
// per-pane header/status-bar overrides, and agent_board. A fixture built
// from the default layout would validate a schema against three fields and
// report the contract as covered.
func seedRichLayout(cfg *config.Config) {
	enabled := true
	hidden := false

	cfg.Workspaces.Items[0].Layout = config.LayoutNode{
		Direction: "horizontal",
		Children: []config.LayoutChild{
			{
				Size: 40,
				Pane: &config.PaneConfig{
					ID: "pane-editor", Type: "local", Shell: "/bin/sh",
					Cwd: "/workspace/user/project", Title: "Editor",
					ShowHeader: &enabled, ShowStatusBar: &hidden,
					AgentBoard: config.PaneAgentBoardConfig{Enabled: &enabled, Mode: "both"},
				},
			},
			{
				Size:      60,
				Direction: "vertical",
				Children: []config.LayoutChild{
					{Size: 50, Pane: &config.PaneConfig{
						ID: "pane-build", Type: "ssh", Connection: "build-box",
						Cwd: "/remote/home/demo", Title: "Build",
					}},
					{Size: 50, Pane: &config.PaneConfig{
						ID: "pane-logs", Type: "ssh_tmux", Connection: "build-box",
						TmuxSession: "logs", Title: "Logs",
					}},
				},
			},
		},
	}
	cfg.Workspaces.Items = append(cfg.Workspaces.Items, config.WorkspaceConfig{
		ID:    "scratch",
		Title: "Scratch",
		Layout: config.LayoutNode{
			Direction: "horizontal",
			// LayoutNode carries its own optional `pane` alongside `children`,
			// and it is set here so the key reaches the fixture — renaming its
			// json tag has to fail something. It is set *alongside* children
			// rather than instead of them, and that is a compromise worth
			// stating: config.LayoutNode's own `direction` and `children` are
			// `omitempty` while LayoutNodeSchema requires both, so a layout of
			// `{pane: ...}` alone — which config.Validate accepts, since
			// validateLayoutNode permits an empty direction and no children —
			// serializes to `{"pane":{...}}` and Zod rejects the whole
			// workspaces response. That mismatch predates this branch and is a
			// production question (which side is right), not a fixture one; it
			// was found by capturing that shape here and watching the frontend
			// suite go red. Recorded rather than quietly avoided.
			// Every optional key set, so this occurrence of PaneConfigSchema
			// covers them at this path too — the frontend's optional-field
			// walk tracks a path, not a schema, so a sparse pane here would
			// report nine gaps that are already covered one level down.
			Pane: &config.PaneConfig{
				ID: "pane-scratch", Type: "tmux", TmuxSession: "scratch", Title: "Scratch",
				Shell: "/bin/sh", Cwd: "/workspace/user/project", Connection: "build-box",
				ShowHeader: &enabled, ShowStatusBar: &hidden,
				AgentBoard: config.PaneAgentBoardConfig{Enabled: &enabled, Mode: "monitor"},
			},
			Children: []config.LayoutChild{
				{Size: 100, Pane: &config.PaneConfig{ID: "pane-scratch", Type: "tmux", TmuxSession: "scratch"}},
			},
		},
	})
}

// runQueryThroughWS drives one full command center query over
// /ws/board-command, with the Runner persisting into the same history file
// GET /api/board/command/history reads. home must be the HOME the caller
// already set, since that is what DefaultHistoryFilePath resolves against.
func runQueryThroughWS(t *testing.T, home string) {
	t.Helper()

	historyPath, err := commandcenter.DefaultHistoryFilePath()
	require.NoError(t, err)
	require.Contains(t, historyPath, home,
		"the history file must resolve inside the test's own HOME, never the developer's")

	e := newWSEnvIn(t, home, newFixtureRunnerAt(t, fixtureClaudeScript(t, ""), historyPath))
	conn, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, conn)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"prompt":"which panes are working?"}`)))
	require.Equal(t, "line", readControl(t, conn)["type"])
	require.Equal(t, "done", readControl(t, conn)["type"])
}

// captureWSControlFrames returns every JSON control frame one pane's
// lifetime produces over /ws/{sessionID}, in order: the connect status, the
// replay bracket around buffered output, and the final status the dashboard
// learns a pane exited from.
func captureWSControlFrames(t *testing.T) []byte {
	t.Helper()

	e := newWSEnv(t, nil)
	sess := e.addFakePane(t, "pane-editor")

	sess.out <- []byte("prompt$ ")
	e.waitForBufferedOutput(t, "pane-editor")

	conn, _ := e.dial(t, "/ws/pane-editor")
	require.NotNil(t, conn)

	frames := []json.RawMessage{
		readRawControl(t, conn), // status: connected
		readRawControl(t, conn), // replay: start
	}
	msgType, _ := readFrame(t, conn) // the replayed output itself, binary
	require.Equal(t, websocket.BinaryMessage, msgType)
	frames = append(frames, readRawControl(t, conn)) // replay: end

	sess.state = session.StateExited
	require.NoError(t, sess.Close())
	frames = append(frames, readRawControl(t, conn)) // status: exited

	out, err := json.Marshal(frames)
	require.NoError(t, err)
	return out
}

// captureBoardCommandFrames returns one of every server->client frame
// /ws/board-command emits: error, busy, line and done. The gate file is what
// makes `busy` reachable without a timing assumption — see
// TestWSIntegration_BoardCommandRoute_SecondConcurrentQueryIsBusy.
func captureBoardCommandFrames(t *testing.T) []byte {
	t.Helper()

	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	gate := "touch " + shellQuote(started) + "\n" +
		"while [ ! -f " + shellQuote(release) + " ]; do sleep 0.02; done\n"

	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, gate)))
	first, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, first)
	second, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, second)

	require.NoError(t, second.WriteMessage(websocket.TextMessage, []byte("{not json")))
	frames := []json.RawMessage{readRawControl(t, second)} // error

	require.NoError(t, first.WriteMessage(websocket.TextMessage, []byte(`{"prompt":"which panes are working?"}`)))
	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, wsReadTimeout, 10*time.Millisecond, "the first query's subprocess never started")

	require.NoError(t, second.WriteMessage(websocket.TextMessage, []byte(`{"prompt":"the impatient one"}`)))
	frames = append(frames, readRawControl(t, second)) // busy

	require.NoError(t, os.WriteFile(release, nil, 0o600))
	frames = append(frames,
		readRawControl(t, first), // line
		readRawControl(t, first), // done
	)

	out, err := json.Marshal(frames)
	require.NoError(t, err)
	return out
}

// readRawControl reads one text frame and returns it byte for byte, so the
// fixture records what the server wrote rather than a re-encoding of it.
func readRawControl(t *testing.T, conn *websocket.Conn) json.RawMessage {
	t.Helper()

	// The deadline is this function's own, not inherited. captureWSControlFrames
	// happens to survive without it — its one readFrame call sets an absolute
	// deadline the later reads reuse — but that is an accident of frame order,
	// and captureBoardCommandFrames never calls readFrame at all. Without this,
	// a regression that stops one of the four board-command frames being emitted
	// blocks here forever and the package dies on its own 10-minute timeout with
	// a goroutine dump, instead of failing in 5s naming the missing frame.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wsReadTimeout)))
	msgType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, msgType)
	return append(json.RawMessage(nil), data...)
}

// newFixtureRunnerAt is newFixtureRunner with the history file placed
// somewhere the caller chooses.
func newFixtureRunnerAt(t *testing.T, claudeBin, historyPath string) *commandcenter.Runner {
	t.Helper()

	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(historyPath), 0o750))

	return commandcenter.NewRunner(commandcenter.RunnerConfig{
		ClaudeBin:    claudeBin,
		SessionPath:  filepath.Join(dir, "session.json"),
		HistoryPath:  historyPath,
		AllowedTools: commandcenter.AllowedTools(),
		BuildMCPConfig: func() (string, func(), error) {
			return mcpPath, func() {}, nil
		},
	})
}

// TestAPIContractFixtures captures every fixture and writes it to disk. It is
// a generator, not an assertion about the previous run's output — see this
// file's header for why failing on drift would defeat the gate.
func TestAPIContractFixtures(t *testing.T) {
	require.NoError(t, os.MkdirAll(contractFixtureDir, 0o750))

	for name, fixture := range contractFixtures {
		t.Run(name, func(t *testing.T) {
			body, literals := fixture.capture(t)
			writeContractFixture(t, name, body, literals)
		})
	}
}

// Every capture must produce the same bytes every time. Nothing else can see
// this: the files are rewritten rather than diffed (D10), so a capture whose
// order wobbles succeeds silently and leaves a modified file behind for the
// next `git status` to attribute to whatever branch happened to run the suite.
// That is how `sessions` shipped map-ordered — GetSessions ranges over
// session.Manager's map, and Go randomizes that.
//
// Repetition is the detector, and its strength is worth measuring rather than
// assuming — the obvious estimate is wrong. Go does not hand out uniform
// permutations: it randomizes the starting *offset* within a bucket, so for a
// two-entry map one order comes up about seven times out of eight (measured:
// 87.6% / 12.4% over 100k iterations). n runs therefore agree by luck with
// probability ~0.876^n, not 0.5^n — at the first draft's 5 runs this caught
// the real `sessions` bug only 6 times in 12, where the uniform model
// predicted 19 in 20. At the 20 runs below it caught the same bug 18
// times in 20.
//
// So this narrows the window; it does not close it, and it is not what makes
// `sessions` correct — sorting the capture is. This is the net for the *next*
// map-ordered capture somebody adds.
func TestAPIContractFixtures_AreDeterministic(t *testing.T) {
	const runs = 20

	for name, fixture := range contractFixtures {
		t.Run(name, func(t *testing.T) {
			body, literals := fixture.capture(t)
			want := normalizedFixtureBytes(t, body, literals)

			for i := 1; i < runs; i++ {
				body, literals = fixture.capture(t)
				require.Equal(t, string(want), string(normalizedFixtureBytes(t, body, literals)),
					"capture %d of %d differs from the first; a map iteration order reached the fixture",
					i+1, runs)
			}
		})
	}
}

// The fixture directory must hold exactly the files the table names. A
// renamed capture would otherwise leave its old output behind, and the
// frontend would keep parsing a file nothing regenerates — a passing test
// over a contract that no longer exists.
//
// This one genuinely is about the directory rather than about content, so
// reading disk is right. It does depend on TestAPIContractFixtures having
// written it — `go test` runs a file's tests in declaration order, and that
// one is declared above — which is stated here because nothing enforces it:
// run under -shuffle=on, or alone via -run, a stray file from a rename would
// still be reported, but a *missing* one would only mean nobody generated it
// yet. That is the weaker half, and it is the half the frontend's own
// "reads every fixture the Go side captured" check covers from the other side.
func TestAPIContractFixtures_DirectoryHasNoStrayFiles(t *testing.T) {
	entries, err := os.ReadDir(contractFixtureDir)
	require.NoError(t, err)

	var found []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue // README.md
		}
		found = append(found, strings.TrimSuffix(entry.Name(), ".json"))
	}
	sort.Strings(found)

	want := make([]string, 0, len(contractFixtures))
	for name := range contractFixtures {
		want = append(want, name)
	}
	sort.Strings(want)

	assert.Equal(t, want, found,
		"every .json in %s must be produced by contractFixtures, and vice versa", contractFixtureDir)
}

// writeContractFixture normalizes and writes one captured response.
func writeContractFixture(t *testing.T, name string, body []byte, literals map[string]string) {
	t.Helper()

	out := normalizedFixtureBytes(t, body, literals)
	path := filepath.Join(contractFixtureDir, name+".json")
	require.NoError(t, os.WriteFile(path, append(out, '\n'), 0o600))
}

// normalizedFixtureBytes is exactly what lands on disk, minus the write — so
// the determinism check above compares what a reader would actually see rather
// than a separate rendering of it.
func normalizedFixtureBytes(t *testing.T, body []byte, literals map[string]string) []byte {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(string(body)))
	// Numbers stay in the exact spelling Go emitted; decoding them as
	// float64 and re-encoding would turn a large integer into exponent
	// notation that no Go handler ever wrote.
	decoder.UseNumber()

	var decoded any
	require.NoError(t, decoder.Decode(&decoded), "the captured body must be JSON: %s", body)

	out, err := json.MarshalIndent(normalizeFixtureValue(decoded, literals), "", "  ")
	require.NoError(t, err)
	return out
}

// normalizeFixtureValue walks a decoded response and rewrites the values
// listed at the top of this file. Keys, nesting and types are never touched:
// those are the contract, and rewriting them would be rewriting the thing
// under test.
func normalizeFixtureValue(v any, literals map[string]string) any {
	switch typed := v.(type) {
	case map[string]any:
		for k, child := range typed {
			typed[k] = normalizeFixtureValue(child, literals)
		}
		return typed
	case []any:
		for i, child := range typed {
			typed[i] = normalizeFixtureValue(child, literals)
		}
		return typed
	case string:
		return normalizeFixtureString(typed, literals)
	default:
		return v
	}
}

func normalizeFixtureString(s string, literals map[string]string) string {
	// Longest key first, and sorted at all because ranging a map is the same
	// randomized order the `sessions` capture was fixed for, one layer down.
	// Inert while every capture passes a single literal, and the two most
	// likely additions already overlap by prefix: newWSEnvIn sets HOME and
	// XDG_CACHE_HOME, and the latter is the former plus "/.cache". Replacing
	// the shorter one first rewrites the longer one's prefix and leaves it
	// unmatchable, so with placeholders that do not happen to nest the same
	// way, one run in some fraction would write a different fixture than the
	// next — landing, under D10, as a committed file that alternates between
	// two spellings.
	froms := make([]string, 0, len(literals))
	for from := range literals {
		froms = append(froms, from)
	}
	sort.Slice(froms, func(i, j int) bool {
		if len(froms[i]) != len(froms[j]) {
			return len(froms[i]) > len(froms[j])
		}
		return froms[i] < froms[j]
	})

	for _, from := range froms {
		to := literals[from]
		if from == "" || from == to {
			continue
		}
		s = strings.ReplaceAll(s, from, to)
	}
	// Checked after the literal substitutions so a temp path is not mistaken
	// for anything else first. A string only reaches here as a timestamp if
	// it parses as one in full, so an ordinary value is never rewritten.
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return fixtureTimestamp
	}
	return s
}

// The substitution order above is latent today — every capture passes one
// literal — so it is checked directly rather than through a fixture that
// cannot yet exercise it. The pair here is the one most likely to be added
// next: newWSEnvIn sets HOME and XDG_CACHE_HOME, and the second is the first
// plus "/.cache". The placeholders deliberately do NOT nest the same way the
// real paths do, which is what makes the two orders give different answers —
// with nesting placeholders both orders agree by luck and the test would pass
// against the unsorted code.
func TestNormalizeFixtureString_AppliesTheMoreSpecificLiteralFirst(t *testing.T) {
	const home = "/tmp/TestSomething123/001"
	literals := map[string]string{
		home:                 "/remote/home/demo",
		home + "/.cache":     "/tmp/sample-cache",
		"/some/other/prefix": "/unused",
	}

	// Ranging the map is randomized, so a single pass could agree by chance;
	// repeating makes a regression fail rather than flake.
	for range 20 {
		assert.Equal(t, "/tmp/sample-cache", normalizeFixtureString(home+"/.cache", literals))
		assert.Equal(t, "/remote/home/demo", normalizeFixtureString(home, literals))
	}
}

// Nothing a capture produces may carry a path from the machine that captured
// it. DEVELOPMENT.md's path-sanitization rule applies to fixtures by name, and
// a normalization rule that silently stopped matching would put a developer's
// home directory into a committed file.
//
// This re-captures rather than reading the directory, and the difference is
// the whole point. Reading disk made the subject "the files already in git",
// which for the failure above is the thing under suspicion, not the reference
// — and it only saw this run's output at all because TestAPIContractFixtures
// is declared earlier in this file and `go test` runs a file's tests in
// declaration order. Nothing stated that dependency and nothing enforced it,
// so `-run TestAPIContractFixtures_ContainNoMachinePaths` — a plausible thing
// to type while iterating on a normalization rule, which is exactly when the
// rule is broken — passed in 6ms having regenerated nothing. Demonstrated by
// dropping the `directories` capture's literal: filtered run ok, full run red.
func TestAPIContractFixtures_ContainNoMachinePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	for name, fixture := range contractFixtures {
		t.Run(name, func(t *testing.T) {
			body, literals := fixture.capture(t)
			written := string(normalizedFixtureBytes(t, body, literals))

			assert.NotContains(t, written, home, "%s carries the capturing machine's home directory", name)
			// t.TempDir() names its directories after the test
			// ("/tmp/TestAPIContractFixtures.../001"), and macOS puts them
			// under /var/folders. A bare os.TempDir() check is not usable
			// here: fixtureHomePath is itself /tmp/sample-project, which is
			// the placeholder these paths are replaced *with*.
			for _, prefix := range []string{filepath.Join(os.TempDir(), "Test"), "/var/folders/"} {
				assert.NotContains(t, written, prefix, "%s carries a temp path from the capturing machine", name)
			}
		})
	}
}
