package board

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureDir holds Tier 1 of the agmsg compatibility contract (see
// docs/agent-board.md#agmsg-compatibility-contract): fast, hermetic tests
// that assert panemux's own parsing against frozen, versioned fixture
// output. See fixtureDir/README.md for why these particular fixtures are
// hand-written rather than captured from a real agmsg install.
const fixtureDir = "testdata/agmsg-unpinned-handwritten"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	require.NoError(t, err)
	return data
}

func TestAgmsgFixture_TeamMessages_ParsesAllRowsInOrder(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")

	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	assert.Equal(t, "1", rows[0].ID)
	assert.Equal(t, "pane-a", rows[0].From)
	assert.Equal(t, "pane-b", rows[0].To)
	assert.Equal(t, "please review my latest commit", rows[0].Body)
	assert.Equal(t, "local", rows[0].Host)
	assert.Equal(t, "panemux", rows[0].Team)
	assert.Equal(t, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), rows[0].At)

	assert.Equal(t, "4", rows[3].ID)
	assert.Equal(t, "lgtm, merging now", rows[3].Body)
}

func TestAgmsgFixture_TeamMessages_StatusRowRecognized(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	status, ok := IsStatusRow(rows[1])
	require.True(t, ok, "row 2 (addressed to _system with kind=board_status) must be recognized as a status report")
	assert.Equal(t, "working", status.State)
	assert.Equal(t, "/home/user/project", status.CWD)
	assert.Equal(t, "feature/x", status.Branch)
	assert.Equal(t, "owner/repo", status.Repo)
	assert.Equal(t, "https://github.com/owner/repo/pull/123", status.PRURL)
}

func TestAgmsgFixture_TeamMessages_CoincidentalJSONNotMistakenForStatus(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	// Row 3 is addressed to _system and happens to be valid JSON with a
	// "state" field, but has no "kind": "board_status" discriminator — it
	// must be left alone as an ordinary message, not swallowed into status.
	_, ok := IsStatusRow(rows[2])
	assert.False(t, ok)
}

func TestAgmsgFixture_TeamMessages_OrdinaryRowsNotMistakenForStatus(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	for _, i := range []int{0, 3} {
		_, ok := IsStatusRow(rows[i])
		assert.False(t, ok, "row %d is not addressed to _system and must never be treated as status", i)
	}
}

func TestAgmsgFixture_TeamMessages_FilterRowsAfterCursor(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")

	newRows := filterRowsAfter(rows, "2")
	require.Len(t, newRows, 2)
	assert.Equal(t, "3", newRows[0].ID)
	assert.Equal(t, "4", newRows[1].ID)
}

func TestAgmsgFixture_TeamMessages_BoardCacheIntegration(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")

	cache := NewBoardCache()
	for _, r := range rows {
		if status, ok := IsStatusRow(r); ok {
			cache.RecordStatus(r.From, status)
			continue
		}
		cache.AppendMessage(r)
	}

	snapshot := cache.StatusSnapshot()
	require.Contains(t, snapshot, "pane-a")
	assert.Equal(t, "working", snapshot["pane-a"].State)

	// Only the two ordinary messages (rows 1 and 4) should have been
	// appended to history — the status row (2) and the coincidental-JSON
	// row (3, addressed to _system but not a status report) are handled
	// differently: 2 goes to RecordStatus and never AppendMessage; 3 is an
	// ordinary message and IS appended.
	history := cache.MessagesSince(0)
	require.Len(t, history, 3)
	bodies := []string{history[0].Row.Body, history[1].Row.Body, history[2].Row.Body}
	assert.Equal(t, []string{
		"please review my latest commit",
		`{"state":"looks like status but has no kind field"}`,
		"lgtm, merging now",
	}, bodies)
}
