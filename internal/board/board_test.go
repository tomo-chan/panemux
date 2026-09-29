package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStatus_ValidFullPayload(t *testing.T) {
	body := `{
		"kind": "board_status",
		"state": "working",
		"cwd": "/home/user/project",
		"branch": "feature/x",
		"repo": "owner/repo",
		"pr_url": "https://github.com/owner/repo/pull/123",
		"last_tool": "Edit internal/api/handler.go",
		"summary": "fixing failing tests"
	}`

	got, ok := ParseStatus(body)
	assert.True(t, ok)
	assert.Equal(t, Status{
		State:    "working",
		CWD:      "/home/user/project",
		Branch:   "feature/x",
		Repo:     "owner/repo",
		PRURL:    "https://github.com/owner/repo/pull/123",
		LastTool: "Edit internal/api/handler.go",
		Summary:  "fixing failing tests",
	}, got)
}

func TestParseStatus_MissingOptionalFields(t *testing.T) {
	got, ok := ParseStatus(`{"kind":"board_status","state":"idle"}`)
	assert.True(t, ok)
	assert.Equal(t, "idle", got.State)
	assert.Empty(t, got.CWD)
	assert.Empty(t, got.Branch)
	assert.Empty(t, got.PRURL)
}

func TestParseStatus_NotValidJSON_FalseNotError(t *testing.T) {
	_, ok := ParseStatus("hello there, not json at all")
	assert.False(t, ok)
}

func TestParseStatus_ValidJSONMissingKind_TreatedAsPlainMessage(t *testing.T) {
	// Regression test for the shape-sniffing ambiguity: a JSON body that
	// happens to have a "state" field but no "kind" must not be mistaken
	// for a status report.
	_, ok := ParseStatus(`{"state":"working","cwd":"/tmp"}`)
	assert.False(t, ok)
}

func TestParseStatus_ValidJSONWrongKind_TreatedAsPlainMessage(t *testing.T) {
	_, ok := ParseStatus(`{"kind":"something_else","state":"working"}`)
	assert.False(t, ok)
}

func TestParseStatus_EmptyBody_False(t *testing.T) {
	_, ok := ParseStatus("")
	assert.False(t, ok)
}

func TestIsStatusRow_AddressedToSystemWithStatusBody_True(t *testing.T) {
	row := Row{
		To:   SystemID,
		Body: `{"kind":"board_status","state":"idle"}`,
		At:   time.Now(),
	}
	status, ok := IsStatusRow(row)
	require.True(t, ok)
	assert.Equal(t, "idle", status.State)
}

func TestIsStatusRow_AddressedToOtherPane_False(t *testing.T) {
	row := Row{
		To:   "some-other-pane",
		Body: `{"kind":"board_status","state":"idle"}`,
	}
	_, ok := IsStatusRow(row)
	assert.False(t, ok)
}

func TestIsStatusRow_AddressedToSystemButPlainMessage_False(t *testing.T) {
	row := Row{
		To:   SystemID,
		Body: "just chatting with the command center",
	}
	_, ok := IsStatusRow(row)
	assert.False(t, ok)
}

func TestIsStatusRow_AddressedToSystemWithCoincidentalJSON_False(t *testing.T) {
	row := Row{
		To:   SystemID,
		Body: `{"state":"looks like status but isn't"}`,
	}
	_, ok := IsStatusRow(row)
	assert.False(t, ok)
}
