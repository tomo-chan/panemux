package session

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

type testSSHResponse struct {
	stdout string
	stderr string
	status uint32
}

type testSSHTransport struct {
	handler     func(string) testSSHResponse
	forward     *forwardRecorder
	echoAddr    string
	connections []net.Conn
	commands    []string
	resizes     [][2]uint32
	mu          sync.Mutex
}

func startSessionTestSSHServer(t *testing.T, handler func(string) testSSHResponse) (*ssh.Client, *testSSHTransport) {
	t.Helper()
	return startTestSSHTransport(t, handler, "", nil)
}

func startTestSSHTransport(
	t *testing.T,
	handler func(string) testSSHResponse,
	echoAddr string,
	forward *forwardRecorder,
) (*ssh.Client, *testSSHTransport) {
	t.Helper()
	transport := &testSSHTransport{handler: handler, echoAddr: echoAddr, forward: forward}
	serverCfg := &ssh.ServerConfig{NoClientAuth: true}
	serverCfg.AddHostKey(testSSHSigner(t))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			raw, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go transport.serve(raw, serverCfg)
		}
	}()

	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User:            "tester",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // ephemeral in-process test server
		Timeout:         5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	return client, transport
}

func testSSHSigner(t *testing.T) ssh.Signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(key)
	require.NoError(t, err)
	return signer
}

func (s *testSSHTransport) serve(raw net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		raw.Close()
		return
	}
	defer conn.Close()
	s.mu.Lock()
	s.connections = append(s.connections, raw)
	s.mu.Unlock()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			channel, requests, acceptErr := newChannel.Accept()
			if acceptErr != nil {
				continue
			}
			go s.serveSession(channel, requests)
		case "direct-tcpip":
			if s.forward == nil || s.echoAddr == "" {
				newChannel.Reject(ssh.Prohibited, "forwarding disabled")
				continue
			}
			var payload directTCPIPPayload
			if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
				newChannel.Reject(ssh.ConnectionFailed, "bad payload")
				continue
			}
			s.forward.record(payload.DestAddr, payload.DestPort)
			channel, requests, acceptErr := newChannel.Accept()
			if acceptErr != nil {
				continue
			}
			go ssh.DiscardRequests(requests)
			go pipeTestChannel(channel, s.echoAddr)
		default:
			newChannel.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

func (s *testSSHTransport) serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	for request := range requests {
		switch request.Type {
		case "pty-req":
			request.Reply(true, nil)
		case "window-change":
			var size struct{ Columns, Rows, Width, Height uint32 }
			_ = ssh.Unmarshal(request.Payload, &size)
			s.mu.Lock()
			s.resizes = append(s.resizes, [2]uint32{size.Columns, size.Rows})
			s.mu.Unlock()
		case "shell":
			request.Reply(true, nil)
			go func() { _, _ = io.Copy(channel, channel) }()
		case "exec":
			var payload struct{ Command string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			s.mu.Lock()
			s.commands = append(s.commands, payload.Command)
			s.mu.Unlock()
			request.Reply(true, nil)
			if strings.HasPrefix(payload.Command, "tmux new-session ") {
				go func() { _, _ = io.Copy(channel, channel) }()
				continue
			}
			response := testSSHResponse{}
			if s.handler != nil {
				response = s.handler(payload.Command)
			}
			_, _ = io.WriteString(channel, response.stdout)
			_, _ = io.WriteString(channel.Stderr(), response.stderr)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{response.status}))
			_ = channel.Close()
		default:
			request.Reply(false, nil)
		}
	}
}

func (s *testSSHTransport) closeConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.connections {
		_ = conn.Close()
	}
}

func (s *testSSHTransport) snapshot() ([]string, [][2]uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...), append([][2]uint32(nil), s.resizes...)
}

func readExactly(t *testing.T, reader io.Reader, size int) string {
	t.Helper()
	type result struct {
		err  error
		data []byte
	}
	done := make(chan result, 1)
	go func() {
		buf := make([]byte, size)
		_, err := io.ReadFull(reader, buf)
		done <- result{data: buf, err: err}
	}()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		return string(got.data)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session output")
		return ""
	}
}

func readUntilContains(t *testing.T, reader io.Reader, marker string) string {
	t.Helper()
	type result struct {
		err  error
		data string
	}
	done := make(chan result, 1)
	go func() {
		var data string
		buf := make([]byte, 128)
		for !strings.Contains(data, marker) {
			n, err := reader.Read(buf)
			data += string(buf[:n])
			if err != nil {
				done <- result{data: data, err: err}
				return
			}
		}
		done <- result{data: data}
	}()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		return got.data
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session output containing " + marker)
		return ""
	}
}

func waitForState(t *testing.T, state func() State, want State) {
	t.Helper()
	require.Eventually(
		t,
		func() bool { return state() == want },
		5*time.Second,
		10*time.Millisecond,
		"state did not become %s",
		want,
	)
}

func TestSSHSessionLifecycleOverInProcessTransport(t *testing.T) {
	withBrowserShimEnabled(t, false)
	client, transport := startSessionTestSSHServer(t, nil)
	sess, err := newSSHSessionFromClient("pane-ssh", "SSH", SSHConfig{ConnectionName: "demo"}, client, nil)
	require.NoError(t, err)
	require.Equal(t, StateConnected, sess.State())
	assert.Equal(t, "pane-ssh", sess.ID())
	assert.Equal(t, TypeSSH, sess.Type())
	assert.Equal(t, "SSH", sess.Title())

	_, err = sess.Write([]byte("round trip"))
	require.NoError(t, err)
	assert.Equal(t, "round trip", readExactly(t, sess, len("round trip")))
	require.NoError(t, sess.Resize(132, 43))
	require.Eventually(t, func() bool {
		_, resizes := transport.snapshot()
		return len(resizes) == 1
	}, 5*time.Second, 10*time.Millisecond)
	_, resizes := transport.snapshot()
	assert.Equal(t, [2]uint32{132, 43}, resizes[0])

	require.NoError(t, sess.Close())
	assert.Equal(t, StateExited, sess.State())
}

func TestSSHSessionRemoteSideClosureChangesStateToExited(t *testing.T) {
	withBrowserShimEnabled(t, false)
	client, transport := startSessionTestSSHServer(t, nil)
	sess, err := newSSHSessionFromClient("pane-ssh", "SSH", SSHConfig{}, client, nil)
	require.NoError(t, err)

	transport.closeConnections()
	waitForState(t, sess.State, StateExited)
}

func TestTmuxSSHSessionLifecycleOverInProcessTransport(t *testing.T) {
	client, transport := startSessionTestSSHServer(t, nil)
	sess, err := newTmuxSSHSessionFromClient(
		"pane-tmux", "Remote tmux", "work", SSHConfig{ConnectionName: "demo"}, client, nil,
	)
	require.NoError(t, err)
	require.Equal(t, StateConnected, sess.State())
	assert.Equal(t, TypeSSHTmux, sess.Type())

	_, err = sess.Write([]byte("tmux round trip"))
	require.NoError(t, err)
	assert.Equal(t, "tmux round trip", readExactly(t, sess, len("tmux round trip")))
	require.NoError(t, sess.Resize(100, 31))
	require.Eventually(t, func() bool {
		commands, resizes := transport.snapshot()
		return len(commands) >= 1 && len(resizes) == 1
	}, 5*time.Second, 10*time.Millisecond)
	commands, resizes := transport.snapshot()
	assert.Equal(t, "tmux new-session -As 'work'", commands[0])
	assert.Equal(t, [2]uint32{100, 31}, resizes[0])

	require.NoError(t, sess.Close())
	assert.Equal(t, StateExited, sess.State())
}

func TestTmuxSSHSessionRemoteSideClosureChangesStateToExited(t *testing.T) {
	client, transport := startSessionTestSSHServer(t, nil)
	sess, err := newTmuxSSHSessionFromClient(
		"pane-tmux", "Remote tmux", "work", SSHConfig{}, client, nil,
	)
	require.NoError(t, err)

	transport.closeConnections()
	waitForState(t, sess.State, StateExited)
}

func TestSSHSessionExecMethodsUseRealChannelsAndParseResponses(t *testing.T) {
	withBrowserShimEnabled(t, false)
	handler := func(command string) testSSHResponse {
		switch command {
		case "'api.sh' 'status'":
			return testSSHResponse{stdout: "ok\n"}
		case sshGetCWDCmd:
			return testSSHResponse{stdout: "/workspace/project\n"}
		case sshShellPIDCmd:
			return testSSHResponse{stdout: "100\n"}
		case sshListProcessesCmd:
			return testSSHResponse{stdout: " 100 1 zsh\n 220 100 gemini\n"}
		default:
			if strings.HasPrefix(command, "cd '/workspace/project' && root=") {
				return testSSHResponse{stdout: "/workspace/project\n" +
					"/workspace/project/.git\nfeature/test\ngit@example.test:demo/project.git\n"}
			}
			return testSSHResponse{stderr: "unexpected command: " + command, status: 1}
		}
	}
	client, _ := startSessionTestSSHServer(t, handler)
	sess, err := newSSHSessionFromClient("pane-ssh", "SSH", SSHConfig{}, client, nil)
	require.NoError(t, err)
	defer sess.Close()

	out, err := sess.RunBoardCommand(context.Background(), []string{"api.sh", "status"})
	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(out))
	cwd, err := sess.GetCWD()
	require.NoError(t, err)
	assert.Equal(t, "/workspace/project", cwd)
	agentType, ok, err := sess.DetectInteractiveAgentType()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "gemini", agentType)
	workdirs, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Empty(t, workdirs)
	gitContext, err := sess.InspectGitContext("/workspace/project")
	require.NoError(t, err)
	assert.Equal(t, GitContext{
		Root: "/workspace/project", CommonDir: "/workspace/project/.git", Branch: "feature/test",
		OriginURL: "git@example.test:demo/project.git", Repo: "project",
	}, gitContext)
}

func TestSSHSessionInspectGitContextClassifiesRemoteFailure(t *testing.T) {
	withBrowserShimEnabled(t, false)
	client, _ := startSessionTestSSHServer(t, func(command string) testSSHResponse {
		return testSSHResponse{
			stdout: "__PANEMUX_GIT_CONTEXT_ERROR__\nshow-toplevel\nfatal: not a git repository\n",
			status: 128,
		}
	})
	sess, err := newSSHSessionFromClient("pane-ssh", "SSH", SSHConfig{}, client, nil)
	require.NoError(t, err)
	defer sess.Close()

	_, err = sess.InspectGitContext("/workspace/not-a-repo")
	var gitErr *GitContextError
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, GitContextCauseNotGitRepo, gitErr.Cause)
}

func TestRemoteShellAndDirectoryListingUseRealExecChannels(t *testing.T) {
	handler := func(command string) testSSHResponse {
		switch command {
		case "echo $SHELL":
			return testSSHResponse{stdout: "/bin/zsh\n"}
		case remoteDirectoryListCommand("/workspace", true):
			return testSSHResponse{stdout: "/workspace\n.alpha\t/workspace/.alpha\t0\nproject\t/workspace/project\t1\n"}
		default:
			return testSSHResponse{stderr: "unexpected command", status: 1}
		}
	}
	client, _ := startSessionTestSSHServer(t, handler)

	shell, err := detectRemoteShellFromClient(client)
	require.NoError(t, err)
	assert.Equal(t, "/bin/zsh", shell)
	entries, resolvedPath, err := listRemoteDirectoriesFromClient(client, "/workspace", true)
	require.NoError(t, err)
	assert.Equal(t, "/workspace", resolvedPath)
	assert.Equal(t, []DirectoryEntry{
		{Name: ".alpha", Path: "/workspace/.alpha", HasChildren: false},
		{Name: "project", Path: "/workspace/project", HasChildren: true},
	}, entries)
}

func TestSSHSessionStartFailureClosesEstablishedTransport(t *testing.T) {
	withBrowserShimEnabled(t, false)
	client, _ := startSessionTestSSHServer(t, nil)
	_, err := newSSHSessionFromClient("pane", "SSH", SSHConfig{Cwd: "relative/path"}, client, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid working directory")
	_, err = client.NewSession()
	require.Error(t, err, "the established transport must be closed when shell setup fails")
}

func TestTmuxSSHSessionExecMethodsUseRealChannelsAndParseResponses(t *testing.T) {
	handler := func(command string) testSSHResponse {
		switch command {
		case "'api.sh' 'status'":
			return testSSHResponse{stdout: "tmux-ok\n"}
		case "tmux display-message -p -t 'work' '#{pane_current_path}'":
			return testSSHResponse{stdout: "/workspace/tmux-project\n"}
		case "tmux display-message -p -t 'work' '#{pane_pid}\t#{pane_current_path}'":
			return testSSHResponse{stdout: "100\t/workspace/tmux-project\n"}
		case sshListProcessesCmd:
			return testSSHResponse{stdout: " 100 1 zsh\n 220 100 gemini\n"}
		default:
			if strings.HasPrefix(command, "cd '/workspace/tmux-project' && root=") {
				return testSSHResponse{stdout: "/workspace/tmux-project\n" +
					"/workspace/tmux-project/.git\nfeature/tmux\n" +
					"https://example.test/demo/tmux-project.git\n"}
			}
			return testSSHResponse{stderr: "unexpected command: " + command, status: 1}
		}
	}
	client, _ := startSessionTestSSHServer(t, handler)
	sess, err := newTmuxSSHSessionFromClient("pane-tmux", "tmux", "work", SSHConfig{}, client, nil)
	require.NoError(t, err)
	defer sess.Close()

	out, err := sess.RunBoardCommand(context.Background(), []string{"api.sh", "status"})
	require.NoError(t, err)
	assert.Equal(t, "tmux-ok\n", string(out))
	cwd, err := sess.GetCWD()
	require.NoError(t, err)
	assert.Equal(t, "/workspace/tmux-project", cwd)
	agentType, ok, err := sess.DetectInteractiveAgentType()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "gemini", agentType)
	workdirs, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Empty(t, workdirs)
	gitContext, err := sess.InspectGitContext("/workspace/tmux-project")
	require.NoError(t, err)
	assert.Equal(t, "tmux-project", gitContext.Repo)
	assert.Equal(t, "feature/tmux", gitContext.Branch)
}

func TestTmuxLocalSessionLifecycleWithInjectedCommand(t *testing.T) {
	previous := tmuxLocalCommandFn
	tmuxLocalCommandFn = func(args []string) *exec.Cmd {
		// os.Args[0] is the current test binary, not caller-controlled input.
		return exec.Command( //nolint:gosec
			os.Args[0], "-test.run=^TestTmuxLocalHelperProcess$", "--", "tmux-local-helper",
		)
	}
	t.Cleanup(func() { tmuxLocalCommandFn = previous })

	sess, err := NewTmuxLocal("pane-local-tmux", "Local tmux", "work", "/workspace/project")
	require.NoError(t, err)
	require.Equal(t, StateConnected, sess.State())
	assert.Equal(t, TypeTmux, sess.Type())
	assert.Equal(t, "pane-local-tmux", sess.ID())
	assert.Equal(t, "Local tmux", sess.Title())

	_, err = sess.Write([]byte("ping\n"))
	require.NoError(t, err)
	assert.Contains(t, readUntilContains(t, sess, "PONG"), "PONG")
	assert.Equal(t, StateConnected, sess.State(), "the helper must remain alive before Close")

	require.NoError(t, sess.Resize(120, 40))
	_, err = sess.Write([]byte("size\n"))
	require.NoError(t, err)
	assert.Contains(t, readUntilContains(t, sess, "SIZE 120 40"), "SIZE 120 40")
	assert.Equal(t, StateConnected, sess.State(), "the helper must remain alive after Resize")

	require.NoError(t, sess.Close())
	assert.Equal(t, StateExited, sess.State())
}

func TestTmuxLocalHelperProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "tmux-local-helper" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "ping":
			_, _ = fmt.Fprintln(os.Stdout, "PONG")
		case "size":
			size, err := pty.GetsizeFull(os.Stdout)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "SIZE ERROR %v\n", err)
				continue
			}
			_, _ = fmt.Fprintf(os.Stdout, "SIZE %d %d\n", size.Cols, size.Rows)
		}
	}
	os.Exit(0)
}

func TestTmuxLocalSessionStartFailureIsReturned(t *testing.T) {
	previous := tmuxLocalCommandFn
	tmuxLocalCommandFn = func(args []string) *exec.Cmd {
		return exec.Command("/path/that/does/not/exist")
	}
	t.Cleanup(func() { tmuxLocalCommandFn = previous })

	_, err := NewTmuxLocal("pane", "tmux", "work", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}
