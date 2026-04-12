import { useEffect, useRef, useCallback } from 'react'
import { api, getToken } from '../api/client'
import type { WSChatMessage, WSTyping, WSPresence, WSKeyChanged } from '../types'

type WSHandlers = {
  onMessage: (msg: WSChatMessage) => void
  onTyping: (msg: WSTyping) => void
  onPresence: (msg: WSPresence) => void
  onKeyChanged: (msg: WSKeyChanged) => void
}

async function fetchWSTicket(): Promise<string> {
  const data = await api.post<{ ticket: string }>('/api/v1/ws/ticket', {})
  return data.ticket
}

export function useWebSocket(handlers: WSHandlers, enabled = true) {
  const ws = useRef<WebSocket | null>(null)
  const reconnTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const handlersRef = useRef(handlers)
  const shouldReconnect = useRef(true)

  useEffect(() => {
    handlersRef.current = handlers
  }, [handlers])

  const connect = useCallback(async () => {
    // Don't attempt WS connection if we have no auth token
    if (!getToken()) return

    if (reconnTimer.current) {
      clearTimeout(reconnTimer.current)
      reconnTimer.current = null
    }

    if (ws.current) {
      ws.current.close()
      ws.current = null
    }

    let ticket: string
    try {
      ticket = await fetchWSTicket()
    } catch {
      if (shouldReconnect.current) {
        reconnTimer.current = setTimeout(connect, 3000)
      }
      return
    }

    const apiUrl = import.meta.env.VITE_API_URL
    if (!apiUrl) {
      console.error('VITE_API_URL is not set')
      return
    }

    const wsBase = apiUrl
      .replace(/^https:/, 'wss:')
      .replace(/^http:/, 'ws:')

    const socket = new WebSocket(
      `${wsBase}/api/v1/ws?ticket=${encodeURIComponent(ticket)}`
    )
    ws.current = socket

    socket.addEventListener('message', (e) => {
      try {
        const msg = JSON.parse(e.data)
        switch (msg.type) {
          case 'message':
            handlersRef.current.onMessage(msg)
            break
          case 'typing':
            handlersRef.current.onTyping(msg)
            break
          case 'presence':
            handlersRef.current.onPresence(msg)
            break
          case 'key_changed':
            handlersRef.current.onKeyChanged(msg)
            break
        }
      } catch {
        // ignore malformed message
      }
    })

    socket.addEventListener('close', () => {
      // Only act if this socket is still the active one — a newer connect()
      // call may have already replaced ws.current before this event fires.
      if (ws.current !== socket) return
      ws.current = null
      if (shouldReconnect.current && getToken()) {
        reconnTimer.current = setTimeout(connect, 3000)
      }
    })

    socket.addEventListener('error', () => {
      socket.close()
    })
  }, [])

  useEffect(() => {
    // Don't connect until the user is authenticated (enabled = !!user).
    // When enabled flips true (login), this effect re-runs and connects.
    // When enabled flips false (logout), cleanup closes the socket.
    if (!enabled) return

    shouldReconnect.current = true
    connect()

    return () => {
      shouldReconnect.current = false

      if (reconnTimer.current) {
        clearTimeout(reconnTimer.current)
        reconnTimer.current = null
      }

      if (ws.current) {
        ws.current.close()
        ws.current = null
      }
    }
  }, [connect, enabled])

  const send = useCallback((data: unknown) => {
    if (ws.current?.readyState === WebSocket.OPEN) {
      ws.current.send(JSON.stringify(data))
    }
  }, [])

  return { send }
}