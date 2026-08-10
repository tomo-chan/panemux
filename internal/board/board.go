// Package board implements Agent Board: cross-pane Claude/Codex messaging
// and status aggregation built entirely on agmsg (github.com/fujibee/agmsg).
// See docs/agent-board.md for the full design this package implements.
package board

import (
	"context"
	"encoding/json"
	"time"
)

// SystemID is the reserved agmsg identity panemux's own relay and command
// center use as their from/to for status reports and broadcasts. It is
// deliberately not derived from the product name so a future rename of the
// application would not need to change it. internal/config independently
// rejects this same literal as a pane ID (see its own reservedSystemID);
// the two are kept in sync by convention since internal/board must not
// import internal/config.
const SystemID = "_system"

// statusKind is the required discriminator value for a status self-report.
// Detecting a status update by JSON shape alone ("does this body happen to
// have a state field") has a real false-positive edge — see
// docs/agent-board.md's Status self-report section — so a literal
// "kind": "board_status" is required, not merely permitted.
const statusKind = "board_status"

// Row is a single agmsg message, either an ordinary cross-pane message or a
// status self-report addressed to SystemID.
type Row struct {
	At   time.Time
	ID   string // agmsg's own id, host-scoped — NOT globally unique, NOT assumed numeric
	Host string // which AgmsgClient/host this row came from
	Team string
	From string
	To   string
	Body string
}

// Status is a pane's self-reported state, decoded from a Row whose Body is
// a board_status JSON payload.
type Status struct {
	UpdatedAt time.Time
	State     string
	CWD       string
	Branch    string
	Repo      string
	PRURL     string
	LastTool  string
	Summary   string
}

// AgmsgClient is the only interface in panemux allowed to call agmsg's
// scripts. LocalAgmsgClient and RemoteAgmsgClient are its two
// implementations.
type AgmsgClient interface {
	HostID() string
	// Send always passes --force. There is no non-forced Send: every
	// board-originated message needs to reach a to/from identity that is
	// not guaranteed to be in the destination team's roster.
	Send(ctx context.Context, team, from, to, body string) error
	// Since has no true "after" primitive to call into (api.sh has no such
	// flag). It calls `api.sh get teams <team> messages --limit <limit>`
	// and returns only rows whose ID sorts after afterID; the caller must
	// treat the possibility of dropped rows as expected, not exceptional.
	Since(ctx context.Context, team, afterID string, limit int) ([]Row, error)
}

type statusPayload struct {
	Kind     string `json:"kind"`
	State    string `json:"state"`
	CWD      string `json:"cwd"`
	Branch   string `json:"branch"`
	Repo     string `json:"repo"`
	PRURL    string `json:"pr_url"`
	LastTool string `json:"last_tool"`
	Summary  string `json:"summary"`
}

// ParseStatus decodes body as a status self-report. It returns ok == false
// whenever body is not valid JSON, or is valid JSON but its "kind" field is
// not exactly "board_status" — including JSON that merely resembles the
// status shape (e.g. has a "state" field) by coincidence. This is
// deliberate: a human typing an ordinary chat message that happens to parse
// as JSON must never be silently swallowed into the status cache.
func ParseStatus(body string) (Status, bool) {
	var p statusPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return Status{}, false
	}
	if p.Kind != statusKind {
		return Status{}, false
	}
	return Status{
		State:    p.State,
		CWD:      p.CWD,
		Branch:   p.Branch,
		Repo:     p.Repo,
		PRURL:    p.PRURL,
		LastTool: p.LastTool,
		Summary:  p.Summary,
	}, true
}

// IsStatusRow reports whether row is a status self-report (addressed to
// SystemID with a body_status-shaped body) rather than an ordinary
// cross-pane message, returning the decoded Status when it is.
func IsStatusRow(row Row) (Status, bool) {
	if row.To != SystemID {
		return Status{}, false
	}
	return ParseStatus(row.Body)
}
