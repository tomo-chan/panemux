package board

import (
	"testing"
	"time"
)

func TestOwnSendLedger_MatchAndConsume(t *testing.T) {
	l := newOwnSendLedger(time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.nowFn = func() time.Time { return now }

	l.Record("hostB", "panemux", "codex-b", "please review")

	if !l.Consume("hostB", "panemux", "codex-b", "please review") {
		t.Fatal("expected Consume to match a recently recorded Send")
	}
	// One-shot: consuming again must fail even though the TTL hasn't
	// expired, since the entry was deleted on first match.
	if l.Consume("hostB", "panemux", "codex-b", "please review") {
		t.Fatal("expected Consume to be one-shot")
	}
}

func TestOwnSendLedger_NoMatch(t *testing.T) {
	l := newOwnSendLedger(time.Minute)
	l.Record("hostB", "panemux", "codex-b", "please review")

	// This is the regression test for cross-host _panemux impersonation:
	// a row crafted with a to/body that doesn't match any real recent send
	// must not be treated as legitimate on the strength of From alone.
	if l.Consume("hostB", "panemux", "codex-b", "a completely different body") {
		t.Fatal("expected no match for a forged body")
	}
	if l.Consume("hostB", "panemux", "someone-else", "please review") {
		t.Fatal("expected no match for a forged to")
	}
	if l.Consume("hostC", "panemux", "codex-b", "please review") {
		t.Fatal("expected no match for a forged dest host")
	}
	// The original, correctly-keyed entry was never actually consumed by
	// the mismatched attempts above, so it must still match.
	if !l.Consume("hostB", "panemux", "codex-b", "please review") {
		t.Fatal("expected the real entry to still be matchable after unrelated forged attempts")
	}
}

func TestOwnSendLedger_TTLExpiry(t *testing.T) {
	l := newOwnSendLedger(time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.nowFn = func() time.Time { return now }

	l.Record("hostB", "panemux", "codex-b", "please review")

	now = now.Add(2 * time.Minute) // past TTL
	if l.Consume("hostB", "panemux", "codex-b", "please review") {
		t.Fatal("expected an entry past its TTL to no longer be matchable")
	}
}

func TestOwnSendLedger_PruneRemovesExpiredEntries(t *testing.T) {
	l := newOwnSendLedger(time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.nowFn = func() time.Time { return now }

	l.Record("hostB", "panemux", "codex-b", "stale")
	now = now.Add(2 * time.Minute)
	l.Record("hostB", "panemux", "codex-c", "fresh") // triggers prune of the stale entry

	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected prune to drop the expired entry, got %d entries", n)
	}
}
