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
		entries: make(map[ownSendKey][]time.Time),
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
		entries: make(map[ownSendKey][]time.Time),
		ttl:     10 * time.Second,
		now:     fixedClock(start),
	}
	l.Record("host-b", "team", "pane-x", "hello")

	l.now = fixedClock(start.Add(2 * time.Second))
	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"))
}

func TestOwnSendLedger_Forget_RemovesEntryBeforeItExpires(t *testing.T) {
	// Regression test: a Send that fails after Record must not leave a
	// live, matchable entry for the rest of the TTL window — that would let
	// any pane on destHost forge a From == SystemID row until the entry
	// naturally expired, even though panemux never actually delivered
	// anything with that body to that host.
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")

	l.Forget("host-b", "team", "pane-x", "hello")

	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"), "forgotten entry must not match")
}

func TestOwnSendLedger_Forget_NoMatchingEntry_NoPanicNoOp(t *testing.T) {
	l := newOwnSendLedger()
	l.Forget("host-b", "team", "pane-x", "hello")
	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"))
}

func TestOwnSendLedger_Forget_OnlyRemovesMatchingKey(t *testing.T) {
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")
	l.Record("host-b", "team", "pane-y", "hello")

	l.Forget("host-b", "team", "pane-x", "hello")

	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"), "forgotten entry must not match")
	assert.True(t, l.Consume("host-b", "team", "pane-y", "hello"), "unrelated entry must be unaffected")
}

func TestOwnSendLedger_DuplicateRecord_BothOccurrencesIndependentlyConsumable(t *testing.T) {
	// Regression test (adversarial review round 2, finding B4a): two
	// broadcasts with the identical destHost/team/to/body are two real,
	// independent sends — each will produce its own row when the relay
	// polls the destination host back. A plain single-entry map would let
	// the second Record silently overwrite the first, so only one of the
	// two real rows could ever Consume successfully and the other would be
	// dropped as "invalid from" — an ordinary duplicate message, not an
	// attack, would vanish from history. Both occurrences must be
	// independently matchable.
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello")
	l.Record("host-b", "team", "pane-x", "hello")

	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"), "first occurrence must match")
	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"), "second, independent occurrence must also match")
	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"), "a third, non-existent occurrence must not match")
}

func TestOwnSendLedger_ForgetOneOfTwoDuplicates_OtherStillMatchable(t *testing.T) {
	// Regression test (adversarial review round 2, finding B4b): the
	// Forget-on-Send-failure fix must remove only the occurrence
	// corresponding to the failed send, never a different, still-live
	// occurrence that happens to share the same key because an earlier,
	// successful send used the identical destHost/team/to/body.
	l := newOwnSendLedger()
	l.Record("host-b", "team", "pane-x", "hello") // first send: succeeds, stays live
	l.Record("host-b", "team", "pane-x", "hello") // second, duplicate send: about to fail

	l.Forget("host-b", "team", "pane-x", "hello") // undo only the failed one

	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"),
		"the first, successful send's occurrence must survive a Forget for an unrelated failed duplicate")
	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"),
		"only one occurrence should have remained after Forget removed one of the two")
}

func TestOwnSendLedger_Consume_SkipsExpiredOccurrenceFindsUnexpiredOne(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &ownSendLedger{
		entries: make(map[ownSendKey][]time.Time),
		ttl:     time.Second,
		now:     fixedClock(start),
	}
	l.Record("host-b", "team", "pane-x", "hello") // will have expired by the time we Consume

	l.now = fixedClock(start.Add(2 * time.Second))
	l.Record("host-b", "team", "pane-x", "hello") // recorded "now", still unexpired

	assert.True(t, l.Consume("host-b", "team", "pane-x", "hello"),
		"an unexpired occurrence must still match even alongside an expired one for the same key")
	assert.False(t, l.Consume("host-b", "team", "pane-x", "hello"),
		"no occurrence should remain: the expired one was garbage-collected, the live one was just consumed")
}
