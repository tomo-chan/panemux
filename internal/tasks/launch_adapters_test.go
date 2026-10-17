package tasks

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
)

// Every agent the registry says launches has a launch adapter registered
// under its ID, every agent that resumes has a resume adapter, and no other
// agent has either.
func TestLaunchAdapters_MatchTheRegistry(t *testing.T) {
	require.NoError(t, validateLaunchAdapters(agent.All(), launchAdapters(), resumeAdapters()))

	launches, resumes := map[agent.ID]bool{}, map[agent.ID]bool{}
	for _, r := range launchAdapters() {
		launches[r.id] = true
	}
	for _, r := range resumeAdapters() {
		resumes[r.id] = true
	}
	for _, d := range agent.All() {
		assert.Equal(t, d.Has(agent.Launch), launches[d.ID], "launch: %s", d.ID)
		assert.Equal(t, d.Has(agent.Resume), resumes[d.ID], "resume: %s", d.ID)
	}
}

type fakeLaunch struct {
	refused map[string]string
	command string
}

func (f fakeLaunch) newSession(io.Reader) (string, string, error) { return "", "task-0a1b2c3d", nil }
func (f fakeLaunch) validNewSessionID(id string) bool             { return id == "" }
func (f fakeLaunch) newCommand() string                           { return f.command }
func (f fakeLaunch) validSessionID(id string) bool                { return validUUID.MatchString(id) }
func (f fakeLaunch) tmuxSessionFor(id string) string              { return tmuxSessionForTask(id) }
func (f fakeLaunch) resumeCommand() string                        { return f.command }
func (f fakeLaunch) refusals() map[string]string                  { return f.refused }

// launchRegs is a registration under test: the launch and resume lists.
type launchRegs struct {
	l []registeredLaunch
	r []registeredResume
}

var (
	fakeRun    = fakeLaunch{command: `run='exec "$2" -- "$p"'`}
	fakeResume = fakeLaunch{command: `resume='exec "$2" "$3"'`}
)

func validLaunchRegs() *launchRegs {
	return &launchRegs{
		l: []registeredLaunch{{id: "third", adapter: fakeRun}, {id: "fourth", adapter: fakeRun}},
		r: []registeredResume{{id: "third", adapter: fakeResume}},
	}
}

func refusing(refused map[string]string) fakeLaunch {
	return fakeLaunch{command: "run='x'", refused: refused}
}

var launchRegMismatches = []struct {
	mutate func(g *launchRegs)
	name   string
	want   string
}{
	{func(g *launchRegs) { g.l = append(g.l, registeredLaunch{id: "sixth", adapter: fakeRun}) },
		"unregistered agent", `launch adapter "sixth" is not registered in the agent registry`},
	{func(g *launchRegs) { g.l = append(g.l, registeredLaunch{id: "fifth", adapter: fakeRun}) },
		"launch not declared", `launch adapter "fifth" does not declare launch in the agent registry`},
	{func(g *launchRegs) { g.r = append(g.r, registeredResume{id: "fourth", adapter: fakeResume}) },
		"resume not declared", `resume adapter "fourth" does not declare resume in the agent registry`},
	{func(g *launchRegs) { g.l = append(g.l, g.l[0]) },
		"launch twice", `launch adapter "third" is registered twice`},
	{func(g *launchRegs) { g.l = g.l[:1] },
		"declared launch without adapter", `agent "fourth" declares launch but has no launch adapter`},
	{func(g *launchRegs) { g.r = nil },
		"declared resume without adapter", `agent "third" declares resume but has no resume adapter`},
	{func(g *launchRegs) { g.l[0].adapter = fakeLaunch{command: `exec "$2"`} },
		"new command does not set run", `launch adapter "third": its command does not set run`},
	{func(g *launchRegs) { g.r[0].adapter = fakeLaunch{command: `run='exec "$2"'`} },
		"resume command does not set resume", `resume adapter "third": its command does not set resume`},
	{func(g *launchRegs) { g.l[0].adapter = fakeLaunch{command: `run='exec "$2" {{PROMPT}}'`} },
		"command holds a placeholder", `launch adapter "third": its command holds "{{"`},
	{func(g *launchRegs) { g.r[0].adapter = fakeLaunch{command: "resume='x' ;;\nesac"} },
		"command closes the case", `resume adapter "third": its command holds ";;"`},
	{func(g *launchRegs) { g.l[0].adapter = fakeLaunch{command: "{ say 'error too-new'; exit 0; }\nrun='x'"} },
		"command refuses with an undeclared code",
		`launch adapter "third": its command refuses with "too-new", which it does not declare`},
	{func(g *launchRegs) {
		g.l[0].adapter = fakeLaunch{
			command: "{ say 'error too-old'; exit 0; }\n{ say 'error too-new'; exit 0; }\nrun='x'",
			refused: map[string]string{"too-old": "m"},
		}
	}, "a later refusal with an undeclared code",
		`launch adapter "third": its command refuses with "too-new", which it does not declare`},
	{func(g *launchRegs) { g.l[0].adapter = refusing(map[string]string{"Too New": "m"}) },
		"refusal code not a word", `launch adapter "third": invalid refusal code "Too New"`},
	{func(g *launchRegs) { g.l[0].adapter = refusing(map[string]string{RefusedNoTmux: "m"}) },
		"refusal code taken by the script", `launch adapter "third": refusal code "no-tmux" is the launch script's own`},
	{func(g *launchRegs) { g.l[0].adapter = refusing(map[string]string{"too-new": ""}) },
		"refusal without message", `launch adapter "third": refusal code "too-new" has no message`},
}

func TestValidateLaunchAdapters_RejectsAMismatchedRegistration(t *testing.T) {
	defs := []agent.Definition{
		{ID: "third", Capabilities: []agent.Capability{agent.Detect, agent.Launch, agent.Resume}},
		{ID: "fourth", Capabilities: []agent.Capability{agent.Detect, agent.Launch}},
		{ID: "fifth", Capabilities: []agent.Capability{agent.Detect}},
	}
	g := validLaunchRegs()
	require.NoError(t, validateLaunchAdapters(defs, g.l, g.r), "a launch without a resume is a valid registration")
	g.l[0].adapter = fakeLaunch{command: "{ say 'error no-third'; exit 0; }\nrun='x'"}
	require.NoError(t, validateLaunchAdapters(defs, g.l, g.r), "an adapter may refuse with its own no-<agent>")
	_, err := composeLaunch(defs, g.l[:1], g.r)
	require.EqualError(t, err, `agent "fourth" declares launch but has no launch adapter`, "nothing is composed")

	for _, tt := range launchRegMismatches {
		t.Run(tt.name, func(t *testing.T) {
			g := validLaunchRegs()
			tt.mutate(g)
			assert.EqualError(t, validateLaunchAdapters(defs, g.l, g.r), tt.want)
		})
	}
}

// The launch script is one fixed script: each adapter's command sits in the
// case of its agent and mode, and a pair no adapter has refuses.
func TestLaunchScript_ComposesEachAdapterUnderItsAgentAndMode(t *testing.T) {
	script := launchPlan.script
	assert.Contains(t, script, "case $agent:$mode in\n")
	for _, c := range []string{"claude:new)", "claude:resume)", "codex:new)", "codex:resume)"} {
		assert.Equal(t, 1, strings.Count(script, "\n"+c+"\n"), c)
	}
	assert.Contains(t, script, "*) say 'error unsupported'; exit 0 ;;\nesac\n")
}

// A refusal an adapter declares has its fixed message; the script's own
// no-<agent> is worded from the agent's ID.
func TestLaunchError_AdapterAndAgentRefusals(t *testing.T) {
	assert.Equal(t, "codex on the host is too old: it has no --no-daemon option",
		(&LaunchError{Code: RefusedCodexTooOld}).Error())
	assert.Equal(t, "claude was not found on the host", (&LaunchError{Code: refusedNoAgent(agent.Claude)}).Error())
	assert.Equal(t, `the host refused the launch ("no-cursor")`, (&LaunchError{Code: "no-cursor"}).Error(),
		"an agent that cannot be launched has no message of its own")
}

// An agent the registry knows but that declares neither launch nor resume
// is refused before anything runs.
func TestLaunchAndResume_AnAgentWithoutTheCapabilityRunsNothing(t *testing.T) {
	svc := New(Options{Rand: launchRand(), RunLocal: func(context.Context, string) ([]byte, error) {
		t.Fatal("ran a script for an agent that cannot be launched")
		return nil, nil
	}})
	defer svc.Close()
	require.True(t, agent.Cursor.Valid())
	_, err := svc.Launch(context.Background(), LaunchRequest{Agent: string(agent.Cursor), CWD: "/w", Prompt: "go"})
	assert.ErrorIs(t, err, ErrInvalidLaunch)
	assert.ErrorContains(t, err, `agent "cursor" cannot be launched`)
	_, err = svc.Resume(context.Background(), "", string(agent.Cursor), testSessionID)
	assert.ErrorIs(t, err, ErrInvalidLaunch)
	assert.ErrorContains(t, err, `agent "cursor" cannot be resumed`)
}

func TestCanLaunch(t *testing.T) {
	assert.True(t, CanLaunch(AgentClaude))
	assert.True(t, CanLaunch(AgentCodex))
	assert.False(t, CanLaunch(string(agent.Cursor)))
	assert.False(t, CanLaunch(""))
	assert.Equal(t, []string{AgentClaude, AgentCodex}, LaunchAgents())
}

// probedLaunchHost is a panemux host whose collection, probe and launch
// script answer as set, counting the launches.
type probedLaunchHost struct {
	probeHost
	launches int
}

func (h *probedLaunchHost) run(ctx context.Context, script string) ([]byte, error) {
	if strings.Contains(script, "say() { printf '::panemux-launch") {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.launches++
		return []byte("::panemux-launch ok\n"), nil
	}
	return h.probeHost.run(ctx, script)
}

func (h *probedLaunchHost) counts() (launches, collects, probes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.launches, h.collects, h.probes
}

// An agent whose CLI the host's probe did not find is refused before
// anything runs, as the launch script would refuse it, and the host is
// probed again at once, so a CLI installed since is found for the next try.
func TestLaunchAndResume_ACLIMissingOnTheHostIsRefusedBeforeAnythingRuns(t *testing.T) {
	host := &probedLaunchHost{probeHost: probeHost{
		collection: minimalOutput("s"),
		probe:      probeAnswer("cli claude missing", "cli codex found"),
	}}
	svc := New(Options{RunLocal: host.run})
	defer svc.Close()
	collectAndProbe(svc)

	_, err := svc.Launch(context.Background(), LaunchRequest{Agent: AgentClaude, CWD: "/w", Prompt: "go"})
	var refused *LaunchError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, RefusedNoClaude, refused.Code)
	assert.EqualError(t, err, "claude was not found on the host")
	svc.probeRuns.Wait()
	launches, collects, probes := host.counts()
	assert.Equal(t, 0, launches)
	assert.Equal(t, 2, probes, "probed again, though the first probe is younger than its TTL")

	_, err = svc.Resume(context.Background(), "", AgentClaude, testSessionID)
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, RefusedNoClaude, refused.Code)
	svc.probeRuns.Wait()
	launches, collectsAfter, _ := host.counts()
	assert.Equal(t, 0, launches)
	assert.Equal(t, collects, collectsAfter, "refused before the host is collected for the resume")

	_, err = svc.Launch(context.Background(), LaunchRequest{Agent: AgentCodex, CWD: "/w", Prompt: "go"})
	require.NoError(t, err, "another agent's CLI is there")

	host.mu.Lock()
	host.probe = probeAnswer("cli claude found", "cli codex found")
	host.mu.Unlock()
	svc.reprobe("")
	svc.probeRuns.Wait()
	_, err = svc.Launch(context.Background(), LaunchRequest{Agent: AgentClaude, CWD: "/w", Prompt: "go"})
	require.NoError(t, err, "found once the host is probed again")
}

// What is not known is not a reason to refuse: before any probe, and when
// the probe failed, the launch script finds out for itself.
func TestLaunch_AnUnknownCLIIsLeftToTheLaunchScript(t *testing.T) {
	for _, tt := range []struct {
		err     error
		name    string
		collect bool
	}{
		{name: "not probed"},
		{name: "probe failed", collect: true, err: errors.New("exit 1")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := &probedLaunchHost{probeHost: probeHost{
				collection: minimalOutput("s"), probeErr: tt.err,
			}}
			svc := New(Options{RunLocal: host.run})
			defer svc.Close()
			if tt.collect {
				collectAndProbe(svc)
			}
			_, err := svc.Launch(context.Background(), LaunchRequest{Agent: AgentClaude, CWD: "/w", Prompt: "go"})
			require.NoError(t, err)
			launches, _, _ := host.counts()
			assert.Equal(t, 1, launches)
		})
	}
}

// A remote host's probe counts only for the connection it ran over: one
// from before a reconnect does not refuse a launch.
func TestCLIMissing_OnlyForTheCurrentConnection(t *testing.T) {
	svc := New(Options{})
	defer svc.Close()
	old := &fakeConn{}
	svc.mu.Lock()
	svc.probes = map[string]probeEntry{"build-box": {conn: old, found: map[agent.ID]bool{agent.Claude: false}}}
	svc.hosts = map[string]*hostConn{"build-box": {conn: &fakeConn{}}}
	svc.mu.Unlock()
	assert.False(t, svc.cliMissing("build-box", agent.Claude))

	svc.mu.Lock()
	svc.hosts["build-box"].conn = old
	svc.mu.Unlock()
	assert.True(t, svc.cliMissing("build-box", agent.Claude))
	assert.False(t, svc.cliMissing("build-box", agent.Codex), "an agent the probe did not answer for")
}
