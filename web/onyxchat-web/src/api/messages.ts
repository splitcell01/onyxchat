import { api } from './client'
import type { Message, User } from '../types'

export async function fetchUsers(): Promise<User[]> {
  const data = await api.get<User[] | { users: User[] }>('/api/v1/users')
  return Array.isArray(data) ? data : data.users
}

export interface FetchMessagesResult {
  messages: Message[]
  hasMore: boolean
}

export async function fetchMessages(peerUsername: string, sinceId = 0): Promise<FetchMessagesResult> {
  const data = await api.get<{ messages: Message[]; hasMore: boolean }>(
    `/api/v1/messages?peer=${peerUsername}&sinceId=${sinceId}`
  )
  return { messages: data.messages ?? [], hasMore: data.hasMore ?? false }
}

export interface SendMessagePayload {
  recipientUsername: string
  body: string
  clientMessageId: string
  // E2E fields — omit for plaintext
  iv?: string
  encrypted?: boolean
}

export async function sendMessage(payload: SendMessagePayload): Promise<Message> {
  return api.post<Message>('/api/v1/messages', payload)
}