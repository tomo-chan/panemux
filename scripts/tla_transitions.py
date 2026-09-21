#!/usr/bin/env python3
"""Export a TLC state graph as the flat transition table Tier 1 replays against.

Reads the `-dump dot,actionlabels` graph TLC writes, projects every node onto
the variables the Go implementation can actually observe, and emits the
deduplicated (from, action, result, to) edges as JSON.

Projection is sound only because the variables dropped here are history
counters that never gate a transition -- see the header comment of
spec/agentboard/OwnSendLedger.tla. If a spec ever adds a variable that DOES
gate one, it has to be added to OBSERVED below or the exported table will
claim transitions the implementation cannot take.

The bound is read from the .cfg, never inferred from the dump. That is not a
detail: Tier 1 reads `maxHeld` back out of the generated file and limits its
own drivers to it, so an exporter that defined the bound as "whatever states I
was handed" would let an under-explored TLC run silently shrink the hermetic
gate to match -- and every completeness check written against that inferred
bound would pass, because its expectation came from the data it was validating.
"""

import argparse
import json
import re
import sys
from collections import defaultdict

# Variables a Go-side trace can observe. Everything else in the spec is
# history bookkeeping and is projected away.
OBSERVED = ("expired", "live")

NODE_RE = re.compile(r'^(-?\d+)\s*\[label="((?:[^"\\]|\\.)*)"')
EDGE_RE = re.compile(r'^(-?\d+)\s*->\s*(-?\d+)\s*\[label="((?:[^"\\]|\\.)*)"')
ASSIGN_RE = re.compile(r'^/\\\s*(\w+)\s*=\s*(.+)$')
# `MaxEntries = 4` in a .cfg's CONSTANTS block, however it is indented.
CONSTANT_RE = re.compile(r'^\s*MaxEntries\s*=\s*(\d+)\s*$', re.M)


def unescape(label):
    out = []
    i = 0
    while i < len(label):
        ch = label[i]
        if ch == "\\" and i + 1 < len(label):
            nxt = label[i + 1]
            out.append({"n": "\n", '"': '"', "\\": "\\"}.get(nxt, nxt))
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def parse_state(label):
    state = {}
    for line in unescape(label).split("\n"):
        m = ASSIGN_RE.match(line.strip())
        if not m:
            continue
        name, raw = m.group(1), m.group(2).strip()
        if raw.startswith('"') and raw.endswith('"'):
            state[name] = raw[1:-1]
        else:
            state[name] = int(raw)
    return state


def parse_max_entries(path):
    """Read the occurrence bound the .cfg asked TLC for.

    Deliberately strict: a .cfg with no MaxEntries is an error rather than a
    default, because every fallback available here would be a guess, and the
    one guess that looks most reasonable -- infer it from the dump -- is
    exactly the bug this function exists to remove.
    """
    try:
        with open(path, encoding="utf-8") as fh:
            text = fh.read()
    except OSError as err:
        sys.exit(f"{path}: cannot read the .cfg the bound comes from: {err}")
    matches = CONSTANT_RE.findall(text)
    if not matches:
        sys.exit(f"{path}: declares no MaxEntries constant; the exported table's "
                 f"bound is read from the .cfg, never inferred from the dump")
    if len(set(matches)) > 1:
        sys.exit(f"{path}: declares MaxEntries more than once, as {sorted(set(matches))}")
    return int(matches[0])


def parse_dot(path):
    nodes, edges = {}, []
    with open(path, encoding="utf-8") as fh:
        for raw in fh:
            line = raw.strip()
            m = EDGE_RE.match(line)
            if m:
                edges.append((m.group(1), m.group(2), unescape(m.group(3))))
                continue
            m = NODE_RE.match(line)
            if m:
                nodes[m.group(1)] = parse_state(m.group(2))
    return nodes, edges


def observed(state):
    return tuple(state[name] for name in OBSERVED)


def as_obj(values):
    return dict(zip(OBSERVED, values))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dot", required=True, help="TLC's -dump dot,actionlabels output")
    ap.add_argument("--cfg-path", required=True, help="the .cfg file to read MaxEntries from")
    ap.add_argument("--spec", required=True, help="repo-relative .tla path, recorded in the table")
    ap.add_argument("--config", required=True, help="repo-relative .cfg path, recorded in the table")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    max_entries = parse_max_entries(args.cfg_path)
    nodes, edges = parse_dot(args.dot)
    if not nodes:
        sys.exit(f"{args.dot}: no states found -- did TLC write the dump?")
    for node_id, state in nodes.items():
        missing = [name for name in OBSERVED if name not in state]
        if missing:
            sys.exit(f"{args.dot}: node {node_id} has no {missing}; spec and "
                     f"{__file__}'s OBSERVED have drifted")

    transitions = set()
    for src, dst, action in edges:
        if src not in nodes or dst not in nodes:
            sys.exit(f"{args.dot}: edge {src} -> {dst} references an undumped state")
        after = nodes[dst]
        # The action label and the destination state's own `action` variable
        # are two independent records of the same step. Requiring them to
        # agree is what keeps the spec's self-describing `action` variable
        # honest -- if they ever disagree the export is not trustworthy.
        if after.get("action") != action:
            sys.exit(f"{args.dot}: edge labelled {action!r} lands in a state "
                     f"whose action variable says {after.get('action')!r}")
        transitions.add((observed(nodes[src]), action,
                         after.get("result", "-"), observed(after)))

    states = sorted({observed(s) for s in nodes.values()})
    observed_max = max(sum(s) for s in states)

    # Export-coverage checks. These do not re-state the model; they assert the
    # bounds in the .cfg were generous enough that the table is COMPLETE. An
    # under-explored table is the dangerous failure: Tier 1 would report a
    # missing edge as a Go bug -- or, worse, read the smaller bound back out of
    # this file and stop looking for the states above it at all.
    #
    # Both checks measure against MaxEntries from the .cfg. Measuring against
    # the dump's own maximum, as an earlier revision did, makes them
    # unfalsifiable: the expectation is then derived from the data it validates.
    if observed_max != max_entries:
        sys.exit(f"{args.dot}: the reachable graph holds at most {observed_max} "
                 f"occurrences, but {args.cfg_path} declares MaxEntries = "
                 f"{max_entries}; raise MaxRecords in that .cfg until states at the "
                 f"bound are reachable, or lower MaxEntries to match")
    expected = {(e, l) for e in range(max_entries + 1)
                for l in range(max_entries + 1) if e + l <= max_entries}
    if set(states) != expected:
        missing = [as_obj(s) for s in sorted(expected - set(states))]
        sys.exit(f"{args.dot}: only {len(states)} of {len(expected)} states with "
                 f"expired+live <= {max_entries} are reachable; missing {missing}; "
                 f"raise MaxRecords in {args.cfg_path}")
    outgoing = defaultdict(set)
    for src, action, _, _ in transitions:
        outgoing[src].add(action)
    for state in states:
        if not outgoing[state]:
            sys.exit(f"{args.dot}: state {as_obj(state)} has no outgoing transition")

    rows = sorted(transitions)
    payload = {
        "_generatedBy": "scripts/model_check.sh (do not hand-edit)",
        "spec": args.spec,
        "config": args.config,
        "observedVariables": list(OBSERVED),
        "maxHeld": max_entries,
        "actions": sorted({a for _, a, _, _ in transitions}),
        "states": [as_obj(s) for s in states],
        "transitions": [
            {"from": as_obj(src), "action": action, "result": result, "to": as_obj(dst)}
            for src, action, result, dst in rows
        ],
    }

    # One JSON object per line: this file lands in a pull request diff, and a
    # changed edge should read as one changed line.
    with open(args.out, "w", encoding="utf-8") as fh:
        fh.write(compact(payload))
        fh.write("\n")


def compact(payload):
    """Render the payload with one JSON object per line where it aids review."""
    lines = ["{"]
    scalar_keys = ["_generatedBy", "spec", "config", "observedVariables",
                   "maxHeld", "actions"]
    for key in scalar_keys:
        lines.append(f'  {json.dumps(key)}: {json.dumps(payload[key])},')
    lines.append('  "states": [')
    for i, state in enumerate(payload["states"]):
        sep = "," if i + 1 < len(payload["states"]) else ""
        lines.append(f"    {json.dumps(state)}{sep}")
    lines.append("  ],")
    lines.append('  "transitions": [')
    for i, row in enumerate(payload["transitions"]):
        sep = "," if i + 1 < len(payload["transitions"]) else ""
        lines.append(f"    {json.dumps(row)}{sep}")
    lines.append("  ]")
    lines.append("}")
    return "\n".join(lines)


if __name__ == "__main__":
    main()
