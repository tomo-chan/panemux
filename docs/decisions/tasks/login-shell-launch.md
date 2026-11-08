---
type: Decision
title: "A started agent runs through the user's login and interactive shell (2026-10-29, issue #445)"
description: "Rationale for A started agent runs through the user's login and interactive shell."
---

# A started agent runs through the user's login and interactive shell (2026-10-29, issue #445)

Dashboard starts could not read ANTHROPIC_API_KEY exported in ~/.bashrc, while pane shells could.
Login and interactive modes are both needed to read the same startup files; either alone misses
common rc paths. Bash/zsh/sh use -lic, while other shells retain direct start.
Copying secrets into tmux argv/files was rejected, as was racing shell startup with send-keys.
Review found rc files can replace positional parameters with set -- or shift. The fixed launcher
therefore reads/removes the prompt file first and exports PANEMUX_LAUNCH_ variables, expanded after
rc execution and unset by the agent command. Reading prompt/cwd/PATH after rc also prevents rc
changes from undoing the launch setup. Shell arrays lack sh support; another argument-bearing
launcher file would duplicate the prompt on disk. An rc bypass setting was not requested.
See [launch security](../../security/command-execution.md) for current argument and prompt handling.
