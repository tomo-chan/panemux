package testcap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// recorder is a TB that records what require asked of it.
type recorder struct {
	skipped, fatal string
}

func (*recorder) Helper()                             {}
func (r *recorder) Skipf(format string, args ...any)  { r.skipped = fmt.Sprintf(format, args...) }
func (r *recorder) Fatalf(format string, args ...any) { r.fatal = fmt.Sprintf(format, args...) }

func withCI(t *testing.T, ci bool) {
	t.Helper()
	original := inCI
	inCI = func() bool { return ci }
	t.Cleanup(func() { inCI = original })
}

func TestRequire(t *testing.T) {
	denied := errors.New("operation not permitted")
	tests := []struct {
		err         error
		name        string
		ci          bool
		wantSkipped bool
		wantFatal   bool
	}{
		{name: "available, outside CI", ci: false, err: nil},
		{name: "available, in CI", ci: true, err: nil},
		{name: "missing, outside CI, skips", ci: false, err: denied, wantSkipped: true},
		{name: "missing, in CI, fails", ci: true, err: denied, wantFatal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCI(t, tt.ci)
			var r recorder
			require(&r, "a pseudo-terminal", tt.err)
			if (r.skipped != "") != tt.wantSkipped {
				t.Errorf("skipped = %q, want skipped: %v", r.skipped, tt.wantSkipped)
			}
			if (r.fatal != "") != tt.wantFatal {
				t.Errorf("fatal = %q, want fatal: %v", r.fatal, tt.wantFatal)
			}
			for _, msg := range []string{r.skipped, r.fatal} {
				if msg != "" && (!strings.Contains(msg, "a pseudo-terminal") || !strings.Contains(msg, "operation not permitted")) {
					t.Errorf("message %q names neither the capability nor the cause", msg)
				}
			}
		})
	}
}

func TestInCIReadsTheCIVariable(t *testing.T) {
	t.Setenv("CI", "")
	if inCI() {
		t.Error("inCI() with CI empty = true")
	}
	t.Setenv("CI", "true")
	if !inCI() {
		t.Error("inCI() with CI=true = false")
	}
}

func TestProbeTmuxInReportsAServerThatCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := probeTmuxIn(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "new-session") {
		t.Fatalf("probeTmuxIn with no tmux on PATH = %v, want a new-session error", err)
	}
}

// The probes answer for the machine the tests run on, so all that holds
// everywhere is that each returns, and that the Require helpers act on the
// answer: they must not fail outside CI whatever the answer is.
func TestRequireHelpersNeverFailOutsideCI(t *testing.T) {
	withCI(t, false)
	requirements := map[string]func(TB){
		"pty": RequirePTY, "tmux": RequireTmux, "ps": RequirePS, "dscl": RequireDscl,
	}
	for name, req := range requirements {
		var r recorder
		req(&r)
		if r.fatal != "" {
			t.Errorf("Require %s outside CI failed: %s", name, r.fatal)
		}
	}
}

// fakeOutput makes output answer by the command's verb: an error for each verb
// in failing, success for the rest. It records every command it saw.
func fakeOutput(t *testing.T, failing ...string) *[]string {
	t.Helper()
	var seen []string
	original := output
	output = func(cmd *exec.Cmd) ([]byte, error) {
		line := strings.Join(cmd.Args, " ")
		seen = append(seen, line)
		for _, verb := range failing {
			if strings.Contains(line, verb) {
				return []byte("denied"), errors.New("exit status 1")
			}
		}
		return nil, nil
	}
	t.Cleanup(func() { output = original })
	return &seen
}

func TestProbePTY(t *testing.T) {
	original := openPTY
	t.Cleanup(func() { openPTY = original })
	pipe := func(t *testing.T) (*os.File, *os.File) {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		return r, w
	}

	openPTY = func() (*os.File, *os.File, error) { return nil, nil, errors.New("operation not permitted") }
	if err := probePTY(); err == nil || !strings.Contains(err.Error(), "opening a pty") {
		t.Errorf("probePTY with open failing = %v", err)
	}

	openPTY = func() (*os.File, *os.File, error) { r, w := pipe(t); return r, w, nil }
	if err := probePTY(); err != nil {
		t.Errorf("probePTY with a pty = %v", err)
	}

	openPTY = func() (*os.File, *os.File, error) {
		r, w := pipe(t)
		_ = r.Close()
		return r, w, nil
	}
	if err := probePTY(); err == nil || !strings.Contains(err.Error(), "closing the pty") {
		t.Errorf("probePTY with close failing = %v", err)
	}
}

func TestProbeTmux(t *testing.T) {
	bin := t.TempDir()
	// The stand-in tmux must be executable for LookPath to find it.
	stub := filepath.Join(bin, "tmux")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // see above
		t.Fatal(err)
	}

	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := probeTmux(); err == nil || !strings.Contains(err.Error(), "finding tmux") {
			t.Errorf("probeTmux = %v", err)
		}
	})
	t.Run("no temporary directory", func(t *testing.T) {
		t.Setenv("PATH", bin)
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		if err := probeTmux(); err == nil || !strings.Contains(err.Error(), "socket directory") {
			t.Errorf("probeTmux = %v", err)
		}
	})
	t.Run("server starts and answers", func(t *testing.T) {
		t.Setenv("PATH", bin)
		seen := fakeOutput(t)
		if err := probeTmux(); err != nil {
			t.Errorf("probeTmux = %v", err)
		}
		if got := strings.Join(*seen, "\n"); !strings.Contains(got, "has-session") || !strings.Contains(got, "kill-server") {
			t.Errorf("commands = %q, want has-session and the kill-server cleanup", got)
		}
		for _, line := range *seen {
			if !strings.HasPrefix(line, "tmux -S s ") {
				t.Errorf("%q does not name the probe's own socket", line)
			}
		}
	})
	t.Run("server does not answer", func(t *testing.T) {
		fakeOutput(t, "has-session")
		if err := probeTmuxIn(t.TempDir()); err == nil || !strings.Contains(err.Error(), "has-session") {
			t.Errorf("probeTmuxIn = %v", err)
		}
	})
}

func TestProbePS(t *testing.T) {
	fakeOutput(t)
	if err := probePS(); err != nil {
		t.Errorf("probePS with ps working = %v", err)
	}
	fakeOutput(t, "ps")
	if err := probePS(); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("probePS with ps denied = %v", err)
	}
}

func TestProbeDscl(t *testing.T) {
	original := goos
	t.Cleanup(func() { goos = original })

	goos = "linux"
	seen := fakeOutput(t, "dscl")
	if err := probeDscl(); err != nil || len(*seen) != 0 {
		t.Errorf("probeDscl on linux = %v after %q; want nil without running dscl", err, *seen)
	}

	goos = "darwin"
	if err := probeDscl(); err == nil || !strings.Contains(err.Error(), "dscl") {
		t.Errorf("probeDscl with dscl denied = %v", err)
	}
	fakeOutput(t)
	if err := probeDscl(); err != nil {
		t.Errorf("probeDscl with dscl working = %v", err)
	}
}
