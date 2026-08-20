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
        handlers.messages ? handlers.messages() : ({ ok: true, json: async () => ({ messages: [], epoch: 'e1' }) } as Response),
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
    render(<BoardDashboardPanel isOpen={false} token="tok" boardPaneIds={[]} onClose={vi.fn()} />)
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

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('main')).toBeDefined()
    expect(screen.getByText('working')).toBeDefined()
    expect(screen.getByText('Running tests')).toBeDefined()
    expect(screen.getByText(/tool:\s*Bash/)).toBeDefined()
  })


  it('renders a card with every optional field omitted without crashing', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: new Date().toISOString() } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

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

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

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

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('stale')).toBeDefined()
  })

  it('does not show a stale pill for a recent status', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: new Date().toISOString(), state: 'idle' } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    await screen.findByText('main')
    expect(screen.queryByText('stale')).toBeNull()
  })

  it('excludes board_status rows from the message feed', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      messages: () => ({
        ok: true,
        json: async () => ({
          messages: [
            { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'main', to: '_system', body: JSON.stringify({ kind: 'board_status' }), is_status: true, seq: 1 },
            { at: '2026-08-14T12:00:01Z', host: 'devbox', team: 'panemux', from: 'main', to: 'side', body: 'hello there', is_status: false, seq: 2 },
          ],
          epoch: 'e1',
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('hello there')).toBeDefined()
    expect(screen.queryByText(/board_status/)).toBeNull()
  })

  it('shows the empty states when there are no statuses or messages', async () => {
    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('No pane has agent board enabled yet.')).toBeDefined()
    expect(screen.getByText('No messages yet.')).toBeDefined()
  })

  it('shows an error message while keeping the panel open', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({ ok: false, status: 401 } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('Not authorized to view the agent board.')).toBeDefined()
    expect(screen.getByRole('dialog')).toBeDefined()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  // Regression test for the Escape handler's listener phase, not merely
  // that it exists: a focused xterm terminal stops keydown propagation
  // before it ever reaches a bubble-phase window listener, so a
  // bubble-registered Escape handler silently does nothing in exactly the
  // state Cmd/Ctrl+Shift+B leaves the user in (panel open, terminal still
  // holding focus). Verified in a real browser by
  // frontend/e2e/agent-board.spec.ts; this is the same assertion at the
  // smallest unit, with a stand-in element that swallows the event the same
  // way.
  it('closes on Escape even when the focused element stops propagation before window', () => {
    const onClose = vi.fn()
    const swallowingTarget = document.createElement('textarea')
    swallowingTarget.addEventListener('keydown', (event) => event.stopPropagation())
    document.body.appendChild(swallowingTarget)

    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={onClose} />)

    fireEvent.keyDown(swallowingTarget, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the overlay backdrop', async () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={onClose} />)

    fireEvent.click(await screen.findByRole('dialog'))

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the close button', async () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={onClose} />)

    fireEvent.click(await screen.findByLabelText('Close agent board panel'))

    expect(onClose).toHaveBeenCalled()
  })

  it('restores focus to the previously focused element when it closes', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = render(<BoardDashboardPanel isOpen={false} token="tok" boardPaneIds={[]} onClose={vi.fn()} />)
    rerender(<BoardDashboardPanel isOpen token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('dialog')).toBeDefined())

    rerender(<BoardDashboardPanel isOpen={false} token="tok" boardPaneIds={[]} onClose={vi.fn()} />)

    expect(document.activeElement).toBe(trigger)
  })
})

describe('BoardDashboardPanel connection and activity', () => {
  const reported = {
    updated_at: new Date().toISOString(),
    state: 'working',
    summary: 'Rewriting the relay tests',
    last_tool: 'Edit',
    repo: 'example/project',
    branch: 'feature/x',
    pr_url: 'https://github.com/example/project/pull/42',
  }

  function renderPanel(statuses: Record<string, unknown>, boardPaneIds: string[]) {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) =>
      Promise.resolve({
        ok: true,
        json: async () => (String(url).includes('/status') ? { statuses } : { messages: [], epoch: 'e' }),
      }),
    ))
    render(<BoardDashboardPanel isOpen token="tok" boardPaneIds={boardPaneIds} onClose={vi.fn()} />)
  }

  // The point of the board is telling you whether a pane is actually on it.
  // Listing only panes that already reported made "configured but never
  // joined" indistinguishable from "not configured at all".
  it('lists a board-enabled pane that has never reported', async () => {
    renderPanel({}, ['api', 'web'])

    expect(await screen.findByText('api')).toBeDefined()
    expect(screen.getByText('web')).toBeDefined()
    expect(screen.getAllByText('not joined')).toHaveLength(2)
  })

  it('shows what a joined pane is doing', async () => {
    renderPanel({ api: reported }, ['api'])

    expect(await screen.findByText('working')).toBeDefined()
    expect(screen.getByText('Rewriting the relay tests')).toBeDefined()
    expect(screen.getByText('tool: Edit')).toBeDefined()
    expect(screen.queryByText('not joined')).toBeNull()
  })

  // repo/branch/PR come from panemux running git itself, shown in the pane
  // header and the workspace bar. The board's copies are self-reported and
  // can contradict them, so the board does not repeat them.
  it('does not repeat the repo, branch or PR the header already owns', async () => {
    renderPanel({ api: reported }, ['api'])
    await screen.findByText('working')

    expect(screen.queryByText('example/project')).toBeNull()
    expect(screen.queryByText('feature/x')).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('still reports a pane that left the config but has a status', async () => {
    renderPanel({ ghost: reported }, [])
    expect(await screen.findByText('ghost')).toBeDefined()
  })

  it('keeps the empty state when no pane is board-enabled', async () => {
    renderPanel({}, [])
    expect(await screen.findByText('No pane has agent board enabled yet.')).toBeDefined()
  })
})
