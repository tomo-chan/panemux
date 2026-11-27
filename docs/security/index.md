# Security

Implementation security invariants, trust boundaries, and per-sink requirements. All security
requirements live here as the single source of truth.

## Requirements by boundary

* [Security overview and invariants](overview.md) - General command-execution rules, development sandbox and runtime boundaries, `gosec` policy, and the `known_hosts` exception.
* [Authentication and browser request guard](auth.md) - Bearer authentication, loopback token bootstrap, MCP token files, transport encryption, and the shared browser request guard.
* [Local process, tmux, and SSH execution](process-and-ssh.md) - Shell path allowlists, tmux target and host-terminal bounds, SSH remote paths, credential resolution, `ProxyCommand`, host execution lanes, and operator browser launch.
* [Task dashboard security](tasks.md) - Fixed task collection and probe scripts, launch/resume shell handover, Claude and Codex summary runners, and the Codex permission hook.
* [Agent Board and command center security](agent-board.md) - Deployment and message trust, remote agmsg writes, dashboard rendering, pinned agmsg installation, and command center subprocess isolation.
* [Split preview and URL-open security](preview-and-urls.md) - Local and SSH split preview isolation, Markdown/Mermaid rendering, loopback relays, OAuth callback forwarding, and browser-open interception.
