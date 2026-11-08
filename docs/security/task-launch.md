---
type: Security Requirement
title: "Security: task launch and resume"
description: "Validated launch operands, shell boundaries and fixed agent argument forms."
---

# Security: task launch and resume

[Common execution requirements](command-execution.md) apply at this boundary.

`POST /api/tasks` and `POST /api/tasks/resume` ([behavior](../behavior/task-launch.md)) start
claude or codex on the panemux host or an `ssh_connections` host. Every request value stays out
of strings a shell parses.

## Script construction and validated operands

`launchScriptTemplate` and each launch/resume adapter fragment are Go constants, composed once
at startup. A fragment may set only its own variable, must stay inside its agent/mode case
(no `;;`, `esac` or `{{`), and may refuse only with a script- or adapter-declared code with a
fixed message. No request value reaches a fragment. Locally and remotely the command is the
literal `sh -s`, with the script on stdin; the remote login shell parses only that command.

`buildLaunchScript` checks each placeholder before filling it, refusing rather than escaping:

| Placeholder | Source and check |
|---|---|
| agent | Request-selected registry ID with the requested launch/resume adapter (`checkLaunchSessionID`); registry IDs exclude quotes. |
| mode | Go constant `new` or `resume`. |
| session ID | crypto/rand UUID for new Claude; empty for new Codex, which chooses its own; request UUID (`validUUID`) for either resume. |
| tmux name | `task-` plus Claude ID's first eight characters, Codex resume ID's last eight, or eight crypto/rand hex digits for new Codex; must pass `validTmuxSessionName`. |
| heredoc tag | 32 crypto/rand hex characters, matching `^[0-9a-f]{32}$`. |
| working directory | Request or resumed conversation log; must pass `session.ValidateRemotePath`, the [SSH path guard](ssh-execution.md#remote-path-arguments-ssh-working-directory). |
| first instruction | Nonempty, at most 32 KiB, no NUL or line equal to its own heredoc terminator. |

CWD and instruction use quoted heredocs (`<<'PANEMUX_CWD_<tag>'`, `<<'PANEMUX_PROMPT_<tag>'`)
and variables used only double-quoted, so neither expands as shell code. Session IDs and tmux
names are single-quoted; their allowlists exclude quotes.

## Executable lookup and shell handover

Only the literal agent names `claude` and `codex` run. Lookup uses `command -v "$agent"`, then
`"${SHELL:-/bin/sh}" -lc "command -v $agent"` if absent, with the fixed agent word. The host's
own `$SHELL` chooses the lookup environment, not a replacement command; this is not Go
`os.Getenv` flowing into `exec.Command`. Accept only an absolute path to an executable regular
file and pass it to tmux as a discrete argument. Codex's binary directory is appended **last**
to the agent's PATH, after rc processing, to find an npm install's node without overriding any
command already on the user's PATH.

`tmux new-session -d -s <name> -- <command...>` runs separate command arguments directly
(tmux 2.0+); `--` ends tmux options. A fixed `sh -c` script (`start`) receives the user's shell,
the resolved `sh`, the fixed agent command, the prompt file and operands as positional parameters.
It reads and removes the prompt file before handing over. It then runs the agent command directly,
or invokes the user's shell as login and interactive with one constant command and no arguments:

`exec "$PANEMUX_LAUNCH_SH" -c "$PANEMUX_LAUNCH_CMD" sh "$PANEMUX_LAUNCH_1" "$PANEMUX_LAUNCH_2" "$PANEMUX_LAUNCH_3" "$PANEMUX_LAUNCH_4"`

These exported variables carry the fixed command, `sh` path and operands (the prompt replaces its
file for a new task). The shell expands them quoted after its rc; the agent command unsets them
before anything else, so the agent never inherits them. Request data never becomes parsed code.

- Use the host's `$SHELL` only if it is an absolute executable regular file named `bash`, `zsh`
  or `sh`. Other values, including unset, fish, csh, dash, ksh and mksh by those names, run the
  agent command directly without reading rc.
- Resolve `sh` to an absolute path with `command -v sh` before rc; an rc's PATH change must not
  select that interpreter. Each stage `exec`s the next, retaining the pane's process identity.
- The user's rc runs with the user's rights, as in a pane. Its `set --` or `shift` cannot change
  the agent operands, which are environment variables rather than shell positional parameters.
  It may read or deliberately alter those variables, including the prompt. An rc that exits or
  execs another shell can leave an agent unstarted after reported success, but no prompt file.
- The agent command changes CWD and appends Codex's directory to the rc's PATH after rc, so rc
  cannot undo those operations. Panemux never reads or transfers the user's authentication key
  through an argument, file or tmux environment; `tmux -e NAME=value` would expose it in `ps`.

No request value goes into a tmux option that expands formats. In particular, do not pass CWD
through tmux `-c`: `#` formats can select a different directory even though the path guard prevents
`#(…)` execution. The fixed agent command instead uses `cd -- "$4"` (new) or `cd -- "$1"`
(resume) and fails before exec if it cannot change directory. Pane `new-session -c` is outside
this task launch contract.

## Agent arguments, privacy and failures

The prompt is written to a `mktemp` file under umask 077; `start` reads it with `p=$(cat -- "$f")`
and removes it. Substitution output is not reparsed. Tmux sees only the filename, never the prompt:
a server retains its first client's arguments for its lifetime. The prompt still becomes the
agent's positional argument, readable by someone able to inspect that process, and briefly the
starting `sh`'s arguments and the user's shell environment.

| Operation | Fixed agent command after CWD handling |
|---|---|
| New Claude | `exec "$2" "--session-id=$3" -- "$1"` |
| New Codex | `PATH=$PATH:${2%/*}; export PATH; exec "$2" --no-daemon -c check_for_update_on_startup=false -- "$1"` |
| Resume Claude | `claude --resume=<id>` using the resolved binary |
| Resume Codex | `codex --no-daemon -c check_for_update_on_startup=false resume -- <id>` using the resolved binary |

`--` keeps a leading-option prompt from becoming an option. Claude's optional-value `--resume`
requires the `=` form; the broader `validSessionID` allowlist alone would admit option-like IDs.
Both resumes accept only UUIDs, never session titles/names. `Service.Resume` recollects the host
and permits only a stopped task of the matching agent, using its own log's guarded working
directory (Codex `session_meta`), never a caller-selected transcript.

For Codex, `--no-daemon` ties the task to its TUI/tmux process. Before launch, `"$bin" --help`
with the same PATH addition must list that flag or the script refuses with `codex-too-old`;
help text is searched, never shown. The fixed `-c` disables only the update prompt. Other startup
screens (directory trust, model notice, usage offer) wait for a person in the pane; panemux does
not write Codex configuration. Slash commands remain enabled for interactive tasks, unlike the
command center's `claude -p`.

A resume may use `tmux new-window -t "=<name>:" -- <command…>` in an existing session only when
fresh collection found no running task there. A fixed `yes`/`no` carries that decision; nothing
existing is replaced or killed. Resumes for one (host, session) are serialized (`Service.lockResume`)
so the next collection sees the first launch and refuses a second agent in that conversation.

Host output is searched only for `::panemux-launch`; refusals are one of eight fixed words mapped
to fixed `LaunchError` messages. No host text reaches the response; no answer is failure.
`NormalizeLabels` runs before execution, so invalid labels cannot leave a started task without
labels. New Codex labels stay in memory by host/tmux session until collection finds its session ID;
only that host-reported ID receives them.
