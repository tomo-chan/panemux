package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestOwnSendLedger_RecordThenConsume_Matches(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")

	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"))
}

func TestOwnSendLedger_ConsumeIsOneShot(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")

	require := assert.New(t)
	require.True(l.Consume("host-b", "team", "pane-x", "hello"))
	require.False(l.Consume("host-b", "team", "pane-x", "hello"), "a consumed entry must not match twice")
}

func TestOwnSendLedger_Consume_NoMatchingEntry_False(t *testing.T) {
	l := newOwnSendLedger()
	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"))
}

func TestOwnSendLedger_Consume_BodyMismatch_False(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "the real body")

	assert.False(t, l.Consume("host-b", "team", "pane-x", "a forged body"))
}

func TestOwnSendLedger_Consume_DestHostMismatch_False(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")

	assert.False(t, l.Consume("host-c", "team", "pane-x", "hello"))
}

func TestOwnSendLedger_Consume_TeamMismatch_False(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")

	assert.False(t, l.Consume("host-b", "other-team", "pane-x", "hello"))
}

func TestOwnSendLedger_Consume_ToMismatch_False(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")

	assert.False(t, l.Consume("host-b", "team", "pane-y", "hello"))
}

func TestOwnSendLedger_EmptyTeam_StillMatches(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "", "pane-x", "hello")

	assert.True(t, l.Consume("host-b", "", "pane-x", "hello"))
}

func TestOwnSendLedger_ExpiredEntry_NoLongerMatchable(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &ownSendLedger{
		entries: make(map[ownSendKey]time.Time),
		ttl:     time.Second,
		now:     fixedClock(start),
	}
	l.Record("host-b", "team", "pane-x", "hello")

	l.now = fixedClock(start.Add(2 * time.Second))
	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"), "expired entry must not match")
}

func TestOwnSendLedger_UnexpiredEntry_StillMatchable(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &ownSendLedger{
		entries: make(map[ownSendKey]time.Time),
		ttl:     10 * time.Second,
		now:     fixedClock(start),
	}
	l.Record("host-b", "team", "pane-x", "hello")

	l.now = fixedClock(start.Add(2 * time.Second))
	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"))
}
