# Application Overview

## Purpose

`PaneMux` is a browser-based terminal workspace that displays multiple terminal sessions in a resizable split layout. A single server process hosts both the frontend UI and the backend APIs, then bridges browser input/output to local PTYs, SSH shells, local tmux sessions, or tmux sessions reached over SSH.

The application is optimized for a local or trusted-network workflow where a developer wants one browser tab to act as a terminal dashboard.

## Core Capabilities

- Render a recursive split-pane layout from YAML configuration.
- Start terminal sessions from config at server startup.
- Stream terminal bytes between browser and backend over WebSocket.
- Persist layout edits back to the config file when the app was launched from one.
- Support four pane backends: `local`, `ssh`, `tmux`, and `ssh_tmux`.
- Allow limited runtime pane creation/removal from the UI.
- Build as a self-contained CLI binary and distribute it through GitHub Releases.

## High-Level Data Flow

```text
config.yaml
   |
   v
Go server ---- REST (/api/layout, /api/display, /api/sessions)
   |
   +---- Session manager ---- PTY / SSH / tmux session implementations
   |
   +---- WebSocket (/ws/{sessionID}) ---- browser terminal pane
```

Each terminal pane owns one WebSocket connection. Binary frames carry raw terminal I/O. Text frames carry control messages such as resize notifications and session status.

## Why This Shape

### Single backend binary with embedded frontend

Why: distribution is simpler when the app can be started with one command and does not require a separate web server.

Benefits:

- Easier local setup and fewer moving parts.
- Backend and frontend versions are always paired.
- Suitable for a desktop-style local tool.
- Fits standard CLI distribution via tarballs and shell installers.

### Config-driven initial state

Why: terminal workspaces are usually predictable and repeated across sessions.

Benefits:

- Layout and SSH aliases are reproducible.
- Startup behavior is deterministic.
- Config can be committed, shared, and reviewed.

### Session-oriented design

Why: each pane maps cleanly to one terminal session with its own lifecycle and transport.

Benefits:

- Failure is isolated to a single pane.
- WebSocket handling stays simple.
- Session backends can vary behind one shared interface.

## Current Boundaries

- The app assumes a trusted environment: CORS is open and WebSocket origin checks are permissive.
- Authentication and authorization are not implemented.
- Runtime session creation exists, but it is intentionally narrow and mainly used by pane splitting in the UI.
- Terminal prompt rendering is currently validated against Chrome. In particular, oh-my-zsh themes that use Powerline glyphs, such as `agnoster`, depend on browser font behavior that is known to work in Chrome.
- Distribution is currently planned as CLI release archives for macOS and Linux. Windows users are expected to install and run it through WSL2 rather than a native Windows package.
