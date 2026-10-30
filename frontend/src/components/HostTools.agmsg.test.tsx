import { act, fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { HostTools } from './HostTools'
import { agmsgTool } from './agmsgTool'

it('shows the installed version and newer warning without offering a downgrade', async () => {
  const tool = agmsgTool()
  tool.load = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'newer', version: '1.3.0' } })
  await act(async () => { render(<HostTools host="build-box" tools={[tool]} />) })
  expect(screen.getByText(/1.3.0.*newer/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Update/ })).not.toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: /all hosts/ })).not.toBeChecked()
})

it('shows installer consent, failure details, and restart guidance after retry', async () => {
  const tool = agmsgTool()
  tool.load = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'missing' } })
  tool.install = vi.fn().mockResolvedValueOnce({ ok: false, error: 'exit status 1: sqlite3 missing' }).mockResolvedValueOnce({ ok: true, launched: { status: 'installed', version: '1.2.0' } })
  await act(async () => { render(<HostTools host="build-box" tools={[tool]} />) })
  expect(screen.getByText(/outside/)).toBeInTheDocument()
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Install agmsg on build-box' })) })
  expect(screen.getByRole('alert')).toHaveTextContent('exit status 1: sqlite3 missing')
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Install agmsg on build-box' })) })
  expect(screen.getByRole('status')).toHaveTextContent(/Restart/)
  expect(screen.getByText('Installed 1.2.0')).toBeInTheDocument()
})

it('saves one global automatic-update preference and displays failure with manual retry', async () => {
  const tool = agmsgTool()
  tool.load = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'outdated', auto_update: true, auto_error: 'exit status 7: recovery required' } })
  tool.setAuto = vi.fn().mockResolvedValue({ ok: true, launched: { auto_update: false } })
  tool.install = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'installed', version: '1.2.0', auto_update: false } })
  await act(async () => { render(<HostTools host="build-box" tools={[tool]} />) })
  expect(screen.getByRole('checkbox', { name: /Update automatically.*all hosts/ })).toBeChecked()
  expect(screen.getByRole('alert')).toHaveTextContent('exit status 7')
  await act(async () => { fireEvent.click(screen.getByRole('checkbox')) })
  expect(tool.setAuto).toHaveBeenCalledWith('build-box', false)
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Update agmsg on build-box' })) })
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent('Restart agents')
})

it('explains unattended installer consent even when the pinned version is installed', async () => {
  const tool = agmsgTool()
  tool.load = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'installed', version: '1.2.0', auto_update: true, auto_updated: true } })
  await act(async () => { render(<HostTools host="build-box" tools={[tool]} />) })
  expect(screen.getByRole('checkbox', { name: /all hosts/ })).toBeChecked()
  expect(screen.getByText(/without further prompts/)).toHaveTextContent('outside ~/.agents/skills')
  expect(screen.getByRole('status')).toHaveTextContent(/Updated automatically.*Restart agents/)
})

it('refreshes global work after enabling from an already installed host', async () => {
  const tool = agmsgTool()
  tool.load = vi.fn()
    .mockResolvedValueOnce({ ok: true, launched: { status: 'installed', auto_update: false } })
    .mockResolvedValueOnce({ ok: true, launched: { status: 'unknown', auto_update: true, error: 'Checking or updating automatically; reopen the menu for the result.' } })
  tool.setAuto = vi.fn().mockResolvedValue({ ok: true, launched: { auto_update: true } })
  await act(async () => { render(<HostTools host="build-box" tools={[tool]} />) })
  await act(async () => { fireEvent.click(screen.getByRole('checkbox')) })
  expect(tool.load).toHaveBeenCalledTimes(2)
  expect(screen.getByRole('checkbox', { name: /all hosts/ })).toBeChecked()
  expect(screen.getByText(/reopen the menu/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Install|Update/ })).not.toBeInTheDocument()
})

it('posts only the global enabled value and validates the saved preference', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ auto_update: true })))
  try {
    const result = await agmsgTool().setAuto!('build-box', true)
    expect(result).toEqual({ ok: true, launched: { auto_update: true } })
    expect(fetchMock).toHaveBeenCalledWith('/api/hosts/agmsg/auto-update', expect.objectContaining({
      method: 'POST', body: JSON.stringify({ enabled: true }),
    }))
  } finally { fetchMock.mockRestore() }
})
