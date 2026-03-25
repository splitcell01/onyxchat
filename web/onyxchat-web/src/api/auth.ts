import { api, setToken } from './client'
import { getOrCreateKeyPair, exportPublicKey, clearKeyPair } from '../lib/crypto'
import { uploadPublicKey } from './keys'
import type { AuthResponse } from '../types'

async function publishKey(): Promise<void> {
  try {
    const kp     = await getOrCreateKeyPair()
    const pubKey = await exportPublicKey(kp)
    await uploadPublicKey(pubKey)
  } catch (err) {
    console.warn('[E2E] Could not upload public key:', err)
  }
}

export async function login(username: string, password: string): Promise<AuthResponse> {
  const data = await api.post<AuthResponse>('/api/v1/login', { username, password })
  setToken(data.token)
  await publishKey()
  return data
}

export async function register(username: string, password: string, inviteCode: string) {
  const data = await api.post<AuthResponse>('/api/v1/register', { username, password, invite_code: inviteCode })
  setToken(data.token)
  await publishKey()
  return data
}

export async function logout(): Promise<void> {
  setToken(null)
  sessionStorage.removeItem('user')
  await clearKeyPair()
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  await api.patch('/api/v1/users/me/password', {
    current_password: currentPassword,
    new_password: newPassword,
  })
}
