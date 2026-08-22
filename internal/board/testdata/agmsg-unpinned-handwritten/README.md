# Hand-written agmsg fixtures — still awaiting real capture

The fixtures in this directory are **not** captured from a real agmsg run.
The directory name says so deliberately: renaming it to a version tag would
claim a provenance these files do not have.

## What has changed since they were written

They were originally written blind — that session had neither `sqlite3` nor
network access to `github.com/fujibee/agmsg`, so the JSONL shape came from
prose in [docs/agent-board.md](../../../../docs/agent-board.md) rather than
from agmsg itself.

Network access to agmsg's source is now available, and the script interface
panemux depends on **was** read directly from agmsg 1.2.0 — `delivery.sh`,
`scripts/lib/type-registry.sh`, and the per-type
`scripts/drivers/types/<type>/type.conf` manifests. That reading is what
`board.TestedAgmsgVersion` now pins, and it corrected two things this
repository had wrong (the hook file is per-agent-type, not always
`.claude/settings.local.json`; and agmsg derives its delivery mode from that
file's contents, so no file means no delivery at all).

## What still blocks a real capture

`sqlite3` — one of agmsg's own runtime dependencies — is not installed and
could not be installed here (the distribution archive returned 404). agmsg
stores messages in SQLite, so `api.sh` cannot be run at all without it, and
no output can be captured.

## What these fixtures are and are not

They satisfy Tier 1 of the [agmsg compatibility
contract](../../../../docs/agent-board.md#agmsg-compatibility-contract):
fast, hermetic parsing tests over a fixed JSONL shape. They cannot detect
that agmsg changed, because they never touch a real install — that is Tier
2's job, and Tier 2 does not exist yet.

Replace this directory's contents with real captured output from a
`sqlite3`-capable environment, then rename it to the captured version tag
and update `board.TestedAgmsgVersion` to match.
