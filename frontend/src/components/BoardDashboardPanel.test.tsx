import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { BoardDashboardPanel } from './BoardDashboardPanel'

function fetchRouter(handlers: {
  status?: () => Response | Promise<Response>
  messages?: () => Response | Promise<Response>
}) {
  return vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.startsWith('/api/board/status')) {
      return Promise.resolve(
        handlers.status ? handlers.status() : ({ ok: true, json: async () => ({ statuses: {} }) } as Response),
      )
    }
    if (url.startsWith('/api/board/messages')) {
      return Promise.resolve(
        handlers.messages ? handlers.messages() : ({ ok: true, json: async () => ({ messages: [] }) } as Response),
      )
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

describe('BoardDashboardPanel', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchRouter({}))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('does not render when closed', () => {
    render(<BoardDashboardPanel isOpen={false} token="tok" onClose={vi.fn()} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('renders a pane status card from the statuses fixture', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({
          statuses: {
            main: {
              updated_at: new Date().toISOString(),
              state: 'working',
              repo: 'panemux',
              branch: 'feature/dashboard',
              summary: 'Running tests',
              last_tool: 'Bash',
              cwd: '/workspace/user/project',
            },
          },
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('main')).toBeDefined()
    expect(screen.getByText('working')).toBeDefined()
    expect(screen.getByText('panemux')).toBeDefined()
    expect(screen.getByText('feature/dashboard')).toBeDefined()
    expect(screen.getByText('Running tests')).toBeDefined()
    expect(screen.getByText(/tool:\s*Bash/)).toBeDefined()
    expect(screen.getByText('/workspace/user/project')).toBeDefined()
  })

  it('renders pr_url as an external link', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({
          statuses: {
            main: {
              updated_at: new Date().toISOString(),
              pr_url: 'https://github.com/example/panemux/pull/42',
            },
          },
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    const link = await screen.findByRole('link', { name: /pr #42|pull\/42/i })
    expect(link).toHaveAttribute('href', 'https://github.com/example/panemux/pull/42')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('renders a card with every optional field omitted without crashing', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: new Date().toISOString() } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('main')).toBeDefined()
  })

  it('renders an unrecognized state string without crashing', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({
          statuses: { main: { updated_at: new Date().toISOString(), state: 'something-new' } },
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('something-new')).toBeDefined()
  })

  it('shows a stale pill for a status older than 5 minutes', async () => {
    const old = new Date(Date.now() - 10 * 60 * 1000).toISOString()
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: old, state: 'idle' } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('stale')).toBeDefined()
  })

  it('does not show a stale pill for a recent status', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: new Date().toISOString(), state: 'idle' } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    await screen.findByText('main')
    expect(screen.queryByText('stale')).toBeNull()
  })

  it('excludes board_status rows from the message feed', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      messages: () => ({
        ok: true,
        json: async () => ({
          messages: [
            { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'main', to: '_system', body: JSON.stringify({ kind: 'board_status' }), seq: 1 },
            { at: '2026-08-14T12:00:01Z', host: 'devbox', team: 'panemux', from: 'main', to: 'side', body: 'hello there', seq: 2 },
          ],
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('hello there')).toBeDefined()
    expect(screen.queryByText(/board_status/)).toBeNull()
  })

  it('shows the empty states when there are no statuses or messages', async () => {
    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('No pane has reported status yet.')).toBeDefined()
    expect(screen.getByText('No messages yet.')).toBeDefined()
  })

  it('shows an error message while keeping the panel open', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({ ok: false, status: 401 } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('Not authorized to view the agent board.')).toBeDefined()
    expect(screen.getByRole('dialog')).toBeDefined()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the overlay backdrop', async () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" onClose={onClose} />)

    fireEvent.click(await screen.findByRole('dialog'))

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the close button', async () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" onClose={onClose} />)

    fireEvent.click(await screen.findByLabelText('Close agent board panel'))

    expect(onClose).toHaveBeenCalled()
  })

  it('restores focus to the previously focused element when it closes', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = render(<BoardDashboardPanel isOpen={false} token="tok" onClose={vi.fn()} />)
    rerender(<BoardDashboardPanel isOpen token="tok" onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('dialog')).toBeDefined())

    rerender(<BoardDashboardPanel isOpen={false} token="tok" onClose={vi.fn()} />)

    expect(document.activeElement).toBe(trigger)
  })
})
