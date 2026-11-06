package agmsg

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/agent"
	"testing"
)

// Every agent that declares board has exactly one board adapter, and no
// other agent has one.
func TestBoardAdapters_MatchTheRegistry(t *testing.T) {
	require.NoError(t, validateBoardAdapters(agent.All(), boardAdapters()))

	var ids []agent.ID
	for _, r := range boardAdapters() {
		ids = append(ids, r.id)
	}
	var want []agent.ID
	for _, d := range agent.All() {
		if d.Has(agent.Board) {
			want = append(want, d.ID)
		}
	}
	assert.ElementsMatch(t, want, ids)
}

func TestValidateBoardAdapters_RefusesASetThatDoesNotMatch(t *testing.T) {
	defs := []agent.Definition{
		{ID: "one", Capabilities: []agent.Capability{agent.Detect, agent.Board}},
		{ID: "two", Capabilities: []agent.Capability{agent.Detect}},
	}
	cases := []struct {
		name     string
		wantErr  string
		adapters []registeredBoard
	}{
		{
			name: "an agent that is not registered",
			adapters: []registeredBoard{{id: "one", agmsgType: "one"},
				{id: "three", agmsgType: "three"}},
			wantErr: `board adapter "three" is not registered in the agent registry`,
		},
		{
			name: "an agent that does not declare board",
			adapters: []registeredBoard{{id: "one", agmsgType: "one"},
				{id: "two", agmsgType: "two"}},
			wantErr: `board adapter "two" does not declare board in the agent registry`,
		},
		{
			name: "an agent registered twice",
			adapters: []registeredBoard{{id: "one", agmsgType: "one"},
				{id: "one", agmsgType: "one"}},
			wantErr: `board adapter "one" is registered twice`,
		},
		{
			name:    "an agent with no adapter",
			wantErr: `agent "one" declares board but has no board adapter`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.EqualError(t, validateBoardAdapters(defs, tc.adapters), tc.wantErr)
		})
	}
	assert.NoError(t, validateBoardAdapters(defs, []registeredBoard{{id: "one", agmsgType: "one"}}))
}

// Which agent claims its identity is the adapter's to say: only Claude
// Code's session-id source is verified.

// A type no adapter is registered for gets the shared steps and no claim.

func TestBoardAdapterTypesRejectMissingInvalidAndDuplicateNames(t *testing.T) {
	defs := []agent.Definition{{ID: "one", Capabilities: []agent.Capability{agent.Board}},
		{ID: "two", Capabilities: []agent.Capability{agent.Board}}}
	for _, types := range [][2]string{{"", "two"}, {"UPPER", "two"}, {"same", "same"}, {"one", "bad name"}} {
		t.Run(types[0]+"/"+types[1], func(t *testing.T) {
			entries := []registeredBoard{{id: "one", agmsgType: types[0]},
				{id: "two", agmsgType: types[1]}}
			require.ErrorContains(t, validateBoardAdapters(defs, entries), "invalid or duplicate agmsg type")
		})
	}
	require.NoError(t, validateBoardAdapters(defs, []registeredBoard{{id: "one", agmsgType: "one"},
		{id: "two", agmsgType: "two"}}))
}
