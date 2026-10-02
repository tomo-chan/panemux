import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { loadTaskTerminalMaximized, saveTaskTerminalMaximized } from './taskTerminalPrefs'

const KEY = 'panemux:task-terminal-maximized'

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
})

describe('task terminal maximized preference', () => {
  it('is off until it is saved', () => {
    expect(loadTaskTerminalMaximized()).toBe(false)
  })

  it('remembers on and off across loads', () => {
    saveTaskTerminalMaximized(true)
    expect(window.localStorage.getItem(KEY)).toBe('true')
    expect(loadTaskTerminalMaximized()).toBe(true)

    saveTaskTerminalMaximized(false)
    expect(loadTaskTerminalMaximized()).toBe(false)
  })

  it('reads anything but "true" as off', () => {
    window.localStorage.setItem(KEY, 'yes')
    expect(loadTaskTerminalMaximized()).toBe(false)
  })

  it('is off, and saving does not throw, when storage cannot be reached', () => {
    useStorage(null, true)
    expect(() => saveTaskTerminalMaximized(true)).not.toThrow()
    expect(loadTaskTerminalMaximized()).toBe(false)
  })

  it('is off, and saving does not throw, when storage refuses the write', () => {
    const throwing = {
      getItem: () => { throw new Error('denied') },
      setItem: () => { throw new Error('quota') },
    } as unknown as Storage
    useStorage(throwing)
    expect(() => saveTaskTerminalMaximized(true)).not.toThrow()
    expect(loadTaskTerminalMaximized()).toBe(false)
  })
})
