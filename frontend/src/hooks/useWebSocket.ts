import { useCallback, useEffect, useRef, useState } from 'react'
import { WSControlMessageSchema } from '../schemas'

type MessageHandler = (data: ArrayBuffer | string, isBinary: boolean) => void

interface UseWebSocketOptions {
  onMessage: MessageHandler
  onOpen?: () => void
  onClose?: () => void
  reconnectDelay?: number
  maxReconnectDelay?: number
  maxReconnectAttempts?: number
}

export function useWebSocket(url: string, options: UseWebSocketOptions) {
  const { reconnectDelay = 2000, maxReconnectDelay = 30000, maxReconnectAttempts = 10 } = options

  const wsRef = useRef<WebSocket | null>(null)
  const attemptsRef = useRef(0)
  const mountedRef = useRef(true)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [connected, setConnected] = useState(false)
  // Set once the reconnect budget is exhausted so callers can stop assuming
  // automatic recovery will happen and surface a manual retry affordance.
  const [exhausted, setExhausted] = useState(false)

  // Store callbacks in refs so connect() doesn't need them as deps
  // and won't recreate/reconnect on every render
  const onMessageRef = useRef(options.onMessage)
  const onOpenRef = useRef(options.onOpen)
  const onCloseRef = useRef(options.onClose)
  useEffect(() => { onMessageRef.current = options.onMessage })
  useEffect(() => { onOpenRef.current = options.onOpen })
  useEffect(() => { onCloseRef.current = options.onClose })

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
  }, [])

  const connect = useCallback(() => {
    if (!mountedRef.current) return
    if (attemptsRef.current >= maxReconnectAttempts) {
      setExhausted(true)
      return
    }

    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      clearReconnectTimer()
      attemptsRef.current = 0
      setConnected(true)
      onOpenRef.current?.()
    }

    ws.onmessage = (event) => {
      const isBinary = isArrayBuffer(event.data)
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
        // Exponential backoff (base delay doubles each attempt), capped at
        // maxReconnectDelay so a long outage doesn't grow the wait unbounded.
        const delay = Math.min(reconnectDelay * 2 ** attemptsRef.current, maxReconnectDelay)
        attemptsRef.current++
        clearReconnectTimer()
        reconnectTimerRef.current = setTimeout(() => {
          reconnectTimerRef.current = null
          connect()
        }, delay)
      }
    }

    ws.onerror = () => {
      ws.close()
    }
    // callbacks excluded via refs
  }, [url, reconnectDelay, maxReconnectDelay, maxReconnectAttempts, clearReconnectTimer])

  useEffect(() => {
    mountedRef.current = true
    connect()
    return () => {
      mountedRef.current = false
      clearReconnectTimer()
      wsRef.current?.close()
    }
  }, [connect, clearReconnectTimer])

  const send = useCallback((data: string | ArrayBuffer | Uint8Array) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(data)
    }
  }, [])

  // Imperatively reconnect: detach the current socket (without triggering the
  // onclose reconnect loop), reset the attempt counter, then connect fresh.
  const reconnect = useCallback(() => {
    clearReconnectTimer()
    const ws = wsRef.current
    if (ws) {
      ws.onclose = null
      ws.onerror = null
      ws.close()
      wsRef.current = null
      setConnected(false)
    }
    attemptsRef.current = 0
    setExhausted(false)
    connect()
  }, [connect, clearReconnectTimer])

  return { send, connected, exhausted, reconnect }
}

function isArrayBuffer(value: unknown): value is ArrayBuffer {
  // Vitest can deliver ArrayBuffer values from a different JS realm.
  return value instanceof ArrayBuffer || Object.prototype.toString.call(value) === '[object ArrayBuffer]'
}
