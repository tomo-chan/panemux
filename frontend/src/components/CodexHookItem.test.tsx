import { describe, it, expect, vi } from 'vitest'
import { act, cleanup, render, screen, fireEvent } from '@testing-library/react'
import { CodexHookItem } from './CodexHookItem'
import type { CodexHookItemProps } from './CodexHookItem'
import type { CodexHook } from '../schemas'
import type { TaskActionResult } from '../hooks/useTasks'

function props(hook: CodexHook, overrides: Partial<CodexHookItemProps> = {}): CodexHookItemProps {
  return {
    host: 'gpu-box',
    load: vi.fn().mockResolvedValue({ ok: true, launched: hook }),
    install: vi.fn().mockResolvedValue({ ok: true, launched: { status: 'installed' } }),
    setAuto: vi.fn().mockResolvedValue({ ok: true, launched: { auto_update: true } }),
    ...overrides,
  }
}

async function renderItem(p: CodexHookItemProps, alone = false) {
  await act(async () => {
    render(<CodexHookItem {...p} alone={alone} />)
  })
}

const checkbox = () => screen.getByRole('checkbox', { name: 'Update automatically' })

describe('CodexHookItem updating by itself (issue #402)', () => {
  it('turns it on and off, starting from what the host has', async () => {
    const setAuto = vi.fn()
      .mockResolvedValueOnce({ ok: true, launched: { auto_update: true } })
      .mockResolvedValueOnce({ ok: true, launched: { auto_update: false } })
    await renderItem(props({ status: 'installed', auto_update: false }, { setAuto }))
    expect(checkbox()).not.toBeChecked()

    await act(async () => fireEvent.click(checkbox()))
    expect(setAuto).toHaveBeenLastCalledWith('gpu-box', true)
    expect(checkbox()).toBeChecked()
    await act(async () => fireEvent.click(checkbox()))
    expect(setAuto).toHaveBeenLastCalledWith('gpu-box', false)
    expect(checkbox()).not.toBeChecked()
  })

  it('says why the setting was not saved, and keeps it as it was', async () => {
    let answer!: (value: TaskActionResult<{ auto_update: boolean }>) => void
    const setAuto = vi.fn(() => new Promise<TaskActionResult<{ auto_update: boolean }>>((r) => { answer = r }))
    await renderItem(props({ status: 'outdated', auto_update: false }, { setAuto }))

    fireEvent.click(checkbox())
    expect(checkbox()).toBeDisabled()
    await act(async () => answer({ ok: false, error: 'parsing codex hook settings file: unexpected end' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Could not save: parsing codex hook settings file: unexpected end')
    expect(checkbox()).not.toBeChecked()
    expect(checkbox()).toBeEnabled()
  })

  it('says the hook was updated by itself, and to trust it', async () => {
    await renderItem(props({ status: 'installed', auto_update: true, auto_updated: true }))
    expect(checkbox()).toBeChecked()
    expect(screen.getByRole('status')).toHaveTextContent("Updated automatically. Trust it in codex's /hooks")
  })

  it('says why updating by itself failed, keeps Update, and clears the reason once it is pressed', async () => {
    await renderItem(props({ status: 'outdated', auto_update: true, auto_error: 'install the codex hook on gpu-box: Permission denied' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Could not update automatically: install the codex hook on gpu-box: Permission denied')
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Update Codex input notifications on gpu-box' }))
    })
    expect(screen.queryByText(/Could not update automatically/)).toBeNull()
  })

  it('is offered only on a host whose hook can be installed', async () => {
    const offered: string[] = []
    for (const status of ['missing', 'outdated', 'installed', 'absent', 'unknown', 'invalid'] as const) {
      await act(async () => {
        render(<CodexHookItem {...props({ status })} alone />)
      })
      if (screen.queryByRole('checkbox')) offered.push(status)
      cleanup()
    }
    expect(offered).toEqual(['missing', 'outdated', 'installed'])
  })
})

describe('CodexHookItem (issue #396)', () => {
  it.each([
    ['missing', 'Install'],
    ['outdated', 'Update'],
  ] as const)('offers %s hosts the %s button, and says to trust the hook after it', async (status, label) => {
    const p = props({ status })
    await renderItem(p)

    expect(p.load).toHaveBeenCalledWith('gpu-box')
    const section = screen.getByRole('group', { name: 'Codex input notifications' })
    expect(section).toBeInTheDocument()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: `${label} Codex input notifications on gpu-box` }))
    })

    expect(p.install).toHaveBeenCalledWith('gpu-box')
    expect(screen.queryByRole('button', { name: /Codex input notifications on/ })).toBeNull()
    expect(section).toHaveTextContent('Installed')
    expect(screen.getByRole('status')).toHaveTextContent("Trust it in codex's /hooks")
  })

  it('shows an installed hook with no button', async () => {
    await renderItem(props({ status: 'installed' }))
    expect(screen.getByRole('group', { name: 'Codex input notifications' })).toHaveTextContent('Installed')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows nothing for a host without codex, unless it is all the menu has', async () => {
    await renderItem(props({ status: 'absent' }))
    expect(screen.queryByRole('group', { name: 'Codex input notifications' })).toBeNull()
  })

  it('says there is no codex when it is all the menu has', async () => {
    await renderItem(props({ status: 'absent' }), true)
    expect(screen.getByRole('group', { name: 'Codex input notifications' })).toHaveTextContent('No Codex found on gpu-box')
  })

  it('shows a host it could not check as not checked, never as without codex', async () => {
    await renderItem(props({ status: 'unknown', error: 'run codex hook check on gpu-box: exit status 255' }))
    const section = screen.getByRole('group', { name: 'Codex input notifications' })
    expect(section).toHaveTextContent('Not checked: run codex hook check on gpu-box: exit status 255')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows a request that failed as not checked', async () => {
    await renderItem(props({ status: 'installed' }, { load: vi.fn().mockResolvedValue({ ok: false, error: 'HTTP 500' }) }))
    expect(screen.getByRole('group', { name: 'Codex input notifications' })).toHaveTextContent('Not checked: HTTP 500')
  })

  it('says why a hooks.json is left alone, with no button', async () => {
    await renderItem(props({ status: 'invalid', error: '~/.codex/hooks.json is not valid JSON' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Cannot install: ~/.codex/hooks.json is not valid JSON')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('says why an install failed, and keeps the button for another try', async () => {
    let answer!: (value: TaskActionResult<CodexHook>) => void
    const install = vi.fn(() => new Promise<TaskActionResult<CodexHook>>((r) => { answer = r }))
    await renderItem(props({ status: 'missing' }, { install }))

    const button = screen.getByRole('button', { name: 'Install Codex input notifications on gpu-box' })
    fireEvent.click(button)
    expect(button).toBeDisabled()
    expect(button).toHaveTextContent('Installing…')
    await act(async () => answer({ ok: false, error: 'run codex hook install on gpu-box: exit status 1' }))

    expect(screen.getByRole('alert')).toHaveTextContent('Could not install: run codex hook install on gpu-box: exit status 1')
    expect(button).toBeEnabled()
    expect(screen.queryByRole('status')).toBeNull()
  })
})
