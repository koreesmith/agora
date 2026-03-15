import { useEffect, useRef, useCallback } from 'react'
import { useAuthStore } from '../store/auth'

export interface WSEvent {
  type: 'new_message' | 'message_edited' | 'message_deleted' | 'message_reaction' | 'read_receipt'
  conversation_id: string
  data: any
}

type Handler = (event: WSEvent) => void

const WS_URL = window.location.origin.replace(/^http/, 'ws') + '/api/ws'

export function useWebSocket(onEvent: Handler) {
  const { token } = useAuthStore()
  const wsRef     = useRef<WebSocket | null>(null)
  const retryRef  = useRef<ReturnType<typeof setTimeout> | null>(null)
  const delay     = useRef(1000)
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const connect = useCallback(() => {
    if (!token) return
    if (wsRef.current?.readyState === WebSocket.OPEN) return

    const ws = new WebSocket(`${WS_URL}?token=${token}`)
    wsRef.current = ws

    ws.onopen = () => { delay.current = 1000 }

    ws.onmessage = (e) => {
      try { onEventRef.current(JSON.parse(e.data)) } catch {}
    }

    ws.onclose = () => {
      wsRef.current = null
      retryRef.current = setTimeout(() => {
        delay.current = Math.min(delay.current * 2, 30000)
        connect()
      }, delay.current)
    }

    ws.onerror = () => ws.close()
  }, [token])

  useEffect(() => {
    connect()
    return () => {
      if (retryRef.current) clearTimeout(retryRef.current)
      wsRef.current?.close()
    }
  }, [connect])

  const send = useCallback((data: object) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data))
    }
  }, [])

  return { send }
}
