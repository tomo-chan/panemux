#!/bin/sh
#
# Tests for scripts/tla_transitions.py and scripts/model_check.sh's preflight.
#
# The exporter is the one artifact in issue #168's two-tier split that nothing
# else checks, and it is load-bearing: the table it writes IS the reference
# model every Tier 1 assertion is compared against. An exporter that silently
# writes a smaller table than the .cfg asked for makes the hermetic gate shrink
# to match, which looks green. So every rejection arm is asserted here, not just
# the happy path.
#
# Hermetic by construction: it drives the exporter against committed dot
# fixtures in scripts/testdata/model-check/, so it needs no JDK and no
# tla2tools.jar — the same reason make test-mutation drives its checker through
# fixture reports rather than installing gremlins.
#
# python3 is optional the way jq is for make test-hooks: without it the
# exporter checks report themselves as SKIPPED rather than passing or failing,
# so make check stays green on a host that has no Python.
#
# Run with: make test-model-check

set -u

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
exporter="$scripts_dir/tla_transitions.py"
driver="$scripts_dir/model_check.sh"
fixtures="$scripts_dir/testdata/model-check"

failures=0
skipped=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }
skip() {
	skipped=$((skipped + 1))
	echo "skip $1 ($2)"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

have_python=1
command -v python3 >/dev/null 2>&1 || have_python=0

# export <want-exit> <name> <dot> <cfg>
export_case() {
	want=$1
	name=$2
	dot=$3
	cfg=$4
	checks=$((checks + 1))
	if [ "$have_python" -eq 0 ]; then
		skip "$name" "python3 not installed"
		return
	fi

	out="$work/out.json"
	rm -f "$out"
	output=$(python3 "$exporter" --dot "$dot" --cfg-path "$cfg" \
		--spec "spec/agentboard/OwnSendLedger.tla" \
		--config "spec/agentboard/OwnSendLedger.cfg" --out "$out" 2>&1)
	got=$?
	if [ "$got" -eq "$want" ]; then
		pass "$name"
	else
		fail "$name: wanted exit $want, got $got" "$output"
	fi
}

echo "== the exporter accepts a real TLC dump =="

export_case 0 "a complete dump at the bound its .cfg declares is exported" \
	"$fixtures/complete-max2.dot" "$fixtures/Max2.cfg"

# The projection is the exporter's whole job, so assert its content rather than
# only its exit code. These 19 edges are the full MaxEntries = 2 lattice.
checks=$((checks + 1))
if [ "$have_python" -eq 0 ]; then
	skip "the exported table holds every transition of the bounded lattice" "python3 not installed"
else
	python3 "$exporter" --dot "$fixtures/complete-max2.dot" --cfg-path "$fixtures/Max2.cfg" \
		--spec "spec/agentboard/OwnSendLedger.tla" --config "spec/agentboard/OwnSendLedger.cfg" \
		--out "$work/complete.json" >/dev/null 2>&1
	got_states=$(grep -c '^    {"expired"' "$work/complete.json" 2>/dev/null || echo 0)
	got_edges=$(grep -c '"action"' "$work/complete.json" 2>/dev/null || echo 0)
	got_bound=$(grep -o '"maxHeld": [0-9]*' "$work/complete.json" 2>/dev/null || echo none)
	if [ "$got_states" = "6" ] && [ "$got_edges" = "19" ] && [ "$got_bound" = '"maxHeld": 2' ]; then
		pass "the exported table holds every transition of the bounded lattice"
	else
		fail "the exported table holds every transition of the bounded lattice" \
			"got $got_states states, $got_edges transitions, $got_bound"
	fi
fi

# A table a reviewer reads in a diff has to be byte-stable, or every
# regeneration churns lines nobody changed.
checks=$((checks + 1))
if [ "$have_python" -eq 0 ]; then
	skip "the same dump exports byte-identically twice" "python3 not installed"
else
	for pass_n in 1 2; do
		python3 "$exporter" --dot "$fixtures/complete-max2.dot" --cfg-path "$fixtures/Max2.cfg" \
			--spec "spec/agentboard/OwnSendLedger.tla" --config "spec/agentboard/OwnSendLedger.cfg" \
			--out "$work/stable-$pass_n.json" >/dev/null 2>&1
	done
	if cmp -s "$work/stable-1.json" "$work/stable-2.json"; then
		pass "the same dump exports byte-identically twice"
	else
		fail "the same dump exports byte-identically twice" "$(diff -u "$work/stable-1.json" "$work/stable-2.json")"
	fi
fi

echo
echo "== the exporter refuses a dump it cannot trust =="

# The one this file exists for. A dump explored only to held <= 1, paired with a
# .cfg asking for 2: the exporter must not quietly emit "maxHeld": 1 and let
# Tier 1 shrink its own driver to match.
export_case 1 "a dump that stopped below the .cfg's MaxEntries is rejected" \
	"$fixtures/truncated-max2.dot" "$fixtures/Max2.cfg"

export_case 1 "a dump with a hole in the reachable lattice is rejected" \
	"$fixtures/lattice-gap-max2.dot" "$fixtures/Max2.cfg"

export_case 1 "an edge label disagreeing with its destination's action is rejected" \
	"$fixtures/action-label-mismatch.dot" "$fixtures/Max2.cfg"

export_case 1 "a node missing an observed variable is rejected" \
	"$fixtures/missing-variable.dot" "$fixtures/Max2.cfg"

export_case 1 "an edge into an undumped state is rejected" \
	"$fixtures/dangling-edge.dot" "$fixtures/Max2.cfg"

export_case 1 "an empty dump is rejected" \
	"$fixtures/empty.dot" "$fixtures/Max2.cfg"

export_case 1 "a .cfg declaring no MaxEntries is rejected rather than guessed at" \
	"$fixtures/complete-max2.dot" "$fixtures/no-constants.cfg"

export_case 1 "a missing .cfg is rejected" \
	"$fixtures/complete-max2.dot" "$work/does-not-exist.cfg"

# A rejected dump must leave nothing behind: a half-written table is worse than
# no table, because Tier 1 would read it.
checks=$((checks + 1))
if [ "$have_python" -eq 0 ]; then
	skip "a rejected dump writes no table" "python3 not installed"
else
	rm -f "$work/rejected.json"
	python3 "$exporter" --dot "$fixtures/truncated-max2.dot" --cfg-path "$fixtures/Max2.cfg" \
		--spec s --config c --out "$work/rejected.json" >/dev/null 2>&1
	if [ -e "$work/rejected.json" ]; then
		fail "a rejected dump writes no table" "$work/rejected.json exists"
	else
		pass "a rejected dump writes no table"
	fi
fi

echo
echo "== the driver preflights its toolchain before doing any work =="

# Each of these must fail before TLC would ever be started, so they are
# runnable with no JDK and no jar present.
checks=$((checks + 1))
output=$(TLA_TOOLS_JAR= "$driver" 2>&1)
got=$?
if [ "$got" -eq 2 ] && printf '%s' "$output" | grep -q "TLA_TOOLS_JAR"; then
	pass "an unset TLA_TOOLS_JAR is reported with the command that fetches it"
else
	fail "an unset TLA_TOOLS_JAR is reported with the command that fetches it" "exit $got: $output"
fi

checks=$((checks + 1))
output=$(TLA_TOOLS_JAR="$work/no-such.jar" "$driver" 2>&1)
got=$?
if [ "$got" -eq 2 ]; then
	pass "a TLA_TOOLS_JAR pointing at nothing is reported"
else
	fail "a TLA_TOOLS_JAR pointing at nothing is reported" "exit $got: $output"
fi

checks=$((checks + 1))
: > "$work/fake.jar"
output=$(TLA_TOOLS_JAR="$work/fake.jar" "$driver" --not-an-argument 2>&1)
got=$?
if [ "$got" -eq 2 ] && printf '%s' "$output" | grep -q "unknown argument"; then
	pass "an unknown argument is rejected rather than ignored"
else
	fail "an unknown argument is rejected rather than ignored" "exit $got: $output"
fi

# java and python3 are equally hard dependencies, so each gets the same early,
# named check. Without the python3 one the operator would pay a full TLC run
# before hitting a bare "command not found" with nothing saying an interpreter
# was what was missing.
#
# Simulated with a PATH holding only the utilities the script needs to reach
# its preflight, so exactly one of the two arms can fire per case. The driver
# is invoked directly rather than through `sh`, so the stubbed PATH cannot
# accidentally hide the interpreter itself.

# stub_path_without <tool> — echoes a PATH with every utility the driver needs
# except the named one; java is a stub that never runs TLC.
stub_path_without() {
	missing=$1
	dir="$work/stub-$missing"
	mkdir -p "$dir"
	for tool in dirname basename mktemp rm tr diff cp cat grep sed python3; do
		[ "$tool" = "$missing" ] && continue
		real=$(command -v "$tool" 2>/dev/null) || continue
		ln -sf "$real" "$dir/$tool"
	done
	if [ "$missing" != "java" ]; then
		printf '#!/bin/sh\nexit 0\n' > "$dir/java"
		chmod +x "$dir/java"
	fi
	echo "$dir"
}

checks=$((checks + 1))
output=$(PATH="$(stub_path_without python3)" TLA_TOOLS_JAR="$work/fake.jar" "$driver" 2>&1)
got=$?
if [ "$got" -eq 2 ] && printf '%s' "$output" | grep -q "python3"; then
	pass "a missing python3 is named before any TLC run starts"
else
	fail "a missing python3 is named before any TLC run starts" "exit $got: $output"
fi

checks=$((checks + 1))
output=$(PATH="$(stub_path_without java)" TLA_TOOLS_JAR="$work/fake.jar" "$driver" 2>&1)
got=$?
if [ "$got" -eq 2 ] && printf '%s' "$output" | grep -q "java"; then
	pass "a missing java is named before any TLC run starts"
else
	fail "a missing java is named before any TLC run starts" "exit $got: $output"
fi

echo
if [ "$failures" -eq 0 ]; then
	if [ "$skipped" -gt 0 ]; then
		echo "all $checks model-check checks passed or skipped ($skipped skipped: python3 not installed)"
	else
		echo "all $checks model-check checks passed"
	fi
	exit 0
fi
echo "$failures of $checks model-check checks failed"
exit 1
