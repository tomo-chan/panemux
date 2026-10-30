package agmsg

import (
	"cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"panemux/internal/agent"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBootstrapInstruction_ModeMonitorOrEmpty_NoDeliveryStep(t *testing.T) {
	for _, mode := range []string{"", "monitor", "off"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", mode)
		assert.NotContains(t, text, "delivery.sh", "mode %q must not include a delivery.sh step", mode)
	}
}

func TestBuildBootstrapInstruction_ModeTurnOrBoth_IncludesDeliveryStep(t *testing.T) {
	for _, mode := range []string{"turn", "both"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "gemini", mode)
		want := `/opt/agmsg/scripts/delivery.sh set "` + mode + `" "gemini" "$(pwd)"`
		assert.Contains(t, text, want, "mode %q must include its own delivery.sh invocation", mode)
	}
}

func TestBuildBootstrapInstruction_ContainsVerifiedScriptInvocations(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "codex", "both")

	assert.Contains(t, text, `/opt/agmsg/scripts/join.sh "panemux" "pane-a" "codex" "$(pwd)" --force`)
	assert.Contains(t, text, `/opt/agmsg/scripts/send.sh "panemux" "pane-a" "<to>" "<body>" --force`)
	assert.Contains(t, text, `/opt/agmsg/scripts/delivery.sh set "both" "codex" "$(pwd)"`)
	assert.Contains(t, text, "AGMSG-DIRECTIVE")
	// The intro sentence explains why slash-command shorthand is avoided, so
	// it legitimately names both prefixes once; no *line* other than that
	// intro sentence may actually invoke either shorthand form.
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "slash-command") {
			continue
		}
		assert.False(t, strings.HasPrefix(trimmed, "/agmsg "),
			"line invokes claude-code-only slash-command shorthand: %q", trimmed)
		assert.False(t, strings.HasPrefix(trimmed, "$agmsg "),
			"line invokes codex/gemini-style slash-command shorthand: %q", trimmed)
	}
}

// TestBuildBootstrapInstruction_DefinesWhatASummaryShouldSay covers the
// half of "show what each pane is doing" that no amount of dashboard work
// can fix: the dashboard can only render a summary the agent chose to
// write. The instruction used to list "summary" as one field name among
// eight with no guidance at all, and "send an update whenever your state
// changes meaningfully" left both the content and the cadence to the
// agent's discretion — which is how a board of many panes ends up with
// state pills and nothing to read.
func TestBuildBootstrapInstruction_DefinesWhatASummaryShouldSay(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", "monitor")

	// The summary must be described as the current task in the operator's
	// terms, not left as a bare field name.
	assert.Contains(t, text, "summary")
	assert.Contains(t, text, "one short sentence")
	assert.Contains(t, text, "what you are working on right now")

	// A cadence the agent can actually act on, rather than "meaningfully".
	assert.Contains(t, text, "starting a new task")
	assert.Contains(t, text, "finishing one")
	assert.Contains(t, text, "blocked")
}

// The instruction is written into a PTY and read by a person as often as by
// an agent, so its prose must not grow into a wall. Command lines are
// exempt: they carry an operator-configured agmsg path and a generated pane
// ID, so their length is not something this file can choose, and wrapping
// one would make it uncopyable.
func TestBuildBootstrapInstruction_StaysReadableInATerminal(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", "both")

	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "/scripts/") {
			continue
		}
		assert.LessOrEqual(t, len(line), 100, "line is too long to read in a pane: %q", line)
	}
}

// TestBuildBootstrapInstruction_ClaimsItsOwnIdentity covers the collision
// two board-enabled panes in the SAME project directory otherwise have.
//
// Verified against agmsg's own source (v1.2.0, the pinned tested version,
// and unchanged in v1.2.2): scripts/watch.sh resolves its subscription with
// identities.sh <project> <type>, which returns EVERY (team, agent) pair
// registered for that project and type — both pane IDs. Without a 4th
// <agent> argument the watcher does a broad subscribe: it drops only pairs
// another live session already holds an actas lock on, and claims nothing
// itself. Two panes in one repo therefore each receive messages addressed
// to the other. agmsg's own scripts/lib/actas-lock.sh names this exact
// failure ("every concurrent CC session in that project would subscribe to
// every registered identity's messages"), and scripts/actas-claim.sh is the
// remedy its claude-code template calls right after join.sh.
func TestBuildBootstrapInstruction_ClaimsItsOwnIdentity(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", "monitor")

	want := `/opt/agmsg/scripts/actas-claim.sh "$(pwd)" "claude-code" "pane-a" "$CLAUDE_CODE_SESSION_ID"`
	assert.Contains(t, text, want)
	// status=held is the case that must not be silently ignored: another
	// live session owns this pane ID, and the template's own rule is to
	// stop rather than disturb the running Monitor.
	assert.Contains(t, text, "status=held")
}

// The session-id source is per agent type — agmsg's own type.conf `detect`
// key is CLAUDE_CODE_SESSION_ID for claude-code, CODEX_THREAD_ID for codex,
// and absent entirely for opencode and cursor. panemux has only verified
// the claude-code invocation against agmsg's own template, so the step is
// emitted for that type alone rather than guessing an env var for the rest.
func TestBuildBootstrapInstruction_OmitsClaimForUnverifiedTypes(t *testing.T) {
	for _, agentType := range []string{"codex", "gemini", "opencode", "cursor"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", agentType, "monitor")
		assert.NotContains(t, text, "actas-claim.sh",
			"type %q has no verified session-id source", agentType)
		assert.NotContains(t, text, "CLAUDE_CODE_SESSION_ID",
			"type %q must not be handed claude-code's own env var", agentType)
	}
}

// Claiming the lock is not enough on its own: the watcher this session is
// already running was launched with no <agent> argument, so it keeps its
// broad subscription until it is replaced. watch.sh's 4th argument is what
// narrows it, and re-claims the lock.
func TestBuildBootstrapInstruction_RearmsTheMonitorForItsOwnPaneOnly(t *testing.T) {
	for _, mode := range []string{"", "monitor", "both"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", mode)
		want := `/opt/agmsg/scripts/watch.sh "$CLAUDE_CODE_SESSION_ID" "$(pwd)" "claude-code" "pane-a"`
		assert.Contains(t, text, want, "mode %q runs a Monitor and must re-arm it", mode)
		assert.Contains(t, text, "agmsg inbox stream", "mode %q must name the task to stop", mode)
	}
}

// turn mode has no Monitor to re-arm (delivery is a Stop hook), and off
// means the operator wants no automatic delivery at all — starting one
// would contradict the setting. Both still claim the lock, which is what
// check-inbox.sh's own subscription filtering reads.
func TestBuildBootstrapInstruction_DoesNotStartAMonitorForTurnOrOff(t *testing.T) {
	for _, mode := range []string{"turn", "off"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", mode)
		assert.NotContains(t, text, "watch.sh", "mode %q must not launch a watcher", mode)
		assert.Contains(t, text, "actas-claim.sh", "mode %q still needs the exclusivity claim", mode)
	}
}

// The onboarding instruction every Board agent is sent, in every mode, is
// fixed in testdata/bootstrap-instructions: moving the per-agent steps into
// board adapters (issue #375) changed none of them. Set
// PANEMUX_UPDATE_BOOTSTRAP_GOLDEN=1 to rewrite the files after a deliberate
// change, and read the diff.
//
//efficacy:exempt characterizes the instruction text the board-adapter refactor must not change
func TestBuildBootstrapInstruction_Golden(t *testing.T) {
	update := os.Getenv("PANEMUX_UPDATE_BOOTSTRAP_GOLDEN") == "1"
	for _, d := range agent.All() {
		if !d.Has(agent.Board) {
			continue
		}
		for _, mode := range []string{"", boardModeMonitor, boardModeTurn, boardModeBoth, "off"} {
			name := typeForAgent(d.ID) + "-" + cmp.Or(mode, "default") + ".txt"
			t.Run(name, func(t *testing.T) {
				got := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", typeForAgent(d.ID), mode)
				path := filepath.Join("testdata", "bootstrap-instructions", name)
				if update {
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
					require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
				}
				want, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, string(want), got)
			})
		}
	}
}

// Every agent that declares board has exactly one board adapter, and no
// other agent has one.
func TestBoardAdapters_MatchTheRegistry(t *testing.T) {
	require.NoError(t, validateBoardAdapters(agent.All(), boardAdapters()))

	var ids []agent.ID
	for _, r := range boardAdapters() {
		ids = append(ids, r.id)
	}
	var want []agent.ID
	for _, d := range agent.All() {
		if d.Has(agent.Board) {
			want = append(want, d.ID)
		}
	}
	assert.ElementsMatch(t, want, ids)
}

func TestValidateBoardAdapters_RefusesASetThatDoesNotMatch(t *testing.T) {
	defs := []agent.Definition{
		{ID: "one", Capabilities: []agent.Capability{agent.Detect, agent.Board}},
		{ID: "two", Capabilities: []agent.Capability{agent.Detect}},
	}
	cases := []struct {
		name     string
		wantErr  string
		adapters []registeredBoard
	}{
		{
			name: "an agent that is not registered",
			adapters: []registeredBoard{{id: "one", agmsgType: "one", adapter: plainBoard{}},
				{id: "three", agmsgType: "three", adapter: plainBoard{}}},
			wantErr: `board adapter "three" is not registered in the agent registry`,
		},
		{
			name: "an agent that does not declare board",
			adapters: []registeredBoard{{id: "one", agmsgType: "one", adapter: plainBoard{}},
				{id: "two", agmsgType: "two", adapter: plainBoard{}}},
			wantErr: `board adapter "two" does not declare board in the agent registry`,
		},
		{
			name: "an agent registered twice",
			adapters: []registeredBoard{{id: "one", agmsgType: "one", adapter: plainBoard{}},
				{id: "one", agmsgType: "one", adapter: plainBoard{}}},
			wantErr: `board adapter "one" is registered twice`,
		},
		{
			name:    "an agent with no adapter",
			wantErr: `agent "one" declares board but has no board adapter`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.EqualError(t, validateBoardAdapters(defs, tc.adapters), tc.wantErr)
		})
	}
	assert.NoError(t, validateBoardAdapters(defs, []registeredBoard{{id: "one", agmsgType: "one", adapter: plainBoard{}}}))
}

// Which agent claims its identity is the adapter's to say: only Claude
// Code's session-id source is verified.
func TestBoardAdapters_OnlyClaudeClaimsItsIdentity(t *testing.T) {
	for _, r := range boardAdapters() {
		_, claims := r.adapter.(claudeBoard)
		assert.Equal(t, r.id == agent.Claude, claims, "agent %s", r.id)
	}
}

// A type no adapter is registered for gets the shared steps and no claim.
func TestBoardAdapterFor_AnUnregisteredTypeClaimsNothing(t *testing.T) {
	assert.Equal(t, plainBoard{}, boardAdapterFor("not-a-type"))
	assert.Equal(t, claudeBoard{}, boardAdapterFor("claude-code"))
}

func typeForAgent(id agent.ID) string {
	for _, entry := range boardAdapters() {
		if entry.id == id {
			return entry.agmsgType
		}
	}
	return ""
}

func TestBoardAdapterTypesRejectMissingInvalidAndDuplicateNames(t *testing.T) {
	defs := []agent.Definition{{ID: "one", Capabilities: []agent.Capability{agent.Board}},
		{ID: "two", Capabilities: []agent.Capability{agent.Board}}}
	for _, types := range [][2]string{{"", "two"}, {"UPPER", "two"}, {"same", "same"}, {"one", "bad name"}} {
		t.Run(types[0]+"/"+types[1], func(t *testing.T) {
			entries := []registeredBoard{{id: "one", agmsgType: types[0], adapter: plainBoard{}},
				{id: "two", agmsgType: types[1], adapter: plainBoard{}}}
			require.ErrorContains(t, validateBoardAdapters(defs, entries), "invalid or duplicate agmsg type")
		})
	}
	require.NoError(t, validateBoardAdapters(defs, []registeredBoard{{id: "one", agmsgType: "one", adapter: plainBoard{}},
		{id: "two", agmsgType: "two", adapter: plainBoard{}}}))
}
