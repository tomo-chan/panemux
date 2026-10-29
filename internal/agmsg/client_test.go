package agmsg_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"log"
	"panemux/internal/agmsg"
	"panemux/internal/hostexec"
	"strings"
	"testing"
	"time"
)

type hostRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f hostRunnerFunc) Run(ctx context.Context, script string, args ...string) ([]byte, error) {
	return f(ctx, script, args...)
}
func captureBoardLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})
	return &buf
}

func TestHostAgmsgClientRetriesProbesAndSends(t *testing.T) {
	for _, failure := range []string{"home", "presence", "version", "none", "absent"} {
		t.Run(failure, func(t *testing.T) {
			logs := captureBoardLog(t)
			failed, sends := false, 0
			runner := hostRunnerFunc(func(_ context.Context, script string, args ...string) ([]byte, error) {
				require.Equal(t, `exec "$@"`, script)
				joined := strings.Join(args, " ")
				stage := "home"
				switch {
				case strings.Contains(joined, "board-agmsg-presence"):
					stage = "presence"
				case strings.Contains(joined, "board-agmsg-version"):
					stage = "version"
				case strings.Contains(joined, "board-send"):
					sends++
					require.Contains(t, joined, "/remote/home/demo/agmsg/scripts/send.sh")
					return nil, nil
				case strings.Contains(joined, "--limit"):
					return []byte("[]"), nil
				}
				if stage == failure && !failed {
					failed = true
					return nil, errors.New("temporarily offline")
				}
				switch stage {
				case "home":
					return []byte("/remote/home/demo"), nil
				case "presence":
					if failure == "absent" {
						return []byte("no"), nil
					}
					return []byte("yes"), nil
				default:
					return []byte(agmsg.TestedAgmsgVersion), nil
				}
			})
			c := agmsg.NewClient(agmsg.ClientConfig{HostID: "ssh:dev", Path: "~/agmsg", Runner: runner})
			_, err := c.Since(context.Background(), "panemux", "", 10)
			if failure == "home" || failure == "presence" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			_, err = c.Since(context.Background(), "panemux", "", 10)
			require.NoError(t, err)
			err = c.Send(context.Background(), "panemux", "pane-a", "pane-b", "hello; $(false)")
			if failure == "absent" {
				require.ErrorContains(t, err, "not installed")
				require.Contains(t, logs.String(), "no agmsg installation")
				require.Zero(t, sends)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, sends)
			}

			if failure == "version" {
				require.Contains(t, logs.String(), "reading agmsg version")
			}
		})
	}
}

func TestHostAgmsgOperationErrors(t *testing.T) {
	for _, operation := range []string{"ready", "read", "send"} {
		t.Run(operation, func(t *testing.T) {
			sentinel := errors.New("command failed")
			runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if operation == "ready" {
					return nil, sentinel
				}
				if strings.Contains(joined, "board-agmsg-presence") {
					return []byte("yes"), nil
				}
				if strings.Contains(joined, "board-agmsg-version") {
					return []byte(agmsg.TestedAgmsgVersion), nil
				}
				return nil, sentinel
			})
			c := agmsg.NewClient(agmsg.ClientConfig{HostID: "ssh:dev", Path: "/opt/agmsg", Runner: runner})
			if operation == "read" {
				_, err := c.Since(context.Background(), "panemux", "", 10)
				require.ErrorIs(t, err, sentinel)
				require.ErrorContains(t, err, "read host messages")
			} else {
				err := c.Send(context.Background(), "panemux", "pane-a", "pane-b", "body")
				require.ErrorIs(t, err, sentinel)
				if operation == "send" {
					require.ErrorContains(t, err, "send host message")
				}
			}
		})
	}
}

func TestHostAgmsgVersionWarningOnce(t *testing.T) {
	for _, version := range []string{agmsg.TestedAgmsgVersion, "0.0.1", ""} {
		t.Run(version, func(t *testing.T) {
			logs := captureBoardLog(t)
			probes := 0
			runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				switch {
				case strings.Contains(joined, "board-agmsg-presence"):
					return []byte("yes"), nil
				case strings.Contains(joined, "board-agmsg-version"):
					probes++
					return []byte(version), nil
				default:
					return nil, nil
				}
			})
			client := agmsg.NewClient(agmsg.ClientConfig{HostID: "ssh:dev", Path: "/opt/agmsg", Runner: runner})
			require.Equal(t, "ssh:dev", client.HostID())
			for range 2 {
				_, err := client.Since(context.Background(), "panemux", "", 10)
				require.NoError(t, err)
			}
			require.Equal(t, 1, probes)
			warning := agmsg.VersionMismatchWarning("ssh:dev", version)
			if version == "0.0.1" {
				require.NotEmpty(t, warning)
				require.Equal(t, 1, strings.Count(logs.String(), warning))
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
}

func TestPollDoesNotWaitForSendInitialization(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	sender := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "board-agmsg-presence") {
			close(started)
			<-release
			return []byte("yes"), nil
		}
		return nil, nil
	})
	poller := hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, hostexec.ErrConnecting
	})
	c := agmsg.NewClient(agmsg.ClientConfig{HostID: "ssh:dev", Path: "/opt/agmsg", Runner: poller, SendRunner: sender})
	sent := make(chan error, 1)
	go func() { sent <- c.Send(context.Background(), "panemux", "pane-a", "pane-b", "hello") }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("send probe did not start")
	}
	polled := make(chan error, 1)
	go func() { _, err := c.Since(context.Background(), "panemux", "", 10); polled <- err }()
	finished := false
	select {
	case err := <-polled:
		finished = true
		require.ErrorIs(t, err, hostexec.ErrConnecting)
	case <-time.After(100 * time.Millisecond):
		t.Error("poll waited for the send initialization")
	}
	close(release)
	require.NoError(t, <-sent)
	if !finished {
		<-polled
	}
}

func TestConcurrentHostDiscoveryPublishesOneVersionWarning(t *testing.T) {
	logs := captureBoardLog(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "board-agmsg-presence") {
			entered <- struct{}{}
			<-release
			return []byte("yes"), nil
		}
		return []byte("0.0.1"), nil
	})
	c := agmsg.NewClient(agmsg.ClientConfig{HostID: "ssh:dev", Path: "/opt/agmsg", Runner: runner})
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := c.Since(context.Background(), "panemux", "", 10); done <- err }()
	}
	<-entered
	<-entered
	close(release)
	for range 2 {
		require.NoError(t, <-done)
	}
	warning := agmsg.VersionMismatchWarning("ssh:dev", "0.0.1")
	require.Equal(t, 1, strings.Count(logs.String(), warning))
}
