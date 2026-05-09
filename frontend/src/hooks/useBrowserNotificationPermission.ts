import { useEffect } from 'react'

export function useBrowserNotificationPermission() {
  useEffect(() => {
    if (!('Notification' in window)) return
    if (Notification.permission !== 'default') return

    const requestPermission = () => {
      void Notification.requestPermission()
      window.removeEventListener('pointerdown', requestPermission, true)
      window.removeEventListener('keydown', requestPermission, true)
    }

    window.addEventListener('pointerdown', requestPermission, true)
    window.addEventListener('keydown', requestPermission, true)

    return () => {
      window.removeEventListener('pointerdown', requestPermission, true)
      window.removeEventListener('keydown', requestPermission, true)
    }
  }, [])
}
