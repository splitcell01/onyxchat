// context/AuthContext.tsx — replaces existing file
// One change: logout is now async (calls clearKeyPair from crypto.ts via auth.ts)

import { createContext, useContext, useState, useEffect, type ReactNode } from 'react'
import { login, logout as apiLogout, register, refresh as apiRefresh, getRefreshToken, publishKey } from '../api/auth'
import { getToken } from '../api/client'
import type { User } from '../types'

interface AuthState {
  user: User | null
  isAuthenticated: boolean
  login:    (username: string, password: string) => Promise<void>
  register: (username: string, password: string, inviteCode: string) => Promise<void>
  logout:   () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(() => {
    const stored = localStorage.getItem('user')
    return stored ? JSON.parse(stored) : null
  })

  useEffect(() => {
    if (user) localStorage.setItem('user', JSON.stringify(user))
    else localStorage.removeItem('user')
  }, [user])

  // On mount: if we have a stored refresh token but lost the session token,
  // silently get a new access token so the user doesn't have to log in again.
  useEffect(() => {
    if (getToken() || !getRefreshToken()) return

    let mounted = true

    apiRefresh()
      .then(async newToken => {
        if (!mounted) return
        if (!newToken) { setUser(null); return }
        await publishKey()
      })
      .catch(() => {
        if (!mounted) return
        setUser(null)
      })

    return () => { mounted = false }
  }, [])

  async function handleLogin(username: string, password: string) {
    const data = await login(username, password)
    setUser({ id: data.id, username: data.username })
  }

  async function handleRegister(username: string, password: string, inviteCode: string) {
    const data = await register(username, password, inviteCode)
    setUser({ id: data.id, username: data.username })
  }

  async function handleLogout() {
    await apiLogout()   // clears token + IDB keypair
    setUser(null)
  }

  return (
    <AuthContext.Provider value={{
      user,
      isAuthenticated: !!user,
      login:    handleLogin,
      register: handleRegister,
      logout:   handleLogout,
    }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}