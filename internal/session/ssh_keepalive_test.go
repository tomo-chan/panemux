package session

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKeepaliveSender struct {
	sendRequestFn func(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

func (f *fakeKeepaliveSender) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return f.sendRequestFn(name, wantReply, payload)
}

// testKeepaliveInterval/testKeepaliveProbeTimeout/testKeepaliveMaxFailures
// are passed explicitly into sshKeepaliveLoop rather than mutating the
// package-level sshKeepaliveInterval/sshKeepaliveProbeTimeout/
// sshKeepaliveMaxFailures vars, so a still-running loop goroutine from one
// test can never race a later test's (or the same test's t.Cleanup)
// assignment to those package vars.
const (
	testKeepaliveInterval     = 10 * time.Millisecond
	testKeepaliveProbeTimeout = 20 * time.Millisecond
	testKeepaliveMaxFailures  = 2
)

func runTestKeepaliveLoop(client sshKeepaliveSender, stop <-chan struct{}, onDead func()) {
	sshKeepaliveLoop(client, stop, onDead, testKeepaliveInterval, testKeepaliveProbeTimeout, testKeepaliveMaxFailures)
}

func TestSSHKeepaliveLoop_ConsecutiveSuccessesNeverCallOnDead(t *testing.T) {
	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			return false, nil, nil
		},
	}
	var onDeadCalls int32
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		runTestKeepaliveLoop(sender, stop, func() { atomic.AddInt32(&onDeadCalls, 1) })
		close(done)
	}()

	time.Sleep(15 * testKeepaliveInterval)
	assert.Zero(t, atomic.LoadInt32(&onDeadCalls))

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected the loop to exit after stop was closed")
	}
}

func TestSSHKeepaliveLoop_ConsecutiveFailuresCallOnDeadOnce(t *testing.T) {
	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			return false, nil, errors.New("connection reset")
		},
	}
	onDeadCh := make(chan struct{}, 10)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })

	go runTestKeepaliveLoop(sender, stop, func() { onDeadCh <- struct{}{} })

	select {
	case <-onDeadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected onDead to be called after sshKeepaliveMaxFailures consecutive failures")
	}

	// onDead should be called exactly once — the loop exits after triggering
	// it, it does not keep firing.
	time.Sleep(10 * testKeepaliveInterval)
	assert.Len(t, onDeadCh, 0, "onDead should fire exactly once; the loop must exit after triggering it")
}

func TestSSHKeepaliveLoop_SingleFailureThenSuccessResetsCounter(t *testing.T) {
	var callCount int32
	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			n := atomic.AddInt32(&callCount, 1)
			if n == 1 {
				return false, nil, errors.New("transient failure")
			}
			return false, nil, nil // recover on every subsequent probe
		},
	}
	var onDeadCalls int32
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })

	go runTestKeepaliveLoop(sender, stop, func() { atomic.AddInt32(&onDeadCalls, 1) })

	// Give it enough ticks that sshKeepaliveMaxFailures would have fired if
	// the single early failure weren't reset by the following success.
	time.Sleep(15 * testKeepaliveInterval)
	assert.Zero(t, atomic.LoadInt32(&onDeadCalls))
	assert.GreaterOrEqual(t, int(atomic.LoadInt32(&callCount)), 3)
}

func TestSSHKeepaliveLoop_StopClosesLoopBeforeNextTick(t *testing.T) {
	var callCount int32
	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			atomic.AddInt32(&callCount, 1)
			return false, nil, nil
		},
	}
	stop := make(chan struct{})

	go runTestKeepaliveLoop(sender, stop, func() {})
	time.Sleep(3 * testKeepaliveInterval)
	close(stop)
	// Allow whichever tick/stop the select was already racing between to
	// settle before sampling the count, since closing stop does not
	// retroactively cancel a select that had already chosen the ticker.C
	// branch on this iteration.
	time.Sleep(2 * testKeepaliveInterval)
	countAtStop := atomic.LoadInt32(&callCount)

	time.Sleep(10 * testKeepaliveInterval)
	assert.Equal(t, countAtStop, atomic.LoadInt32(&callCount), "no further probes should run after stop is closed")
}

func TestSSHKeepaliveProbe_TimesOutWhenSendRequestNeverReturns(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			<-block
			return false, nil, nil
		},
	}

	start := time.Now()
	ok := sshKeepaliveProbe(sender, 10*time.Millisecond)
	elapsed := time.Since(start)

	assert.False(t, ok)
	assert.Less(t, elapsed, time.Second)
}

func TestSSHKeepaliveProbe_TrueOnSuccessOrExpectedRequestFailure(t *testing.T) {
	// An unrecognized global request name gets SSH_MSG_REQUEST_FAILURE from a
	// compliant server, which the golang.org/x/crypto/ssh client surfaces as
	// (false, nil, nil) rather than an error — that reply is itself proof of
	// liveness, matching OpenSSH's ServerAliveInterval trick.
	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			require.Equal(t, "keepalive@panemux", name)
			return false, nil, nil
		},
	}
	assert.True(t, sshKeepaliveProbe(sender, time.Second))
}

// TestStartSSHKeepalive_UsesPackageLevelDefaults is a thin smoke test that
// startSSHKeepalive wires the package-level sshKeepaliveInterval/
// sshKeepaliveProbeTimeout/sshKeepaliveMaxFailures vars into the loop; the
// loop's actual branching logic is covered by the TestSSHKeepaliveLoop_*
// tests above via explicit test-local values.
func TestStartSSHKeepalive_UsesPackageLevelDefaults(t *testing.T) {
	sender := &fakeKeepaliveSender{
		sendRequestFn: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
			return false, nil, nil
		},
	}
	stop := make(chan struct{})

	startSSHKeepalive(sender, stop, func() {})
	close(stop) // exit promptly; this test only checks that it starts without panicking
	time.Sleep(10 * time.Millisecond)
}
