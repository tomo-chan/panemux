package session

import (
	"bytes"
	"testing"
)

// The replay buffer is the state every byte a pane produces passes through, and
// until issue #193 it was a plain slice that reallocated and copied the whole
// 256KB window on every chunk. These tests pin the two things that replacing it
// with a ring must not change — which bytes are retained, and that a snapshot
// handed to a subscriber is its own copy — plus the one thing the change exists
// for: a steady-state append that allocates nothing.
//
// They use a small limit deliberately. Wrap-around is the case a ring buffer
// gets wrong, and at the real 256KB limit a test would have to publish
// megabytes to reach it even once.

// replayString is the readable form of what a subscriber would replay.
func replayString(r *replayBuffer) string { return string(r.snapshot()) }

// replayBufferRetentionCases is hoisted out of its test so the test itself
// stays inside the linter's function-length limit; the field order is the one
// govet's fieldalignment asks for, not a meaningful one.
var replayBufferRetentionCases = []struct {
	name   string
	want   string
	chunks []string
	limit  int
}{
	{
		name:   "empty buffer replays nothing",
		limit:  8,
		chunks: nil,
		want:   "",
	},
	{
		name:   "below the limit keeps everything",
		limit:  8,
		chunks: []string{"abc"},
		want:   "abc",
	},
	{
		name:   "exactly the limit keeps everything",
		limit:  8,
		chunks: []string{"abcdefgh"},
		want:   "abcdefgh",
	},
	{
		name:   "one past the limit drops exactly the oldest byte",
		limit:  8,
		chunks: []string{"abcdefghi"},
		want:   "bcdefghi",
	},
	{
		name:   "a chunk larger than the limit keeps only its own tail",
		limit:  4,
		chunks: []string{"abcdefghij"},
		want:   "ghij",
	},
	{
		name:   "several chunks accumulate in order",
		limit:  8,
		chunks: []string{"ab", "cd", "ef"},
		want:   "abcdef",
	},
	{
		name:   "several chunks crossing the limit drop the oldest end",
		limit:  8,
		chunks: []string{"abcd", "efgh", "ijkl"},
		want:   "efghijkl",
	},
	{
		name:   "a chunk that wraps past the physical end stays in order",
		limit:  8,
		chunks: []string{"abcdef", "ghijk"},
		want:   "defghijk",
	},
	{
		name:   "an oversized chunk resets a full buffer to its own tail",
		limit:  4,
		chunks: []string{"abcd", "wxyz!"},
		want:   "xyz!",
	},
	{
		name:   "an empty chunk changes nothing",
		limit:  8,
		chunks: []string{"abc", "", "de"},
		want:   "abcde",
	},
	{
		name:   "a zero limit retains nothing",
		limit:  0,
		chunks: []string{"abc"},
		want:   "",
	},
}

func TestReplayBuffer_RetainsNewestBytes(t *testing.T) {
	for _, tt := range replayBufferRetentionCases {
		t.Run(tt.name, func(t *testing.T) {
			r := newReplayBuffer(tt.limit)
			for _, chunk := range tt.chunks {
				r.append([]byte(chunk))
			}
			if got := replayString(r); got != tt.want {
				t.Fatalf("snapshot = %q, want %q", got, tt.want)
			}
			if got := r.len(); got != len(tt.want) {
				t.Fatalf("len = %d, want %d", got, len(tt.want))
			}
		})
	}
}

// TestReplayBuffer_MatchesTailOfEverythingWritten drives the buffer through
// enough differently sized appends to wrap it many times over, and compares it
// against the naive "keep the last limit bytes of everything" model the old
// slice implementation was. Every retained byte is distinguishable, so an
// off-by-one in the wrap arithmetic shows up as a mismatch rather than as a
// correct-looking length.
func TestReplayBuffer_MatchesTailOfEverythingWritten(t *testing.T) {
	const limit = 64
	r := newReplayBuffer(limit)

	var want []byte
	var next byte
	// Sizes deliberately include ones below, at, and above the limit, and ones
	// that are not divisors of it, so the write position lands everywhere.
	for _, size := range []int{1, 7, 3, 64, 5, 63, 65, 2, 31, 33, 128, 1, 17, 64, 9} {
		chunk := make([]byte, size)
		for i := range chunk {
			chunk[i] = next
			next++
		}
		r.append(chunk)

		want = append(want, chunk...)
		if len(want) > limit {
			want = want[len(want)-limit:]
		}
		if got := r.snapshot(); !bytes.Equal(got, want) {
			t.Fatalf("after appending %d bytes: snapshot = %v, want %v", size, got, want)
		}
	}
}

// TestReplayBuffer_SnapshotIsIndependentOfLaterAppends is the aliasing bug a
// ring buffer makes possible and a fresh slice per publish did not: a snapshot
// is handed to a subscriber goroutine, so it must not be a view onto storage
// that later output overwrites in place.
func TestReplayBuffer_SnapshotIsIndependentOfLaterAppends(t *testing.T) {
	r := newReplayBuffer(8)
	r.append([]byte("abcdefgh"))

	snapshot := r.snapshot()
	r.append([]byte("ijkl"))

	if string(snapshot) != "abcdefgh" {
		t.Fatalf("snapshot changed under the caller: got %q, want %q", snapshot, "abcdefgh")
	}
	if got := replayString(r); got != "efghijkl" {
		t.Fatalf("buffer after the later append = %q, want %q", got, "efghijkl")
	}

	// The reverse direction: a caller writing into its own snapshot must not
	// corrupt what the next subscriber replays.
	for i := range snapshot {
		snapshot[i] = '!'
	}
	if got := replayString(r); got != "efghijkl" {
		t.Fatalf("buffer after the caller wrote into its snapshot = %q, want %q", got, "efghijkl")
	}
}

// TestReplayBuffer_CapacityNeverExceedsLimit pins the memory bound. The old
// implementation kept exactly one window; a ring that grows geometrically must
// stop at the limit rather than doubling past it.
func TestReplayBuffer_CapacityNeverExceedsLimit(t *testing.T) {
	const limit = 100
	r := newReplayBuffer(limit)
	chunk := make([]byte, 7)
	for i := 0; i < 200; i++ {
		r.append(chunk)
		if got := r.capacity(); got > limit {
			t.Fatalf("capacity grew to %d, past the %d-byte limit", got, limit)
		}
	}
	if got := r.len(); got != limit {
		t.Fatalf("len = %d, want the buffer held at %d", got, limit)
	}
}

// TestReplayBuffer_CapacityStaysProportionalWhileSmall pins the other half of
// that: a pane that has only ever produced a few bytes must not be charged a
// full 256KB window up front, since a workspace can hold dozens of panes.
func TestReplayBuffer_CapacityStaysProportionalWhileSmall(t *testing.T) {
	r := newReplayBuffer(sessionReplayLimitBytes)
	r.append([]byte("a short prompt\r\n"))

	if got := r.capacity(); got >= sessionReplayLimitBytes {
		t.Fatalf("capacity = %d after 16 bytes; a barely-used pane must not hold the whole %d-byte window",
			got, sessionReplayLimitBytes)
	}
}

func TestReplayBuffer_ResetDropsEverything(t *testing.T) {
	r := newReplayBuffer(8)
	r.append([]byte("abcdefghij"))
	r.reset()

	if got := replayString(r); got != "" {
		t.Fatalf("snapshot after reset = %q, want empty", got)
	}
	r.append([]byte("xy"))
	if got := replayString(r); got != "xy" {
		t.Fatalf("snapshot after reset and append = %q, want %q", got, "xy")
	}
}

// TestReplayBuffer_SteadyStateAppendDoesNotAllocate is the reason issue #193
// exists. Once the window is full the old implementation reallocated and copied
// all 256KB per chunk — two orders of magnitude more expensive than the same
// call on a cold buffer. A ring reuses its storage, so the steady-state append
// must allocate nothing at all.
func TestReplayBuffer_SteadyStateAppendDoesNotAllocate(t *testing.T) {
	r := newReplayBuffer(sessionReplayLimitBytes)
	chunk := make([]byte, 4096)
	// Fill the window first: growth allocates, and that is not what is measured.
	for filled := 0; filled < sessionReplayLimitBytes; filled += len(chunk) {
		r.append(chunk)
	}

	allocs := testing.AllocsPerRun(100, func() { r.append(chunk) })
	if allocs != 0 {
		t.Fatalf("steady-state append allocated %.0f times per call, want 0 — the window is still being copied", allocs)
	}
}
