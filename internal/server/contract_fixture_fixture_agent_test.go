//go:build panemux_fixture_agent

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
	"panemux/internal/tasks"
)

// fixtureAgentSessionID is the fixture agent's session on the panemux host.
const fixtureAgentSessionID = "11111111-2222-4333-8444-555555555501"

// fixtureAgentCollection is the panemux host's output with one fixture agent
// session waiting in an attachable tmux session, and its log; the shipped
// agents' sections are there and empty, as a host without them prints them.
const fixtureAgentCollection = `::panemux-tasks v2
::now 1790000000
::section claude/state
::section codex/open
::section codex/permission
::section panemux-fixture-agent/state
::file 301.json
{"pid":301,"sessionId":"` + fixtureAgentSessionID + `","cwd":"/workspace/user/project","status":"waiting",` +
	`"waitingFor":"approve the plan","statusUpdatedAt":1789999820000}
::section ps
300 1 -zsh
301 300 panemux-fixture-agent --session-id=` + fixtureAgentSessionID + `
::section tmux
300 task-11111111
::section cwd
::section env
::section claude/transcripts
::section codex/rollouts
::section panemux-fixture-agent/logs
1789999900	2048	` + fixtureAgentSessionID + `.log
::end
`

// captureTasksWithFixtureAgent is GET /api/tasks in a build that registers
// the fixture agent (issue #377): the agent among agents[] with what it
// declares, its availability on the host, and its task with a summary and
// without a resume. The same production code as the tasks capture reads it.
func captureTasksWithFixtureAgent(t *testing.T) ([]byte, map[string]string) {
	e := newAPIEnv(t)
	e.srv.api.SetTaskService(tasks.New(tasks.Options{
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			switch {
			case strings.Contains(script, fixtureProbeHeader):
				return []byte(fixtureProbeHeader +
					"\ncli claude found\ncli codex found\ncli panemux-fixture-agent found\n::end\n"), nil
			case strings.Contains(script, ".panemux-fixture-agent/logs/"+fixtureAgentSessionID+".log"):
				return []byte(`{"text":"Add the fixture agent"}` + "\n" + `{"text":"The adapters are in."}` + "\n"), nil
			}
			return []byte(fixtureAgentCollection), nil
		},
		Summarizers: map[agent.ID]tasks.SummarizeFunc{agent.FixtureAgent: fixtureSummarize},
	}))
	e.cfg.TaskDashboard.Summary.Enabled = true

	var rr = e.do(t, http.MethodGet, "/api/tasks", "")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Eventually(t, func() bool {
		rr = e.do(t, http.MethodGet, "/api/tasks", "")
		return rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), `"state":"ready"`) &&
			!strings.Contains(rr.Body.String(), `"reason":"not_probed"`)
	}, 10*time.Second, 20*time.Millisecond, "the fixture agent's summary becomes ready and the host is probed")
	return rr.Body.Bytes(), nil
}

// TestAPIContractFixtures_FixtureAgent writes the one fixture only this
// build can capture, and checks it is the same on every capture. The
// untagged suite writes every other fixture and knows this one's name.
func TestAPIContractFixtures_FixtureAgent(t *testing.T) {
	body, literals := captureTasksWithFixtureAgent(t)
	want := normalizedFixtureBytes(t, body, literals)
	for i := 1; i < 5; i++ {
		body, literals = captureTasksWithFixtureAgent(t)
		require.Equal(t, string(want), string(normalizedFixtureBytes(t, body, literals)), "capture %d differs", i+1)
	}
	writeContractFixture(t, fixtureAgentTasksFixture, body, literals)
}
