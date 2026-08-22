package board_test

// Tier 2 of docs/agent-board.md's agmsg compatibility contract: the
// behaviors panemux depends on, asserted against a REAL agmsg install
// rather than a fake.
//
// Tier 1 (every other test in this package) pins what panemux itself sends
// and parses, using fakes. That catches panemux regressions and nothing
// else: an agmsg release that changed one of these scripts would leave
// every Tier 1 test green while panes silently stopped communicating,
// which is the failure mode the contract exists to make mechanical.
//
// These tests are skipped unless PANEMUX_AGMSG_PATH names an agmsg install
// root, so `make check` stays hermetic. Run them with:
//
//	make test-agmsg-contract AGMSG_PATH=~/.agents/skills/agmsg
//
// The install is copied to a temp directory first. agmsg's scripts write
// into their own install root (teams/, run/, messages store), and a test
// must never mutate the operator's real one.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
)

// agmsgPathEnv names the install under test. Absent means skip: a
// contributor without agmsg installed still gets a green `make check`.
const agmsgPathEnv = "PANEMUX_AGMSG_PATH"

// watchProbeTimeout bounds how long watch.sh is allowed to run. It is a
// streaming process that never exits on its own, so the timeout is the exit
// condition; it only has to exceed the two grace periods below.
const watchProbeTimeout = 30 * time.Second

// watchStartupGrace gives watch.sh time to resolve its subscription before
// the first message is sent, and watchDeliveryGrace gives it time to scan
// and print afterwards. Both are wall-clock waits because watch.sh's only
// readiness signal is for exclusive (actas) watchers, which the broad
// watcher under test here deliberately is not.
const (
	watchStartupGrace  = 4 * time.Second
	watchDeliveryGrace = 5 * time.Second
)

// contractInstall copies the agmsg install named by PANEMUX_AGMSG_PATH into
// a temp directory and returns its scripts/ directory, skipping the test
// when the variable is unset or the tools agmsg needs are missing.
func contractInstall(t *testing.T) string {
	t.Helper()

	src := strings.TrimSpace(os.Getenv(agmsgPathEnv))
	if src == "" {
		t.Skipf("%s is not set; see docs/agent-board.md's agmsg compatibility contract", agmsgPathEnv)
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("agmsg's own scripts require sqlite3, which is not on PATH")
	}
	// G703: the path comes from an environment variable a developer sets
	// deliberately to point at their own agmsg install. Test-only, and the
	// value is the whole point of the flag — there is nothing to sanitize it
	// against. DEVELOPMENT.md allows a narrow suppression in test code for
	// exactly this shape.
	//nolint:gosec // developer-supplied install path
	_, err := os.Stat(filepath.Join(src, "scripts", "join.sh"))
	if err != nil {
		t.Fatalf("%s=%q does not look like an agmsg install: %v", agmsgPathEnv, src, err)
	}

	dst := filepath.Join(t.TempDir(), "agmsg")
	// -a preserves the executable bits agmsg's scripts rely on. G702: same
	// developer-supplied path as above, passed as a discrete argv element to
	// a fixed command with no shell involved.
	//nolint:gosec // developer-supplied install path
	out, err := exec.Command("cp", "-a", src, dst).CombinedOutput()
	require.NoError(t, err, "copying the agmsg install: %s", out)
	// The copy inherits whatever teams and locks the real install had, and
	// a leftover team or actas lock would make these assertions read the
	// wrong state. Start from an empty one.
	// db/ holds agmsg's own SQLite message store, which lives inside the
	// install root (scripts/lib/storage.sh resolves it from BASH_SOURCE, so
	// the copy really is a separate world). Clearing it is what makes the
	// message assertions below start from an empty team rather than
	// whatever the developer's own install happened to contain.
	require.NoError(t, os.RemoveAll(filepath.Join(dst, "teams")))
	require.NoError(t, os.RemoveAll(filepath.Join(dst, "run")))
	require.NoError(t, os.RemoveAll(filepath.Join(dst, "db")))

	return dst
}

// contractScripts is contractInstall's scripts/ directory, for the tests
// that invoke agmsg's scripts directly rather than through panemux's own
// client.
func contractScripts(t *testing.T) string {
	t.Helper()
	return filepath.Join(contractInstall(t), "scripts")
}

// runScript runs one agmsg script to completion and returns its combined
// output, failing the test if it exits non-zero.
func runScript(t *testing.T, scripts, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(filepath.Join(scripts, name), args...) //nolint:gosec // fixed script name, test-local paths
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s %v failed: %s", name, args, out)
	return string(out)
}

// watchDelivery starts watch.sh for one session, sends the given messages
// once it is running, and returns everything the watcher printed on stdout
// — which is the delivery stream itself, one line per message it decided
// this session should receive. stderr is returned alongside because that is
// where watch.sh reports which pairs it skipped.
//
// Delivery is the observable this test asserts on deliberately. watch.sh
// prints nothing at all about the pairs it resolved when nothing is
// skipped, so "which identities did it subscribe to" cannot be read off its
// logs in the very case that matters; what a person actually experiences —
// a message for another pane arriving in this pane — is visible either way.
// watch.sh streams forever by design, so the deadline is the exit
// condition rather than an error.
func watchDelivery(t *testing.T, scripts, sessionID, project string, send [][]string) (stdout, stderr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), watchProbeTimeout)
	defer cancel()

	//nolint:gosec // fixed script name under a test-local copy of the install
	cmd := exec.CommandContext(ctx,
		filepath.Join(scripts, "watch.sh"), sessionID, project, "claude-code")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	require.NoError(t, cmd.Start())

	// watch.sh resolves its subscription before it can deliver anything, so
	// a message sent too early is written to the store while the watcher is
	// still starting. It would still be picked up on a later scan, but this
	// keeps the test asserting on prompt delivery rather than on catch-up.
	time.Sleep(watchStartupGrace)
	for _, args := range send {
		runScript(t, scripts, "send.sh", args...)
	}
	time.Sleep(watchDeliveryGrace)

	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	return outBuf.String(), errBuf.String()
}

// TestAgmsgContract_TwoAgentsInOneProjectNeedTheClaim is the mechanical
// version of the manual verification recorded in docs/agent-board.md's "Two
// panes in one project directory". It asserts both halves: the exposure
// that exists without a claim, and that the claim closes it.
//
// If a future agmsg release makes identities.sh project-scoped differently,
// or changes watch.sh's exclusivity handling, this test fails — which is
// the entire point. panemux's bootstrap instruction tells an agent to run
// actas-claim.sh precisely because of the behavior asserted here.
func TestAgmsgContract_TwoAgentsInOneProjectNeedTheClaim(t *testing.T) {
	scripts := contractScripts(t)
	project := t.TempDir()

	runScript(t, scripts, "join.sh", "contract", "pane-a", "claude-code", project, "--force")
	runScript(t, scripts, "join.sh", "contract", "pane-b", "claude-code", project, "--force")

	// Precondition, and the reason the claim is needed at all: identities
	// are resolved per (project, type), so one directory holding two panes
	// yields both pairs to whichever session asks.
	identities := runScript(t, scripts, "identities.sh", project, "claude-code")
	assert.Contains(t, identities, "pane-a")
	assert.Contains(t, identities, "pane-b")

	forA := []string{"contract", "pane-x", "pane-a", "message-for-pane-a", "--force"}
	forB := []string{"contract", "pane-x", "pane-b", "message-for-pane-b", "--force"}

	// Without a claim, pane-b's watcher receives pane-a's message too.
	unclaimed, unclaimedErr := watchDelivery(t, scripts, "sid-b", project, [][]string{forA, forB})
	assert.Contains(t, unclaimed, "message-for-pane-a",
		"with no claim held, pane-b's watcher must still receive pane-a's message — the exposure this guards")
	assert.Contains(t, unclaimed, "message-for-pane-b")
	assert.NotContains(t, unclaimedErr, "skipping pairs held by other sessions")

	// The remedy panemux's bootstrap instruction now tells the agent to run.
	claim := runScript(t, scripts, "actas-claim.sh", project, "claude-code", "pane-a", "sid-a")
	assert.Contains(t, claim, "status=ok", "actas-claim.sh must report the claim it took")

	claimed, claimedErr := watchDelivery(t, scripts, "sid-b", project, [][]string{forA, forB})
	assert.NotContains(t, claimed, "message-for-pane-a",
		"pane-a is claimed by another session, so pane-b's watcher must not receive its messages")
	assert.Contains(t, claimed, "message-for-pane-b",
		"pane-b must still receive its own messages")
	assert.Contains(t, claimedErr, "skipping pairs held by other sessions",
		"and the watcher must say which pair it dropped")
	assert.Contains(t, claimedErr, "contract/pane-a")
}

// TestAgmsgContract_ClaimIsRefusedWhileAnotherSessionHoldsIt covers the
// branch panemux's bootstrap instruction tells the agent to stop on: a
// second live session asking for a pane ID that is already owned.
func TestAgmsgContract_ClaimIsRefusedWhileAnotherSessionHoldsIt(t *testing.T) {
	scripts := contractScripts(t)
	project := t.TempDir()

	runScript(t, scripts, "join.sh", "contract", "pane-a", "claude-code", project, "--force")
	first := runScript(t, scripts, "actas-claim.sh", project, "claude-code", "pane-a", "sid-a")
	require.Contains(t, first, "status=ok")

	// Not runScript: a refused claim exits non-zero by design (1 = held),
	// and that exit code is part of the contract.
	cmd := exec.Command(filepath.Join(scripts, "actas-claim.sh"), //nolint:gosec // fixed script name, test-local paths
		project, "claude-code", "pane-a", "sid-other")
	out, err := cmd.CombinedOutput()

	assert.Error(t, err, "claiming a pane ID another live session owns must fail")
	assert.Contains(t, string(out), "status=held",
		"the refusal must be reported as status=held, the string the bootstrap instruction names")
}

// TestAgmsgContract_JoinUsesThePaneIDVerbatim guards the assumption every
// board address rests on: agmsg registers the agent_id it was given, so a
// pane ID stays a pane ID across join, identities, and delivery.
func TestAgmsgContract_JoinUsesThePaneIDVerbatim(t *testing.T) {
	scripts := contractScripts(t)
	project := t.TempDir()

	// A realistic generated pane ID, not a friendly name: panemux's own IDs
	// carry digits and hyphens, and agmsg's agent-name validation has to
	// accept that shape.
	const paneID = "pane-1787195690568-re241"
	runScript(t, scripts, "join.sh", "contract", paneID, "claude-code", project, "--force")

	identities := runScript(t, scripts, "identities.sh", project, "claude-code")
	assert.Contains(t, identities, paneID, "the registered agent_id must be the pane ID panemux passed")
}

// ── The read/write path: send.sh and api.sh ──────────────────────────────
//
// Everything below drives panemux's OWN client — board.LocalAgmsgClient —
// against the real install, rather than re-deriving the invocations here.
// That is deliberate: a test that rebuilt the command strings itself would
// keep passing after panemux started sending something different, which is
// half of what this contract exists to detect.

// contractClient returns panemux's own local agmsg client, pointed at a
// throwaway copy of a real install with a team already joined.
func contractClient(t *testing.T, team string) (*board.LocalAgmsgClient, string) {
	t.Helper()

	root := contractInstall(t)
	scripts := filepath.Join(root, "scripts")
	project := t.TempDir()

	runScript(t, scripts, "join.sh", team, "pane-a", "claude-code", project, "--force")
	runScript(t, scripts, "join.sh", team, "pane-b", "claude-code", project, "--force")

	return board.NewLocalAgmsgClient(root), project
}

func contractCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestAgmsgContract_SendThenSinceRoundTrip is the core of the read/write
// contract: what panemux writes through send.sh must come back out of
// api.sh in the shape panemux parses, with every field intact.
func TestAgmsgContract_SendThenSinceRoundTrip(t *testing.T) {
	client, _ := contractClient(t, "contract")
	ctx := contractCtx(t)

	require.NoError(t, client.Send(ctx, "contract", "pane-a", "pane-b", "please review my latest commit"))

	rows, err := client.Since(ctx, "contract", "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1, "api.sh must return exactly the row send.sh wrote")

	assert.Equal(t, "contract", rows[0].Team)
	assert.Equal(t, "pane-a", rows[0].From)
	assert.Equal(t, "pane-b", rows[0].To)
	assert.Equal(t, "please review my latest commit", rows[0].Body)
	assert.Equal(t, "local", rows[0].Host)
	assert.NotEmpty(t, rows[0].ID, "every row must carry agmsg's own id")
	assert.False(t, rows[0].At.IsZero(),
		"the at timestamp must parse as RFC3339 — parseAgmsgMessageRows silently zeroes it otherwise")
}

// TestAgmsgContract_MessageIDsAreOpaque is the drift check for the
// assumption that broke the relay: panemux used to compare agmsg's ids
// numerically, which no id emitted by the event-log storage driver
// satisfies. The contract panemux now relies on is weaker and is asserted
// directly — ids exist and are distinct, and NOTHING about their ordering
// is assumed.
func TestAgmsgContract_MessageIDsAreOpaque(t *testing.T) {
	client, _ := contractClient(t, "contract")
	ctx := contractCtx(t)

	for _, body := range []string{"one", "two", "three"} {
		require.NoError(t, client.Send(ctx, "contract", "pane-a", "pane-b", body))
	}

	rows, err := client.Since(ctx, "contract", "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	seen := map[string]bool{}
	for _, r := range rows {
		assert.False(t, seen[r.ID], "ids within one host must be distinct: %q repeated", r.ID)
		seen[r.ID] = true
	}

	// Recorded, not required: today's ids are UUIDv7, not the integers the
	// legacy sqlite driver exposed. panemux works either way now, so this
	// only reports which driver the install under test is using.
	if _, err := strconv.ParseInt(rows[0].ID, 10, 64); err == nil {
		t.Logf("note: this agmsg install still emits integer message ids (%q) — the legacy sqlite driver", rows[0].ID)
	}
}

// TestAgmsgContract_SinceCursorAnchorsOnReturnedOrder walks the relay's
// actual poll cycle against a real store: read, remember the last id, read
// again with it, and get only what arrived in between. This is the test
// that fails against a real install if panemux ever goes back to comparing
// ids — and the one the hand-written integer fixtures could not express.
func TestAgmsgContract_SinceCursorAnchorsOnReturnedOrder(t *testing.T) {
	client, _ := contractClient(t, "contract")
	ctx := contractCtx(t)

	for _, body := range []string{"one", "two"} {
		require.NoError(t, client.Send(ctx, "contract", "pane-a", "pane-b", body))
	}

	first, err := client.Since(ctx, "contract", "", 100)
	require.NoError(t, err)
	require.Len(t, first, 2)
	cursor := first[len(first)-1].ID

	// A poll with nothing new in between must be silent. Before the fix
	// this returned the whole window again, on every tick, forever.
	quiet, err := client.Since(ctx, "contract", cursor, 100)
	require.NoError(t, err)
	assert.Empty(t, quiet, "a poll with no new rows must report nothing new")

	require.NoError(t, client.Send(ctx, "contract", "pane-b", "pane-a", "three"))

	next, err := client.Since(ctx, "contract", cursor, 100)
	require.NoError(t, err)
	require.Len(t, next, 1, "only the row written after the cursor may come back")
	assert.Equal(t, "three", next[0].Body)
}

// TestAgmsgContract_SinceLimitKeepsTheNewestRows pins the bound the relay's
// truncation tradeoff rests on: --limit returns the NEWEST n rows, oldest
// first. If agmsg ever returned the oldest n instead, the relay would stall
// on ancient rows and never reach current traffic.
func TestAgmsgContract_SinceLimitKeepsTheNewestRows(t *testing.T) {
	client, _ := contractClient(t, "contract")
	ctx := contractCtx(t)

	for _, body := range []string{"one", "two", "three", "four"} {
		require.NoError(t, client.Send(ctx, "contract", "pane-a", "pane-b", body))
	}

	rows, err := client.Since(ctx, "contract", "", 2)
	require.NoError(t, err)
	require.Len(t, rows, 2, "--limit must bound the response")
	assert.Equal(t, []string{"three", "four"}, []string{rows[0].Body, rows[1].Body},
		"--limit must keep the newest rows and return them oldest-first")
}

// TestAgmsgContract_ShellMetacharacterBodyRoundTrips is the real-install
// counterpart of the escaping unit tests in local_client_test.go /
// remote_client_test.go. Those assert the command string panemux builds;
// this asserts what agmsg actually stores and returns, which is the half a
// fake can never answer.
func TestAgmsgContract_ShellMetacharacterBodyRoundTrips(t *testing.T) {
	client, _ := contractClient(t, "contract")
	ctx := contractCtx(t)

	// Every character class that would change meaning if any layer between
	// here and the store re-parsed the body as shell or SQL.
	const hostile = `it's ${HOME}; rm -rf /; $(id) ` + "`whoami`" + ` "quoted" 'single' -- DROP TABLE messages;`

	require.NoError(t, client.Send(ctx, "contract", "pane-a", "pane-b", hostile))

	rows, err := client.Since(ctx, "contract", "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, hostile, rows[0].Body,
		"the body must round-trip byte for byte: no shell expansion, no SQL interpretation, no quote stripping")
}

// TestAgmsgContract_StatusRowRoundTrips walks the status path end to end
// against a real store: a board_status body addressed to _system comes back
// recognizable to the same parser the relay uses.
func TestAgmsgContract_StatusRowRoundTrips(t *testing.T) {
	client, _ := contractClient(t, "contract")
	ctx := contractCtx(t)

	const body = `{"kind":"board_status","state":"working","cwd":"/tmp/sample-project",` +
		`"branch":"feature/x","summary":"fixing failing tests"}`
	require.NoError(t, client.Send(ctx, "contract", "pane-a", board.SystemID, body))

	rows, err := client.Since(ctx, "contract", "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, board.SystemID, rows[0].To,
		"the _system sentinel must survive agmsg's own agent-name handling verbatim")

	status, ok := board.IsStatusRow(rows[0])
	require.True(t, ok, "a board_status body must still be recognized after a real round trip")
	assert.Equal(t, "working", status.State)
	assert.Equal(t, "/tmp/sample-project", status.CWD)
	assert.Equal(t, "fixing failing tests", status.Summary)
}

// TestAgmsgContract_SinceOnEmptyTeamIsEmptyNotAnError covers the cold-start
// read every panemux instance performs before any pane has said anything.
func TestAgmsgContract_SinceOnEmptyTeamIsEmptyNotAnError(t *testing.T) {
	client, _ := contractClient(t, "contract")

	rows, err := client.Since(contractCtx(t), "contract", "", 100)
	require.NoError(t, err, "reading a team with no messages must not be an error")
	assert.Empty(t, rows)
}

// TestAgmsgContract_InstalledVersionMatchesThePin reports when the install
// under test is not the version this repository's fixtures and prose were
// verified against. It is deliberately not a failure: the weekly canary run
// points at agmsg's latest tag on purpose, and the signal that matters
// there is whether the behavioral assertions above still hold, not the
// version string itself.
func TestAgmsgContract_InstalledVersionMatchesThePin(t *testing.T) {
	root := contractInstall(t)

	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Skipf("install has no VERSION file: %v", err)
	}
	installed := strings.TrimSpace(string(data))
	if installed != board.TestedAgmsgVersion {
		t.Logf("note: contract ran against agmsg %s, while board.TestedAgmsgVersion pins %s",
			installed, board.TestedAgmsgVersion)
	}
}
