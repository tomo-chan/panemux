import { execFileSync } from 'node:child_process'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

// Stops the tmux server run-panemux-screenshots.sh started, and with it the
// dev-server command its session runs. Playwright stops only the web server
// (panemux); the tmux server daemonized away from it.
export default function globalTeardown() {
  const env = join(dirname(fileURLToPath(import.meta.url)), 'screenshots-env.sh')
  execFileSync('sh', ['-c', '. "$1" && shot_teardown', 'sh', env], { stdio: 'inherit' })
}
