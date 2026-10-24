//go:build !panemux_fixture_agent

package tasks

// Without the panemux_fixture_agent tag — in every shipped build — the
// fixture agent has no adapters, as the registry does not register it
// (issue #377).

func fixtureCollectAdapters() []registeredCollect { return nil }
func fixtureLaunchAdapters() []registeredLaunch   { return nil }
func fixtureSummaryAdapters() []registeredSummary { return nil }
