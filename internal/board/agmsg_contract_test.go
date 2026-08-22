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
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agmsgPathEnv names the install under test. Absent means skip: a
// contributor without agmsg installed still gets a green `make check`.
const agmsgPathEnv = "PANEMUX_AGMSG_PATH"

// watchProbeTimeout bounds how long watch.sh is allowed to run. It is a
// streaming process that never exits on its own; everything asserted here
// is written to stderr during its first subscription-resolution pass, well
// inside this budget.
const watchProbeTimeout = 15 * time.Second

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
	require.NoError(t, os.RemoveAll(filepath.Join(dst, "teams")))
	require.NoError(t, os.RemoveAll(filepath.Join(dst, "run")))

	return filepath.Join(dst, "scripts")
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

// watchStderr starts watch.sh, collects its stderr until it goes quiet, and
// kills it. watch.sh streams forever by design, so the deadline is the exit
// condition rather than an error.
//
// stderr is the observable this test asserts on deliberately: it is what a
// person running agmsg sees, and it names the (team, agent) pairs the
// watcher actually resolved. Sourcing agmsg's own subscription library
// would reach past that public surface and could keep passing after the
// behavior a user sees had changed.
func watchStderr(t *testing.T, scripts, sessionID, project string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), watchProbeTimeout)
	defer cancel()

	//nolint:gosec // fixed script name under a test-local copy of the install
	cmd := exec.CommandContext(ctx,
		filepath.Join(scripts, "watch.sh"), sessionID, project, "claude-code")
	pipe, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	var collected strings.Builder
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		collected.WriteString(scanner.Text())
		collected.WriteString("\n")
		// Every line asserted on here names a pair. Once both registered
		// pairs have been accounted for there is nothing further to wait
		// for, and holding the process open would only add latency.
		if strings.Count(collected.String(), "pane-") >= 2 {
			break
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	return collected.String()
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
	scripts := contractInstall(t)
	project := t.TempDir()

	runScript(t, scripts, "join.sh", "contract", "pane-a", "claude-code", project, "--force")
	runScript(t, scripts, "join.sh", "contract", "pane-b", "claude-code", project, "--force")

	// Precondition, and the reason the claim is needed at all: identities
	// are resolved per (project, type), so one directory holding two panes
	// yields both pairs to whichever session asks.
	identities := runScript(t, scripts, "identities.sh", project, "claude-code")
	assert.Contains(t, identities, "pane-a")
	assert.Contains(t, identities, "pane-b")

	// Without a claim, pane-b's watcher resolves pane-a's identity too —
	// it would receive messages addressed to the other pane.
	unclaimed := watchStderr(t, scripts, "sid-b", project)
	assert.Contains(t, unclaimed, "contract/pane-a",
		"with no claim held, pane-b's watcher must still resolve pane-a — the exposure this guards")
	assert.Contains(t, unclaimed, "contract/pane-b")
	assert.NotContains(t, unclaimed, "skipping pairs held by other sessions")

	// The remedy panemux's bootstrap instruction now tells the agent to run.
	claim := runScript(t, scripts, "actas-claim.sh", project, "claude-code", "pane-a", "sid-a")
	assert.Contains(t, claim, "status=ok", "actas-claim.sh must report the claim it took")

	claimed := watchStderr(t, scripts, "sid-b", project)
	assert.Contains(t, claimed, "skipping pairs held by other sessions",
		"pane-a is claimed, so pane-b's watcher must say it is skipping it")
	assert.Contains(t, claimed, "contract/pane-a", "the skip message names the pair it dropped")
	assert.Contains(t, claimed, "contract/pane-b", "pane-b's own pair must still resolve")
}

// TestAgmsgContract_ClaimIsRefusedWhileAnotherSessionHoldsIt covers the
// branch panemux's bootstrap instruction tells the agent to stop on: a
// second live session asking for a pane ID that is already owned.
func TestAgmsgContract_ClaimIsRefusedWhileAnotherSessionHoldsIt(t *testing.T) {
	scripts := contractInstall(t)
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
	scripts := contractInstall(t)
	project := t.TempDir()

	// A realistic generated pane ID, not a friendly name: panemux's own IDs
	// carry digits and hyphens, and agmsg's agent-name validation has to
	// accept that shape.
	const paneID = "pane-1787195690568-re241"
	runScript(t, scripts, "join.sh", "contract", paneID, "claude-code", project, "--force")

	identities := runScript(t, scripts, "identities.sh", project, "claude-code")
	assert.Contains(t, identities, paneID, "the registered agent_id must be the pane ID panemux passed")
}
