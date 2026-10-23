package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
	"panemux/internal/session"
)

const testHookCommand = "/remote/home/demo/.local/bin/panemux-codex-permission-hook.sh"

// registerCase is one hooks.json for registerCodexHook. want is the
// PermissionRequest groups after registering, and keep a top-level key
// whose value must come through unchanged.
type registerCase struct {
	name       string
	in         string
	wantErr    string
	keep       string
	want       []string
	registered bool
}

func TestRegisterCodexHook(t *testing.T) {
	ours := `{"hooks":[{"type":"command","command":"` + testHookCommand + `","timeout":1}]}`
	cases := []registerCase{
		{name: "no file", in: "", want: []string{ours}},
		{name: "empty object", in: `{}`, want: []string{ours}},
		{name: "null", in: `null`, want: []string{ours}},
		{
			name: "other hooks and settings are kept",
			in: `{"other": {"x": 1.50}, "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "/bin/true"}]}],` +
				` "PermissionRequest": [{"matcher": "x", "hooks": [{"type": "command", "command": "/bin/other"}]}]}}`,
			want: []string{`{"matcher":"x","hooks":[{"type":"command","command":"/bin/other"}]}`, ours},
			keep: "other",
		},
		{
			name:       "already registered",
			in:         `{"hooks":{"PermissionRequest":[` + ours + `]}}`,
			registered: true,
			want:       []string{ours},
		},
		{
			name: "a group of another shape is left alone",
			in:   `{"hooks":{"PermissionRequest":["odd"]}}`,
			want: []string{`"odd"`, ours},
		},
		{name: "not JSON", in: `{"hooks":`, wantErr: "is not valid JSON"},
		{name: "not an object", in: `[]`, wantErr: "is not a JSON object"},
		{name: "hooks not an object", in: `{"hooks":[]}`, wantErr: `"hooks" is not an object`},
		{
			name:    "PermissionRequest not a list",
			in:      `{"hooks":{"PermissionRequest":{}}}`,
			wantErr: `"hooks.PermissionRequest" is not a list`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkRegisterCodexHook(t, tc) })
	}
}

func TestRegisterCodexHookKeepsHTMLCharacters(t *testing.T) {
	in := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"a && b <x> c"}]}]}}`
	merged, _, err := registerCodexHook([]byte(in), "/remote/home/a&b/<hook>")
	require.NoError(t, err)
	assert.Contains(t, string(merged), `"a && b <x> c"`)
	assert.Contains(t, string(merged), `"/remote/home/a&b/<hook>"`)
}

func checkRegisterCodexHook(t *testing.T, tc registerCase) {
	t.Helper()
	var in []byte
	if tc.in != "" {
		in = []byte(tc.in)
	}
	merged, registered, err := registerCodexHook(in, testHookCommand)
	if tc.wantErr != "" {
		require.Error(t, err)
		assert.Contains(t, err.Error(), tc.wantErr)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, tc.registered, registered)
	var doc struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(merged, &doc))
	got := make([]string, 0, len(doc.Hooks["PermissionRequest"]))
	for _, group := range doc.Hooks["PermissionRequest"] {
		got = append(got, compactJSON(t, group))
	}
	assert.Equal(t, tc.want, got)
	if tc.keep != "" {
		var top map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(merged, &top))
		assert.JSONEq(t, `{"x": 1.50}`, string(top[tc.keep]))
		assert.Contains(t, string(merged), "1.50", "numbers are kept as written")
		assert.Contains(t, string(doc.Hooks["Stop"][0]), "/bin/true")
	}
	if !tc.registered {
		assert.True(t, strings.HasSuffix(string(merged), "\n"))
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, json.Compact(&b, raw))
	return b.String()
}

// codexHookOutput is what codexHookReadScript prints for a host with codex,
// a hook file (nil for none) and a hooks.json (nil for none).
func codexHookOutput(hook []byte, executable bool, hooksJSON []byte) []byte {
	var b strings.Builder
	b.WriteString("::home /remote/home/demo\n")
	for i, f := range [][]byte{hook, hooksJSON} {
		if f == nil {
			b.WriteString("::none\n")
			continue
		}
		mode := "-"
		if i == 0 && executable {
			mode = "x"
		}
		b.WriteString("::file " + strconv.Itoa(len(f)) + " " + mode + "\n")
		b.Write(f)
		b.WriteString("\n")
	}
	b.WriteString("::end\n")
	return []byte(b.String())
}

// statusCase is one output of codexHookReadScript (or a failed run) and
// the status it reads as.
type statusCase struct {
	name    string
	want    CodexHookStatus
	wantErr string
	runErr  error
	out     []byte
}

var (
	testRegistered = []byte(`{"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"` +
		testHookCommand + `"}]}]}}`)
	testHook = []byte(codexPermissionHook)
)

func TestCodexHookStatus(t *testing.T) {
	hook, registered := testHook, testRegistered
	checkCodexHookStatuses(t, []statusCase{
		{name: "no codex", out: []byte("::codex none\n"), want: CodexHookAbsent},
		{name: "nothing installed", out: codexHookOutput(nil, false, nil), want: CodexHookMissing},
		{name: "hooks.json without it", out: codexHookOutput(nil, false, []byte(`{"hooks":{}}`)), want: CodexHookMissing},
		{name: "installed", out: codexHookOutput(hook, true, registered), want: CodexHookInstalled},
		{
			name: "a login banner before the output is ignored",
			out:  append([]byte("Welcome\n::not ours\n"), codexHookOutput(hook, true, registered)...),
			want: CodexHookInstalled,
		},
		{name: "older hook", out: codexHookOutput([]byte("#!/bin/sh\nexit 0\n"), true, registered), want: CodexHookOutdated},
		{name: "hook not executable", out: codexHookOutput(hook, false, registered), want: CodexHookOutdated},
		{name: "hook not registered", out: codexHookOutput(hook, true, nil), want: CodexHookOutdated},
		{name: "registered without the hook", out: codexHookOutput(nil, false, registered), want: CodexHookOutdated},
		{
			name:    "unreadable hooks.json",
			out:     codexHookOutput(hook, true, []byte(`{"hooks":`)),
			want:    CodexHookInvalid,
			wantErr: "~/.codex/hooks.json is not valid JSON",
		},
		{
			name: "an empty hooks.json is a file, not none", out: codexHookOutput(hook, true, []byte{}),
			want: CodexHookInvalid, wantErr: "is not valid JSON",
		},
		{name: "host fails", runErr: errors.New("boom"), want: CodexHookUnknown, wantErr: "boom"},
		{
			name: "a hooks.json too large to read",
			out:  []byte("::home /remote/home/demo\n::none\n::large 2000000\n::end\n"),
			want: CodexHookInvalid, wantErr: "~/.codex/hooks.json is larger than 1 MiB",
		},
		{
			name: "a hook file too large to read is not ours",
			out:  []byte("::home /remote/home/demo\n::large 2000000\n::none\n::end\n"),
			want: CodexHookOutdated,
		},
	})
}

func TestCodexHookCheckDoesNotReadALargeHooksJSON(t *testing.T) {
	home := codexHome(t, `{"x":"`+strings.Repeat("a", codexHookMaxRead)+`"}`)
	homedir.SetForTest(t, home)
	var out []byte
	s := New(Options{
		Hosts: func() []string { return nil },
		RunLocal: func(ctx context.Context, script string) ([]byte, error) {
			var err error
			out, err = runLocal(ctx, script)
			return out, err
		},
	})
	defer s.Close()
	got := codexHookStatus(t, s, "")
	assert.Equal(t, CodexHookInvalid, got.Status)
	assert.Contains(t, got.Error, "larger than 1 MiB")
	assert.Less(t, len(out), 4096, "the file is not printed")
}

func TestCodexHookStatusOfOutputCutShort(t *testing.T) {
	const early = "ended early"
	checkCodexHookStatuses(t, []statusCase{
		{name: "no output", out: []byte(""), want: CodexHookUnknown, wantErr: early},
		{name: "only the home line", out: []byte("::home /remote/home/demo\n"), want: CodexHookUnknown, wantErr: early},
		{name: "the home line cut short", out: []byte("::home /remote/home/demo"), want: CodexHookUnknown, wantErr: early},
		{
			name: "a file cut short", out: []byte("::home /remote/home/demo\n::file 900 x\n#!/bin/sh\n"),
			want: CodexHookUnknown, wantErr: early,
		},
		{
			name: "content shorter than its size", out: []byte("::home /remote/home/demo\n::file 3 x\nabc"),
			want: CodexHookUnknown, wantErr: early,
		},
		{
			name: "no end marker", out: bytes.TrimSuffix(codexHookOutput(testHook, true, testRegistered), []byte("::end\n")),
			want: CodexHookUnknown, wantErr: early,
		},
	})
}

func checkCodexHookStatuses(t *testing.T, cases []statusCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{
				Hosts:    func() []string { return nil },
				RunLocal: func(context.Context, string) ([]byte, error) { return tc.out, tc.runErr },
			})
			defer s.Close()
			got, err := s.CodexHook(context.Background(), "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Status)
			if tc.wantErr == "" {
				assert.Empty(t, got.Error)
			} else {
				assert.Contains(t, got.Error, tc.wantErr)
			}
		})
	}
}

func TestCodexHookUnknownHost(t *testing.T) {
	s := New(Options{Hosts: func() []string { return []string{"build-box"} }})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "elsewhere")
	require.ErrorIs(t, err, ErrUnknownHost)
	_, err = s.CodexHook(context.Background(), "elsewhere")
	require.ErrorIs(t, err, ErrUnknownHost)
}

// codexHome is a HOME with ~/.codex, and hooks.json when given.
func codexHome(t *testing.T, hooksJSON string) string {
	t.Helper()
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".codex"), 0o700))
	if hooksJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(hooksJSON), 0o600))
	}
	return home
}

func TestInstallCodexHookLocal(t *testing.T) {
	existing := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/bin/true"}]}]},"feature":true}`
	home := codexHome(t, existing)
	homedir.SetForTest(t, home)
	s := New(Options{Hosts: func() []string { return nil }})
	defer s.Close()

	assert.Equal(t, CodexHookMissing, codexHookStatus(t, s, "").Status)
	got, err := s.InstallCodexHook(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, CodexHook{Status: CodexHookInstalled}, got)

	assertCodexHookInstalled(t, home)
	hooksJSON, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	assert.Contains(t, string(hooksJSON), `"feature": true`)
	assert.Contains(t, string(hooksJSON), `"command": "/bin/true"`)

	// Installing again changes nothing.
	got, err = s.InstallCodexHook(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, CodexHookInstalled, got.Status)
	again, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	assert.Equal(t, string(hooksJSON), string(again))
}

func TestInstallCodexHookUpdatesTheScriptOnly(t *testing.T) {
	home := codexHome(t, "")
	homedir.SetForTest(t, home)
	command := filepath.Join(home, ".local", "bin", "panemux-codex-permission-hook.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(command), 0o700))
	require.NoError(t, os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o600))
	hooksJSON := `{"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"` + command + `","timeout":1}]}]}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(hooksJSON), 0o600))
	s := New(Options{Hosts: func() []string { return nil }})
	defer s.Close()

	assert.Equal(t, CodexHookOutdated, codexHookStatus(t, s, "").Status)
	got, err := s.InstallCodexHook(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, CodexHookInstalled, got.Status)
	after, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	assert.Equal(t, hooksJSON, string(after), "a registered hook leaves hooks.json as it was")
	assertCodexHookInstalled(t, home)
}

func TestInstallCodexHookCreatesHooksJSON(t *testing.T) {
	home := codexHome(t, "")
	homedir.SetForTest(t, home)
	s := New(Options{Hosts: func() []string { return nil }})
	defer s.Close()

	got, err := s.InstallCodexHook(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, CodexHookInstalled, got.Status)
	assertCodexHookInstalled(t, home)
}

func TestInstallCodexHookRefusesUnreadableHooksJSON(t *testing.T) {
	for _, bad := range []string{`{"hooks":`, `{"hooks":{"PermissionRequest":"x"}}`} {
		home := codexHome(t, bad)
		homedir.SetForTest(t, home)
		s := New(Options{Hosts: func() []string { return nil }})

		got := codexHookStatus(t, s, "")
		assert.Equal(t, CodexHookInvalid, got.Status)
		_, err := s.InstallCodexHook(context.Background(), "")
		require.Error(t, err)
		assert.Equal(t, got.Error, err.Error())
		after, readErr := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
		require.NoError(t, readErr)
		assert.Equal(t, bad, string(after))
		_, statErr := os.Stat(filepath.Join(home, ".local", "bin", "panemux-codex-permission-hook.sh"))
		assert.True(t, os.IsNotExist(statErr), "nothing is installed")
		s.Close()
	}
}

func TestInstallCodexHookWithoutCodex(t *testing.T) {
	s := New(Options{
		Hosts:    func() []string { return nil },
		RunLocal: func(context.Context, string) ([]byte, error) { return []byte("::codex none\n"), nil },
	})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no codex")
}

func TestInstallCodexHookReportsAFailedWrite(t *testing.T) {
	home := codexHome(t, "")
	homedir.SetForTest(t, home)
	// ~/.local is a file, so ~/.local/bin cannot be made.
	require.NoError(t, os.WriteFile(filepath.Join(home, ".local"), nil, 0o600))
	s := New(Options{Hosts: func() []string { return nil }})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir", "the shell's own reason is reported")
	// uutils mkdir (Ubuntu 26.04) leaves the path out of its message, so the
	// script names the directory itself (issue #423).
	assert.Contains(t, err.Error(), "~/.local/bin could not be created")
	_, statErr := os.Stat(filepath.Join(home, ".codex", "hooks.json"))
	assert.True(t, os.IsNotExist(statErr), "hooks.json is not written when the hook could not be")
}

func TestInstallCodexHookOverSSHReportsWhyItFailed(t *testing.T) {
	home := codexHome(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".local"), nil, 0o600))
	conn := &shConn{home: home}
	s := New(Options{
		Hosts: func() []string { return []string{"build-box"} },
		Dial:  func(string) (Conn, error) { return conn, nil },
	})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "build-box")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir", "stderr, which ssh does not return, is reported")
}

// A hooks.json write that fails part way leaves the file as it was.
func TestInstallCodexHookKeepsHooksJSONWhenTheWriteFails(t *testing.T) {
	existing := `{"keep":"` + strings.Repeat("a", 200_000) + `"}`
	home := codexHome(t, existing)
	// 40 blocks fit the hook script but not hooks.json, in 512- or
	// 1024-byte blocks.
	conn := &shConn{home: home, prefix: "ulimit -f 40\n"}
	s := New(Options{
		Hosts: func() []string { return []string{"build-box"} },
		Dial:  func(string) (Conn, error) { return conn, nil },
	})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "build-box")
	require.Error(t, err)
	after, readErr := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, readErr)
	assert.Equal(t, existing, string(after))
	left, dirErr := os.ReadDir(filepath.Join(home, ".codex"))
	require.NoError(t, dirErr)
	require.Len(t, left, 1, "no temporary file is left")
	assert.Equal(t, "hooks.json", left[0].Name())
}

// A hooks.json that is a symbolic link stays one, and keeps its mode.
//
//efficacy:exempt guards the in-place write the temporary file must keep; it held before
func TestInstallCodexHookWritesThroughALink(t *testing.T) {
	home := codexHome(t, "")
	target := filepath.Join(t.TempDir(), "hooks.json")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(home, ".codex", "hooks.json")))
	homedir.SetForTest(t, home)
	s := New(Options{Hosts: func() []string { return nil }})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "")
	require.NoError(t, err)
	assertCodexHookInstalled(t, home)
	info, err := os.Lstat(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	assert.Equal(t, os.ModeSymlink, info.Mode().Type())
	info, err = os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// shConn is an ssh_connections host whose `sh -s` runs locally with HOME set
// to home, so the remote path is exercised against real files.
type shConn struct {
	home string
	// prefix runs before each script.
	prefix string
	cmds   []string
}

func (c *shConn) Run(ctx context.Context, cmd string, stdin io.Reader) ([]byte, error) {
	c.cmds = append(c.cmds, cmd)
	run := exec.CommandContext(ctx, "sh", "-s")
	run.Stdin = io.MultiReader(strings.NewReader(c.prefix), stdin)
	run.Env = append(os.Environ(), "HOME="+c.home)
	// ssh's Session.Output returns no stderr, so neither does this.
	run.Stderr = io.Discard
	return run.Output()
}

func (c *shConn) InspectGitContext(context.Context, string) (session.GitContext, error) {
	return session.GitContext{}, nil
}
func (c *shConn) Ping(context.Context) error { return nil }
func (c *shConn) Close() error               { return nil }

func TestInstallCodexHookOverSSH(t *testing.T) {
	home := codexHome(t, `{"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"/bin/other"}]}]}}`)
	conn := &shConn{home: home}
	s := New(Options{
		Hosts: func() []string { return []string{"build-box"} },
		Dial:  func(string) (Conn, error) { return conn, nil },
		RunLocal: func(context.Context, string) ([]byte, error) {
			return nil, errors.New("the panemux host is not used")
		},
	})
	defer s.Close()

	assert.Equal(t, CodexHookMissing, codexHookStatus(t, s, "build-box").Status)
	got, err := s.InstallCodexHook(context.Background(), "build-box")
	require.NoError(t, err)
	assert.Equal(t, CodexHookInstalled, got.Status)
	assertCodexHookInstalled(t, home, `{"type":"command","command":"/bin/other"}`)
	for _, cmd := range conn.cmds {
		assert.Equal(t, "sh -s", cmd, "only the literal command crosses the remote shell")
	}
}

// assertCodexHookInstalled checks the hook file under home and that
// hooks.json registers it, after the other PermissionRequest hooks given.
func assertCodexHookInstalled(t *testing.T, home string, others ...string) {
	t.Helper()
	command := filepath.Join(home, ".local", "bin", "panemux-codex-permission-hook.sh")
	data, err := os.ReadFile(command)
	require.NoError(t, err)
	assert.Equal(t, codexPermissionHook, string(data))
	info, err := os.Stat(command)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	raw, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	var doc struct {
		Hooks struct {
			PermissionRequest []struct {
				Hooks []json.RawMessage `json:"hooks"`
			} `json:"PermissionRequest"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	var got []string
	for _, group := range doc.Hooks.PermissionRequest {
		for _, h := range group.Hooks {
			got = append(got, compactJSON(t, h))
		}
	}
	want := append(append([]string{}, others...),
		`{"type":"command","command":`+string(mustJSON(t, command))+`,"timeout":1}`)
	assert.Equal(t, want, got)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func codexHookStatus(t *testing.T, s *Service, host string) CodexHook {
	t.Helper()
	got, err := s.CodexHook(context.Background(), host)
	require.NoError(t, err)
	return got
}

func TestCodexHookOnAHostStillConnecting(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	s := New(Options{
		Hosts:       func() []string { return []string{"build-box"} },
		Dial:        func(string) (Conn, error) { <-release; return nil, errors.New("closed") },
		HostTimeout: 20 * time.Millisecond,
	})
	defer s.Close()
	got := codexHookStatus(t, s, "build-box")
	assert.Equal(t, CodexHook{Status: CodexHookUnknown, Error: "build-box is still connecting"}, got)
}

func TestInstallCodexHookWithoutRandomness(t *testing.T) {
	home := codexHome(t, "")
	homedir.SetForTest(t, home)
	s := New(Options{Hosts: func() []string { return nil }, Rand: iotest.ErrReader(errors.New("no entropy"))})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "")
	require.ErrorContains(t, err, "no entropy")
	_, statErr := os.Stat(filepath.Join(home, ".local", "bin", "panemux-codex-permission-hook.sh"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestCodexHookWriteError(t *testing.T) {
	cases := []struct {
		name, out, want string
	}{
		{name: "success", out: "::status 0\n"},
		{name: "success after other output", out: "note\n::status 0\n"},
		{
			name: "the shell's reason",
			out:  "sh: cannot create x: Permission denied\n::status 2\n", want: "sh: cannot create x: Permission denied",
		},
		{name: "no reason printed", out: "::status 153\n", want: "exit status 153"},
		{name: "no output", out: "", want: "ended early"},
		{name: "no status line", out: "mkdir: x\n", want: "ended early"},
		{name: "the marker not at a line start", out: "x::status 0\n", want: "ended early"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := codexHookWriteError([]byte(tc.out))
			if tc.want == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tc.want, strings.TrimPrefix(err.Error(), "its output "))
		})
	}
}

func TestInstallCodexHookReportsAFailedRun(t *testing.T) {
	calls := 0
	s := New(Options{
		Hosts: func() []string { return nil },
		RunLocal: func(context.Context, string) ([]byte, error) {
			calls++
			if calls == 1 {
				return codexHookOutput(nil, false, nil), nil
			}
			return nil, errors.New("connection lost")
		},
	})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "")
	require.ErrorContains(t, err, "connection lost")
	assert.Equal(t, 2, calls)
}

// A copy over hooks.json that fails part way keeps the temporary file, whose
// content is the user's hooks with panemux's entry, and the error names it.
func TestInstallCodexHookKeepsTheNewContentWhenTheCopyFails(t *testing.T) {
	home := codexHome(t, `{"keep":"me"}`)
	bin := t.TempDir()
	// cat fails part way only when it copies the temporary file.
	fakeCat := "#!/bin/sh\ncase \"$1\" in\n*/.hooks.json.panemux-*) head -c 5 \"$1\"; " +
		"echo 'cat: write error: No space left on device' >&2; exit 1;;\nesac\nexec /bin/cat \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "cat"), []byte(fakeCat), 0o700)) //nolint:gosec // test fixture
	conn := &shConn{home: home, prefix: "PATH='" + bin + "':$PATH\n"}
	s := New(Options{
		Hosts: func() []string { return []string{"build-box"} },
		Dial:  func(string) (Conn, error) { return conn, nil },
	})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "build-box")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No space left on device")
	left, dirErr := filepath.Glob(filepath.Join(home, ".codex", ".hooks.json.panemux-*"))
	require.NoError(t, dirErr)
	require.Len(t, left, 1, "the temporary file is kept")
	assert.Contains(t, err.Error(), left[0])
	kept, readErr := os.ReadFile(left[0])
	require.NoError(t, readErr)
	assert.Contains(t, string(kept), `"keep": "me"`)
	assert.Contains(t, string(kept), "panemux-codex-permission-hook.sh")
}

// A hooks.json that cannot be opened for writing is a plain failure: the file
// is untouched, so no temporary file is kept.
func TestInstallCodexHookRefusesAReadOnlyHooksJSON(t *testing.T) {
	const existing = `{"keep":"me"}`
	home := codexHome(t, existing)
	hooksJSON := filepath.Join(home, ".codex", "hooks.json")
	require.NoError(t, os.Chmod(hooksJSON, 0o400))
	homedir.SetForTest(t, home)
	s := New(Options{Hosts: func() []string { return nil }})
	defer s.Close()
	_, err := s.InstallCodexHook(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "~/.codex/hooks.json is not writable")
	assert.NotContains(t, err.Error(), "not fully written")
	after, readErr := os.ReadFile(hooksJSON)
	require.NoError(t, readErr)
	assert.Equal(t, existing, string(after))
	left, dirErr := os.ReadDir(filepath.Join(home, ".codex"))
	require.NoError(t, dirErr)
	require.Len(t, left, 1, "no temporary file is left")
}
