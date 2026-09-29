package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_AddAndGet(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)
	got, ok := m.Get("sess1")
	require.True(t, ok)
	assert.Equal(t, "sess1", got.ID())
}

func TestManager_GetNonexistent(t *testing.T) {
	m := NewManager()
	_, ok := m.Get("missing")
	assert.False(t, ok)
}

func TestManager_List(t *testing.T) {
	m := NewManager()
	m.Add(newMock("a"))
	m.Add(newMock("b"))
	list := m.List()
	assert.Len(t, list, 2)
}

func TestManager_Remove_ClosesSession(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)
	err := m.Remove("sess1")
	require.NoError(t, err)
	assert.True(t, s.closed)
	_, ok := m.Get("sess1")
	assert.False(t, ok)
}

func TestManager_Remove_Nonexistent(t *testing.T) {
	m := NewManager()
	err := m.Remove("missing")
	assert.Error(t, err)
}

func TestManager_CloseAll(t *testing.T) {
	m := NewManager()
	s1 := newMock("a")
	s2 := newMock("b")
	m.Add(s1)
	m.Add(s2)
	m.CloseAll()
	assert.True(t, s1.closed)
	assert.True(t, s2.closed)
	assert.Empty(t, m.List())
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("sess%d", i)
			s := newMock(id)
			m.Add(s)
			m.Get(id)
			m.List()
		}(i)
	}
	wg.Wait()
}

func TestManager_Subscribe_ReplaysBufferedOutputAndStreamsNewOutput(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)

	_, err := s.Write([]byte("before"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		snapshot, updates, unsubscribe, ok := m.Subscribe("sess1")
		if !ok {
			return false
		}
		defer unsubscribe()

		if string(snapshot) != "before" {
			return false
		}

		_, err = s.Write([]byte("after"))
		require.NoError(t, err)

		select {
		case got := <-updates:
			return string(got) == "after"
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestManager_Subscribe_ClosedSessionReturnsSnapshotAndClosedStream(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)

	_, err := s.Write([]byte("before close"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, _, unsubscribe, ok := m.Subscribe("sess1")
		if !ok {
			return false
		}
		defer unsubscribe()
		return string(snapshot) == "before close"
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, s.Close())

	require.Eventually(t, func() bool {
		snapshot, updates, unsubscribe, ok := m.Subscribe("sess1")
		if !ok {
			return false
		}
		defer unsubscribe()

		if string(snapshot) != "before close" {
			return false
		}

		select {
		case _, stillOpen := <-updates:
			return !stillOpen
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

// TestManagedSession_Publish_ReplayBufferBound pins the replay buffer's own
// bound. Nothing in this suite published more than a few bytes before, so the
// truncation arithmetic ran only in the benchmark — the buffer could have been
// dropping the wrong end, or slicing out of range on the first pane that ever
// filled it, with the suite still green. Issue #190.
// Nothing under this test changed on this branch, so the red-check could never
// see it go red: it pins behavior that was already correct and merely
// unasserted. See docs/quality-gateway.md's "Clearing the boundary-value class".
//
//efficacy:exempt pins pre-existing behavior; no implementation under it changed
func TestManagedSession_Publish_ReplayBufferBound(t *testing.T) {
	tests := []struct {
		name      string
		published int
		wantLen   int
		// wantFirst is the byte the retained history must start with, which
		// is what says the OLDEST output was dropped and not the newest.
		wantFirst byte
	}{
		{
			name:      "one short of the bound keeps everything",
			published: sessionReplayLimitBytes - 1,
			wantLen:   sessionReplayLimitBytes - 1,
			wantFirst: 'a',
		},
		{
			name:      "exactly the bound keeps everything",
			published: sessionReplayLimitBytes,
			wantLen:   sessionReplayLimitBytes,
			wantFirst: 'a',
		},
		{
			name:      "one past the bound drops exactly one byte",
			published: sessionReplayLimitBytes + 1,
			wantLen:   sessionReplayLimitBytes,
			wantFirst: 'b',
		},
		{
			name:      "well past the bound keeps only the newest window",
			published: sessionReplayLimitBytes + 4096,
			wantLen:   sessionReplayLimitBytes,
			wantFirst: 'b',
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &managedSession{subscribers: map[int]chan []byte{}}
			// A distinguishable first byte, then filler: if truncation kept
			// the wrong end, the retained history still starts with 'a'.
			chunk := make([]byte, tt.published)
			chunk[0] = 'a'
			for i := 1; i < len(chunk); i++ {
				chunk[i] = 'b'
			}
			entry.publish(chunk)

			snapshot, _, unsubscribe := entry.subscribe()
			unsubscribe()
			if len(snapshot) != tt.wantLen {
				t.Fatalf("replay buffer length after publishing %d bytes = %d, want %d",
					tt.published, len(snapshot), tt.wantLen)
			}
			if snapshot[0] != tt.wantFirst {
				t.Fatalf("replay buffer starts with %q, want %q — the wrong end was dropped",
					snapshot[0], tt.wantFirst)
			}
		})
	}
}

// TestManagedSession_Publish_ReplayBufferBoundAcrossChunks pins the same bound
// when the buffer crosses it over several publishes, which is how a real pane
// reaches it — pump() writes 4 KiB at a time.
// Nothing under this test changed on this branch, so the red-check could never
// see it go red: it pins behavior that was already correct and merely
// unasserted. See docs/quality-gateway.md's "Clearing the boundary-value class".
//
//efficacy:exempt pins pre-existing behavior; no implementation under it changed
func TestManagedSession_Publish_ReplayBufferBoundAcrossChunks(t *testing.T) {
	entry := &managedSession{subscribers: map[int]chan []byte{}}
	chunk := make([]byte, 4096)
	for i := range chunk {
		chunk[i] = 'x'
	}
	for published := 0; published < sessionReplayLimitBytes+2*len(chunk); published += len(chunk) {
		entry.publish(append([]byte(nil), chunk...))
	}

	snapshot, _, unsubscribe := entry.subscribe()
	unsubscribe()
	if len(snapshot) != sessionReplayLimitBytes {
		t.Fatalf("replay buffer length = %d, want it held at %d", len(snapshot), sessionReplayLimitBytes)
	}
}

func TestManagedSession_SubscribeStillWorksWhilePublishWaitsOnSlowSubscriber(t *testing.T) {
	slowSubscriber := make(chan []byte, 1)
	slowSubscriber <- []byte("already full")

	entry := &managedSession{
		subscribers: map[int]chan []byte{
			0: slowSubscriber,
		},
	}

	go entry.publish([]byte("next chunk"))

	done := make(chan struct{})
	go func() {
		_, updates, unsubscribe := entry.subscribe()
		unsubscribe()
		for {
			// A publish racing with unsubscribe may enqueue one last update before
			// the subscription is removed. The important invariant is that
			// subscribe/unsubscribe never blocks behind another slow subscriber and
			// the stream closes promptly once drained.
			if _, ok := <-updates; !ok {
				break
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscribe blocked behind a slow subscriber")
	}
}

// TestManagedSession_Publish_SteadyStateDoesNotCopyTheReplayWindow is issue
// #193's completion condition expressed as a test rather than as a benchmark
// number. `make bench` found publish allocating ~598KB per 4KB chunk once the
// replay window was full, because retaining the window meant reallocating and
// copying it every time; the ring buffer makes that constant. With no
// subscribers to fan out to, a steady-state publish has nothing left to
// allocate.
//
// It asserts a shape (zero allocations), not a duration, so it is not a
// benchmark in disguise: docs/quality-gateway.md's principle 4 rules out gating
// on timings measured in a shared container, and the spread there is 2.9×.
// Allocation counts have no such spread.
func TestManagedSession_Publish_SteadyStateDoesNotCopyTheReplayWindow(t *testing.T) {
	entry := &managedSession{subscribers: map[int]chan []byte{}}
	chunk := make([]byte, 4096)
	for published := 0; published < sessionReplayLimitBytes; published += len(chunk) {
		entry.publish(chunk)
	}

	allocs := testing.AllocsPerRun(100, func() { entry.publish(chunk) })
	if allocs != 0 {
		t.Fatalf("publish allocated %.0f times per 4KB chunk in steady state, want 0 — "+
			"the replay window is still being copied per chunk (issue #193)", allocs)
	}
}

// TestManagedSession_Publish_DeliversTheChunkWithAFullReplayWindow pins that
// the fan-out still carries the right bytes once the ring has wrapped. The
// subscriber gets its own copy of the chunk, not a view onto ring storage that
// later output overwrites underneath it.
func TestManagedSession_Publish_DeliversTheChunkWithAFullReplayWindow(t *testing.T) {
	entry := &managedSession{subscribers: map[int]chan []byte{}}
	filler := make([]byte, 4096)
	for i := range filler {
		filler[i] = 'x'
	}
	for published := 0; published < sessionReplayLimitBytes; published += len(filler) {
		entry.publish(filler)
	}

	_, updates, unsubscribe := entry.subscribe()
	defer unsubscribe()

	entry.publish([]byte("newest"))
	entry.publish(filler)

	select {
	case got := <-updates:
		if string(got) != "newest" {
			t.Fatalf("subscriber received %q, want %q", got, "newest")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber received nothing after a publish onto a full replay window")
	}
}
