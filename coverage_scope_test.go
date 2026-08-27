package main

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The coverage gate's scope is a hand-written list in the Makefile, which
// makes it exactly the kind of duplicated knowledge principle P1 warns about
// (docs/quality-gateway.md): a package added to this repository later is
// silently outside the gate, and — worse than the failure this scope widening
// was closing — the reported percentage goes UP, because the new, untested
// code is invisible to the measurement rather than dragging it down.
//
// So the list is checked against the source of truth instead of trusted. This
// is the same move internal/server/route_table_test.go makes for the HTTP
// route table, deriving it from chi.Walk rather than restating it.
//
// A package that genuinely should not be gated is excluded BY NAME below, with
// its reason. That is the point: skipping a package stays possible, but only
// as a visible edit to this file, never by forgetting the Makefile.

// ungatedPackages are deliberately outside the coverage gate. Keep the reasons
// here in step with the comment block above COVERAGE_PKGS in the Makefile.
var ungatedPackages = map[string]string{
	// Its lifecycle methods drive a real PTY (local, tmux), a live SSH
	// connection (ssh, ssh_tmux) and a real tmux server. `make check` is
	// hermetic — design principle 5 — so these are covered by the package's
	// own tests and by E2E, not gated here. The pure decisions extracted out
	// of them (validateShell, validRemotePath, classifySSHWaitError) are
	// unit-tested in place.
	"panemux/internal/session": "needs a real PTY, SSH connection and tmux server",

	// A parser over the user's own ~/.ssh/config, with its own tests and no
	// gate-worthy branching the measured packages do not already drive.
	"panemux/internal/sshconfig": "a self-contained parser with its own tests",
}

func TestCoverageScopeCoversEveryPackage(t *testing.T) {
	covered := coveragePkgsFromMakefile(t)
	all := modulePackages(t)

	var want []string
	for _, pkg := range all {
		if _, skip := ungatedPackages[pkg]; skip {
			continue
		}
		want = append(want, pkg)
	}
	sort.Strings(want)

	got := make([]string, 0, len(covered))
	for _, pattern := range covered {
		got = append(got, packagesMatching(t, pattern, all)...)
	}
	sort.Strings(got)

	assert.Equal(t, want, got,
		"COVERAGE_PKGS in the Makefile no longer matches this module's packages.\n"+
			"Add the new package to COVERAGE_PKGS (and to the go test argument list in the\n"+
			"coverage-go recipe), or, if it genuinely should not be gated, add it to\n"+
			"ungatedPackages above with the reason.")
}

// The Makefile states the package set twice — once as COVERAGE_PKGS for
// -coverpkg, and once as the go test argument list that decides which packages
// are actually RUN. They mean different things and both matter: a package in
// the first but not the second is measured but never exercised, and reports 0%.
func TestCoverageRecipeRunsEveryGatedPackage(t *testing.T) {
	assert.Equal(t, coveragePkgsFromMakefile(t), coverageRecipeArgsFromMakefile(t),
		"the coverage-go recipe's package arguments must match COVERAGE_PKGS; "+
			"a package measured but not run reports 0% and drags the gate down for no reason")
}

func makefile(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("Makefile")
	require.NoError(t, err)
	return string(raw)
}

// coveragePkgsFromMakefile reads the COVERAGE_PKGS assignment.
func coveragePkgsFromMakefile(t *testing.T) []string {
	t.Helper()

	for _, line := range strings.Split(makefile(t), "\n") {
		if !strings.HasPrefix(line, "COVERAGE_PKGS") {
			continue
		}
		_, value, ok := strings.Cut(line, ":=")
		require.True(t, ok, "COVERAGE_PKGS must be a := assignment")

		var out []string
		for _, p := range strings.Split(strings.TrimSpace(value), ",") {
			out = append(out, strings.TrimSpace(p))
		}
		sort.Strings(out)
		return out
	}

	t.Fatal("no COVERAGE_PKGS assignment in the Makefile — has the variable been renamed?")
	return nil
}

// coverageRecipeArgsFromMakefile reads the package arguments of the go test
// invocation in the coverage-go recipe.
func coverageRecipeArgsFromMakefile(t *testing.T) []string {
	t.Helper()

	var (
		out      []string
		inRecipe bool
	)
	for _, line := range strings.Split(makefile(t), "\n") {
		if strings.HasPrefix(line, "coverage-go:") {
			inRecipe = true
			continue
		}
		if !inRecipe {
			continue
		}
		field := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
		if field == "." || strings.HasPrefix(field, "./") {
			out = append(out, field)
			continue
		}
		// The package arguments are the leading run of the invocation; the
		// first flag ends them.
		if strings.HasPrefix(field, "-") {
			break
		}
	}

	require.NotEmpty(t, out, "no package arguments found in the coverage-go recipe")
	sort.Strings(out)
	return out
}

// modulePackages is every package in this module, from go list — the source of
// truth the Makefile's list is checked against.
func modulePackages(t *testing.T) []string {
	t.Helper()

	out, err := exec.Command("go", "list", "./...").Output()
	require.NoError(t, err, "go list ./... must succeed for this gate to mean anything")

	var pkgs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			pkgs = append(pkgs, line)
		}
	}
	require.NotEmpty(t, pkgs)
	return pkgs
}

// packagesMatching expands one COVERAGE_PKGS pattern ("." or "./x/...")
// against the module's real package list.
func packagesMatching(t *testing.T, pattern string, all []string) []string {
	t.Helper()

	const module = "panemux"

	if pattern == "." {
		return []string{module}
	}

	require.True(t, strings.HasPrefix(pattern, "./"),
		"unexpected COVERAGE_PKGS entry %q: expected \".\" or a \"./…\" pattern", pattern)
	prefix := module + "/" + strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/...")

	var out []string
	for _, pkg := range all {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			out = append(out, pkg)
		}
	}
	require.NotEmpty(t, out, "COVERAGE_PKGS names %q, which matches no package in this module", pattern)
	return out
}
