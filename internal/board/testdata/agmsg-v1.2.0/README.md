# agmsg v1.2.0 fixtures — captured from a real install

Tier 1 of the [agmsg compatibility
contract](../../../../docs/agent-board.md#agmsg-compatibility-contract):
frozen output that panemux's own parsing is asserted against, hermetically,
on every `make check`.

These files are **captured**, not written by hand. `capture.sh` regenerates
them end to end — it installs agmsg at the tag this directory is named for
into a throwaway `HOME`, joins a team, sends the four messages Tier 1
asserts on, and records what `scripts/api.sh` actually printed:

```sh
git clone https://github.com/fujibee/agmsg.git /tmp/agmsg-src
internal/board/testdata/agmsg-v1.2.0/capture.sh /tmp/agmsg-src
```

`sqlite3` must be installed — agmsg stores messages in SQLite and `api.sh`
cannot run without it.

Every value in these files is a placeholder chosen by `capture.sh`
(`/tmp/sample-project`, team `panemux`, `pane-a`/`pane-b`), so no real
working directory is recorded here — see DEVELOPMENT.md's path-sanitization
rule.

## What replacing the hand-written fixtures changed

An earlier `agmsg-unpinned-handwritten/` directory held fixtures written
from prose in `docs/agent-board.md`, in a session that had neither `sqlite3`
nor network access to agmsg. Its own README asked for exactly this
replacement.

It mattered more than provenance hygiene. The hand-written rows carried
`"id":"1"` … `"id":"4"`, because that is what agmsg's legacy sqlite driver
exposes. Real 1.2.0 emits **UUIDv7** ids from its event-log driver:

```
01a02760-c340-7ec7-8f18-071cce739579
```

panemux compared those ids numerically to decide which rows a poll had
already seen. Against real output no id parsed, so no cursor ever advanced
and the relay re-delivered its entire poll window on every tick — while
every Tier 1 test stayed green, because the fixtures were integers. The
capture is what surfaced it.

Note also that all four rows here were written in the same millisecond and
so share the `01a02760-c340` prefix, differing only in random bits: their
lexicographic order is **not** the order `api.sh` returned them in. That is
why panemux now treats the id as an opaque token and anchors its cursor on
the response's own order — see `filterRowsAfter` in `../../agmsg_parse.go`.

## Files

| File | Command |
|---|---|
| `get_teams.jsonl` | `api.sh get teams` |
| `get_team_members.jsonl` | `api.sh get teams panemux members` |
| `get_team_messages.jsonl` | `api.sh get teams panemux messages --limit 100` |
| `VERSION.txt` | `scripts/version.sh` — the provenance of this capture |

Tier 1 cannot detect that agmsg changed; it only pins panemux against a
fixed shape. That is [Tier
2](../../../../docs/agent-board.md#agmsg-compatibility-contract)'s job —
`make test-agmsg-contract`, run in CI by
`.github/workflows/agmsg-contract.yml`.
