package board

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// agmsgMessageAtLayout is the exact format agmsg's own schema uses for a
// message row's "at" timestamp (scripts/internal/init-db.sh:
// `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`), verified against agmsg's source
// rather than assumed.
const agmsgMessageAtLayout = "2006-01-02T15:04:05Z"

// agmsgMessageRow mirrors one line of api.sh's `get teams <team> messages`
// JSONL output: {"type":"message_sent","id","team","from","to","body","at"}.
type agmsgMessageRow struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Team string `json:"team"`
	From string `json:"from"`
	To   string `json:"to"`
	Body string `json:"body"`
	At   string `json:"at"`
}

// parseMessageRows parses api.sh's JSONL output (one JSON object per line)
// into Rows tagged with the given host. Malformed lines are skipped with an
// error describing the first failure encountered, rather than silently
// dropped — a compatibility break in agmsg's output shape (see
// docs/agent-board.md's "agmsg compatibility contract") should surface as a
// mechanical failure, not a quietly empty poll.
func parseMessageRows(data []byte, host string) ([]Row, error) {
	var rows []Row
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var mr agmsgMessageRow
		if err := json.Unmarshal(line, &mr); err != nil {
			return nil, fmt.Errorf("parse agmsg message row (line %d): %w", lineNo, err)
		}
		at, err := time.Parse(agmsgMessageAtLayout, mr.At)
		if err != nil {
			// Timestamp parse failure does not invalidate the row's
			// identity/routing fields, which are what correctness-critical
			// logic (from-validation, status detection, relay routing)
			// actually depends on; keep the row with a zero time rather
			// than dropping a real message.
			at = time.Time{}
		}
		rows = append(rows, Row{
			ID:   mr.ID,
			Host: host,
			Team: mr.Team,
			From: mr.From,
			To:   mr.To,
			Body: mr.Body,
			At:   at,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan agmsg message rows: %w", err)
	}
	return rows, nil
}

// idAfter reports whether id is numerically or, failing that,
// lexicographically greater than afterID. agmsg's own id is documented as
// not guaranteed to stay a bare integer forever (see docs/agent-board.md),
// so a numeric comparison is used only when both values parse as integers
// today; any other id scheme falls back to a string comparison rather than
// panicking or silently accepting everything.
func idAfter(id, afterID string) bool {
	if afterID == "" {
		return true
	}
	if id == afterID {
		return false
	}
	idN, idErr := strconv.ParseInt(id, 10, 64)
	afterN, afterErr := strconv.ParseInt(afterID, 10, 64)
	if idErr == nil && afterErr == nil {
		return idN > afterN
	}
	return id > afterID
}

// filterAfterID returns only rows whose ID sorts after afterID, preserving
// input order.
func filterAfterID(rows []Row, afterID string) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if idAfter(r.ID, afterID) {
			out = append(out, r)
		}
	}
	return out
}
