// Package board implements the Agent Board messaging/status/relay core: a
// thin, panemux-owned layer on top of an operator-installed agmsg
// (https://github.com/fujibee/agmsg) instance per host. See
// docs/agent-board.md for the full design; panemux owns no message schema
// or storage of its own here — see that document's "Design principles".
package board

import "time"

// PanemuxSentinel is the reserved agmsg identity used for status reports
// and every message panemux itself originates (broadcast handler, and in a
// later phase, the command center). It is never a real agmsg roster member.
const PanemuxSentinel = "_panemux"

// Row is a single agmsg message row, normalized into panemux's own
// representation regardless of which host it came from.
type Row struct {
	// ID is agmsg's own id for this row. It is host-scoped, not globally
	// unique, and must not be assumed to stay numeric forever (agmsg's own
	// source comments describe this as future-proofing against a
	// non-integer ID scheme).
	ID string
	// Host identifies which AgmsgClient/host this row came from; required
	// to compare or sort rows across hosts, since agmsg IDs from different
	// hosts are not comparable or even guaranteed non-colliding.
	Host string
	Team string
	From string
	To   string
	Body string
	At   time.Time
}

// Status is a pane's self-reported state, decoded from a board_status
// message body addressed to PanemuxSentinel. See ParseStatus.
type Status struct {
	State    string
	CWD      string
	Branch   string
	Repo     string
	PRURL    string
	LastTool string
	Summary  string
	// UpdatedAt is when this status was recorded by BoardCache, so
	// staleness is visible to the dashboard. It is not part of the
	// self-reported JSON body.
	UpdatedAt time.Time
}
