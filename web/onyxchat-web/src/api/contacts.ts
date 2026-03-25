// web/onyxchat-web/src/api/contacts.ts

import { api } from './client'
import type { Contact } from '../types'

// GET /api/v1/contacts
export async function fetchContacts(): Promise<Contact[]> {
  const data = await api.get<Contact[]>('/api/v1/contacts')
  return Array.isArray(data) ? data : []
}

// POST /api/v1/contacts
export async function addContact(username: string): Promise<void> {
  await api.post('/api/v1/contacts', { username })
}

// DELETE /api/v1/contacts/:username
export async function removeContact(username: string): Promise<void> {
  await api.delete(`/api/v1/contacts/${username}`)
}

// DELETE /api/v1/account  — GDPR right to erasure
// Requires password confirmation
export async function deleteAccount(password: string): Promise<void> {
  await api.delete('/api/v1/account', { password })
}