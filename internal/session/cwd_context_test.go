package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type cwdRunner struct {
	output func(string) ([]byte, error)
	closed chan struct{}
	once   sync.Once
}

func (s *cwdRunner) Output(cmd string) ([]byte, error) { return s.output(cmd) }
func (s *cwdRunner) Close() error                      { s.once.Do(func() { close(s.closed) }); return nil }

func awaitCWDSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("CWD operation did not finish")
	}
}

func TestRemoteCWDCancellationReclaimsLateSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	sess := &cwdRunner{closed: make(chan struct{}), output: func(string) ([]byte, error) {
		t.Error("canceled late session executed")
		return nil, nil
	}}
	go func() {
		cwd, err := remoteCWD(ctx, func() (sshSessionRunner, error) { close(entered); <-release; return sess, nil }, "pwd")
		if cwd != "" || !errors.Is(err, context.Canceled) {
			t.Errorf("canceled lookup: %q %v", cwd, err)
		}
		close(done)
	}()
	awaitCWDSignal(t, entered)
	cancel()
	awaitCWDSignal(t, done)
	unblock()
	awaitCWDSignal(t, sess.closed)
}

func TestRemoteCWDExecCancellationAndLateResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	sess := &cwdRunner{closed: make(chan struct{}), output: func(string) ([]byte, error) {
		close(entered)
		<-release
		return []byte("/workspace/late"), nil
	}}
	go func() {
		cwd, err := remoteCWD(ctx, func() (sshSessionRunner, error) { return sess, nil }, "pwd")
		if cwd != "" || !errors.Is(err, context.Canceled) {
			t.Errorf("canceled lookup: %q %v", cwd, err)
		}
		close(done)
	}()
	awaitCWDSignal(t, entered)
	cancel()
	awaitCWDSignal(t, done)
	awaitCWDSignal(t, sess.closed)
	unblock()
}

func TestRemoteCWDResultsAndPreCancellation(t *testing.T) {
	wantErr := errors.New("fixture error")
	for _, stage := range []string{"success", "open-error", "exec-error", "pre-cancel", "cancel-with-result"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "pre-cancel" {
				cancel()
			}
			sess := &cwdRunner{closed: make(chan struct{}), output: func(cmd string) ([]byte, error) {
				if cmd != "pwd" {
					t.Errorf("command: %q", cmd)
				}
				if stage == "exec-error" {
					return nil, wantErr
				}
				if stage == "cancel-with-result" {
					cancel()
				}
				return []byte(" /workspace/project\n"), nil
			}}
			cwd, err := remoteCWD(ctx, func() (sshSessionRunner, error) {
				if stage == "pre-cancel" {
					t.Error("pre-cancel opened channel")
				}
				if stage == "open-error" {
					return nil, wantErr
				}
				return sess, nil
			}, "pwd")
			switch stage {
			case "success":
				if cwd != "/workspace/project" || err != nil {
					t.Fatalf("result: %q %v", cwd, err)
				}
			case "pre-cancel", "cancel-with-result":
				if cwd != "" || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel: %q %v", cwd, err)
				}
			default:
				if !errors.Is(err, wantErr) {
					t.Fatal(err)
				}
			}
			if stage != "pre-cancel" && stage != "open-error" {
				awaitCWDSignal(t, sess.closed)
			}
		})
	}
}
