// @vitest-environment jsdom
//
// VITE_API_URL is set to 'http://localhost:8080' via vite.config.ts test.env
// so BASE_URL is populated when the client module is first loaded.

import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

import { setToken, getToken, api } from './client'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function textResponse(text: string, status = 200) {
  return new Response(text, {
    status,
    headers: { 'Content-Type': 'text/plain' },
  })
}

beforeEach(() => {
  mockFetch.mockReset()
  sessionStorage.clear()
  localStorage.clear()
  setToken(null)
})

// ── token helpers ─────────────────────────────────────────────────────────────

describe('getToken / setToken', () => {
  it('returns null initially', () => {
    expect(getToken()).toBeNull()
  })

  it('stores and retrieves a token', () => {
    setToken('abc')
    expect(getToken()).toBe('abc')
  })

  it('clears token and removes from sessionStorage', () => {
    setToken('abc')
    setToken(null)
    expect(getToken()).toBeNull()
    expect(sessionStorage.getItem('token')).toBeNull()
  })
})

// ── GET ───────────────────────────────────────────────────────────────────────

describe('api.get', () => {
  it('makes a GET request to the correct URL', async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ ok: true }))
    await api.get('/api/v1/health')
    const [url, opts] = mockFetch.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('http://localhost:8080/api/v1/health')
    expect(opts.method).toBe('GET')
  })

  it('attaches Bearer token when set', async () => {
    setToken('my-token')
    mockFetch.mockResolvedValueOnce(jsonResponse({ ok: true }))
    await api.get('/api/v1/users')
    const [, opts] = mockFetch.mock.calls[0] as [string, RequestInit]
    expect((opts.headers as Record<string, string>).Authorization).toBe('Bearer my-token')
  })

  it('omits Authorization header when no token is set', async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ ok: true }))
    await api.get('/api/v1/health')
    const [, opts] = mockFetch.mock.calls[0] as [string, RequestInit]
    expect((opts.headers as Record<string, string>).Authorization).toBeUndefined()
  })

  it('returns parsed JSON', async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ data: 42 }))
    const result = await api.get<{ data: number }>('/api/v1/something')
    expect(result).toEqual({ data: 42 })
  })

  it('returns null for non-JSON content-type', async () => {
    mockFetch.mockResolvedValueOnce(textResponse('OK'))
    const result = await api.get('/api/v1/health')
    expect(result).toBeNull()
  })
})

// ── POST ──────────────────────────────────────────────────────────────────────

describe('api.post', () => {
  it('sends JSON body with POST', async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ id: 1 }))
    await api.post('/api/v1/login', { username: 'alice', password: 'pw' })
    const [url, opts] = mockFetch.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('http://localhost:8080/api/v1/login')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body as string)).toEqual({ username: 'alice', password: 'pw' })
  })
})

// ── DELETE ────────────────────────────────────────────────────────────────────

describe('api.delete', () => {
  it('sends a DELETE request and returns null for empty body', async () => {
    mockFetch.mockResolvedValueOnce(textResponse(''))
    const result = await api.delete('/api/v1/messages/42')
    const [url, opts] = mockFetch.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('http://localhost:8080/api/v1/messages/42')
    expect(opts.method).toBe('DELETE')
    expect(result).toBeNull()
  })
})

// ── 401 / retry ───────────────────────────────────────────────────────────────

describe('401 handling', () => {
  it('does NOT retry 401 on /api/v1/login', async () => {
    mockFetch.mockResolvedValueOnce(textResponse('Unauthorized', 401))
    await expect(api.post('/api/v1/login', {})).rejects.toThrow()
    expect(mockFetch).toHaveBeenCalledOnce()
  })

  it('does NOT retry 401 on /api/v1/refresh', async () => {
    mockFetch.mockResolvedValueOnce(textResponse('Unauthorized', 401))
    await expect(api.post('/api/v1/refresh', {})).rejects.toThrow()
    expect(mockFetch).toHaveBeenCalledOnce()
  })

  it('clears token and user from storage when refresh fails after 401', async () => {
    setToken('expired')
    localStorage.setItem('user', JSON.stringify({ id: 1, username: 'alice' }))

    // First call → 401. The client will try to refresh via dynamic import of auth.
    // The auth module's refresh() calls api.post('/api/v1/refresh') internally,
    // which triggers another fetch call → also 401 → refresh clears state.
    mockFetch
      .mockResolvedValueOnce(textResponse('Unauthorized', 401)) // original request
      .mockResolvedValueOnce(textResponse('Unauthorized', 401)) // refresh attempt

    await expect(api.get('/api/v1/users')).rejects.toThrow()

    // After failed refresh the client clears user from localStorage
    expect(localStorage.getItem('user')).toBeNull()
  })
})

// ── errors ────────────────────────────────────────────────────────────────────

describe('non-2xx errors', () => {
  it('throws with the response body as the message', async () => {
    mockFetch.mockResolvedValueOnce(textResponse('bad request', 400))
    await expect(api.post('/api/v1/register', {})).rejects.toThrow('bad request')
  })

  it('throws on 500', async () => {
    mockFetch.mockResolvedValueOnce(textResponse('Internal Server Error', 500))
    await expect(api.get('/api/v1/messages')).rejects.toThrow()
  })
})
