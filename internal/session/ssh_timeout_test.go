package session

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunOutputWithTimeout_ReturnsResultBeforeTimeout(t *testing.T) {
	outputFn := func(cmd string) ([]byte, error) { return []byte("ok: " + cmd), nil }
	closeCalled := false
	closeFn := func() error { closeCalled = true; return nil }

	out, err := runOutputWithTimeout(outputFn, closeFn, "echo hi", time.Second)
	require.NoError(t, err)
	assert.Equal(t, "ok: echo hi", string(out))
	assert.False(t, closeCalled, "close should not be called when the command completes in time")
}

func TestRunOutputWithTimeout_PropagatesUnderlyingError(t *testing.T) {
	wantErr := errors.New("boom")
	outputFn := func(cmd string) ([]byte, error) { return nil, wantErr }
	closeFn := func() error { return nil }

	_, err := runOutputWithTimeout(outputFn, closeFn, "echo hi", time.Second)
	assert.ErrorIs(t, err, wantErr)
}

func TestRunOutputWithTimeout_TimesOutAndClosesSession(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	outputFn := func(cmd string) ([]byte, error) {
		<-block // simulate a command that never returns
		return nil, nil
	}
	closeCalled := make(chan struct{}, 1)
	closeFn := func() error { closeCalled <- struct{}{}; return nil }

	start := time.Now()
	_, err := runOutputWithTimeout(outputFn, closeFn, "hung command", 10*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, time.Second, "runOutputWithTimeout should return promptly once the timeout fires")
	select {
	case <-closeCalled:
	case <-time.After(time.Second):
		t.Fatal("expected close to be called on timeout to unblock the underlying command")
	}
}

func TestRunOutputWithTimeout_CommandCompletesJustUnderTimeout(t *testing.T) {
	outputFn := func(cmd string) ([]byte, error) {
		time.Sleep(5 * time.Millisecond)
		return []byte("done"), nil
	}
	closeFn := func() error { return nil }

	out, err := runOutputWithTimeout(outputFn, closeFn, "echo hi", 200*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, "done", string(out))
}
