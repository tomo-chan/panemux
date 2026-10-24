//go:build !panemux_fixture_agent

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A build without the panemux_fixture_agent tag — every shipped binary —
// registers the shipped agents and nothing else (issue #377).
func TestShippedBuild_RegistersNoFixtureAgent(t *testing.T) {
	assert.Equal(t, definitionIDs(shipped()), definitionIDs(All()))
	assert.Empty(t, fixtureAgents())
	_, ok := Resolve("panemux-fixture-agent")
	assert.False(t, ok)
}
