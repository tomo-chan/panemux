import { defineConfig } from '@playwright/test'
import config from './playwright.config'

// Run just this spec and its isolated installation, without starting unrelated
// servers (or the real-tmux fixtures) for a focused Agent Board change.
export default defineConfig({
  ...config,
  webServer: Array.isArray(config.webServer) ? config.webServer.filter(server => server.url === 'http://127.0.0.1:4175') : [],
  projects: config.projects?.filter(project => project.name === 'chromium-agent-board-agmsg'),
})
