package tasks

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"panemux/internal/host"
	"testing"
)

func TestAfterCollectionNotifiesOnlySuccessfulCollections(t *testing.T) {
	for _, mode := range []string{"full", "live"} {
		for _, outcome := range []string{"success", "run failure", "parse failure"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				notifications := make(chan host.Host, 4)
				s := New(Options{
					AfterCollection: func(h host.Host) { notifications <- h },
					RunLocal: func(_ context.Context, _ string) ([]byte, error) {
						if outcome == "run failure" {
							return nil, errors.New("offline")
						}
						if outcome == "parse failure" {
							return []byte("cut short"), nil
						}
						return fullOutput("::panemux-tasks v2", "::now 1000", "::end"), nil
					}})
				defer s.Close()
				if mode == "full" {
					s.Collect(context.Background())
				} else {
					s.CollectAttention(context.Background())
				}
				if outcome == "success" {
					require.Len(t, notifications, 1)
					require.Equal(t, host.Local, <-notifications)
				} else {
					require.Empty(t, notifications)
				}
			})
		}
	}
}
