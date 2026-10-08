package testcap

import (
	"errors"
	"fmt"
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
		name        string
		ci          bool
		err         error
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

func TestProbeTmuxAtReportsAServerThatCannotStart(t *testing.T) {
	err := probeTmuxAt(filepath.Join(t.TempDir(), "no-such-tmux"), filepath.Join(t.TempDir(), "s"))
	if err == nil || !strings.Contains(err.Error(), "new-session") {
		t.Fatalf("probeTmuxAt with a missing binary = %v, want a new-session error", err)
	}
}

// The probes answer for the machine the tests run on, so all that holds
// everywhere is that each returns, and that the Require helpers act on the
// answer: they must not fail outside CI whatever the answer is.
func TestRequireHelpersNeverFailOutsideCI(t *testing.T) {
	withCI(t, false)
	for name, req := range map[string]func(TB){"pty": RequirePTY, "tmux": RequireTmux, "ps": RequirePS, "dscl": RequireDscl} {
		var r recorder
		req(&r)
		if r.fatal != "" {
			t.Errorf("Require %s outside CI failed: %s", name, r.fatal)
		}
	}
}
