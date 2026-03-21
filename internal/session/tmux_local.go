package session

import (
	"fmt"
	"os"
	"os/exec"
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
}

// NewTmuxLocal creates a new session that attaches to a local tmux session.
func NewTmuxLocal(id, title, tmuxSession string) (*TmuxLocalSession, error) {
	if tmuxSession == "" {
		tmuxSession = "0"
	}

	cmd := exec.Command("tmux", "attach-session", "-t", tmuxSession)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("starting tmux pty: %w", err)
	}

	s := &TmuxLocalSession{
		id:          id,
		title:       title,
		tmuxSession: tmuxSession,
		state:       StateConnected,
		cmd:         cmd,
		ptmx:        ptmx,
	}

	go func() {
		cmd.Wait()
		s.mu.Lock()
		s.state = StateExited
		s.mu.Unlock()
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
	return s.ptmx.Read(p)
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

func (s *TmuxLocalSession) Close() error {
	s.mu.Lock()
	s.state = StateExited
	s.mu.Unlock()

	s.ptmx.Close()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	return nil
}
