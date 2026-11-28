package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolateEmptyProcesses puts no-op ps, lsof, and tmux shims first on PATH so
// tests that only exercise file collection do not scan unrelated agent
// processes running on the developer's machine (issue #408).
func isolateEmptyProcesses(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"ps", "lsof", "tmux"} {
		writeExecutable(t, filepath.Join(bin, name), "#!/bin/sh\nexit 0\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// isolateHostProcessesToPIDs wraps the real ps binary so listing invocations
// (`ps -U ...`) report only pids while per-PID queries (`ps -p ...`, `ps -E`)
// pass straight through. Tests still exercise real ps, real lsof, and /proc
// without paying the cost of scanning unrelated live agents on the host
// (issue #408).
func isolateHostProcessesToPIDs(t *testing.T, pids ...int) {
	t.Helper()
	realPS, err := exec.LookPath("ps")
	require.NoError(t, err)

	var keep strings.Builder
	keep.WriteByte(' ')
	for _, pid := range pids {
		keep.WriteString(strconv.Itoa(pid))
		keep.WriteByte(' ')
	}
	quotedPS := "'" + strings.ReplaceAll(realPS, "'", `'\''`) + "'"
	wrapper := "#!/bin/sh\ncase \"$1\" in\n-U)\n\t" + quotedPS +
		" \"$@\" | awk -v keep='" + keep.String() + "' 'index(keep, \" \" $1 \" \") > 0'\n\t;;\n*)\n\texec " +
		quotedPS + " \"$@\"\n\t;;\nesac\n"

	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "ps"), wrapper)
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
