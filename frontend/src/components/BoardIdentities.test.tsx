import { fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { BoardDashboardPanel } from './BoardDashboardPanel'
import { useBoardStatus } from '../hooks/useBoardStatus'
import type { BoardIdentity } from '../schemas'

const identity = (overrides: Partial<BoardIdentity> = {}): BoardIdentity => ({
  name: 'worker', host: 'local', team: 'demo', pane_id: null, source: 'unreported',
  state: null, waiting_for: null, status_since: null, task_observed_at: null,
  last_message_at: null, self_report: null, ...overrides,
})

afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })
beforeEach(() => Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }))

function serve(identities: Record<string, BoardIdentity>, messages: unknown[] = []) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => ({ ok: true, json: async () => url.includes('/status')
    ? { identities, statuses: { worker: { updated_at: '2026-01-01', state: 'legacy' } }, participation: {
      worker: { pane_id: 'worker', host: 'local', agent_type: 'claude', instructed: false },
      detected: { pane_id: 'detected', host: 'local', agent_type: 'codex', instructed: false },
    } }
    : { messages, epoch: 'e1' },
  })))
}

it('replaces the identity snapshot including removed hosts instead of retaining old names', async () => {
  const entries = { 'worker@local': identity() }
  serve(entries)
  const { result, rerender } = renderHook(({ enabled }) => useBoardStatus({ enabled, token: 'tok' }), { initialProps: { enabled: true } })
  await waitFor(() => expect(result.current.identities).toEqual(entries))
  rerender({ enabled: false })
  serve({})
  rerender({ enabled: true })
  await waitFor(() => expect(result.current.identities).toEqual({}))
})

it('merges pane details once, separates equal host names and keeps reports supplemental to tasks', async () => {
  serve({
    'worker@local': identity({ pane_id: 'worker', source: 'task', state: 'busy', waiting_for: 'approval', task_observed_at: new Date().toISOString(),
      self_report: { updated_at: new Date().toISOString(), state: 'waiting', summary: 'Review requested' } }),
    'worker@ssh:build': identity({ host: 'ssh:build', source: 'self_report', state: 'idle',
      self_report: { updated_at: new Date().toISOString(), state: 'idle' } }),
    'reader@local': identity({ name: 'reader' }),
  })
  render(<BoardDashboardPanel isOpen token="tok" boardPanes={[{ id: 'worker', title: 'Main worker' }]} onClose={() => {}} />)
  const local = (await screen.findByText('worker@local')).closest('article')!
  expect(within(local).getByText('Task observed')).toBeDefined()
  expect(within(local).getByText('busy')).toBeDefined()
  expect(within(local).getByText('Waiting for: approval')).toBeDefined()
  expect(within(local).getByText(/^Self-report/)).toBeDefined()
  expect(screen.getAllByText('Main worker')).toHaveLength(1)
  expect(screen.queryByText('legacy')).toBeNull()
  expect(screen.getByText('worker@ssh:build')).toBeDefined()
  const reader = screen.getByText('reader@local').closest('article')!
  expect(within(reader).getByText('Unreported')).toBeDefined()
  expect(within(reader).getByText(/No pane/)).toBeDefined()
  expect(within(reader).getAllByRole('button')).toHaveLength(1)
  expect(within(reader).queryByRole('link')).toBeNull()
  expect(screen.getByText('Detected panes')).toBeDefined()
  expect(screen.getByText('detected')).toBeDefined()
})

it.each([
  ['busy', 'rgb(123, 216, 143)'], ['wait', 'rgb(244, 191, 79)'],
  ['idle', 'rgb(122, 162, 247)'], ['run', 'rgb(143, 166, 196)'],
  ['unknown', 'rgb(197, 134, 192)'], ['stop', 'rgb(128, 133, 141)'],
])('uses the task observation palette for real task state %s', async (state, color) => {
  serve({ 'worker@local': identity({ source: 'task', state }) })
  render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={() => {}} />)
  const card = (await screen.findByText('worker@local')).closest('article')!
  const dot = within(card).getByText('Task observed').previousElementSibling as HTMLElement
  expect(dot.style.backgroundColor).toBe(color)
})

it('keeps an arbitrary self-reported task-like state neutral', async () => {
  serve({ 'worker@local': identity({ source: 'self_report', state: 'busy', self_report: { updated_at: new Date().toISOString(), state: 'busy' } }) })
  render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={() => {}} />)
  const card = (await screen.findByText('worker@local')).closest('article')!
  const dot = within(card).getByText('Self-report').previousElementSibling as HTMLElement
  expect(dot.style.backgroundColor).toBe('rgb(75, 85, 101)')
})

it('renders self-reports as expandable text with visible control characters and copies exact addresses', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  const summary = '<img src=x onerror=alert(1)>\r\nline\tend\u001b\u202e' + ' long'.repeat(100)
  serve({ 'worker@local': identity({ source: 'self_report', self_report: { updated_at: '2026-01-01', state: 'idle', summary } }) })
  render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={() => {}} />)
  const report = await screen.findByText(/<img src=x/)
  expect(report.textContent).toContain('line end\\u001b\\u202e')
  expect(report.querySelector('img')).toBeNull()
  expect(report.style.getPropertyValue('-webkit-line-clamp')).toBe('4')
  fireEvent.click(screen.getByRole('button', { name: 'Show more' }))
  expect(report.style.getPropertyValue('-webkit-line-clamp')).not.toBe('4')
  fireEvent.click(screen.getByRole('button', { name: 'Show less' }))
  fireEvent.click(screen.getByRole('button', { name: 'Copy address' }))
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('worker@local'))
  expect(await screen.findByText('Address copied')).toBeDefined()
})

it('keeps source host visible even for one host and preserves qualified and legacy recipients', async () => {
  serve({}, [
    { at: '2026-01-01', host: 'ssh:build', team: 'demo', from: 'worker', to: 'orch@local', body: 'ready', seq: 1, is_status: false },
    { at: '2026-01-01', host: 'ssh:build', team: 'demo', from: '_system', to: 'old-pane', body: 'legacy', seq: 2, is_status: false },
  ])
  render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={() => {}} />)
  expect(await screen.findByText('worker@ssh:build → orch@local')).toBeDefined()
  expect(screen.getByText('_system → old-pane')).toBeDefined()
  expect(screen.getAllByText(/ssh:build/).length).toBeGreaterThan(1)
})
