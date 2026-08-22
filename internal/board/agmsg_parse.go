package board

import (
	"bytes"
	"encoding/json"
	"time"
)

// agmsgMessageRow is the JSONL shape api.sh's "messages" verb emits per
// line: {"type":"message_sent","id","team","from","to","body","at"}. See
// docs/agent-board.md's Integration with agmsg section.
type agmsgMessageRow struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Team string `json:"team"`
	From string `json:"from"`
	To   string `json:"to"`
	Body string `json:"body"`
	At   string `json:"at"`
}

const agmsgMessageSentType = "message_sent"

// parseAgmsgMessageRows decodes JSONL output from `api.sh get teams <team>
// messages`, tagging every row with host (which AgmsgClient/host it came
// from — not part of agmsg's own schema). A line that isn't valid JSON, or
// doesn't parse as a message_sent record, is skipped rather than failing
// the whole poll — matching this repository's existing JSONL-tolerance
// convention (see internal/session/local.go's forEachJSONLRecord).
func parseAgmsgMessageRows(data []byte, host string) []Row {
	var rows []Row
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var raw agmsgMessageRow
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw.Type != agmsgMessageSentType {
			continue
		}
		at, _ := time.Parse(time.RFC3339, raw.At)
		rows = append(rows, Row{
			ID:   raw.ID,
			Host: host,
			Team: raw.Team,
			From: raw.From,
			To:   raw.To,
			Body: raw.Body,
			At:   at,
		})
	}
	return rows
}

// filterRowsAfter returns the rows that follow afterID in the order api.sh
// itself returned them.
//
// The cursor is an OPAQUE token, matched by identity and never compared.
// That is agmsg's own contract, not caution on panemux's part: api.sh's
// `messages` query documents its id column as TEXT ("the driver-interface
// spec treats every message id as opaque"), and orders rows by each
// storage source's native counter — `events.seq` or `messages.id` — a value
// its own comment says is "never compared across sources". The legacy sqlite
// driver exposes integer rowids as decimal strings while the event-log
// driver emits UUIDv7, and a single response can UNION both. Comparing ids
// is therefore wrong for two independent reasons, and panemux used to do it
// numerically: every UUID failed to parse, so no cursor ever advanced and
// the relay re-delivered its whole poll window on every tick. Ordering by
// the response's own order is the only signal agmsg actually offers.
//
// afterID is anchored on its LAST occurrence, since ids are host-scoped and
// carry no global-uniqueness promise; anchoring on the newest occurrence
// never re-delivers a row an earlier poll already handled.
//
// Two cases yield every row: an afterID that is empty (no cursor yet, i.e.
// cold start) and one that is absent from this response. Absent means the
// cursor scrolled out of the poll window — more than --limit new rows since
// the last poll, or a reset store — and agmsg has no forward "since"
// primitive to resolve it with (only backwards --before-id pagination), so
// the window is re-delivered rather than skipped. That matches the
// at-least-once delivery this design already documents, and self-corrects
// on the next poll.
func filterRowsAfter(rows []Row, afterID string) []Row {
	if afterID == "" {
		return rows
	}
	anchor := -1
	for i, r := range rows {
		if r.ID == afterID {
			anchor = i
		}
	}
	if anchor < 0 {
		return rows
	}
	return rows[anchor+1:]
}
