package agmsg

import (
	"bytes"
	"encoding/json"
	"os"
	"panemux/internal/board"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureDir holds Tier 1 of the agmsg compatibility contract (see
// docs/agent-board/agmsg-contract.md#agmsg-compatibility-contract): fast, hermetic tests
// that assert panemux's own parsing against frozen, versioned output
// captured from a real agmsg install at TestedAgmsgVersion. See
// fixtureDir/README.md for how it is regenerated, and for what the capture
// caught that the hand-written fixtures it replaced could not.
const fixtureDir = "testdata/agmsg-v" + TestedAgmsgVersion

// Read expected capture metadata independently of the production parser. Bumps
// change data, never these tests; opaque IDs remain anchored to response order.
type capturedMessageMetadata struct {
	At time.Time
	ID string
}

func fixtureMessages(t *testing.T) []capturedMessageMetadata {
	t.Helper()
	var rows []capturedMessageMetadata
	for _, line := range bytes.Split(bytes.TrimSpace(readFixture(t, "get_team_messages.jsonl")), []byte("\n")) {
		var row capturedMessageMetadata
		require.NoError(t, json.Unmarshal(line, &row))
		rows = append(rows, row)
	}
	require.Len(t, rows, 4)
	return rows
}

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

	assert.Equal(t, fixtureMessages(t)[0].ID, rows[0].ID)
	assert.Equal(t, "pane-a", rows[0].From)
	assert.Equal(t, "pane-b", rows[0].To)
	assert.Equal(t, "please review my latest commit", rows[0].Body)
	assert.Equal(t, "local", rows[0].Host)
	assert.Equal(t, "panemux", rows[0].Team)
	assert.Equal(t, fixtureMessages(t)[0].At, rows[0].At)

	assert.Equal(t, fixtureMessages(t)[3].ID, rows[3].ID)
	assert.Equal(t, "lgtm, merging now", rows[3].Body)
}

func TestAgmsgFixture_TeamMessages_StatusRowRecognized(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	status, ok := board.IsStatusRow(rows[1])
	require.True(t, ok, "row 2 (addressed to _system with kind=board_status) must be recognized as a status report")
	assert.Equal(t, "working", status.State)
	assert.Equal(t, "/tmp/sample-project", status.CWD)
	assert.Equal(t, "feature/x", status.Branch)
	assert.Equal(t, "owner/repo", status.Repo)
	assert.Equal(t, "https://github.com/owner/repo/pull/123", status.PRURL)
}

func TestAgmsgFixture_TeamMessages_CoincidentalJSONNotMistakenForStatus(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	// board.Row 3 is addressed to _system and happens to be valid JSON with a
	// "state" field, but has no "kind": "board_status" discriminator — it
	// must be left alone as an ordinary message, not swallowed into status.
	_, ok := board.IsStatusRow(rows[2])
	assert.False(t, ok)
}

func TestAgmsgFixture_TeamMessages_OrdinaryRowsNotMistakenForStatus(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	for _, i := range []int{0, 3} {
		_, ok := board.IsStatusRow(rows[i])
		assert.False(t, ok, "row %d is not addressed to _system and must never be treated as status", i)
	}
}

func TestAgmsgFixture_TeamMessages_FilterRowsAfterCursor(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")

	newRows := filterRowsAfter(rows, fixtureMessages(t)[1].ID)
	require.Len(t, newRows, 2)
	assert.Equal(t, fixtureMessages(t)[2].ID, newRows[0].ID)
	assert.Equal(t, fixtureMessages(t)[3].ID, newRows[1].ID)
}

// TestAgmsgFixture_TeamMessages_IDsAreNotNumeric is the hermetic guard
// against the assumption the real capture disproved. If a future fixture
// refresh ever brings integer ids back, that is a storage-driver change
// worth noticing deliberately rather than inheriting.
func TestAgmsgFixture_TeamMessages_IDsAreNotNumeric(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	for _, r := range rows {
		_, err := strconv.ParseInt(r.ID, 10, 64)
		assert.Error(t, err, "agmsg %s ids are opaque text, not integers: %q", TestedAgmsgVersion, r.ID)
	}
}

// TestAgmsgFixture_TeamMessages_LexicographicOrderDiffersFromReturnedOrder
// records why the cursor anchors on position rather than on any comparison:
// these four rows were written in the same millisecond, so their UUIDv7
// prefixes are identical and only the random bits separate them.
func TestAgmsgFixture_TeamMessages_LexicographicOrderDiffersFromReturnedOrder(t *testing.T) {
	data, err := os.ReadFile("testdata/agmsg-v1.2.0/get_team_messages.jsonl")
	require.NoError(t, err)
	rows := parseAgmsgMessageRows(data, "local")
	require.Len(t, rows, 4)

	returned := make([]string, 0, len(rows))
	for _, r := range rows {
		returned = append(returned, r.ID)
	}
	sorted := append([]string(nil), returned...)
	sort.Strings(sorted)

	assert.NotEqual(t, returned, sorted,
		"sorting these ids as strings must not reproduce api.sh's own order — a fixture where it "+
			"does no longer demonstrates the hazard it was captured for")
}

func TestAgmsgFixture_TeamMessages_BoardCacheIntegration(t *testing.T) {
	data := readFixture(t, "get_team_messages.jsonl")
	rows := parseAgmsgMessageRows(data, "local")

	cache := board.NewBoardCache()
	for _, r := range rows {
		if status, ok := board.IsStatusRow(r); ok {
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
