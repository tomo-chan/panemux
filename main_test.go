package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

func TestVersionDefault(t *testing.T) {
	if version != "dev" {
		t.Errorf("expected default version 'dev', got %q", version)
	}
}

// The root package's startup path used to be untestable end to end:
// parseOptions read the process-global flag.CommandLine, loadConfig called
// config.Load directly, and startSessionsFromConfig called
// session.CreateFromConfig directly — so none of them could be driven without
// spawning real sessions or reading the developer's own config. Each now
// takes its dependency as an argument (DEVELOPMENT.md's testability rule,
// docs/quality-gateway.md's principle P3), which is what lets the tests below
// exist and is why the root package joined COVERAGE_PKGS.

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want cliOptions
	}{
		{
			name: "no arguments uses the documented defaults",
			args: nil,
			want: cliOptions{},
		},
		{
			name: "config path",
			args: []string{"--config", "/tmp/sample-project/config.yaml"},
			want: cliOptions{configPath: "/tmp/sample-project/config.yaml"},
		},
		{
			name: "port override",
			args: []string{"--port", "9090"},
			want: cliOptions{port: 9090},
		},
		{
			name: "open browser",
			args: []string{"--open"},
			want: cliOptions{openBrowser: true},
		},
		{
			name: "version",
			args: []string{"--version"},
			want: cliOptions{showVersion: true},
		},
		{
			name: "single-dash spelling, as the README uses",
			args: []string{"-port", "9090", "-open"},
			want: cliOptions{port: 9090, openBrowser: true},
		},
		{
			name: "every flag at once",
			args: []string{"--config", "/tmp/sample-project/config.yaml", "--port", "1", "--open", "--version"},
			want: cliOptions{configPath: "/tmp/sample-project/config.yaml", port: 1, openBrowser: true, showVersion: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOptions(tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseOptions_UnknownFlag_ReturnsError(t *testing.T) {
	_, err := parseOptions([]string{"--nonesuch"})
	assert.Error(t, err)
}

func TestParseOptions_NonNumericPort_ReturnsError(t *testing.T) {
	_, err := parseOptions([]string{"--port", "http"})
	assert.Error(t, err)
}

// parseOptions must not touch the process-global flag set: main() is not the
// only caller any more, and a second call would panic on redefined flags if
// it did.
func TestParseOptions_IsRepeatable(t *testing.T) {
	first, err := parseOptions([]string{"--port", "1"})
	require.NoError(t, err)
	second, err := parseOptions([]string{"--port", "2"})
	require.NoError(t, err)

	assert.Equal(t, 1, first.port)
	assert.Equal(t, 2, second.port)
}

func TestLoadConfig_ExplicitPath_UsesLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := &config.Config{Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"}}
	var loadedPath string
	loader := configLoader{
		load: func(path string) (*config.Config, error) {
			loadedPath = path
			return want, nil
		},
		loadOrDefault: func() (*config.Config, error) {
			t.Fatal("loadOrDefault must not be called when --config is given")
			return nil, nil
		},
	}

	got, err := loadConfig(cliOptions{configPath: "/tmp/sample-project/config.yaml"}, loader)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/sample-project/config.yaml", loadedPath)
	assert.Same(t, want, got)
}

func TestLoadConfig_NoPath_UsesLoadOrDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := &config.Config{Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"}}
	loader := configLoader{
		load: func(string) (*config.Config, error) {
			t.Fatal("load must not be called without --config")
			return nil, nil
		},
		loadOrDefault: func() (*config.Config, error) { return want, nil },
	}

	got, err := loadConfig(cliOptions{}, loader)
	require.NoError(t, err)
	assert.Same(t, want, got)
}

func TestLoadConfig_PortOverride_WinsOverFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	loader := configLoader{
		loadOrDefault: func() (*config.Config, error) {
			return &config.Config{Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"}}, nil
		},
	}

	got, err := loadConfig(cliOptions{port: 9090}, loader)
	require.NoError(t, err)
	assert.Equal(t, 9090, got.Server.Port)
}

// Port 0 means "not given", not "port zero": the flag's own zero value must
// leave whatever the config file said intact.
func TestLoadConfig_ZeroPort_LeavesConfigPortAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	loader := configLoader{
		loadOrDefault: func() (*config.Config, error) {
			return &config.Config{Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"}}, nil
		},
	}

	got, err := loadConfig(cliOptions{port: 0}, loader)
	require.NoError(t, err)
	assert.Equal(t, 8080, got.Server.Port)
}

func TestLoadConfig_LoadError_IsWrapped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sentinel := errors.New("config file is unreadable")
	loader := configLoader{
		load: func(string) (*config.Config, error) { return nil, sentinel },
	}

	_, err := loadConfig(cliOptions{configPath: "/tmp/sample-project/config.yaml"}, loader)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "loading config")
}

// EnsureAuthToken runs as part of loading, so a config that arrives without a
// token leaves loadConfig with one. HOME is a temp directory here, so the
// token file lands there rather than in the developer's own ~/.config.
func TestLoadConfig_GeneratesAuthToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	loader := configLoader{
		loadOrDefault: func() (*config.Config, error) {
			return &config.Config{Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"}}, nil
		},
	}

	got, err := loadConfig(cliOptions{}, loader)
	require.NoError(t, err)
	assert.NotEmpty(t, got.Server.AuthToken)

	written, err := os.ReadFile(filepath.Join(home, ".config", "panemux", "token"))
	require.NoError(t, err)
	assert.Equal(t, got.Server.AuthToken, string(written))
}

func TestLoadConfig_ExistingAuthToken_IsKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	loader := configLoader{
		loadOrDefault: func() (*config.Config, error) {
			return &config.Config{
				Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1", AuthToken: "already-set"},
			}, nil
		},
	}

	got, err := loadConfig(cliOptions{}, loader)
	require.NoError(t, err)
	assert.Equal(t, "already-set", got.Server.AuthToken)
}

func twoPaneConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8080, Host: "127.0.0.1"},
		Workspaces: config.WorkspacesConfig{
			Active: "default",
			Items: []config.WorkspaceConfig{{
				ID:    "default",
				Title: "Default",
				Layout: config.LayoutNode{
					Direction: "horizontal",
					Children: []config.LayoutChild{
						{Size: 50, Pane: &config.PaneConfig{ID: "pane-a", Type: "local"}},
						{Size: 50, Pane: &config.PaneConfig{ID: "pane-b", Type: "local"}},
					},
				},
			}},
		},
	}
}

func TestStartSessionsFromConfig_AddsEveryPane(t *testing.T) {
	mgr := session.NewManager()
	var createdIDs []string
	create := func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		createdIDs = append(createdIDs, pane.ID)
		return &fakeBoardSession{id: pane.ID}, nil
	}

	require.NoError(t, startSessionsFromConfig(twoPaneConfig(), mgr, create))

	assert.Equal(t, []string{"pane-a", "pane-b"}, createdIDs)
	assert.Len(t, mgr.List(), 2)
}

// One pane failing to start must not take the others down with it: a bad SSH
// host in a four-pane layout should still leave three working panes.
func TestStartSessionsFromConfig_OnePaneFails_OthersStillStart(t *testing.T) {
	mgr := session.NewManager()
	create := func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		if pane.ID == "pane-a" {
			return nil, errors.New("connection refused")
		}
		return &fakeBoardSession{id: pane.ID}, nil
	}

	require.NoError(t, startSessionsFromConfig(twoPaneConfig(), mgr, create))

	_, ok := mgr.Get("pane-a")
	assert.False(t, ok, "the pane that failed to start must not be registered")
	_, ok = mgr.Get("pane-b")
	assert.True(t, ok)
}

func TestStartSessionsFromConfig_EveryPaneFails_StillReturnsNil(t *testing.T) {
	mgr := session.NewManager()
	create := func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		return nil, errors.New("connection refused")
	}

	// Startup is deliberately best-effort: panemux comes up with an empty
	// pane rather than refusing to start, so the operator can fix the config
	// from the dashboard itself.
	require.NoError(t, startSessionsFromConfig(twoPaneConfig(), mgr, create))
	assert.Empty(t, mgr.List())
}

func TestStartSessionsFromConfig_NoPanes_IsANoOp(t *testing.T) {
	mgr := session.NewManager()
	create := func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error) {
		t.Fatal("nothing should be created for a config with no panes")
		return nil, nil
	}

	require.NoError(t, startSessionsFromConfig(&config.Config{}, mgr, create))
	assert.Empty(t, mgr.List())
}

// startSessionsFromConfig hands the config's SSH connection map straight to
// the factory; a pane referencing an alias defined there must see it.
func TestStartSessionsFromConfig_PassesSSHConnections(t *testing.T) {
	cfg := twoPaneConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"demo": {Host: "demo.invalid", User: "demo"}}

	var seen map[string]config.SSHConnection
	create := func(_ *config.PaneConfig, conns map[string]config.SSHConnection) (session.Session, error) {
		seen = conns
		return &fakeBoardSession{id: "x"}, nil
	}

	require.NoError(t, startSessionsFromConfig(cfg, session.NewManager(), create))
	assert.Equal(t, cfg.SSHConnections, seen)
}

func TestBrowserOpenArgv(t *testing.T) {
	const url = "http://127.0.0.1:8080"

	tests := []struct {
		name     string
		goos     string
		wantName string
		wantArgs []string
		wantOK   bool
	}{
		{
			name:     "macOS opens Chrome by application name",
			goos:     goosDarwin,
			wantName: "open",
			wantArgs: []string{"-a", "Google Chrome", url},
			wantOK:   true,
		},
		{
			name:     "linux uses Chrome's own app mode",
			goos:     goosLinux,
			wantName: "google-chrome",
			wantArgs: []string{"--app=" + url},
			wantOK:   true,
		},
		{
			name:     "windows goes through the shell's start verb",
			goos:     goosWindows,
			wantName: "cmd",
			wantArgs: []string{"/c", "start", "chrome", url},
			wantOK:   true,
		},
		{
			name:   "an unsupported OS opens nothing rather than guessing",
			goos:   "plan9",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, args, ok := browserOpenArgv(tc.goos, url)
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}
			assert.Equal(t, tc.wantName, name)
			assert.Equal(t, tc.wantArgs, args)
		})
	}
}

// The URL is the only caller-derived value that reaches an exec argv here.
// It is always panemux's own listen address, and it travels as a discrete
// argument rather than through a shell — see docs/security.md's general
// rules — so this pins that it is never concatenated into one string.
func TestBrowserOpenArgv_URLStaysADiscreteArgument(t *testing.T) {
	const url = "http://127.0.0.1:8080"

	for _, goos := range []string{goosDarwin, goosLinux, goosWindows} {
		name, args, ok := browserOpenArgv(goos, url)
		require.True(t, ok)
		assert.NotContains(t, name, url)
		for _, arg := range args {
			if arg == url || arg == "--app="+url {
				continue
			}
			assert.NotContains(t, arg, url, "the URL must not be spliced into another argument")
		}
	}
}
