package session

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"panemux/internal/testcap"
)

func TestTmuxSSHBrowserEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, version               string
		enabled, writable, wantShim bool
	}{
		{"new tmux", "tmux 3.3a", true, true, true},
		{"future tmux", "tmux 4.0", true, true, true},
		{"double digit minor", "tmux 3.10", true, true, true},
		{"old tmux", "tmux 3.2a", true, true, false},
		{"unknown version", "unknown", true, true, false},
		{"no version", "", true, true, false},
		{"disabled", "tmux 3.3", false, true, false},
		{"unwritable home", "tmux 3.3", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withBrowserShimEnabled(t, tc.enabled)
			dir, home := t.TempDir(), t.TempDir()
			if !tc.writable {
				blocker := filepath.Join(home, "blocker")
				if err := os.WriteFile(blocker, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				home = filepath.Join(blocker, "home")
			}
			// The stand-in exposes tmux's argv and the client's environment.
			// The real-tmux test below verifies -e's create-only semantics.
			stub := "#!/bin/sh\nif [ \"$1\" = -V ]; then printf '%s\\n' " + shellQuotePath(tc.version) + "; exit; fi\n" +
				"printf 'client-browser=%s\\nclient-marker=%s\\nclient-path=%s\\n' " +
				"\"${BROWSER:-unset}\" \"${PANEMUX_SHIM_TMUX:-unset}\" \"$PATH\"\nprintf 'arg=%s\\n' \"$@\"\n"
			//nolint:gosec // G306: executable stand-in for tmux
			if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			command, err := tmuxSSHCommand("demo", SSHConfig{Cwd: "/remote/home/demo/my project"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(command, "\n!") {
				t.Fatalf("unsafe login-shell command: %q", command)
			}
			path := dir + ":/usr/bin:/bin"
			run := exec.Command("sh", "-c", command) //nolint:gosec // G204: generated command under test
			run.Env = []string{"HOME=" + home, "PATH=" + path, "BROWSER=original-browser"}
			out, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("command failed: %v: %s", err, out)
			}
			text := string(out)
			if !strings.Contains(text, "client-browser=original-browser\nclient-marker=unset\nclient-path="+path+"\n") {
				t.Fatalf("changed tmux client's inherited environment: %s", text)
			}
			if !strings.Contains(text, "arg=new-session\narg=-As\narg=demo\narg=-c\narg=/remote/home/demo/my project\n") {
				t.Fatalf("lost session/cwd operands: %s", text)
			}
			if tc.wantShim {
				assertTmuxShimEnvironment(t, text, home)
			} else if strings.Contains(text, "arg=-e\n") {
				t.Fatalf("unsupported/disabled shim changed environment: %s", text)
			}
		})
	}
}

func assertTmuxShimEnvironment(t *testing.T, text, home string) {
	t.Helper()
	shimDir := filepath.Join(home, ".cache/panemux/bin")
	for _, operand := range []string{
		"BROWSER=" + filepath.Join(shimDir, browserShimPrimaryName), "PANEMUX_SHIM_TMUX=1",
	} {
		if !strings.Contains(text, "arg=-e\narg="+operand+"\n") {
			t.Fatalf("missing new-session environment %q: %s", operand, text)
		}
	}
	if readFileString(t, filepath.Join(shimDir, browserShimPrimaryName)) != browserShimScript {
		t.Fatal("shim not installed")
	}
	if !strings.Contains(text, "PANEMUX_SHIM_FALLBACK_PATH=") {
		t.Fatal("missing pane-local PATH bootstrap")
	}
}

// Exercise the generated SSH command against an isolated real tmux server.
// A stand-in default-command stays alive like Claude and asks to open a URL
// only after the attached client is ready. No operator tmux server is used.
func TestTmuxSSHBrowserOpenThroughRealTmux(t *testing.T) {
	testcap.RequirePTY(t)
	testcap.RequireTmux(t)
	withBrowserShimEnabled(t, true)
	fixture := newTmuxURLFixture(t)
	fixture.configure(t)
	terminal := fixture.attach(t, "new")
	readTmuxNotification(t, terminal, []byte("ready"))
	wantBrowser := filepath.Join(fixture.home, ".cache/panemux/bin/panemux-open")
	// Native tmux uses the attaching client's PATH for a new pane, rather than
	// the server's PATH. Preserve that effective path underneath the shim.
	originalPath := fixture.bin + ":/usr/bin:/bin"
	want := wantBrowser + "\n1\n" + filepath.Dir(wantBrowser) + ":" + originalPath + "\n" + originalPath + "\n"
	if got := readFileString(t, fixture.state); got != want {
		t.Fatalf("default-command environment = %q", got)
	}
	if _, err := terminal.Write([]byte("open\n")); err != nil {
		t.Fatal(err)
	}
	readTmuxNotification(t, terminal, []byte("\x1b]7373;panemux-open;https://example.com/auth\a"))
	for _, args := range [][]string{
		{"show-options", "-g", "-v", "allow-passthrough"},
		{"show-options", "-A", "-p", "-v", "-t", "existing:0.0", "allow-passthrough"},
	} {
		if out := fixture.run(t, args...); strings.TrimSpace(out) != "off" {
			t.Fatalf("changed other pane/global option: %s", out)
		}
	}
	// -A must not replace the preexisting session's environment.
	old := fixture.attach(t, "existing")
	readTmuxNotification(t, old, []byte("existing"))
	for _, args := range [][]string{
		{"show-environment", "-t", "existing", "BROWSER"},
		{"show-environment", "-g", "BROWSER"},
	} {
		if out := fixture.run(t, args...); strings.TrimSpace(out) != "BROWSER=original-browser" {
			t.Fatalf("changed existing session/server browser: %s", out)
		}
	}
	cmd := fixture.command("show-environment", "-t", "existing", "PANEMUX_SHIM_TMUX")
	if out, _ := cmd.CombinedOutput(); strings.Contains(string(out), "PANEMUX_SHIM_TMUX=1") {
		t.Fatalf("opted existing session in: %s", out)
	}
	if got := strings.TrimSpace(fixture.run(t, "show-environment", "-g", "PATH")); got != "PATH="+fixture.serverPath {
		t.Fatalf("changed server PATH: %s", got)
	}
}

func TestTmuxSSHBrowserDefaultShell(t *testing.T) {
	testcap.RequirePTY(t)
	testcap.RequireTmux(t)
	withBrowserShimEnabled(t, true)
	fixture := newTmuxURLFixture(t)
	fixture.configure(t)
	shell := filepath.Join(fixture.home, "ksh")
	argsFile := filepath.Join(fixture.home, "shell-args")
	startup := readFileString(t, filepath.Join(fixture.home, "startup"))
	content := "#!/bin/sh\nif [ \"$1\" = -c ]; then exec /bin/sh -c \"$2\"; fi\n" +
		"printf '%s\\n' \"$@\" > " + shellQuotePath(argsFile) + "\n" + startup
	//nolint:gosec // G306: isolated configured-shell fixture
	if err := os.WriteFile(shell, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.run(t, "set-option", "-g", "default-shell", shell)
	fixture.run(t, "set-option", "-g", "default-command", "")
	terminal := fixture.attach(t, "new")
	readTmuxNotification(t, terminal, []byte("ready"))
	if got := readFileString(t, argsFile); got != "-l\n" {
		t.Fatalf("configured shell arguments = %q", got)
	}
	wantBrowser := filepath.Join(fixture.home, ".cache/panemux/bin/panemux-open")
	if got := readFileString(t, fixture.state); !strings.HasPrefix(got, wantBrowser+"\n1\n") {
		t.Fatalf("default-shell environment = %q", got)
	}
}

type tmuxURLFixture struct {
	bin, home, state, shell, serverPath string
	env                                 []string
}

func newTmuxURLFixture(t *testing.T) tmuxURLFixture {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("", "pmx-url-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	fixture := tmuxURLFixture{bin: filepath.Join(root, "bin"), home: filepath.Join(root, "home"), shell: shell}
	for _, dir := range []string{fixture.bin, fixture.home} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A relative socket avoids macOS's sockaddr_un path limit.
	wrapper := "#!/bin/sh\ncd " + shellQuotePath(root) + " || exit\nexec " +
		shellQuotePath(realTmux) + " -S s -f /dev/null \"$@\"\n"
	//nolint:gosec // G306: executable fixture for an isolated tmux server
	if err := os.WriteFile(filepath.Join(fixture.bin, "tmux"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.env = []string{
		"HOME=" + fixture.home, "PATH=" + fixture.bin + ":/usr/bin:/bin", "TERM=xterm-256color",
		"SHELL=" + shell, "BROWSER=original-browser",
	}
	fixture.state = filepath.Join(fixture.home, "new-env")
	fixture.serverPath = fixture.bin + ":/tmp/sample-project/bin:/usr/bin:/bin"
	t.Cleanup(func() { _ = fixture.command("kill-server").Run() })
	version := fixture.run(t, "-V")
	var major, minor int
	if n, _ := fmt.Sscanf(version, "tmux %d.%d", &major, &minor); n != 2 || major < 3 || (major == 3 && minor < 3) {
		t.Skip("tmux 3.3 or newer is required for interception")
	}
	return fixture
}

func (f tmuxURLFixture) command(args ...string) *exec.Cmd {
	cmd := exec.Command(filepath.Join(f.bin, "tmux"), args...) //nolint:gosec // G204: fixture wrapper
	cmd.Env = f.env
	return cmd
}

func (f tmuxURLFixture) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := f.command(args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v: %s", args, err, out)
	}
	return string(out)
}

func (f tmuxURLFixture) configure(t *testing.T) {
	t.Helper()
	f.run(t, "new-session", "-d", "-s", "existing", "sleep 60")
	f.run(t, "set-environment", "-t", "existing", "BROWSER", "original-browser")
	f.run(t, "set-environment", "-g", "PATH", f.serverPath)
	startup := filepath.Join(f.home, "startup")
	content := "#!/bin/sh\nprintf '%s\\n' \"$BROWSER\" \"$PANEMUX_SHIM_TMUX\" " +
		"\"$PATH\" \"$PANEMUX_SHIM_FALLBACK_PATH\" > " + shellQuotePath(f.state) +
		"\nprintf '\\r\\nready\\r\\n'\nwhile IFS= read -r line; do\n" +
		"case \"$line\" in open) xdg-open https://example.com/auth;; esac\ndone\n"
	if err := os.WriteFile(startup, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f.run(t, "set-option", "-g", "default-shell", f.shell)
	f.run(t, "set-option", "-g", "default-command", "sh "+shellQuotePath(startup))
	f.run(t, "set-option", "-g", "allow-passthrough", "off")
}

func (f tmuxURLFixture) attach(t *testing.T, name string) *os.File {
	t.Helper()
	command, err := tmuxSSHCommand(name, SSHConfig{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", command) //nolint:gosec // G204: generated command under test
	cmd.Env = f.env
	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return terminal
}

func readTmuxNotification(t *testing.T, terminal *os.File, needle []byte) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		var data []byte
		buffer := make([]byte, 4096)
		for {
			n, err := terminal.Read(buffer)
			data = append(data, buffer[:n]...)
			if bytes.Contains(data, needle) {
				result <- nil
				return
			}
			if err != nil {
				result <- fmt.Errorf("read: %w; bytes=%q", err, data)
				return
			}
		}
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		_ = terminal.Close()
		t.Fatalf("tmux never emitted %q: %v", needle, <-result)
	}
}

func TestBrowserShimTmuxPassthrough(t *testing.T) {
	for _, tc := range []struct {
		name, marker, tmux, pane string
		wrapped                  bool
	}{
		{"opted in tmux pane", "1", "private-socket,123,0", "%7", true},
		{"ordinary ssh", "", "", "", false},
		{"inherited tmux without opt in", "", "private-socket,123,0", "%7", false},
		{"marker without tmux", "1", "", "", false},
		{"missing pane cannot change another pane", "1", "private-socket,123,0", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			shim := installShimForTest(t, dir, browserShimPrimaryName)
			tty, calls := filepath.Join(dir, "tty"), filepath.Join(dir, "calls")
			stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuotePath(calls) + "\n"
			//nolint:gosec // G306: executable stand-in for tmux
			if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			const url = "https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A8085"
			run := exec.Command(shim, url) //nolint:gosec // G204: test fixture executable
			run.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin", "PANEMUX_SHIM_TTY=" + tty,
				"PANEMUX_SHIM_TMUX=" + tc.marker, "TMUX=" + tc.tmux, "TMUX_PANE=" + tc.pane,
			}
			if out, err := run.CombinedOutput(); err != nil || len(out) > 0 {
				t.Fatalf("shim failed: %v: %s", err, out)
			}
			osc := "\x1b]7373;panemux-open;https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A8085\a"
			wantCalls := ""
			if tc.wrapped {
				osc = "\x1bPtmux;\x1b" + osc + "\x1b\\"
				wantCalls = "set-option\n-p\n-t\n%7\nallow-passthrough\non\n"
			}
			if got := readFileString(t, tty); got != osc {
				t.Fatalf("notification bytes = %q, want %q", got, osc)
			}
			if got := readFileString(t, calls); got != wantCalls {
				t.Fatalf("tmux option scope = %q, want %q", got, wantCalls)
			}
		})
	}
}
