# Workspace Backend

Server startup, YAML configuration, PTY/SSH/tmux session management, pane Git/PR metadata, split
preview serving, and pane URL opening.

## Contracts

* [Startup, Configuration, Sessions, and Pane Metadata](sessions.md) - Startup sequence, YAML configuration rules, operational assumptions, distribution, SSH connections, and pane Git/PR worktree resolution.
* [Split Preview and Pane URL Opening](preview-and-urls.md) - Local and SSH split preview resolution, SFTP and loopback relays, Mermaid rendering, browser-open interception, and OAuth loopback port forwarding.
