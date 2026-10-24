//go:build panemux_fixture_agent

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixture agent is registered after every shipped agent, declaring
// some capabilities and leaving the others out, so the tests and the E2E
// run see a third agent with partial support (issue #377).
func TestFixtureAgent_IsRegisteredAfterTheShippedAgents(t *testing.T) {
	all := All()
	require.Len(t, all, len(shipped())+1)
	d := all[len(all)-1]
	assert.Equal(t, FixtureAgent, d.ID)
	assert.Equal(t, "Fixture Agent", d.DisplayName)
	assert.Equal(t, []Capability{Detect, Collect, Launch, Summary}, d.Capabilities)
	for _, c := range []Capability{Resume, Workdir, Board, Usage} {
		assert.False(t, d.Has(c), "the fixture agent does not declare %s", c)
	}
	assert.Empty(t, d.AgmsgType)
	assert.Nil(t, d.Detection.Board)

	id, ok := Resolve("panemux-fixture-agent")
	assert.True(t, ok)
	assert.Equal(t, FixtureAgent, id)
}

func TestFixtureAgent_TaskRule(t *testing.T) {
	d, ok := Lookup(FixtureAgent)
	require.True(t, ok)
	rule := d.Detection.Task
	require.NotNil(t, rule)
	cases := []struct {
		command     string
		process     bool
		interactive bool
	}{
		{"panemux-fixture-agent", true, true},
		{"/workspace/user/bin/panemux-fixture-agent --session-id=abc", true, true},
		{"panemux-fixture-agent --print hello", true, false},
		{"panemux-fixture-agent-helper", false, false},
		{"claude", false, false},
		{"sh -c panemux-fixture-agent", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.process, rule.Process(c.command), "process %q", c.command)
		assert.Equal(t, c.interactive, rule.IsInteractive(c.command), "interactive %q", c.command)
	}
}

// Without workdir and board, the fixture agent is neither a pane's session
// nor an Agent Board type.
func TestFixtureAgent_IsNoPaneSessionAndNoBoardType(t *testing.T) {
	_, ok := MatchInteractiveSession("panemux-fixture-agent")
	assert.False(t, ok)
	_, ok = MatchBoardProcess("panemux-fixture-agent")
	assert.False(t, ok)
}
