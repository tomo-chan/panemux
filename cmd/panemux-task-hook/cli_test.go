package main

import (
	"bytes"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"panemux/internal/taskhook"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCLIRejectsUnsafeArgumentsWithoutReflectingPrivateData(t *testing.T) {
	for _, args := range [][]string{nil, {"private-value"}, {"selfcheck", "private-value", "/private-fixture"},
		{"selfcheck", rootFlag, t.TempDir(), "private-value"},
		{managedMode, rootFlag, t.TempDir()},
		{managedMode, rootFlag, t.TempDir(), "--scope", "true"},
	} {
		var out, diagnostic bytes.Buffer
		if cli(args, strings.NewReader("private-body"), &out, &diagnostic) != 1 || out.Len() != 0 ||
			diagnostic.String() != "task hook helper unavailable\n" {

			t.Fatal("unsafe CLI failure", args)
		}
	}
	for _, args := range [][]string{{hookMode}, {hookMode, "wrong", "private"},
		{hookMode, rootFlag, t.TempDir(), "extra"}, {hookMode, rootFlag, t.TempDir()}} {
		var out, diagnostic bytes.Buffer
		if cli(args, strings.NewReader("private-body"), &out, &diagnostic) != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
			t.Fatal("hook failure escaped silent fallback")
		}
	}
	var out, diagnostic bytes.Buffer
	if cli([]string{"selfcheck", rootFlag, t.TempDir()}, strings.NewReader(""), &out, &diagnostic) != 0 ||
		!strings.Contains(out.String(), `"protocol_version":1`) || diagnostic.Len() != 0 {

		t.Fatal("health unavailable")
	}
}

func TestMainUsesCLIExitStatus(t *testing.T) {
	originalArgs, originalExit, originalOut := os.Args, exitProcess, os.Stdout
	defer func() { os.Args = originalArgs; exitProcess = originalExit; os.Stdout = originalOut }()
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	os.Stdout = file
	os.Args = []string{"helper", "selfcheck", rootFlag, t.TempDir()}
	status := -1
	exitProcess = func(code int) { status = code }
	main()
	if status != 0 {
		t.Fatal("main ignored CLI result")
	}
}

func TestCLIManagedRegistrationUsesKernelParentAndUnknownScope(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	target := taskhook.TargetIdentity{Version: 1, UID: os.Geteuid(), AccountHome: home,
		CodexHome: filepath.Join(home, ".codex"),
		Machine:   "linux-machine-id:0123456789abcdef0123456789abcdef"}
	paths, err := taskhook.ContextPaths(target)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(paths.State, 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(target.CodexHome, "sessions"), 0700))
	target.Context, err = taskhook.ContextID(target)
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{"target": target, "helper_protocol": 1})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(paths.Context, "install.json"), metadata, 0600))
	executable, err := os.Executable()
	require.NoError(t, err)
	args := []string{managedMode, rootFlag, paths.State, "--account-home", home, "--codex-home", target.CodexHome,
		"--machine-identity", target.Machine, "--uid", strconv.Itoa(os.Geteuid()), "--executable", executable}
	var out, diagnostic bytes.Buffer
	require.Zero(t, cli(args, strings.NewReader(""), &out, &diagnostic))
	require.Empty(t, diagnostic.String())
	owner, err := taskhook.NewRegistry(paths.State).Read(out.String())
	require.NoError(t, err)
	require.Equal(t, os.Getppid(), owner.Owner.PID)
	require.False(t, owner.Scope.Supported())
	require.Equal(t, taskhook.UnknownScopeVersion, owner.Scope.CodexVersion)
}

func TestCLIDeadlineAllowsTheDocumentedProbeBudget(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	var out, diagnostic bytes.Buffer
	began := time.Now()
	require.Zero(t, cli([]string{hookMode, rootFlag, t.TempDir()}, reader, &out, &diagnostic))
	elapsed := time.Since(began)
	require.GreaterOrEqual(t, elapsed, 450*time.Millisecond)
	require.Less(t, elapsed, 2*time.Second)
	require.Empty(t, out.String())
	require.Empty(t, diagnostic.String())
}
