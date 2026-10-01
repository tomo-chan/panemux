package session

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
)

// The board's attach (issue #283) must never create a tmux session: it names
// the target with tmux's "=" exact-match prefix so a missing session fails
// instead of resolving to another session that starts with the same name.
func TestTmuxLocalAttachArgs_AttachesToTheExactSessionOnly(t *testing.T) {
	assert.Equal(t, []string{"attach-session", "-t", "=review-api"}, tmuxLocalAttachArgs("review-api"))
}

func TestTmuxSSHAttachCommand_AttachesToTheExactSessionOnly(t *testing.T) {
	assert.Equal(t, "tmux attach-session -t '=review-api'", tmuxSSHAttachCommand("review-api"))
}

func TestValidateTmuxAttachName(t *testing.T) {
	cases := []struct {
		name    string
		session string
		wantErr bool
	}{
		{name: "plain name", session: "review-api"},
		{name: "dots and underscores", session: "task_1.a"},
		{name: "empty is refused rather than defaulting to 0", session: "", wantErr: true},
		{name: "shell metacharacter", session: "bad;session", wantErr: true},
		{name: "quote", session: "it's", wantErr: true},
		{name: "tmux target syntax", session: "work:1", wantErr: true},
		{name: "already exact-match prefixed", session: "=work", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateTmuxAttachName(tc.session)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.session, got)
		})
	}
}

func TestNewTmuxLocalAttach_RunsAttachSessionNotNewSession(t *testing.T) {
	previous := tmuxLocalCommandFn
	var got []string
	tmuxLocalCommandFn = func(args []string) *exec.Cmd {
		got = args
		return exec.Command("/path/that/does/not/exist")
	}
	t.Cleanup(func() { tmuxLocalCommandFn = previous })

	_, err := NewTmuxLocalAttach("board-1", "review-api", "review-api")
	require.Error(t, err, "the injected command cannot start")
	assert.Equal(t, []string{"attach-session", "-t", "=review-api"}, got)
}

func TestNewTmuxLocalAttach_InvalidNameNeverReachesExec(t *testing.T) {
	previous := tmuxLocalCommandFn
	called := false
	tmuxLocalCommandFn = func(args []string) *exec.Cmd {
		called = true
		return exec.Command("/path/that/does/not/exist")
	}
	t.Cleanup(func() { tmuxLocalCommandFn = previous })

	_, err := NewTmuxLocalAttach("board-1", "t", "bad;session")
	require.Error(t, err)
	assert.False(t, called)
}

func TestNewTmuxSSHAttach_InvalidNameIsRefusedBeforeDialing(t *testing.T) {
	_, err := NewTmuxSSHAttach("board-1", "t", "bad;session", SSHConfig{Host: "dev.invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tmux session name")
}

func TestCreateTmuxAttach_DispatchesOnConnection(t *testing.T) {
	previous := newTmuxLocalAttachFn
	var local []string
	newTmuxLocalAttachFn = func(id, title, tmuxSession string) (*TmuxLocalSession, error) {
		local = []string{id, title, tmuxSession}
		return &TmuxLocalSession{id: id}, nil
	}
	t.Cleanup(func() { newTmuxLocalAttachFn = previous })

	sess, err := createTmuxAttach("board-1", "Review", "", "review-api", nil, "")
	require.NoError(t, err)
	assert.Equal(t, "board-1", sess.ID())
	assert.Equal(t, []string{"board-1", "Review", "review-api"}, local)

	_, err = createTmuxAttach("board-2", "Review", "missing", "review-api",
		map[string]config.SSHConnection{}, "/nonexistent/ssh_config")
	require.Error(t, err, "a connection that resolves nowhere is an error, not a local attach")
}
