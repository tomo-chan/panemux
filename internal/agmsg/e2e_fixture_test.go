package agmsg_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/host"
	"panemux/internal/hostexec"
)

func e2eFixtureClient(t *testing.T) (*agmsg.Client, string) {
	t.Helper()
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts")
	require.NoError(t, os.Mkdir(scripts, 0700))
	for _, name := range []string{"api.sh", "send.sh"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "e2e", "fixtures", "agmsg", "scripts", name))
		require.NoError(t, err)
		//nolint:gosec // executable repository fixture in a test-owned directory
		require.NoError(t, os.WriteFile(filepath.Join(scripts, name), source, 0700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "next_id"), []byte("1000\n"), 0600))
	pool := hostexec.New(hostexec.Options{})
	t.Cleanup(pool.Close)
	return agmsg.NewClient(agmsg.ClientConfig{HostID: "local", Path: root, Runner: host.Bind(pool, host.Local)}), root
}

func TestE2EAgmsgFixtureBroadcastReachesRelayCache(t *testing.T) {
	for _, body := range []string{"Broadcast from the e2e suite.", "--force", "--body", "--body-file", "--", "--help"} {
		t.Run(body, func(t *testing.T) {
			client, root := e2eFixtureClient(t)
			// Exercise the public stdin ABI independently of the wrapper: an old
			// positional wrapper paired with an old fixture would otherwise mask drift.
			//nolint:gosec // repository fixture in a test-owned installation
			seed := exec.Command("sh", filepath.Join(root, "scripts", "send.sh"),
				"panemux-e2e", "board-worker", "board-worker", "--body", "-", "--force")
			seed.Stdin = strings.NewReader("stdin seed")
			output, seedErr := seed.CombinedOutput()
			require.NoError(t, seedErr, string(output))
			seeded, seedErr := client.Since(context.Background(), "panemux-e2e", "", 100)
			require.NoError(t, seedErr)
			require.Len(t, seeded, 1)
			require.Equal(t, "stdin seed", seeded[0].Body)
			cache := board.NewBoardCache()
			relay := board.NewRelay(cache, board.RelayConfig{Team: "panemux-e2e",
				Clients: map[string]board.MessageClient{"local": client}, PaneHosts: map[string]string{"board-worker": "local"},
				Limit: 100, BackfillLimit: 1000})
			ctx := context.Background()
			delivered, err := relay.Broadcast(ctx, board.SystemID, []string{"board-worker"}, body)
			require.NoError(t, err)
			require.Equal(t, []string{"board-worker"}, delivered)
			require.NoError(t, relay.Poll(ctx))
			rows := cache.MessagesSince(0)
			require.Len(t, rows, 2, "the fixture body must match the own-send ledger")
			require.Equal(t, body, rows[1].Row.Body)
			require.Equal(t, board.SystemID, rows[1].Row.From)
			require.NoError(t, relay.Poll(ctx))
			require.Len(t, cache.MessagesSince(0), 2)
		})
	}
}

//efficacy:exempt Pins existing positional fixture seeding used by E2E and screenshots; unchanged behavior.
func TestE2EAgmsgFixturePositionalSeedingStillWorks(t *testing.T) {
	client, root := e2eFixtureClient(t)
	const body = `{"kind":"board_status","state":"working"}`
	//nolint:gosec // repository-owned public fixture, test-owned installation
	out, err := exec.Command("sh", filepath.Join(root, "scripts", "send.sh"),
		"panemux-e2e", "board-main", "_system", body, "--force").CombinedOutput()
	require.NoError(t, err, string(out))
	rows, err := client.Since(context.Background(), "panemux-e2e", "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, body, rows[0].Body)
}

func TestAgmsgClientsRejectNULBeforeExecution(t *testing.T) {
	for _, body := range []string{"\x00", "before\x00after", "\x00before", "after\x00", "--force\x00\n"} {
		for _, mode := range []string{"remote", "lazy", "leased"} {
			t.Run(mode+"/"+body, func(t *testing.T) {
				calls := 0
				runner := hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
					calls++
					return nil, nil
				})
				var client board.MessageClient = agmsg.NewRemoteAgmsgClient("local", "/opt/agmsg", runner)
				if mode != "remote" {
					lazy := agmsg.NewClient(agmsg.ClientConfig{HostID: "local", Path: "/opt/agmsg", Runner: runner})
					client = lazy
					if mode == "leased" {
						leased, release, err := lazy.LeaseMessages()
						require.NoError(t, err)
						defer release()
						client = leased
					}
				}
				err := client.Send(context.Background(), "panemux", "worker", "orch@ssh:build", body)
				require.ErrorContains(t, err, "NUL")
				require.Zero(t, calls, "unsupported bytes must fail before discovery or send execution")
			})
		}
	}
}

func TestE2EAgmsgFixtureNULBroadcastReportsNoDelivery(t *testing.T) {
	client, _ := e2eFixtureClient(t)
	cache := board.NewBoardCache()
	relay := board.NewRelay(cache, board.RelayConfig{Team: "panemux-e2e",
		Clients: map[string]board.MessageClient{"local": client}, PaneHosts: map[string]string{"board-worker": "local"},
		Limit: 100, BackfillLimit: 1000})
	for _, body := range []string{"\x00", "before\x00after"} {
		delivered, err := relay.Broadcast(context.Background(), board.SystemID, []string{"board-worker"}, body)
		require.ErrorContains(t, err, "NUL")
		require.Empty(t, delivered)
	}
	require.NoError(t, relay.Poll(context.Background()))
	require.Empty(t, cache.MessagesSince(0))
	rows, err := client.Since(context.Background(), "panemux-e2e", "", 100)
	require.NoError(t, err)
	require.Empty(t, rows, "rejection must not store a modified copy")
}

// A rejected relay send is logged and retained at its original source, rather
// than claiming successful delivery of altered bytes.
func TestRelayNULBodyRejectionIsRecorded(t *testing.T) {
	logs := captureBoardLog(t)
	source := hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"type":"message_sent","id":"1","team":"panemux",` +
			`"from":"worker","to":"orch@ssh:build","body":"before\u0000after"}`), nil
	})
	calls := 0
	dest := hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return nil, nil
	})
	cache := board.NewBoardCache()
	relay := board.NewRelay(cache, board.RelayConfig{Team: "panemux", Limit: 100, BackfillLimit: 1000,
		Clients: map[string]board.MessageClient{
			"local":     agmsg.NewRemoteAgmsgClient("local", "/opt/agmsg", source),
			"ssh:build": agmsg.NewRemoteAgmsgClient("ssh:build", "/opt/agmsg", dest),
		}})
	require.NoError(t, relay.Poll(context.Background()))
	require.Contains(t, logs.String(), "NUL")
	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1)
	require.Equal(t, "before\x00after", rows[0].Row.Body)
	require.Equal(t, "local", rows[0].Row.Host)
	require.Equal(t, 1, calls, "destination is read but must never receive a send")
}

func TestPaneRelaySSHKeySendAndReply(t *testing.T) {
	for _, key := range []string{"build1", "build.1_x-y", "build+1", "開発機", "開発+1_x-y.dev", "de\u0301v"} {
		t.Run(key, func(t *testing.T) {
			_, localRoot := e2eFixtureClient(t)
			_, remoteRoot := e2eFixtureClient(t)
			pool := hostexec.New(hostexec.Options{})
			t.Cleanup(pool.Close)
			runner := host.Bind(pool, host.Local)
			local := agmsg.NewRemoteAgmsgClient("local", localRoot, runner)
			remoteID := host.Host(key).BoardID()
			remote := agmsg.NewRemoteAgmsgClient(remoteID, remoteRoot, runner)
			cache := board.NewBoardCache()
			relay := board.NewRelay(cache, board.RelayConfig{Team: "panemux-e2e", Limit: 100, BackfillLimit: 1000,
				Clients:   map[string]board.MessageClient{"local": local, remoteID: remote},
				PaneHosts: map[string]string{"pane-a": "local", "pane-b": remoteID}})
			ctx := context.Background()
			require.NoError(t, local.Send(ctx, "panemux-e2e", "pane-a", "pane-b", "request"))
			require.NoError(t, relay.Poll(ctx))
			received, err := remote.Since(ctx, "panemux-e2e", "", 100)
			require.NoError(t, err)
			require.Len(t, received, 1)
			require.Equal(t, "pane-a@local", received[0].From)
			require.Equal(t, "pane-b", received[0].To)
			require.Equal(t, "request", received[0].Body)
			require.NoError(t, remote.Send(ctx, "panemux-e2e", "pane-b", received[0].From, "reply"))
			require.NoError(t, relay.Poll(ctx))
			returned, err := local.Since(ctx, "panemux-e2e", "", 100)
			require.NoError(t, err)
			require.Len(t, returned, 2, "the remote reply must be delivered, not dropped by sender validation")
			require.Equal(t, "pane-b@"+remoteID, returned[1].From)
			require.Equal(t, "pane-a", returned[1].To)
			require.Equal(t, "reply", returned[1].Body)
			require.NoError(t, relay.Poll(ctx))
			require.Len(t, cache.MessagesSince(0), 2)
		})
	}
}
