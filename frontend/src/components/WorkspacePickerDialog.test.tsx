import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { WorkspacePickerDialog } from './WorkspacePickerDialog'

const items = [{ id: 'dev', title: 'Dev' }, { id: 'ops', title: 'Ops' }]
function setup(spaces = items) {
  const onOpen = vi.fn().mockResolvedValue(undefined)
  const onCancel = vi.fn()
  render(<WorkspacePickerDialog workspaces={spaces} active="ops" onOpen={onOpen} onCancel={onCancel} />)
  return { onOpen, onCancel }
}

describe('WorkspacePickerDialog', () => {
  it('defaults to the active workspace and opens the chosen workspace', async () => {
    const { onOpen } = setup()
    expect(screen.getByRole('radio', { name: 'Ops' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'Ops' })).toHaveFocus()
    fireEvent.click(screen.getByRole('radio', { name: 'Dev' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open here' }))
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith({ workspaceId: 'dev' }))
  })
  it('still asks when there is only one workspace', () => {
    setup([items[1]])
    expect(screen.getByRole('dialog', { name: 'Open in workspace' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Ops' })).toBeChecked()
  })
  it.each(['Cancel', 'Close', 'Escape'])('cancels with %s without opening a pane', (action) => {
    const { onOpen, onCancel } = setup()
    if (action === 'Escape') fireEvent.keyDown(window, { key: 'Escape' })
    else fireEvent.click(screen.getByRole('button', { name: action }))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onOpen).not.toHaveBeenCalled()
  })
  it.each(['', '   ', 'Dev', ' Dev '])('rejects invalid new name %j', (name) => {
    const { onOpen } = setup()
    fireEvent.click(screen.getByRole('radio', { name: /New workspace/ }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Workspace name' }), { target: { value: name } })
    fireEvent.click(screen.getByRole('button', { name: 'Create and open' }))
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(onOpen).not.toHaveBeenCalled()
  })
  it('keeps focus inside the dialog', () => {
    setup()
    const close = screen.getByRole('button', { name: 'Close' })
    const confirm = screen.getByRole('button', { name: 'Open here' })
    confirm.focus()
    fireEvent.keyDown(window, { key: 'Tab' })
    expect(close).toHaveFocus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(confirm).toHaveFocus()
  })
  it('reports when the selected workspace disappears while choosing', () => {
    const onOpen = vi.fn()
    const { rerender } = render(<WorkspacePickerDialog workspaces={items} active="ops" onOpen={onOpen} onCancel={() => {}} />)
    rerender(<WorkspacePickerDialog workspaces={[items[0]]} active="dev" onOpen={onOpen} onCancel={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open here' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Workspace no longer exists')
    expect(onOpen).not.toHaveBeenCalled()
  })
  it('trims a new name and reports asynchronous errors', async () => {
    const { onOpen } = setup()
    onOpen.mockRejectedValueOnce(new Error('HTTP 500'))
    fireEvent.click(screen.getByRole('radio', { name: /New workspace/ }))
    const input = screen.getByRole('textbox', { name: 'Workspace name' })
    expect(input).toHaveFocus()
    fireEvent.change(input, { target: { value: ' Review ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create and open' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('HTTP 500')
    expect(onOpen).toHaveBeenCalledWith({ newName: 'Review' })
  })
})
