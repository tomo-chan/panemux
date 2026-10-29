import { afterEach, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { BoardDashboardPanel } from './BoardDashboardPanel'

afterEach(() => vi.unstubAllGlobals())

it('lists detected agents and reporting panes, excluding idle shells', async () => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => ({ ok: true, json: async () => url.includes('/status') ? {
    participation: {
      detected: { pane_id: 'detected', host: 'local', agent_type: 'codex', instructed: true },
      pending: { pane_id: 'pending', host: 'ssh:build', agent_type: 'claude', instructed: false },
      shell: { pane_id: 'shell', host: 'local', agent_type: '', instructed: false },
    },
    statuses: { reporter: { updated_at: new Date().toISOString(), summary: 'Reported from shell' } },
  } : { messages: [], epoch: 'test' } })))
  render(<BoardDashboardPanel isOpen token="token" boardPanes={[{ id: 'shell' }, { id: 'detected', title: 'Agent' }]} onClose={() => {}} />)
  expect(await screen.findByText('detected')).toBeDefined()
  expect(screen.getByText('instruction sent')).toBeDefined()
  expect(screen.getByText('reporter')).toBeDefined()
  expect(screen.queryByText('shell')).toBeNull()
  expect(screen.getByText('agent detected')).toBeDefined()
})
