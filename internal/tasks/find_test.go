package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FindTask (issue #283) collects only the host a task ID names, so the
// board's attach is decided from a fresh collection rather than a list the
// browser may hold from before the task moved or ended.
func TestFindTask(t *testing.T) {
	newService := func(local []byte, localErr error, conns map[string]*fakeConn) (*Service, map[string]int) {
		dials := map[string]int{}
		svc := New(Options{
			Hosts: func() []string { return []string{"dev", "dev-2"} },
			Dial: func(name string) (Conn, error) {
				dials[name]++
				conn, ok := conns[name]
				if !ok {
					return nil, errors.New("i/o timeout")
				}
				return conn, nil
			},
			RunLocal: localOutput(local, localErr),
		})
		t.Cleanup(svc.Close)
		return svc, dials
	}

	t.Run("a local task", func(t *testing.T) {
		svc, dials := newService(minimalOutput("local-sess"), nil, nil)
		task, err := svc.FindTask(context.Background(), "local:claude:local-sess")
		require.NoError(t, err)
		assert.Equal(t, "local:claude:local-sess", task.ID)
		assert.Empty(t, task.Host)
		assert.Empty(t, dials, "a local task must not reach any ssh host")
	})

	t.Run("an ssh task is looked up only on its own host", func(t *testing.T) {
		svc, dials := newService(nil, errors.New("must not run"), map[string]*fakeConn{
			"dev-2": {output: minimalOutput("remote-sess")},
		})
		task, err := svc.FindTask(context.Background(), "ssh:dev-2:claude:remote-sess")
		require.NoError(t, err)
		assert.Equal(t, "dev-2", task.Host)
		assert.Equal(t, map[string]int{"dev-2": 1}, dials, `"dev" is a prefix of "dev-2" but not its host`)
	})

	t.Run("a task the host no longer reports", func(t *testing.T) {
		svc, _ := newService(minimalOutput("other"), nil, nil)
		_, err := svc.FindTask(context.Background(), "local:claude:gone")
		assert.ErrorIs(t, err, ErrTaskNotFound)
	})

	t.Run("an ID naming no configured host", func(t *testing.T) {
		svc, dials := newService(minimalOutput("x"), nil, nil)
		for _, id := range []string{"ssh:unknown:claude:x", "bogus", "", "ssh:dev"} {
			_, err := svc.FindTask(context.Background(), id)
			assert.ErrorIs(t, err, ErrTaskNotFound, id)
		}
		assert.Empty(t, dials)
	})

	t.Run("a host that cannot be collected", func(t *testing.T) {
		svc, _ := newService(nil, nil, nil)
		_, err := svc.FindTask(context.Background(), "ssh:dev:claude:x")
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrTaskNotFound)
		assert.ErrorIs(t, err, ErrHostUnavailable)
	})
}
