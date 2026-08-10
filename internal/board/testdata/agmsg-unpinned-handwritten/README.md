# Hand-written agmsg fixtures — awaiting real capture

The fixtures in this directory are **not** captured from a real, pinned
agmsg install. Attempting that in this session's sandbox failed on two
independent grounds: `sqlite3` (one of agmsg's own runtime dependencies) is
not installed, and this environment's network policy returns `403` for
`github.com/fujibee/agmsg`, so neither installing agmsg nor cloning its
source to verify script behavior against a real run was possible here.

These files were instead written by hand to match the JSONL shape
documented in [docs/agent-board.md](../../../../docs/agent-board.md)'s
"Integration with agmsg" section, which is itself sourced from a prior
reading of agmsg's own script source. They satisfy Tier 1 of the [agmsg
compatibility contract](../../../../docs/agent-board.md#agmsg-compatibility-contract)
— fast, hermetic parsing tests — but they cannot substitute for Tier 2 (a
real, pinned agmsg install exercised in CI), and this directory's name
(`agmsg-unpinned-handwritten`, not `agmsg-<real-version-tag>`) reflects
that. Replace this directory's contents with real captured output the next
time an environment with network access to GitHub and a working `sqlite3`
is available, then rename it to the actual pinned version tag.
