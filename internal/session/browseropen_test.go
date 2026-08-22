package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// installShimForTest writes the shim script under dir/<name> so the test can
// run it exactly as a pane's shell would.
func installShimForTest(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	//nolint:gosec // G306: the fixture mirrors the executable shim panemux installs
	if err := os.WriteFile(path, []byte(browserShimScript), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return path
}

// fakeOpener writes its arguments to argsFile, standing in for the real
// xdg-open/open the shim must fall through to.
func fakeOpener(t *testing.T, dir, name, argsFile string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n"
	//nolint:gosec // G306: a stand-in for the real opener has to be executable
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opener: %v", err)
	}
}

type shimRun struct {
	ttyPath  string
	argsFile string
	exitCode int
}

func runShim(t *testing.T, shimPath, fallbackDir string, args ...string) shimRun {
	t.Helper()
	tmp := t.TempDir()
	run := shimRun{
		ttyPath:  filepath.Join(tmp, "tty"),
		argsFile: filepath.Join(tmp, "opener-args"),
	}
	if err := os.WriteFile(run.ttyPath, nil, 0o600); err != nil {
		t.Fatalf("seed tty file: %v", err)
	}

	cmd := exec.Command(shimPath, args...) //nolint:gosec // G204: fixed test fixture path
	cmd.Env = append(os.Environ(),
		"PANEMUX_SHIM_TTY="+run.ttyPath,
		"PANEMUX_SHIM_FALLBACK_PATH="+fallbackDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !asExitError(err, &exitErr) {
			t.Fatalf("run shim: %v (output %q)", err, out)
		}
		run.exitCode = exitErr.ExitCode()
	}
	if len(out) != 0 {
		t.Fatalf("shim wrote to stdout/stderr: %q", out)
	}
	return run
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok { //nolint:errorlint // exact type is what the assertion needs
		*target = e
		return true
	}
	return false
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // G304: test-controlled path
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestBrowserShimEmitsOSCForHTTPURLs(t *testing.T) {
	dir := t.TempDir()
	shim := installShimForTest(t, dir, browserShimPrimaryName)
	fallbackDir := t.TempDir()

	for _, url := range []string{
		"https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A8085%2Fcb",
		"http://localhost:8085/cb",
	} {
		t.Run(url, func(t *testing.T) {
			fakeOpener(t, fallbackDir, "xdg-open", filepath.Join(t.TempDir(), "unused"))
			run := runShim(t, shim, fallbackDir, url)

			if run.exitCode != 0 {
				t.Fatalf("exit code = %d, want 0", run.exitCode)
			}
			want := "\x1b]" + strconv.Itoa(BrowserOpenOSCIdent) + ";panemux-open;" + url + "\a"
			if got := readFileString(t, run.ttyPath); got != want {
				t.Fatalf("tty payload = %q, want %q", got, want)
			}
			if got := readFileString(t, run.argsFile); got != "" {
				t.Fatalf("shim fell through to the real opener: %q", got)
			}
		})
	}
}

func TestBrowserShimFallsThroughForNonHTTPArguments(t *testing.T) {
	dir := t.TempDir()
	shim := installShimForTest(t, dir, "xdg-open")
	fallbackDir := t.TempDir()

	cases := []struct {
		name string
		args []string
	}{
		{name: "local file", args: []string{"/tmp/sample-project/report.pdf"}},
		{name: "file scheme", args: []string{"file:///tmp/sample-project/index.html"}},
		{name: "javascript scheme", args: []string{"javascript:alert(1)"}},
		{name: "mail scheme", args: []string{"mailto:someone@example.com"}},
		{name: "flags before the url", args: []string{"-a", "Safari", "https://example.com/"}},
		{name: "no arguments", args: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "opener-args")
			fakeOpener(t, fallbackDir, "xdg-open", argsFile)

			run := runShim(t, shim, fallbackDir, tc.args...)

			if run.exitCode != 0 {
				t.Fatalf("exit code = %d, want 0", run.exitCode)
			}
			if got := readFileString(t, run.ttyPath); got != "" {
				t.Fatalf("shim emitted an OSC for %v: %q", tc.args, got)
			}
			gotArgs := strings.Split(strings.TrimSuffix(readFileString(t, argsFile), "\n"), "\n")
			if len(tc.args) == 0 {
				return
			}
			if strings.Join(gotArgs, "|") != strings.Join(tc.args, "|") {
				t.Fatalf("real opener got %v, want %v", gotArgs, tc.args)
			}
		})
	}
}

func TestBrowserShimFallsThroughWhenNoTerminalIsAvailable(t *testing.T) {
	dir := t.TempDir()
	shim := installShimForTest(t, dir, browserShimPrimaryName)
	fallbackDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "opener-args")
	fakeOpener(t, fallbackDir, "xdg-open", argsFile)

	cmd := exec.Command(shim, "https://example.com/") //nolint:gosec // G204: fixed test fixture path
	cmd.Env = append(os.Environ(),
		"PANEMUX_SHIM_TTY="+filepath.Join(t.TempDir(), "missing-dir", "tty"),
		"PANEMUX_SHIM_FALLBACK_PATH="+fallbackDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run shim: %v (output %q)", err, out)
	}
	if len(out) != 0 {
		t.Fatalf("shim wrote to stdout/stderr: %q", out)
	}
	if got := readFileString(t, argsFile); !strings.Contains(got, "https://example.com/") {
		t.Fatalf("real opener args = %q, want the URL passed through", got)
	}
}

// Without this guard a shim reachable from its own fallback PATH would exec
// itself forever.
func TestBrowserShimDoesNotRecurseIntoItself(t *testing.T) {
	dir := t.TempDir()
	shim := installShimForTest(t, dir, "xdg-open")

	run := runShim(t, shim, dir, "/tmp/sample-project/report.pdf")

	if run.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", run.exitCode)
	}
}

func TestBrowserShimExitsQuietlyWhenNoRealOpenerExists(t *testing.T) {
	dir := t.TempDir()
	shim := installShimForTest(t, dir, browserShimPrimaryName)

	run := runShim(t, shim, t.TempDir(), "/tmp/sample-project/report.pdf")

	if run.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", run.exitCode)
	}
}

func withShimCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := userCacheDirFn
	userCacheDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDirFn = prev })
	return dir
}

func withBrowserShimEnabled(t *testing.T, enabled bool) {
	t.Helper()
	prev := BrowserShimEnabled()
	SetBrowserShimEnabled(enabled)
	t.Cleanup(func() { SetBrowserShimEnabled(prev) })
}

// envValue returns the value a started process would see for key: os/exec
// deduplicates cmd.Env and keeps the last entry for a repeated name.
func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if name, value, ok := strings.Cut(env[i], "="); ok && name == key {
			return value, true
		}
	}
	return "", false
}

func TestInstallLocalBrowserShimWritesEveryOpenerName(t *testing.T) {
	cacheDir := withShimCacheDir(t)

	dir, err := installLocalBrowserShim()
	if err != nil {
		t.Fatalf("installLocalBrowserShim: %v", err)
	}
	if want := filepath.Join(cacheDir, "panemux", "bin"); dir != want {
		t.Fatalf("shim dir = %q, want %q", dir, want)
	}

	for _, name := range append([]string{browserShimPrimaryName}, browserShimAliases...) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s mode = %v, want owner-executable", name, info.Mode().Perm())
		}
	}
}

func TestInstallLocalBrowserShimIsRepeatable(t *testing.T) {
	withShimCacheDir(t)

	first, err := installLocalBrowserShim()
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	second, err := installLocalBrowserShim()
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if first != second {
		t.Fatalf("shim dir changed between installs: %q vs %q", first, second)
	}
	if got := readFileString(t, filepath.Join(second, browserShimPrimaryName)); got != browserShimScript {
		t.Fatal("second install did not leave the shim intact")
	}
}

func TestLocalBrowserShimEnvPointsAtTheShimAndKeepsTheOriginalPath(t *testing.T) {
	env := localBrowserShimEnv("/tmp/sample-project/bin", "/usr/bin:/bin")

	if got, _ := envValue(env, "BROWSER"); got != "/tmp/sample-project/bin/panemux-open" {
		t.Fatalf("BROWSER = %q", got)
	}
	if got, _ := envValue(env, "PATH"); got != "/tmp/sample-project/bin:/usr/bin:/bin" {
		t.Fatalf("PATH = %q", got)
	}
	if got, _ := envValue(env, "PANEMUX_SHIM_FALLBACK_PATH"); got != "/usr/bin:/bin" {
		t.Fatalf("PANEMUX_SHIM_FALLBACK_PATH = %q", got)
	}
}

func TestLocalBrowserShimEnvWithEmptyPath(t *testing.T) {
	env := localBrowserShimEnv("/tmp/sample-project/bin", "")

	if got, _ := envValue(env, "PATH"); got != "/tmp/sample-project/bin" {
		t.Fatalf("PATH = %q", got)
	}
}

func TestNewLocalExportsTheBrowserShim(t *testing.T) {
	withBrowserShimEnabled(t, true)
	cacheDir := withShimCacheDir(t)

	sess, err := NewLocal("shim-pane", "/bin/sh", "", "shim")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	defer sess.Close()

	shimDir := filepath.Join(cacheDir, "panemux", "bin")
	browser, ok := envValue(sess.cmd.Env, "BROWSER")
	if !ok || browser != filepath.Join(shimDir, browserShimPrimaryName) {
		t.Fatalf("BROWSER = %q, want the installed shim", browser)
	}
	path, _ := envValue(sess.cmd.Env, "PATH")
	if !strings.HasPrefix(path, shimDir+string(os.PathListSeparator)) && path != shimDir {
		t.Fatalf("PATH = %q, want the shim directory first", path)
	}
}

func TestNewLocalWithoutTheBrowserShim(t *testing.T) {
	withBrowserShimEnabled(t, false)
	withShimCacheDir(t)

	sess, err := NewLocal("plain-pane", "/bin/sh", "", "plain")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	defer sess.Close()

	// The operator's own BROWSER (if any) must survive untouched; only the
	// shim must be absent.
	if browser, _ := envValue(sess.cmd.Env, "BROWSER"); strings.Contains(browser, browserShimPrimaryName) {
		t.Fatalf("BROWSER = %q, want no shim when it is disabled", browser)
	}
}

func TestNewLocalStartsEvenWhenTheShimCannotBeInstalled(t *testing.T) {
	withBrowserShimEnabled(t, true)
	prev := userCacheDirFn
	userCacheDirFn = func() (string, error) { return "", os.ErrPermission }
	t.Cleanup(func() { userCacheDirFn = prev })

	sess, err := NewLocal("degraded-pane", "/bin/sh", "", "degraded")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	defer sess.Close()

	if sess.State() != StateConnected {
		t.Fatalf("state = %v, want the pane to start anyway", sess.State())
	}
	if browser, _ := envValue(sess.cmd.Env, "BROWSER"); strings.Contains(browser, browserShimPrimaryName) {
		t.Fatalf("BROWSER = %q, want no shim after a failed install", browser)
	}
}

// The generated remote snippet is executed by a real shell here, because a
// string-equality assertion would not catch a quoting or ordering mistake
// that only a shell can reveal.
func TestRemoteBrowserShimSetupInstallsTheShimWhenRunByAShell(t *testing.T) {
	home := t.TempDir()
	script := remoteBrowserShimSetup() + `printf '%s\n%s\n' "$BROWSER" "$PATH"`

	cmd := exec.Command("sh", "-c", script) //nolint:gosec // G204: fixed, non-tainted script under test
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run remote setup: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q, want BROWSER and PATH lines", out)
	}
	shimDir := filepath.Join(home, ".cache", "panemux", "bin")
	wantBrowser := filepath.Join(shimDir, browserShimPrimaryName)
	if lines[0] != wantBrowser {
		t.Fatalf("BROWSER = %q, want %q", lines[0], wantBrowser)
	}
	if lines[1] != shimDir+":/usr/bin:/bin" {
		t.Fatalf("PATH = %q, want the shim directory prepended", lines[1])
	}
	if got := readFileString(t, wantBrowser); got != browserShimScript {
		t.Fatal("installed remote shim content differs from the script")
	}
	for _, alias := range browserShimAliases {
		if _, err := os.Stat(filepath.Join(shimDir, alias)); err != nil {
			t.Fatalf("alias %s missing: %v", alias, err)
		}
	}
}

// A home directory the shim cannot be written into must not break the pane.
// HOME points below a regular file here, so mkdir fails for any user,
// including root in CI containers.
func TestRemoteBrowserShimSetupLeavesTheShellUsableWhenInstallFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	home := filepath.Join(blocker, "home")
	script := remoteBrowserShimSetup() + `printf '%s|%s\n' "${BROWSER:-unset}" "$PATH"`

	cmd := exec.Command("sh", "-c", script) //nolint:gosec // G204: fixed, non-tainted script under test
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run remote setup: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "unset|/usr/bin:/bin" {
		t.Fatalf("output = %q, want the environment left untouched", got)
	}
}

func TestSSHShellCommandWithoutTheBrowserShim(t *testing.T) {
	withBrowserShimEnabled(t, false)

	tests := []struct {
		name string
		want string
		cfg  SSHConfig
	}{
		{
			name: "plain pane uses the SSH shell request",
			cfg:  SSHConfig{},
			want: "",
		},
		{
			name: "cwd only",
			cfg:  SSHConfig{Cwd: "/remote/home/demo"},
			want: "cd '/remote/home/demo' && exec $SHELL",
		},
		{
			name: "shell only",
			cfg:  SSHConfig{Shell: "/bin/zsh"},
			want: "exec '/bin/zsh'",
		},
		{
			name: "shell and cwd",
			cfg:  SSHConfig{Shell: "/bin/zsh", Cwd: "/remote/home/demo"},
			want: "cd '/remote/home/demo' && exec '/bin/zsh'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sshShellCommand(tt.cfg)
			if err != nil {
				t.Fatalf("sshShellCommand: %v", err)
			}
			if got != tt.want {
				t.Fatalf("sshShellCommand = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSSHShellCommandWithTheBrowserShim(t *testing.T) {
	withBrowserShimEnabled(t, true)
	setup := remoteBrowserShimSetup()

	tests := []struct {
		name     string
		wantTail string
		cfg      SSHConfig
	}{
		{
			name:     "plain pane still starts a login shell",
			cfg:      SSHConfig{},
			wantTail: remoteLoginShellExec,
		},
		{
			name:     "cwd only keeps the existing exec form",
			cfg:      SSHConfig{Cwd: "/remote/home/demo"},
			wantTail: "cd '/remote/home/demo' && exec $SHELL",
		},
		{
			name:     "shell only keeps the existing exec form",
			cfg:      SSHConfig{Shell: "/bin/zsh"},
			wantTail: "exec '/bin/zsh'",
		},
		{
			name:     "shell and cwd keep the existing exec form",
			cfg:      SSHConfig{Shell: "/bin/zsh", Cwd: "/remote/home/demo"},
			wantTail: "cd '/remote/home/demo' && exec '/bin/zsh'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sshShellCommand(tt.cfg)
			if err != nil {
				t.Fatalf("sshShellCommand: %v", err)
			}
			if !strings.HasPrefix(got, setup) {
				t.Fatalf("command does not start with the shim setup: %q", got)
			}
			if tail := strings.TrimPrefix(got, setup); tail != tt.wantTail {
				t.Fatalf("command tail = %q, want %q", tail, tt.wantTail)
			}
		})
	}
}

func TestSSHShellCommandRejectsInvalidPathsWithTheShimEnabled(t *testing.T) {
	withBrowserShimEnabled(t, true)

	for _, cfg := range []SSHConfig{
		{Cwd: "/remote/home/demo; rm -rf /"},
		{Shell: "/bin/zsh; id"},
		{Shell: "relative/shell"},
	} {
		if _, err := sshShellCommand(cfg); err == nil {
			t.Fatalf("sshShellCommand(%+v) = nil, want a validation error", cfg)
		}
	}
}

// The whole remote command must be something a shell can parse, including
// the branch that starts a login shell.
func TestSSHShellCommandIsValidShellSyntax(t *testing.T) {
	withBrowserShimEnabled(t, true)

	for _, cfg := range []SSHConfig{
		{},
		{Cwd: "/remote/home/demo"},
		{Shell: "/bin/zsh", Cwd: "/remote/home/demo"},
	} {
		cmd, err := sshShellCommand(cfg)
		if err != nil {
			t.Fatalf("sshShellCommand: %v", err)
		}
		check := exec.Command("sh", "-n", "-c", cmd) //nolint:gosec // G204: syntax-checking the generated command
		if out, err := check.CombinedOutput(); err != nil {
			t.Fatalf("generated command is not valid shell syntax: %v\n%s\n%s", err, cmd, out)
		}
	}
}

// $SHELL is set by OpenSSH for exec sessions, but a pane must not die if some
// other server leaves it unset — before the shim, a pane with no cwd used the
// SSH shell request and never depended on $SHELL at all.
func TestRemoteLoginShellExecFallsBackWhenShellIsUnset(t *testing.T) {
	tests := []struct {
		name  string
		wantX string
		env   []string
	}{
		{name: "bash gets a login shell", env: []string{"SHELL=/bin/bash"}, wantX: "login"},
		{name: "zsh gets a login shell", env: []string{"SHELL=/usr/bin/zsh"}, wantX: "login"},
		{name: "fish gets a login shell", env: []string{"SHELL=/usr/local/bin/fish"}, wantX: "login"},
		{name: "unknown shell execs plainly", env: []string{"SHELL=/bin/ksh"}, wantX: "plain"},
		{name: "unset shell falls back", env: nil, wantX: "fallback"},
		{name: "empty shell falls back", env: []string{"SHELL="}, wantX: "fallback"},
	}

	// Replaces the exec targets with echoes so the branch taken is observable.
	probe := strings.NewReplacer(
		`exec "$SHELL" -l`, `printf login`,
		`exec "$SHELL"`, `printf plain`,
		`exec /bin/sh`, `printf fallback`,
	).Replace(remoteLoginShellExec)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", probe) //nolint:gosec // G204: fixed script under test
			cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, tt.env...)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("run login-shell branch: %v", err)
			}
			if string(out) != tt.wantX {
				t.Fatalf("branch = %q, want %q", out, tt.wantX)
			}
		})
	}
}
