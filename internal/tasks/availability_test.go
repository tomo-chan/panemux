package tasks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
)

// The probe is its own fixed script: no value of any request is put into
// it, it is the same on every host, and each agent it asks about is a
// registry ID of an agent panemux can launch or resume.
func TestProbeScript_IsFixedAndAsksAboutEveryLaunchableAgent(t *testing.T) {
	assert.Equal(t, []agent.ID{agent.Claude, agent.Codex}, probeAgents)
	assert.True(t, strings.HasPrefix(probeScript, "LC_ALL=C\n"))
	assert.Contains(t, probeScript, "echo '"+probeHeader+"'\n")
	assert.Contains(t, probeScript, `bin=$(command -v "$1" 2>/dev/null)`)
	assert.Contains(t, probeScript, `"${SHELL:-/bin/sh}" -lc "command -v $1" </dev/null`)
	assert.Contains(t, probeScript, "probe_cli claude\nprobe_cli codex\n")
	assert.True(t, strings.HasSuffix(probeScript, "echo '::end'\n"))
	assert.NotEqual(t, collectScript, probeScript, "the probe is not part of the collection")
}

func TestComposeProbe_RefusesAnIDTheShellCouldReadAsMoreThanAWord(t *testing.T) {
	_, err := composeProbe([]agent.ID{"claude; rm"})
	require.Error(t, err)
	script, err := composeProbe([]agent.ID{"grok-build"})
	require.NoError(t, err)
	assert.Contains(t, script, "probe_cli grok-build\n")
}

func TestParseProbeOutput(t *testing.T) {
	agents := []agent.ID{agent.Claude, agent.Codex}
	ok := "noise from a profile\n" + probeHeader + "\ncli claude found\ncli codex missing\n::end\n"
	found, err := parseProbeOutput([]byte(ok), agents)
	require.NoError(t, err)
	assert.Equal(t, map[agent.ID]bool{agent.Claude: true, agent.Codex: false}, found)

	for name, out := range map[string]string{
		"no header":         "cli claude found\ncli codex found\n::end\n",
		"no end":            probeHeader + "\ncli claude found\ncli codex found\n",
		"an agent missing":  probeHeader + "\ncli claude found\n::end\n",
		"an unknown answer": probeHeader + "\ncli claude found\ncli codex maybe\n::end\n",
		"an unknown agent":  probeHeader + "\ncli claude found\ncli codex found\ncli gemini found\n::end\n",
		"a malformed line":  probeHeader + "\ncli claude found\ncli codex found\nbogus\n::end\n",
		"an agent twice":    probeHeader + "\ncli claude found\ncli claude missing\ncli codex found\n::end\n",
	} {
		_, err := parseProbeOutput([]byte(out), agents)
		assert.Error(t, err, name)
	}
}

// The probe run for real: a CLI on PATH is found with command -v, one only
// a login shell knows is found through it, and one neither knows is missing.
func TestRunLocal_ProbeFindsTheCLIOnPathOrThroughALoginShell(t *testing.T) {
	bin := t.TempDir()
	hidden := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "claude"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(hidden, "codex"), "#!/bin/sh\nexit 0\n")
	// The stand-in login shell answers `-lc "command -v codex"` with the
	// path only it knows, as a profile that extends PATH would.
	shell := filepath.Join(t.TempDir(), "login-shell")
	writeExecutable(t, shell, "#!/bin/sh\ncase \"$2\" in *' codex') echo '"+filepath.Join(hidden, "codex")+"' ;; esac\n")

	run := func(t *testing.T, env ...string) map[agent.ID]bool {
		t.Helper()
		cmd := exec.Command("sh", "-s")
		cmd.Stdin = strings.NewReader(probeScript)
		cmd.Env = append([]string{"PATH=" + bin + ":/usr/bin:/bin"}, env...)
		out, err := cmd.Output()
		require.NoError(t, err)
		found, err := parseProbeOutput(out, probeAgents)
		require.NoError(t, err)
		return found
	}
	assert.Equal(t, map[agent.ID]bool{agent.Claude: true, agent.Codex: true}, run(t, "SHELL="+shell))
	assert.Equal(t, map[agent.ID]bool{agent.Claude: true, agent.Codex: false},
		run(t, "SHELL="+filepath.Join(t.TempDir(), "no-such-shell")))
}

// probeHost answers the collection script with collection and the probe
// with probe, counting each.
type probeHost struct {
	probeErr   error
	collection []byte
	probe      []byte
	probes     int
	collects   int
}

func (h *probeHost) run(_ context.Context, script string) ([]byte, error) {
	if script == probeScript {
		h.probes++
		return h.probe, h.probeErr
	}
	h.collects++
	return h.collection, nil
}

func probeAnswer(lines ...string) []byte {
	return []byte(probeHeader + "\n" + strings.Join(lines, "\n") + "\n::end\n")
}

func availabilityOf(list []Availability, id agent.ID, c agent.Capability) Availability {
	for _, a := range list {
		if a.Agent == id && a.Capability == c {
			return a
		}
	}
	return Availability{}
}

func TestAvailability_BeforeAnyCollectionNothingIsProbed(t *testing.T) {
	svc := New(Options{RunLocal: (&probeHost{}).run})
	defer svc.Close()
	list := svc.Availability("")
	require.Len(t, list, 6, "collect, launch and resume of claude and codex")
	for _, a := range list {
		assert.Equal(t, AvailabilityUnknown, a.Status, "%+v", a)
		assert.Equal(t, ReasonNotProbed, a.Reason, "%+v", a)
	}
	assert.Equal(t, agent.Claude, list[0].Agent)
	assert.Equal(t, []agent.Capability{agent.Collect, agent.Launch, agent.Resume},
		[]agent.Capability{list[0].Capability, list[1].Capability, list[2].Capability})
}

// A missing CLI makes launch and resume unavailable and leaves collect
// alone: the logs of a CLI that was removed are still there to list.
func TestAvailability_AMissingCLIMakesLaunchAndResumeUnavailable(t *testing.T) {
	host := &probeHost{
		collection: minimalOutput("s"),
		probe:      probeAnswer("cli claude found", "cli codex missing"),
	}
	svc := New(Options{RunLocal: host.run})
	defer svc.Close()

	snap := svc.Collect(context.Background())
	require.Equal(t, HostOK, hostResult(t, snap, "").Status)
	list := svc.Availability("")

	assert.Equal(t, Availability{Agent: agent.Claude, Capability: agent.Launch, Status: Available},
		availabilityOf(list, agent.Claude, agent.Launch))
	assert.Equal(t, Available, availabilityOf(list, agent.Claude, agent.Resume).Status)
	for _, c := range []agent.Capability{agent.Launch, agent.Resume} {
		got := availabilityOf(list, agent.Codex, c)
		assert.Equal(t, Unavailable, got.Status)
		assert.Equal(t, ReasonCLIMissing, got.Reason)
		assert.Equal(t, "codex is not on PATH or in a login shell's PATH", got.Detail)
	}
	assert.Equal(t, Availability{Agent: agent.Codex, Capability: agent.Collect, Status: Available},
		availabilityOf(list, agent.Codex, agent.Collect))
}

// A probe that fails leaves the collection as it was: the host is OK and
// its tasks are listed. Launch and resume are unknown, with why.
func TestAvailability_AFailedProbeDoesNotFailTheCollection(t *testing.T) {
	for name, host := range map[string]*probeHost{
		"an error":       {probeErr: errors.New("exit 1")},
		"garbled output": {probe: []byte("hello\n")},
	} {
		t.Run(name, func(t *testing.T) {
			host.collection = minimalOutput("s")
			svc := New(Options{RunLocal: host.run})
			defer svc.Close()

			snap := svc.Collect(context.Background())
			assert.Equal(t, HostOK, hostResult(t, snap, "").Status)
			assert.Empty(t, hostResult(t, snap, "").Error)
			assert.Len(t, snap.Tasks, 1)
			got := availabilityOf(svc.Availability(""), agent.Claude, agent.Launch)
			assert.Equal(t, AvailabilityUnknown, got.Status)
			assert.Equal(t, ReasonProbeFailed, got.Reason)
			assert.NotEmpty(t, got.Detail)
		})
	}
}

// The probe runs with the full collection only, once per host within its
// TTL — failed or not — and again once that has passed.
func TestAvailability_TheProbeIsCachedPerHostForFiveMinutes(t *testing.T) {
	clk := &clock{now: collectedAt}
	host := &probeHost{collection: minimalOutput("s"), probeErr: errors.New("exit 1")}
	svc := New(Options{RunLocal: host.run, Now: clk.Now})
	defer svc.Close()

	svc.CollectAttention(context.Background())
	svc.CollectHostLive(context.Background(), "")
	assert.Zero(t, host.probes, "the attention collection does not probe")
	assert.Equal(t, ReasonNotProbed, availabilityOf(svc.Availability(""), agent.Claude, agent.Launch).Reason)

	svc.Collect(context.Background())
	assert.Equal(t, 1, host.probes)
	host.probe, host.probeErr = probeAnswer("cli claude found", "cli codex found"), nil
	clk.advance(probeTTL - time.Second)
	svc.Collect(context.Background())
	assert.Equal(t, 1, host.probes, "a failure is cached too")
	assert.Equal(t, ReasonProbeFailed, availabilityOf(svc.Availability(""), agent.Claude, agent.Launch).Reason)

	clk.advance(time.Second)
	svc.Collect(context.Background())
	assert.Equal(t, 2, host.probes)
	assert.Equal(t, Available, availabilityOf(svc.Availability(""), agent.Claude, agent.Launch).Status)
}

// A host whose collection fails is not probed.
func TestAvailability_AFailedCollectionIsNotProbed(t *testing.T) {
	host := &probeHost{collection: []byte("garbage\n")}
	svc := New(Options{RunLocal: host.run})
	defer svc.Close()
	svc.Collect(context.Background())
	assert.Zero(t, host.probes)
}

// A remote host is probed over its collection connection, with `sh -s` and
// the script on stdin, and probed again on a new connection.
func TestAvailability_ARemoteHostIsProbedAgainAfterReconnecting(t *testing.T) {
	first := &fakeConn{output: minimalOutput("s")}
	second := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{conns: []*fakeConn{first, second}}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     dialer.dial,
		RunLocal: (&probeHost{collection: minimalOutput("l")}).run,
	})
	defer svc.Close()

	svc.Collect(context.Background())
	svc.Collect(context.Background())
	assert.Equal(t, []string{"sh -s", "sh -s", "sh -s"}, first.cmds)
	assert.Equal(t, []string{collectScript, probeScript, collectScript}, first.stdins)
	got := availabilityOf(svc.Availability("build-box"), agent.Codex, agent.Resume)
	assert.Equal(t, ReasonProbeFailed, got.Reason, "the fake answers the probe with collection output")

	require.NoError(t, svc.Reconnect("build-box"))
	assert.Equal(t, ReasonNotProbed, availabilityOf(svc.Availability("build-box"), agent.Codex, agent.Resume).Reason,
		"what the old connection found is not reported for the new one")
	svc.Collect(context.Background())
	assert.Equal(t, []string{collectScript, probeScript}, second.stdins)
}

// A host removed from the configuration forgets what was found about it.
func TestAvailability_AHostRemovedFromTheConfigIsForgotten(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	hosts := []string{"build-box"}
	svc := New(Options{
		Hosts:    func() []string { return hosts },
		Dial:     (&fakeDialer{conns: []*fakeConn{conn}}).dial,
		RunLocal: (&probeHost{collection: minimalOutput("l")}).run,
	})
	defer svc.Close()
	svc.Collect(context.Background())
	assert.Equal(t, ReasonProbeFailed, availabilityOf(svc.Availability("build-box"), agent.Claude, agent.Launch).Reason)
	assert.Equal(t, Available, availabilityOf(svc.Availability("build-box"), agent.Claude, agent.Collect).Status)

	hosts = nil
	svc.Collect(context.Background())
	for _, a := range svc.Availability("build-box") {
		assert.Equal(t, ReasonNotProbed, a.Reason, "%+v", a)
	}
}

// An unknown format makes the agent's collect degraded, with how many of
// each kind, a few examples and the versions seen; the change is logged
// once each way.
func TestAvailability_AnUnknownFormatDegradesCollectAndIsLoggedOnChange(t *testing.T) {
	unknown := fullOutput(
		"::panemux-tasks v2",
		"::now 1000",
		"::section claude/state",
		"::file 7.json",
		`{"pid":7,"sessionId":"s","status":"pondering","statusUpdatedAt":999000,"version":"2.3.0"}`,
		"::section ps",
		"7 1 claude",
		"::end",
	)
	host := &probeHost{collection: unknown, probe: probeAnswer("cli claude found", "cli codex found")}
	var logs []string
	svc := New(Options{RunLocal: host.run, Logf: func(format string, args ...any) {
		logs = append(logs, strings.TrimSpace(fmt.Sprintf(format, args...)))
	}})
	defer svc.Close()

	snap := svc.Collect(context.Background())
	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, StateUnknown, snap.Tasks[0].State, "listed, its state not guessed")
	got := availabilityOf(svc.Availability(""), agent.Claude, agent.Collect)
	assert.Equal(t, Availability{
		Agent: agent.Claude, Capability: agent.Collect, Status: Degraded, Reason: ReasonUnknownFormat,
		Detail:          `unknown_status 1; e.g. status "pondering"; versions 2.3.0`,
		ObservedVersion: "2.3.0",
	}, got)
	assert.Equal(t, Available, availabilityOf(svc.Availability(""), agent.Claude, agent.Launch).Status,
		"an unknown format is collect's, not launch's")
	assert.Equal(t, Available, availabilityOf(svc.Availability(""), agent.Codex, agent.Collect).Status)

	svc.Collect(context.Background())
	host.collection = minimalOutput("s")
	svc.Collect(context.Background())
	assert.Equal(t, Available, availabilityOf(svc.Availability(""), agent.Claude, agent.Collect).Status)
	assert.Equal(t, []string{
		`task collection on the panemux host: claude: collect degraded (unknown_format): ` +
			`unknown_status 1; e.g. status "pondering"; versions 2.3.0`,
		`task collection on the panemux host: claude: collect available again`,
	}, logs)
}

func TestFormatReport_Detail(t *testing.T) {
	var r formatReport
	assert.Empty(t, r.detail())
	for i, ex := range []string{"a", "b", "c", "d"} {
		r.add("malformed_row", ex)
		if i == 0 {
			r.add("unknown_status", "status \"x\"\x07"+strings.Repeat("y", 200))
		}
	}
	for _, v := range []struct {
		v  string
		at int64
	}{{"0.4.0", 4}, {"0.5.0", 9}, {"0.4.0", 3}, {"0.3.0", 2}, {"0.2.0", 1}, {"", 10}} {
		r.observeVersion(v.v, v.at)
	}
	detail := r.detail()
	assert.True(t, strings.HasPrefix(detail, "malformed_row 4, unknown_status 1; e.g. a | "), detail)
	assert.NotContains(t, detail, "\x07")
	assert.Contains(t, detail, "…")
	assert.NotContains(t, detail, "| c", "three examples at most")
	assert.True(t, strings.HasSuffix(detail, "; versions 0.5.0, 0.4.0, 0.3.0"), detail)
	assert.Equal(t, "0.5.0", r.newestVersion())

	var tie formatReport
	tie.observeVersion("first", 5)
	tie.observeVersion("second", 5)
	assert.Equal(t, "first", tie.newestVersion(), "of two sessions written at once, the first seen")
}

// A probe cut short because the caller gave up is not kept: the next full
// collection probes again rather than reporting a failure for five minutes.
func TestAvailability_AnAbandonedProbeIsNotCached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	host := &probeHost{collection: minimalOutput("s")}
	svc := New(Options{RunLocal: func(c context.Context, script string) ([]byte, error) {
		if script == probeScript {
			cancel()
			<-c.Done()
			host.probes++
			return nil, c.Err()
		}
		return host.run(c, script)
	}})
	defer svc.Close()

	svc.Collect(ctx)
	assert.Equal(t, 1, host.probes)
	assert.Equal(t, ReasonNotProbed, availabilityOf(svc.Availability(""), agent.Claude, agent.Launch).Reason)
}
