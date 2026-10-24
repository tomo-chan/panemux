//go:build panemux_fixture_agent

package agent

import (
	"path/filepath"
	"strings"
)

// FixtureAgent is an agent that exists only in a build with the
// panemux_fixture_agent tag: the third agent the tests and the E2E run add
// through registration alone, to show that the common code, the API and the
// dashboard need no change for a new agent (issue #377). Its CLI is a fake
// the E2E run puts on PATH, named like its ID, as every launchable agent's
// CLI is.
const FixtureAgent ID = "panemux-fixture-agent"

// fixtureHeadless is the argument that makes a fixture agent process a
// one-shot run, as the fake CLI's summary mode is, rather than a session.
const fixtureHeadless = "--print"

// fixtureAgents registers the fixture agent after every shipped agent. It
// declares detect, collect, launch and summary, and leaves resume, workdir,
// board and usage out, so it also stands for an agent supported in part.
func fixtureAgents() []Definition {
	return []Definition{{
		ID: FixtureAgent, DisplayName: "Fixture Agent",
		Capabilities: []Capability{Detect, Collect, Launch, Summary},
		Detection: Detection{Task: &TaskRule{
			Process: isFixtureProcess, Interactive: isInteractiveFixture, UnknownArgs: Accept,
		}},
	}}
}

// isFixtureProcess reports whether command's program is the fixture CLI.
func isFixtureProcess(command string) bool {
	fields := strings.Fields(command)
	return len(fields) > 0 && filepath.Base(fields[0]) == string(FixtureAgent)
}

// isInteractiveFixture is a fixture process that is not a one-shot run.
func isInteractiveFixture(command string) bool {
	if !isFixtureProcess(command) {
		return false
	}
	for _, arg := range strings.Fields(command)[1:] {
		if arg == fixtureHeadless {
			return false
		}
	}
	return true
}
