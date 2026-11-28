import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BoardMessageForm } from './BoardMessageForm'

afterEach(() => vi.unstubAllGlobals())

function setup(status = 200, response: unknown = { delivered: ['worker@local', 'pane-a'] }) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith('/broadcast')) return { ok: status === 200, status, json: async () => response }
    return { ok: true, json: async () => url.endsWith('/status') ? { identities: {
      'worker@local': { name: 'worker', host: 'local', team: 'demo', pane_id: null, source: 'unreported', state: null,
        waiting_for: null, status_since: null, task_observed_at: null, last_message_at: null, self_report: null },
    }, statuses: {}, participation: {} } : { messages: [], epoch: 'e' } }
  })
  vi.stubGlobal('fetch', fetcher)
  render(<BoardMessageForm token="secret" boardPanes={[{ id: 'pane-a', title: 'Main' }]} />)
  return fetcher
}

function fill(to = 'worker@local, pane-a') {
  fireEvent.change(screen.getByLabelText('Recipients'), { target: { value: to } })
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Please review\nnow.' } })
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }))
}

it('offers observed names and compatible panes, sends exact recipients with bearer auth and preserves the body', async () => {
  const fetcher = setup()
  expect(await screen.findByRole('option', { name: 'worker@local · observed' })).toBeDefined()
  fireEvent.change(screen.getByLabelText('Add recipient'), { target: { value: 'worker@local' } })
  expect(screen.getByLabelText('Recipients')).toHaveValue('worker@local')
  fireEvent.change(screen.getByLabelText('Add recipient'), { target: { value: 'pane-a' } })
  expect(screen.getByLabelText('Recipients')).toHaveValue('worker@local, pane-a')
  fill()
  expect(await screen.findByText('Sent to: worker@local, pane-a')).toBeDefined()
  expect(fetcher).toHaveBeenCalledWith('/api/board/broadcast', {
    method: 'POST', headers: { Authorization: 'Bearer secret', 'Content-Type': 'application/json' },
    body: JSON.stringify({ to: ['worker@local', 'pane-a'], body: 'Please review\nnow.' }),
  })
  expect(screen.getByLabelText('Message body')).toHaveValue('Please review\nnow.')
})

it('allows unobserved qualified names and shows validation as zero sends', async () => {
  setup(422, { error: 'unknown host' })
  fill('new-name@ssh:missing')
  expect(await screen.findByRole('alert')).toHaveTextContent('Not sent: unknown host')
  expect(screen.getByLabelText('Recipients')).toHaveValue('new-name@ssh:missing')
})

it('shows actual partial delivery without automatically retrying already delivered recipients', async () => {
  const fetcher = setup(502, { error: 'offline', delivered: ['worker@local'] })
  fill()
  expect(await screen.findByRole('alert')).toHaveTextContent('Partially sent. Delivered: worker@local. Not sent: pane-a. offline')
  expect(fetcher.mock.calls.filter(([url]) => url.endsWith('/broadcast'))).toHaveLength(1)
})

it('shows unknown delivery after a transport error, without retrying', async () => {
  const fetcher = setup()
  fetcher.mockImplementation(async (url: string) => { if (url.endsWith('/broadcast')) throw new Error('network'); return { ok: false, status: 500, json: async () => ({}) } })
  fill()
  expect(await screen.findByRole('alert')).toHaveTextContent('Delivery unknown')
  await waitFor(() => expect(screen.getByRole('button', { name: 'Send message' })).not.toBeDisabled())
})
