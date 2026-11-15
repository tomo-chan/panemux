import { describe, it, expect } from 'vitest'
import { BoardStatusResponseSchema } from './index'

describe('board identity contract', () => {
  const entry = {
    host: 'local', team: 'panemux', name: 'worker', pane_id: null,
    source: 'unreported', state: null, waiting_for: null, status_since: null,
    task_observed_at: null, last_message_at: '2026-11-15T02:00:00Z', self_report: null,
  }
  it('requires identities even for an empty board', () => {
    expect(BoardStatusResponseSchema.safeParse({ statuses: {}, participation: {} }).success).toBe(false)
    expect(BoardStatusResponseSchema.safeParse({ statuses: {}, participation: {}, identities: {} }).success).toBe(true)
  })
  it('preserves nullable fields and separates equal names on different hosts', () => {
    const identities = {
      'worker@local': entry,
      'worker@ssh:build': { ...entry, host: 'ssh:build', source: 'self_report', state: 'something-new',
        self_report: { updated_at: '2026-11-15T02:00:00Z', summary: 'hello' } },
    }
    expect(BoardStatusResponseSchema.parse({ statuses: {}, participation: {}, identities }).identities).toEqual(identities)
  })
  it.each([
    { source: 'invented' }, { pane_id: 1 }, { status_since: 1 }, { self_report: {} },
  ])('rejects an invalid identity field %o', (patch) => {
    expect(BoardStatusResponseSchema.safeParse({ statuses: {}, participation: {}, identities: { 'worker@local': { ...entry, ...patch } } }).success).toBe(false)
  })
})
