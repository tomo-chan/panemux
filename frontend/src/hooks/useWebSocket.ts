import { useCallback, useEffect, useRef, useState } from 'react'
import { WSControlMessageSchema } from '../schemas'

type MessageHandler = (data: ArrayBuffer | string, isBinary: boolean) => void

interface UseWebSocketOptions {
  onMessage: MessageHandler
  onOpen?: () => void
  onClose?: () => void
  reconnectDelay?: number
  maxReconnectAttempts?: number
}

export function useWebSocket(url: string, options: UseWebSocketOptions) {
  const { reconnectDelay = 2000, maxReconnectAttempts = 10 } = options

  const wsRef = useRef<WebSocket | null>(null)
  const attemptsRef = useRef(0)
  const mountedRef = useRef(true)
  const [connected, setConnected] = useState(false)

  // Store callbacks in refs so connect() doesn't need them as deps
  // and won't recreate/reconnect on every render
  const onMessageRef = useRef(options.onMessage)
  const onOpenRef = useRef(options.onOpen)
  const onCloseRef = useRef(options.onClose)
  useEffect(() => { onMessageRef.current = options.onMessage })
  useEffect(() => { onOpenRef.current = options.onOpen })
  useEffect(() => { onCloseRef.current = options.onClose })

  const connect = useCallback(() => {
    if (!mountedRef.current) return
    if (attemptsRef.current >= maxReconnectAttempts) return

    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      attemptsRef.current = 0
      setConnected(true)
      onOpenRef.current?.()
    }

    ws.onmessage = (event) => {
      const isBinary = event.data instanceof ArrayBuffer
      if (!isBinary) {
        // Validate text frames before passing to handler
        try {
          const parsed = WSControlMessageSchema.safeParse(JSON.parse(event.data as string))
          if (!parsed.success) return
        } catch {
          return
        }
      }
      onMessageRef.current(event.data, isBinary)
    }

    ws.onclose = () => {
      setConnected(false)
      onCloseRef.current?.()
      if (mountedRef.current) {
        attemptsRef.current++
        setTimeout(connect, reconnectDelay)
      }
    }

    ws.onerror = () => {
      ws.close()
    }
  }, [url, reconnectDelay, maxReconnectAttempts]) // callbacks excluded via refs

  useEffect(() => {
    mountedRef.current = true
    connect()
    return () => {
      mountedRef.current = false
      wsRef.current?.close()
    }
  }, [connect])

  const send = useCallback((data: string | ArrayBuffer | Uint8Array) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(data)
    }
  }, [])

  return { send, connected }
}
