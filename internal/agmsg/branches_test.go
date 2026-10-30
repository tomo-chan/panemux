package agmsg

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseAgmsgMessageRowsSkipsWhatItCannotUseAndKeepsGoing(t *testing.T) {
	data := []byte("" +
		`{"type":"message_sent","id":"1","team":"panemux","from":"a","to":"b","body":"first"}` + "\n" +
		"\n" +
		`{"type":"message_sent","id":"2","team":"panemux","from":"a","to":"b","body":"after a blank line"}` + "\n" +
		"this is not json at all\n" +
		`{"type":"message_sent","id":"3","team":"panemux","from":"a","to":"b","body":"after a bad line"}` + "\n" +
		`{"type":"agent_joined","id":"x","team":"panemux","from":"a","to":"b","body":"ignored"}` + "\n" +
		`{"type":"message_sent","id":"4","team":"panemux","from":"a","to":"b","body":"after another type"}` + "\n")

	rows := parseAgmsgMessageRows(data, "host-a")

	var bodies []string
	for _, r := range rows {
		bodies = append(bodies, r.Body)
	}
	assert.Equal(t, []string{"first", "after a blank line", "after a bad line", "after another type"}, bodies,
		"each skipped line must cost only itself, never the rows behind it")
}

func TestParseAgmsgReleaseRejectsNonNumericAndNegativeComponents(t *testing.T) {
	for _, version := range []string{"1.x.3", "1.2.z", "1.-2.3", "-1.2.3"} {
		t.Run(version, func(t *testing.T) {
			_, ok := parseAgmsgRelease(version)
			assert.False(t, ok, "%q is not a release", version)
		})
	}

	got, ok := parseAgmsgRelease("1.2.3")
	require.True(t, ok, "the rejection above must not be rejecting everything")
	assert.Equal(t, agmsgRelease{major: 1, minor: 2, patch: 3}, got)
}
