import { renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useBrowserNotificationPermission } from './useBrowserNotificationPermission'

describe('useBrowserNotificationPermission', () => {
  let originalNotification: typeof Notification | undefined

  beforeEach(() => {
    originalNotification = window.Notification
    vi.stubGlobal('Notification', vi.fn() as unknown as typeof Notification)
    Object.defineProperty(window.Notification, 'permission', {
      configurable: true,
      value: 'default',
    })
    Object.defineProperty(window.Notification, 'requestPermission', {
      configurable: true,
      value: vi.fn().mockResolvedValue('granted'),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    if (originalNotification === undefined) {
      // @ts-expect-error test cleanup
      delete window.Notification
    } else {
      window.Notification = originalNotification
    }
  })

  it('requests permission on first pointer interaction when permission is default', () => {
    renderHook(() => useBrowserNotificationPermission())

    window.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    window.dispatchEvent(new Event('pointerdown', { bubbles: true }))

    expect(window.Notification.requestPermission).toHaveBeenCalledTimes(1)
  })

  it('requests permission on first key interaction when permission is default', () => {
    renderHook(() => useBrowserNotificationPermission())

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))

    expect(window.Notification.requestPermission).toHaveBeenCalledTimes(1)
  })

  it('does not register permission handlers when permission is already granted', () => {
    Object.defineProperty(window.Notification, 'permission', {
      configurable: true,
      value: 'granted',
    })
    renderHook(() => useBrowserNotificationPermission())

    window.dispatchEvent(new Event('pointerdown', { bubbles: true }))

    expect(window.Notification.requestPermission).not.toHaveBeenCalled()
  })

  it('removes permission handlers on unmount', () => {
    const { unmount } = renderHook(() => useBrowserNotificationPermission())

    unmount()
    window.dispatchEvent(new Event('pointerdown', { bubbles: true }))

    expect(window.Notification.requestPermission).not.toHaveBeenCalled()
  })
})
