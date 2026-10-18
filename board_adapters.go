package main

import (
	"fmt"
	"strings"

	"panemux/internal/agent"
)

// boardAdapters is every agent that can join Agent Board, each registered
// under the registry ID whose Board capability it implements. Nothing
// registers itself. What every agent shares — joining, sending, the status
// report and the delivery mode — is buildBootstrapInstruction's; an adapter
// supplies only what differs per agent. The agents are found in a pane by
// the registry's Board rules, which mirror agmsg's own detection and so stay
// apart from the other rules; the capability depends on agmsg being present
// on the pane's host, which the watcher checks before writing anything.
// TestBoardAdapters_MatchTheRegistry keeps the list and the registry's
// declarations in step.
func boardAdapters() []registeredBoard {
	return []registeredBoard{
		{id: agent.Claude, adapter: claudeBoard{}},
		{id: agent.Codex, adapter: plainBoard{}},
		{id: agent.Cursor, adapter: plainBoard{}},
		{id: agent.Gemini, adapter: plainBoard{}},
		{id: agent.GrokBuild, adapter: plainBoard{}},
		{id: agent.OpenCode, adapter: plainBoard{}},
	}
}

type registeredBoard struct {
	adapter boardAdapter
	id      agent.ID
}

// boardAdapter is what joining Agent Board differs in per agent.
type boardAdapter interface {
	// writeIdentityClaim writes the steps that make the pane's agmsg
	// identity exclusive to this session, numbering each with nextStep, or
	// writes nothing when the agent has no verified way to name its session.
	writeIdentityClaim(b *strings.Builder, join boardJoin, nextStep func() int)
}

// boardJoin is what an adapter's steps are written from.
type boardJoin struct {
	agmsgPath string
	paneID    string
	agmsgType string
	// mode is the pane's agent_board.mode, "" meaning monitor.
	mode string
}

// boardAdaptersByType is boardAdapters keyed by agmsg type, the name a pane's
// detection reports, checked against the registry when panemux starts.
var boardAdaptersByType = mustBoardAdapters(agent.All(), boardAdapters())

func mustBoardAdapters(defs []agent.Definition, adapters []registeredBoard) map[string]boardAdapter {
	//coverage:exempt the registered set is checked by TestBoardAdapters_MatchTheRegistry
	if err := validateBoardAdapters(defs, adapters); err != nil {
		panic(err)
	}
	types := map[agent.ID]string{}
	for _, d := range defs {
		types[d.ID] = d.AgmsgType
	}
	byType := make(map[string]boardAdapter, len(adapters))
	for _, r := range adapters {
		byType[types[r.id]] = r.adapter
	}
	return byType
}

// boardAdapterFor is the adapter of the agent agmsgType names. Detection
// reports only registered types, so the fallback — the steps every agent
// shares and no claim — is never reached in practice.
func boardAdapterFor(agmsgType string) boardAdapter {
	if a, ok := boardAdaptersByType[agmsgType]; ok {
		return a
	}
	return plainBoard{}
}

// validateBoardAdapters checks adapters against the registry: each is
// registered once, under an agent that declares board, and every agent that
// declares board has one.
func validateBoardAdapters(defs []agent.Definition, adapters []registeredBoard) error {
	known := map[agent.ID]agent.Definition{}
	for _, d := range defs {
		known[d.ID] = d
	}
	seen := map[agent.ID]bool{}
	for _, r := range adapters {
		d, ok := known[r.id]
		switch {
		case !ok:
			return fmt.Errorf("board adapter %q is not registered in the agent registry", r.id)
		case !d.Has(agent.Board):
			return fmt.Errorf("board adapter %q does not declare %s in the agent registry", r.id, agent.Board)
		case seen[r.id]:
			return fmt.Errorf("board adapter %q is registered twice", r.id)
		}
		seen[r.id] = true
	}
	for _, d := range defs {
		if d.Has(agent.Board) && !seen[d.ID] {
			return fmt.Errorf("agent %q declares board but has no board adapter", d.ID)
		}
	}
	return nil
}

// plainBoard is an agent that joins with the shared steps alone. agmsg's
// per-type type.conf `detect` key names a session-id source for some of
// them (CODEX_THREAD_ID for codex) and none for others (opencode, cursor),
// but none is verified, and guessing one wrong would hand actas-claim.sh an
// empty session id, so none of them claims its identity.
type plainBoard struct{}

func (plainBoard) writeIdentityClaim(*strings.Builder, boardJoin, func() int) {}

// claudeBoard is Claude Code, the one agent whose session-id source panemux
// has verified.
type claudeBoard struct{}

// claudeCodeSessionIDVar is Claude Code's own session-id environment
// variable, taken from agmsg's type.conf rather than assumed.
const claudeCodeSessionIDVar = "$CLAUDE_CODE_SESSION_ID"

// writeIdentityClaim emits the step that makes this pane's agmsg identity
// exclusive to this process, and (when a watcher is running) narrows that
// watcher to it.
//
// Without it, two board-enabled panes whose agents run in the SAME project
// directory each receive the other's messages. Verified in agmsg's own
// source (v1.2.0, unchanged in v1.2.2): scripts/watch.sh resolves its
// subscription through identities.sh <project> <type>, which returns every
// (team, agent) pair registered for that project and type — both pane IDs.
// Handed no 4th <agent> argument the watcher subscribes broadly, dropping
// only pairs another live session already holds an actas lock on and
// claiming none itself. agmsg's scripts/lib/actas-lock.sh states the same
// failure in its own words, and its claude-code template calls
// actas-claim.sh directly after join.sh for exactly this reason. The call
// order and the argument shapes here are that template's, not invented.
//
// The claim also covers turn mode, where there is no watcher at all:
// check-inbox.sh resolves its pairs through the same subscription helper.
// The watcher is replaced with Claude Code's own TaskList, TaskStop and
// Monitor tools.
func (claudeBoard) writeIdentityClaim(b *strings.Builder, join boardJoin, nextStep func() int) {
	fmt.Fprintf(b, "%d. Claim that identity for this process, so a second agent running in the same\n", nextStep())
	b.WriteString("   directory does not also receive messages addressed to this pane:\n")
	fmt.Fprintf(b, "   %s/scripts/actas-claim.sh \"$(pwd)\" %q %q %q\n",
		join.agmsgPath, join.agmsgType, join.paneID, claudeCodeSessionIDVar)
	b.WriteString("   Expect \"status=ok\". If it prints \"status=held\", another live session already owns\n")
	b.WriteString("   this pane ID — say so and stop here; do not touch the running inbox stream.\n")
	if !runsAgmsgMonitor(join.mode) {
		return
	}
	b.WriteString("   Then replace your inbox stream so it only receives messages addressed to this\n")
	b.WriteString("   pane, since the one already running was started without that restriction:\n")
	b.WriteString("   - Run TaskList. If a task's description starts with \"agmsg inbox stream\",\n")
	b.WriteString("     TaskStop it. If there is none, skip this — do not guess a task id.\n")
	b.WriteString("   - Invoke the Monitor tool with persistent: true, description\n")
	fmt.Fprintf(b, "     \"agmsg inbox stream (acting as %s)\", and command:\n", join.paneID)
	fmt.Fprintf(b, "     %s/scripts/watch.sh %q \"$(pwd)\" %q %q\n",
		join.agmsgPath, claudeCodeSessionIDVar, join.agmsgType, join.paneID)
}
