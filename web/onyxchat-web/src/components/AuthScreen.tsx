import { useState } from 'react'
import { useAuth } from '../context/AuthContext'

export function AuthScreen() {
  const { login, register } = useAuth()
  const [tab, setTab] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [inviteCode, setInviteCode] = useState('')

  async function handleSubmit() {
    setError('')
    if (!username || !password) return setError('Username and password required.')
    if (tab === 'register' && password.length < 8) return setError('Password must be at least 8 characters.')

    setLoading(true)
    try {
      if (tab === 'login') await login(username, password)
      else await register(username, password, inviteCode)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{
      position: 'fixed', inset: 0,
      background: 'var(--bg)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      padding: '20px',
    }}>
      <div style={{
        width: '100%', maxWidth: '360px',
        background: 'var(--bg-2)',
        border: '1px solid var(--border-2)',
        borderRadius: '20px',
        padding: '36px 32px',
      }}>
        {/* Logo */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '28px' }}>
          <svg width="36" height="36" viewBox="0 0 36 36" fill="none">
            <rect width="36" height="36" rx="8" fill="#050d1e"/>
            <circle cx="18" cy="18" r="9" stroke="#2563eb" strokeWidth="2"/>
            <circle cx="18" cy="18" r="4" fill="#2563eb"/>
            <circle cx="18" cy="9" r="2" fill="#2563eb" opacity="0.4"/>
          </svg>
          <span style={{ fontSize: '20px', fontWeight: 700, letterSpacing: '-0.02em', color: 'var(--text)' }}>
            OnyxChat
          </span>
        </div>

        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', marginBottom: '24px' }}>
          {(['login', 'register'] as const).map(t => (
            <button key={t} onClick={() => { setTab(t); setError('') }} style={{
              flex: 1, padding: '8px 0', textAlign: 'center',
              fontSize: '13px', fontWeight: 500, cursor: 'pointer',
              background: 'transparent', border: 'none',
              borderBottom: `2px solid ${tab === t ? 'var(--blue)' : 'transparent'}`,
              marginBottom: '-1px',
              color: tab === t ? 'var(--blue)' : 'var(--text-mute)',
              transition: 'color 0.15s, border-color 0.15s',
            }}>
              {t === 'login' ? 'Sign in' : 'Register'}
            </button>
          ))}
        </div>

        {/* Error */}
        {error && (
          <div style={{
            fontSize: '12px', color: 'var(--red)',
            background: 'rgba(239,68,68,0.08)',
            border: '1px solid rgba(239,68,68,0.2)',
            borderRadius: '8px', padding: '8px 12px', marginBottom: '12px',
          }}>
            {error}
          </div>
        )}

        {/* Fields */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div>
            <div style={{ fontSize: '12px', fontWeight: 500, color: 'var(--text-dim)', marginBottom: '4px' }}>
              Username
            </div>
            <input
              value={username}
              onChange={e => setUsername(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSubmit()}
              placeholder="your username"
              autoComplete="username"
              style={{
                width: '100%', background: 'var(--bg-3)',
                border: '1px solid var(--border)', borderRadius: '8px',
                color: 'var(--text)', fontSize: '14px', padding: '10px 14px',
                outline: 'none', boxSizing: 'border-box',
              }}
            />
          </div>
          <div>
            <div style={{ fontSize: '12px', fontWeight: 500, color: 'var(--text-dim)', marginBottom: '4px' }}>
              Password
            </div>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSubmit()}
              placeholder={tab === 'register' ? 'min 8 characters' : '••••••••'}
              autoComplete={tab === 'login' ? 'current-password' : 'new-password'}
              style={{
                width: '100%', background: 'var(--bg-3)',
                border: '1px solid var(--border)', borderRadius: '8px',
                color: 'var(--text)', fontSize: '14px', padding: '10px 14px',
                outline: 'none', boxSizing: 'border-box',
              }}
            />
          </div>

          {/* Invite code — register tab only */}
          {tab === 'register' && (
            <div>
              <div style={{ fontSize: '12px', fontWeight: 500, color: 'var(--text-dim)', marginBottom: '4px' }}>
                Invite Code
              </div>
              <input
                value={inviteCode}
                onChange={e => setInviteCode(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleSubmit()}
                placeholder="xxxx-xxxx-xxxx"
                style={{
                  width: '100%', background: 'var(--bg-3)',
                  border: '1px solid var(--border)', borderRadius: '8px',
                  color: 'var(--text)', fontSize: '14px', padding: '10px 14px',
                  outline: 'none', boxSizing: 'border-box',
                }}
              />
            </div>
          )}

          <button
            onClick={handleSubmit}
            disabled={loading}
            style={{
              width: '100%', padding: '11px',
              background: 'var(--blue)', border: 'none',
              borderRadius: '8px', color: 'white',
              fontSize: '14px', fontWeight: 600, cursor: 'pointer',
              marginTop: '4px', opacity: loading ? 0.5 : 1,
            }}
          >
            {loading ? 'Please wait…' : tab === 'login' ? 'Sign in' : 'Create account'}
          </button>

          {/* Privacy policy link — shown on register tab */}
          {tab === 'register' && (
            <p style={{
              fontSize: '11px', color: 'var(--text-mute)',
              textAlign: 'center', margin: '4px 0 0',
              lineHeight: 1.5,
            }}>
              By creating an account you agree to our{' '}
              <a
                href="/privacy"
                target="_blank"
                rel="noopener noreferrer"
                style={{ color: 'var(--blue)', textDecoration: 'none' }}
              >
                Privacy Policy
              </a>
            </p>
          )}
        </div>

        {/* Footer privacy link — always visible */}
        <div style={{
          marginTop: '24px',
          textAlign: 'center',
          fontSize: '11px',
          color: 'var(--text-mute)',
        }}>
          <a
            href="/privacy"
            target="_blank"
            rel="noopener noreferrer"
            style={{ color: 'var(--text-mute)', textDecoration: 'none' }}
          >
            Privacy Policy
          </a>
        </div>
      </div>
    </div>
  )
}
