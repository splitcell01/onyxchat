// types/index.ts — replaces existing file
// Added: iv, encrypted to Message; E2E key API types

export interface User {
  id: number
  username: string
}

export interface Message {
  id: number
  senderId: number
  recipientId: number
  body: string
  createdAt: string
  // E2E fields — present when encrypted === true
  iv?: string
  encrypted: boolean
  // UI-only: true if send failed (never persisted to server)
  failed?: boolean
}

export interface Contact extends User {
  online: boolean
  lastMsg?: Message
}

export interface AuthResponse {
  id: number
  username: string
  token: string
  refresh_token: string
}

// WebSocket message types
export interface WSMessage {
  type: 'message' | 'typing' | 'presence' | 'key_changed'
}

export interface WSChatMessage extends WSMessage {
  type: 'message'
  message: Message
}

export interface WSTyping extends WSMessage {
  type: 'typing'
  from: string
  to: string
  isTyping: boolean
}

export interface WSPresence extends WSMessage {
  type: 'presence'
  userId: number
  username: string
  status: 'online' | 'offline'
}

export interface WSKeyChanged extends WSMessage {
  type: 'key_changed'
  username: string
}

// E2E key API
export interface GetKeyResponse {
  username: string
  publicKey: string
}