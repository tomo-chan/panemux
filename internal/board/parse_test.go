package board

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These fixtures are frozen JSONL shaped to match api.sh's real, documented
// output (verified against agmsg's source, not inferred — see
// docs/agent-board.md's "agmsg compatibility contract", Tier 1).
const fixtureDir = "testdata/agmsg-v1.1.x"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseMessageRows_Basic(t *testing.T) {
	data := readFixture(t, "messages-basic.jsonl")
	rows, err := parseMessageRows(data, "hostA")
	if err != nil {
		t.Fatalf("parseMessageRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	want := Row{
		ID: "101", Host: "hostA", Team: "panemux",
		From: "pane-a", To: "pane-b", Body: "please review",
		At: time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
	}
	if rows[0] != want {
		t.Fatalf("rows[0] = %+v, want %+v", rows[0], want)
	}
}

func TestParseMessageRows_StatusBodyRoundTrips(t *testing.T) {
	data := readFixture(t, "messages-status.jsonl")
	rows, err := parseMessageRows(data, "hostA")
	if err != nil {
		t.Fatalf("parseMessageRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	st, ok := ParseStatus(rows[0].Body)
	if !ok {
		t.Fatalf("expected rows[0] body to parse as a status update")
	}
	if st.State != "working" || st.Branch != "feature/x" {
		t.Fatalf("unexpected status: %+v", st)
	}
	if _, ok := ParseStatus(rows[1].Body); ok {
		t.Fatalf("expected rows[1] (plain chat) to not parse as a status update")
	}
}

func TestParseMessageRows_EmptyTeam(t *testing.T) {
	data := readFixture(t, "messages-empty-team.jsonl")
	rows, err := parseMessageRows(data, "hostA")
	if err != nil {
		t.Fatalf("parseMessageRows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for an empty team, got %d", len(rows))
	}
}

func TestParseMessageRows_MalformedLine(t *testing.T) {
	_, err := parseMessageRows([]byte("not json\n"), "hostA")
	if err == nil {
		t.Fatal("expected an error for a malformed line, not a silent drop")
	}
}

func TestParseMessageRows_BlankLinesSkipped(t *testing.T) {
	line1 := `{"type":"message_sent","id":"1","team":"panemux","from":"pane-a","to":"pane-b",` +
		`"body":"hi","at":"2026-08-08T10:00:00Z"}`
	line2 := `{"type":"message_sent","id":"2","team":"panemux","from":"pane-a","to":"pane-b",` +
		`"body":"hi again","at":"2026-08-08T10:01:00Z"}`
	data := []byte(line1 + "\n" + "\n" + "   \n" + line2 + "\n")
	rows, err := parseMessageRows(data, "hostA")
	if err != nil {
		t.Fatalf("parseMessageRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected blank lines to be skipped without affecting row count, got %d rows: %+v", len(rows), rows)
	}
}

func TestParseMessageRows_MalformedTimestamp_FallsBackToZeroTime(t *testing.T) {
	line := `{"type":"message_sent","id":"1","team":"panemux","from":"pane-a","to":"pane-b",` +
		`"body":"hi","at":"not-a-timestamp"}`
	data := []byte(line + "\n")
	rows, err := parseMessageRows(data, "hostA")
	if err != nil {
		t.Fatalf("parseMessageRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row despite the malformed timestamp, got %d", len(rows))
	}
	if !rows[0].At.IsZero() {
		t.Fatalf("expected a zero-time fallback for a malformed timestamp, got %v", rows[0].At)
	}
	if rows[0].ID != "1" || rows[0].From != "pane-a" || rows[0].To != "pane-b" {
		t.Fatalf("expected identity/routing fields to remain intact despite the timestamp failure, got %+v", rows[0])
	}
}

func TestParseMessageRows_LineTooLong_ReturnsScanError(t *testing.T) {
	longLine := bytes.Repeat([]byte("a"), 2*1024*1024) // exceeds the scanner's 1MB max token size
	if _, err := parseMessageRows(longLine, "hostA"); err == nil {
		t.Fatal("expected a scan error for a line exceeding the scanner's max token size")
	}
}

func TestIdAfter(t *testing.T) {
	tests := []struct {
		id, afterID string
		want        bool
	}{
		{"5", "3", true},
		{"3", "5", false},
		{"5", "5", false},
		{"5", "", true},   // empty afterID means "everything passes"
		{"10", "9", true}, // numeric, not lexicographic (lexicographic would say "10" < "9")
		{"abc", "abd", false},
		{"abd", "abc", true},
	}
	for _, tt := range tests {
		if got := idAfter(tt.id, tt.afterID); got != tt.want {
			t.Errorf("idAfter(%q, %q) = %v, want %v", tt.id, tt.afterID, got, tt.want)
		}
	}
}

func TestFilterAfterID(t *testing.T) {
	rows := []Row{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	got := filterAfterID(rows, "1")
	if len(got) != 2 || got[0].ID != "2" || got[1].ID != "3" {
		t.Fatalf("unexpected filter result: %+v", got)
	}
}
