import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TaskTerminal, taskTerminalStatus } from './TaskTerminal'

const { useTerminalMock, terminalState } = vi.hoisted(() => {
  const terminalState = {
    connected: false,
    sessionState: 'running' as 'running' | 'disconnected' | 'exited',
    reconnectFailed: false,
    handleResize: vi.fn(),
  }
  const useTerminalMock = vi.fn(() => ({
    ...terminalState,
    dims: null,
    restartSession: vi.fn(),
  }))
  return { useTerminalMock, terminalState }
})

vi.mock('../hooks/useTerminal', () => ({ useTerminal: useTerminalMock }))

class ResizeObserverStub {
  static instances: ResizeObserverStub[] = []
  callback: () => void
  constructor(callback: () => void) {
    this.callback = callback
    ResizeObserverStub.instances.push(this)
  }
  observe() {}
  disconnect() {}
}

describe('taskTerminalStatus', () => {
  const base = { connected: false, sessionState: 'running' as const, reconnectFailed: false, everConnected: false }

  it('is connecting until the WebSocket first opens', () => {
    expect(taskTerminalStatus(base)).toBe('connecting')
  })

  it('is connected only while the WebSocket is open and the session running', () => {
    expect(taskTerminalStatus({ ...base, connected: true, everConnected: true })).toBe('connected')
    expect(taskTerminalStatus({ ...base, connected: true, everConnected: true, sessionState: 'disconnected' }))
      .toBe('disconnected')
  })

  it('is failed when it never connected and gave up', () => {
    expect(taskTerminalStatus({ ...base, reconnectFailed: true })).toBe('failed')
  })

  it('is disconnected once it had connected and the socket closed', () => {
    expect(taskTerminalStatus({ ...base, everConnected: true })).toBe('disconnected')
    expect(taskTerminalStatus({ ...base, everConnected: true, reconnectFailed: true })).toBe('disconnected')
  })

  it('is ended when the tmux client exited, connected or not', () => {
    expect(taskTerminalStatus({ ...base, connected: true, everConnected: true, sessionState: 'exited' })).toBe('ended')
    expect(taskTerminalStatus({ ...base, sessionState: 'exited' })).toBe('ended')
  })
})

describe('TaskTerminal', () => {
  const originalResizeObserver = globalThis.ResizeObserver

  beforeEach(() => {
    Object.assign(terminalState, { connected: false, sessionState: 'running', reconnectFailed: false })
    ResizeObserverStub.instances = []
    globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
    useTerminalMock.mockClear()
    terminalState.handleResize.mockClear()
  })

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver
  })

  it('reads the board attach without asking for a pane restart', () => {
    render(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={vi.fn()} />)
    expect(useTerminalMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ sessionId: 'board-0123456789abcdef', recoverOnDisconnect: false }),
    )
  })

  it('takes no input and reports connecting until connected', () => {
    const onStatus = vi.fn()
    const { getByTestId } = render(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={onStatus} />)
    expect(getByTestId('task-terminal-surface')).toHaveAttribute('inert')
    expect(onStatus).toHaveBeenLastCalledWith('connecting')
  })

  it('takes input once connected, and none again after a disconnect', () => {
    const onStatus = vi.fn()
    const { getByTestId, rerender } = render(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={onStatus} />)

    terminalState.connected = true
    rerender(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={onStatus} />)
    expect(getByTestId('task-terminal-surface')).not.toHaveAttribute('inert')
    expect(onStatus).toHaveBeenLastCalledWith('connected')

    terminalState.connected = false
    rerender(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={onStatus} />)
    expect(getByTestId('task-terminal-surface')).toHaveAttribute('inert')
    expect(onStatus).toHaveBeenLastCalledWith('disconnected')
  })

  it('moves focus into the terminal when it connects', () => {
    terminalState.connected = false
    const { getByTestId, rerender } = render(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={vi.fn()} />)
    const textarea = document.createElement('textarea')
    textarea.className = 'xterm-helper-textarea'
    getByTestId('task-terminal-surface').appendChild(textarea)

    terminalState.connected = true
    rerender(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={vi.fn()} />)
    expect(document.activeElement).toBe(textarea)
  })

  it('resizes the terminal when its surface changes size', () => {
    terminalState.connected = true
    render(<TaskTerminal sessionId="board-0123456789abcdef" onStatus={vi.fn()} />)
    act(() => ResizeObserverStub.instances.at(-1)!.callback())
    expect(terminalState.handleResize).toHaveBeenCalled()
  })
})
