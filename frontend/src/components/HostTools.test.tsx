import { act, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { HostTools } from './HostTools'

describe('HostTools', () => {
 it('renders each tool with its own words and host actions', async () => {
  const load = vi.fn().mockResolvedValue({ ok: true, launched: { status: 'missing' } })
  const install = vi.fn()
  const setAuto = vi.fn()
  await act(async () => {
   render(<HostTools host="build-box" tools={['One', 'Two'].map(title => ({ id: title, title, absent: (label: string) => `No ${title} on ${label}`, outdated: `Old ${title}`, installedNotice: `Trust ${title}`, autoUpdatedNotice: `Updated ${title}`, load, install, setAuto }))} />)
  })
  expect(screen.getByRole('group', { name: 'One' })).toBeInTheDocument()
  expect(screen.getByRole('group', { name: 'Two' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Install One on build-box' })).toBeInTheDocument()
  expect(load).toHaveBeenCalledTimes(2)
  expect(load).toHaveBeenCalledWith('build-box')
 })
})
