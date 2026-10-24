//go:build !panemux_fixture_agent

package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"panemux/internal/agent"
)

// fixtureSampleCommands and fixtureSummaryContractSamples are empty without
// the panemux_fixture_agent tag.
var (
	fixtureSampleCommands         []string
	fixtureSummaryContractSamples map[agent.ID]summaryContractSample
)

// A shipped build collects, launches and summarizes Claude Code and codex
// alone (issue #377).
func TestShippedBuild_HasNoFixtureAgentAdapters(t *testing.T) {
	assert.Empty(t, fixtureCollectAdapters())
	assert.Empty(t, fixtureLaunchAdapters())
	assert.Empty(t, fixtureSummaryAdapters())
	assert.Equal(t, "claude|codex", collectPlan.prefilter)
	assert.Equal(t, []string{"claude", "codex"}, LaunchAgents())
	assert.False(t, CanLaunch("panemux-fixture-agent"))
	_, ok := summaryPlan.lookup("panemux-fixture-agent")
	assert.False(t, ok)
}
