// @vitest-environment jsdom

import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest'
import { render, screen, act, cleanup } from '@testing-library/react'
import { AuthProvider, useAuth } from './AuthContext'

vi.mock('../api/auth', () => ({
  refresh:         vi.fn(),
  getRefreshToken: vi.fn(),
  publishKey:      vi.fn(),
  login:           vi.fn(),
  logout:          vi.fn(),
  register:        vi.fn(),
}))
vi.mock('../api/client', () => ({
  getToken:  vi.fn(),
  setToken:  vi.fn(),
  api:       { post: vi.fn() },
}))

import * as authApi   from '../api/auth'
import * as clientApi from '../api/client'

function Probe() {
  const { user } = useAuth()
  return <div data-testid="probe">{user?.username ?? 'logged-out'}</div>
}

describe('AuthContext – mount-time session refresh', () => {
  afterEach(() => cleanup())

  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('calls publishKey and keeps user when refresh succeeds', async () => {
    localStorage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
    vi.mocked(clientApi.getToken).mockReturnValue(null)
    vi.mocked(authApi.getRefreshToken).mockReturnValue('rt')
    vi.mocked(authApi.refresh).mockResolvedValue('new-token')
    vi.mocked(authApi.publishKey).mockResolvedValue(undefined)

    await act(async () => {
      render(<AuthProvider><Probe /></AuthProvider>)
    })

    expect(authApi.refresh).toHaveBeenCalledOnce()
    expect(authApi.publishKey).toHaveBeenCalledOnce()
    expect(screen.getByTestId('probe').textContent).toBe('alice')
  })

  it('clears user when refresh returns null (token expired)', async () => {
    localStorage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
    vi.mocked(clientApi.getToken).mockReturnValue(null)
    vi.mocked(authApi.getRefreshToken).mockReturnValue('rt')
    vi.mocked(authApi.refresh).mockResolvedValue(null)

    await act(async () => {
      render(<AuthProvider><Probe /></AuthProvider>)
    })

    expect(screen.getByTestId('probe').textContent).toBe('logged-out')
    expect(authApi.publishKey).not.toHaveBeenCalled()
  })

  it('clears user and does not call publishKey when refresh throws', async () => {
    localStorage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
    vi.mocked(clientApi.getToken).mockReturnValue(null)
    vi.mocked(authApi.getRefreshToken).mockReturnValue('rt')
    vi.mocked(authApi.refresh).mockRejectedValue(new Error('network error'))

    await act(async () => {
      render(<AuthProvider><Probe /></AuthProvider>)
    })

    expect(screen.getByTestId('probe').textContent).toBe('logged-out')
    expect(authApi.publishKey).not.toHaveBeenCalled()
  })

  it('skips refresh when an access token is already in memory', async () => {
    vi.mocked(clientApi.getToken).mockReturnValue('still-valid-token')
    vi.mocked(authApi.getRefreshToken).mockReturnValue('rt')

    await act(async () => {
      render(<AuthProvider><Probe /></AuthProvider>)
    })

    expect(authApi.refresh).not.toHaveBeenCalled()
  })

  it('skips refresh when no refresh token is stored', async () => {
    vi.mocked(clientApi.getToken).mockReturnValue(null)
    vi.mocked(authApi.getRefreshToken).mockReturnValue(null)

    await act(async () => {
      render(<AuthProvider><Probe /></AuthProvider>)
    })

    expect(authApi.refresh).not.toHaveBeenCalled()
  })

  it('does not call setState after unmount (mounted guard)', async () => {
    localStorage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))
    vi.mocked(clientApi.getToken).mockReturnValue(null)
    vi.mocked(authApi.getRefreshToken).mockReturnValue('rt')

    // Refresh won't resolve until we say so
    let resolveRefresh!: (token: string) => void
    vi.mocked(authApi.refresh).mockReturnValue(
      new Promise<string>(r => { resolveRefresh = r })
    )

    let unmount!: () => void
    await act(async () => {
      const result = render(<AuthProvider><Probe /></AuthProvider>)
      unmount = result.unmount
    })

    // Unmount before refresh resolves
    act(() => unmount())

    // Resolving after unmount must not throw or trigger a React warning.
    // Without the mounted guard this would call setUser on a dead component.
    await act(async () => { resolveRefresh('late-token') })
  })
})
