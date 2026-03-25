const BASE_URL = import.meta.env.VITE_API_URL

if (!BASE_URL) {
  throw new Error('VITE_API_URL is not set')
}

let token: string | null = sessionStorage.getItem('token')

export function setToken(t: string | null) {
  token = t
  if (t) sessionStorage.setItem('token', t)
  else sessionStorage.removeItem('token')
}

export function getToken() {
  return token
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  if (token) headers['Authorization'] = `Bearer ${token}`

  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })

  // Auto-clear stale token on 401 so the user is returned to the login screen
  // rather than being silently stuck with an expired/invalid session.
  if (res.status === 401) {
    setToken(null)
    sessionStorage.removeItem('user')
  }

  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }

  const ct = res.headers.get('content-type') || ''
  if (!ct.includes('application/json')) return null as T
  return res.json() as Promise<T>
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body: unknown) => request<T>('POST', path, body),
}