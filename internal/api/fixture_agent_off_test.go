//go:build !panemux_fixture_agent

package api

// fixtureAgentCollectionSections is empty without the panemux_fixture_agent
// tag, whose build alone registers the fixture agent (issue #377).
var fixtureAgentCollectionSections []string
