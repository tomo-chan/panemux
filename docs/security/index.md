# Security

Security requirements grouped by execution sink and trust boundary.

* [Security: Agent Board](agent-board.md) - Agent Board remote-write constraints and treatment of agent-reported values.
* [Security: auth token and transport encryption](auth.md) - Browser request guards, bearer tokens, loopback bootstrap, and transport encryption requirements.
* [Security: command center subprocess execution](command-center.md) - Command center subprocess execution, token handling, and working directory constraints.
* [Security: command execution sinks](command-execution.md) - Common command execution requirements and routes to each execution boundary.
* [Security: shell paths](shell-path.md) - Trusted shell selection and validation before local process execution.
* [Security: tmux sessions](tmux.md) - Validated exact tmux targets and guarded task and host terminal attachment.
* [Security: SSH execution](ssh-execution.md) - Remote path guards, credential path resolution and SSH proxy command trust boundaries.
* [Security: operator browser launch](browser-open.md) - Fixed platform browser programs and discrete URL arguments for --open.
* [Security: host execution](host-execution.md) - Trusted scripts, separate stdin and output capture contracts for host operations.
* [Security: agmsg acquisition and installation](agmsg-install.md) - Pinned upstream source verification and fixed-script agmsg installation.
* [Security: task collection](task-collection.md) - Fixed task collection scripts and untrusted host output guards.
* [Security: Codex permission hook](codex-hook.md) - Data-only permission records and guarded fixed-script hook installation.
* [Security: task agent probe](task-probe.md) - Fixed CLI availability probes and bounded host diagnostics.
* [Security: task launch and resume](task-launch.md) - Validated launch operands, shell boundaries and fixed agent argument forms.
* [Security: task summaries](task-summaries.md) - Conversation extraction, agent runner restrictions and private summary output.
* [Security: the split preview](preview.md) - Preview isolation, path validation, symbolic links, tokens, and remote content boundaries.
* [Security: opening URLs from a pane](url-open.md) - URL validation and execution boundaries for browser opening and loopback forwarding.
