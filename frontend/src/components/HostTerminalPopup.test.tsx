import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ComponentProps } from 'react'
import type { TaskTerminal } from './TaskTerminal'
import { HostTerminalPopup } from './HostTerminalPopup'

const terminal = vi.hoisted(() => ({ props: {} as ComponentProps<typeof TaskTerminal> }))
vi.mock('./TaskTerminal', () => ({
  TaskTerminal: (props: ComponentProps<typeof TaskTerminal>) => {
    terminal.props = props
    return <div data-testid="terminal" />
  },
}))

function props(): ComponentProps<typeof HostTerminalPopup> {
  return {
    session: {
      host: 'dev-server', type: 'ssh', tmuxSession: undefined, originElement: null,
      attach: { phase: 'ready', sessionId: 'board-0123456789abcdef', tmuxSession: '' },
      attempt: 0, terminal: 'connected',
    },
    onTerminalStatus: vi.fn(), maximized: false, sheet: false, topOffset: 0,
    onToggleMaximize: vi.fn(), onClose: vi.fn(), onRetry: vi.fn(),
  }
}

const url = 'http://localhost:34567/callback'
let fetchMock: ReturnType<typeof vi.fn>
let openMock: ReturnType<typeof vi.spyOn>
let tab: { opener: unknown; location: { href: string }; close: ReturnType<typeof vi.fn> }

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

beforeEach(() => {
  fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ forwarded: true, port: 34567, url }) })
  vi.stubGlobal('fetch', fetchMock)
  tab = { opener: {}, location: { href: 'about:blank' }, close: vi.fn() }
  openMock = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
})

describe('HostTerminalPopup URL opening (issue #455)', () => {
  it.each(['ssh', 'ssh_tmux'] as const)('%s opens printed links through the host session forward', async (type) => {
    const p = props()
    p.session.type = type
    render(<HostTerminalPopup {...p} />)
    act(() => terminal.props.onLinkActivate?.(url))
    expect(openMock).toHaveBeenCalledWith('about:blank', '_blank')
    await waitFor(() => expect(tab.location.href).toBe(url))
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/board-0123456789abcdef/open-url', expect.objectContaining({ body: JSON.stringify({ url }) }))
    expect(screen.queryByRole('button', { name: 'Preview' })).toBeNull()
  })

  it.each(['program', 'link'] as const)('asks before a %s request, supports Ignore and opens once after approval', async (source) => {
    render(<HostTerminalPopup {...props()} />)
    act(() => terminal.props.onBrowserOpenRequest?.(url, source))
    expect(screen.getByText(source === 'link' ? 'This link goes to' : 'This pane wants to open')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Preview' })).toBeNull()
    expect(openMock).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Ignore' }))
    expect(screen.queryByRole('button', { name: 'Open' })).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
    act(() => terminal.props.onBrowserOpenRequest?.(url, source))
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    await waitFor(() => expect(tab.location.href).toBe(url))
    expect(openMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it.each(['attempt', 'session', 'connecting'] as const)('drops a pending request when %s changes', (change) => {
    const p = props()
    const view = render(<HostTerminalPopup {...p} />)
    act(() => terminal.props.onBrowserOpenRequest?.(url))
    expect(screen.getByRole('button', { name: 'Open' })).toBeInTheDocument()
    const next = { ...p.session, attempt: change === 'attempt' ? 1 : 0 }
    if (change === 'session') next.attach = { phase: 'ready', sessionId: 'board-fedcba9876543210', tmuxSession: '' }
    if (change === 'connecting') next.attach = { phase: 'connecting' }
    view.rerender(<HostTerminalPopup {...p} session={next} />)
    expect(screen.queryByRole('button', { name: 'Open' })).toBeNull()
    expect(openMock).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('reports a forward failure and closes the blank tab, and allows dismissal', async () => {
    fetchMock.mockResolvedValue({ ok: false, text: async () => 'port already in use' })
    render(<HostTerminalPopup {...props()} />)
    act(() => terminal.props.onLinkActivate?.(url))
    expect(await screen.findByText(/Port forwarding failed: port already in use/)).toBeInTheDocument()
    expect(tab.close).toHaveBeenCalledOnce()
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText(/Port forwarding failed/)).toBeNull()
  })
})
