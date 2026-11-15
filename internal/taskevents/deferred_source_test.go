package taskevents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"panemux/internal/tasks"
)

type deferredTestSource struct {
	*fakeSource
	accepted chan string
}

func (s *deferredTestSource) CollectHostLiveDeferred(
	ctx context.Context, name string,
) (tasks.HostResult, []tasks.Task, func()) {
	result, list := s.CollectHostLive(ctx, name)
	return result, list, func() { s.accepted <- name }
}

func TestPublisherCommitsOnlyAcceptedDeferredObservations(t *testing.T) {
	for _, mode := range []string{"accepted", "removed", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			src := &deferredTestSource{fakeSource: newFakeSource("build"), accepted: make(chan string, 1)}
			p := New(src, Options{Interval: time.Hour})
			t.Cleanup(p.Close)
			p.model.syncHosts([]string{"build"})
			p.observing = true
			p.stop = make(chan struct{})
			p.looping["build"] = true
			done := make(chan struct{})
			go func() { defer close(done); p.observeHost("build") }()
			src.awaitStart(t, "build")
			p.mu.Lock()
			switch mode {
			case "removed":
				p.model.syncHosts(nil)
			case "stopped":
				p.observing = false
			}
			p.mu.Unlock()
			src.answerChan("build") <- hostAnswer{result: okHost("build")}
			if mode == "accepted" {
				select {
				case name := <-src.accepted:
					assert.Equal(t, "build", name)
				case <-time.After(5 * time.Second):
					t.Fatal("accepted result did not commit")
				}
				p.Close()
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("discarded or stopped observation did not finish")
			}
			assert.Empty(t, src.accepted, "discarded results must not commit")
		})
	}
}
