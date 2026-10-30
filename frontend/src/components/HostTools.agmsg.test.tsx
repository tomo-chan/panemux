import { act, fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { HostTools } from './HostTools'
import { agmsgTool } from './agmsgTool'

it('shows the installed version and newer warning without automatic updating or downgrade', async () => {
  const tool = agmsgTool()
  tool.load = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'newer', version: '1.3.0' } })
  await act(async () => { render(<HostTools host="build-box" tools={[tool]} />) })
  expect(screen.getByText(/1.3.0.*newer/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Update/ })).not.toBeInTheDocument()
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
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
