package session

// replayBufferInitialBytes is the first allocation a pane's replay buffer makes.
// It matches pump()'s read size, so an ordinary pane's first chunk fits without
// a regrow, while a pane that produces almost nothing — a workspace can hold
// dozens — never pays for the whole window.
const replayBufferInitialBytes = 4096

// replayBuffer retains the newest `limit` bytes a session has produced.
//
// It is a ring: appending writes into storage the buffer already owns and moves
// two indices, so the steady-state cost is the length of the chunk rather than
// the length of the window. The slice it replaced re-sliced the whole window
// into a fresh allocation on every chunk, which `make bench` measured at ~598KB
// and two orders of magnitude of time per 4KB of pane output — issue #193.
//
// It is not safe for concurrent use; managedSession holds its mutex around
// every call.
type replayBuffer struct {
	buf   []byte
	start int // index of the oldest retained byte
	size  int // number of retained bytes
	limit int // most bytes ever retained; storage never grows past it
}

func newReplayBuffer(limit int) *replayBuffer {
	return &replayBuffer{limit: limit}
}

// len reports how many bytes a snapshot would return.
func (r *replayBuffer) len() int { return r.size }

// capacity reports how much storage the buffer currently holds. It exists for
// the tests that pin both ends of the growth rule — never past the limit, and
// not the whole limit for a pane that has barely spoken.
func (r *replayBuffer) capacity() int { return len(r.buf) }

// reset drops everything retained, keeping the storage for reuse.
func (r *replayBuffer) reset() {
	r.start = 0
	r.size = 0
}

// append records chunk, dropping the oldest bytes once the limit is reached.
func (r *replayBuffer) append(chunk []byte) {
	if len(chunk) == 0 || r.limit == 0 {
		return
	}

	// A chunk at least as large as the window makes everything already retained
	// unreachable, so there is nothing to preserve and no wrap to compute.
	//
	//mutation:exempt equivalent — at len(chunk) == limit the path below keeps chunk's own bytes too
	if len(chunk) >= r.limit {
		r.reserve(r.limit)
		copy(r.buf, chunk[len(chunk)-r.limit:])
		r.start = 0
		r.size = r.limit
		return
	}

	want := r.size + len(chunk)
	if want > r.limit {
		want = r.limit
	}
	r.reserve(want)

	// reserve guarantees len(r.buf) >= len(chunk), so at most one wrap occurs.
	end := (r.start + r.size) % len(r.buf)
	written := copy(r.buf[end:], chunk)
	copy(r.buf, chunk[written:])

	r.size += len(chunk)
	if overflow := r.size - len(r.buf); overflow > 0 {
		r.start = (r.start + overflow) % len(r.buf)
		r.size = len(r.buf)
	}
}

// snapshot returns the retained bytes, oldest first, in a slice the caller owns.
// Subscribers keep it while the pane goes on producing output, so it must not
// alias the ring's storage.
//
// It appends into a nil slice rather than copying into a make()d one, because
// make zeroes the whole allocation before the copy overwrites it — a second
// pass over 256KB on the path a workspace switch pays per remounted pane. The
// two forms did not separate from container noise at -count 5, so this is
// chosen on the work it avoids, not on a measured gap.
func (r *replayBuffer) snapshot() []byte {
	if r.size == 0 {
		return nil
	}
	if end := r.start + r.size; end <= len(r.buf) {
		return append([]byte(nil), r.buf[r.start:end]...)
	}
	out := append([]byte(nil), r.buf[r.start:]...)
	return append(out, r.buf[:r.start+r.size-len(r.buf)]...)
}

// copyTo writes the retained bytes, oldest first, into dst.
func (r *replayBuffer) copyTo(dst []byte) {
	if r.size == 0 {
		return
	}
	if end := r.start + r.size; end <= len(r.buf) {
		copy(dst, r.buf[r.start:end])
		return
	}
	n := copy(dst, r.buf[r.start:])
	copy(dst[n:], r.buf[:r.start+r.size-len(r.buf)])
}

// reserve grows storage to hold at least n bytes, capped at the limit.
//
// Growth doubles, so a pane reaches the full window in a handful of copies and
// then never allocates again. It also linearizes: the retained bytes move to
// the front of the new storage, which is why append's wrap arithmetic only ever
// has to handle one wrap.
func (r *replayBuffer) reserve(n int) {
	if len(r.buf) >= n {
		return
	}

	grown := len(r.buf)
	if grown < replayBufferInitialBytes {
		grown = replayBufferInitialBytes
	}
	for grown < n {
		grown *= 2
	}
	if grown > r.limit {
		grown = r.limit
	}

	next := make([]byte, grown)
	r.copyTo(next)
	r.buf = next
	r.start = 0
	// r.size is unchanged: growth only ever happens when everything retained
	// still fits, since a chunk large enough to evict it takes append's
	// oversized-chunk path instead.
}
