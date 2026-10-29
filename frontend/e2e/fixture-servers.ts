// Shared with the dependency-free fixture lifecycle tests. Keep shutdown policy
// beside the server definitions so the tests exercise the policy Playwright uses.
export const AGENT_BOARD_BASE_URL = 'http://127.0.0.1:4174'
export const AGENT_BOARD_AGMSG_BASE_URL = 'http://127.0.0.1:4175'
export const COMMAND_CENTER_BASE_URL = 'http://127.0.0.1:4176'

const gracefulShutdown = { signal: 'SIGTERM' as const, timeout: 5000 }
export const ownedHomeServers = [
  { command: 'sh ./e2e/run-panemux-e2e.sh agent-board.yml 4174', url: AGENT_BOARD_BASE_URL, gracefulShutdown },
  { command: 'sh ./e2e/run-panemux-agent-board-e2e.sh', url: AGENT_BOARD_AGMSG_BASE_URL, gracefulShutdown },
  { command: 'sh ./e2e/run-panemux-command-center-e2e.sh', url: COMMAND_CENTER_BASE_URL, gracefulShutdown },
]
