package board

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Tier 1 of issue #168's model-checking split.
//
// TLC proves things about spec/agentboard/OwnSendLedger.tla. It proves
// nothing whatsoever about ledger.go — a spec and an implementation can both
// be internally consistent and still disagree, which is the failure mode
// model checking on its own invites. What closes that gap is here: the real
// ownSendLedger emits its own transitions as it runs, and every one of them
// is looked up in the transition table TLC exported. A transition the model
// has no case for, or one that lands somewhere the model forbids, fails.
//
// This file needs no JDK and no tla2tools.jar: it reads the committed table.
// `make model-check` is what keeps that table honest about the spec.

const (
	// ledgerModelTTL and ledgerModelTick are chosen so that a single tick
	// expires nothing and two ticks expire everything recorded before them.
	// That gives the drivers below two live generations plus an expired one,
	// which is what it takes to reach states like {expired:2 live:1} — an
	// expiry granularity coarse enough to expire all-or-nothing could never
	// produce them, and those are exactly the states where Consume's
	// garbage-collection and Forget's last-occurrence rule interact.
	ledgerModelTTL  = 3 * time.Second
	ledgerModelTick = 2 * time.Second
)

var ledgerModelEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ledgerConformance replays a live ownSendLedger's own transitions against
// the model and accumulates which of the model's edges were exercised.
type ledgerConformance struct {
	t       *testing.T
	model   *ledgerModel
	prev    map[ownSendKey]ledgerCounts
	covered map[ledgerTransition]bool
	steps   int
}

func newLedgerConformance(t *testing.T, model *ledgerModel) *ledgerConformance {
	return &ledgerConformance{
		t:       t,
		model:   model,
		prev:    make(map[ownSendKey]ledgerCounts),
		covered: make(map[ledgerTransition]bool),
	}
}

// watch attaches the checker to a ledger. Each ledger starts a fresh run, so
// the per-key "where the model thinks we are" state resets; edge coverage
// accumulates across every ledger the checker has watched.
func (c *ledgerConformance) watch(l *ownSendLedger) {
	c.prev = make(map[ownSendKey]ledgerCounts)
	l.trace = c.observe
}

func (c *ledgerConformance) observe(step ownSendLedgerStep) {
	c.t.Helper()
	c.steps++

	keys := ledgerStepKeys(c.prev, step.Before, step.After)

	for _, key := range keys {
		// A slice rather than a map: Go randomizes map range order, so with a
		// key out of bounds on both sides the reported side would flip between
		// runs of the same failing test — the nondeterminism ledgerStepKeys
		// above exists to remove.
		for _, side := range []struct {
			label  string
			counts ledgerCounts
		}{{"before", step.Before[key]}, {"after", step.After[key]}} {
			counts := side.counts
			require.True(c.t, c.model.knows(counts),
				"step %d (%s %s): key %v is %s %s, which is outside the model's bound of %d occurrences; "+
					"either the driver overshot the bound or MaxEntries in %s is too small",
				c.steps, step.Action, step.Result, key, side.label, counts, c.model.file.MaxHeld, c.model.file.Config)
		}
	}

	// Between two ledger calls the clock may have advanced, expiring
	// occurrences for any key. The spec models that as Expire steps, which no
	// trace ever names, so every key must have moved from where the previous
	// step left it to where this one starts by some number of them.
	for _, key := range keys {
		from, to := c.prev[key], step.Before[key]
		require.True(c.t, c.model.expireReachable(from, to),
			"step %d (%s %s): key %v moved from %s to %s between calls, which no sequence of "+
				"Expire steps in the model allows", c.steps, step.Action, step.Result, key, from, to)
	}

	// No time passes inside one ledger call, so every key the call did not
	// name must come out of it exactly as it went in. This is the key
	// independence the spec assumes when it models a single key and does not
	// itself prove — a Forget that truncated the wrong key's slice would show
	// up here and nowhere else.
	for _, key := range keys {
		if key == step.Key {
			continue
		}
		require.Equal(c.t, step.Before[key], step.After[key],
			"step %d: %s on key %v also changed key %v", c.steps, step.Action, step.Key, key)
	}

	from, to := step.Before[step.Key], step.After[step.Key]
	targets, known := c.model.successors(from, step.Action, step.Result)
	require.True(c.t, known,
		"step %d: the model has no %s transition returning %q out of %s (key %v); "+
			"the implementation took a step %s does not describe",
		c.steps, step.Action, step.Result, from, step.Key, c.model.file.Spec)
	require.True(c.t, c.model.allows(from, step.Action, step.Result, to),
		"step %d: %s returning %q took key %v from %s to %s; the model allows only %v",
		c.steps, step.Action, step.Result, step.Key, from, to, targets)

	c.covered[ledgerTransition{Action: step.Action, Result: step.Result, From: from, To: to}] = true

	c.prev = make(map[ownSendKey]ledgerCounts, len(step.After))
	for key, counts := range step.After {
		c.prev[key] = counts
	}
}

// requireEveryModelEdgeExercised turns "the implementation never contradicted
// the model" into the stronger "the implementation matched the model on every
// transition the model has". Without it a table permissive enough to accept
// anything would pass just as quietly as a correct one.
func (c *ledgerConformance) requireEveryModelEdgeExercised() {
	c.t.Helper()
	var missing []string
	for edge := range c.model.nonExpireEdges() {
		if !c.covered[edge] {
			missing = append(missing, fmt.Sprintf("%s %s: %s -> %s",
				edge.Action, edge.Result, edge.From, edge.To))
		}
	}
	sort.Strings(missing)
	require.Empty(c.t, missing,
		"%d model transitions were never taken by the real ledger across %d observed steps",
		len(missing), c.steps)
}

func ledgerStepKeys(sets ...map[ownSendKey]ledgerCounts) []ownSendKey {
	seen := make(map[ownSendKey]bool)
	var keys []ownSendKey
	for _, set := range sets {
		for key := range set {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	// A total order, so a failure names the same key first on every run.
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.DestHost != b.DestHost {
			return a.DestHost < b.DestHost
		}
		if a.Team != b.Team {
			return a.Team < b.Team
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.BodyHash < b.BodyHash
	})
	return keys
}

// ── The driver's operation alphabet ───────────────────────────────────────

type ledgerOp struct {
	// apply runs the operation against the ledger, or advances the clock.
	// It returns false when the operation was skipped because performing it
	// would have pushed a key past the model's bound.
	apply func(l *ownSendLedger, clock *time.Time, maxHeld int) bool
	name  string
}

func ledgerOps(keys []ownSendKey) []ledgerOp {
	ops := []ledgerOp{{
		name: "Tick",
		apply: func(l *ownSendLedger, clock *time.Time, _ int) bool {
			*clock = clock.Add(ledgerModelTick)
			l.now = fixedClock(*clock)
			return true
		},
	}}
	for _, key := range keys {
		k := key
		ops = append(ops,
			ledgerOp{name: "Record(" + k.To + ")", apply: func(l *ownSendLedger, _ *time.Time, maxHeld int) bool {
				if len(l.entries[k]) >= maxHeld {
					return false
				}
				l.Record(k.DestHost, k.Team, k.To, ledgerModelBody)
				return true
			}},
			ledgerOp{name: "Consume(" + k.To + ")", apply: func(l *ownSendLedger, _ *time.Time, _ int) bool {
				l.Consume(k.DestHost, k.Team, k.To, ledgerModelBody)
				return true
			}},
			ledgerOp{name: "Forget(" + k.To + ")", apply: func(l *ownSendLedger, _ *time.Time, _ int) bool {
				l.Forget(k.DestHost, k.Team, k.To, ledgerModelBody)
				return true
			}},
		)
	}
	return ops
}

const ledgerModelBody = "conformance-body"

func ledgerModelKey(to string) ownSendKey {
	return ownSendKey{DestHost: "host-b", Team: "team", To: to, BodyHash: bodyHash(ledgerModelBody)}
}

func newLedgerUnderTest(c *ledgerConformance) (*ownSendLedger, time.Time) {
	clock := ledgerModelEpoch
	l := &ownSendLedger{
		entries: make(map[ownSendKey][]time.Time),
		ttl:     ledgerModelTTL,
		now:     fixedClock(clock),
	}
	c.watch(l)
	return l, clock
}

// replay drives a fresh ledger through one operation sequence. Every ledger
// call inside it is checked against the model by the tracer.
func (c *ledgerConformance) replay(ops []ledgerOp, seq []int, maxHeld int) *ownSendLedger {
	l, clock := newLedgerUnderTest(c)
	for _, i := range seq {
		ops[i].apply(l, &clock, maxHeld)
	}
	return l
}

// ── Driver 1: every transition the model has, taken by the real ledger ────

// TestOwnSendLedgerTakesEveryModelTransition walks the model's own state
// space using the real implementation: from each distinct ledger shape it can
// reach, it tries every operation and checks the resulting transition. Because
// it visits every shape and tries everything from each one, it ends with every
// non-Expire edge in the table exercised — which is the claim
// requireEveryModelEdgeExercised makes.
//
// The search key is the ledger's shape relative to the current clock (how many
// occurrences have expired, and the remaining lifetimes of those that have
// not), not the {expired, live} abstraction: two ledgers that look identical
// under the abstraction can behave differently one tick later if their
// occurrences were recorded at different times, and states such as
// {expired:2 live:1} are only reachable through that difference.
func TestOwnSendLedgerTakesEveryModelTransition(t *testing.T) {
	model := loadLedgerModel(t)
	c := newLedgerConformance(t, model)

	key := ledgerModelKey("pane-x")
	ops := ledgerOps([]ownSendKey{key})

	type node struct {
		shape string
		seq   []int
	}
	start := node{shape: ledgerShape(c.replay(ops, nil, model.file.MaxHeld), ledgerModelEpoch, key)}
	seen := map[string]bool{start.shape: true}
	queue := []node{start}

	const maxDepth = 16
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if len(cur.seq) >= maxDepth {
			continue
		}
		for i := range ops {
			seq := append(append([]int(nil), cur.seq...), i)
			l, clock := newLedgerUnderTest(c)
			for _, j := range seq {
				ops[j].apply(l, &clock, model.file.MaxHeld)
			}
			shape := ledgerShape(l, clock, key)
			if !seen[shape] {
				seen[shape] = true
				queue = append(queue, node{seq: seq, shape: shape})
			}
		}
	}

	require.Positive(t, c.steps, "the driver never called the ledger")
	c.requireEveryModelEdgeExercised()
}

// ledgerShape renders a ledger's occurrences for one key as the remaining
// lifetime of each, with everything already expired collapsed into a single
// marker (expired occurrences are interchangeable — Consume drops them all
// and Forget only ever reaches one when nothing live is left).
func ledgerShape(l *ownSendLedger, now time.Time, key ownSendKey) string {
	expired := 0
	var remaining []int
	for _, expiry := range l.entries[key] {
		if now.After(expiry) {
			expired++
			continue
		}
		remaining = append(remaining, int(expiry.Sub(now)/time.Second))
	}
	sort.Ints(remaining)
	return fmt.Sprintf("expired=%d live=%v", expired, remaining)
}

// ── Driver 2: exhaustive short sequences over two interleaved keys ────────

// TestOwnSendLedgerConformsOverEveryInterleavingOfTwoKeys is the other half:
// driver 1 covers every transition but on one key at a time, so it can say
// nothing about keys interfering with each other. This runs every operation
// sequence of a fixed length over two keys, which is where the tracer's
// "no other key changed" rule does its work — the regression the hand-written
// TestOwnSendLedger_Forget_OnlyRemovesMatchingKey covers for exactly one
// scenario, checked here for all of them.
func TestOwnSendLedgerConformsOverEveryInterleavingOfTwoKeys(t *testing.T) {
	model := loadLedgerModel(t)
	c := newLedgerConformance(t, model)

	ops := ledgerOps([]ownSendKey{ledgerModelKey("pane-x"), ledgerModelKey("pane-y")})

	const depth = 4
	seq := make([]int, depth)
	var walk func(pos int)
	walk = func(pos int) {
		if pos == depth {
			c.replay(ops, seq, model.file.MaxHeld)
			return
		}
		for i := range ops {
			seq[pos] = i
			walk(pos + 1)
		}
	}
	walk(0)

	require.Positive(t, c.steps)
}

// ── Driver 3: trace conformance through the relay's real call sites ───────

// TestRelayOwnSendLedgerUseConformsToModel is the trace-conformance case
// proper: nothing here drives the ledger directly. Relay.Broadcast and
// Relay.Poll call it themselves — Record before a Send, Forget when that Send
// fails, Consume when a polled-back row claims From == SystemID — and the
// transitions those calls produce are checked against the same model. It is
// the relay's own use of the ledger that has to conform, not just the ledger
// in isolation.
func TestRelayOwnSendLedgerUseConformsToModel(t *testing.T) {
	model := loadLedgerModel(t)
	c := newLedgerConformance(t, model)

	hostA := &fakeAgmsgClient{hostID: "host-a"}
	hostB := &fakeAgmsgClient{hostID: "host-b"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{
		"host-a": hostA, "host-b": hostB,
	}, map[string]string{"pane-a": "host-a", "pane-b": "host-b"})
	c.watch(r.ledger)

	ctx := context.Background()

	// Two identical broadcasts: the duplicate-send case PR #167's review round
	// found the multiset bug in, driven through the relay rather than the
	// ledger's own API.
	_, err := r.Broadcast(ctx, SystemID, []string{"pane-b"}, "hello")
	require.NoError(t, err)
	_, err = r.Broadcast(ctx, SystemID, []string{"pane-b"}, "hello")
	require.NoError(t, err)

	// A failing Send: Record then Forget, closing the forgery window.
	hostB.sendErr = errors.New("agmsg send failed")
	_, err = r.Broadcast(ctx, SystemID, []string{"pane-b"}, "hello")
	require.Error(t, err)
	hostB.sendErr = nil

	// Polling host-b back sees both delivered rows claiming From == SystemID,
	// plus one forged row with the same shape that must not match.
	hostB.sinceRows = []Row{
		{ID: "1", Team: "panemux", From: SystemID, To: "pane-b", Body: "hello"},
		{ID: "2", Team: "panemux", From: SystemID, To: "pane-b", Body: "hello"},
		{ID: "3", Team: "panemux", From: SystemID, To: "pane-b", Body: "hello"},
	}
	require.NoError(t, r.Poll(ctx))

	// Broadcasting as a real pane touches the ledger not at all; polling that
	// row back must therefore not consume anything either.
	_, err = r.Broadcast(ctx, "pane-a", []string{"pane-b"}, "from a pane")
	require.NoError(t, err)

	require.Positive(t, c.steps, "the relay never reached the ledger")
	require.Len(t, r.cache.MessagesSince(0), 2,
		"exactly the two genuinely-sent rows should have survived validFrom")
}
