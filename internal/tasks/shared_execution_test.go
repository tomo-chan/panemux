package tasks

import (
	"context"
	"github.com/stretchr/testify/require"
	"panemux/internal/hostexec"
	"testing"
	"time"
)

func TestProbeKeepsOnlyCurrentSharedConnectionResult(t *testing.T) {
	for _, state := range []string{"panemux", "current", "replaced", "removed"} {
		t.Run(state, func(t *testing.T) {
			answer := probeAnswer("cli claude found", "cli codex found")
			current := &fakeConn{output: answer}
			pool := hostexec.New(hostexec.Options{Hosts: func() []string { return []string{"dev"} },
				Dial: func(string) (hostexec.Conn, error) { return current, nil }})
			defer pool.Close()
			svc := New(Options{Execution: pool, RunLocal: func(context.Context, string) ([]byte, error) { return answer, nil }})
			defer svc.Close()
			name := "dev"
			old := current
			if state == "panemux" {
				name = ""
			} else {
				_, err := pool.Connect(context.Background(), name)
				require.NoError(t, err)
				if state == "replaced" {
					require.NoError(t, pool.Reconnect(name))
					current = &fakeConn{output: answer}
					_, err = pool.Connect(context.Background(), name)
					require.NoError(t, err)
				}
				if state == "removed" {
					pool.Retain(nil)
				}
			}
			svc.probeRuns.Add(1)
			svc.probeHost(name, old, time.Now())
			svc.mu.Lock()
			_, found := svc.probes[name]
			svc.mu.Unlock()
			require.Equal(t, state == "panemux" || state == "current", found)
		})
	}
}
