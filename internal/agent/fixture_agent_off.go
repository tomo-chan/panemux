//go:build !panemux_fixture_agent

package agent

// fixtureAgents is empty without the panemux_fixture_agent tag, so no
// shipped binary registers the fixture agent (issue #377).
func fixtureAgents() []Definition { return nil }
