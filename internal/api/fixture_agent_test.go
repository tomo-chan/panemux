//go:build panemux_fixture_agent

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/tasks"
)

// These tests run only with the panemux_fixture_agent tag, which registers
// the fixture agent (issue #377): a third agent that launches and does not
// resume, which the HTTP routes handle by its declarations alone.

// fixtureAgentCollectionSections are the fixture agent's sections, which
// every host output carries in this build.
var fixtureAgentCollectionSections = []string{"panemux-fixture-agent/state", "panemux-fixture-agent/logs"}

func TestFixtureAgent_LaunchReachesTheHost(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks", `{"host":"","agent":"panemux-fixture-agent",`+
		`"cwd":"/workspace/user/project","prompt":"fix the flaky test"}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp taskLaunchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, tasks.Launched{
		TaskID: "local:panemux-fixture-agent:" + launchSessionID, SessionID: launchSessionID, TmuxSession: "task-0f0e0d0c",
	}, resp.Launched)
	require.Equal(t, 1, host.launches())
	assert.Contains(t, host.scripts[0], "\nfix the flaky test\n")
}

// A resume of the fixture agent, which declares no resume, is refused as a
// bad request, and nothing runs on the host.
func TestFixtureAgent_ResumeIsRefusedAndRunsNothing(t *testing.T) {
	host := &launchHost{answer: "::panemux-launch ok\n"}
	h, _ := newLaunchHandler(t, host)

	rec := postJSON(t, h, "/api/tasks/resume",
		`{"host":"","agent":"panemux-fixture-agent","session_id":"`+resumeSessionID+`"}`, nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Zero(t, host.launches())
}
