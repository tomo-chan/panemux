package agmsg

import (
	"fmt"
	"strings"
)

const (
	boardModeMonitor = "monitor"
	boardModeTurn    = "turn"
	boardModeBoth    = "both"
)

// runsAgmsgMonitor reports whether a Monitor-based watcher is running for a
// pane in this mode, and therefore whether re-arming one is the right move.
// turn delivers through a Stop hook with no watcher to replace, and off
// means the operator asked for no automatic delivery at all — starting one
// would contradict the setting.
func runsAgmsgMonitor(mode string) bool {
	return mode == "" || mode == boardModeMonitor || mode == boardModeBoth
}

// buildBootstrapInstruction builds the one-time onboarding instruction
// written into a pane's PTY. It tells the agent to invoke agmsg's own
// scripts (join.sh/actas-claim.sh/watch.sh/send.sh/delivery.sh) directly
// with their verified positional-argument signatures, rather than agmsg's
// slash-command shorthand (`/agmsg ...` or `$agmsg ...` depending on agent
// type — the prefix itself differs per agmsg's own per-type `cmd_prefix`
// driver setting, so hardcoding either one would silently be wrong for the
// other half of agmsg's supported agent types). agmsgType is the
// agmsg-recognized type string the adapter resolved (e.g.
// "claude-code", "gemini") and is passed as each script's own `<type>`
// argument. mode is the pane's agent_board.mode config value ("" is treated
// the same as "monitor": agmsg's own default already behaves like monitor
// without any explicit delivery.sh call).
//
// Step numbers are generated rather than written literally because the
// exclusivity step is the agent's board adapter's to write, and most write
// none — see docs/agent-board.md's "Two panes in one project directory".
func buildBootstrapInstruction(agmsgPath, team, paneID, agmsgType, mode string) string {
	var b strings.Builder
	step := 0
	nextStep := func() int { step++; return step }
	b.WriteString("Please set up Agent Board messaging for this session using agmsg's own scripts\n")
	b.WriteString("directly (not the /agmsg or $agmsg slash-command, since its exact prefix differs\n")
	b.WriteString("by agent type):\n")
	fmt.Fprintf(&b, "%d. Join the agmsg team %q by running:\n", nextStep(), team)
	fmt.Fprintf(&b, "   %s/scripts/join.sh %q %q %q \"$(pwd)\" --force\n", agmsgPath, team, paneID, agmsgType)
	b.WriteString("   Use exactly that agent_id — not a name of your own choosing — since every cross-pane\n")
	b.WriteString("   and relay address in Agent Board assumes from/to values are pane IDs.\n")
	boardAdapterFor(agmsgType).writeIdentityClaim(&b, boardJoin{
		agmsgPath: agmsgPath, paneID: paneID, agmsgType: agmsgType, mode: mode,
	}, nextStep)
	fmt.Fprintf(&b, "%d. From now on, send every board-related message (status reports and messages\n", nextStep())
	b.WriteString("   to other panes) with:\n")
	fmt.Fprintf(&b, "   %s/scripts/send.sh %q %q \"<to>\" \"<body>\" --force\n", agmsgPath, team, paneID)
	fmt.Fprintf(&b, "%d. Self-report your status to \"_system\" using that same send.sh invocation,\n", nextStep())
	b.WriteString("   with a JSON body shaped exactly like:\n")
	b.WriteString("   {\"kind\":\"board_status\",\"state\":\"working|idle|waiting\",\"cwd\":\"...\",\"branch\":\"...\",\n")
	b.WriteString("   \"repo\":\"...\",\"pr_url\":\"...\",\"last_tool\":\"...\",\"summary\":\"...\"}\n")
	b.WriteString("   Omit fields you don't currently have.\n")
	b.WriteString("   \"summary\" is the field a person reads to tell your pane apart from every other\n")
	b.WriteString("   pane on the board, so always include it: one short sentence, in plain language,\n")
	b.WriteString("   saying what you are working on right now (e.g. \"Fixing the flaky relay test\").\n")
	b.WriteString("   Not your last tool call, not a summary of the whole session. When you are\n")
	b.WriteString("   blocked, say what you are blocked on.\n")
	b.WriteString("   Send an update when starting a new task, when finishing one, and whenever you\n")
	b.WriteString("   become blocked or go idle — not on a timer.\n")
	if mode == boardModeTurn || mode == boardModeBoth {
		fmt.Fprintf(&b, "%d. Also run:\n", nextStep())
		fmt.Fprintf(&b, "   %s/scripts/delivery.sh set %q %q \"$(pwd)\"\n", agmsgPath, mode, agmsgType)
		b.WriteString("   This prints an AGMSG-DIRECTIVE: block — read it and follow its instructions exactly;\n")
		b.WriteString("   it configures how you receive incoming board messages from now on.\n")
	}
	return b.String()
}
