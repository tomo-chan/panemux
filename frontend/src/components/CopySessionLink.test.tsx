import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CopySessionLink } from './CopySessionLink'

afterEach(() => vi.unstubAllGlobals())
const task = { host: '', agent: 'claude', session_id: 'abc' }

describe('CopySessionLink', () => {
  it('copies a link without changing the current URL', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const before = window.location.href
    render(<CopySessionLink task={task} />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy session link' }))
    await screen.findByText('Copied')
    expect(new URL(writeText.mock.calls[0][0]).searchParams.get('session_id')).toBe('abc')
    expect(window.location.href).toBe(before)
  })
  it.each([undefined, { writeText: vi.fn().mockRejectedValue(new Error('Denied')) }])('offers manual copying when clipboard is unavailable', async (clipboard) => {
    vi.stubGlobal('navigator', { clipboard })
    render(<CopySessionLink task={task} />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy session link' }))
    await waitFor(() => expect((screen.getByRole('textbox', { name: 'Session link' }) as HTMLInputElement).value).toContain('session_id=abc'))
  })
  it('explains why an unkeyed session cannot be linked', () => {
    render(<CopySessionLink task={{ host: '', agent: 'codex' }} />)
    expect(screen.getByRole('button')).toBeDisabled()
    expect(screen.getByText('Session ID is not known yet')).toBeInTheDocument()
  })
})
