import { describe, expect, it } from 'vitest'
import { colorForBoardState, formatRelativeTime, isStaleUpdatedAt } from './boardStatusColors'

describe('colorForBoardState', () => {
  it('maps working to the connected green', () => {
    expect(colorForBoardState('working')).toBe('#7bd88f')
  })

  it('maps idle to blue', () => {
    expect(colorForBoardState('idle')).toBe('#7aa2f7')
  })

  it('maps waiting to attention gold', () => {
    expect(colorForBoardState('waiting')).toBe('#f4bf4f')
  })

  it('maps an unrecognized state string to a neutral gray without throwing', () => {
    expect(colorForBoardState('something-new')).toBe('#4b5565')
  })

  it('maps an empty/undefined state to the same neutral gray', () => {
    expect(colorForBoardState(undefined)).toBe('#4b5565')
    expect(colorForBoardState('')).toBe('#4b5565')
  })
})

describe('isStaleUpdatedAt', () => {
  const now = new Date('2026-08-14T12:10:00Z')

  it('is not stale just under the 5 minute threshold', () => {
    expect(isStaleUpdatedAt('2026-08-14T12:05:01Z', now)).toBe(false)
  })

  it('is stale just over the 5 minute threshold', () => {
    expect(isStaleUpdatedAt('2026-08-14T12:04:59Z', now)).toBe(true)
  })

  it('is not stale for an unparsable timestamp', () => {
    expect(isStaleUpdatedAt('not-a-date', now)).toBe(false)
  })
})

describe('formatRelativeTime', () => {
  const now = new Date('2026-08-14T12:10:00Z')

  it('formats seconds ago', () => {
    expect(formatRelativeTime('2026-08-14T12:09:48Z', now)).toBe('12s ago')
  })

  it('formats minutes ago', () => {
    expect(formatRelativeTime('2026-08-14T12:05:00Z', now)).toBe('5m ago')
  })

  it('formats hours ago', () => {
    expect(formatRelativeTime('2026-08-14T09:10:00Z', now)).toBe('3h ago')
  })

  it('formats days ago', () => {
    expect(formatRelativeTime('2026-08-10T12:10:00Z', now)).toBe('4d ago')
  })

  it('falls back to the raw string for an unparsable timestamp', () => {
    expect(formatRelativeTime('not-a-date', now)).toBe('not-a-date')
  })
})
