package board

import "context"

// AgmsgClient is the only abstraction in panemux allowed to call agmsg's
// scripts (scripts/api.sh for reads, scripts/send.sh for writes). See
// docs/agent-board.md's "Integration with agmsg" and "Package layout".
type AgmsgClient interface {
	// HostID identifies which host this client talks to: "local" for the
	// host panemux itself runs on, or the SSH connection name for a remote
	// host.
	HostID() string

	// Send always passes --force. There is no non-forced Send: every
	// board-originated message (self-reported status, cross-host relay,
	// and in a later phase the command center) needs to reach a to/from
	// identity that is not guaranteed to be in the destination team's
	// roster — see docs/agent-board.md's "Integration with agmsg".
	Send(ctx context.Context, team, from, to, body string) error

	// Since has no true "after" primitive to call into (api.sh has no such
	// flag). It calls `api.sh get teams <team> messages --limit <limit>`
	// and returns only rows whose ID sorts after afterID; the caller must
	// treat the possibility of dropped rows (more than `limit` new rows
	// since the last poll) as expected, not exceptional — see
	// docs/agent-board.md's "Integration with agmsg". An empty afterID
	// returns every row within the polled limit.
	Since(ctx context.Context, team, afterID string, limit int) ([]Row, error)
}
