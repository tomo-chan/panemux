import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TaskHost } from '../schemas'
import UnreadableStateDialog from './UnreadableStateDialog'

const hosts: TaskHost[] = [
  {
    name: '',
    status: 'ok',
    unreadable_state_files: [
      {
        file: '48213.json',
        reason: 'not_json',
        detail: 'unexpected end of JSON input',
        pid: 48213,
        location: { kind: 'outside', attachable: false },
      },
    ],
  },
  {
    name: 'gpu-1',
    status: 'ok',
    unreadable_state_files: [{ file: '<img src=x>.json', reason: 'invalid_pid', detail: 'pid: "<b>5</b>"' }],
  },
]

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('UnreadableStateDialog', () => {
  it('lists one file per row with its host, reason, detail and process', () => {
    render(<UnreadableStateDialog isOpen hosts={hosts} onClose={vi.fn()} />)
    const dialog = screen.getByRole('dialog', { name: 'Unreadable session state · 2 files on 2 hosts' })
    const rows = within(dialog).getAllByRole('row').slice(1)
    expect(rows.map((row) => within(row).getAllByRole('cell').map((cell) => cell.textContent))).toEqual([
      ['Local', '48213.json', 'Not valid JSON' + 'unexpected end of JSON input', 'claude · pid 48213' + 'running, not in a pane'],
      ['gpu-1', '<img src=x>.json', 'pid missing or not positive' + 'pid: "<b>5</b>"', 'unknown' + 'file name is not <pid>.json'],
    ])
    expect(dialog.querySelector('img, b')).toBeNull()
  })

  it('names the fields panemux reads and links the steps to fix it', () => {
    render(<UnreadableStateDialog isOpen hosts={hosts} onClose={vi.fn()} />)
    for (const field of ['sessionId', 'pid', 'cwd', 'status']) {
      expect(screen.getByText(field, { selector: 'code' })).toBeInTheDocument()
    }
    const link = screen.getByRole('link', { name: /How to fix/ })
    expect(link).toHaveAttribute(
      'href',
      'https://github.com/tomo-chan/panemux/blob/main/docs/behavior/tasks.md#unreadable-state-files',
    )
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('closes from its button and from Escape', () => {
    const onClose = vi.fn()
    render(<UnreadableStateDialog isOpen hosts={hosts} onClose={onClose} />)
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('copies the details as text', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    render(<UnreadableStateDialog isOpen hosts={hosts} onClose={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy details' }))
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('Local\t48213.json\tNot valid JSON'))
    expect(await screen.findByRole('status')).toHaveTextContent('Copied')
  })

  it('reports a copy that failed', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    render(<UnreadableStateDialog isOpen hosts={hosts} onClose={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy details' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Could not copy: denied')
  })

  it('disables copying where the clipboard is not available', () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: undefined })
    render(<UnreadableStateDialog isOpen hosts={hosts} onClose={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Copy details' })).toBeDisabled()
  })
})
