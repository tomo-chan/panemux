package board

import (
	"bytes"
	"encoding/json"
	"strconv"
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

// parseAgmsgID parses agmsg's own row id as the integer it is in today's
// implementation. agmsg's source comments describe this as future-proofing
// against a non-integer ID scheme; a row whose id doesn't parse this way is
// signaled via ok == false rather than assumed to sort anywhere in
// particular.
func parseAgmsgID(id string) (int64, bool) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// filterRowsAfter returns the rows whose id sorts numerically after
// afterID. There is no true "after" primitive in api.sh (see
// docs/agent-board.md's Integration with agmsg section) — this is the
// client-side filtering step every poll performs. An afterID that doesn't
// parse (including the empty string used before any cursor exists) means
// every row is "new". A row whose own id doesn't parse is conservatively
// kept rather than dropped, since panemux cannot safely order it.
func filterRowsAfter(rows []Row, afterID string) []Row {
	afterN, ok := parseAgmsgID(afterID)
	if !ok {
		return rows
	}
	var out []Row
	for _, r := range rows {
		n, ok := parseAgmsgID(r.ID)
		if !ok || n > afterN {
			out = append(out, r)
		}
	}
	return out
}
