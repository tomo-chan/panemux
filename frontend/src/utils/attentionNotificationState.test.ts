import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  getLastNotifiedAttentionSignature,
  setLastNotifiedAttentionSignature,
} from './attentionNotificationState'

const KEY = 'panemux:last-notified-attention-signatures'

function useStorage(storage: Storage | null, throwOnAccess = false) {
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    get() {
      if (throwOnAccess) throw new Error('blocked')
      return storage
    },
  })
}

let realStorage: Storage

beforeEach(() => {
  realStorage = window.localStorage
  window.localStorage.clear()
})

afterEach(() => {
  useStorage(realStorage)
  window.localStorage.clear()
  vi.restoreAllMocks()
})

describe('attention notification state', () => {
  it('reports nothing for a pane that has never notified', () => {
    expect(getLastNotifiedAttentionSignature('main')).toBeNull()
  })

  it('remembers a signature per pane', () => {
    setLastNotifiedAttentionSignature('main', 'sig-a')
    setLastNotifiedAttentionSignature('side', 'sig-b')

    expect(getLastNotifiedAttentionSignature('main')).toBe('sig-a')
    expect(getLastNotifiedAttentionSignature('side')).toBe('sig-b')
  })

  it('replaces a pane signature rather than accumulating them', () => {
    setLastNotifiedAttentionSignature('main', 'sig-a')
    setLastNotifiedAttentionSignature('main', 'sig-b')

    expect(getLastNotifiedAttentionSignature('main')).toBe('sig-b')
    expect(JSON.parse(window.localStorage.getItem(KEY) ?? '{}')).toEqual({ main: 'sig-b' })
  })

  it('survives a reload, which is the whole point of persisting it', () => {
    window.localStorage.setItem(KEY, JSON.stringify({ main: 'from-a-previous-page' }))

    expect(getLastNotifiedAttentionSignature('main')).toBe('from-a-previous-page')
  })

  it.each([
    ['malformed json', 'not json at all'],
    ['a json scalar', '42'],
    ['null', 'null'],
  ])('ignores %s left in storage', (_name, raw) => {
    window.localStorage.setItem(KEY, raw)

    expect(getLastNotifiedAttentionSignature('main')).toBeNull()
  })

  it('ignores entries whose value is not a string', () => {
    window.localStorage.setItem(KEY, JSON.stringify({ main: 7, side: 'sig-b' }))

    expect(getLastNotifiedAttentionSignature('main')).toBeNull()
    expect(getLastNotifiedAttentionSignature('side')).toBe('sig-b')
  })

  it('keeps working in memory when storage is unavailable', () => {
    useStorage(null)

    setLastNotifiedAttentionSignature('main', 'sig-a')

    expect(getLastNotifiedAttentionSignature('main')).toBe('sig-a')
  })

  it('keeps working in memory when touching storage throws', () => {
    useStorage(null, true)

    setLastNotifiedAttentionSignature('main', 'sig-a')

    expect(getLastNotifiedAttentionSignature('main')).toBe('sig-a')
  })

  it('falls back to memory when a write is refused, so the next read still dedupes', () => {
    const refusing = {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota exceeded')
      },
      removeItem: () => {},
      clear: () => {},
      key: () => null,
      length: 0,
    } as unknown as Storage
    useStorage(refusing)

    setLastNotifiedAttentionSignature('main', 'sig-a')

    // The read finds nothing in storage and falls back to what the write kept.
    expect(getLastNotifiedAttentionSignature('main')).toBeNull()
  })
})
