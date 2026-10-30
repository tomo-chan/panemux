package agmsg

import (
	"context"
	"github.com/stretchr/testify/require"
	"panemux/internal/board"
	"panemux/internal/host"
	"panemux/internal/hosttools"
	"testing"
	"time"
)

func TestClientPauseRefusesEveryOperationAndInvalidatesAbsence(t *testing.T) {
	gate := &hosttools.CallGate{}
	revision := uint64(0)
	runner := &fakeVersionExecutor{out: []byte("no")}
	c := NewClient(ClientConfig{HostID: "local", Path: "/tmp/sample-agmsg", Runner: runner,
		BeginCall: func() (func(), error) { return gate.Begin(hosttools.AgmsgTool, host.Local) },
		Revision:  func() uint64 { return revision },
	})
	_, err := c.Since(context.Background(), "team", "", 5)
	require.NoError(t, err)
	require.True(t, c.absent)
	resume, err := gate.Pause(context.Background(), hosttools.AgmsgTool, host.Local)
	require.NoError(t, err)
	_, err = c.Since(context.Background(), "team", "", 5)
	require.ErrorIs(t, err, hosttools.ErrUpdating)
	err = c.Send(context.Background(), "team", "from", "to", "body")
	require.ErrorIs(t, err, hosttools.ErrUpdating)
	_, err = c.Available(context.Background())
	require.ErrorIs(t, err, hosttools.ErrUpdating)
	_, _, err = c.LeaseParticipation()
	require.ErrorIs(t, err, hosttools.ErrUpdating)
	resume()
	revision++
	client, release, err := c.LeaseMessages()
	require.NoError(t, err)
	require.NotNil(t, client)
	require.False(t, c.absent)
	release()
}

func TestAdmittedBoardOperationsDrainWithoutNestedAdmission(t *testing.T) {
	gate := &hosttools.CallGate{}
	c := NewClient(ClientConfig{HostID: "local", Path: "/tmp/sample-agmsg",
		Runner:    &fakeVersionExecutor{out: []byte("no")},
		BeginCall: func() (func(), error) { return gate.Begin(hosttools.AgmsgTool, host.Local) }})
	c.absent = true
	messages, release, err := c.LeaseMessages()
	require.NoError(t, err)
	participant, done, err := c.LeaseParticipation()
	require.NoError(t, err)
	paused := make(chan func(), 1)
	go func() {
		resume, pauseErr := gate.Pause(context.Background(), hosttools.AgmsgTool, host.Local)
		if pauseErr == nil {
			paused <- resume
		}
	}()
	require.Eventually(t, func() bool { return gate.Updating(hosttools.AgmsgTool, host.Local) },
		time.Second, time.Millisecond)
	_, _, err = c.LeaseMessages()
	require.ErrorIs(t, err, hosttools.ErrUpdating)
	request := board.JoinRequest{Agent: "claude", Team: "team", PaneID: "pane", Mode: "off"}
	_, err = c.JoinInstruction(context.Background(), request)
	require.ErrorIs(t, err, hosttools.ErrUpdating)
	rows, err := messages.Since(context.Background(), "team", "", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	err = messages.Send(context.Background(), "team", "from", "to", "body")
	require.ErrorContains(t, err, "not installed")
	present, err := participant.Available(context.Background())
	require.NoError(t, err)
	require.False(t, present)
	instruction, err := participant.JoinInstruction(context.Background(), request)
	require.NoError(t, err)
	require.Contains(t, instruction, "join.sh")
	select {
	case <-paused:
		t.Fatal("update began before leases drained")
	default:
	}
	release()
	done()
	select {
	case resume := <-paused:
		resume()
	case <-time.After(time.Second):
		t.Fatal("update did not drain")
	}
}
