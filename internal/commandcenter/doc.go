// Package commandcenter implements the Agent Board command center: a
// headless, per-query `claude -p --resume` subprocess that reads and writes
// the board only through panemux's own authenticated REST API (see
// docs/agent-board.md's Command center section).
package commandcenter
