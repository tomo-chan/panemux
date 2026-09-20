package session

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The remote resolver's fixtures, written out once: a Claude session whose
// transcript is NOT under the directory name panemux derives from its cwd.
const (
	remoteSessionID  = "session-123"
	remoteSessionCWD = "/home/dev.user/my.project"
	// What claudeProjectDirName derives, and what older Claude releases used.
	remoteDerivedDir = "-home-dev-user-my-project"
	// Where this host actually keeps it — dots left as dots.
	remoteActualDir = "-home-dev-user-my.project"
)

func remoteProjectsRoot() string { return "/home/dev.user/.claude/projects" }

func transcriptCmds(dir string) (fingerprint, read string) {
	shellPath := "~/.claude/projects/" + shellQuotePath(dir+"/"+remoteSessionID+".jsonl")
	return remoteFileFingerprintCmd(shellPath), "cat " + shellPath
}

func absTranscriptCmds(dir string) (fingerprint, read string) {
	shellPath := shellQuotePath(dir + "/" + remoteSessionID + ".jsonl")
	return remoteFileFingerprintCmd(shellPath), "cat " + shellPath
}

const remoteTranscriptBody = `{"type":"assistant","cwd":"/home/dev.user/my.project/worktree",` +
	`"message":{"content":[{"type":"text","text":"ok"}]}}` + "\n"

// baseRemoteOutputs is a host on which only the process list and the session
// metadata resolve; every transcript path is added by the individual test.
func baseRemoteOutputs() map[string][]byte {
	return map[string][]byte{
		sshListProcessesCmd: []byte(" 100 1 sh\n 220 100 claude\n"),
		"cat ~/.claude/sessions/220.json": []byte(fmt.Sprintf(
			`{"pid":220,"sessionId":%q,"cwd":%q}`, remoteSessionID, remoteSessionCWD,
		)),
	}
}

func probeCmd() string {
	return remoteClaudeProjectProbeCmd(remoteSessionID)
}

// forgetRemoteClaudeProjectDirs clears the per-session probe answers so a
// test starts from a host nothing has been asked about yet.
func forgetRemoteClaudeProjectDirs(t *testing.T) {
	t.Helper()
	reset := func() {
		remoteClaudeProjectDirs.mu.Lock()
		defer remoteClaudeProjectDirs.mu.Unlock()
		remoteClaudeProjectDirs.answered = nil
	}
	reset()
	t.Cleanup(reset)
}

func captureRemoteLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

// TestActiveRemoteWorkdir_DerivedTranscriptFound_DoesNotProbe is the cost
// guard, and the reason the fallback is shaped the way it is: on the path that
// works, the resolver must issue exactly the commands it issued before.
func TestActiveRemoteWorkdir_DerivedTranscriptFound_DoesNotProbe(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)
	fingerprint, read := transcriptCmds(remoteDerivedDir)

	outputs := baseRemoteOutputs()
	outputs[fingerprint] = []byte("120 1700000000\n")
	outputs[read] = []byte(remoteTranscriptBody)
	outputs["ls -1 "+remoteClaudeTranscriptShellPath(
		remoteDerivedDir+"/"+remoteSessionID+"/subagents", "",
	)+" 2>/dev/null || true"] = []byte("")

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)

	require.NoError(t, err)
	assert.Equal(t, []string{"/home/dev.user/my.project/worktree"}, cwds)
	assert.NotContains(t, runner.commands, probeCmd(),
		"the derived path resolved, so nothing should have been probed")
}

// TestActiveRemoteWorkdir_DerivedTranscriptMissing_ProbesAndReadsTheRealOne
// is the fallback itself: the encoding changed under the host, and the pane
// still learns its worktree.
func TestActiveRemoteWorkdir_DerivedTranscriptMissing_ProbesAndReadsTheRealOne(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)
	logged := captureRemoteLog(t)

	derivedFingerprint, _ := transcriptCmds(remoteDerivedDir)
	actual := remoteProjectsRoot() + "/" + remoteActualDir
	actualFingerprint, actualRead := absTranscriptCmds(actual)

	outputs := baseRemoteOutputs()
	outputs[derivedFingerprint] = nil // present but empty: no such file
	outputs[probeCmd()] = []byte(actual + "/" + remoteSessionID + ".jsonl\n")
	outputs[actualFingerprint] = []byte("120 1700000000\n")
	outputs[actualRead] = []byte(remoteTranscriptBody)
	outputs["ls -1 "+shellQuotePath(actual+"/"+remoteSessionID+"/subagents")+" 2>/dev/null || true"] = []byte("")

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)

	require.NoError(t, err)
	assert.Equal(t, []string{"/home/dev.user/my.project/worktree"}, cwds)
	assert.Contains(t, logged.String(), remoteDerivedDir, "the log must name what was derived")
	assert.Contains(t, logged.String(), remoteActualDir, "and what was found instead")
}

// TestActiveRemoteWorkdir_ProbedDirIsReusedRatherThanProbedAgain bounds what
// the fallback costs a host it applies to: one probe, not one per refresh.
func TestActiveRemoteWorkdir_ProbedDirIsReusedRatherThanProbedAgain(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)

	derivedFingerprint, _ := transcriptCmds(remoteDerivedDir)
	actual := remoteProjectsRoot() + "/" + remoteActualDir
	actualFingerprint, actualRead := absTranscriptCmds(actual)

	outputs := baseRemoteOutputs()
	outputs[derivedFingerprint] = nil
	outputs[probeCmd()] = []byte(actual + "/" + remoteSessionID + ".jsonl\n")
	outputs[actualFingerprint] = []byte("120 1700000000\n")
	outputs[actualRead] = []byte(remoteTranscriptBody)
	outputs["ls -1 "+shellQuotePath(actual+"/"+remoteSessionID+"/subagents")+" 2>/dev/null || true"] = []byte("")

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	for range 3 {
		_, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
		require.NoError(t, err)
	}

	probes := 0
	for _, cmd := range runner.commands {
		if cmd == probeCmd() {
			probes++
		}
	}
	assert.Equal(t, 1, probes, "the resolved directory must be remembered, not re-probed every refresh")
}

// TestActiveRemoteWorkdir_ProbeFindsNothing_IsQuietAndBounded covers the state
// a fresh Claude session is in before it has written anything: there is no
// transcript anywhere, and that must not turn every refresh into a probe.
func TestActiveRemoteWorkdir_ProbeFindsNothing_IsQuietAndBounded(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)
	freezeClock(t)
	logged := captureRemoteLog(t)

	derivedFingerprint, _ := transcriptCmds(remoteDerivedDir)
	outputs := baseRemoteOutputs()
	outputs[derivedFingerprint] = nil
	outputs[probeCmd()] = []byte("")

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	for range 3 {
		cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
		require.NoError(t, err)
		assert.Empty(t, cwds)
	}

	probes, derivedReads := 0, 0
	for _, cmd := range runner.commands {
		switch cmd {
		case probeCmd():
			probes++
		case derivedFingerprint:
			derivedReads++
		}
	}
	assert.Equal(t, 1, probes, "a session with no transcript must not pay for a probe on every refresh")
	assert.Equal(t, 3, derivedReads,
		"and it must keep looking under the derived directory, not under the nothing the probe answered")
	assert.NotContains(t, logged.String(), "may have changed",
		"nothing was found, so there is no encoding change to report")
}

// TestActiveRemoteWorkdir_ProbeAnswerIsValidatedBeforeItReachesACommand is the
// security half: the answer comes back from the remote host and is then used
// to build further commands, so it goes through validRemotePath first — the
// same regex-allowlist-before-the-sink discipline docs/security.md requires
// for every other remote path.
func TestActiveRemoteWorkdir_ProbeAnswerIsValidatedBeforeItReachesACommand(t *testing.T) {
	for _, answer := range []string{
		"/home/dev.user/.claude/projects/evil$(id)/session-123.jsonl",
		"/home/dev.user/.claude/projects/a;rm -rf ~/session-123.jsonl",
		"relative/path/session-123.jsonl",
		"/home/dev.user/.claude/projects/-a-b/other-session.jsonl",
	} {
		t.Run(answer, func(t *testing.T) {
			forgetRemoteClaudeProjectDirs(t)
			derivedFingerprint, _ := transcriptCmds(remoteDerivedDir)

			outputs := baseRemoteOutputs()
			outputs[derivedFingerprint] = nil
			outputs[probeCmd()] = []byte(answer + "\n")

			runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

			cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)

			require.NoError(t, err)
			assert.Empty(t, cwds)
			for _, cmd := range runner.commands {
				assert.NotContains(t, cmd, "evil")
				assert.NotContains(t, cmd, "rm -rf")
				assert.NotContains(t, cmd, "other-session")
			}
		})
	}
}

// TestActiveRemoteWorkdir_ProbedDirAlsoResolvesSubagentTranscripts is why the
// probe resolves the project *directory* rather than just the parent file: a
// delegated subagent's worktree lives beside it, and the derived directory is
// wrong for both.
func TestActiveRemoteWorkdir_ProbedDirAlsoResolvesSubagentTranscripts(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)

	derivedFingerprint, _ := transcriptCmds(remoteDerivedDir)
	actual := remoteProjectsRoot() + "/" + remoteActualDir
	actualFingerprint, actualRead := absTranscriptCmds(actual)
	subagentDir := actual + "/" + remoteSessionID + "/subagents"
	subagentPath := subagentDir + "/agent-1.jsonl"

	outputs := baseRemoteOutputs()
	outputs[derivedFingerprint] = nil
	outputs[probeCmd()] = []byte(actual + "/" + remoteSessionID + ".jsonl\n")
	outputs[actualFingerprint] = []byte("120 1700000000\n")
	outputs[actualRead] = []byte(remoteTranscriptBody)
	outputs["ls -1 "+shellQuotePath(subagentDir)+" 2>/dev/null || true"] = []byte("agent-1.jsonl\n")
	outputs[remoteFileFingerprintCmd(shellQuotePath(subagentPath))] = []byte("90 1700000001\n")
	outputs["cat "+shellQuotePath(subagentPath)] = []byte(
		`{"type":"assistant","cwd":"/home/dev.user/my.project/subagent-worktree",` +
			`"message":{"content":[{"type":"text","text":"ok"}]}}` + "\n",
	)

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"/home/dev.user/my.project/worktree",
		"/home/dev.user/my.project/subagent-worktree",
	}, cwds)
}

func TestRemoteClaudeProjectProbeCmd_QuotesTheSessionIDAndGlobsOnlyOneLevel(t *testing.T) {
	cmd := remoteClaudeProjectProbeCmd(remoteSessionID)

	assert.Contains(t, cmd, "~/.claude/projects/*/"+shellQuotePath(remoteSessionID+".jsonl"))
	assert.Contains(t, cmd, "head -n 1", "the answer is bounded to one line")
	assert.NotContains(t, cmd, "**", "one level down, not a recursive walk")
	assert.True(t, strings.HasPrefix(cmd, "ls -1 "), "cmd=%q", cmd)
}

// freezeClock pins nowFn so a test decides when the probe's answer goes stale
// rather than the wall clock deciding for it.
func freezeClock(t *testing.T) *fakeClock {
	t.Helper()
	clock := newFakeClock()
	orig := nowFn
	nowFn = clock.now
	t.Cleanup(func() { nowFn = orig })
	return clock
}

// flakyProbeRunner fails the probe command its first n times, so a test can
// separate "the host said there is nothing" from "the question never arrived".
type flakyProbeRunner struct {
	recordingSSHRunner
	failProbes int
}

func (f *flakyProbeRunner) Output(cmd string) ([]byte, error) {
	if cmd == probeCmd() && f.failProbes > 0 {
		f.failProbes--
		f.commands = append(f.commands, cmd)
		return nil, errors.New("ssh: exec channel closed")
	}
	return f.recordingSSHRunner.Output(cmd)
}

func probedOutputs(t *testing.T) map[string][]byte {
	t.Helper()
	actual := remoteProjectsRoot() + "/" + remoteActualDir
	actualFingerprint, actualRead := absTranscriptCmds(actual)
	derivedFingerprint, _ := transcriptCmds(remoteDerivedDir)

	outputs := baseRemoteOutputs()
	outputs[derivedFingerprint] = nil
	outputs[probeCmd()] = []byte(actual + "/" + remoteSessionID + ".jsonl\n")
	outputs[actualFingerprint] = []byte("120 1700000000\n")
	outputs[actualRead] = []byte(remoteTranscriptBody)
	outputs["ls -1 "+shellQuotePath(actual+"/"+remoteSessionID+"/subagents")+" 2>/dev/null || true"] = []byte("")
	return outputs
}

// TestActiveRemoteWorkdir_ProbeError_IsNotRememberedAsAnAnswer: a dropped exec
// channel is not the host saying "there is nothing here". Recording it as one
// retired the probe for the rest of the process's life, so a single network
// blip during the first refresh cost the pane its worktree permanently.
func TestActiveRemoteWorkdir_ProbeError_IsNotRememberedAsAnAnswer(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)
	freezeClock(t)

	runner := &flakyProbeRunner{
		recordingSSHRunner: recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: probedOutputs(t)}},
		failProbes:         1,
	}

	// The refresh that hits the failure learns nothing...
	cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
	require.NoError(t, err)
	assert.Empty(t, cwds)

	// ...and the next one asks again, without the clock having moved.
	cwds, err = activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/home/dev.user/my.project/worktree"}, cwds)
}

// TestActiveRemoteWorkdir_NothingFoundYet_IsRetriedOnceItGoesStale covers the
// ordinary startup order: the session metadata exists as soon as the remote
// claude process does, but its transcript only appears once the session
// produces output. The first refresh lands in that window, and a permanent
// negative answer would mean the pane never resolves its worktree afterwards.
func TestActiveRemoteWorkdir_NothingFoundYet_IsRetriedOnceItGoesStale(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)
	clock := freezeClock(t)

	outputs := probedOutputs(t)
	appeared := outputs[probeCmd()]
	outputs[probeCmd()] = []byte("") // nothing written yet

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
	require.NoError(t, err)
	assert.Empty(t, cwds)

	// The transcript appears a moment later, but the answer is still fresh.
	outputs[probeCmd()] = appeared
	cwds, err = activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
	require.NoError(t, err)
	assert.Empty(t, cwds, "a fresh answer is reused rather than re-asked")

	clock.advance(remoteClaudeProbeTTL + time.Second)

	cwds, err = activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/home/dev.user/my.project/worktree"}, cwds,
		"once the answer is stale the host is asked again")
}

// TestActiveRemoteWorkdir_ProbeFindsTheDerivedDirectory_IsNotAnEncodingChange
// is the comparison the caller makes after a probe. The probe runs whenever
// nothing was resolved, which includes a transcript that simply has no cwd in
// it yet — and then it finds the file exactly where panemux already looked.
func TestActiveRemoteWorkdir_ProbeFindsTheDerivedDirectory_IsNotAnEncodingChange(t *testing.T) {
	forgetRemoteClaudeProjectDirs(t)
	freezeClock(t)
	logged := captureRemoteLog(t)

	derivedFingerprint, derivedRead := transcriptCmds(remoteDerivedDir)
	derivedAbs := remoteProjectsRoot() + "/" + remoteDerivedDir

	outputs := baseRemoteOutputs()
	outputs[derivedFingerprint] = []byte("40 1700000000\n")
	// A transcript that exists but carries no working directory yet.
	outputs[derivedRead] = []byte(`{"type":"summary","summary":"starting"}` + "\n")
	outputs["ls -1 "+remoteClaudeTranscriptShellPath(
		remoteDerivedDir+"/"+remoteSessionID+"/subagents", "",
	)+" 2>/dev/null || true"] = []byte("")
	outputs[probeCmd()] = []byte(derivedAbs + "/" + remoteSessionID + ".jsonl\n")

	runner := &recordingSSHRunner{fakeSSHRunner: fakeSSHRunner{outputs: outputs}}

	cwds, err := activeRemoteWorkdirs(runner, "test remote claude", "/repo/main", 100)

	require.NoError(t, err)
	assert.Empty(t, cwds)
	assert.NotContains(t, logged.String(), "may have changed",
		"the transcript is where panemux looked, so nothing about the encoding changed")

	absFingerprint, absRead := absTranscriptCmds(derivedAbs)
	assert.NotContains(t, runner.commands, absFingerprint,
		"the same directory must not be re-read under an absolute spelling of itself")
	assert.NotContains(t, runner.commands, absRead)
}
