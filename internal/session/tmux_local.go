package session

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// TmuxLocalSession attaches to an existing local tmux session via PTY.
type TmuxLocalSession struct {
	mu          sync.RWMutex
	id          string
	title       string
	tmuxSession string
	state       State
	cmd         *exec.Cmd
	ptmx        *os.File
	pr          *io.PipeReader // Read() reads from here
	pw          *io.PipeWriter // background goroutine writes output then closes
}

// NewTmuxLocal creates a new session that attaches to a local tmux session.
func NewTmuxLocal(id, title, tmuxSession string) (*TmuxLocalSession, error) {
	if tmuxSession == "" {
		tmuxSession = "0"
	}

	cmd := exec.Command("tmux", "new-session", "-A", "-s", tmuxSession)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("starting tmux pty: %w", err)
	}

	pr, pw := io.Pipe()

	s := &TmuxLocalSession{
		id:          id,
		title:       title,
		tmuxSession: tmuxSession,
		state:       StateConnected,
		cmd:         cmd,
		ptmx:        ptmx,
		pr:          pr,
		pw:          pw,
	}

	// Bridge PTY output to the pipe. After the PTY is closed (EIO on macOS),
	// inject an error message when tmux exited with a non-zero status so that
	// the WebSocket reader always delivers the exit reason to the browser.
	go func() {
		io.Copy(pw, ptmx) //nolint:errcheck -- EIO is expected when slave closes
		exitErr := cmd.Wait()
		s.mu.Lock()
		s.state = StateExited
		s.mu.Unlock()
		if exitErr != nil {
			msg := fmt.Sprintf(
				"\r\n\x1b[31m[panemux] tmux session %q exited: %v\x1b[0m\r\n",
				tmuxSession, exitErr,
			)
			pw.Write([]byte(msg)) //nolint:errcheck -- pw may be closed if Close() was called first
		}
		pw.Close()
	}()

	return s, nil
}

func (s *TmuxLocalSession) ID() string    { return s.id }
func (s *TmuxLocalSession) Type() Type    { return TypeTmux }
func (s *TmuxLocalSession) Title() string { return s.title }

func (s *TmuxLocalSession) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *TmuxLocalSession) Read(p []byte) (int, error) {
	return s.pr.Read(p)
}

func (s *TmuxLocalSession) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

func (s *TmuxLocalSession) Resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// GetCWD returns the current working directory of the active tmux pane.
func (s *TmuxLocalSession) GetCWD() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", s.tmuxSession, "#{pane_current_path}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *TmuxLocalSession) Close() error {
	s.mu.Lock()
	s.state = StateExited
	s.mu.Unlock()

	// Close the write end of the pipe (causes pr.Read to return EOF) before
	// closing the PTY, so the bridge goroutine unblocks cleanly.
	s.pw.Close()
	s.ptmx.Close()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	return nil
}
