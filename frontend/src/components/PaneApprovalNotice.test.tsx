import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PaneApprovalNotice } from './PaneApprovalNotice'

// Issue #404: the strip a pane shows while a task it runs waits on a codex
// permission request.
describe('PaneApprovalNotice', () => {
  it('says what the task waits for in the Tasks detail words, and offers Input given for one task', () => {
    const onInputGiven = vi.fn(async () => null)
    render(<PaneApprovalNotice waits={1} onInputGiven={onInputGiven} onReview={vi.fn()} />)

    expect(screen.getByRole('status', { name: 'Codex permission request' })).toHaveTextContent('Waiting for input: a permission request from codex')
    expect(screen.queryByRole('button', { name: 'Review in Tasks' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Input given' }))
    expect(onInputGiven).toHaveBeenCalledTimes(1)
  })

  it('offers Review in Tasks, not Input given, for more than one task', () => {
    const onInputGiven = vi.fn(async () => null)
    const onReview = vi.fn()
    render(<PaneApprovalNotice waits={2} onInputGiven={onInputGiven} onReview={onReview} />)

    expect(screen.getByRole('status')).toHaveTextContent('Waiting for input: a permission request from codex · 2 tasks')
    expect(screen.queryByRole('button', { name: 'Input given' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Review in Tasks' }))
    expect(onReview).toHaveBeenCalledTimes(1)
    expect(onInputGiven).not.toHaveBeenCalled()
  })

  it('disables Input given while it is being sent, and says why it failed', async () => {
    let finish: (failure: string | null) => void = () => {}
    const onInputGiven = vi.fn(() => new Promise<string | null>((resolve) => { finish = resolve }))
    render(<PaneApprovalNotice waits={1} onInputGiven={onInputGiven} onReview={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Input given' }))
    expect(screen.getByRole('button', { name: 'Input given' })).toBeDisabled()

    await act(async () => finish('HTTP 500'))
    expect(screen.getByRole('button', { name: 'Input given' })).toBeEnabled()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not mark the input given: HTTP 500')

    onInputGiven.mockResolvedValueOnce(null)
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Input given' })))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('leaves every key to the terminal', () => {
    const onInputGiven = vi.fn(async () => null)
    const onKeyDown = vi.fn()
    render(
      <div onKeyDown={onKeyDown}>
        <PaneApprovalNotice waits={1} onInputGiven={onInputGiven} onReview={vi.fn()} />
      </div>,
    )
    for (const key of ['Tab', 'Escape', 'Enter']) {
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
      screen.getByRole('status').dispatchEvent(event)
      expect(event.defaultPrevented).toBe(false)
    }
    expect(onKeyDown).toHaveBeenCalledTimes(3)
    expect(onInputGiven).not.toHaveBeenCalled()
  })
})
